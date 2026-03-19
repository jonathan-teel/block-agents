package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"aichain/internal/execution"
	"aichain/internal/proof"
	"aichain/internal/protocol"
	"aichain/internal/txauth"

	"github.com/google/uuid"
)

func (s *Store) executePendingTransactionTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	if err := s.authorizePendingTransactionTx(ctx, tx, pending); err != nil {
		return nil, err
	}

	savepointName := fmt.Sprintf("tx_%d", pending.Sequence)
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepointName); err != nil {
		return nil, fmt.Errorf("create transaction savepoint: %w", err)
	}

	events, err := s.executeTransaction(ctx, tx, pending, nowUnix)
	if err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepointName); rollbackErr != nil {
			return nil, fmt.Errorf("rollback failed transaction state: %v (original error: %w)", rollbackErr, err)
		}
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepointName); releaseErr != nil {
			return nil, fmt.Errorf("release transaction savepoint after rollback: %w", releaseErr)
		}
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepointName); err != nil {
		return nil, fmt.Errorf("release transaction savepoint: %w", err)
	}

	return events, nil
}

func (s *Store) executeTransaction(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	switch pending.Type {
	case protocol.TxTypeCreateTask:
		return s.executeCreateTaskTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitInference:
		return s.executeSubmissionTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitProposal:
		return s.executeProposalTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitEvaluation:
		return s.executeEvaluationTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitRebuttal:
		return s.executeRebuttalTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitVote:
		return s.executeVoteTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitProof:
		return s.executeProofTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeFundAgent:
		return s.executeFundAgentTx(ctx, tx, pending)
	case protocol.TxTypeBootstrapAgentKey:
		return s.executeBootstrapAgentKeyTx(ctx, tx, pending)
	case protocol.TxTypeRotateAgentKey:
		return s.executeRotateAgentKeyTx(ctx, tx, pending)
	case protocol.TxTypeUpsertValidator:
		return s.executeUpsertValidatorTx(ctx, tx, pending)
	case protocol.TxTypeDeactivateValidator:
		return s.executeDeactivateValidatorTx(ctx, tx, pending)
	case protocol.TxTypeOpenDispute:
		return s.executeOpenDisputeTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeResolveDispute:
		return s.executeResolveDisputeTx(ctx, tx, pending)
	case protocol.TxTypeSubmitGovernanceProposal:
		return s.executeSubmitGovernanceProposalTx(ctx, tx, pending, nowUnix)
	case protocol.TxTypeSubmitGovernanceVote:
		return s.executeSubmitGovernanceVoteTx(ctx, tx, pending, nowUnix)
	default:
		return nil, fmt.Errorf("unsupported transaction type %q", pending.Type)
	}
}

func (s *Store) executeCreateTaskTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.CreateTaskRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode create_task payload: %w", err)
	}
	if pending.Sender != payload.Creator {
		return nil, fmt.Errorf("%w: sender does not match creator", ErrValidation)
	}
	if payload.Type == "" {
		payload.Type = protocol.TaskTypePrediction
	}
	if payload.RoleSelectionPolicy == "" {
		payload.RoleSelectionPolicy = effectiveRoleSelectionPolicyTx(ctx, tx, s.cfg)
	}

	if payload.Deadline <= nowUnix {
		return nil, fmt.Errorf("deadline must be in the future when block is sealed")
	}

	balance, err := lockBalanceTx(ctx, tx, payload.Creator)
	if err != nil {
		return nil, err
	}
	if balance < payload.RewardPool {
		return nil, ErrInsufficientBalance
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance - $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		payload.RewardPool,
		payload.Creator,
	); err != nil {
		return nil, fmt.Errorf("fund task reward pool: %w", err)
	}

	taskID := uuid.NewString()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO tasks (id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, role_selection_policy, oracle_source, oracle_endpoint, oracle_path, reward_pool, min_stake, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		taskID,
		payload.Creator,
		payload.Type,
		payload.Question,
		payload.Deadline,
		maxInt(payload.DebateRounds, 1),
		payload.WorkerCount,
		payload.MinerCount,
		payload.RoleSelectionPolicy,
		payload.OracleSource,
		payload.OracleEndpoint,
		payload.OraclePath,
		payload.RewardPool,
		payload.MinStake,
		protocol.StatusOpen,
	); err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}

	task := protocol.Task{
		ID:      taskID,
		Creator: payload.Creator,
		Type:    payload.Type,
		Input: protocol.TaskInput{
			Question:     payload.Question,
			Deadline:     payload.Deadline,
			DebateRounds: maxInt(payload.DebateRounds, 1),
			WorkerCount:  payload.WorkerCount,
			MinerCount:   payload.MinerCount,
			RoleSelectionPolicy: payload.RoleSelectionPolicy,
			OracleSource: payload.OracleSource,
			OracleEndpoint: payload.OracleEndpoint,
			OraclePath: payload.OraclePath,
		},
		RewardPool: payload.RewardPool,
		MinStake:   payload.MinStake,
		Status:     protocol.StatusOpen,
	}

	events := []protocol.Event{
		{
			Type: "task.created",
			Attributes: map[string]string{
				"task_id":     taskID,
				"creator":     payload.Creator,
				"task_type":   payload.Type,
				"reward_pool": formatFloat(payload.RewardPool),
				"deadline":    strconv.FormatInt(payload.Deadline, 10),
				"role_selection_policy": payload.RoleSelectionPolicy,
				"oracle_source": payload.OracleSource,
			},
		},
	}

	if payload.Type == protocol.TaskTypeBlockAgents {
		if err := initializeDebateStateTx(ctx, tx, task, nowUnix); err != nil {
			return nil, err
		}
		assignmentEvents, err := s.assignTaskRolesTx(ctx, tx, taskID, payload.Creator, payload.WorkerCount, payload.MinerCount, payload.RoleSelectionPolicy)
		if err != nil {
			return nil, err
		}
		events = append(events, assignmentEvents...)
	}

	return events, nil
}

