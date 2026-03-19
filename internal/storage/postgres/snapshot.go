package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aichain/internal/protocol"
)

func (s *Store) ExportStateSnapshot(ctx context.Context, window int) (protocol.StateSnapshot, error) {
	if window <= 0 {
		window = s.cfg.SyncLookaheadBlocks
	}

	info, err := s.GetChainInfo(ctx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	headBlock, err := s.GetHeadBlock(ctx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}

	retainedFrom := int64(0)
	certifiedWindow := make([]protocol.CertifiedBlock, 0)
	if info.HeadHeight > 0 {
		retainedFrom = info.HeadHeight - int64(window) + 1
		if retainedFrom < 1 {
			retainedFrom = 1
		}
		certifiedWindow, err = s.ListCertifiedBlocksRange(ctx, retainedFrom, window)
		if err != nil {
			return protocol.StateSnapshot{}, err
		}
	}

	validators, err := s.ListValidatorRegistry(ctx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	forkChoice, err := s.ListForkChoicePreferences(ctx, 4096)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.StateSnapshot{}, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer tx.Rollback()

	agents, err := listAllAgentsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	tasks, err := listAllTasksTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	assignments, err := listAllRoleAssignmentsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	debateStates, err := listAllDebateStatesTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	submissions, err := listAllSubmissionsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	proposals, err := listAllProposalsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	evaluations, err := listAllEvaluationsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	rebuttals, err := listAllRebuttalsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	votes, err := listAllVotesTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	proofs, err := listAllProofsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	results, err := listAllResultsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	disputes, err := listAllDisputesTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	oracleReports, err := listAllOracleReportsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	governanceParameters, err := listAllGovernanceParametersTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	governanceProposals, err := listAllGovernanceProposalsTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	governanceVotes, err := listAllGovernanceVotesTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	evidence, err := listAllConsensusEvidenceTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	roundChanges, err := listAllConsensusRoundChangesTx(ctx, tx)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}

	return protocol.StateSnapshot{
		ChainInfo:          info,
		RetainedFromHeight: retainedFrom,
		HeadBlock:          headBlock,
		CertifiedWindow:    certifiedWindow,
		Validators:         validators,
		ForkChoice:         forkChoice,
		Agents:             agents,
		Tasks:              tasks,
		Assignments:        assignments,
		DebateStates:       debateStates,
		Submissions:        submissions,
		Proposals:          proposals,
		Evaluations:        evaluations,
		Rebuttals:          rebuttals,
		Votes:              votes,
		Proofs:             proofs,
		Results:            results,
		Disputes:           disputes,
		OracleReports:      oracleReports,
		GovernanceParameters: governanceParameters,
		GovernanceProposals: governanceProposals,
		GovernanceVotes:    governanceVotes,
		ConsensusEvidence:  evidence,
		ConsensusRounds:    roundChanges,
		ExportedAt:         time.Now().UTC(),
	}, nil
}

func (s *Store) ImportStateSnapshot(ctx context.Context, snapshot protocol.StateSnapshot) error {
	if snapshot.ChainInfo.ChainID == "" || snapshot.HeadBlock.Header.ChainID == "" {
		return fmt.Errorf("%w: snapshot chain metadata is required", ErrValidation)
	}
	if snapshot.HeadBlock.Hash != protocol.BuildBlockHash(snapshot.HeadBlock.Header) {
		return fmt.Errorf("%w: snapshot head block hash is invalid", ErrValidation)
	}
	if snapshot.HeadBlock.Header.Height != snapshot.ChainInfo.HeadHeight || snapshot.HeadBlock.Hash != snapshot.ChainInfo.HeadHash {
		return fmt.Errorf("%w: snapshot head metadata does not match head block", ErrValidation)
	}
	if snapshot.HeadBlock.Header.Height > 0 {
		if len(snapshot.CertifiedWindow) == 0 {
			return fmt.Errorf("%w: non-genesis snapshot requires a certified window", ErrValidation)
		}
		if err := validateCertifiedBranch(snapshot.CertifiedWindow); err != nil {
			return err
		}
		if snapshot.RetainedFromHeight != 0 && snapshot.CertifiedWindow[0].Block.Header.Height != snapshot.RetainedFromHeight {
			return fmt.Errorf("%w: snapshot retained_from_height does not match the certified window", ErrValidation)
		}
		last := snapshot.CertifiedWindow[len(snapshot.CertifiedWindow)-1].Block
		if last.Hash != snapshot.HeadBlock.Hash {
			return fmt.Errorf("%w: snapshot head block must match the certified window tip", ErrValidation)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin snapshot import transaction: %w", err)
	}
	defer tx.Rollback()

	meta, err := getMetadataForUpdate(ctx, tx)
	if err != nil {
		return err
	}
	if snapshot.ChainInfo.ChainID != meta.ChainID || snapshot.HeadBlock.Header.ChainID != meta.ChainID {
		return fmt.Errorf("%w: snapshot chain_id does not match local chain", ErrValidation)
	}
	if strings.TrimSpace(snapshot.ChainInfo.GenesisHash) == "" {
		return fmt.Errorf("%w: snapshot genesis_hash is required", ErrValidation)
	}
	if snapshot.ChainInfo.GenesisHash != meta.GenesisHash {
		return fmt.Errorf("%w: snapshot genesis_hash does not match local chain", ErrValidation)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM tx_pool`); err != nil {
		return fmt.Errorf("clear tx_pool for snapshot import: %w", err)
	}
	if err := clearConsensusStateTx(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM blocks WHERE height > 0`); err != nil {
		return fmt.Errorf("clear canonical blocks for snapshot import: %w", err)
	}
	if err := clearExecutionStateTx(ctx, tx); err != nil {
		return err
	}

	if err := importValidatorsTx(ctx, tx, snapshot.Validators); err != nil {
		return err
	}
	if err := importAgentsTx(ctx, tx, snapshot.Agents); err != nil {
		return err
	}
	if err := importTasksTx(ctx, tx, snapshot.Tasks); err != nil {
		return err
	}
	if err := importRoleAssignmentsTx(ctx, tx, snapshot.Assignments); err != nil {
		return err
	}
	if err := importDebateStatesTx(ctx, tx, snapshot.DebateStates); err != nil {
		return err
	}
	if err := importSubmissionsTx(ctx, tx, snapshot.Submissions); err != nil {
		return err
	}
	if err := importProposalsTx(ctx, tx, snapshot.Proposals); err != nil {
		return err
	}
	if err := importEvaluationsTx(ctx, tx, snapshot.Evaluations); err != nil {
		return err
	}
	if err := importRebuttalsTx(ctx, tx, snapshot.Rebuttals); err != nil {
		return err
	}
	if err := importVotesTx(ctx, tx, snapshot.Votes); err != nil {
		return err
	}
	if err := importProofsTx(ctx, tx, snapshot.Proofs); err != nil {
		return err
	}
	if err := importResultsTx(ctx, tx, snapshot.Results); err != nil {
		return err
	}
	if err := importDisputesTx(ctx, tx, snapshot.Disputes); err != nil {
		return err
	}
	if err := importOracleReportsTx(ctx, tx, snapshot.OracleReports); err != nil {
		return err
	}
	if err := importGovernanceParametersTx(ctx, tx, snapshot.GovernanceParameters); err != nil {
		return err
	}
	if err := importGovernanceProposalsTx(ctx, tx, snapshot.GovernanceProposals); err != nil {
		return err
	}
	if err := importGovernanceVotesTx(ctx, tx, snapshot.GovernanceVotes); err != nil {
		return err
	}

	for _, bundle := range snapshot.CertifiedWindow {
		if err := insertBlockTx(ctx, tx, bundle.Block); err != nil {
			return err
		}
		if err := upsertCommittedTransactionsTx(ctx, tx, bundle.Block); err != nil {
			return err
		}
		if err := persistConsensusBundleTx(ctx, tx, bundle); err != nil {
			return err
		}
	}
	if err := importConsensusEvidenceTx(ctx, tx, snapshot.ConsensusEvidence); err != nil {
		return err
	}
	if err := importConsensusRoundChangesTx(ctx, tx, snapshot.ConsensusRounds); err != nil {
		return err
	}
	if err := importForkChoicePreferencesTx(ctx, tx, snapshot.ForkChoice); err != nil {
		return err
	}
	if err := updateMetadataHeadTx(ctx, tx, snapshot.HeadBlock.Header.Height, snapshot.HeadBlock.Hash); err != nil {
		return err
	}

	stateRoot, err := computeStateRootTx(ctx, tx)
	if err != nil {
		return err
	}
	if stateRoot != snapshot.HeadBlock.Header.StateRoot {
		return fmt.Errorf("%w: snapshot state root does not match imported state", ErrValidation)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot import transaction: %w", err)
	}
	return nil
}

func clearConsensusStateTx(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`DELETE FROM fork_choice_preferences`,
		`DELETE FROM consensus_round_changes`,
		`DELETE FROM consensus_evidence`,
		`DELETE FROM consensus_votes`,
		`DELETE FROM consensus_certificates`,
		`DELETE FROM consensus_proposals`,
		`DELETE FROM validator_registry`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("clear consensus state: %w", err)
		}
	}
	return nil
}

