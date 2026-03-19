package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aichain/internal/config"
	"aichain/internal/protocol"
)

func (s *Store) executeSubmitGovernanceProposalTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitGovernanceProposalRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_governance_proposal payload: %w", err)
	}
	if pending.Sender != payload.Proposer {
		return nil, fmt.Errorf("%w: sender does not match proposer", ErrValidation)
	}
	payload.ProposalType = strings.TrimSpace(payload.ProposalType)
	payload.Title = strings.TrimSpace(payload.Title)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.TargetAddress = strings.TrimSpace(payload.TargetAddress)
	payload.ParameterName = strings.TrimSpace(payload.ParameterName)
	payload.ParameterValue = strings.TrimSpace(payload.ParameterValue)

	active, err := isActiveValidatorTx(ctx, tx, payload.Proposer)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: proposer must be an active validator", ErrUnauthorized)
	}
	if payload.VotingDeadline <= nowUnix {
		return nil, fmt.Errorf("%w: voting_deadline must be in the future", ErrValidation)
	}
	if err := validateGovernanceProposalPayload(payload); err != nil {
		return nil, err
	}

	var proposalID int64
	if err := tx.QueryRowContext(
		ctx,
		`INSERT INTO governance_proposals (
			proposer, proposal_type, title, description, target_address, amount,
			parameter_name, parameter_value, voting_deadline, status, execution_note
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '')
		RETURNING id`,
		payload.Proposer,
		payload.ProposalType,
		payload.Title,
		payload.Description,
		payload.TargetAddress,
		payload.Amount,
		payload.ParameterName,
		payload.ParameterValue,
		payload.VotingDeadline,
		protocol.GovernanceProposalOpen,
	).Scan(&proposalID); err != nil {
		return nil, fmt.Errorf("insert governance proposal: %w", err)
	}

	return []protocol.Event{
		{
			Type: "governance.proposal_submitted",
			Attributes: map[string]string{
				"proposal_id":     strconv.FormatInt(proposalID, 10),
				"proposer":        payload.Proposer,
				"proposal_type":   payload.ProposalType,
				"voting_deadline": strconv.FormatInt(payload.VotingDeadline, 10),
			},
		},
	}, nil
}

