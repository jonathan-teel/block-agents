package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aichain/internal/protocol"
)

func (s *Store) executeOpenDisputeTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.OpenDisputeRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode open_dispute payload: %w", err)
	}
	if pending.Sender != payload.Challenger {
		return nil, fmt.Errorf("%w: sender does not match challenger", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Status != protocol.StatusSettled {
		return nil, fmt.Errorf("%w: only settled tasks can be disputed", ErrValidation)
	}

	settledAt, err := getTaskSettledAtTx(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	disputeWindow := effectiveTaskDisputeWindowTx(ctx, tx, s.cfg)
	disputeBond := effectiveTaskDisputeBondTx(ctx, tx, s.cfg)
	if settledAt.Add(disputeWindow).Before(time.Unix(nowUnix, 0).UTC()) {
		return nil, fmt.Errorf("%w: dispute window has expired", ErrValidation)
	}
	if open, err := hasOpenTaskDisputeTx(ctx, tx, task.ID); err != nil {
		return nil, err
	} else if open {
		return nil, fmt.Errorf("%w: task already has an open dispute", ErrValidation)
	}

	balance, err := lockBalanceTx(ctx, tx, payload.Challenger)
	if err != nil {
		return nil, err
	}
	if balance < disputeBond {
		return nil, ErrInsufficientBalance
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance - $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		disputeBond,
		payload.Challenger,
	); err != nil {
		return nil, fmt.Errorf("lock dispute bond: %w", err)
	}

	var disputeID int64
	var openedAt time.Time
	if err := tx.QueryRowContext(
		ctx,
		`INSERT INTO task_disputes (task_id, challenger, bond, reason, status, opened_at)
		 VALUES ($1, $2, $3, $4, 'open', NOW())
		 RETURNING id, opened_at`,
		task.ID,
		payload.Challenger,
		disputeBond,
		payload.Reason,
	).Scan(&disputeID, &openedAt); err != nil {
		return nil, fmt.Errorf("insert task dispute: %w", err)
	}

	return []protocol.Event{
		{
			Type: "task.dispute_opened",
			Attributes: map[string]string{
				"dispute_id":  strconv.FormatInt(disputeID, 10),
				"task_id":     task.ID,
				"challenger":  payload.Challenger,
				"bond":        formatFloat(disputeBond),
				"opened_at":   openedAt.UTC().Format(time.RFC3339),
			},
		},
	}, nil
}

func (s *Store) executeResolveDisputeTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	var payload protocol.ResolveDisputeRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode resolve_dispute payload: %w", err)
	}
	if pending.Sender != payload.Resolver {
		return nil, fmt.Errorf("%w: sender does not match resolver", ErrValidation)
	}
	active, err := isActiveValidatorTx(ctx, tx, payload.Resolver)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: resolver must be an active validator", ErrUnauthorized)
	}

	dispute, err := getTaskDisputeForUpdateTx(ctx, tx, payload.DisputeID)
	if err != nil {
		return nil, err
	}
	if dispute.Status != "open" {
		return nil, fmt.Errorf("%w: dispute is not open", ErrValidation)
	}

	switch payload.Resolution {
	case "reject":
		if err := creditTreasuryTx(ctx, tx, s.cfg, dispute.Bond); err != nil {
			return nil, err
		}
		if err := updateTaskDisputeResolutionTx(ctx, tx, dispute.ID, payload.Resolver, "rejected", payload.Resolution, payload.Notes); err != nil {
			return nil, err
		}
	case "uphold":
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			dispute.Bond,
			dispute.Challenger,
		); err != nil {
			return nil, fmt.Errorf("refund dispute bond: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE tasks
			 SET status = $2,
			     updated_at = NOW()
			 WHERE id = $1`,
			dispute.TaskID,
			protocol.StatusDisputed,
		); err != nil {
			return nil, fmt.Errorf("mark task disputed: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE task_results
			 SET settled = FALSE,
			     settled_at = NULL,
			     updated_at = NOW()
			 WHERE task_id = $1`,
			dispute.TaskID,
		); err != nil {
			return nil, fmt.Errorf("reopen disputed task result: %w", err)
		}
		if err := updateTaskDisputeResolutionTx(ctx, tx, dispute.ID, payload.Resolver, "upheld", payload.Resolution, payload.Notes); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: unsupported dispute resolution", ErrValidation)
	}

	return []protocol.Event{
		{
			Type: "task.dispute_resolved",
			Attributes: map[string]string{
				"dispute_id": strconv.FormatInt(dispute.ID, 10),
				"task_id":    dispute.TaskID,
				"resolver":   payload.Resolver,
				"resolution": payload.Resolution,
			},
		},
	}, nil
}

