package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aichain/internal/protocol"
)

func (s *Store) syncValidatorRegistry(ctx context.Context) error {
	for _, validator := range s.cfg.Genesis.Validators {
		if _, err := s.db.ExecContext(
			ctx,
			`INSERT INTO validator_registry (address, public_key, power, active, updated_at)
			 VALUES ($1, $2, $3, TRUE, NOW())
			 ON CONFLICT (address) DO NOTHING`,
			validator.Address,
			validator.PublicKey,
			validator.Power,
		); err != nil {
			return fmt.Errorf("sync validator registry for %s: %w", validator.Address, err)
		}
	}
	return nil
}

func (s *Store) ListValidators(ctx context.Context) ([]protocol.Validator, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT address, public_key, power
		 FROM validator_registry
		 WHERE active = TRUE
		 ORDER BY address ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query validator registry: %w", err)
	}
	defer rows.Close()

	validators := make([]protocol.Validator, 0)
	for rows.Next() {
		var validator protocol.Validator
		if err := rows.Scan(&validator.Address, &validator.PublicKey, &validator.Power); err != nil {
			return nil, fmt.Errorf("scan validator registry: %w", err)
		}
		validator.Active = true
		validators = append(validators, validator)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validator registry: %w", err)
	}

	return validators, nil
}

func (s *Store) ListValidatorRegistry(ctx context.Context) ([]protocol.Validator, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT address, public_key, power, active
		 FROM validator_registry
		 ORDER BY address ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query full validator registry: %w", err)
	}
	defer rows.Close()

	validators := make([]protocol.Validator, 0)
	for rows.Next() {
		var validator protocol.Validator
		if err := rows.Scan(&validator.Address, &validator.PublicKey, &validator.Power, &validator.Active); err != nil {
			return nil, fmt.Errorf("scan full validator registry: %w", err)
		}
		validators = append(validators, validator)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate full validator registry: %w", err)
	}

	return validators, nil
}

