package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"aichain/internal/protocol"
)

func (s *Store) ImportCertifiedBranch(ctx context.Context, bundles []protocol.CertifiedBlock) error {
	if len(bundles) == 0 {
		return fmt.Errorf("%w: certified branch is empty", ErrValidation)
	}
	if err := validateCertifiedBranch(bundles); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin branch import transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return err
	}

	first := bundles[0].Block.Header
	switch {
	case first.Height == meta.HeadHeight+1 && first.ParentHash == meta.HeadHash:
		if err := s.importForwardBranchTx(ctx, tx, meta, bundles); err != nil {
			return err
		}
	case first.Height <= meta.HeadHeight:
		if s.cfg.ReorgPolicy != "best_certified" {
			return fmt.Errorf("%w: certified branch requires reorg policy best_certified", ErrValidation)
		}
		if err := s.reorganizeCanonicalBranchTx(ctx, tx, meta, bundles); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: certified branch does not connect to the local head", ErrValidation)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit branch import transaction: %w", err)
	}
	return nil
}

func validateCertifiedBranch(bundles []protocol.CertifiedBlock) error {
	for index := range bundles {
		header := bundles[index].Block.Header
		if header.Height <= 0 {
			return fmt.Errorf("%w: certified branch height must be > 0", ErrValidation)
		}
		if index == 0 {
			continue
		}

		parent := bundles[index-1].Block
		if header.Height != parent.Header.Height+1 {
			return fmt.Errorf("%w: certified branch heights must be contiguous", ErrValidation)
		}
		if header.ParentHash != parent.Hash {
			return fmt.Errorf("%w: certified branch parent hash mismatch at height %d", ErrValidation, header.Height)
		}
	}
	return nil
}

func (s *Store) importForwardBranchTx(ctx context.Context, tx *sql.Tx, meta chainMetadata, bundles []protocol.CertifiedBlock) error {
	current := meta
	for _, bundle := range bundles {
		if bundle.Block.Header.Height != current.HeadHeight+1 {
			return fmt.Errorf("%w: certified branch height must extend the current head", ErrValidation)
		}
		if bundle.Block.Header.ParentHash != current.HeadHash {
			return fmt.Errorf("%w: certified branch parent hash must match the current head", ErrValidation)
		}
		if err := s.replayAndValidateBlockTx(ctx, tx, current, bundle.Block); err != nil {
			return err
		}
		if err := insertBlockTx(ctx, tx, bundle.Block); err != nil {
			return err
		}
		if err := upsertCommittedTransactionsTx(ctx, tx, bundle.Block); err != nil {
			return err
		}
		if err := updateMetadataHeadTx(ctx, tx, bundle.Block.Header.Height, bundle.Block.Hash); err != nil {
			return err
		}
		if err := persistConsensusBundleTx(ctx, tx, bundle); err != nil {
			return err
		}

		current.HeadHeight = bundle.Block.Header.Height
		current.HeadHash = bundle.Block.Hash
	}
	return nil
}

func (s *Store) reorganizeCanonicalBranchTx(ctx context.Context, tx *sql.Tx, meta chainMetadata, bundles []protocol.CertifiedBlock) error {
	forkHeight := bundles[0].Block.Header.Height
	if forkHeight <= 0 {
		return fmt.Errorf("%w: cannot reorganize genesis", ErrValidation)
	}

	depth := meta.HeadHeight - forkHeight + 1
	if depth <= 0 {
		return fmt.Errorf("%w: certified branch is not a reorg candidate", ErrValidation)
	}

	parentBlock, err := loadCanonicalBlockTx(ctx, tx, forkHeight-1)
	if err != nil {
		return err
	}
	if bundles[0].Block.Header.ParentHash != parentBlock.Hash {
		return fmt.Errorf("%w: certified branch parent does not match the canonical fork base", ErrValidation)
	}

	prefixBlocks, err := loadCanonicalBlocksRangeTx(ctx, tx, 1, forkHeight-1)
	if err != nil {
		return err
	}
	orphanedTransactions, err := loadCanonicalSuffixTransactionsTx(ctx, tx, forkHeight)
	if err != nil {
		return err
	}
	if err := deleteCanonicalSuffixTx(ctx, tx, forkHeight); err != nil {
		return err
	}
	if err := resetExecutionStateTx(ctx, tx, s.cfg.Genesis, s.cfg.TreasuryAddress, s.cfg.DefaultAgentReputation); err != nil {
		return err
	}
	if err := resetHeadToGenesisTx(ctx, tx); err != nil {
		return err
	}
	if err := deleteSuffixTransactionsFromPoolTx(ctx, tx, orphanedTransactions); err != nil {
		return err
	}

	current, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return err
	}
	for _, block := range prefixBlocks {
		if err := s.replayAndValidateBlockTx(ctx, tx, current, block); err != nil {
			return fmt.Errorf("replay canonical prefix height=%d: %w", block.Header.Height, err)
		}
		if err := updateMetadataHeadTx(ctx, tx, block.Header.Height, block.Hash); err != nil {
			return err
		}
		current.HeadHeight = block.Header.Height
		current.HeadHash = block.Hash
	}

	if err := s.importForwardBranchTx(ctx, tx, current, bundles); err != nil {
		return err
	}

	replacementTxs := make(map[string]struct{})
	for _, bundle := range bundles {
		for _, transaction := range bundle.Block.Transactions {
			replacementTxs[transaction.Hash] = struct{}{}
		}
	}
	if err := restoreOrphanedTransactionsTx(ctx, tx, orphanedTransactions, replacementTxs); err != nil {
		return err
	}

	return nil
}

