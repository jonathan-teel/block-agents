package protocol

import (
	"encoding/json"
	"time"
)

const (
	StatusOpen    = "open"
	StatusSettled = "settled"

	TaskTypePrediction  = "prediction"
	TaskTypeBlockAgents = "blockagents"

	RoleWorker = "worker"
	RoleMiner  = "miner"

	DebateStageProposal   = "proposal"
	DebateStageEvaluation = "evaluation"
	DebateStageVote       = "vote"
	DebateStageComplete   = "complete"

	ConsensusEvidenceDoubleProposal = "double_proposal"
	ConsensusEvidenceDoubleVote     = "double_vote"
)

type TxType string

const (
	TxTypeCreateTask        TxType = "create_task"
	TxTypeSubmitInference   TxType = "submit_inference"
	TxTypeSubmitProposal    TxType = "submit_proposal"
	TxTypeSubmitEvaluation  TxType = "submit_evaluation"
	TxTypeSubmitVote        TxType = "submit_vote"
	TxTypeSubmitProof       TxType = "submit_proof"
	TxTypeFundAgent         TxType = "fund_agent"
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
	Balance    float64 `json:"balance"`
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
}

type Task struct {
	ID         string    `json:"id"`
	Creator    string    `json:"creator"`
	Type       string    `json:"type"`
	Input      TaskInput `json:"input"`
	RewardPool float64   `json:"reward_pool"`
	MinStake   float64   `json:"min_stake"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Submission struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Agent     string    `json:"agent"`
	Value     float64   `json:"value"`
	Stake     float64   `json:"stake"`
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
	AppliedBalancePenalty float64 `json:"applied_balance_penalty,omitempty"`
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
	ListenAddr       string    `json:"listen_addr"`
	ValidatorAddress string    `json:"validator_address,omitempty"`
	HeadHeight       int64     `json:"head_height"`
	HeadHash         string    `json:"head_hash"`
	ObservedAt       time.Time `json:"observed_at"`
}

type PeerHello struct {
	NodeID           string    `json:"node_id"`
	ChainID          string    `json:"chain_id"`
	ListenAddr       string    `json:"listen_addr"`
	ValidatorAddress string    `json:"validator_address,omitempty"`
	SeenAt           time.Time `json:"seen_at"`
}

type Agent struct {
	Address    string    `json:"address"`
	PublicKey  string    `json:"public_key,omitempty"`
	NextNonce  int64     `json:"next_nonce"`
	Balance    float64   `json:"balance"`
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

type TaskDetails struct {
	Task             Task                 `json:"task"`
	Assignments      []RoleAssignment     `json:"assignments,omitempty"`
	DebateState      *DebateState         `json:"debate_state,omitempty"`
	Submissions      []Submission         `json:"submissions,omitempty"`
	Proposals        []Proposal           `json:"proposals,omitempty"`
	Evaluations      []ProposalEvaluation `json:"evaluations,omitempty"`
	Votes            []ProposalVote       `json:"votes,omitempty"`
	Proofs           []ProofOfThought     `json:"proofs,omitempty"`
	CurrentConsensus *float64             `json:"current_consensus,omitempty"`
	FinalResult      *Result              `json:"final_result,omitempty"`
}

type CreateTaskRequest struct {
	Creator      string  `json:"creator"`
	Type         string  `json:"type,omitempty"`
	Question     string  `json:"question"`
	Deadline     int64   `json:"deadline"`
	RewardPool   float64 `json:"reward_pool"`
	MinStake     float64 `json:"min_stake"`
	DebateRounds int     `json:"debate_rounds,omitempty"`
	WorkerCount  int     `json:"worker_count,omitempty"`
	MinerCount   int     `json:"miner_count,omitempty"`
	Auth         TxAuth  `json:"auth"`
}

type SubmitRequest struct {
	TaskID string  `json:"task_id"`
	Agent  string  `json:"agent"`
	Value  float64 `json:"value"`
	Stake  float64 `json:"stake"`
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
	Amount float64 `json:"amount"`
	Auth   TxAuth  `json:"auth"`
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
	Error       string  `json:"error,omitempty"`
	Events      []Event `json:"events,omitempty"`
}

type TransactionStatus struct {
	Transaction Transaction `json:"transaction"`
	Status      string      `json:"status"`
	BlockHeight *int64      `json:"block_height,omitempty"`
	Receipt     *Receipt    `json:"receipt,omitempty"`
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
	BlockIntervalSeconds    int64  `json:"block_interval_seconds"`
	MaxTransactionsPerBlock int    `json:"max_transactions_per_block"`
	FaucetEnabled           bool   `json:"faucet_enabled"`
}