func (s *Store) UpsertPeer(ctx context.Context, status protocol.PeerStatus) error {
	status.NodeID = strings.TrimSpace(status.NodeID)
	status.ListenAddr = strings.TrimSpace(status.ListenAddr)
	status.ChainID = strings.TrimSpace(status.ChainID)
	status.ValidatorAddress = strings.TrimSpace(status.ValidatorAddress)
	if status.NodeID == "" || status.ListenAddr == "" || status.ChainID == "" {
		return nil
	}
	if status.ObservedAt.IsZero() {
		status.ObservedAt = time.Now().UTC()
	}

	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO peer_registry (node_id, chain_id, listen_addr, validator_address, head_height, head_hash, observed_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		 ON CONFLICT (node_id) DO UPDATE
		 SET chain_id = EXCLUDED.chain_id,
		     listen_addr = EXCLUDED.listen_addr,
		     validator_address = EXCLUDED.validator_address,
		     head_height = EXCLUDED.head_height,
		     head_hash = EXCLUDED.head_hash,
		     observed_at = EXCLUDED.observed_at,
		     updated_at = NOW()`,
		status.NodeID,
		status.ChainID,
		status.ListenAddr,
		nullIfEmpty(status.ValidatorAddress),
		status.HeadHeight,
		status.HeadHash,
		status.ObservedAt,
	); err != nil {
		return fmt.Errorf("upsert peer registry: %w", err)
	}

	return nil
}

func (s *Store) ListKnownPeers(ctx context.Context, limit int) ([]protocol.PeerStatus, error) {
	if limit <= 0 {
		limit = 256
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT node_id, chain_id, listen_addr, COALESCE(validator_address, ''), head_height, head_hash, observed_at
		 FROM peer_registry
		 WHERE chain_id = $1
		 ORDER BY observed_at DESC, node_id ASC
		 LIMIT $2`,
		s.cfg.ChainID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query peer registry: %w", err)
	}
	defer rows.Close()

	peers := make([]protocol.PeerStatus, 0, limit)
	for rows.Next() {
		var peer protocol.PeerStatus
		if err := rows.Scan(&peer.NodeID, &peer.ChainID, &peer.ListenAddr, &peer.ValidatorAddress, &peer.HeadHeight, &peer.HeadHash, &peer.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan peer registry: %w", err)
		}
		peers = append(peers, peer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate peer registry: %w", err)
	}

	return peers, nil
}

func (s *Store) ListForkChoicePreferences(ctx context.Context, limit int) ([]protocol.ForkChoicePreference, error) {
	if limit <= 0 {
		limit = 256
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT payload_json, updated_at
		 FROM fork_choice_preferences
		 ORDER BY height ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query fork choice preferences: %w", err)
	}
	defer rows.Close()

	preferences := make([]protocol.ForkChoicePreference, 0, limit)
	for rows.Next() {
		var (
			payload   []byte
			updatedAt time.Time
		)
		if err := rows.Scan(&payload, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan fork choice preference: %w", err)
		}
		var certificate protocol.QuorumCertificate
		if err := json.Unmarshal(payload, &certificate); err != nil {
			return nil, fmt.Errorf("decode fork choice preference: %w", err)
		}
		preferences = append(preferences, protocol.ForkChoicePreference{
			Height:      certificate.Height,
			Certificate: certificate,
			UpdatedAt:   updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fork choice preferences: %w", err)
	}

	return preferences, nil
}

func recordForkChoicePreference(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, certificate protocol.QuorumCertificate) error {
	var existingPayload []byte
	err := querier.QueryRowContext(
		ctx,
		`SELECT payload_json
		 FROM fork_choice_preferences
		 WHERE height = $1`,
		certificate.Height,
	).Scan(&existingPayload)
	switch {
	case err == nil:
		var current protocol.QuorumCertificate
		if err := json.Unmarshal(existingPayload, &current); err != nil {
			return fmt.Errorf("decode fork choice certificate: %w", err)
		}
		if !forkChoiceBetterCertificate(certificate, current) {
			return nil
		}
	case err != sql.ErrNoRows:
		return fmt.Errorf("query fork choice preference: %w", err)
	}

	payload, err := json.Marshal(certificate)
	if err != nil {
		return fmt.Errorf("marshal fork choice certificate: %w", err)
	}
	if _, err := querier.ExecContext(
		ctx,
		`INSERT INTO fork_choice_preferences (height, block_hash, updated_at, payload_json)
		 VALUES ($1, $2, NOW(), $3)
		 ON CONFLICT (height) DO UPDATE
		 SET block_hash = EXCLUDED.block_hash,
		     updated_at = NOW(),
		     payload_json = EXCLUDED.payload_json`,
		certificate.Height,
		certificate.BlockHash,
		payload,
	); err != nil {
		return fmt.Errorf("persist fork choice preference: %w", err)
	}

	return nil
}

func persistForkChoicePreferenceTx(ctx context.Context, tx *sql.Tx, certificate protocol.QuorumCertificate) error {
	return recordForkChoicePreference(ctx, tx, certificate)
}

func forkChoiceBetterCertificate(candidate protocol.QuorumCertificate, current protocol.QuorumCertificate) bool {
	if candidate.Height != current.Height {
		return candidate.Height > current.Height
	}
	if candidate.Round != current.Round {
		return candidate.Round > current.Round
	}
	if candidate.Power != current.Power {
		return candidate.Power > current.Power
	}
	if !candidate.CertifiedAt.Equal(current.CertifiedAt) {
		return candidate.CertifiedAt.Before(current.CertifiedAt)
	}
	return candidate.BlockHash < current.BlockHash
}

func isActiveValidatorTx(ctx context.Context, tx *sql.Tx, address string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM validator_registry
		 WHERE address = $1 AND active = TRUE`,
		strings.TrimSpace(address),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("query active validator: %w", err)
	}
	return count > 0, nil
}

func countActiveValidatorsTx(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM validator_registry
		 WHERE active = TRUE`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active validators: %w", err)
	}
	return count, nil
}

func ensureAgentExistsTx(ctx context.Context, tx *sql.Tx, address string, defaultReputation float64) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("%w: agent address is required", ErrValidation)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO agents (address, next_nonce, balance, reputation, created_at, updated_at)
		 VALUES ($1, 0, 0, $2, NOW(), NOW())
		 ON CONFLICT (address) DO NOTHING`,
		address,
		defaultReputation,
	); err != nil {
		return fmt.Errorf("ensure validator agent account: %w", err)
	}
	return nil
}

func upsertValidatorRegistryTx(ctx context.Context, tx *sql.Tx, address string, publicKey string, power int64, defaultReputation float64) error {
	address = strings.TrimSpace(address)
	publicKey = strings.ToLower(strings.TrimSpace(publicKey))
	if address == "" || publicKey == "" || power <= 0 {
		return fmt.Errorf("%w: invalid validator update", ErrValidation)
	}
	decoded, err := hex.DecodeString(publicKey)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("%w: validator public_key must be a 32-byte hex ed25519 key", ErrValidation)
	}
	if err := ensureAgentExistsTx(ctx, tx, address, defaultReputation); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO validator_registry (address, public_key, power, active, created_at, updated_at)
		 VALUES ($1, $2, $3, TRUE, NOW(), NOW())
		 ON CONFLICT (address) DO UPDATE
		 SET public_key = EXCLUDED.public_key,
		     power = EXCLUDED.power,
		     active = TRUE,
		     updated_at = NOW()`,
		address,
		publicKey,
		power,
	); err != nil {
		return fmt.Errorf("upsert validator registry: %w", err)
	}
	return nil
}

func deactivateValidatorRegistryTx(ctx context.Context, tx *sql.Tx, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("%w: validator is required", ErrValidation)
	}
	activeCount, err := countActiveValidatorsTx(ctx, tx)
	if err != nil {
		return err
	}
	if activeCount <= 1 {
		return fmt.Errorf("%w: cannot deactivate the last active validator", ErrValidation)
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE validator_registry
		 SET active = FALSE,
		     updated_at = NOW()
		 WHERE address = $1 AND active = TRUE`,
		address,
	)
	if err != nil {
		return fmt.Errorf("deactivate validator registry: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deactivated validator rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