func clearExecutionStateTx(ctx context.Context, tx *sql.Tx) error {
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
		return fmt.Errorf("clear execution state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents`); err != nil {
		return fmt.Errorf("clear agent state: %w", err)
	}
	return nil
}

func importValidatorsTx(ctx context.Context, tx *sql.Tx, validators []protocol.Validator) error {
	for _, validator := range validators {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO validator_registry (address, public_key, power, active, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, NOW(), NOW())`,
			validator.Address,
			validator.PublicKey,
			validator.Power,
			validator.Active,
		); err != nil {
			return fmt.Errorf("import validator registry: %w", err)
		}
	}
	return nil
}

func importAgentsTx(ctx context.Context, tx *sql.Tx, agents []protocol.Agent) error {
	for _, agent := range agents {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO agents (address, public_key, next_nonce, balance, reputation, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			agent.Address,
			nullIfEmpty(agent.PublicKey),
			agent.NextNonce,
			agent.Balance,
			agent.Reputation,
			agent.CreatedAt,
			agent.UpdatedAt,
		); err != nil {
			return fmt.Errorf("import agent %s: %w", agent.Address, err)
		}
	}
	return nil
}

func importTasksTx(ctx context.Context, tx *sql.Tx, tasks []protocol.Task) error {
	for _, task := range tasks {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO tasks (id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, role_selection_policy, oracle_source, oracle_endpoint, oracle_path, reward_pool, min_stake, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())`,
			task.ID,
			task.Creator,
			task.Type,
			task.Input.Question,
			task.Input.Deadline,
			task.Input.DebateRounds,
			task.Input.WorkerCount,
			task.Input.MinerCount,
			task.Input.RoleSelectionPolicy,
			task.Input.OracleSource,
			task.Input.OracleEndpoint,
			task.Input.OraclePath,
			task.RewardPool,
			task.MinStake,
			task.Status,
			task.CreatedAt,
		); err != nil {
			return fmt.Errorf("import task %s: %w", task.ID, err)
		}
	}
	return nil
}