func (s *Store) executeSubmissionTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_inference payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypePrediction && task.Type != protocol.TaskTypeOraclePrediction {
		return nil, fmt.Errorf("%w: submissions are only valid for prediction-family tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Stake < task.MinStake {
		return nil, fmt.Errorf("%w: stake below task minimum", ErrValidation)
	}

	balance, err := lockBalanceTx(ctx, tx, payload.Agent)
	if err != nil {
		return nil, err
	}
	if balance < payload.Stake {
		return nil, ErrInsufficientBalance
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance - $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		payload.Stake,
		payload.Agent,
	); err != nil {
		return nil, fmt.Errorf("lock stake: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO submissions (task_id, agent, value, stake)
		 VALUES ($1, $2, $3, $4)`,
		payload.TaskID,
		payload.Agent,
		payload.Value,
		payload.Stake,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateSubmission
		}
		return nil, fmt.Errorf("insert submission: %w", err)
	}

	return []protocol.Event{
		{
			Type: "submission.accepted",
			Attributes: map[string]string{
				"task_id": payload.TaskID,
				"agent":   payload.Agent,
				"value":   formatFloat(payload.Value),
				"stake":   formatFloat(payload.Stake),
			},
		},
	}, nil
}

func (s *Store) executeProposalTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitProposalRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_proposal payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypeBlockAgents {
		return nil, fmt.Errorf("%w: proposals are only valid for blockagents tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Round > task.Input.DebateRounds {
		return nil, fmt.Errorf("%w: round exceeds debate_rounds", ErrValidation)
	}
	if _, err := requireDebateStageTx(ctx, tx, task, payload.Round, protocol.DebateStageProposal); err != nil {
		return nil, err
	}
	if !hasRoleTx(ctx, tx, payload.TaskID, payload.Agent, protocol.RoleWorker) {
		return nil, fmt.Errorf("%w: agent is not assigned as a worker", ErrValidation)
	}
	hasProof, err := hasProofForStageTx(ctx, tx, payload.TaskID, payload.Agent, payload.Round, protocol.DebateStageProposal)
	if err != nil {
		return nil, err
	}
	if !hasProof {
		return nil, fmt.Errorf("%w: proof-of-thought artifact required before proposal submission", ErrValidation)
	}

	var proposalID int64
	if err := tx.QueryRowContext(
		ctx,
		`INSERT INTO task_proposals (task_id, agent, round, content)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		payload.TaskID,
		payload.Agent,
		payload.Round,
		payload.Content,
	).Scan(&proposalID); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: worker already submitted a proposal for this round", ErrValidation)
		}
		return nil, fmt.Errorf("insert proposal: %w", err)
	}

	events := []protocol.Event{
		{
			Type: "proposal.submitted",
			Attributes: map[string]string{
				"task_id":     payload.TaskID,
				"proposal_id": strconv.FormatInt(proposalID, 10),
				"agent":       payload.Agent,
				"round":       strconv.Itoa(payload.Round),
			},
		},
	}
	if s.cfg.AllowEarlyDebateAdvance {
		advanceEvents, err := s.maybeAdvanceDebateStateTx(ctx, tx, task, nowUnix)
		if err != nil {
			return nil, err
		}
		events = append(events, advanceEvents...)
	}

	return events, nil
}

