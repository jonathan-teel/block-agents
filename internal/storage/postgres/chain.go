package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"aichain/internal/protocol"
)

func (s *Store) initChain(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin chain init transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadata(ctx, tx)
	switch {
	case err == nil:
		if meta.ChainID != s.cfg.Genesis.ChainID {
			return fmt.Errorf("configured chain_id %s does not match stored chain_id %s", s.cfg.Genesis.ChainID, meta.ChainID)
		}
		return tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	for _, account := range s.cfg.Genesis.Accounts {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO agents (address, public_key, next_nonce, balance, reputation, created_at, updated_at)
			 VALUES ($1, $2, 0, $3, $4, $5, $5)`,
			account.Address,
			nullIfEmpty(account.PublicKey),
			account.Balance,
			account.Reputation,
			s.cfg.Genesis.GenesisTime,
		); err != nil {
			return fmt.Errorf("insert genesis account %s: %w", account.Address, err)
		}
	}
	if err := ensureTreasuryAccountTx(ctx, tx, s.cfg); err != nil {
		return fmt.Errorf("ensure treasury account: %w", err)
	}
	for _, validator := range s.cfg.Genesis.Validators {
		if err := ensureAgentExistsTx(ctx, tx, validator.Address, s.cfg.DefaultAgentReputation); err != nil {
			return fmt.Errorf("ensure genesis validator account %s: %w", validator.Address, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO validator_registry (address, public_key, power, active, created_at, updated_at)
			 VALUES ($1, $2, $3, TRUE, $4, $4)
			 ON CONFLICT (address) DO NOTHING`,
			validator.Address,
			validator.PublicKey,
			validator.Power,
			s.cfg.Genesis.GenesisTime,
		); err != nil {
			return fmt.Errorf("insert genesis validator %s: %w", validator.Address, err)
		}
	}

	stateRoot, err := computeStateRootTx(ctx, tx)
	if err != nil {
		return err
	}

	header := protocol.BlockHeader{
		ChainID:    s.cfg.Genesis.ChainID,
		Height:     0,
		ParentHash: protocol.ZeroHash,
		Timestamp:  s.cfg.Genesis.GenesisTime,
		Proposer:   "genesis",
		TxRoot:     protocol.ComputeMerkleRoot(nil),
		StateRoot:  stateRoot,
		TxCount:    0,
	}
	header.AppHash = protocol.BuildAppHash(header)

	block := protocol.Block{
		Hash:   protocol.BuildBlockHash(header),
		Header: header,
		Events: []protocol.Event{
			{
				Type: "genesis.initialized",
				Attributes: map[string]string{
					"accounts": strconv.Itoa(len(s.cfg.Genesis.Accounts)),
				},
			},
		},
	}

	if err := protocol.ValidateBlock(nil, block); err != nil {
		return fmt.Errorf("validate genesis block: %w", err)
	}
	if err := insertBlockTx(ctx, tx, block); err != nil {
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO chain_metadata (singleton, chain_id, node_id, head_height, head_hash, genesis_hash)
		 VALUES (TRUE, $1, $2, $3, $4, $5)`,
		s.cfg.Genesis.ChainID,
		s.cfg.NodeID,
		block.Header.Height,
		block.Hash,
		block.Hash,
	); err != nil {
		return fmt.Errorf("insert chain metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit chain init transaction: %w", err)
	}

	return nil
}

func (s *Store) syncSystemAccounts(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin system account sync transaction: %w", err)
	}
	defer tx.Rollback()

	if err := ensureTreasuryAccountTx(ctx, tx, s.cfg); err != nil {
		return fmt.Errorf("ensure treasury account: %w", err)
	}
	for _, validator := range s.cfg.Genesis.Validators {
		if err := ensureAgentExistsTx(ctx, tx, validator.Address, s.cfg.DefaultAgentReputation); err != nil {
			return fmt.Errorf("ensure validator account %s: %w", validator.Address, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit system account sync transaction: %w", err)
	}
	return nil
}

func (s *Store) GetChainInfo(ctx context.Context) (protocol.ChainInfo, error) {
	meta, err := getMetadata(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.ChainInfo{}, ErrNotFound
		}
		return protocol.ChainInfo{}, err
	}

	return protocol.ChainInfo{
		ChainID:     meta.ChainID,
		NodeID:      meta.NodeID,
		HeadHeight:  meta.HeadHeight,
		HeadHash:    meta.HeadHash,
		GenesisHash: meta.GenesisHash,
		SchemaVersion: s.schemaVersion,
		RoleSelectionPolicy: effectiveRoleSelectionPolicyTx(ctx, s.db, s.cfg),
		MinerVotePolicy: effectiveMinerVotePolicyTx(ctx, s.db, s.cfg),
		ReorgPolicy: s.cfg.ReorgPolicy,
	}, nil
}

func (s *Store) GetHeadBlock(ctx context.Context) (protocol.Block, error) {
	meta, err := getMetadata(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Block{}, ErrNotFound
		}
		return protocol.Block{}, err
	}

	return s.GetBlockByHeight(ctx, meta.HeadHeight)
}

func (s *Store) GetBlockByHeight(ctx context.Context, height int64) (protocol.Block, error) {
	var (
		hash      string
		headerRaw []byte
		eventsRaw []byte
	)

	if err := s.db.QueryRowContext(
		ctx,
		`SELECT hash, header_json, events_json
		 FROM blocks
		 WHERE height = $1`,
		height,
	).Scan(&hash, &headerRaw, &eventsRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Block{}, ErrNotFound
		}
		return protocol.Block{}, fmt.Errorf("query block: %w", err)
	}

	var header protocol.BlockHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return protocol.Block{}, fmt.Errorf("decode block header: %w", err)
	}

	var events []protocol.Event
	if err := json.Unmarshal(eventsRaw, &events); err != nil {
		return protocol.Block{}, fmt.Errorf("decode block events: %w", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT tx_hash, tx_type, sender, nonce, public_key, signature, payload, accepted_at, receipt_json
		 FROM block_transactions
		 WHERE block_height = $1
		 ORDER BY tx_index ASC`,
		height,
	)
	if err != nil {
		return protocol.Block{}, fmt.Errorf("query block transactions: %w", err)
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
			return protocol.Block{}, fmt.Errorf("scan block transaction: %w", err)
		}

		transaction := protocol.Transaction{
			Hash:      txHash,
			Type:      protocol.TxType(txType),
			Sender:    sender,
			Nonce:     nonce,
			Payload:   payload,
			PublicKey: publicKey.String,
			Signature: signature.String,
		}
		if acceptedAt.Valid {
			transaction.AcceptedAt = acceptedAt.Time
		}
		block.Transactions = append(block.Transactions, transaction)

		var receipt protocol.Receipt
		if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
			return protocol.Block{}, fmt.Errorf("decode receipt: %w", err)
		}
		block.Receipts = append(block.Receipts, receipt)
	}

	if err := rows.Err(); err != nil {
		return protocol.Block{}, fmt.Errorf("iterate block transactions: %w", err)
	}

	return block, nil
}

