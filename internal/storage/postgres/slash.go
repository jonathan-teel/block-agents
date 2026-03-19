package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"aichain/internal/config"
	"aichain/internal/execution"
	"aichain/internal/protocol"
)

func applyConsensusEvidencePenaltiesTx(ctx context.Context, tx *sql.Tx, cfg config.Config, nowUnix int64, slashFraction float64, reputationPenalty float64) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, validator, evidence_type
		 FROM consensus_evidence
		 WHERE processed_at IS NULL
		   AND observed_at <= TO_TIMESTAMP($1)
		 ORDER BY id ASC
		 FOR UPDATE`,
		nowUnix,
	)
	if err != nil {
		return nil, fmt.Errorf("query unprocessed consensus evidence: %w", err)
	}
	defer rows.Close()

	type evidenceRef struct {
		ID           int64
		Validator    string
		EvidenceType string
	}
	refs := make([]evidenceRef, 0)
	for rows.Next() {
		var item evidenceRef
		if err := rows.Scan(&item.ID, &item.Validator, &item.EvidenceType); err != nil {
			return nil, fmt.Errorf("scan consensus evidence: %w", err)
		}
		refs = append(refs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate consensus evidence: %w", err)
	}

	events := make([]protocol.Event, 0, len(refs))
	for _, item := range refs {
		var (
			balance    protocol.Amount
			reputation float64
		)
		err := tx.QueryRowContext(
			ctx,
			`SELECT balance, reputation
			 FROM agents
			 WHERE address = $1
			 FOR UPDATE`,
			item.Validator,
		).Scan(&balance, &reputation)
		if err != nil {
			if err == sql.ErrNoRows {
				if _, err := tx.ExecContext(
					ctx,
					`UPDATE consensus_evidence
					 SET processed_at = NOW()
					 WHERE id = $1`,
					item.ID,
				); err != nil {
					return nil, fmt.Errorf("mark evidence processed without validator account: %w", err)
				}
				continue
			}
			return nil, fmt.Errorf("lock validator account for slashing: %w", err)
		}

		balancePenalty := execution.ScaleAmount(balance, slashFraction)
		if balancePenalty > balance {
			balancePenalty = balance
		}
		newReputation := execution.Clamp01(reputation - reputationPenalty)

		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance - $1,
			     reputation = $2,
			     updated_at = NOW()
			 WHERE address = $3`,
			balancePenalty,
			newReputation,
			item.Validator,
		); err != nil {
			return nil, fmt.Errorf("apply slash to validator %s: %w", item.Validator, err)
		}
		if err := creditTreasuryTx(ctx, tx, cfg, balancePenalty); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE consensus_evidence
			 SET processed_at = NOW(),
			     applied_balance_penalty = $2,
			     applied_reputation_penalty = $3
			 WHERE id = $1`,
			item.ID,
			balancePenalty,
			reputationPenalty,
		); err != nil {
			return nil, fmt.Errorf("mark evidence processed: %w", err)
		}

		events = append(events, protocol.Event{
			Type: "validator.slashed",
			Attributes: map[string]string{
				"validator":           item.Validator,
				"evidence_id":         fmt.Sprintf("%d", item.ID),
				"evidence_type":       item.EvidenceType,
				"balance_penalty":     formatAmount(balancePenalty),
				"reputation_penalty":  formatFloat(reputationPenalty),
				"treasury":            cfg.TreasuryAddress,
			},
		})
	}

	return events, nil
}
