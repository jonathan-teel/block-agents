package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"aichain/internal/protocol"
)

func (s *Store) RecordConsensusProposal(ctx context.Context, proposal protocol.ConsensusProposal) error {
	payload, err := json.Marshal(proposal)
	if err != nil {
		return fmt.Errorf("marshal consensus proposal: %w", err)
	}
	_, err = s.db.ExecContext(
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
		return fmt.Errorf("persist consensus proposal: %w", err)
	}
	return nil
}

func (s *Store) RecordConsensusVote(ctx context.Context, vote protocol.ConsensusVote) error {
	payload, err := json.Marshal(vote)
	if err != nil {
		return fmt.Errorf("marshal consensus vote: %w", err)
	}
	_, err = s.db.ExecContext(
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
		return fmt.Errorf("persist consensus vote: %w", err)
	}
	return nil
}

func (s *Store) RecordConsensusRoundChange(ctx context.Context, message protocol.ConsensusRoundChange) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal consensus round change: %w", err)
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO consensus_round_changes (chain_id, height, round, validator, reason, requested_at, signature, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (height, round, validator) DO UPDATE
		 SET reason = EXCLUDED.reason,
		     requested_at = EXCLUDED.requested_at,
		     signature = EXCLUDED.signature,
		     payload_json = EXCLUDED.payload_json`,
		message.ChainID,
		message.Height,
		message.Round,
		message.Validator,
		message.Reason,
		message.RequestedAt,
		message.Signature,
		payload,
	)
	if err != nil {
		return fmt.Errorf("persist consensus round change: %w", err)
	}
	return nil
}

func (s *Store) RecordConsensusEvidence(ctx context.Context, evidence protocol.ConsensusEvidence) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO consensus_evidence (evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash, details, observed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash) DO NOTHING`,
		evidence.EvidenceType,
		evidence.Validator,
		evidence.Height,
		evidence.Round,
		evidence.VoteType,
		evidence.BlockHash,
		evidence.ConflictingBlockHash,
		evidence.Details,
		evidence.ObservedAt,
	)
	if err != nil {
		return fmt.Errorf("persist consensus evidence: %w", err)
	}
	return nil
}

func (s *Store) RecordQuorumCertificate(ctx context.Context, certificate protocol.QuorumCertificate) error {
	payload, err := json.Marshal(certificate)
	if err != nil {
		return fmt.Errorf("marshal quorum certificate: %w", err)
	}
	_, err = s.db.ExecContext(
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
		return fmt.Errorf("persist quorum certificate: %w", err)
	}
	if err := recordForkChoicePreference(ctx, s.db, certificate); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListQuorumCertificates(ctx context.Context, limit int) ([]protocol.QuorumCertificate, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_certificates
		 ORDER BY height DESC, round DESC, certified_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query quorum certificates: %w", err)
	}
	defer rows.Close()

	certificates := make([]protocol.QuorumCertificate, 0, limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan quorum certificate: %w", err)
		}
		var certificate protocol.QuorumCertificate
		if err := json.Unmarshal(payload, &certificate); err != nil {
			return nil, fmt.Errorf("decode quorum certificate: %w", err)
		}
		certificates = append(certificates, certificate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quorum certificates: %w", err)
	}

	return certificates, nil
}

func (s *Store) ListConsensusRoundChanges(ctx context.Context, limit int) ([]protocol.ConsensusRoundChange, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_round_changes
		 ORDER BY height DESC, round DESC, requested_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query consensus round changes: %w", err)
	}
	defer rows.Close()

	messages := make([]protocol.ConsensusRoundChange, 0, limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan consensus round change: %w", err)
		}
		var message protocol.ConsensusRoundChange
		if err := json.Unmarshal(payload, &message); err != nil {
			return nil, fmt.Errorf("decode consensus round change: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate consensus round changes: %w", err)
	}

	return messages, nil
}