func (s *Store) GetTransactionStatus(ctx context.Context, txHash string) (protocol.TransactionStatus, error) {
	var (
		txType      string
		sender      string
		nonce       int64
		publicKey   sql.NullString
		signature   sql.NullString
		payload     []byte
		status      string
		errorCode   sql.NullString
		errorText   sql.NullString
		blockHeight sql.NullInt64
		acceptedAt  time.Time
		receiptRaw  sql.NullString
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT p.tx_type, p.sender, p.nonce, p.public_key, p.signature, p.payload, p.status, p.error_code, p.error, p.block_height, p.accepted_at, bt.receipt_json
		 FROM tx_pool p
		 LEFT JOIN block_transactions bt ON bt.tx_hash = p.tx_hash
		 WHERE p.tx_hash = $1`,
		txHash,
	).Scan(&txType, &sender, &nonce, &publicKey, &signature, &payload, &status, &errorCode, &errorText, &blockHeight, &acceptedAt, &receiptRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.TransactionStatus{}, ErrNotFound
		}
		return protocol.TransactionStatus{}, fmt.Errorf("query transaction status: %w", err)
	}

	result := protocol.TransactionStatus{
		Transaction: protocol.Transaction{
			Hash:       txHash,
			Type:       protocol.TxType(txType),
			Sender:     sender,
			Nonce:      nonce,
			PublicKey:  publicKey.String,
			Signature:  signature.String,
			Payload:    payload,
			AcceptedAt: acceptedAt,
		},
		Status: status,
	}

	if errorText.Valid {
		result.Error = errorText.String
	}
	if errorCode.Valid {
		result.ErrorCode = errorCode.String
	}
	if blockHeight.Valid {
		height := blockHeight.Int64
		result.BlockHeight = &height
	}
	if receiptRaw.Valid {
		var receipt protocol.Receipt
		if err := json.Unmarshal([]byte(receiptRaw.String), &receipt); err != nil {
			return protocol.TransactionStatus{}, fmt.Errorf("decode receipt: %w", err)
		}
		result.Receipt = &receipt
	}

	return result, nil
}

func (s *Store) SealPendingBlock(ctx context.Context, opts SealOptions) (*protocol.Block, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin seal transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	block, created, err := buildCandidateBlockTx(ctx, tx, meta, opts, s)
	if err != nil {
		return nil, false, err
	}
	if !created {
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit no-op seal transaction: %w", err)
		}
		return nil, false, nil
	}

	if err := insertBlockTx(ctx, tx, *block); err != nil {
		return nil, false, err
	}
	if err := markTransactionsCommittedTx(ctx, tx, *block); err != nil {
		return nil, false, err
	}
	if err := updateMetadataHeadTx(ctx, tx, block.Header.Height, block.Hash); err != nil {
		return nil, false, err
	}

	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit seal transaction: %w", err)
	}

	return block, true, nil
}

func (s *Store) BuildCandidateBlock(ctx context.Context, opts SealOptions) (*protocol.Block, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin candidate build transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	block, created, err := buildCandidateBlockTx(ctx, tx, meta, opts, s)
	if err != nil {
		return nil, false, err
	}
	if !created {
		return nil, false, nil
	}

	return block, true, nil
}

func buildCandidateBlockTx(ctx context.Context, tx *sql.Tx, meta chainMetadata, opts SealOptions, store *Store) (*protocol.Block, bool, error) {
	pending, err := listPendingTransactionsTx(ctx, tx, opts.MaxTransactions)
	if err != nil {
		return nil, false, err
	}

	debateEvents, err := advanceDebateStagesTx(ctx, tx, opts.Now.Unix())
	if err != nil {
		return nil, false, err
	}

	receipts := make([]protocol.Receipt, 0, len(pending))
	transactions := make([]protocol.Transaction, 0, len(pending))
	for _, item := range pending {
		transactions = append(transactions, protocol.Transaction{
			Hash:       item.Hash,
			Type:       item.Type,
			Sender:     item.Sender,
			Nonce:      item.Nonce,
			PublicKey:  item.PublicKey,
			Signature:  item.Signature,
			Sequence:   item.Sequence,
			Payload:    item.Payload,
			AcceptedAt: item.AcceptedAt,
		})

		receipt := protocol.Receipt{
			TxHash:  item.Hash,
			Success: false,
		}

		events, execErr := store.executePendingTransactionTx(ctx, tx, item, opts.Now.Unix())
		if execErr == nil {
			receipt.Success = true
			receipt.Events = events
		} else {
			receipt.ErrorCode = classifyErrorCode(execErr)
			receipt.Error = execErr.Error()
		}
		receipts = append(receipts, receipt)
	}

	consensusEvents, err := updateConsensusTx(ctx, tx, opts.MaxEffectiveWeight)
	if err != nil {
		return nil, false, err
	}
	governanceEvents, err := finalizeGovernanceProposalsTx(ctx, tx, store.cfg, opts.Now.Unix())
	if err != nil {
		return nil, false, err
	}
	settlementEvents, err := store.settleExpiredTasksTx(ctx, tx, opts.Now.Unix())
	if err != nil {
		return nil, false, err
	}
	slashingEvents, err := applyConsensusEvidencePenaltiesTx(ctx, tx, store.cfg, opts.Now.Unix(), store.cfg.ValidatorSlashFraction, store.cfg.ValidatorSlashReputationPenalty)
	if err != nil {
		return nil, false, err
	}

	events := append(consensusEvents, debateEvents...)
	events = append(events, governanceEvents...)
	events = append(events, settlementEvents...)
	events = append(events, slashingEvents...)
	if len(transactions) == 0 && len(events) == 0 && !opts.CreateEmptyBlocks {
		return nil, false, nil
	}

	stateRoot, err := computeStateRootTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}

	txHashes := make([]string, 0, len(transactions))
	for _, transaction := range transactions {
		txHashes = append(txHashes, transaction.Hash)
	}

	header := protocol.BlockHeader{
		ChainID:    meta.ChainID,
		Height:     meta.HeadHeight + 1,
		ParentHash: meta.HeadHash,
		Timestamp:  opts.Now.UTC().Truncate(time.Second),
		Proposer:   opts.Proposer,
		TxRoot:     protocol.ComputeMerkleRoot(txHashes),
		StateRoot:  stateRoot,
		TxCount:    len(transactions),
	}
	header.AppHash = protocol.BuildAppHash(header)

	block := protocol.Block{
		Hash:         protocol.BuildBlockHash(header),
		Header:       header,
		Transactions: transactions,
		Receipts:     receipts,
		Events:       events,
	}

	parent := &protocol.Block{
		Hash: meta.HeadHash,
		Header: protocol.BlockHeader{
			ChainID: meta.ChainID,
			Height:  meta.HeadHeight,
		},
	}
	if err := protocol.ValidateBlock(parent, block); err != nil {
		return nil, false, fmt.Errorf("validate sealed block: %w", err)
	}

	for index := range block.Receipts {
		block.Receipts[index].BlockHeight = block.Header.Height
	}

	return &block, true, nil
}

func listPendingTransactionsTx(ctx context.Context, tx *sql.Tx, limit int) ([]pendingTx, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT sequence, tx_hash, tx_type, sender, nonce, public_key, signature, payload, accepted_at
		 FROM tx_pool
		 WHERE status = 'pending'
		 ORDER BY sequence ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending transactions: %w", err)
	}
	defer rows.Close()

	transactions := make([]pendingTx, 0)
	for rows.Next() {
		var item pendingTx
		var txType string
		var publicKey sql.NullString
		var signature sql.NullString
		if err := rows.Scan(&item.Sequence, &item.Hash, &txType, &item.Sender, &item.Nonce, &publicKey, &signature, &item.Payload, &item.AcceptedAt); err != nil {
			return nil, fmt.Errorf("scan pending transaction: %w", err)
		}
		item.Type = protocol.TxType(txType)
		item.PublicKey = publicKey.String
		item.Signature = signature.String
		transactions = append(transactions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending transactions: %w", err)
	}

	return transactions, nil
}

func insertBlockTx(ctx context.Context, tx *sql.Tx, block protocol.Block) error {
	headerJSON, err := json.Marshal(block.Header)
	if err != nil {
		return fmt.Errorf("marshal block header: %w", err)
	}
	eventsJSON, err := json.Marshal(block.Events)
	if err != nil {
		return fmt.Errorf("marshal block events: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO blocks (height, hash, parent_hash, chain_id, proposer, tx_root, state_root, app_hash, tx_count, sealed_at, header_json, events_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		block.Header.Height,
		block.Hash,
		block.Header.ParentHash,
		block.Header.ChainID,
		block.Header.Proposer,
		block.Header.TxRoot,
		block.Header.StateRoot,
		block.Header.AppHash,
		block.Header.TxCount,
		block.Header.Timestamp,
		headerJSON,
		eventsJSON,
	); err != nil {
		return fmt.Errorf("insert block: %w", err)
	}

	for index, transaction := range block.Transactions {
		receiptJSON, err := json.Marshal(block.Receipts[index])
		if err != nil {
			return fmt.Errorf("marshal receipt: %w", err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO block_transactions (block_height, tx_index, tx_hash, tx_type, sender, nonce, public_key, signature, payload, success, error_code, error, receipt_json, accepted_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			block.Header.Height,
			index,
			transaction.Hash,
			string(transaction.Type),
			transaction.Sender,
			transaction.Nonce,
			nullIfEmpty(transaction.PublicKey),
			nullIfEmpty(transaction.Signature),
			transaction.Payload,
			block.Receipts[index].Success,
			nullIfEmpty(block.Receipts[index].ErrorCode),
			nullIfEmpty(block.Receipts[index].Error),
			receiptJSON,
			transaction.AcceptedAt,
		); err != nil {
			return fmt.Errorf("insert block transaction: %w", err)
		}
	}

	return nil
}

func markTransactionsCommittedTx(ctx context.Context, tx *sql.Tx, block protocol.Block) error {
	for index, transaction := range block.Transactions {
		receipt := block.Receipts[index]
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE tx_pool
			 SET status = 'committed',
			     block_height = $2,
			     error_code = $3,
			     error = $4
			 WHERE tx_hash = $1`,
			transaction.Hash,
			block.Header.Height,
			nullIfEmpty(receipt.ErrorCode),
			nullIfEmpty(receipt.Error),
		); err != nil {
			return fmt.Errorf("mark transaction committed: %w", err)
		}
	}
	return nil
}