func (s *Store) executeSubmitGovernanceVoteTx(ctx context.Context, tx *sql.Tx, pending pendingTx, nowUnix int64) ([]protocol.Event, error) {
	var payload protocol.SubmitGovernanceVoteRequest
	if err := json.Unmarshal(pending.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode submit_governance_vote payload: %w", err)
	}
	if pending.Sender != payload.Voter {
		return nil, fmt.Errorf("%w: sender does not match voter", ErrValidation)
	}
	payload.Vote = strings.TrimSpace(strings.ToLower(payload.Vote))
	if payload.Vote != "approve" && payload.Vote != "reject" {
		return nil, fmt.Errorf("%w: vote must be approve or reject", ErrValidation)
	}

	active, err := isActiveValidatorTx(ctx, tx, payload.Voter)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: voter must be an active validator", ErrUnauthorized)
	}

	proposal, err := getGovernanceProposalForUpdateTx(ctx, tx, payload.ProposalID)
	if err != nil {
		return nil, err
	}
	if proposal.Status != protocol.GovernanceProposalOpen {
		return nil, fmt.Errorf("%w: governance proposal is not open", ErrValidation)
	}
	if proposal.VotingDeadline <= nowUnix {
		return nil, fmt.Errorf("%w: governance proposal voting window has closed", ErrValidation)
	}

	power, err := activeValidatorPowerForAddressTx(ctx, tx, payload.Voter)
	if err != nil {
		return nil, err
	}
	if power <= 0 {
		return nil, fmt.Errorf("%w: validator voting power must be positive", ErrValidation)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO governance_votes (proposal_id, voter, vote, power)
		 VALUES ($1, $2, $3, $4)`,
		payload.ProposalID,
		payload.Voter,
		payload.Vote,
		power,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: validator already voted on this proposal", ErrValidation)
		}
		return nil, fmt.Errorf("insert governance vote: %w", err)
	}

	return []protocol.Event{
		{
			Type: "governance.vote_submitted",
			Attributes: map[string]string{
				"proposal_id": strconv.FormatInt(payload.ProposalID, 10),
				"voter":       payload.Voter,
				"vote":        payload.Vote,
				"power":       strconv.FormatInt(power, 10),
			},
		},
	}, nil
}

func finalizeGovernanceProposalsTx(ctx context.Context, tx *sql.Tx, cfg config.Config, nowUnix int64) ([]protocol.Event, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id
		 FROM governance_proposals
		 WHERE status = $1 AND voting_deadline <= $2
		 ORDER BY created_at ASC, id ASC`,
		protocol.GovernanceProposalOpen,
		nowUnix,
	)
	if err != nil {
		return nil, fmt.Errorf("query governance proposals for finalization: %w", err)
	}
	defer rows.Close()

	proposalIDs := make([]int64, 0)
	for rows.Next() {
		var proposalID int64
		if err := rows.Scan(&proposalID); err != nil {
			return nil, fmt.Errorf("scan governance proposal id: %w", err)
		}
		proposalIDs = append(proposalIDs, proposalID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance proposal ids: %w", err)
	}

	totalPower, err := totalActiveValidatorPowerTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	threshold := quorumThreshold(totalPower)

	events := make([]protocol.Event, 0)
	for _, proposalID := range proposalIDs {
		proposal, err := getGovernanceProposalForUpdateTx(ctx, tx, proposalID)
		if err != nil {
			return nil, err
		}

		approvePower, rejectPower, err := tallyGovernanceVotesTx(ctx, tx, proposalID)
		if err != nil {
			return nil, err
		}

		status := protocol.GovernanceProposalRejected
		note := "proposal rejected"
		if approvePower >= threshold {
			if execNote, err := executeGovernanceProposalTx(ctx, tx, cfg, proposal); err == nil {
				status = protocol.GovernanceProposalExecuted
				note = execNote
			} else {
				note = err.Error()
			}
		} else if rejectPower >= threshold {
			note = "proposal rejected by validator vote"
		} else {
			note = "proposal expired without supermajority approval"
		}

		if _, err := tx.ExecContext(
			ctx,
			`UPDATE governance_proposals
			 SET status = $2,
			     execution_note = $3,
			     resolved_at = NOW()
			 WHERE id = $1`,
			proposalID,
			status,
			note,
		); err != nil {
			return nil, fmt.Errorf("resolve governance proposal: %w", err)
		}

		events = append(events, protocol.Event{
			Type: "governance.proposal_finalized",
			Attributes: map[string]string{
				"proposal_id":   strconv.FormatInt(proposalID, 10),
				"proposal_type": proposal.ProposalType,
				"status":        status,
				"approve_power": strconv.FormatInt(approvePower, 10),
				"reject_power":  strconv.FormatInt(rejectPower, 10),
			},
		})
	}

	return events, nil
}

func executeGovernanceProposalTx(ctx context.Context, tx *sql.Tx, cfg config.Config, proposal protocol.GovernanceProposal) (string, error) {
	switch proposal.ProposalType {
	case protocol.GovernanceProposalTreasuryTransfer:
		if err := ensureTreasuryAccountTx(ctx, tx, cfg); err != nil {
			return "", err
		}
		balance, err := lockBalanceTx(ctx, tx, cfg.TreasuryAddress)
		if err != nil {
			return "", err
		}
		if balance < proposal.Amount {
			return "", fmt.Errorf("%w: treasury balance is insufficient for transfer", ErrInsufficientBalance)
		}
		if err := ensureAgentExistsTx(ctx, tx, proposal.TargetAddress, cfg.DefaultAgentReputation); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance - $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			proposal.Amount,
			cfg.TreasuryAddress,
		); err != nil {
			return "", fmt.Errorf("debit treasury balance: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET balance = balance + $1,
			     updated_at = NOW()
			 WHERE address = $2`,
			proposal.Amount,
			proposal.TargetAddress,
		); err != nil {
			return "", fmt.Errorf("credit governance treasury transfer recipient: %w", err)
		}
		return fmt.Sprintf("treasury transfer executed to %s", proposal.TargetAddress), nil
	case protocol.GovernanceProposalParameterChange:
		if err := validateGovernanceParameterValue(proposal.ParameterName, proposal.ParameterValue); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO governance_parameters (name, value, updated_at)
			 VALUES ($1, $2, NOW())
			 ON CONFLICT (name) DO UPDATE
			 SET value = EXCLUDED.value,
			     updated_at = NOW()`,
			proposal.ParameterName,
			proposal.ParameterValue,
		); err != nil {
			return "", fmt.Errorf("upsert governance parameter: %w", err)
		}
		return fmt.Sprintf("parameter %s updated", proposal.ParameterName), nil
	default:
		return "", fmt.Errorf("%w: unsupported governance proposal type", ErrValidation)
	}
}