func importRoleAssignmentsTx(ctx context.Context, tx *sql.Tx, assignments []protocol.RoleAssignment) error {
	for _, assignment := range assignments {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_roles (task_id, agent, role, assigned_at)
			 VALUES ($1, $2, $3, $4)`,
			assignment.TaskID,
			assignment.Agent,
			assignment.Role,
			assignment.AssignedAt,
		); err != nil {
			return fmt.Errorf("import task role %s/%s: %w", assignment.TaskID, assignment.Agent, err)
		}
	}
	return nil
}

func importDebateStatesTx(ctx context.Context, tx *sql.Tx, states []protocol.DebateState) error {
	for _, state := range states {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_debate_state (task_id, current_round, current_stage, stage_duration_seconds, stage_started_at, stage_deadline, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			state.TaskID,
			state.CurrentRound,
			state.CurrentStage,
			state.StageDurationSec,
			state.StageStartedAt,
			state.StageDeadline,
			state.UpdatedAt,
		); err != nil {
			return fmt.Errorf("import debate state %s: %w", state.TaskID, err)
		}
	}
	return nil
}

func importSubmissionsTx(ctx context.Context, tx *sql.Tx, submissions []protocol.Submission) error {
	for _, submission := range submissions {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO submissions (id, task_id, agent, value, stake, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			submission.ID,
			submission.TaskID,
			submission.Agent,
			submission.Value,
			submission.Stake,
			submission.CreatedAt,
		); err != nil {
			return fmt.Errorf("import submission %d: %w", submission.ID, err)
		}
	}
	return nil
}

func importProposalsTx(ctx context.Context, tx *sql.Tx, proposals []protocol.Proposal) error {
	for _, proposal := range proposals {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_proposals (id, task_id, agent, round, content, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			proposal.ID,
			proposal.TaskID,
			proposal.Agent,
			proposal.Round,
			proposal.Content,
			proposal.CreatedAt,
		); err != nil {
			return fmt.Errorf("import proposal %d: %w", proposal.ID, err)
		}
	}
	return nil
}

func importEvaluationsTx(ctx context.Context, tx *sql.Tx, evaluations []protocol.ProposalEvaluation) error {
	for _, evaluation := range evaluations {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_evaluations (id, task_id, proposal_id, evaluator, round, factual_consistency, redundancy_score, causal_relevance, overall_score, comments, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			evaluation.ID,
			evaluation.TaskID,
			evaluation.ProposalID,
			evaluation.Evaluator,
			evaluation.Round,
			evaluation.Metrics.FactualConsistency,
			evaluation.Metrics.RedundancyScore,
			evaluation.Metrics.CausalRelevance,
			evaluation.Metrics.OverallScore,
			evaluation.Comments,
			evaluation.CreatedAt,
		); err != nil {
			return fmt.Errorf("import evaluation %d: %w", evaluation.ID, err)
		}
	}
	return nil
}

func importRebuttalsTx(ctx context.Context, tx *sql.Tx, rebuttals []protocol.Rebuttal) error {
	for _, rebuttal := range rebuttals {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_rebuttals (id, task_id, proposal_id, agent, round, content, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			rebuttal.ID,
			rebuttal.TaskID,
			rebuttal.ProposalID,
			rebuttal.Agent,
			rebuttal.Round,
			rebuttal.Content,
			rebuttal.CreatedAt,
		); err != nil {
			return fmt.Errorf("import rebuttal %d: %w", rebuttal.ID, err)
		}
	}
	return nil
}

func importVotesTx(ctx context.Context, tx *sql.Tx, votes []protocol.ProposalVote) error {
	for _, vote := range votes {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_votes (id, task_id, proposal_id, voter, round, reason, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			vote.ID,
			vote.TaskID,
			vote.ProposalID,
			vote.Voter,
			vote.Round,
			vote.Reason,
			vote.CreatedAt,
		); err != nil {
			return fmt.Errorf("import vote %d: %w", vote.ID, err)
		}
	}
	return nil
}

func importProofsTx(ctx context.Context, tx *sql.Tx, proofs []protocol.ProofOfThought) error {
	for _, proof := range proofs {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO proof_artifacts (id, task_id, agent, round, stage, artifact_type, content, content_hash, claim_root, semantic_root, parent_type, parent_id, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			proof.ID,
			proof.TaskID,
			proof.Agent,
			proof.Round,
			proof.Stage,
			proof.ArtifactType,
			proof.Content,
			proof.ContentHash,
			proof.ClaimRoot,
			proof.SemanticRoot,
			nullIfEmpty(proof.ParentType),
			nullableInt64(proof.ParentID),
			proof.CreatedAt,
		); err != nil {
			return fmt.Errorf("import proof %d: %w", proof.ID, err)
		}
	}
	return nil
}

func importResultsTx(ctx context.Context, tx *sql.Tx, results []protocol.Result) error {
	for _, result := range results {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO task_results (task_id, final_value, outcome, winning_proposal_id, winning_agent, settled, updated_at, settled_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			result.TaskID,
			nullableFloat64(result.FinalValue),
			nullableFloat64(result.Outcome),
			nullableInt64(result.WinningProposalID),
			nullableString(result.WinningAgent),
			result.Settled,
			result.LastUpdatedAt,
			nullableTime(result.SettledAt),
		); err != nil {
			return fmt.Errorf("import result %s: %w", result.TaskID, err)
		}
	}
	return nil
}

func importConsensusEvidenceTx(ctx context.Context, tx *sql.Tx, evidence []protocol.ConsensusEvidence) error {
	for _, item := range evidence {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO consensus_evidence (id, evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash, details, observed_at, processed_at, applied_balance_penalty, applied_reputation_penalty, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())`,
			item.ID,
			item.EvidenceType,
			item.Validator,
			item.Height,
			item.Round,
			item.VoteType,
			item.BlockHash,
			item.ConflictingBlockHash,
			item.Details,
			item.ObservedAt,
			nullableTime(item.ProcessedAt),
			item.AppliedBalancePenalty,
			item.AppliedReputationPenalty,
		); err != nil {
			return fmt.Errorf("import consensus evidence %d: %w", item.ID, err)
		}
	}
	return nil
}

