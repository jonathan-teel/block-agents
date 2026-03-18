package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"aichain/internal/protocol"
)

func (s *Store) ImportCertifiedBlock(ctx context.Context, bundle protocol.CertifiedBlock) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return err
	}
	if bundle.Block.Header.Height <= meta.HeadHeight {
		return nil
	}
	if err := s.replayAndValidateBlockTx(ctx, tx, meta, bundle.Block); err != nil {
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import transaction: %w", err)
	}
	return nil
}

func upsertCommittedTransactionsTx(ctx context.Context, tx *sql.Tx, block protocol.Block) error {
	for index, transaction := range block.Transactions {
		receiptRaw, err := json.Marshal(block.Receipts[index])
		if err != nil {
			return fmt.Errorf("marshal imported receipt: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO tx_pool (tx_hash, tx_type, sender, nonce, public_key, signature, payload, status, error, block_height, accepted_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'committed', $8, $9, $10)
			 ON CONFLICT (tx_hash) DO UPDATE
			 SET status = 'committed',
			     error = EXCLUDED.error,
			     block_height = EXCLUDED.block_height,
			     accepted_at = EXCLUDED.accepted_at`,
			transaction.Hash,
			string(transaction.Type),
			transaction.Sender,
			transaction.Nonce,
			nullIfEmpty(transaction.PublicKey),
			nullIfEmpty(transaction.Signature),
			transaction.Payload,
			nullIfEmpty(block.Receipts[index].Error),
			block.Header.Height,
			transaction.AcceptedAt,
		); err != nil {
			return fmt.Errorf("upsert committed transaction: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE block_transactions
			 SET receipt_json = $3
			 WHERE block_height = $1 AND tx_hash = $2`,
			block.Header.Height,
			transaction.Hash,
			receiptRaw,
		); err != nil {
			return fmt.Errorf("update imported receipt: %w", err)
		}
	}
	return nil
}

func persistConsensusBundleTx(ctx context.Context, tx *sql.Tx, bundle protocol.CertifiedBlock) error {
	if err := persistConsensusProposalTx(ctx, tx, bundle.Proposal); err != nil {
		return err
	}
	for _, vote := range bundle.Votes {
		if err := persistConsensusVoteTx(ctx, tx, vote); err != nil {
			return err
		}
	}
	if err := persistQuorumCertificateTx(ctx, tx, bundle.Certificate); err != nil {
		return err
	}
	return nil
}

func persistConsensusProposalTx(ctx context.Context, tx *sql.Tx, proposal protocol.ConsensusProposal) error {
	payload, err := json.Marshal(proposal)
	if err != nil {
		return fmt.Errorf("marshal proposal payload: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO consensus_proposals (block_hash, chain_id, height, round, proposer, block_height, parent_hash, proposed_at, signature, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (block_hash) DO UPDATE
		 SET signature = EXCLUDED.signature,
		     payload_json = EXCLUDED.payload_json`,
		proposal.BlockHash,
		proposal.ChainID,
		proposal.Height,
		proposal.Round,
		proposal.Proposer,
		proposal.BlockHeight,
		proposal.ParentHash,
		proposal.ProposedAt,
		proposal.Signature,
		payload,
	)
	if err != nil {
		return fmt.Errorf("persist proposal bundle: %w", err)
	}
	return nil
}

func persistConsensusVoteTx(ctx context.Context, tx *sql.Tx, vote protocol.ConsensusVote) error {
	payload, err := json.Marshal(vote)
	if err != nil {
		return fmt.Errorf("marshal vote payload: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO consensus_votes (chain_id, height, round, vote_type, voter, block_hash, voted_at, signature, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (height, round, vote_type, voter, block_hash) DO UPDATE
		 SET voted_at = EXCLUDED.voted_at,
		     signature = EXCLUDED.signature,
		     payload_json = EXCLUDED.payload_json`,
		vote.ChainID,
		vote.Height,
		vote.Round,
		vote.Type,
		vote.Voter,
		vote.BlockHash,
		vote.VotedAt,
		vote.Signature,
		payload,
	)
	if err != nil {
		return fmt.Errorf("persist vote bundle: %w", err)
	}
	return nil
}

func persistQuorumCertificateTx(ctx context.Context, tx *sql.Tx, certificate protocol.QuorumCertificate) error {
	payload, err := json.Marshal(certificate)
	if err != nil {
		return fmt.Errorf("marshal certificate payload: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO consensus_certificates (block_hash, chain_id, height, round, vote_type, power, threshold_power, certified_at, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (block_hash, vote_type) DO UPDATE
		 SET power = EXCLUDED.power,
		     threshold_power = EXCLUDED.threshold_power,
		     certified_at = EXCLUDED.certified_at,
		     payload_json = EXCLUDED.payload_json`,
		certificate.BlockHash,
		certificate.ChainID,
		certificate.Height,
		certificate.Round,
		certificate.VoteType,
		certificate.Power,
		certificate.Threshold,
		certificate.CertifiedAt,
		payload,
	)
	if err != nil {
		return fmt.Errorf("persist certificate bundle: %w", err)
	}
	return nil
}

func receiptsEqual(left []protocol.Receipt, right []protocol.Receipt) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].TxHash != right[index].TxHash ||
			left[index].Success != right[index].Success ||
			left[index].Error != right[index].Error ||
			!reflect.DeepEqual(left[index].Events, right[index].Events) {
			return false
		}
	}
	return true
}
