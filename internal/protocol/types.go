package protocol

import (
	"encoding/json"
	"time"
)

const (
	StatusOpen    = "open"
	StatusSettled = "settled"
	StatusDisputed = "disputed"

	TaskTypePrediction       = "prediction"
	TaskTypeOraclePrediction = "oracle_prediction"
	TaskTypeBlockAgents      = "blockagents"

	RoleWorker = "worker"
	RoleMiner  = "miner"

	DebateStageProposal   = "proposal"
	DebateStageEvaluation = "evaluation"
	DebateStageRebuttal   = "rebuttal"
	DebateStageVote       = "vote"
	DebateStageComplete   = "complete"

	GovernanceProposalOpen             = "open"
	GovernanceProposalExecuted         = "executed"
	GovernanceProposalRejected         = "rejected"
	GovernanceProposalTreasuryTransfer = "treasury_transfer"
	GovernanceProposalParameterChange  = "parameter_change"

	ConsensusEvidenceDoubleProposal = "double_proposal"
	ConsensusEvidenceDoubleVote     = "double_vote"

	ReceiptCodeValidation          = "validation_error"
	ReceiptCodeUnauthorized        = "unauthorized"
	ReceiptCodeInvalidNonce        = "invalid_nonce"
	ReceiptCodeInsufficientBalance = "insufficient_balance"
	ReceiptCodeNotFound            = "not_found"
	ReceiptCodeConflict            = "conflict"
	ReceiptCodeInternal            = "internal_error"
)

type TxType string

const (
	TxTypeCreateTask        TxType = "create_task"
	TxTypeSubmitInference   TxType = "submit_inference"
	TxTypeSubmitProposal    TxType = "submit_proposal"
	TxTypeSubmitEvaluation  TxType = "submit_evaluation"
	TxTypeSubmitRebuttal    TxType = "submit_rebuttal"
	TxTypeSubmitVote        TxType = "submit_vote"
	TxTypeSubmitProof       TxType = "submit_proof"
	TxTypeFundAgent         TxType = "fund_agent"
	TxTypeBootstrapAgentKey TxType = "bootstrap_agent_key"
	TxTypeRotateAgentKey    TxType = "rotate_agent_key"
	TxTypeUpsertValidator   TxType = "upsert_validator"
	TxTypeDeactivateValidator TxType = "deactivate_validator"
	TxTypeOpenDispute       TxType = "open_dispute"
	TxTypeResolveDispute    TxType = "resolve_dispute"
	TxTypeSubmitGovernanceProposal TxType = "submit_governance_proposal"
	TxTypeSubmitGovernanceVote     TxType = "submit_governance_vote"
)

type Genesis struct {
	ChainID       string           `json:"chain_id"`
	GenesisTime   time.Time        `json:"genesis_time"`
	FaucetAddress string           `json:"faucet_address"`
	Accounts      []GenesisAccount `json:"accounts"`
	Validators    []GenesisValidator `json:"validators,omitempty"`
}

type GenesisAccount struct {
	Address    string `json:"address"`
	PublicKey  string `json:"public_key,omitempty"`
	Balance    Amount `json:"balance"`
	Reputation float64 `json:"reputation"`
}

type GenesisValidator struct {
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
	Power     int64  `json:"power"`
}

type TxAuth struct {
	Nonce     int64  `json:"nonce"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

type TaskInput struct {
	Question     string `json:"question"`
	Deadline     int64  `json:"deadline"`
	DebateRounds int    `json:"debate_rounds,omitempty"`
	WorkerCount  int    `json:"worker_count,omitempty"`
	MinerCount   int    `json:"miner_count,omitempty"`
	RoleSelectionPolicy string `json:"role_selection_policy,omitempty"`
	OracleSource string `json:"oracle_source,omitempty"`
	OracleEndpoint string `json:"oracle_endpoint,omitempty"`
	OraclePath string `json:"oracle_path,omitempty"`
}

type Task struct {
	ID         string    `json:"id"`
	Creator    string    `json:"creator"`
	Type       string    `json:"type"`
	Input      TaskInput `json:"input"`
	RewardPool Amount    `json:"reward_pool"`
	MinStake   Amount    `json:"min_stake"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Submission struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Agent     string    `json:"agent"`
	Value     float64   `json:"value"`
	Stake     Amount    `json:"stake"`
	CreatedAt time.Time `json:"created_at"`
}