func (s *Store) executeEvaluationTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitEvaluationRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_evaluation payload: %w", err)
	}
	if pending.Sender != payload.Evaluator {
		return nil, fmt.Errorf("%w: sender does not match evaluator", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypeBlockAgents {
		return nil, fmt.Errorf("%w: evaluations are only valid for blockagents tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Round > task.Input.DebateRounds {
		return nil, fmt.Errorf("%w: round exceeds debate_rounds", ErrValidation)
	}
	if _, err := requireDebateStageTx(ctx, tx, task, payload.Round, protocol.DebateStageEvaluation); err != nil {
		return nil, err
	}
	if !hasRoleTx(ctx, tx, payload.TaskID, payload.Evaluator, protocol.RoleMiner) {
		return nil, fmt.Errorf("%w: evaluator is not assigned as a miner", ErrValidation)
	}
	hasProof, err := hasProofForStageTx(ctx, tx, payload.TaskID, payload.Evaluator, payload.Round, protocol.DebateStageEvaluation)
	if err != nil {
		return nil, err
	}
	if !hasProof {
		return nil, fmt.Errorf("%w: proof-of-thought artifact required before evaluation submission", ErrValidation)
	}

	var proposalRound int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT round
		 FROM task_proposals
		 WHERE id = $1 AND task_id = $2`,
		payload.ProposalID,
		payload.TaskID,
	).Scan(&proposalRound); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query proposal for evaluation: %w", err)
	}
	if proposalRound != payload.Round {
		return nil, fmt.Errorf("%w: proposal round mismatch", ErrValidation)
	}

	overallScore := (payload.FactualConsistency + payload.RedundancyScore + payload.CausalRelevance) / 3

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_evaluations (
			task_id, proposal_id, evaluator, round,
			factual_consistency, redundancy_score, causal_relevance, overall_score, comments
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		payload.TaskID,
		payload.ProposalID,
		payload.Evaluator,
		payload.Round,
		payload.FactualConsistency,
		payload.RedundancyScore,
		payload.CausalRelevance,
		overallScore,
		payload.Comments,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: miner already evaluated this proposal for this round", ErrValidation)
		}
		return nil, fmt.Errorf("insert evaluation: %w", err)
	}

	events := []protocol.Event{
		{
			Type: "evaluation.submitted",
			Attributes: map[string]string{
				"task_id":       payload.TaskID,
				"proposal_id":   strconv.FormatInt(payload.ProposalID, 10),
				"evaluator":     payload.Evaluator,
				"round":         strconv.Itoa(payload.Round),
				"overall_score": formatFloat(overallScore),
			},
		},
	}
	if s.cfg.AllowEarlyDebateAdvance {
		advanceEvents, err := s.maybeAdvanceDebateStateTx(ctx, tx, task, nowUnix)
		if err != nil {
			return nil, err
		}
		events = append(events, advanceEvents...)
	}

	return events, nil
}

func (s *Store) executeRebuttalTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitRebuttalRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_rebuttal payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypeBlockAgents {
		return nil, fmt.Errorf("%w: rebuttals are only valid for blockagents tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Round > task.Input.DebateRounds {
		return nil, fmt.Errorf("%w: round exceeds debate_rounds", ErrValidation)
	}
	if _, err := requireDebateStageTx(ctx, tx, task, payload.Round, protocol.DebateStageRebuttal); err != nil {
		return nil, err
	}
	if !hasRoleTx(ctx, tx, payload.TaskID, payload.Agent, protocol.RoleWorker) {
		return nil, fmt.Errorf("%w: agent is not assigned as a worker", ErrValidation)
	}
	hasProof, err := hasProofForStageTx(ctx, tx, payload.TaskID, payload.Agent, payload.Round, protocol.DebateStageRebuttal)
	if err != nil {
		return nil, err
	}
	if !hasProof {
		return nil, fmt.Errorf("%w: proof-of-thought artifact required before rebuttal submission", ErrValidation)
	}

	var proposalRound int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT round
		 FROM task_proposals
		 WHERE id = $1 AND task_id = $2`,
		payload.ProposalID,
		payload.TaskID,
	).Scan(&proposalRound); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query proposal for rebuttal: %w", err)
	}
	if proposalRound != payload.Round {
		return nil, fmt.Errorf("%w: proposal round mismatch", ErrValidation)
	}

	var rebuttalID int64
	if err := tx.QueryRowContext(
		ctx,
		`INSERT INTO task_rebuttals (task_id, proposal_id, agent, round, content)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		payload.TaskID,
		payload.ProposalID,
		payload.Agent,
		payload.Round,
		payload.Content,
	).Scan(&rebuttalID); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: worker already submitted a rebuttal for this round", ErrValidation)
		}
		return nil, fmt.Errorf("insert rebuttal: %w", err)
	}

	events := []protocol.Event{
		{
			Type: "rebuttal.submitted",
			Attributes: map[string]string{
				"task_id":     payload.TaskID,
				"rebuttal_id": strconv.FormatInt(rebuttalID, 10),
				"proposal_id": strconv.FormatInt(payload.ProposalID, 10),
				"agent":       payload.Agent,
				"round":       strconv.Itoa(payload.Round),
			},
		},
	}
	if s.cfg.AllowEarlyDebateAdvance {
		advanceEvents, err := s.maybeAdvanceDebateStateTx(ctx, tx, task, nowUnix)
		if err != nil {
			return nil, err
		}
		events = append(events, advanceEvents...)
	}

	return events, nil
}

func (s *Store) executeVoteTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitVoteRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_vote payload: %w", err)
	}
	if pending.Sender != payload.Voter {
		return nil, fmt.Errorf("%w: sender does not match voter", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypeBlockAgents {
		return nil, fmt.Errorf("%w: votes are only valid for blockagents tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Round > task.Input.DebateRounds {
		return nil, fmt.Errorf("%w: round exceeds debate_rounds", ErrValidation)
	}
	if _, err := requireDebateStageTx(ctx, tx, task, payload.Round, protocol.DebateStageVote); err != nil {
		return nil, err
	}
	if !hasRoleTx(ctx, tx, payload.TaskID, payload.Voter, protocol.RoleMiner) {
		return nil, fmt.Errorf("%w: voter is not assigned as a miner", ErrValidation)
	}
	hasProof, err := hasProofForStageTx(ctx, tx, payload.TaskID, payload.Voter, payload.Round, protocol.DebateStageVote)
	if err != nil {
		return nil, err
	}
	if !hasProof {
		return nil, fmt.Errorf("%w: proof-of-thought artifact required before vote submission", ErrValidation)
	}

	var proposalRound int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT round
		 FROM task_proposals
		 WHERE id = $1 AND task_id = $2`,
		payload.ProposalID,
		payload.TaskID,
	).Scan(&proposalRound); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query proposal for vote: %w", err)
	}
	if proposalRound != payload.Round {
		return nil, fmt.Errorf("%w: proposal round mismatch", ErrValidation)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_votes (task_id, proposal_id, voter, round, reason)
		 VALUES ($1, $2, $3, $4, $5)`,
		payload.TaskID,
		payload.ProposalID,
		payload.Voter,
		payload.Round,
		payload.Reason,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: miner already voted in this round", ErrValidation)
		}
		return nil, fmt.Errorf("insert vote: %w", err)
	}

	events := []protocol.Event{
		{
			Type: "vote.submitted",
			Attributes: map[string]string{
				"task_id":     payload.TaskID,
				"proposal_id": strconv.FormatInt(payload.ProposalID, 10),
				"voter":       payload.Voter,
				"round":       strconv.Itoa(payload.Round),
			},
		},
	}
	if s.cfg.AllowEarlyDebateAdvance {
		advanceEvents, err := s.maybeAdvanceDebateStateTx(ctx, tx, task, nowUnix)
		if err != nil {
			return nil, err
		}
		events = append(events, advanceEvents...)
	}

	return events, nil
}

func (s *Store) executeProofTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitProofRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_proof payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	task, err := getTaskForUpdate(ctx, tx, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != protocol.TaskTypeBlockAgents {
		return nil, fmt.Errorf("%w: proofs are only valid for blockagents tasks", ErrValidation)
	}
	if task.Status != protocol.StatusOpen {
		return nil, fmt.Errorf("%w: task is not open", ErrValidation)
	}
	if task.Input.Deadline <= nowUnix {
		return nil, fmt.Errorf("%w: task deadline has passed", ErrValidation)
	}
	if payload.Round > task.Input.DebateRounds {
		return nil, fmt.Errorf("%w: round exceeds debate_rounds", ErrValidation)
	}
	if payload.Stage != protocol.DebateStageProposal && payload.Stage != protocol.DebateStageEvaluation && payload.Stage != protocol.DebateStageRebuttal && payload.Stage != protocol.DebateStageVote {
		return nil, fmt.Errorf("%w: unsupported proof stage", ErrValidation)
	}
	if _, err := requireDebateStageTx(ctx, tx, task, payload.Round, payload.Stage); err != nil {
		return nil, err
	}

	expectedRole := protocol.RoleMiner
	if payload.Stage == protocol.DebateStageProposal || payload.Stage == protocol.DebateStageRebuttal {
		expectedRole = protocol.RoleWorker
	}
	if !hasRoleTx(ctx, tx, payload.TaskID, payload.Agent, expectedRole) {
		return nil, fmt.Errorf("%w: agent is not assigned for this proof stage", ErrValidation)
	}

	verifiedArtifact, err := proof.VerifyArtifact(payload.Stage, payload.ArtifactType, payload.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	references, err := verifyProofReferencesTx(ctx, tx, payload.TaskID, verifiedArtifact.References)
	if err != nil {
		return nil, err
	}
	if len(references) > 0 {
		verifiedArtifact, err = proof.FinalizeArtifact(payload.Stage, payload.ArtifactType, payload.Content, references)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}
	if payload.ParentType != "" && payload.ParentID != nil {
		digest, err := referencedObjectDigestTx(ctx, tx, payload.TaskID, payload.ParentType, *payload.ParentID)
		if err != nil {
			return nil, err
		}
		if digest == "" {
			return nil, fmt.Errorf("%w: referenced parent %s %d is missing", ErrValidation, payload.ParentType, *payload.ParentID)
		}
	}
	var proofID int64
	if err := tx.QueryRowContext(
		ctx,
		`INSERT INTO proof_artifacts (task_id, agent, round, stage, artifact_type, content, content_hash, claim_root, semantic_root, parent_type, parent_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id`,
		payload.TaskID,
		payload.Agent,
		payload.Round,
		payload.Stage,
		payload.ArtifactType,
		verifiedArtifact.NormalizedContent,
		verifiedArtifact.ContentHash,
		verifiedArtifact.ClaimRoot,
		verifiedArtifact.SemanticRoot,
		nullIfEmpty(payload.ParentType),
		nullableInt64(payload.ParentID),
	).Scan(&proofID); err != nil {
		return nil, fmt.Errorf("insert proof artifact: %w", err)
	}

	return []protocol.Event{
		{
			Type: "proof.submitted",
			Attributes: map[string]string{
				"task_id":       payload.TaskID,
				"proof_id":      strconv.FormatInt(proofID, 10),
				"agent":         payload.Agent,
				"round":         strconv.Itoa(payload.Round),
				"stage":         payload.Stage,
				"artifact_type": payload.ArtifactType,
			},
		},
	}, nil
}

func (s *Store) executeFundAgentTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	if !s.cfg.EnableFaucet {
		return nil, ErrFaucetDisabled
	}

	var payload protocol.FundAgentRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode fund_agent payload: %w", err)
	}
	if pending.Sender != s.cfg.Genesis.FaucetAddress {
		return nil, fmt.Errorf("%w: invalid faucet sender", ErrValidation)
	}

	balance, err := lockBalanceTx(ctx, tx, s.cfg.Genesis.FaucetAddress)
	if err != nil {
		return nil, err
	}
	if balance < payload.Amount {
		return nil, ErrInsufficientBalance
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance - $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		payload.Amount,
		s.cfg.Genesis.FaucetAddress,
	); err != nil {
		return nil, fmt.Errorf("debit faucet: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO agents (address, balance, reputation)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (address) DO NOTHING`,
		payload.Agent,
		0,
		s.cfg.DefaultAgentReputation,
	); err != nil {
		return nil, fmt.Errorf("ensure funded agent: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance + $1,
		     updated_at = NOW()
		 WHERE address = $2`,
		payload.Amount,
		payload.Agent,
	); err != nil {
		return nil, fmt.Errorf("credit funded agent: %w", err)
	}

	return []protocol.Event{
		{
			Type: "agent.funded",
			Attributes: map[string]string{
				"agent":  payload.Agent,
				"amount": formatFloat(payload.Amount),
			},
		},
	}, nil
}

func (s *Store) executeBootstrapAgentKeyTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	var payload protocol.BootstrapAgentKeyRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode bootstrap_agent_key payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	state, err := lookupAuthStateForUpdate(ctx, tx, payload.Agent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !state.PublicKey.Valid || state.PublicKey.String == "" {
		return nil, fmt.Errorf("%w: agent key bootstrap requires a bound public key", ErrValidation)
	}

	return []protocol.Event{
		{
			Type: "agent.key_bootstrapped",
			Attributes: map[string]string{
				"agent":      payload.Agent,
				"public_key": pending.PublicKey,
			},
		},
	}, nil
}

func (s *Store) executeRotateAgentKeyTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	var payload protocol.RotateAgentKeyRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode rotate_agent_key payload: %w", err)
	}
	if pending.Sender != payload.Agent {
		return nil, fmt.Errorf("%w: sender does not match agent", ErrValidation)
	}

	state, err := lookupAuthStateForUpdate(ctx, tx, payload.Agent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !state.PublicKey.Valid || state.PublicKey.String == "" {
		return nil, fmt.Errorf("%w: agent must bootstrap a key before rotation", ErrValidation)
	}
	oldPublicKey := txauth.NormalizePublicKey(state.PublicKey.String)
	if oldPublicKey != pending.PublicKey {
		return nil, fmt.Errorf("%w: rotation must be authorized by the current public key", ErrUnauthorized)
	}
	if payload.NewPublicKey == "" || payload.NewSignature == "" {
		return nil, fmt.Errorf("%w: new_public_key and new_signature are required", ErrValidation)
	}
	if payload.NewPublicKey == oldPublicKey {
		return nil, fmt.Errorf("%w: new_public_key must differ from current public key", ErrValidation)
	}
	if err := txauth.VerifyRotationProof(s.cfg.Genesis.ChainID, payload.Agent, oldPublicKey, payload.NewPublicKey, pending.Nonce, payload.NewSignature); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET public_key = $2,
		     updated_at = NOW()
		 WHERE address = $1`,
		payload.Agent,
		payload.NewPublicKey,
	); err != nil {
		return nil, fmt.Errorf("rotate agent public key: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO agent_key_rotations (agent, old_public_key, new_public_key, tx_hash, rotated_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		payload.Agent,
		oldPublicKey,
		payload.NewPublicKey,
		pending.Hash,
	); err != nil {
		return nil, fmt.Errorf("record agent key rotation: %w", err)
	}

	return []protocol.Event{
		{
			Type: "agent.key_rotated",
			Attributes: map[string]string{
				"agent":          payload.Agent,
				"old_public_key": oldPublicKey,
				"new_public_key": payload.NewPublicKey,
			},
		},
	}, nil
}

func (s *Store) executeUpsertValidatorTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	var payload protocol.UpsertValidatorRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode upsert_validator payload: %w", err)
	}
	if pending.Sender != payload.Operator {
		return nil, fmt.Errorf("%w: sender does not match operator", ErrValidation)
	}

	active, err := isActiveValidatorTx(ctx, tx, payload.Operator)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: operator is not an active validator", ErrUnauthorized)
	}
	if err := upsertValidatorRegistryTx(ctx, tx, payload.Validator, payload.PublicKey, payload.Power, s.cfg.DefaultAgentReputation); err != nil {
		return nil, err
	}

	return []protocol.Event{
		{
			Type: "validator.upserted",
			Attributes: map[string]string{
				"operator":  payload.Operator,
				"validator": payload.Validator,
				"power":     strconv.FormatInt(payload.Power, 10),
			},
		},
	}, nil
}