func getTaskSettledAtTx(ctx context.Context, tx *sql.Tx, taskID string) (time.Time, error) {
	var settledAt sql.NullTime
	var settled bool
	err := tx.QueryRowContext(
		ctx,
		`SELECT settled, settled_at
		 FROM task_results
		 WHERE task_id = $1`,
		strings.TrimSpace(taskID),
	).Scan(&settled, &settledAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrNotFound
		}
		return time.Time{}, fmt.Errorf("query task settlement time: %w", err)
	}
	if !settled || !settledAt.Valid {
		return time.Time{}, fmt.Errorf("%w: task is not settled", ErrValidation)
	}
	return settledAt.Time.UTC(), nil
}

func hasOpenTaskDisputeTx(ctx context.Context, tx *sql.Tx, taskID string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM task_disputes
		 WHERE task_id = $1 AND status = 'open'`,
		strings.TrimSpace(taskID),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("query open task disputes: %w", err)
	}
	return count > 0, nil
}

func getTaskDisputeForUpdateTx(ctx context.Context, tx *sql.Tx, disputeID int64) (protocol.TaskDispute, error) {
	var (
		dispute    protocol.TaskDispute
		resolver   sql.NullString
		resolution sql.NullString
		notes      sql.NullString
		resolvedAt sql.NullTime
	)
	err := tx.QueryRowContext(
		ctx,
		`SELECT id, task_id, challenger, bond, reason, status, resolver, resolution, notes, opened_at, resolved_at
		 FROM task_disputes
		 WHERE id = $1
		 FOR UPDATE`,
		disputeID,
	).Scan(&dispute.ID, &dispute.TaskID, &dispute.Challenger, &dispute.Bond, &dispute.Reason, &dispute.Status, &resolver, &resolution, &notes, &dispute.OpenedAt, &resolvedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.TaskDispute{}, ErrNotFound
		}
		return protocol.TaskDispute{}, fmt.Errorf("query task dispute: %w", err)
	}
	dispute.Resolver = resolver.String
	dispute.Resolution = resolution.String
	dispute.Notes = notes.String
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC()
		dispute.ResolvedAt = &value
	}
	return dispute, nil
}

func updateTaskDisputeResolutionTx(ctx context.Context, tx *sql.Tx, disputeID int64, resolver string, status string, resolution string, notes string) error {
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE task_disputes
		 SET status = $2,
		     resolver = $3,
		     resolution = $4,
		     notes = $5,
		     resolved_at = NOW()
		 WHERE id = $1`,
		disputeID,
		status,
		resolver,
		resolution,
		notes,
	); err != nil {
		return fmt.Errorf("update task dispute resolution: %w", err)
	}
	return nil
}

func (s *Store) listDisputesByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.TaskDispute, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, challenger, bond, reason, status, COALESCE(resolver, ''), COALESCE(resolution, ''), COALESCE(notes, ''), opened_at, resolved_at
		 FROM task_disputes
		 WHERE task_id = $1
		 ORDER BY id ASC`,
		strings.TrimSpace(taskID),
	)
	if err != nil {
		return nil, fmt.Errorf("query task disputes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.TaskDispute, 0)
	for rows.Next() {
		var (
			item       protocol.TaskDispute
			resolvedAt sql.NullTime
		)
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Challenger, &item.Bond, &item.Reason, &item.Status, &item.Resolver, &item.Resolution, &item.Notes, &item.OpenedAt, &resolvedAt); err != nil {
			return nil, fmt.Errorf("scan task dispute: %w", err)
		}
		if resolvedAt.Valid {
			value := resolvedAt.Time.UTC()
			item.ResolvedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task disputes: %w", err)
	}
	return items, nil
}

func listAllDisputesTx(ctx context.Context, tx *sql.Tx) ([]protocol.TaskDispute, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, challenger, bond, reason, status, COALESCE(resolver, ''), COALESCE(resolution, ''), COALESCE(notes, ''), opened_at, resolved_at
		 FROM task_disputes
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot disputes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.TaskDispute, 0)
	for rows.Next() {
		var (
			item       protocol.TaskDispute
			resolvedAt sql.NullTime
		)
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Challenger, &item.Bond, &item.Reason, &item.Status, &item.Resolver, &item.Resolution, &item.Notes, &item.OpenedAt, &resolvedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot dispute: %w", err)
		}
		if resolvedAt.Valid {
			value := resolvedAt.Time.UTC()
			item.ResolvedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot disputes: %w", err)
	}
	return items, nil
}

func importDisputesTx(ctx context.Context, tx *sql.Tx, items []protocol.TaskDispute) error {
	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_disputes (id, task_id, challenger, bond, reason, status, resolver, resolution, notes, opened_at, resolved_at, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, $11, $10)`,
			item.ID,
			item.TaskID,
			item.Challenger,
			item.Bond,
			item.Reason,
			item.Status,
			item.Resolver,
			item.Resolution,
			item.Notes,
			item.OpenedAt,
			nullableTime(item.ResolvedAt),
		); err != nil {
			return fmt.Errorf("import task dispute %d: %w", item.ID, err)
		}
	}
	return nil
}