func (s *Store) ListConsensusEvidence(ctx context.Context, limit int) ([]protocol.ConsensusEvidence, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash, details, observed_at, processed_at, applied_balance_penalty, applied_reputation_penalty
		 FROM consensus_evidence
		 ORDER BY height DESC, round DESC, observed_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query consensus evidence: %w", err)
	}
	defer rows.Close()

	evidence := make([]protocol.ConsensusEvidence, 0, limit)
	for rows.Next() {
		var item protocol.ConsensusEvidence
		var processedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.EvidenceType,
			&item.Validator,
			&item.Height,
			&item.Round,
			&item.VoteType,
			&item.BlockHash,
			&item.ConflictingBlockHash,
			&item.Details,
			&item.ObservedAt,
			&processedAt,
			&item.AppliedBalancePenalty,
			&item.AppliedReputationPenalty,
		); err != nil {
			return nil, fmt.Errorf("scan consensus evidence: %w", err)
		}
		if processedAt.Valid {
			value := processedAt.Time
			item.ProcessedAt = &value
		}
		evidence = append(evidence, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate consensus evidence: %w", err)
	}

	return evidence, nil
}

func (s *Store) ListCertifiedBlocksRange(ctx context.Context, from int64, limit int) ([]protocol.CertifiedBlock, error) {
	if from < 0 {
		return nil, fmt.Errorf("%w: from height must be non-negative", ErrValidation)
	}
	if limit <= 0 {
		limit = 10
	}

	bundles := make([]protocol.CertifiedBlock, 0, limit)
	for height := from; height < from+int64(limit); height++ {
		bundle, err := s.GetCertifiedBlockByHeight(ctx, height)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				break
			}
			return nil, err
		}
		bundles = append(bundles, bundle)
	}

	return bundles, nil
}

func (s *Store) GetCertifiedBlockByHeight(ctx context.Context, height int64) (protocol.CertifiedBlock, error) {
	block, err := s.GetBlockByHeight(ctx, height)
	if err != nil {
		return protocol.CertifiedBlock{}, err
	}

	var proposalRaw []byte
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_proposals
		 WHERE height = $1 AND block_hash = $2
		 ORDER BY round DESC
		 LIMIT 1`,
		height,
		block.Hash,
	).Scan(&proposalRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.CertifiedBlock{}, ErrNotFound
		}
		return protocol.CertifiedBlock{}, fmt.Errorf("query consensus proposal: %w", err)
	}

	var proposal protocol.ConsensusProposal
	if err := json.Unmarshal(proposalRaw, &proposal); err != nil {
		return protocol.CertifiedBlock{}, fmt.Errorf("decode consensus proposal: %w", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_votes
		 WHERE height = $1 AND block_hash = $2 AND vote_type = 'precommit'
		 ORDER BY voter ASC`,
		height,
		block.Hash,
	)
	if err != nil {
		return protocol.CertifiedBlock{}, fmt.Errorf("query consensus votes: %w", err)
	}
	defer rows.Close()

	votes := make([]protocol.ConsensusVote, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return protocol.CertifiedBlock{}, fmt.Errorf("scan consensus vote: %w", err)
		}
		var vote protocol.ConsensusVote
		if err := json.Unmarshal(payload, &vote); err != nil {
			return protocol.CertifiedBlock{}, fmt.Errorf("decode consensus vote: %w", err)
		}
		votes = append(votes, vote)
	}
	if err := rows.Err(); err != nil {
		return protocol.CertifiedBlock{}, fmt.Errorf("iterate consensus votes: %w", err)
	}

	var certificateRaw []byte
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_certificates
		 WHERE height = $1 AND block_hash = $2 AND vote_type = 'precommit'
		 ORDER BY certified_at DESC
		 LIMIT 1`,
		height,
		block.Hash,
	).Scan(&certificateRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.CertifiedBlock{}, ErrNotFound
		}
		return protocol.CertifiedBlock{}, fmt.Errorf("query quorum certificate: %w", err)
	}

	var certificate protocol.QuorumCertificate
	if err := json.Unmarshal(certificateRaw, &certificate); err != nil {
		return protocol.CertifiedBlock{}, fmt.Errorf("decode quorum certificate: %w", err)
	}

	return protocol.CertifiedBlock{
		Block:       block,
		Proposal:    proposal,
		Votes:       votes,
		Certificate: certificate,
	}, nil
}