func (s *Store) executeDeactivateValidatorTx(ctx context.Context, tx *sql.Tx, pending pendingTx) ([]protocol.Event, error) {
	var payload protocol.DeactivateValidatorRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode deactivate_validator payload: %w", err)
	}
	if pending.Sender != payload.Operator {
		return nil, fmt.Errorf("%w: sender does not match operator", ErrValidation)
	}

	active, err := isActiveValidatorTx(ctx, tx, payload.Operator)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: operator is not an active validator", ErrUnauthorized)
	}
	if err := deactivateValidatorRegistryTx(ctx, tx, payload.Validator); err != nil {
		return nil, err
	}

	return []protocol.Event{
		{
			Type: "validator.deactivated",
			Attributes: map[string]string{
				"operator":  payload.Operator,
				"validator": payload.Validator,
			},
		},
	}, nil
}

func updateConsensusTx(ctx context.Context, tx *sql.Tx, maxEffectiveWeight float64) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, type
		 FROM tasks
		 WHERE status = $1
		 ORDER BY created_at ASC`,
		protocol.StatusOpen,
	)
	if err != nil {
		return nil, fmt.Errorf("query open tasks for consensus: %w", err)
	}
	defer rows.Close()

	type taskRef struct {
		ID   string
		Type string
	}

	taskRefs := make([]taskRef, 0)
	for rows.Next() {
		var item taskRef
		if err := rows.Scan(&item.ID, &item.Type); err != nil {
			return nil, fmt.Errorf("scan consensus task id: %w", err)
		}
		taskRefs = append(taskRefs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate consensus task ids: %w", err)
	}

	events := make([]protocol.Event, 0)
	for _, taskRef := range taskRefs {
		if taskRef.Type != protocol.TaskTypePrediction && taskRef.Type != protocol.TaskTypeOraclePrediction {
			continue
		}

		submissions, err := listWeightedSubmissionsTx(ctx, tx, taskRef.ID)
		if err != nil {
			return nil, err
		}
		if len(submissions) == 0 {
			continue
		}

		consensus, ok := execution.ComputeWeightedConsensus(submissions, maxEffectiveWeight)
		if !ok {
			continue
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_results (task_id, final_value, settled, updated_at)
			 VALUES ($1, $2, FALSE, NOW())
			 ON CONFLICT (task_id) DO UPDATE
			 SET final_value = EXCLUDED.final_value,
			     updated_at = NOW()
			 WHERE task_results.settled = FALSE`,
			taskRef.ID,
			consensus,
		); err != nil {
			return nil, fmt.Errorf("upsert task consensus: %w", err)
		}

		events = append(events, protocol.Event{
			Type: "consensus.updated",
			Attributes: map[string]string{
				"task_id":     taskRef.ID,
				"final_value": formatFloat(consensus),
			},
		})
	}

	return events, nil
}

