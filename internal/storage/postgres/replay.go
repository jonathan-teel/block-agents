package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"aichain/internal/protocol"
)

func (s *Store) ValidateCandidateBlock(ctx context.Context, block protocol.Block) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin candidate validation transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return err
	}

	return s.replayAndValidateBlockTx(ctx, tx, meta, block)
}

func (s *Store) replayAndValidateBlockTx(ctx context.Context, tx *sql.Tx, meta chainMetadata, block protocol.Block) error {
	if block.Header.Height <= meta.HeadHeight {
		return fmt.Errorf("%w: candidate block height must extend the local head", ErrValidation)
	}
	if block.Header.Height != meta.HeadHeight+1 {
		return fmt.Errorf("%w: candidate block height must extend the local head", ErrValidation)
	}

	parent := &protocol.Block{
		Hash: meta.HeadHash,
		Header: protocol.BlockHeader{
			ChainID: meta.ChainID,
			Height:  meta.HeadHeight,
		},
	}
	if err := protocol.ValidateBlock(parent, block); err != nil {
		return fmt.Errorf("validate replay block: %w", err)
	}

	nowUnix := block.Header.Timestamp.Unix()
	debateEvents, err := advanceDebateStagesTx(ctx, tx, nowUnix)
	if err != nil {
		return err
	}

	computedReceipts := make([]protocol.Receipt, 0, len(block.Transactions))
	for index, transaction := range block.Transactions {
		pending := pendingTx{
			Sequence:   int64(index + 1),
			Hash:       transaction.Hash,
			Type:       transaction.Type,
			Sender:     transaction.Sender,
			Nonce:      transaction.Nonce,
			PublicKey:  transaction.PublicKey,
			Signature:  transaction.Signature,
			Payload:    transaction.Payload,
			AcceptedAt: transaction.AcceptedAt,
		}

		receipt := protocol.Receipt{
			TxHash:  transaction.Hash,
			Success: false,
		}
		events, execErr := s.executePendingTransactionTx(ctx, tx, pending, nowUnix)
		if execErr == nil {
			receipt.Success = true
			receipt.Events = events
		} else {
			receipt.ErrorCode = classifyErrorCode(execErr)
			receipt.Error = execErr.Error()
		}
		computedReceipts = append(computedReceipts, receipt)
	}

	consensusEvents, err := updateConsensusTx(ctx, tx, s.cfg.MaxEffectiveWeight)
	if err != nil {
		return err
	}
	settlementEvents, err := s.settleExpiredTasksTx(ctx, tx, nowUnix)
	if err != nil {
		return err
	}
	slashingEvents, err := applyConsensusEvidencePenaltiesTx(ctx, tx, nowUnix, s.cfg.ValidatorSlashFraction, s.cfg.ValidatorSlashReputationPenalty)
	if err != nil {
		return err
	}

	computedEvents := append(consensusEvents, debateEvents...)
	computedEvents = append(computedEvents, settlementEvents...)
	computedEvents = append(computedEvents, slashingEvents...)

	stateRoot, err := computeStateRootTx(ctx, tx)
	if err != nil {
		return err
	}
	txHashes := make([]string, 0, len(block.Transactions))
	for _, transaction := range block.Transactions {
		txHashes = append(txHashes, transaction.Hash)
	}
	computedHeader := block.Header
	computedHeader.TxRoot = protocol.ComputeMerkleRoot(txHashes)
	computedHeader.StateRoot = stateRoot
	computedHeader.AppHash = protocol.BuildAppHash(computedHeader)
	computedHash := protocol.BuildBlockHash(computedHeader)

	if computedHash != block.Hash {
		return fmt.Errorf("%w: candidate block replay hash mismatch", ErrValidation)
	}
	if computedHeader.StateRoot != block.Header.StateRoot || computedHeader.AppHash != block.Header.AppHash || computedHeader.TxRoot != block.Header.TxRoot {
		return fmt.Errorf("%w: candidate block header commitments do not match replayed state", ErrValidation)
	}
	if !receiptsEqual(computedReceipts, block.Receipts) {
		return fmt.Errorf("%w: candidate block receipts do not match replayed execution", ErrValidation)
	}
	if !reflect.DeepEqual(computedEvents, block.Events) {
		return fmt.Errorf("%w: candidate block events do not match replayed execution", ErrValidation)
	}

	return nil
}
