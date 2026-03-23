package operator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"aichain/internal/protocol"
)

type kvRow struct {
	Key   string
	Value string
}

func renderInitChainReport(w io.Writer, report InitChainReport) {
	renderSection(w, "Chain Ready", []kvRow{
		{Key: "status", Value: report.Status},
		{Key: "chain_id", Value: report.Chain.ChainID},
		{Key: "node_id", Value: report.Chain.NodeID},
		{Key: "head_height", Value: strconv.FormatInt(report.Chain.HeadHeight, 10)},
		{Key: "head_hash", Value: report.Chain.HeadHash},
		{Key: "genesis_hash", Value: report.Chain.GenesisHash},
		{Key: "schema_version", Value: strconv.Itoa(report.Chain.SchemaVersion)},
	})
	renderSection(w, "Genesis", []kvRow{
		{Key: "genesis_time", Value: formatTime(report.Genesis.GenesisTime)},
		{Key: "faucet_address", Value: report.Genesis.FaucetAddress},
		{Key: "accounts", Value: strconv.Itoa(len(report.Genesis.Accounts))},
		{Key: "validators", Value: strconv.Itoa(len(report.Genesis.Validators))},
		{Key: "genesis_file", Value: valueOrFallback(report.GenesisFile, "embedded default")},
	})
	renderSection(w, "Runtime", []kvRow{
		{Key: "database_url", Value: report.DatabaseURL},
		{Key: "block_interval", Value: formatBlockInterval(report.Chain.BlockIntervalSeconds)},
		{Key: "max_transactions_per_block", Value: strconv.Itoa(report.Chain.MaxTransactionsPerBlock)},
		{Key: "faucet_enabled", Value: formatBool(report.Chain.FaucetEnabled)},
	})
}

func renderChainInfo(w io.Writer, info protocol.ChainInfo) {
	renderSection(w, "Chain", []kvRow{
		{Key: "chain_id", Value: info.ChainID},
		{Key: "node_id", Value: info.NodeID},
		{Key: "head_height", Value: strconv.FormatInt(info.HeadHeight, 10)},
		{Key: "head_hash", Value: info.HeadHash},
		{Key: "genesis_hash", Value: info.GenesisHash},
		{Key: "schema_version", Value: strconv.Itoa(info.SchemaVersion)},
	})
	renderSection(w, "Runtime", []kvRow{
		{Key: "block_interval", Value: formatBlockInterval(info.BlockIntervalSeconds)},
		{Key: "max_transactions_per_block", Value: strconv.Itoa(info.MaxTransactionsPerBlock)},
		{Key: "faucet_enabled", Value: formatBool(info.FaucetEnabled)},
		{Key: "role_selection_policy", Value: valueOrFallback(info.RoleSelectionPolicy, "-")},
		{Key: "miner_vote_policy", Value: valueOrFallback(info.MinerVotePolicy, "-")},
		{Key: "reorg_policy", Value: valueOrFallback(info.ReorgPolicy, "-")},
	})
}

func renderBlock(w io.Writer, block protocol.Block) {
	renderSection(w, "Block", []kvRow{
		{Key: "height", Value: strconv.FormatInt(block.Header.Height, 10)},
		{Key: "hash", Value: block.Hash},
		{Key: "parent_hash", Value: block.Header.ParentHash},
		{Key: "chain_id", Value: block.Header.ChainID},
		{Key: "proposer", Value: block.Header.Proposer},
		{Key: "timestamp", Value: formatTime(block.Header.Timestamp)},
		{Key: "tx_count", Value: strconv.Itoa(block.Header.TxCount)},
		{Key: "events", Value: strconv.Itoa(len(block.Events))},
	})
	renderSection(w, "Roots", []kvRow{
		{Key: "tx_root", Value: block.Header.TxRoot},
		{Key: "state_root", Value: block.Header.StateRoot},
		{Key: "app_hash", Value: block.Header.AppHash},
	})

	rows := make([][]string, 0, len(block.Transactions))
	for index, tx := range block.Transactions {
		result := "ok"
		if index < len(block.Receipts) && !block.Receipts[index].Success {
			if block.Receipts[index].ErrorCode != "" {
				result = block.Receipts[index].ErrorCode
			} else {
				result = "failed"
			}
		}
		rows = append(rows, []string{
			strconv.Itoa(index),
			shortID(tx.Hash, 14),
			string(tx.Type),
			tx.Sender,
			strconv.FormatInt(tx.Nonce, 10),
			result,
		})
	}
	renderTable(w, "Transactions", []string{"#", "hash", "type", "sender", "nonce", "result"}, rows)

	eventRows := make([][]string, 0, len(block.Events))
	for _, event := range block.Events {
		eventRows = append(eventRows, []string{event.Type, formatAttributes(event.Attributes)})
	}
	renderTable(w, "Events", []string{"type", "attributes"}, eventRows)
}