func (s *Store) settleExpiredTasksTx(ctx context.Context, tx *sql.Tx, nowUnix int64) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id
		 FROM tasks
		 WHERE status = $1 AND deadline < $2
		 ORDER BY deadline ASC, created_at ASC`,
		protocol.StatusOpen,
		nowUnix,
	)
	if err != nil {
		return nil, fmt.Errorf("query expired tasks: %w", err)
	}
	defer rows.Close()

	taskIDs := make([]string, 0)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("scan expired task id: %w", err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired task ids: %w", err)
	}

	events := make([]protocol.Event, 0)
	for _, taskID := range taskIDs {
		task, err := getTaskForUpdate(ctx, tx, taskID)
		if err != nil {
			return nil, err
		}
		if task.Status != protocol.StatusOpen {
			continue
		}

		switch task.Type {
		case protocol.TaskTypeBlockAgents:
			settlementEvents, err := s.settleBlockAgentsTaskTx(ctx, tx, task)
			if err != nil {
				return nil, err
			}
			events = append(events, settlementEvents...)
		default:
			settlementEvents, err := s.settlePredictionTaskTx(ctx, tx, task)
			if err != nil {
				return nil, err
			}
			events = append(events, settlementEvents...)
		}
	}

	return events, nil
}

func (s *Store) settlePredictionTaskTx(ctx context.Context, tx *sql.Tx, task protocol.Task) ([]protocol.Event, error) {
	outcome, outcomeSource, ok, err := resolvePredictionOutcomeTx(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	submissions, err := listWeightedSubmissionsTx(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}

	scores := execution.ScoreSubmissions(submissions, outcome)
	rewards := execution.ComputeRewards(scores, task.RewardPool)

	if len(scores) == 0 || execution.RewardWeight(scores) == 0 {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			task.RewardPool,
			task.Creator,
		); err != nil {
			return nil, fmt.Errorf("refund unused reward pool: %w", err)
		}
	}

	for _, scored := range scores {
		payout := rewards[scored.SubmissionID] + (scored.Stake * scored.Score)
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     reputation = $2,
			     updated_at = NOW()
			 WHERE address = $3`,
			payout,
			execution.BlendReputation(scored.OldReputation, scored.Score),
			scored.Agent,
		); err != nil {
			return nil, fmt.Errorf("update agent settlement state: %w", err)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_results (task_id, outcome, settled, settled_at, updated_at)
		 VALUES ($1, $2, TRUE, NOW(), NOW())
		 ON CONFLICT (task_id) DO UPDATE
		 SET outcome = EXCLUDED.outcome,
		     settled = TRUE,
		     settled_at = NOW(),
		     updated_at = NOW()`,
		task.ID,
		outcome,
	); err != nil {
		return nil, fmt.Errorf("upsert settled result: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tasks
		 SET status = $2,
		     updated_at = NOW()
		 WHERE id = $1`,
		task.ID,
		protocol.StatusSettled,
	); err != nil {
		return nil, fmt.Errorf("mark task settled: %w", err)
	}

	return []protocol.Event{
		{
			Type: "task.settled",
			Attributes: map[string]string{
				"task_id":     task.ID,
				"task_type":   task.Type,
				"outcome":     formatFloat(outcome),
				"outcome_source": outcomeSource,
				"submissions": strconv.Itoa(len(submissions)),
			},
		},
	}, nil
}

