package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"aichain/internal/execution"
	"aichain/internal/protocol"
)

func initializeDebateStateTx(ctx context.Context, tx *sql.Tx, task protocol.Task, nowUnix int64) error {
	stageDuration := execution.ComputeStageDurationSeconds(nowUnix, task.Input.Deadline, task.Input.DebateRounds)
	stageStartedAt := time.Unix(nowUnix, 0).UTC()
	stageDeadline := time.Unix(execution.ClampStageDeadline(nowUnix, stageDuration, task.Input.Deadline), 0).UTC()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_debate_state (task_id, current_round, current_stage, stage_duration_seconds, stage_started_at, stage_deadline, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())`,
		task.ID,
		1,
		protocol.DebateStageProposal,
		stageDuration,
		stageStartedAt,
		stageDeadline,
	); err != nil {
		return fmt.Errorf("insert debate state: %w", err)
	}

	return nil
}

func advanceDebateStagesTx(ctx context.Context, tx *sql.Tx, nowUnix int64) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT ds.task_id
		 FROM task_debate_state ds
		 INNER JOIN tasks t ON t.id = ds.task_id
		 WHERE t.status = $1
		 ORDER BY ds.task_id ASC
		 FOR UPDATE OF ds`,
		protocol.StatusOpen,
	)
	if err != nil {
		return nil, fmt.Errorf("query debate states: %w", err)
	}
	defer rows.Close()

	taskIDs := make([]string, 0)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("scan debate state task id: %w", err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate debate states: %w", err)
	}

	events := make([]protocol.Event, 0)
	for _, taskID := range taskIDs {
		task, err := getTaskForUpdate(ctx, tx, taskID)
		if err != nil {
			return nil, err
		}
		if task.Type != protocol.TaskTypeBlockAgents || task.Status != protocol.StatusOpen {
			continue
		}

		state, err := getDebateStateForUpdateTx(ctx, tx, taskID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}

		for state.CurrentStage != protocol.DebateStageComplete && state.StageDeadline.Unix() <= nowUnix {
			nextRound, nextStage, terminal := execution.NextDebateState(state.CurrentRound, state.CurrentStage, task.Input.DebateRounds)
			state.CurrentRound = nextRound
			state.CurrentStage = nextStage
			state.StageStartedAt = time.Unix(nowUnix, 0).UTC()
			state.StageDeadline = time.Unix(execution.ClampStageDeadline(nowUnix, state.StageDurationSec, task.Input.Deadline), 0).UTC()
			if terminal {
				state.StageDeadline = time.Unix(task.Input.Deadline, 0).UTC()
			}

			if _, err := tx.ExecContext(
				ctx,
				`UPDATE task_debate_state
				 SET current_round = $2,
				     current_stage = $3,
				     stage_started_at = $4,
				     stage_deadline = $5,
				     updated_at = NOW()
				 WHERE task_id = $1`,
				taskID,
				state.CurrentRound,
				state.CurrentStage,
				state.StageStartedAt,
				state.StageDeadline,
			); err != nil {
				return nil, fmt.Errorf("advance debate stage: %w", err)
			}

			events = append(events, protocol.Event{
				Type: "debate.stage_advanced",
				Attributes: map[string]string{
					"task_id":       taskID,
					"current_round": fmt.Sprintf("%d", state.CurrentRound),
					"current_stage": state.CurrentStage,
				},
			})

			if terminal {
				break
			}
		}
	}

	return events, nil
}

func getDebateStateForUpdateTx(ctx context.Context, tx *sql.Tx, taskID string) (protocol.DebateState, error) {
	var state protocol.DebateState
	if err := tx.QueryRowContext(
		ctx,
		`SELECT task_id, current_round, current_stage, stage_duration_seconds, stage_started_at, stage_deadline, updated_at
		 FROM task_debate_state
		 WHERE task_id = $1
		 FOR UPDATE`,
		taskID,
	).Scan(&state.TaskID, &state.CurrentRound, &state.CurrentStage, &state.StageDurationSec, &state.StageStartedAt, &state.StageDeadline, &state.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.DebateState{}, ErrNotFound
		}
		return protocol.DebateState{}, fmt.Errorf("lock debate state: %w", err)
	}
	return state, nil
}

func requireDebateStageTx(ctx context.Context, tx *sql.Tx, task protocol.Task, expectedRound int, expectedStage string) (protocol.DebateState, error) {
	state, err := getDebateStateForUpdateTx(ctx, tx, task.ID)
	if err != nil {
		return protocol.DebateState{}, err
	}
	if state.CurrentStage != expectedStage {
		return protocol.DebateState{}, fmt.Errorf("%w: current debate stage is %s", ErrValidation, state.CurrentStage)
	}
	if state.CurrentRound != expectedRound {
		return protocol.DebateState{}, fmt.Errorf("%w: current debate round is %d", ErrValidation, state.CurrentRound)
	}
	return state, nil
}