func renderTransactionStatus(w io.Writer, status protocol.TransactionStatus) {
	rows := []kvRow{
		{Key: "hash", Value: status.Transaction.Hash},
		{Key: "type", Value: string(status.Transaction.Type)},
		{Key: "sender", Value: status.Transaction.Sender},
		{Key: "nonce", Value: strconv.FormatInt(status.Transaction.Nonce, 10)},
		{Key: "status", Value: status.Status},
		{Key: "accepted_at", Value: formatTime(status.Transaction.AcceptedAt)},
	}
	if status.BlockHeight != nil {
		rows = append(rows, kvRow{Key: "block_height", Value: strconv.FormatInt(*status.BlockHeight, 10)})
	}
	if status.ErrorCode != "" {
		rows = append(rows, kvRow{Key: "error_code", Value: status.ErrorCode})
	}
	if status.Error != "" {
		rows = append(rows, kvRow{Key: "error", Value: status.Error})
	}
	renderSection(w, "Transaction", rows)

	if status.Receipt != nil {
		renderSection(w, "Receipt", []kvRow{
			{Key: "success", Value: formatBool(status.Receipt.Success)},
			{Key: "block_height", Value: strconv.FormatInt(status.Receipt.BlockHeight, 10)},
			{Key: "error_code", Value: valueOrFallback(status.Receipt.ErrorCode, "-")},
			{Key: "error", Value: valueOrFallback(status.Receipt.Error, "-")},
		})

		eventRows := make([][]string, 0, len(status.Receipt.Events))
		for _, event := range status.Receipt.Events {
			eventRows = append(eventRows, []string{event.Type, formatAttributes(event.Attributes)})
		}
		renderTable(w, "Receipt Events", []string{"type", "attributes"}, eventRows)
	}

	payload, err := indentJSON(status.Transaction.Payload)
	if err == nil && strings.TrimSpace(payload) != "" {
		fmt.Fprintln(w, "Payload")
		fmt.Fprintln(w, payload)
	}
}