func (s *Store) settleBlockAgentsTaskTx(ctx context.Context, tx *sql.Tx, task protocol.Task) ([]protocol.Event, error) {
	proposals, err := s.listProposalsByTask(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	if len(proposals) == 0 {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			task.RewardPool,
			task.Creator,
		); err != nil {
			return nil, fmt.Errorf("refund reward pool without proposals: %w", err)
		}
		return s.markTaskSettledTx(ctx, tx, task.ID, nil, nil, nil)
	}

	type proposalScore struct {
		Proposal protocol.Proposal
		Score    float64
		Weight   float64
		Votes    int
		VotePower float64
		Round    int
	}

	scored := make([]proposalScore, 0, len(proposals))
	latestRound := 0
	for _, proposal := range proposals {
		if proposal.Round > latestRound {
			latestRound = proposal.Round
		}
	}
	var winning *proposalScore
	minerVotePolicy := effectiveMinerVotePolicyTx(ctx, tx, s.cfg)
	for _, proposal := range proposals {
		if proposal.Round != latestRound {
			continue
		}
		score, weight, err := s.computeProposalScoreTx(ctx, tx, task.ID, proposal.ID)
		if err != nil {
			return nil, err
		}
		votes, votePower, err := countProposalVotesTx(ctx, tx, task.ID, proposal.ID, proposal.Round, minerVotePolicy)
		if err != nil {
			return nil, err
		}
		entry := proposalScore{Proposal: proposal, Score: score, Weight: weight, Votes: votes, VotePower: votePower, Round: proposal.Round}
		scored = append(scored, entry)
		if winning == nil ||
			entry.VotePower > winning.VotePower ||
			(entry.VotePower == winning.VotePower && entry.Votes > winning.Votes) ||
			(entry.VotePower == winning.VotePower && entry.Votes == winning.Votes && entry.Score > winning.Score) ||
			(entry.VotePower == winning.VotePower && entry.Votes == winning.Votes && entry.Score == winning.Score && entry.Weight > winning.Weight) {
			copy := entry
			winning = &copy
		}
	}

	if winning == nil {
		return s.markTaskSettledTx(ctx, tx, task.ID, nil, nil, nil)
	}
	if winning.Weight == 0 || winning.Votes == 0 {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			task.RewardPool,
			task.Creator,
		); err != nil {
			return nil, fmt.Errorf("refund reward pool without miner evaluations: %w", err)
		}
		return s.markTaskSettledTx(ctx, tx, task.ID, nil, nil, nil)
	}

	workerReward := task.RewardPool
	minerRewardPool := 0.0
	if len(scored) > 1 {
		workerReward = task.RewardPool * 0.8
		minerRewardPool = task.RewardPool - workerReward
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET balance = balance + $1,
		     reputation = $2,
		     updated_at = NOW()
		 WHERE address = $3`,
		workerReward,
		execution.BlendReputation(currentReputationTx(ctx, tx, winning.Proposal.Agent), winning.Score),
		winning.Proposal.Agent,
	); err != nil {
		return nil, fmt.Errorf("reward winning worker: %w", err)
	}

	if minerRewardPool > 0 {
		if err := s.distributeMinerRewardsTx(ctx, tx, task.ID, winning.Proposal.ID, winning.Round, minerRewardPool); err != nil {
			return nil, err
		}
	}

	proposalID := winning.Proposal.ID
	agent := winning.Proposal.Agent
	score := winning.Score
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_results (task_id, final_value, winning_proposal_id, winning_agent, settled, settled_at, updated_at)
		 VALUES ($1, $2, $3, $4, TRUE, NOW(), NOW())
		 ON CONFLICT (task_id) DO UPDATE
		 SET final_value = EXCLUDED.final_value,
		     winning_proposal_id = EXCLUDED.winning_proposal_id,
		     winning_agent = EXCLUDED.winning_agent,
		     settled = TRUE,
		     settled_at = NOW(),
		     updated_at = NOW()`,
		task.ID,
		score,
		proposalID,
		agent,
	); err != nil {
		return nil, fmt.Errorf("upsert blockagents task result: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tasks
		 SET status = $2,
		     updated_at = NOW()
		 WHERE id = $1`,
		task.ID,
		protocol.StatusSettled,
	); err != nil {
		return nil, fmt.Errorf("mark blockagents task settled: %w", err)
	}

	return []protocol.Event{
		{
			Type: "task.settled",
			Attributes: map[string]string{
				"task_id":            task.ID,
				"task_type":          task.Type,
				"winning_proposal_id": strconv.FormatInt(winning.Proposal.ID, 10),
				"winning_agent":      winning.Proposal.Agent,
				"winning_score":      formatFloat(winning.Score),
				"winning_votes":      strconv.Itoa(winning.Votes),
				"winning_vote_power": formatFloat(winning.VotePower),
			},
		},
	}, nil
}

func (s *Store) assignTaskRolesTx(ctx context.Context, tx *sql.Tx, taskID string, creator string, workerCount int, minerCount int, policy string) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT address, balance, reputation
		 FROM agents
		 WHERE address <> $1`,
		creator,
	)
	if err != nil {
		return nil, fmt.Errorf("query role candidates: %w", err)
	}
	defer rows.Close()

	type roleCandidate struct {
		Address    string
		Balance    float64
		Reputation float64
	}

	candidates := make([]roleCandidate, 0)
	for rows.Next() {
		var candidate roleCandidate
		if err := rows.Scan(&candidate.Address, &candidate.Balance, &candidate.Reputation); err != nil {
			return nil, fmt.Errorf("scan role candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role candidates: %w", err)
	}

	required := workerCount + minerCount
	if len(candidates) < required {
		return nil, fmt.Errorf("%w: not enough funded agents to assign workers and miners", ErrValidation)
	}
	sort.Slice(candidates, func(i, j int) bool {
		switch strings.TrimSpace(policy) {
		case "reputation_balance":
			if candidates[i].Reputation == candidates[j].Reputation {
				if candidates[i].Balance == candidates[j].Balance {
					return candidates[i].Address < candidates[j].Address
				}
				return candidates[i].Balance > candidates[j].Balance
			}
			return candidates[i].Reputation > candidates[j].Reputation
		case "round_robin_hash":
			left := protocol.HashStrings([]string{taskID, candidates[i].Address})
			right := protocol.HashStrings([]string{taskID, candidates[j].Address})
			if left == right {
				return candidates[i].Address < candidates[j].Address
			}
			return left < right
		default:
			if candidates[i].Balance == candidates[j].Balance {
				if candidates[i].Reputation == candidates[j].Reputation {
					return candidates[i].Address < candidates[j].Address
				}
				return candidates[i].Reputation > candidates[j].Reputation
			}
			return candidates[i].Balance > candidates[j].Balance
		}
	})

	events := make([]protocol.Event, 0, required+1)
	for index := 0; index < minerCount; index++ {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_roles (task_id, agent, role)
			 VALUES ($1, $2, $3)`,
			taskID,
			candidates[index].Address,
			protocol.RoleMiner,
		); err != nil {
			return nil, fmt.Errorf("assign miner role: %w", err)
		}
	}

	for index := minerCount; index < required; index++ {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_roles (task_id, agent, role)
			 VALUES ($1, $2, $3)`,
			taskID,
			candidates[index].Address,
			protocol.RoleWorker,
		); err != nil {
			return nil, fmt.Errorf("assign worker role: %w", err)
		}
	}

	events = append(events, protocol.Event{
		Type: "task.roles_assigned",
		Attributes: map[string]string{
			"task_id":      taskID,
			"miner_count":  strconv.Itoa(minerCount),
			"worker_count": strconv.Itoa(workerCount),
			"policy":       policy,
		},
	})

	return events, nil
}