func importConsensusRoundChangesTx(ctx context.Context, tx *sql.Tx, messages []protocol.ConsensusRoundChange) error {
	for _, message := range messages {
		payload, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("marshal round change payload: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO consensus_round_changes (chain_id, height, round, validator, reason, requested_at, signature, payload_json, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())`,
			message.ChainID,
			message.Height,
			message.Round,
			message.Validator,
			message.Reason,
			message.RequestedAt,
			message.Signature,
			payload,
		); err != nil {
			return fmt.Errorf("import consensus round change %d/%d/%s: %w", message.Height, message.Round, message.Validator, err)
		}
	}
	return nil
}

func importForkChoicePreferencesTx(ctx context.Context, tx *sql.Tx, preferences []protocol.ForkChoicePreference) error {
	for _, preference := range preferences {
		if err := persistForkChoicePreferenceTx(ctx, tx, preference.Certificate); err != nil {
			return err
		}
	}
	return nil
}

func listAllAgentsTx(ctx context.Context, tx *sql.Tx) ([]protocol.Agent, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT address, COALESCE(public_key, ''), next_nonce, balance, reputation, created_at, updated_at
		 FROM agents
		 ORDER BY address ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot agents: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Agent, 0)
	for rows.Next() {
		var item protocol.Agent
		if err := rows.Scan(&item.Address, &item.PublicKey, &item.NextNonce, &item.Balance, &item.Reputation, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot agent: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot agents: %w", err)
	}
	return items, nil
}