func loadCanonicalBlocksRangeTx(ctx context.Context, tx *sql.Tx, fromHeight int64, toHeight int64) ([]protocol.Block, error) {
	if toHeight < fromHeight || toHeight <= 0 {
		return nil, nil
	}

	blocks := make([]protocol.Block, 0, toHeight-fromHeight+1)
	for height := fromHeight; height <= toHeight; height++ {
		block, err := loadCanonicalBlockTx(ctx, tx, height)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func loadCanonicalSuffixTransactionsTx(ctx context.Context, tx *sql.Tx, fromHeight int64) ([]pendingTx, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT tx_hash, tx_type, sender, nonce, public_key, signature, payload, accepted_at
		 FROM block_transactions
		 WHERE block_height >= $1
		 ORDER BY block_height ASC, tx_index ASC`,
		fromHeight,
	)
	if err != nil {
		return nil, fmt.Errorf("query canonical suffix transactions: %w", err)
	}
	defer rows.Close()

	transactions := make([]pendingTx, 0)
	for rows.Next() {
		var (
			item      pendingTx
			txType    string
			publicKey sql.NullString
			signature sql.NullString
		)
		if err := rows.Scan(&item.Hash, &txType, &item.Sender, &item.Nonce, &publicKey, &signature, &item.Payload, &item.AcceptedAt); err != nil {
			return nil, fmt.Errorf("scan canonical suffix transaction: %w", err)
		}
		item.Type = protocol.TxType(txType)
		item.PublicKey = publicKey.String
		item.Signature = signature.String
		transactions = append(transactions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canonical suffix transactions: %w", err)
	}

	return transactions, nil
}

func deleteCanonicalSuffixTx(ctx context.Context, tx *sql.Tx, fromHeight int64) error {
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM blocks
		 WHERE height >= $1`,
		fromHeight,
	); err != nil {
		return fmt.Errorf("delete canonical suffix: %w", err)
	}
	return nil
}

func resetExecutionStateTx(ctx context.Context, tx *sql.Tx, genesis protocol.Genesis, treasuryAddress string, defaultReputation float64) error {
	if _, err := tx.ExecContext(
		ctx,
		`TRUNCATE TABLE
		     governance_votes,
		     governance_proposals,
		     governance_parameters,
		     oracle_reports,
		     proof_artifacts,
		     task_votes,
		     task_rebuttals,
		     task_evaluations,
		     task_proposals,
		     task_roles,
		     task_debate_state,
		     task_disputes,
		     task_results,
		     submissions,
		     tasks,
		     agent_key_rotations
		 RESTART IDENTITY CASCADE`,
	); err != nil {
		return fmt.Errorf("truncate execution state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents`); err != nil {
		return fmt.Errorf("reset agents: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM validator_registry`); err != nil {
		return fmt.Errorf("reset validator registry: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE consensus_evidence
		 SET processed_at = NULL,
		     applied_balance_penalty = 0,
		     applied_reputation_penalty = 0`,
	); err != nil {
		return fmt.Errorf("reset consensus evidence processing state: %w", err)
	}

	for _, account := range genesis.Accounts {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO agents (address, public_key, next_nonce, balance, reputation, created_at, updated_at)
			 VALUES ($1, $2, 0, $3, $4, $5, $5)`,
			account.Address,
			nullIfEmpty(account.PublicKey),
			account.Balance,
			account.Reputation,
			genesis.GenesisTime,
		); err != nil {
			return fmt.Errorf("restore genesis account %s: %w", account.Address, err)
		}
	}
	if err := ensureAgentExistsTx(ctx, tx, treasuryAddress, defaultReputation); err != nil {
		return fmt.Errorf("restore treasury account: %w", err)
	}
	for _, validator := range genesis.Validators {
		if err := ensureAgentExistsTx(ctx, tx, validator.Address, defaultReputation); err != nil {
			return fmt.Errorf("restore validator account %s: %w", validator.Address, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO validator_registry (address, public_key, power, active, created_at, updated_at)
			 VALUES ($1, $2, $3, TRUE, $4, $4)`,
			validator.Address,
			validator.PublicKey,
			validator.Power,
			genesis.GenesisTime,
		); err != nil {
			return fmt.Errorf("restore genesis validator %s: %w", validator.Address, err)
		}
	}

	return nil
}

func resetHeadToGenesisTx(ctx context.Context, tx *sql.Tx) error {
	genesisBlock, err := loadCanonicalBlockTx(ctx, tx, 0)
	if err != nil {
		return err
	}
	return updateMetadataHeadTx(ctx, tx, genesisBlock.Header.Height, genesisBlock.Hash)
}

func deleteSuffixTransactionsFromPoolTx(ctx context.Context, tx *sql.Tx, transactions []pendingTx) error {
	for _, item := range transactions {
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM tx_pool
			 WHERE tx_hash = $1`,
			item.Hash,
		); err != nil {
			return fmt.Errorf("delete orphaned transaction %s from tx_pool: %w", item.Hash, err)
		}
	}
	return nil
}

func restoreOrphanedTransactionsTx(ctx context.Context, tx *sql.Tx, transactions []pendingTx, replacements map[string]struct{}) error {
	for _, item := range transactions {
		if _, replaced := replacements[item.Hash]; replaced {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO tx_pool (tx_hash, tx_type, sender, nonce, public_key, signature, payload, status, accepted_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8)
			 ON CONFLICT (tx_hash) DO NOTHING`,
			item.Hash,
			string(item.Type),
			item.Sender,
			item.Nonce,
			nullIfEmpty(item.PublicKey),
			nullIfEmpty(item.Signature),
			item.Payload,
			item.AcceptedAt,
		); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return fmt.Errorf("restore orphaned transaction %s: %w", item.Hash, err)
		}
	}
	return nil
}

func loadCanonicalBlockTx(ctx context.Context, tx *sql.Tx, height int64) (protocol.Block, error) {
	var (
		hash      string
		headerRaw []byte
		eventsRaw []byte
	)

	if err := tx.QueryRowContext(
		ctx,
		`SELECT hash, header_json, events_json
		 FROM blocks
		 WHERE height = $1`,
		height,
	).Scan(&hash, &headerRaw, &eventsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Block{}, ErrNotFound
		}
		return protocol.Block{}, fmt.Errorf("query canonical block: %w", err)
	}

	var header protocol.BlockHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return protocol.Block{}, fmt.Errorf("decode canonical block header: %w", err)
	}

	var events []protocol.Event
	if err := json.Unmarshal(eventsRaw, &events); err != nil {
		return protocol.Block{}, fmt.Errorf("decode canonical block events: %w", err)
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT tx_hash, tx_type, sender, nonce, public_key, signature, payload, accepted_at, receipt_json
		 FROM block_transactions
		 WHERE block_height = $1
		 ORDER BY tx_index ASC`,
		height,
	)
	if err != nil {
		return protocol.Block{}, fmt.Errorf("query canonical block transactions: %w", err)
	}
	defer rows.Close()

	block := protocol.Block{
		Hash:   hash,
		Header: header,
		Events: events,
	}
	for rows.Next() {
		var (
			txHash     string
			txType     string
			sender     string
			nonce      int64
			publicKey  sql.NullString
			signature  sql.NullString
			payload    []byte
			acceptedAt sql.NullTime
			receiptRaw []byte
		)
		if err := rows.Scan(&txHash, &txType, &sender, &nonce, &publicKey, &signature, &payload, &acceptedAt, &receiptRaw); err != nil {
			return protocol.Block{}, fmt.Errorf("scan canonical block transaction: %w", err)
		}

		transaction := protocol.Transaction{
			Hash:      txHash,
			Type:      protocol.TxType(txType),
			Sender:    sender,
			Nonce:     nonce,
			PublicKey: publicKey.String,
			Signature: signature.String,
			Payload:   payload,
		}
		if acceptedAt.Valid {
			transaction.AcceptedAt = acceptedAt.Time
		}
		block.Transactions = append(block.Transactions, transaction)

		var receipt protocol.Receipt
		if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
			return protocol.Block{}, fmt.Errorf("decode canonical block receipt: %w", err)
		}
		block.Receipts = append(block.Receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return protocol.Block{}, fmt.Errorf("iterate canonical block transactions: %w", err)
	}

	return block, nil
}