func hasRoleTx(ctx context.Context, tx *sql.Tx, taskID string, agent string, role string) bool {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM task_roles
		 WHERE task_id = $1 AND agent = $2 AND role = $3`,
		taskID,
		agent,
		role,
	).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (s *Store) computeProposalScoreTx(ctx context.Context, tx *sql.Tx, taskID string, proposalID int64) (float64, float64, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT e.overall_score, a.reputation
		 FROM task_evaluations e
		 INNER JOIN agents a ON a.address = e.evaluator
		 WHERE e.task_id = $1 AND e.proposal_id = $2`,
		taskID,
		proposalID,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("query proposal evaluations: %w", err)
	}
	defer rows.Close()

	var numerator float64
	var denominator float64
	for rows.Next() {
		var overall float64
		var reputation float64
		if err := rows.Scan(&overall, &reputation); err != nil {
			return 0, 0, fmt.Errorf("scan proposal evaluation: %w", err)
		}
		weight := execution.Clamp01(reputation)
		if weight == 0 {
			weight = 0.1
		}
		numerator += overall * weight
		denominator += weight
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate proposal evaluations: %w", err)
	}
	if denominator == 0 {
		return 0, 0, nil
	}
	return numerator / denominator, denominator, nil
}

func (s *Store) distributeMinerRewardsTx(ctx context.Context, tx *sql.Tx, taskID string, proposalID int64, round int, rewardPool float64) error {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT v.voter, a.reputation
		 FROM task_votes v
		 INNER JOIN agents a ON a.address = v.voter
		 WHERE v.task_id = $1 AND v.proposal_id = $2 AND v.round = $3`,
		taskID,
		proposalID,
		round,
	)
	if err != nil {
		return fmt.Errorf("query winning proposal voters: %w", err)
	}
	defer rows.Close()

	type minerReward struct {
		Agent  string
		Weight float64
	}
	votePolicy := effectiveMinerVotePolicyTx(ctx, tx, s.cfg)
	miners := make([]minerReward, 0)
	for rows.Next() {
		var entry minerReward
		var reputation float64
		if err := rows.Scan(&entry.Agent, &reputation); err != nil {
			return fmt.Errorf("scan winning proposal voter: %w", err)
		}
		if votePolicy == "one_agent_one_vote" {
			entry.Weight = 1
		} else {
			entry.Weight = execution.Clamp01(reputation)
			if entry.Weight == 0 {
				entry.Weight = 0.1
			}
		}
		miners = append(miners, entry)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate winning proposal voters: %w", err)
	}

	if len(miners) == 0 {
		return nil
	}

	var totalWeight float64
	for _, miner := range miners {
		totalWeight += miner.Weight
	}
	if totalWeight == 0 {
		totalWeight = float64(len(miners))
	}

	for _, miner := range miners {
		reward := rewardPool * (miner.Weight / totalWeight)
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     reputation = $2,
			     updated_at = NOW()
			WHERE address = $3`,
			reward,
			execution.BlendReputation(currentReputationTx(ctx, tx, miner.Agent), 1),
			miner.Agent,
		); err != nil {
			return fmt.Errorf("reward miner: %w", err)
		}
	}

	return nil
}