func listAllTasksTx(ctx context.Context, tx *sql.Tx) ([]protocol.Task, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, creator, type, question, deadline, debate_rounds, worker_count, miner_count, role_selection_policy, oracle_source, oracle_endpoint, oracle_path, reward_pool, min_stake, status, created_at
		 FROM tasks
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot tasks: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot tasks: %w", err)
	}
	return items, nil
}

func listAllRoleAssignmentsTx(ctx context.Context, tx *sql.Tx) ([]protocol.RoleAssignment, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT task_id, agent, role, assigned_at
		 FROM task_roles
		 ORDER BY task_id ASC, role ASC, agent ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot roles: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.RoleAssignment, 0)
	for rows.Next() {
		var item protocol.RoleAssignment
		if err := rows.Scan(&item.TaskID, &item.Agent, &item.Role, &item.AssignedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot role: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot roles: %w", err)
	}
	return items, nil
}

func listAllDebateStatesTx(ctx context.Context, tx *sql.Tx) ([]protocol.DebateState, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT task_id, current_round, current_stage, stage_duration_seconds, stage_started_at, stage_deadline, updated_at
		 FROM task_debate_state
		 ORDER BY task_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot debate states: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.DebateState, 0)
	for rows.Next() {
		var item protocol.DebateState
		if err := rows.Scan(&item.TaskID, &item.CurrentRound, &item.CurrentStage, &item.StageDurationSec, &item.StageStartedAt, &item.StageDeadline, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot debate state: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot debate states: %w", err)
	}
	return items, nil
}

func listAllSubmissionsTx(ctx context.Context, tx *sql.Tx) ([]protocol.Submission, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, agent, value, stake, created_at
		 FROM submissions
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot submissions: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Submission, 0)
	for rows.Next() {
		var item protocol.Submission
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Agent, &item.Value, &item.Stake, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot submission: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot submissions: %w", err)
	}
	return items, nil
}

func listAllProposalsTx(ctx context.Context, tx *sql.Tx) ([]protocol.Proposal, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, agent, round, content, created_at
		 FROM task_proposals
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot proposals: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Proposal, 0)
	for rows.Next() {
		var item protocol.Proposal
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Agent, &item.Round, &item.Content, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot proposal: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot proposals: %w", err)
	}
	return items, nil
}

func listAllEvaluationsTx(ctx context.Context, tx *sql.Tx) ([]protocol.ProposalEvaluation, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, proposal_id, evaluator, round, factual_consistency, redundancy_score, causal_relevance, overall_score, comments, created_at
		 FROM task_evaluations
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot evaluations: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.ProposalEvaluation, 0)
	for rows.Next() {
		var item protocol.ProposalEvaluation
		if err := rows.Scan(&item.ID, &item.TaskID, &item.ProposalID, &item.Evaluator, &item.Round, &item.Metrics.FactualConsistency, &item.Metrics.RedundancyScore, &item.Metrics.CausalRelevance, &item.Metrics.OverallScore, &item.Comments, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot evaluation: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot evaluations: %w", err)
	}
	return items, nil
}