type RoleAssignment struct {
	TaskID     string    `json:"task_id"`
	Agent      string    `json:"agent"`
	Role       string    `json:"role"`
	AssignedAt time.Time `json:"assigned_at"`
}

type Proposal struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Agent     string    `json:"agent"`
	Round     int       `json:"round"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type EvaluationMetrics struct {
	FactualConsistency float64 `json:"factual_consistency"`
	RedundancyScore    float64 `json:"redundancy_score"`
	CausalRelevance    float64 `json:"causal_relevance"`
	OverallScore       float64 `json:"overall_score"`
}

type ProposalEvaluation struct {
	ID         int64             `json:"id"`
	TaskID     string            `json:"task_id"`
	ProposalID int64             `json:"proposal_id"`
	Evaluator  string            `json:"evaluator"`
	Round      int               `json:"round"`
	Metrics    EvaluationMetrics `json:"metrics"`
	Comments   string            `json:"comments,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

type ProposalVote struct {
	ID         int64     `json:"id"`
	TaskID     string    `json:"task_id"`
	ProposalID int64     `json:"proposal_id"`
	Voter      string    `json:"voter"`
	Round      int       `json:"round"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Rebuttal struct {
	ID         int64     `json:"id"`
	TaskID     string    `json:"task_id"`
	ProposalID int64     `json:"proposal_id"`
	Agent      string    `json:"agent"`
	Round      int       `json:"round"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type DebateState struct {
	TaskID           string    `json:"task_id"`
	CurrentRound     int       `json:"current_round"`
	CurrentStage     string    `json:"current_stage"`
	StageDurationSec int64     `json:"stage_duration_sec"`
	StageStartedAt   time.Time `json:"stage_started_at"`
	StageDeadline    time.Time `json:"stage_deadline"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProofOfThought struct {
	ID           int64     `json:"id"`
	TaskID        string    `json:"task_id"`
	Agent         string    `json:"agent"`
	Round         int       `json:"round"`
	Stage         string    `json:"stage"`
	ArtifactType  string    `json:"artifact_type"`
	Content       string    `json:"content"`
	ContentHash   string    `json:"content_hash"`
	ClaimRoot     string    `json:"claim_root"`
	SemanticRoot  string    `json:"semantic_root"`
	ParentType    string    `json:"parent_type,omitempty"`
	ParentID      *int64    `json:"parent_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Validator struct {
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
	Power     int64  `json:"power"`
	Active    bool   `json:"active,omitempty"`
}

type ConsensusProposal struct {
	ChainID        string    `json:"chain_id"`
	Height         int64     `json:"height"`
	Round          int       `json:"round"`
	Proposer       string    `json:"proposer"`
	BlockHash      string    `json:"block_hash"`
	BlockHeight    int64     `json:"block_height"`
	ParentHash     string    `json:"parent_hash"`
	ProposedAt     time.Time `json:"proposed_at"`
	Signature      string    `json:"signature"`
}

type ConsensusVote struct {
	ChainID   string    `json:"chain_id"`
	Height    int64     `json:"height"`
	Round     int       `json:"round"`
	Type      string    `json:"type"`
	Voter     string    `json:"voter"`
	BlockHash string    `json:"block_hash"`
	VotedAt   time.Time `json:"voted_at"`
	Signature string    `json:"signature"`
}

type QuorumCertificate struct {
	ChainID      string          `json:"chain_id"`
	Height       int64           `json:"height"`
	Round        int             `json:"round"`
	BlockHash    string          `json:"block_hash"`
	VoteType     string          `json:"vote_type"`
	Signers      []string        `json:"signers"`
	Power        int64           `json:"power"`
	Threshold    int64           `json:"threshold"`
	CertifiedAt  time.Time       `json:"certified_at"`
}

type CertifiedBlock struct {
	Block       Block               `json:"block"`
	Proposal    ConsensusProposal   `json:"proposal"`
	Votes       []ConsensusVote     `json:"votes"`
	Certificate QuorumCertificate   `json:"certificate"`
}

type ConsensusCandidateBlock struct {
	Block       Block             `json:"block"`
	Proposal    ConsensusProposal `json:"proposal"`
	ObservedAt  time.Time         `json:"observed_at"`
}

type ConsensusEvidence struct {
	ID                  int64     `json:"id"`
	EvidenceType        string    `json:"evidence_type"`
	Validator           string    `json:"validator"`
	Height              int64     `json:"height"`
	Round               int       `json:"round"`
	VoteType            string    `json:"vote_type,omitempty"`
	BlockHash           string    `json:"block_hash"`
	ConflictingBlockHash string   `json:"conflicting_block_hash"`
	ObservedAt          time.Time `json:"observed_at"`
	ProcessedAt         *time.Time `json:"processed_at,omitempty"`
	AppliedBalancePenalty Amount `json:"applied_balance_penalty,omitempty"`
	AppliedReputationPenalty float64 `json:"applied_reputation_penalty,omitempty"`
	Details             string    `json:"details,omitempty"`
}

type ConsensusRoundChange struct {
	ChainID     string    `json:"chain_id"`
	Height      int64     `json:"height"`
	Round       int       `json:"round"`
	Validator   string    `json:"validator"`
	Reason      string    `json:"reason"`
	RequestedAt time.Time `json:"requested_at"`
	Signature   string    `json:"signature"`
}

type PeerStatus struct {
	NodeID           string    `json:"node_id"`
	ChainID          string    `json:"chain_id"`
	GenesisHash      string    `json:"genesis_hash"`
	ListenAddr       string    `json:"listen_addr"`
	ValidatorAddress string    `json:"validator_address,omitempty"`
	HeadHeight       int64     `json:"head_height"`
	HeadHash         string    `json:"head_hash"`
	ObservedAt       time.Time `json:"observed_at"`
	Signature        string    `json:"signature,omitempty"`
}

type PeerHello struct {
	NodeID           string    `json:"node_id"`
	ChainID          string    `json:"chain_id"`
	GenesisHash      string    `json:"genesis_hash"`
	ListenAddr       string    `json:"listen_addr"`
	ValidatorAddress string    `json:"validator_address,omitempty"`
	SeenAt           time.Time `json:"seen_at"`
	Signature        string    `json:"signature,omitempty"`
}

type PeerTelemetry struct {
	Peer                PeerStatus  `json:"peer"`
	Score               int         `json:"score"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	LastAttemptAt       *time.Time  `json:"last_attempt_at,omitempty"`
	LastSuccessAt       *time.Time  `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time  `json:"last_failure_at,omitempty"`
	BackoffUntil        *time.Time  `json:"backoff_until,omitempty"`
	LastError           string      `json:"last_error,omitempty"`
}

type Agent struct {
	Address    string    `json:"address"`
	PublicKey  string    `json:"public_key,omitempty"`
	NextNonce  int64     `json:"next_nonce"`
	Balance    Amount    `json:"balance"`
	Reputation float64   `json:"reputation"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Result struct {
	TaskID            string     `json:"task_id"`
	FinalValue        *float64   `json:"final_value,omitempty"`
	Outcome           *float64   `json:"outcome,omitempty"`
	WinningProposalID *int64     `json:"winning_proposal_id,omitempty"`
	WinningAgent      *string    `json:"winning_agent,omitempty"`
	Settled           bool       `json:"settled"`
	SettledAt         *time.Time `json:"settled_at,omitempty"`
	LastUpdatedAt     time.Time  `json:"last_updated_at"`
}

type TaskDispute struct {
	ID         int64      `json:"id"`
	TaskID      string     `json:"task_id"`
	Challenger  string     `json:"challenger"`
	Bond        Amount     `json:"bond"`
	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	Resolver    string     `json:"resolver,omitempty"`
	Resolution  string     `json:"resolution,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	OpenedAt    time.Time  `json:"opened_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

type OracleReport struct {
	ID         int64     `json:"id"`
	TaskID     string    `json:"task_id"`
	Source     string    `json:"source"`
	Endpoint   string    `json:"endpoint"`
	Path       string    `json:"path"`
	Value      float64   `json:"value"`
	ObservedAt time.Time `json:"observed_at"`
	RawHash    string    `json:"raw_hash"`
	CreatedAt  time.Time `json:"created_at"`
}

type GovernanceParameter struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GovernanceProposal struct {
	ID             int64      `json:"id"`
	Proposer       string     `json:"proposer"`
	ProposalType   string     `json:"proposal_type"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	TargetAddress  string     `json:"target_address,omitempty"`
	Amount         Amount     `json:"amount,omitempty"`
	ParameterName  string     `json:"parameter_name,omitempty"`
	ParameterValue string     `json:"parameter_value,omitempty"`
	VotingDeadline int64      `json:"voting_deadline"`
	Status         string     `json:"status"`
	ExecutionNote  string     `json:"execution_note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

type GovernanceVote struct {
	ID         int64     `json:"id"`
	ProposalID int64     `json:"proposal_id"`
	Voter      string    `json:"voter"`
	Vote       string    `json:"vote"`
	Power      int64     `json:"power"`
	CreatedAt  time.Time `json:"created_at"`
}

type TaskDetails struct {
	Task             Task                 `json:"task"`
	Assignments      []RoleAssignment     `json:"assignments,omitempty"`
	DebateState      *DebateState         `json:"debate_state,omitempty"`
	Submissions      []Submission         `json:"submissions,omitempty"`
	Proposals        []Proposal           `json:"proposals,omitempty"`
	Evaluations      []ProposalEvaluation `json:"evaluations,omitempty"`
	Rebuttals        []Rebuttal           `json:"rebuttals,omitempty"`
	Votes            []ProposalVote       `json:"votes,omitempty"`
	Proofs           []ProofOfThought     `json:"proofs,omitempty"`
	Disputes         []TaskDispute        `json:"disputes,omitempty"`
	OracleReports    []OracleReport       `json:"oracle_reports,omitempty"`
	CurrentConsensus *float64             `json:"current_consensus,omitempty"`
	FinalResult      *Result              `json:"final_result,omitempty"`
}

type CreateTaskRequest struct {
	Creator      string  `json:"creator"`
	Type         string  `json:"type,omitempty"`
	Question     string  `json:"question"`
	Deadline     int64   `json:"deadline"`
	RewardPool   Amount  `json:"reward_pool"`
	MinStake     Amount  `json:"min_stake"`
	DebateRounds int     `json:"debate_rounds,omitempty"`
	WorkerCount  int     `json:"worker_count,omitempty"`
	MinerCount   int     `json:"miner_count,omitempty"`
	RoleSelectionPolicy string `json:"role_selection_policy,omitempty"`
	OracleSource string `json:"oracle_source,omitempty"`
	OracleEndpoint string `json:"oracle_endpoint,omitempty"`
	OraclePath string `json:"oracle_path,omitempty"`
	Auth         TxAuth  `json:"auth"`
}

type SubmitRequest struct {
	TaskID string  `json:"task_id"`
	Agent  string  `json:"agent"`
	Value  float64 `json:"value"`
	Stake  Amount  `json:"stake"`
	Auth   TxAuth  `json:"auth"`
}

type SubmitProposalRequest struct {
	TaskID  string `json:"task_id"`
	Agent   string `json:"agent"`
	Round   int    `json:"round"`
	Content string `json:"content"`
	Auth    TxAuth `json:"auth"`
}

type SubmitEvaluationRequest struct {
	TaskID             string  `json:"task_id"`
	ProposalID         int64   `json:"proposal_id"`
	Evaluator          string  `json:"evaluator"`
	Round              int     `json:"round"`
	FactualConsistency float64 `json:"factual_consistency"`
	RedundancyScore    float64 `json:"redundancy_score"`
	CausalRelevance    float64 `json:"causal_relevance"`
	Comments           string  `json:"comments,omitempty"`
	Auth               TxAuth  `json:"auth"`
}

type SubmitRebuttalRequest struct {
	TaskID     string `json:"task_id"`
	ProposalID int64  `json:"proposal_id"`
	Agent      string `json:"agent"`
	Round      int    `json:"round"`
	Content    string `json:"content"`
	Auth       TxAuth `json:"auth"`
}

type SubmitVoteRequest struct {
	TaskID     string `json:"task_id"`
	ProposalID int64  `json:"proposal_id"`
	Voter      string `json:"voter"`
	Round      int    `json:"round"`
	Reason     string `json:"reason,omitempty"`
	Auth       TxAuth `json:"auth"`
}

type SubmitProofRequest struct {
	TaskID       string `json:"task_id"`
	Agent        string `json:"agent"`
	Round        int    `json:"round"`
	Stage        string `json:"stage"`
	ArtifactType string `json:"artifact_type"`
	Content      string `json:"content"`
	ParentType   string `json:"parent_type,omitempty"`
	ParentID     *int64 `json:"parent_id,omitempty"`
	Auth         TxAuth `json:"auth"`
}

type FundAgentRequest struct {
	Agent  string  `json:"agent"`
	Amount Amount  `json:"amount"`
	Auth   TxAuth  `json:"auth"`
}

type BootstrapAgentKeyRequest struct {
	Agent string `json:"agent"`
	Auth  TxAuth `json:"auth"`
}

type RotateAgentKeyRequest struct {
	Agent        string `json:"agent"`
	NewPublicKey string `json:"new_public_key"`
	NewSignature string `json:"new_signature"`
	Auth         TxAuth `json:"auth"`
}

type UpsertValidatorRequest struct {
	Operator  string `json:"operator"`
	Validator string `json:"validator"`
	PublicKey string `json:"public_key"`
	Power     int64  `json:"power"`
	Auth      TxAuth `json:"auth"`
}

type DeactivateValidatorRequest struct {
	Operator  string `json:"operator"`
	Validator string `json:"validator"`
	Auth      TxAuth `json:"auth"`
}

type OpenDisputeRequest struct {
	TaskID      string `json:"task_id"`
	Challenger  string `json:"challenger"`
	Reason      string `json:"reason"`
	Auth        TxAuth `json:"auth"`
}

type ResolveDisputeRequest struct {
	DisputeID   int64  `json:"dispute_id"`
	Resolver    string `json:"resolver"`
	Resolution  string `json:"resolution"`
	Notes       string `json:"notes,omitempty"`
	Auth        TxAuth `json:"auth"`
}

type SubmitGovernanceProposalRequest struct {
	Proposer       string  `json:"proposer"`
	ProposalType   string  `json:"proposal_type"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	TargetAddress  string  `json:"target_address,omitempty"`
	Amount         Amount  `json:"amount,omitempty"`
	ParameterName  string  `json:"parameter_name,omitempty"`
	ParameterValue string  `json:"parameter_value,omitempty"`
	VotingDeadline int64   `json:"voting_deadline"`
	Auth           TxAuth  `json:"auth"`
}

type SubmitGovernanceVoteRequest struct {
	ProposalID int64  `json:"proposal_id"`
	Voter      string `json:"voter"`
	Vote       string `json:"vote"`
	Auth       TxAuth `json:"auth"`
}

type Transaction struct {
	Hash       string          `json:"hash"`
	Type       TxType          `json:"type"`
	Sender     string          `json:"sender"`
	Nonce      int64           `json:"nonce"`
	PublicKey  string          `json:"public_key,omitempty"`
	Signature  string          `json:"signature,omitempty"`
	Sequence   int64           `json:"sequence,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	AcceptedAt time.Time       `json:"accepted_at"`
}

type Event struct {
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Receipt struct {
	TxHash      string  `json:"tx_hash"`
	BlockHeight int64   `json:"block_height"`
	Success     bool    `json:"success"`
	ErrorCode   string  `json:"error_code,omitempty"`
	Error       string  `json:"error,omitempty"`
	Events      []Event `json:"events,omitempty"`
}

type TransactionStatus struct {
	Transaction Transaction `json:"transaction"`
	Status      string      `json:"status"`
	BlockHeight *int64      `json:"block_height,omitempty"`
	Receipt     *Receipt    `json:"receipt,omitempty"`
	ErrorCode   string      `json:"error_code,omitempty"`
	Error       string      `json:"error,omitempty"`
}

type BlockHeader struct {
	ChainID    string    `json:"chain_id"`
	Height     int64     `json:"height"`
	ParentHash string    `json:"parent_hash"`
	Timestamp  time.Time `json:"timestamp"`
	Proposer   string    `json:"proposer"`
	TxRoot     string    `json:"tx_root"`
	StateRoot  string    `json:"state_root"`
	AppHash    string    `json:"app_hash"`
	TxCount    int       `json:"tx_count"`
}

type Block struct {
	Hash         string        `json:"hash"`
	Header       BlockHeader   `json:"header"`
	Transactions []Transaction `json:"transactions"`
	Receipts     []Receipt     `json:"receipts"`
	Events       []Event       `json:"events,omitempty"`
}

type ChainInfo struct {
	ChainID                 string `json:"chain_id"`
	NodeID                  string `json:"node_id"`
	HeadHeight              int64  `json:"head_height"`
	HeadHash                string `json:"head_hash"`
	GenesisHash             string `json:"genesis_hash"`
	SchemaVersion           int    `json:"schema_version"`
	BlockIntervalSeconds    int64  `json:"block_interval_seconds"`
	MaxTransactionsPerBlock int    `json:"max_transactions_per_block"`
	FaucetEnabled           bool   `json:"faucet_enabled"`
	RoleSelectionPolicy     string `json:"role_selection_policy,omitempty"`
	MinerVotePolicy         string `json:"miner_vote_policy,omitempty"`
	ReorgPolicy             string `json:"reorg_policy,omitempty"`
}

type ForkChoicePreference struct {
	Height      int64             `json:"height"`
	Certificate QuorumCertificate `json:"certificate"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type StateSnapshot struct {
	ChainInfo           ChainInfo               `json:"chain_info"`
	RetainedFromHeight  int64                   `json:"retained_from_height"`
	HeadBlock           Block                   `json:"head_block"`
	CertifiedWindow     []CertifiedBlock        `json:"certified_window,omitempty"`
	Validators          []Validator             `json:"validators,omitempty"`
	ForkChoice          []ForkChoicePreference  `json:"fork_choice,omitempty"`
	Agents              []Agent                 `json:"agents,omitempty"`
	Tasks               []Task                  `json:"tasks,omitempty"`
	Assignments         []RoleAssignment        `json:"assignments,omitempty"`
	DebateStates        []DebateState           `json:"debate_states,omitempty"`
	Submissions         []Submission            `json:"submissions,omitempty"`
	Proposals           []Proposal              `json:"proposals,omitempty"`
	Evaluations         []ProposalEvaluation    `json:"evaluations,omitempty"`
	Rebuttals           []Rebuttal              `json:"rebuttals,omitempty"`
	Votes               []ProposalVote          `json:"votes,omitempty"`
	Proofs              []ProofOfThought        `json:"proofs,omitempty"`
	Results             []Result                `json:"results,omitempty"`
	Disputes            []TaskDispute           `json:"disputes,omitempty"`
	OracleReports       []OracleReport          `json:"oracle_reports,omitempty"`
	GovernanceParameters []GovernanceParameter  `json:"governance_parameters,omitempty"`
	GovernanceProposals []GovernanceProposal    `json:"governance_proposals,omitempty"`
	GovernanceVotes     []GovernanceVote        `json:"governance_votes,omitempty"`
	ConsensusEvidence   []ConsensusEvidence     `json:"consensus_evidence,omitempty"`
	ConsensusRounds     []ConsensusRoundChange  `json:"consensus_round_changes,omitempty"`
	ExportedAt          time.Time               `json:"exported_at"`
}

type SyncStatus struct {
	LastAttemptAt     *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt     *time.Time `json:"last_failure_at,omitempty"`
	LastMode          string     `json:"last_mode,omitempty"`
	LastPeer          string     `json:"last_peer,omitempty"`
	LastForkHeight    int64      `json:"last_fork_height,omitempty"`
	LastTargetHeight  int64      `json:"last_target_height,omitempty"`
	LastImportedHeight int64     `json:"last_imported_height,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
}