func (s *Store) ListGovernanceParameters(ctx context.Context) ([]protocol.GovernanceParameter, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT name, value, updated_at
		 FROM governance_parameters
		 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query governance parameters: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceParameter, 0)
	for rows.Next() {
		var item protocol.GovernanceParameter
		if err := rows.Scan(&item.Name, &item.Value, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan governance parameter: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance parameters: %w", err)
	}
	return items, nil
}

func (s *Store) ListGovernanceProposals(ctx context.Context, limit int) ([]protocol.GovernanceProposal, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, proposer, proposal_type, title, description, target_address, amount, parameter_name, parameter_value, voting_deadline, status, execution_note, created_at, resolved_at
		 FROM governance_proposals
		 ORDER BY created_at DESC, id DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query governance proposals: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceProposal, 0, limit)
	for rows.Next() {
		item, err := scanGovernanceProposal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance proposals: %w", err)
	}
	return items, nil
}

func (s *Store) ListGovernanceVotes(ctx context.Context, proposalID *int64) ([]protocol.GovernanceVote, error) {
	query := `SELECT id, proposal_id, voter, vote, power, created_at
		FROM governance_votes`
	args := make([]any, 0, 1)
	if proposalID != nil {
		query += ` WHERE proposal_id = $1`
		args = append(args, *proposalID)
	}
	query += ` ORDER BY proposal_id ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query governance votes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceVote, 0)
	for rows.Next() {
		var item protocol.GovernanceVote
		if err := rows.Scan(&item.ID, &item.ProposalID, &item.Voter, &item.Vote, &item.Power, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan governance vote: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance votes: %w", err)
	}
	return items, nil
}

func getGovernanceProposalForUpdateTx(ctx context.Context, tx *sql.Tx, proposalID int64) (protocol.GovernanceProposal, error) {
	item, err := scanGovernanceProposal(tx.QueryRowContext(
		ctx,
		`SELECT id, proposer, proposal_type, title, description, target_address, amount, parameter_name, parameter_value, voting_deadline, status, execution_note, created_at, resolved_at
		 FROM governance_proposals
		 WHERE id = $1
		 FOR UPDATE`,
		proposalID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.GovernanceProposal{}, ErrNotFound
		}
		return protocol.GovernanceProposal{}, err
	}
	return item, nil
}

func tallyGovernanceVotesTx(ctx context.Context, tx *sql.Tx, proposalID int64) (int64, int64, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT vote, power
		 FROM governance_votes
		 WHERE proposal_id = $1`,
		proposalID,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("query governance votes: %w", err)
	}
	defer rows.Close()

	var approvePower int64
	var rejectPower int64
	for rows.Next() {
		var (
			vote  string
			power int64
		)
		if err := rows.Scan(&vote, &power); err != nil {
			return 0, 0, fmt.Errorf("scan governance vote tally: %w", err)
		}
		if vote == "approve" {
			approvePower += power
		} else {
			rejectPower += power
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate governance votes: %w", err)
	}
	return approvePower, rejectPower, nil
}

func totalActiveValidatorPowerTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	var total int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(SUM(power), 0)
		 FROM validator_registry
		 WHERE active = TRUE`,
	).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum active validator power: %w", err)
	}
	return total, nil
}

func activeValidatorPowerForAddressTx(ctx context.Context, tx *sql.Tx, address string) (int64, error) {
	var power int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT power
		 FROM validator_registry
		 WHERE address = $1 AND active = TRUE`,
		address,
	).Scan(&power); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("query validator power: %w", err)
	}
	return power, nil
}