func renderTaskDetails(w io.Writer, details protocol.TaskDetails) {
	task := details.Task
	renderSection(w, "Task", []kvRow{
		{Key: "id", Value: task.ID},
		{Key: "type", Value: task.Type},
		{Key: "status", Value: task.Status},
		{Key: "creator", Value: task.Creator},
		{Key: "deadline", Value: formatUnix(task.Input.Deadline)},
		{Key: "reward_pool", Value: task.RewardPool.String()},
		{Key: "min_stake", Value: task.MinStake.String()},
		{Key: "debate_rounds", Value: strconv.Itoa(task.Input.DebateRounds)},
		{Key: "worker_count", Value: strconv.Itoa(task.Input.WorkerCount)},
		{Key: "miner_count", Value: strconv.Itoa(task.Input.MinerCount)},
		{Key: "question", Value: task.Input.Question},
	})

	if details.DebateState != nil {
		renderSection(w, "Debate", []kvRow{
			{Key: "current_round", Value: strconv.Itoa(details.DebateState.CurrentRound)},
			{Key: "current_stage", Value: details.DebateState.CurrentStage},
			{Key: "stage_duration", Value: formatDuration(details.DebateState.StageDurationSec)},
			{Key: "stage_started_at", Value: formatTime(details.DebateState.StageStartedAt)},
			{Key: "stage_deadline", Value: formatTime(details.DebateState.StageDeadline)},
		})
	}

	if details.CurrentConsensus != nil {
		renderSection(w, "Consensus", []kvRow{
			{Key: "current_consensus", Value: formatScore(*details.CurrentConsensus)},
		})
	}

	if details.FinalResult != nil {
		renderSection(w, "Result", []kvRow{
			{Key: "settled", Value: formatBool(details.FinalResult.Settled)},
			{Key: "winning_agent", Value: valueOrFallback(derefString(details.FinalResult.WinningAgent), "-")},
			{Key: "winning_proposal_id", Value: valueOrFallback(derefInt64(details.FinalResult.WinningProposalID), "-")},
			{Key: "final_value", Value: valueOrFallback(derefFloat(details.FinalResult.FinalValue), "-")},
			{Key: "outcome", Value: valueOrFallback(derefFloat(details.FinalResult.Outcome), "-")},
			{Key: "settled_at", Value: valueOrFallback(derefTime(details.FinalResult.SettledAt), "-")},
		})
	}

	assignmentRows := make([][]string, 0, len(details.Assignments))
	for _, assignment := range details.Assignments {
		assignmentRows = append(assignmentRows, []string{
			assignment.Agent,
			assignment.Role,
			formatTime(assignment.AssignedAt),
		})
	}
	renderTable(w, "Assignments", []string{"agent", "role", "assigned_at"}, assignmentRows)

	proposalRows := make([][]string, 0, len(details.Proposals))
	for _, proposal := range details.Proposals {
		proposalRows = append(proposalRows, []string{
			strconv.FormatInt(proposal.ID, 10),
			strconv.Itoa(proposal.Round),
			proposal.Agent,
			shortText(proposal.Content, 88),
			formatTime(proposal.CreatedAt),
		})
	}
	renderTable(w, "Proposals", []string{"id", "round", "agent", "content", "created_at"}, proposalRows)

	evaluationRows := make([][]string, 0, len(details.Evaluations))
	for _, evaluation := range details.Evaluations {
		evaluationRows = append(evaluationRows, []string{
			strconv.FormatInt(evaluation.ID, 10),
			strconv.Itoa(evaluation.Round),
			evaluation.Evaluator,
			strconv.FormatInt(evaluation.ProposalID, 10),
			formatScore(evaluation.Metrics.OverallScore),
			shortText(evaluation.Comments, 56),
		})
	}
	renderTable(w, "Evaluations", []string{"id", "round", "evaluator", "proposal", "overall", "comments"}, evaluationRows)

	rebuttalRows := make([][]string, 0, len(details.Rebuttals))
	for _, rebuttal := range details.Rebuttals {
		rebuttalRows = append(rebuttalRows, []string{
			strconv.FormatInt(rebuttal.ID, 10),
			strconv.Itoa(rebuttal.Round),
			rebuttal.Agent,
			strconv.FormatInt(rebuttal.ProposalID, 10),
			shortText(rebuttal.Content, 88),
		})
	}
	renderTable(w, "Rebuttals", []string{"id", "round", "agent", "proposal", "content"}, rebuttalRows)

	voteRows := make([][]string, 0, len(details.Votes))
	for _, vote := range details.Votes {
		voteRows = append(voteRows, []string{
			strconv.FormatInt(vote.ID, 10),
			strconv.Itoa(vote.Round),
			vote.Voter,
			strconv.FormatInt(vote.ProposalID, 10),
			shortText(vote.Reason, 56),
		})
	}
	renderTable(w, "Votes", []string{"id", "round", "voter", "proposal", "reason"}, voteRows)

	proofRows := make([][]string, 0, len(details.Proofs))
	for _, proof := range details.Proofs {
		proofRows = append(proofRows, []string{
			strconv.FormatInt(proof.ID, 10),
			strconv.Itoa(proof.Round),
			proof.Stage,
			proof.Agent,
			proof.ArtifactType,
			shortID(proof.ContentHash, 14),
		})
	}
	renderTable(w, "Proofs", []string{"id", "round", "stage", "agent", "type", "content_hash"}, proofRows)

	disputeRows := make([][]string, 0, len(details.Disputes))
	for _, dispute := range details.Disputes {
		disputeRows = append(disputeRows, []string{
			strconv.FormatInt(dispute.ID, 10),
			dispute.Status,
			dispute.Challenger,
			dispute.Bond.String(),
			valueOrFallback(dispute.Resolver, "-"),
			formatTime(dispute.OpenedAt),
		})
	}
	renderTable(w, "Disputes", []string{"id", "status", "challenger", "bond", "resolver", "opened_at"}, disputeRows)

	oracleRows := make([][]string, 0, len(details.OracleReports))
	for _, report := range details.OracleReports {
		oracleRows = append(oracleRows, []string{
			strconv.FormatInt(report.ID, 10),
			report.Source,
			formatScore(report.Value),
			formatTime(report.ObservedAt),
			shortID(report.RawHash, 14),
		})
	}
	renderTable(w, "Oracle Reports", []string{"id", "source", "value", "observed_at", "raw_hash"}, oracleRows)
}

func renderAgent(w io.Writer, agent protocol.Agent) {
	renderSection(w, "Agent", []kvRow{
		{Key: "address", Value: agent.Address},
		{Key: "public_key", Value: valueOrFallback(agent.PublicKey, "-")},
		{Key: "next_nonce", Value: strconv.FormatInt(agent.NextNonce, 10)},
		{Key: "balance", Value: agent.Balance.String()},
		{Key: "reputation", Value: formatScore(agent.Reputation)},
		{Key: "created_at", Value: formatTime(agent.CreatedAt)},
		{Key: "updated_at", Value: formatTime(agent.UpdatedAt)},
	})
}

