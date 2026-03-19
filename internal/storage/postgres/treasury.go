package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"aichain/internal/config"
)

func ensureTreasuryAccountTx(ctx context.Context, tx *sql.Tx, cfg config.Config) error {
	return ensureAgentExistsTx(ctx, tx, cfg.TreasuryAddress, cfg.DefaultAgentReputation)
}

func creditTreasuryTx(ctx context.Context, tx *sql.Tx, cfg config.Config, amount float64) error {
	if amount <= 0 {
		return nil
	}
	if err := ensureTreasuryAccountTx(ctx, tx, cfg); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance + $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		amount,
		cfg.TreasuryAddress,
	); err != nil {
		return fmt.Errorf("credit treasury balance: %w", err)
	}
	return nil
}