func quorumThreshold(totalPower int64) int64 {
	return (2*totalPower)/3 + 1
}

func validateGovernanceProposalPayload(payload protocol.SubmitGovernanceProposalRequest) error {
	switch payload.ProposalType {
	case protocol.GovernanceProposalTreasuryTransfer:
		if strings.TrimSpace(payload.TargetAddress) == "" {
			return fmt.Errorf("%w: treasury_transfer requires target_address", ErrValidation)
		}
		if payload.Amount <= 0 {
			return fmt.Errorf("%w: treasury_transfer amount must be > 0", ErrValidation)
		}
	case protocol.GovernanceProposalParameterChange:
		if err := validateGovernanceParameterValue(payload.ParameterName, payload.ParameterValue); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported governance proposal type", ErrValidation)
	}
	return nil
}

func validateGovernanceParameterValue(name string, value string) error {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	switch name {
	case "task_dispute_bond":
		parsed, err := protocol.ParseAmountString(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%w: task_dispute_bond must be a positive decimal amount", ErrValidation)
		}
	case "task_dispute_window_seconds":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%w: task_dispute_window_seconds must be a positive integer", ErrValidation)
		}
	case "min_evaluations_per_proposal", "min_votes_per_round":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("%w: %s must be a positive integer", ErrValidation, name)
		}
	case "role_selection_policy":
		if !isSupportedRoleSelectionPolicy(value) {
			return fmt.Errorf("%w: unsupported role_selection_policy", ErrValidation)
		}
	case "miner_vote_policy":
		if value != "reputation_weighted" && value != "one_agent_one_vote" {
			return fmt.Errorf("%w: unsupported miner_vote_policy", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: unsupported governance parameter %s", ErrValidation, name)
	}
	return nil
}

func governanceParameterValueTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, name string) (string, bool, error) {
	var value string
	if err := querier.QueryRowContext(
		ctx,
		`SELECT value
		 FROM governance_parameters
		 WHERE name = $1`,
		name,
	).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query governance parameter %s: %w", name, err)
	}
	return value, true, nil
}

func effectiveTaskDisputeBondTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) protocol.Amount {
	value, ok, err := governanceParameterValueTx(ctx, querier, "task_dispute_bond")
	if err != nil || !ok {
		return cfg.TaskDisputeBond
	}
	parsed, err := protocol.ParseAmountString(value)
	if err != nil || parsed <= 0 {
		return cfg.TaskDisputeBond
	}
	return parsed
}

func effectiveTaskDisputeWindowTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) time.Duration {
	value, ok, err := governanceParameterValueTx(ctx, querier, "task_dispute_window_seconds")
	if err != nil || !ok {
		return cfg.TaskDisputeWindow
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return cfg.TaskDisputeWindow
	}
	return time.Duration(parsed) * time.Second
}

func effectiveMinEvaluationsPerProposalTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) int {
	value, ok, err := governanceParameterValueTx(ctx, querier, "min_evaluations_per_proposal")
	if err != nil || !ok {
		return cfg.MinEvaluationsPerProposal
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return cfg.MinEvaluationsPerProposal
	}
	return parsed
}

func effectiveMinVotesPerRoundTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) int {
	value, ok, err := governanceParameterValueTx(ctx, querier, "min_votes_per_round")
	if err != nil || !ok {
		return cfg.MinVotesPerRound
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return cfg.MinVotesPerRound
	}
	return parsed
}

func effectiveRoleSelectionPolicyTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) string {
	value, ok, err := governanceParameterValueTx(ctx, querier, "role_selection_policy")
	if err != nil || !ok || !isSupportedRoleSelectionPolicy(value) {
		return cfg.RoleSelectionPolicy
	}
	return value
}

func effectiveMinerVotePolicyTx(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cfg config.Config) string {
	value, ok, err := governanceParameterValueTx(ctx, querier, "miner_vote_policy")
	if err != nil || !ok {
		return cfg.MinerVotePolicy
	}
	if value != "reputation_weighted" && value != "one_agent_one_vote" {
		return cfg.MinerVotePolicy
	}
	return value
}

