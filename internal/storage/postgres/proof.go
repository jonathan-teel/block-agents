package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"aichain/internal/proof"
	"aichain/internal/protocol"
)

func verifyProofReferencesTx(ctx context.Context, tx *sql.Tx, taskID string, references []proof.Reference) ([]proof.Reference, error) {
	verified := make([]proof.Reference, 0, len(references))
	for _, reference := range references {
		digest, err := referencedObjectDigestTx(ctx, tx, taskID, reference.Type, reference.ID)
		if err != nil {
			return nil, err
		}
		if digest == "" {
			return nil, fmt.Errorf("%w: referenced %s %d is missing", ErrValidation, reference.Type, reference.ID)
		}
		reference.Digest = digest
		verified = append(verified, reference)
	}
	return verified, nil
}

func referencedObjectDigestTx(ctx context.Context, tx *sql.Tx, taskID string, referenceType string, referenceID int64) (string, error) {
	switch referenceType {
	case "proposal":
		var content string
		if err := tx.QueryRowContext(
			ctx,
			`SELECT content
			 FROM task_proposals
			 WHERE id = $1 AND task_id = $2`,
			referenceID,
			taskID,
		).Scan(&content); err != nil {
			if err == sql.ErrNoRows {
				return "", nil
			}
			return "", fmt.Errorf("query referenced proposal: %w", err)
		}
		return protocol.HashBytes([]byte(content)), nil
	case "evaluation":
		var (
			evaluator          string
			factualConsistency float64
			redundancyScore    float64
			causalRelevance    float64
			overallScore       float64
			comments           string
		)
		if err := tx.QueryRowContext(
			ctx,
			`SELECT evaluator, factual_consistency, redundancy_score, causal_relevance, overall_score, comments
			 FROM task_evaluations
			 WHERE id = $1 AND task_id = $2`,
			referenceID,
			taskID,
		).Scan(&evaluator, &factualConsistency, &redundancyScore, &causalRelevance, &overallScore, &comments); err != nil {
			if err == sql.ErrNoRows {
				return "", nil
			}
			return "", fmt.Errorf("query referenced evaluation: %w", err)
		}
		return protocol.HashStrings([]string{
			evaluator,
			formatFloat(factualConsistency),
			formatFloat(redundancyScore),
			formatFloat(causalRelevance),
			formatFloat(overallScore),
			comments,
		}), nil
	case "vote":
		var vote protocol.ProposalVote
		if err := tx.QueryRowContext(
			ctx,
			`SELECT id, task_id, proposal_id, voter, round, reason, created_at
			 FROM task_votes
			 WHERE id = $1 AND task_id = $2`,
			referenceID,
			taskID,
		).Scan(&vote.ID, &vote.TaskID, &vote.ProposalID, &vote.Voter, &vote.Round, &vote.Reason, &vote.CreatedAt); err != nil {
			if err == sql.ErrNoRows {
				return "", nil
			}
			return "", fmt.Errorf("query referenced vote: %w", err)
		}
		payload, _ := json.Marshal(vote)
		return protocol.HashBytes(payload), nil
	case "rebuttal":
		var (
			agent   string
			content string
		)
		if err := tx.QueryRowContext(
			ctx,
			`SELECT agent, content
			 FROM task_rebuttals
			 WHERE id = $1 AND task_id = $2`,
			referenceID,
			taskID,
		).Scan(&agent, &content); err != nil {
			if err == sql.ErrNoRows {
				return "", nil
			}
			return "", fmt.Errorf("query referenced rebuttal: %w", err)
		}
		return protocol.HashStrings([]string{agent, content}), nil
	case "proof":
		var semanticRoot string
		if err := tx.QueryRowContext(
			ctx,
			`SELECT semantic_root
			 FROM proof_artifacts
			 WHERE id = $1 AND task_id = $2`,
			referenceID,
			taskID,
		).Scan(&semanticRoot); err != nil {
			if err == sql.ErrNoRows {
				return "", nil
			}
			return "", fmt.Errorf("query referenced proof: %w", err)
		}
		return semanticRoot, nil
	default:
		return "", fmt.Errorf("%w: unsupported reference type %s", ErrValidation, referenceType)
	}
}