func renderPeers(w io.Writer, peers []protocol.PeerStatus) {
	rows := make([][]string, 0, len(peers))
	for _, peer := range peers {
		rows = append(rows, []string{
			peer.NodeID,
			valueOrFallback(peer.ValidatorAddress, "-"),
			strconv.FormatInt(peer.HeadHeight, 10),
			shortID(peer.HeadHash, 14),
			peer.ListenAddr,
			formatTime(peer.ObservedAt),
		})
	}
	renderTable(w, "Peers", []string{"node", "validator", "height", "head_hash", "listen_addr", "observed_at"}, rows)
}

func renderValidators(w io.Writer, validators []protocol.Validator) {
	rows := make([][]string, 0, len(validators))
	for _, validator := range validators {
		rows = append(rows, []string{
			validator.Address,
			strconv.FormatInt(validator.Power, 10),
			formatBool(validator.Active),
			shortID(validator.PublicKey, 16),
		})
	}
	renderTable(w, "Validators", []string{"address", "power", "active", "public_key"}, rows)
}

func renderOpenTasks(w io.Writer, tasks []protocol.Task) {
	rows := make([][]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []string{
			task.ID,
			task.Type,
			task.Status,
			formatUnix(task.Input.Deadline),
			task.RewardPool.String(),
			shortText(task.Input.Question, 72),
		})
	}
	renderTable(w, "Open Tasks", []string{"id", "type", "status", "deadline", "reward_pool", "question"}, rows)
}

func renderSyncStatus(w io.Writer, status protocol.SyncStatus) {
	renderSection(w, "Sync", []kvRow{
		{Key: "last_mode", Value: valueOrFallback(status.LastMode, "-")},
		{Key: "last_peer", Value: valueOrFallback(status.LastPeer, "-")},
		{Key: "last_fork_height", Value: strconv.FormatInt(status.LastForkHeight, 10)},
		{Key: "last_target_height", Value: strconv.FormatInt(status.LastTargetHeight, 10)},
		{Key: "last_imported_height", Value: strconv.FormatInt(status.LastImportedHeight, 10)},
		{Key: "last_attempt_at", Value: valueOrFallback(derefTime(status.LastAttemptAt), "-")},
		{Key: "last_success_at", Value: valueOrFallback(derefTime(status.LastSuccessAt), "-")},
		{Key: "last_failure_at", Value: valueOrFallback(derefTime(status.LastFailureAt), "-")},
		{Key: "last_error", Value: valueOrFallback(status.LastError, "-")},
	})
}

func renderSection(w io.Writer, title string, rows []kvRow) {
	if len(rows) == 0 {
		return
	}

	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s:\t%s\n", row.Key, row.Value)
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
}

func renderTable(w io.Writer, title string, headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	divider := make([]string, len(headers))
	for index, header := range headers {
		width := len(header)
		if width < 3 {
			width = 3
		}
		divider[index] = strings.Repeat("-", width)
	}
	fmt.Fprintln(tw, strings.Join(divider, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
}

func indentJSON(value any) (string, error) {
	var raw []byte
	switch typed := value.(type) {
	case []byte:
		raw = typed
	case json.RawMessage:
		raw = typed
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return "", err
		}
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return string(raw), nil
	}
	return pretty.String(), nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatUnix(value int64) string {
	if value <= 0 {
		return "-"
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return (time.Duration(seconds) * time.Second).String()
}

func formatBlockInterval(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return formatDuration(seconds)
}

func formatBool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatScore(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func valueOrFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func shortID(value string, width int) string {
	text := strings.TrimSpace(value)
	if text == "" || width <= 0 || len(text) <= width {
		return text
	}
	if width <= 6 {
		return text[:width]
	}
	head := (width - 3) / 2
	tail := width - head - 3
	return text[:head] + "..." + text[len(text)-tail:]
}

func shortText(value string, limit int) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 1 {
		return text[:limit]
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func formatAttributes(attributes map[string]string) string {
	if len(attributes) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(attributes))
	keys := make([]string, 0, len(attributes))
	for key, value := range attributes {
		keys = append(keys, key)
		_ = value
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+attributes[key])
	}
	return strings.Join(parts, ", ")
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func derefFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return formatScore(*value)
}

func derefTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}