func scanGovernanceProposal(scanner rowScanner) (protocol.GovernanceProposal, error) {
	var (
		item       protocol.GovernanceProposal
		resolvedAt sql.NullTime
	)
	if err := scanner.Scan(
		&item.ID,
		&item.Proposer,
		&item.ProposalType,
		&item.Title,
		&item.Description,
		&item.TargetAddress,
		&item.Amount,
		&item.ParameterName,
		&item.ParameterValue,
		&item.VotingDeadline,
		&item.Status,
		&item.ExecutionNote,
		&item.CreatedAt,
		&resolvedAt,
	); err != nil {
		return protocol.GovernanceProposal{}, fmt.Errorf("scan governance proposal: %w", err)
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		item.ResolvedAt = &value
	}
	return item, nil
}

func listAllGovernanceParametersTx(ctx context.Context, tx *sql.Tx) ([]protocol.GovernanceParameter, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT name, value, updated_at
		 FROM governance_parameters
		 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot governance parameters: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceParameter, 0)
	for rows.Next() {
		var item protocol.GovernanceParameter
		if err := rows.Scan(&item.Name, &item.Value, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot governance parameter: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot governance parameters: %w", err)
	}
	return items, nil
}

func listAllGovernanceProposalsTx(ctx context.Context, tx *sql.Tx) ([]protocol.GovernanceProposal, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, proposer, proposal_type, title, description, target_address, amount, parameter_name, parameter_value, voting_deadline, status, execution_note, created_at, resolved_at
		 FROM governance_proposals
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot governance proposals: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceProposal, 0)
	for rows.Next() {
		item, err := scanGovernanceProposal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot governance proposals: %w", err)
	}
	return items, nil
}

func listAllGovernanceVotesTx(ctx context.Context, tx *sql.Tx) ([]protocol.GovernanceVote, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, proposal_id, voter, vote, power, created_at
		 FROM governance_votes
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query snapshot governance votes: %w", err)
	}
	defer rows.Close()

	items := make([]protocol.GovernanceVote, 0)
	for rows.Next() {
		var item protocol.GovernanceVote
		if err := rows.Scan(&item.ID, &item.ProposalID, &item.Voter, &item.Vote, &item.Power, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot governance vote: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshot governance votes: %w", err)
	}
	return items, nil
}

func importGovernanceParametersTx(ctx context.Context, tx *sql.Tx, items []protocol.GovernanceParameter) error {
	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO governance_parameters (name, value, updated_at)
			 VALUES ($1, $2, $3)`,
			item.Name,
			item.Value,
			item.UpdatedAt,
		); err != nil {
			return fmt.Errorf("import governance parameter %s: %w", item.Name, err)
		}
	}
	return nil
}

func importGovernanceProposalsTx(ctx context.Context, tx *sql.Tx, items []protocol.GovernanceProposal) error {
	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO governance_proposals (id, proposer, proposal_type, title, description, target_address, amount, parameter_name, parameter_value, voting_deadline, status, execution_note, created_at, resolved_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			item.ID,
			item.Proposer,
			item.ProposalType,
			item.Title,
			item.Description,
			item.TargetAddress,
			item.Amount,
			item.ParameterName,
			item.ParameterValue,
			item.VotingDeadline,
			item.Status,
			item.ExecutionNote,
			item.CreatedAt,
			nullableTime(item.ResolvedAt),
		); err != nil {
			return fmt.Errorf("import governance proposal %d: %w", item.ID, err)
		}
	}
	return nil
}

func importGovernanceVotesTx(ctx context.Context, tx *sql.Tx, items []protocol.GovernanceVote) error {
	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO governance_votes (id, proposal_id, voter, vote, power, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			item.ID,
			item.ProposalID,
			item.Voter,
			item.Vote,
			item.Power,
			item.CreatedAt,
		); err != nil {
			return fmt.Errorf("import governance vote %d: %w", item.ID, err)
		}
	}
	return nil
}
