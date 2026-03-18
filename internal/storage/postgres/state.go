package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aichain/internal/execution"
	"aichain/internal/protocol"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) GetAgent(ctx context.Context, address string) (protocol.Agent, error) {
	var agent protocol.Agent
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT address, COALESCE(public_key, ''), next_nonce, balance, reputation, created_at, updated_at
		 FROM agents
		 WHERE address = $1`,
		strings.TrimSpace(address),
	).Scan(&agent.Address, &agent.PublicKey, &agent.NextNonce, &agent.Balance, &agent.Reputation, &agent.CreatedAt, &agent.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Agent{}, ErrNotFound
		}
		return protocol.Agent{}, fmt.Errorf("query agent: %w", err)
	}

	return agent, nil
}

func (s *Store) ListOpenTasks(ctx context.Context) ([]protocol.Task, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, reward_pool, min_stake, status, created_at
		 FROM tasks
		 WHERE status = $1
		 ORDER BY deadline ASC, created_at ASC`,
		protocol.StatusOpen,
	)
	if err != nil {
		return nil, fmt.Errorf("query open tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]protocol.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open tasks: %w", err)
	}

	return tasks, nil
}

func (s *Store) GetTaskDetails(ctx context.Context, taskID string) (protocol.TaskDetails, error) {
	task, err := s.getTask(ctx, s.db, strings.TrimSpace(taskID))
	if err != nil {
		return protocol.TaskDetails{}, err
	}

	submissions, err := s.listSubmissionsByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}

	details := protocol.TaskDetails{
		Task:        task,
		Submissions: submissions,
	}

	assignments, err := s.listRoleAssignmentsByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}
	details.Assignments = assignments

	debateState, err := s.getDebateStateByTask(ctx, s.db, taskID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return protocol.TaskDetails{}, err
	}
	if err == nil {
		details.DebateState = debateState
	}

	proposals, err := s.listProposalsByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}
	details.Proposals = proposals

	evaluations, err := s.listEvaluationsByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}
	details.Evaluations = evaluations

	votes, err := s.listVotesByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}
	details.Votes = votes

	proofs, err := s.listProofsByTask(ctx, s.db, taskID)
	if err != nil {
		return protocol.TaskDetails{}, err
	}
	details.Proofs = proofs

	var (
		finalValue sql.NullFloat64
		outcome    sql.NullFloat64
		winningProposalID sql.NullInt64
		winningAgent sql.NullString
		settled    bool
		updatedAt  time.Time
		settledAt  sql.NullTime
	)

	err = s.db.QueryRowContext(
		ctx,
		`SELECT final_value, outcome, winning_proposal_id, winning_agent, settled, updated_at, settled_at
		 FROM task_results
		 WHERE task_id = $1`,
		taskID,
	).Scan(&finalValue, &outcome, &winningProposalID, &winningAgent, &settled, &updatedAt, &settledAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return details, nil
		}
		return protocol.TaskDetails{}, fmt.Errorf("query task result: %w", err)
	}

	if finalValue.Valid {
		value := finalValue.Float64
		details.CurrentConsensus = &value
	}

	result := &protocol.Result{
		TaskID:        taskID,
		Settled:       settled,
		LastUpdatedAt: updatedAt,
	}
	if finalValue.Valid {
		value := finalValue.Float64
		result.FinalValue = &value
	}
	if outcome.Valid {
		resolved := outcome.Float64
		result.Outcome = &resolved
	}
	if winningProposalID.Valid {
		proposalID := winningProposalID.Int64
		result.WinningProposalID = &proposalID
	}
	if winningAgent.Valid {
		agent := winningAgent.String
		result.WinningAgent = &agent
	}
	if settledAt.Valid {
		settledAtValue := settledAt.Time
		result.SettledAt = &settledAtValue
	}
	if settled {
		details.FinalResult = result
	}

	return details, nil
}