func countProposalVotesTx(ctx context.Context, tx *sql.Tx, taskID string, proposalID int64, round int, policy string) (int, float64, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT v.voter, a.reputation
		 FROM task_votes v
		 INNER JOIN agents a ON a.address = v.voter
		 WHERE v.task_id = $1 AND v.proposal_id = $2 AND v.round = $3`,
		taskID,
		proposalID,
		round,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("count proposal votes: %w", err)
	}
	defer rows.Close()

	var (
		count int
		power float64
	)
	for rows.Next() {
		var (
			voter      string
			reputation float64
		)
		if err := rows.Scan(&voter, &reputation); err != nil {
			return 0, 0, fmt.Errorf("scan proposal vote: %w", err)
		}
		count++
		if policy == "one_agent_one_vote" {
			power += 1
			continue
		}
		weight := execution.Clamp01(reputation)
		if weight == 0 {
			weight = 0.1
		}
		power += weight
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate proposal votes: %w", err)
	}
	return count, power, nil
}

func currentReputationTx(ctx context.Context, tx *sql.Tx, agent string) float64 {
	var reputation float64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT reputation
		 FROM agents
		 WHERE address = $1`,
		agent,
	).Scan(&reputation); err != nil {
		return 0.5
	}
	return reputation
}

func (s *Store) markTaskSettledTx(ctx context.Context, tx *sql.Tx, taskID string, finalValue *float64, winningProposalID *int64, winningAgent *string) ([]protocol.Event, error) {
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_results (task_id, final_value, winning_proposal_id, winning_agent, settled, settled_at, updated_at)
		 VALUES ($1, $2, $3, $4, TRUE, NOW(), NOW())
		 ON CONFLICT (task_id) DO UPDATE
		 SET final_value = EXCLUDED.final_value,
		     winning_proposal_id = EXCLUDED.winning_proposal_id,
		     winning_agent = EXCLUDED.winning_agent,
		     settled = TRUE,
		     settled_at = NOW(),
		     updated_at = NOW()`,
		taskID,
		nullableFloat64(finalValue),
		nullableInt64(winningProposalID),
		nullableString(winningAgent),
	); err != nil {
		return nil, fmt.Errorf("mark task settled: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tasks
		 SET status = $2,
		     updated_at = NOW()
		 WHERE id = $1`,
		taskID,
		protocol.StatusSettled,
	); err != nil {
		return nil, fmt.Errorf("update settled task: %w", err)
	}

	return []protocol.Event{
		{
			Type: "task.settled",
			Attributes: map[string]string{
				"task_id": taskID,
			},
		},
	}, nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
