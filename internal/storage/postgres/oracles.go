package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"aichain/internal/execution"
	"aichain/internal/protocol"
)

func (s *Store) ListPendingOracleTasks(ctx context.Context, now time.Time, limit int) ([]protocol.Task, error) {
	if limit <= 0 {
		limit = 64
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, role_selection_policy, oracle_source, oracle_endpoint, oracle_path, reward_pool, min_stake, status, created_at
		 FROM tasks
		 WHERE (type = $1 OR type = $2)
		   AND status = $3
		   AND oracle_source <> ''
		   AND deadline <= $4
		 ORDER BY deadline ASC, created_at ASC
		 LIMIT $5`,
		protocol.TaskTypePrediction,
		protocol.TaskTypeOraclePrediction,
		protocol.StatusOpen,
		now.Unix(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending oracle tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]protocol.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending oracle tasks: %w", err)
	}
	return tasks, nil
}

func (s *Store) RecordOracleReport(ctx context.Context, report protocol.OracleReport) error {
	report.TaskID = strings.TrimSpace(report.TaskID)
	report.Source = strings.TrimSpace(report.Source)
	report.Endpoint = strings.TrimSpace(report.Endpoint)
	report.Path = strings.TrimSpace(report.Path)
	report.RawHash = strings.TrimSpace(report.RawHash)
	if report.TaskID == "" || report.Source == "" || report.Endpoint == "" || report.Path == "" || report.RawHash == "" {
		return fmt.Errorf("%w: oracle report is incomplete", ErrValidation)
	}
	if report.Value < 0 || report.Value > 1 {
		return fmt.Errorf("%w: oracle report value must be within [0,1]", ErrValidation)
	}
	if report.ObservedAt.IsZero() {
		report.ObservedAt = time.Now().UTC()
	}
	var existingHash sql.NullString
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT raw_hash
		 FROM oracle_reports
		 WHERE task_id = $1
		 ORDER BY observed_at DESC, id DESC
		 LIMIT 1`,
		report.TaskID,
	).Scan(&existingHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query latest oracle report hash: %w", err)
	}
	if existingHash.Valid && existingHash.String == report.RawHash {
		return nil
	}

	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO oracle_reports (task_id, source, endpoint, path, value, observed_at, raw_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		report.TaskID,
		report.Source,
		report.Endpoint,
		report.Path,
		report.Value,
		report.ObservedAt.UTC(),
		report.RawHash,
	); err != nil {
		return fmt.Errorf("insert oracle report: %w", err)
	}
	return nil
}

func latestOracleReportByTaskTx(ctx context.Context, tx *sql.Tx, taskID string) (*protocol.OracleReport, error) {
	var report protocol.OracleReport
	err := tx.QueryRowContext(
		ctx,
		`SELECT id, task_id, source, endpoint, path, value, observed_at, raw_hash, created_at
		 FROM oracle_reports
		 WHERE task_id = $1
		 ORDER BY observed_at DESC, id DESC
		 LIMIT 1`,
		taskID,
	).Scan(&report.ID, &report.TaskID, &report.Source, &report.Endpoint, &report.Path, &report.Value, &report.ObservedAt, &report.RawHash, &report.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest oracle report: %w", err)
	}
	return &report, nil
}

func (s *Store) listOracleReportsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.OracleReport, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, source, endpoint, path, value, observed_at, raw_hash, created_at
		 FROM oracle_reports
		 WHERE task_id = $1
		 ORDER BY observed_at DESC, id DESC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query oracle reports: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.OracleReport, 0)
	for rows.Next() {
		var item protocol.OracleReport
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Source, &item.Endpoint, &item.Path, &item.Value, &item.ObservedAt, &item.RawHash, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan oracle report: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle reports: %w", err)
	}
	return items, nil
}

func listAllOracleReportsTx(ctx context.Context, tx *sql.Tx) ([]protocol.OracleReport, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, source, endpoint, path, value, observed_at, raw_hash, created_at
		 FROM oracle_reports
		 ORDER BY observed_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot oracle reports: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.OracleReport, 0)
	for rows.Next() {
		var item protocol.OracleReport
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Source, &item.Endpoint, &item.Path, &item.Value, &item.ObservedAt, &item.RawHash, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot oracle report: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot oracle reports: %w", err)
	}
	return items, nil
}

func importOracleReportsTx(ctx context.Context, tx *sql.Tx, items []protocol.OracleReport) error {
	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO oracle_reports (id, task_id, source, endpoint, path, value, observed_at, raw_hash, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			item.ID,
			item.TaskID,
			item.Source,
			item.Endpoint,
			item.Path,
			item.Value,
			item.ObservedAt,
			item.RawHash,
			item.CreatedAt,
		); err != nil {
			return fmt.Errorf("import oracle report %d: %w", item.ID, err)
		}
	}
	return nil
}

func resolvePredictionOutcomeTx(ctx context.Context, tx *sql.Tx, task protocol.Task) (float64, string, bool, error) {
	if strings.TrimSpace(task.Input.OracleSource) == "" {
		return execution.ResolveOutcome(task), "synthetic", true, nil
	}
	report, err := latestOracleReportByTaskTx(ctx, tx, task.ID)
	if err != nil {
		return 0, "", false, err
	}
	if report == nil {
		return 0, "", false, nil
	}
	return report.Value, report.Source, true, nil
}