func hasProofForStageTx(ctx context.Context, tx *sql.Tx, taskID string, agent string, round int, stage string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM proof_artifacts
		 WHERE task_id = $1 AND agent = $2 AND round = $3 AND stage = $4`,
		taskID,
		agent,
		round,
		stage,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("count proofs for stage: %w", err)
	}
	return count > 0, nil
}

func (s *Store) maybeAdvanceDebateStateTx(ctx context.Context, tx *sql.Tx, task protocol.Task, nowUnix int64) ([]protocol.Event, error) {
	state, err := getDebateStateForUpdateTx(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	if state.CurrentStage == protocol.DebateStageComplete {
		return nil, nil
	}
	if !s.shouldAdvanceDebateStageTx(ctx, tx, task, state) {
		return nil, nil
	}

	nextRound, nextStage, terminal := execution.NextDebateState(state.CurrentRound, state.CurrentStage, task.Input.DebateRounds)
	nextStartedAt := time.Unix(nowUnix, 0).UTC()
	nextDeadline := time.Unix(execution.ClampStageDeadline(nowUnix, state.StageDurationSec, task.Input.Deadline), 0).UTC()
	if terminal {
		nextDeadline = time.Unix(task.Input.Deadline, 0).UTC()
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE task_debate_state
		 SET current_round = $2,
		     current_stage = $3,
		     stage_started_at = $4,
		     stage_deadline = $5,
		     updated_at = NOW()
		 WHERE task_id = $1`,
		task.ID,
		nextRound,
		nextStage,
		nextStartedAt,
		nextDeadline,
	); err != nil {
		return nil, fmt.Errorf("early advance debate stage: %w", err)
	}

	return []protocol.Event{
		{
			Type: "debate.stage_advanced",
			Attributes: map[string]string{
				"task_id":       task.ID,
				"current_round": fmt.Sprintf("%d", nextRound),
				"current_stage": nextStage,
				"advance_mode":  "policy",
			},
		},
	}, nil
}

func (s *Store) shouldAdvanceDebateStageTx(ctx context.Context, tx *sql.Tx, task protocol.Task, state protocol.DebateState) bool {
	switch state.CurrentStage {
	case protocol.DebateStageProposal:
		count, err := countRoundProposalsTx(ctx, tx, task.ID, state.CurrentRound)
		if err != nil {
			return false
		}
		required := task.Input.WorkerCount
		if required < 1 {
			required = 1
		}
		return count >= required
	case protocol.DebateStageEvaluation:
		ready, err := hasMinimumProposalEvaluationsTx(ctx, tx, task.ID, state.CurrentRound, effectiveMinEvaluationsPerProposalTx(ctx, tx, s.cfg))
		if err != nil {
			return false
		}
		return ready
	case protocol.DebateStageRebuttal:
		count, err := countRoundRebuttalsTx(ctx, tx, task.ID, state.CurrentRound)
		if err != nil {
			return false
		}
		required := task.Input.WorkerCount
		if required < 1 {
			required = 1
		}
		return count >= required
	case protocol.DebateStageVote:
		count, err := countRoundVotesTx(ctx, tx, task.ID, state.CurrentRound)
		if err != nil {
			return false
		}
		required := effectiveMinVotesPerRoundTx(ctx, tx, s.cfg)
		if task.Input.MinerCount > 0 && required > task.Input.MinerCount {
			required = task.Input.MinerCount
		}
		if required < 1 {
			required = 1
		}
		return count >= required
	default:
		return false
	}
}

func countRoundProposalsTx(ctx context.Context, tx *sql.Tx, taskID string, round int) (int, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM task_proposals
		 WHERE task_id = $1 AND round = $2`,
		taskID,
		round,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count round proposals: %w", err)
	}
	return count, nil
}

func hasMinimumProposalEvaluationsTx(ctx context.Context, tx *sql.Tx, taskID string, round int, minimum int) (bool, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT p.id, COUNT(e.id)
		 FROM task_proposals p
		 LEFT JOIN task_evaluations e
		   ON e.task_id = p.task_id
		  AND e.proposal_id = p.id
		  AND e.round = p.round
		 WHERE p.task_id = $1 AND p.round = $2
		 GROUP BY p.id
		 ORDER BY p.id ASC`,
		taskID,
		round,
	)
	if err != nil {
		return false, fmt.Errorf("query evaluation coverage: %w", err)
	}
	defer rows.Close()

	seenProposal := false
	for rows.Next() {
		seenProposal = true
		var (
			proposalID int64
			count      int
		)
		if err := rows.Scan(&proposalID, &count); err != nil {
			return false, fmt.Errorf("scan evaluation coverage: %w", err)
		}
		if count < minimum {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate evaluation coverage: %w", err)
	}
	return seenProposal, nil
}

func countRoundVotesTx(ctx context.Context, tx *sql.Tx, taskID string, round int) (int, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(DISTINCT voter)
		 FROM task_votes
		 WHERE task_id = $1 AND round = $2`,
		taskID,
		round,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count round votes: %w", err)
	}
	return count, nil
}

func countRoundRebuttalsTx(ctx context.Context, tx *sql.Tx, taskID string, round int) (int, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1)
		 FROM task_rebuttals
		 WHERE task_id = $1 AND round = $2`,
		taskID,
		round,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count round rebuttals: %w", err)
	}
	return count, nil
}
