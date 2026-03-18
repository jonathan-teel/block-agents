package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aichain/internal/protocol"
	"aichain/internal/txauth"
)

type authState struct {
	PublicKey sql.NullString
	NextNonce int64
}

func (s *Store) prepareTransaction(ctx context.Context, txType protocol.TxType, sender string, auth protocol.TxAuth, payload any, requireAuth bool) (protocol.Transaction, error) {
	if !requireAuth {
		auth = protocol.TxAuth{}
	}

	tx, err := protocol.NewTransaction(txType, sender, payload, auth, time.Now().UTC())
	if err != nil {
		return protocol.Transaction{}, err
	}

	if !requireAuth {
		return tx, nil
	}
	if err := txauth.VerifyTransaction(s.cfg.Genesis.ChainID, tx); err != nil {
		return protocol.Transaction{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	state, err := s.lookupAuthState(ctx, s.db, tx.Sender)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Transaction{}, ErrNotFound
		}
		return protocol.Transaction{}, err
	}
	if state.PublicKey.Valid && txauth.NormalizePublicKey(state.PublicKey.String) != tx.PublicKey {
		return protocol.Transaction{}, fmt.Errorf("%w: public key does not match registered agent identity", ErrUnauthorized)
	}
	if !state.PublicKey.Valid {
		pendingPublicKey, err := s.pendingSenderPublicKey(ctx, tx.Sender)
		if err != nil {
			return protocol.Transaction{}, err
		}
		if pendingPublicKey != "" && pendingPublicKey != tx.PublicKey {
			return protocol.Transaction{}, fmt.Errorf("%w: public key does not match pending sender identity", ErrUnauthorized)
		}
	}

	expectedNonce, err := s.nextEnqueueNonce(ctx, tx.Sender, state.NextNonce)
	if err != nil {
		return protocol.Transaction{}, err
	}
	if tx.Nonce != expectedNonce {
		return protocol.Transaction{}, fmt.Errorf("%w: expected nonce %d", ErrInvalidNonce, expectedNonce)
	}

	return tx, nil
}

func (s *Store) authorizePendingTransactionTx(ctx context.Context, tx *sql.Tx, pending pendingTx) error {
	if !requiresSignature(pending.Type) {
		return nil
	}

	transaction := protocol.Transaction{
		Hash:       pending.Hash,
		Type:       pending.Type,
		Sender:     pending.Sender,
		Nonce:      pending.Nonce,
		PublicKey:  pending.PublicKey,
		Signature:  pending.Signature,
		Payload:    pending.Payload,
		AcceptedAt: pending.AcceptedAt,
	}
	if err := txauth.VerifyTransaction(s.cfg.Genesis.ChainID, transaction); err != nil {
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	state, err := lookupAuthStateForUpdate(ctx, tx, pending.Sender)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if state.PublicKey.Valid && txauth.NormalizePublicKey(state.PublicKey.String) != pending.PublicKey {
		return fmt.Errorf("%w: public key does not match registered agent identity", ErrUnauthorized)
	}

	expectedNonce := state.NextNonce + 1
	if pending.Nonce != expectedNonce {
		return fmt.Errorf("%w: expected nonce %d", ErrInvalidNonce, expectedNonce)
	}

	if !state.PublicKey.Valid {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET public_key = $2,
			     updated_at = NOW()
			 WHERE address = $1`,
			pending.Sender,
			pending.PublicKey,
		); err != nil {
			return fmt.Errorf("bind agent public key: %w", err)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET next_nonce = $2,
		     updated_at = NOW()
		 WHERE address = $1`,
		pending.Sender,
		pending.Nonce,
	); err != nil {
		return fmt.Errorf("advance agent nonce: %w", err)
	}

	return nil
}

func (s *Store) nextEnqueueNonce(ctx context.Context, sender string, committedNonce int64) (int64, error) {
	var pendingMax sql.NullInt64
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT MAX(nonce)
		 FROM tx_pool
		 WHERE sender = $1 AND status = 'pending' AND nonce > 0`,
		sender,
	).Scan(&pendingMax); err != nil {
		return 0, fmt.Errorf("query pending nonce: %w", err)
	}

	expected := committedNonce + 1
	if pendingMax.Valid && pendingMax.Int64 >= expected {
		expected = pendingMax.Int64 + 1
	}
	return expected, nil
}

func (s *Store) pendingSenderPublicKey(ctx context.Context, sender string) (string, error) {
	var publicKey sql.NullString
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT public_key
		 FROM tx_pool
		 WHERE sender = $1 AND status = 'pending' AND nonce > 0 AND public_key IS NOT NULL
		 ORDER BY nonce DESC
		 LIMIT 1`,
		sender,
	).Scan(&publicKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("query pending sender public key: %w", err)
	}
	return txauth.NormalizePublicKey(publicKey.String), nil
}

func (s *Store) lookupAuthState(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, address string) (authState, error) {
	var state authState
	if err := querier.QueryRowContext(
		ctx,
		`SELECT public_key, next_nonce
		 FROM agents
		 WHERE address = $1`,
		address,
	).Scan(&state.PublicKey, &state.NextNonce); err != nil {
		return authState{}, fmt.Errorf("query agent auth state: %w", err)
	}
	return state, nil
}

func lookupAuthStateForUpdate(ctx context.Context, tx *sql.Tx, address string) (authState, error) {
	var state authState
	if err := tx.QueryRowContext(
		ctx,
		`SELECT public_key, next_nonce
		 FROM agents
		 WHERE address = $1
		 FOR UPDATE`,
		address,
	).Scan(&state.PublicKey, &state.NextNonce); err != nil {
		return authState{}, fmt.Errorf("lock agent auth state: %w", err)
	}
	return state, nil
}

func requiresSignature(txType protocol.TxType) bool {
	return txType != protocol.TxTypeFundAgent
}
