package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const latestSchemaVersion = 3

type migration struct {
	Version int
	Name    string
	SQL     string
}

var migrations = []migration{
	{
		Version: 1,
		Name:    "bootstrap_schema",
		SQL:     schemaSQL,
	},
	{
		Version: 2,
		Name:    "phase2_protocol_hardening",
		SQL: `
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS role_selection_policy TEXT NOT NULL DEFAULT 'balance_reputation';
ALTER TABLE tx_pool ADD COLUMN IF NOT EXISTS error_code TEXT NULL;
ALTER TABLE block_transactions ADD COLUMN IF NOT EXISTS error_code TEXT NULL;

CREATE TABLE IF NOT EXISTS agent_key_rotations (
	id BIGSERIAL PRIMARY KEY,
	agent TEXT NOT NULL REFERENCES agents(address) ON DELETE CASCADE,
	old_public_key TEXT NOT NULL,
	new_public_key TEXT NOT NULL,
	tx_hash TEXT NOT NULL UNIQUE,
	rotated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_key_rotations_agent ON agent_key_rotations(agent, rotated_at DESC);
`,
	},
	{
		Version: 3,
		Name:    "phase3_network_and_fork_state",
		SQL: `
CREATE TABLE IF NOT EXISTS peer_registry (
	node_id TEXT PRIMARY KEY,
	chain_id TEXT NOT NULL,
	listen_addr TEXT NOT NULL,
	validator_address TEXT NULL,
	head_height BIGINT NOT NULL DEFAULT 0,
	head_hash TEXT NOT NULL DEFAULT '',
	observed_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS validator_registry (
	address TEXT PRIMARY KEY,
	public_key TEXT NOT NULL,
	power BIGINT NOT NULL CHECK (power > 0),
	active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS fork_choice_preferences (
	height BIGINT PRIMARY KEY,
	block_hash TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	payload_json JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_peer_registry_observed_at ON peer_registry(observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_validator_registry_active ON validator_registry(active, address);
`,
	},
}

func (s *Store) initSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	); err != nil {
		return fmt.Errorf("init schema migrations table: %w", err)
	}

	applied, err := s.currentSchemaVersion(ctx)
	if err != nil {
		return err
	}

	for _, step := range migrations {
		if step.Version <= applied {
			continue
		}
		if _, err := s.db.ExecContext(ctx, step.SQL); err != nil {
			return fmt.Errorf("apply schema migration %d (%s): %w", step.Version, step.Name, err)
		}
		if _, err := s.db.ExecContext(
			ctx,
			`INSERT INTO schema_migrations (version, name, applied_at)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (version) DO UPDATE
			 SET name = EXCLUDED.name,
			     applied_at = EXCLUDED.applied_at`,
			step.Version,
			step.Name,
			time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("record schema migration %d: %w", step.Version, err)
		}
		applied = step.Version
	}

	s.schemaVersion = applied
	return nil
}

func (s *Store) currentSchemaVersion(ctx context.Context) (int, error) {
	var version sql.NullInt64
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT MAX(version)
		 FROM schema_migrations`,
	).Scan(&version); err != nil {
		return 0, fmt.Errorf("query current schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}