func (s *Store) getTask(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, taskID string) (protocol.Task, error) {
	task, err := scanTask(querier.QueryRowContext(
		ctx,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, reward_pool, min_stake, status, created_at
		 FROM tasks
		 WHERE id = $1`,
		taskID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Task{}, ErrNotFound
		}
		return protocol.Task{}, err
	}

	return task, nil
}

func (s *Store) listSubmissionsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.Submission, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, agent, value, stake, created_at
		 FROM submissions
		 WHERE task_id = $1
		 ORDER BY id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query submissions: %w", err)
	}
	defer rows.Close()

	submissions := make([]protocol.Submission, 0)
	for rows.Next() {
		var submission protocol.Submission
		if err := rows.Scan(&submission.ID, &submission.TaskID, &submission.Agent, &submission.Value, &submission.Stake, &submission.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan submission: %w", err)
		}
		submissions = append(submissions, submission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate submissions: %w", err)
	}

	return submissions, nil
}

func (s *Store) listRoleAssignmentsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.RoleAssignment, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT task_id, agent, role, assigned_at
		 FROM task_roles
		 WHERE task_id = $1
		 ORDER BY role ASC, assigned_at ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query role assignments: %w", err)
	}
	defer rows.Close()

	assignments := make([]protocol.RoleAssignment, 0)
	for rows.Next() {
		var assignment protocol.RoleAssignment
		if err := rows.Scan(&assignment.TaskID, &assignment.Agent, &assignment.Role, &assignment.AssignedAt); err != nil {
			return nil, fmt.Errorf("scan role assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role assignments: %w", err)
	}

	return assignments, nil
}

func (s *Store) listProposalsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.Proposal, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, agent, round, content, created_at
		 FROM task_proposals
		 WHERE task_id = $1
		 ORDER BY round ASC, id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query proposals: %w", err)
	}
	defer rows.Close()

	proposals := make([]protocol.Proposal, 0)
	for rows.Next() {
		var proposal protocol.Proposal
		if err := rows.Scan(&proposal.ID, &proposal.TaskID, &proposal.Agent, &proposal.Round, &proposal.Content, &proposal.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		proposals = append(proposals, proposal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposals: %w", err)
	}

	return proposals, nil
}

func (s *Store) listEvaluationsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.ProposalEvaluation, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, proposal_id, evaluator, round, factual_consistency, redundancy_score, causal_relevance, overall_score, comments, created_at
		 FROM task_evaluations
		 WHERE task_id = $1
		 ORDER BY round ASC, id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query evaluations: %w", err)
	}
	defer rows.Close()

	evaluations := make([]protocol.ProposalEvaluation, 0)
	for rows.Next() {
		var evaluation protocol.ProposalEvaluation
		if err := rows.Scan(
			&evaluation.ID,
			&evaluation.TaskID,
			&evaluation.ProposalID,
			&evaluation.Evaluator,
			&evaluation.Round,
			&evaluation.Metrics.FactualConsistency,
			&evaluation.Metrics.RedundancyScore,
			&evaluation.Metrics.CausalRelevance,
			&evaluation.Metrics.OverallScore,
			&evaluation.Comments,
			&evaluation.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan evaluation: %w", err)
		}
		evaluations = append(evaluations, evaluation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evaluations: %w", err)
	}

	return evaluations, nil
}

func (s *Store) listVotesByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.ProposalVote, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, proposal_id, voter, round, reason, created_at
		 FROM task_votes
		 WHERE task_id = $1
		 ORDER BY round ASC, id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query votes: %w", err)
	}
	defer rows.Close()

	votes := make([]protocol.ProposalVote, 0)
	for rows.Next() {
		var vote protocol.ProposalVote
		if err := rows.Scan(&vote.ID, &vote.TaskID, &vote.ProposalID, &vote.Voter, &vote.Round, &vote.Reason, &vote.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		votes = append(votes, vote)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate votes: %w", err)
	}

	return votes, nil
}

func (s *Store) getDebateStateByTask(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, taskID string) (*protocol.DebateState, error) {
	var state protocol.DebateState
	if err := querier.QueryRowContext(
		ctx,
		`SELECT task_id, current_round, current_stage, stage_duration_seconds, stage_started_at, stage_deadline, updated_at
		 FROM task_debate_state
		 WHERE task_id = $1`,
		taskID,
	).Scan(&state.TaskID, &state.CurrentRound, &state.CurrentStage, &state.StageDurationSec, &state.StageStartedAt, &state.StageDeadline, &state.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query debate state: %w", err)
	}

	return &state, nil
}

func (s *Store) listProofsByTask(ctx context.Context, querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, taskID string) ([]protocol.ProofOfThought, error) {
	rows, err := querier.QueryContext(
		ctx,
		`SELECT id, task_id, agent, round, stage, artifact_type, content, content_hash, claim_root, semantic_root, COALESCE(parent_type, ''), parent_id, created_at
		 FROM proof_artifacts
		 WHERE task_id = $1
		 ORDER BY round ASC, created_at ASC, id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query proofs: %w", err)
	}
	defer rows.Close()

	proofs := make([]protocol.ProofOfThought, 0)
	for rows.Next() {
		var (
			proof      protocol.ProofOfThought
			parentID   sql.NullInt64
		)
		if err := rows.Scan(
			&proof.ID,
			&proof.TaskID,
			&proof.Agent,
			&proof.Round,
			&proof.Stage,
			&proof.ArtifactType,
			&proof.Content,
			&proof.ContentHash,
			&proof.ClaimRoot,
			&proof.SemanticRoot,
			&proof.ParentType,
			&parentID,
			&proof.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan proof: %w", err)
		}
		if parentID.Valid {
			value := parentID.Int64
			proof.ParentID = &value
		}
		proofs = append(proofs, proof)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proofs: %w", err)
	}

	return proofs, nil
}

func scanTask(scanner rowScanner) (protocol.Task, error) {
	var (
		task     protocol.Task
		question string
	)

	if err := scanner.Scan(
		&task.ID,
		&task.Creator,
		&task.Type,
		&question,
		&task.Input.Deadline,
		&task.Input.DebateRounds,
		&task.Input.WorkerCount,
		&task.Input.MinerCount,
		&task.RewardPool,
		&task.MinStake,
		&task.Status,
		&task.CreatedAt,
	); err != nil {
		return protocol.Task{}, fmt.Errorf("scan task: %w", err)
	}

	task.Input.Question = question
	return task, nil
}

func getTaskForUpdate(ctx context.Context, tx *sql.Tx, taskID string) (protocol.Task, error) {
	task, err := scanTask(tx.QueryRowContext(
		ctx,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, reward_pool, min_stake, status, created_at
		 FROM tasks
		 WHERE id = $1
		 FOR UPDATE`,
		taskID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Task{}, ErrNotFound
		}
		return protocol.Task{}, err
	}

	return task, nil
}

func listWeightedSubmissionsTx(ctx context.Context, tx *sql.Tx, taskID string) ([]execution.WeightedSubmission, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT s.id, s.agent, s.value, s.stake, a.reputation
		 FROM submissions s
		 INNER JOIN agents a ON a.address = s.agent
		 WHERE s.task_id = $1
		 ORDER BY s.id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query weighted submissions: %w", err)
	}
	defer rows.Close()

	submissions := make([]execution.WeightedSubmission, 0)
	for rows.Next() {
		var submission execution.WeightedSubmission
		if err := rows.Scan(
			&submission.SubmissionID,
			&submission.Agent,
			&submission.Value,
			&submission.Stake,
			&submission.Reputation,
		); err != nil {
			return nil, fmt.Errorf("scan weighted submission: %w", err)
		}
		submissions = append(submissions, submission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weighted submissions: %w", err)
	}

	return submissions, nil
}

func lockBalanceTx(ctx context.Context, tx *sql.Tx, address string) (float64, error) {
	var balance float64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT balance
		 FROM agents
		 WHERE address = $1
		 FOR UPDATE`,
		address,
	).Scan(&balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("lock agent balance: %w", err)
	}
	return balance, nil
}

func computeStateRootTx(ctx context.Context, tx *sql.Tx) (string, error) {
	parts := make([]string, 0, 128)

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT address, COALESCE(public_key, ''), next_nonce, balance, reputation
		 FROM agents
		 ORDER BY address ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				address    string
				publicKey  string
				nextNonce  int64
				balance    float64
				reputation float64
			)
			if err := rows.Scan(&address, &publicKey, &nextNonce, &balance, &reputation); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"agent",
				address,
				publicKey,
				strconv.FormatInt(nextNonce, 10),
				formatFloat(balance),
				formatFloat(reputation),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, reward_pool, min_stake, status
		 FROM tasks
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id         string
				creator    string
				taskType   string
				question   string
				deadline   int64
				debateRounds int
				workerCount int
				minerCount int
				rewardPool float64
				minStake   float64
				status     string
			)
			if err := rows.Scan(&id, &creator, &taskType, &question, &deadline, &debateRounds, &workerCount, &minerCount, &rewardPool, &minStake, &status); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"task",
				id,
				creator,
				taskType,
				question,
				strconv.FormatInt(deadline, 10),
				strconv.Itoa(debateRounds),
				strconv.Itoa(workerCount),
				strconv.Itoa(minerCount),
				formatFloat(rewardPool),
				formatFloat(minStake),
				status,
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT task_id, agent, role
		 FROM task_roles
		 ORDER BY task_id ASC, role ASC, agent ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				taskID string
				agent  string
				role   string
			)
			if err := rows.Scan(&taskID, &agent, &role); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"role",
				taskID,
				agent,
				role,
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT task_id, current_round, current_stage, stage_duration_seconds, EXTRACT(EPOCH FROM stage_started_at)::BIGINT, EXTRACT(EPOCH FROM stage_deadline)::BIGINT
		 FROM task_debate_state
		 ORDER BY task_id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				taskID            string
				currentRound      int
				currentStage      string
				stageDurationSec  int64
				stageStartedAt    int64
				stageDeadline     int64
			)
			if err := rows.Scan(&taskID, &currentRound, &currentStage, &stageDurationSec, &stageStartedAt, &stageDeadline); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"debate_state",
				taskID,
				strconv.Itoa(currentRound),
				currentStage,
				strconv.FormatInt(stageDurationSec, 10),
				strconv.FormatInt(stageStartedAt, 10),
				strconv.FormatInt(stageDeadline, 10),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, task_id, agent, value, stake
		 FROM submissions
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id     int64
				taskID string
				agent  string
				value  float64
				stake  float64
			)
			if err := rows.Scan(&id, &taskID, &agent, &value, &stake); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"submission",
				strconv.FormatInt(id, 10),
				taskID,
				agent,
				formatFloat(value),
				formatFloat(stake),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, task_id, agent, round, content
		 FROM task_proposals
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id      int64
				taskID  string
				agent   string
				round   int
				content string
			)
			if err := rows.Scan(&id, &taskID, &agent, &round, &content); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"proposal",
				strconv.FormatInt(id, 10),
				taskID,
				agent,
				strconv.Itoa(round),
				content,
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, task_id, proposal_id, evaluator, round, factual_consistency, redundancy_score, causal_relevance, overall_score
		 FROM task_evaluations
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id                 int64
				taskID             string
				proposalID         int64
				evaluator          string
				round              int
				factualConsistency float64
				redundancyScore    float64
				causalRelevance    float64
				overallScore       float64
			)
			if err := rows.Scan(&id, &taskID, &proposalID, &evaluator, &round, &factualConsistency, &redundancyScore, &causalRelevance, &overallScore); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"evaluation",
				strconv.FormatInt(id, 10),
				taskID,
				strconv.FormatInt(proposalID, 10),
				evaluator,
				strconv.Itoa(round),
				formatFloat(factualConsistency),
				formatFloat(redundancyScore),
				formatFloat(causalRelevance),
				formatFloat(overallScore),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, task_id, proposal_id, voter, round
		 FROM task_votes
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id         int64
				taskID     string
				proposalID int64
				voter      string
				round      int
			)
			if err := rows.Scan(&id, &taskID, &proposalID, &voter, &round); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"vote",
				strconv.FormatInt(id, 10),
				taskID,
				strconv.FormatInt(proposalID, 10),
				voter,
				strconv.Itoa(round),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT id, task_id, agent, round, stage, artifact_type, content_hash, claim_root, semantic_root, COALESCE(parent_type, ''), COALESCE(parent_id, -1)
		 FROM proof_artifacts
		 ORDER BY id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				id           int64
				taskID       string
				agent        string
				round        int
				stage        string
				artifactType string
				contentHash  string
				claimRoot    string
				semanticRoot string
				parentType   string
				parentID     int64
			)
			if err := rows.Scan(&id, &taskID, &agent, &round, &stage, &artifactType, &contentHash, &claimRoot, &semanticRoot, &parentType, &parentID); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"proof",
				strconv.FormatInt(id, 10),
				taskID,
				agent,
				strconv.Itoa(round),
				stage,
				artifactType,
				contentHash,
				claimRoot,
				semanticRoot,
				parentType,
				strconv.FormatInt(parentID, 10),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	if err := appendStateRows(ctx, tx, &parts,
		`SELECT task_id, COALESCE(final_value, -1), COALESCE(outcome, -1), COALESCE(winning_proposal_id, -1), COALESCE(winning_agent, ''), settled
		 FROM task_results
		 ORDER BY task_id ASC`,
		func(rows *sql.Rows) (string, error) {
			var (
				taskID     string
				finalValue float64
				outcome    float64
				winningProposalID int64
				winningAgent string
				settled    bool
			)
			if err := rows.Scan(&taskID, &finalValue, &outcome, &winningProposalID, &winningAgent, &settled); err != nil {
				return "", err
			}
			return strings.Join([]string{
				"result",
				taskID,
				formatFloat(finalValue),
				formatFloat(outcome),
				strconv.FormatInt(winningProposalID, 10),
				winningAgent,
				strconv.FormatBool(settled),
			}, "|"), nil
		},
	); err != nil {
		return "", err
	}

	return protocol.HashStrings(parts), nil
}

func appendStateRows(ctx context.Context, tx *sql.Tx, parts *[]string, query string, encoder func(*sql.Rows) (string, error)) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query state rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		encoded, err := encoder(rows)
		if err != nil {
			return fmt.Errorf("encode state row: %w", err)
		}
		*parts = append(*parts, encoded)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate state rows: %w", err)
	}

	return nil
}