func updateMetadataHeadTx(ctx context.Context, tx *sql.Tx, height int64, hash string) error {
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE chain_metadata
		 SET head_height = $1,
		     head_hash = $2,
		     updated_at = NOW()
		 WHERE singleton = TRUE`,
		height,
		hash,
	); err != nil {
		return fmt.Errorf("update chain head: %w", err)
	}
	return nil
}

func getMetadata(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (chainMetadata, error) {
	var meta chainMetadata
	if err := querier.QueryRowContext(
		ctx,
		`SELECT chain_id, node_id, head_height, head_hash, genesis_hash
		 FROM chain_metadata
		 WHERE singleton = TRUE`,
	).Scan(&meta.ChainID, &meta.NodeID, &meta.HeadHeight, &meta.HeadHash, &meta.GenesisHash); err != nil {
		return chainMetadata{}, fmt.Errorf("query chain metadata: %w", err)
	}
	return meta, nil
}

func getMetadataForUpdate(ctx context.Context, tx *sql.Tx) (chainMetadata, error) {
	var meta chainMetadata
	if err := tx.QueryRowContext(
		ctx,
		`SELECT chain_id, node_id, head_height, head_hash, genesis_hash
		 FROM chain_metadata
		 WHERE singleton = TRUE
		 FOR UPDATE`,
	).Scan(&meta.ChainID, &meta.NodeID, &meta.HeadHeight, &meta.HeadHash, &meta.GenesisHash); err != nil {
		return chainMetadata{}, fmt.Errorf("lock chain metadata: %w", err)
	}
	return meta, nil
}