func listAllRebuttalsTx(ctx context.Context, tx *sql.Tx) ([]protocol.Rebuttal, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, proposal_id, agent, round, content, created_at
		 FROM task_rebuttals
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot rebuttals: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Rebuttal, 0)
	for rows.Next() {
		var item protocol.Rebuttal
		if err := rows.Scan(&item.ID, &item.TaskID, &item.ProposalID, &item.Agent, &item.Round, &item.Content, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot rebuttal: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot rebuttals: %w", err)
	}
	return items, nil
}

func listAllVotesTx(ctx context.Context, tx *sql.Tx) ([]protocol.ProposalVote, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, proposal_id, voter, round, reason, created_at
		 FROM task_votes
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot votes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.ProposalVote, 0)
	for rows.Next() {
		var item protocol.ProposalVote
		if err := rows.Scan(&item.ID, &item.TaskID, &item.ProposalID, &item.Voter, &item.Round, &item.Reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot vote: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot votes: %w", err)
	}
	return items, nil
}

func listAllProofsTx(ctx context.Context, tx *sql.Tx) ([]protocol.ProofOfThought, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, task_id, agent, round, stage, artifact_type, content, content_hash, claim_root, semantic_root, COALESCE(parent_type, ''), parent_id, created_at
		 FROM proof_artifacts
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot proofs: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.ProofOfThought, 0)
	for rows.Next() {
		var (
			item     protocol.ProofOfThought
			parentID sql.NullInt64
		)
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Agent, &item.Round, &item.Stage, &item.ArtifactType, &item.Content, &item.ContentHash, &item.ClaimRoot, &item.SemanticRoot, &item.ParentType, &parentID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot proof: %w", err)
		}
		if parentID.Valid {
			value := parentID.Int64
			item.ParentID = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot proofs: %w", err)
	}
	return items, nil
}

func listAllResultsTx(ctx context.Context, tx *sql.Tx) ([]protocol.Result, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT task_id, final_value, outcome, winning_proposal_id, winning_agent, settled, updated_at, settled_at
		 FROM task_results
		 ORDER BY task_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot results: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.Result, 0)
	for rows.Next() {
		var (
			item              protocol.Result
			finalValue        sql.NullFloat64
			outcome           sql.NullFloat64
			winningProposalID sql.NullInt64
			winningAgent      sql.NullString
			settledAt         sql.NullTime
		)
		if err := rows.Scan(&item.TaskID, &finalValue, &outcome, &winningProposalID, &winningAgent, &item.Settled, &item.LastUpdatedAt, &settledAt); err != nil {
			return nil, fmt.Errorf("scan snapshot result: %w", err)
		}
		if finalValue.Valid {
			value := finalValue.Float64
			item.FinalValue = &value
		}
		if outcome.Valid {
			value := outcome.Float64
			item.Outcome = &value
		}
		if winningProposalID.Valid {
			value := winningProposalID.Int64
			item.WinningProposalID = &value
		}
		if winningAgent.Valid {
			value := winningAgent.String
			item.WinningAgent = &value
		}
		if settledAt.Valid {
			value := settledAt.Time
			item.SettledAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot results: %w", err)
	}
	return items, nil
}

func listAllConsensusEvidenceTx(ctx context.Context, tx *sql.Tx) ([]protocol.ConsensusEvidence, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash, details, observed_at, processed_at, applied_balance_penalty, applied_reputation_penalty
		 FROM consensus_evidence
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot consensus evidence: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.ConsensusEvidence, 0)
	for rows.Next() {
		var (
			item        protocol.ConsensusEvidence
			processedAt sql.NullTime
		)
		if err := rows.Scan(&item.ID, &item.EvidenceType, &item.Validator, &item.Height, &item.Round, &item.VoteType, &item.BlockHash, &item.ConflictingBlockHash, &item.Details, &item.ObservedAt, &processedAt, &item.AppliedBalancePenalty, &item.AppliedReputationPenalty); err != nil {
			return nil, fmt.Errorf("scan snapshot consensus evidence: %w", err)
		}
		if processedAt.Valid {
			value := processedAt.Time
			item.ProcessedAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot consensus evidence: %w", err)
	}
	return items, nil
}

func listAllConsensusRoundChangesTx(ctx context.Context, tx *sql.Tx) ([]protocol.ConsensusRoundChange, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT payload_json
		 FROM consensus_round_changes
		 ORDER BY height ASC, round ASC, validator ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot consensus round changes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.ConsensusRoundChange, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan snapshot round change payload: %w", err)
		}
		var item protocol.ConsensusRoundChange
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode snapshot round change payload: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot round changes: %w", err)
	}
	return items, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
