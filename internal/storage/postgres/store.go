package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aichain/internal/config"
	"aichain/internal/protocol"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrValidation           = errors.New("validation error")
	ErrInsufficientBalance  = errors.New("insufficient balance")
	ErrDuplicateSubmission  = errors.New("duplicate submission")
	ErrDuplicateTransaction = errors.New("duplicate transaction")
	ErrFaucetDisabled       = errors.New("faucet is disabled")
	ErrUnauthorized         = errors.New("unauthorized transaction")
	ErrInvalidNonce         = errors.New("invalid nonce")
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS agents (
	address TEXT PRIMARY KEY,
	public_key TEXT NULL,
	next_nonce BIGINT NOT NULL DEFAULT 0,
	balance DOUBLE PRECISION NOT NULL CHECK (balance >= 0),
	reputation DOUBLE PRECISION NOT NULL CHECK (reputation >= 0 AND reputation <= 1),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	creator TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	type TEXT NOT NULL,
	question TEXT NOT NULL,
	deadline BIGINT NOT NULL,
	debate_rounds INTEGER NOT NULL DEFAULT 1,
	worker_count INTEGER NOT NULL DEFAULT 0,
	miner_count INTEGER NOT NULL DEFAULT 0,
	reward_pool DOUBLE PRECISION NOT NULL CHECK (reward_pool >= 0),
	min_stake DOUBLE PRECISION NOT NULL CHECK (min_stake > 0),
	status TEXT NOT NULL CHECK (status IN ('open', 'settled')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS submissions (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	agent TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	value DOUBLE PRECISION NOT NULL CHECK (value >= 0 AND value <= 1),
	stake DOUBLE PRECISION NOT NULL CHECK (stake > 0),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT submissions_task_agent_unique UNIQUE (task_id, agent)
);

CREATE TABLE IF NOT EXISTS task_results (
	task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
	final_value DOUBLE PRECISION NULL,
	outcome DOUBLE PRECISION NULL CHECK (outcome >= 0 AND outcome <= 1),
	winning_proposal_id BIGINT NULL,
	winning_agent TEXT NULL,
	settled BOOLEAN NOT NULL DEFAULT FALSE,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	settled_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS task_roles (
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	agent TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	role TEXT NOT NULL CHECK (role IN ('worker', 'miner')),
	assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (task_id, agent, role)
);

CREATE TABLE IF NOT EXISTS task_proposals (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	agent TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	round INTEGER NOT NULL CHECK (round > 0),
	content TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT task_proposals_unique UNIQUE (task_id, agent, round)
);

CREATE TABLE IF NOT EXISTS task_evaluations (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	proposal_id BIGINT NOT NULL REFERENCES task_proposals(id) ON DELETE CASCADE,
	evaluator TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	round INTEGER NOT NULL CHECK (round > 0),
	factual_consistency DOUBLE PRECISION NOT NULL CHECK (factual_consistency >= 0 AND factual_consistency <= 1),
	redundancy_score DOUBLE PRECISION NOT NULL CHECK (redundancy_score >= 0 AND redundancy_score <= 1),
	causal_relevance DOUBLE PRECISION NOT NULL CHECK (causal_relevance >= 0 AND causal_relevance <= 1),
	overall_score DOUBLE PRECISION NOT NULL CHECK (overall_score >= 0 AND overall_score <= 1),
	comments TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT task_evaluations_unique UNIQUE (task_id, proposal_id, evaluator, round)
);

CREATE TABLE IF NOT EXISTS task_votes (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	proposal_id BIGINT NOT NULL REFERENCES task_proposals(id) ON DELETE CASCADE,
	voter TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	round INTEGER NOT NULL CHECK (round > 0),
	reason TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT task_votes_unique UNIQUE (task_id, voter, round)
);

CREATE TABLE IF NOT EXISTS task_debate_state (
	task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
	current_round INTEGER NOT NULL CHECK (current_round > 0),
	current_stage TEXT NOT NULL CHECK (current_stage IN ('proposal', 'evaluation', 'vote', 'complete')),
	stage_duration_seconds BIGINT NOT NULL CHECK (stage_duration_seconds > 0),
	stage_started_at TIMESTAMPTZ NOT NULL,
	stage_deadline TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proof_artifacts (
	id BIGSERIAL PRIMARY KEY,
	task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	agent TEXT NOT NULL REFERENCES agents(address) ON DELETE RESTRICT,
	round INTEGER NOT NULL CHECK (round > 0),
	stage TEXT NOT NULL CHECK (stage IN ('proposal', 'evaluation', 'vote')),
	artifact_type TEXT NOT NULL,
	content TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	claim_root TEXT NOT NULL DEFAULT '',
	semantic_root TEXT NOT NULL DEFAULT '',
	parent_type TEXT NULL,
	parent_id BIGINT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS blocks (
	height BIGINT PRIMARY KEY,
	hash TEXT NOT NULL UNIQUE,
	parent_hash TEXT NOT NULL,
	chain_id TEXT NOT NULL,
	proposer TEXT NOT NULL,
	tx_root TEXT NOT NULL,
	state_root TEXT NOT NULL,
	app_hash TEXT NOT NULL,
	tx_count INTEGER NOT NULL,
	sealed_at TIMESTAMPTZ NOT NULL,
	header_json JSONB NOT NULL,
	events_json JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS block_transactions (
	block_height BIGINT NOT NULL REFERENCES blocks(height) ON DELETE CASCADE,
	tx_index INTEGER NOT NULL,
	tx_hash TEXT NOT NULL UNIQUE,
	tx_type TEXT NOT NULL,
	sender TEXT NOT NULL,
	nonce BIGINT NOT NULL DEFAULT 0,
	public_key TEXT NULL,
	signature TEXT NULL,
	payload JSONB NOT NULL,
	success BOOLEAN NOT NULL,
	error TEXT NULL,
	receipt_json JSONB NOT NULL,
	accepted_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (block_height, tx_index)
);

CREATE TABLE IF NOT EXISTS tx_pool (
	sequence BIGSERIAL PRIMARY KEY,
	tx_hash TEXT NOT NULL UNIQUE,
	tx_type TEXT NOT NULL,
	sender TEXT NOT NULL,
	nonce BIGINT NOT NULL DEFAULT 0,
	public_key TEXT NULL,
	signature TEXT NULL,
	payload JSONB NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('pending', 'committed', 'rejected')),
	error TEXT NULL,
	block_height BIGINT NULL REFERENCES blocks(height) ON DELETE SET NULL,
	accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS chain_metadata (
	singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
	chain_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	head_height BIGINT NOT NULL,
	head_hash TEXT NOT NULL,
	genesis_hash TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS consensus_proposals (
	block_hash TEXT PRIMARY KEY,
	chain_id TEXT NOT NULL,
	height BIGINT NOT NULL,
	round INTEGER NOT NULL,
	proposer TEXT NOT NULL,
	block_height BIGINT NOT NULL,
	parent_hash TEXT NOT NULL,
	proposed_at TIMESTAMPTZ NOT NULL,
	signature TEXT NOT NULL,
	payload_json JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS consensus_votes (
	chain_id TEXT NOT NULL,
	height BIGINT NOT NULL,
	round INTEGER NOT NULL,
	vote_type TEXT NOT NULL,
	voter TEXT NOT NULL,
	block_hash TEXT NOT NULL,
	voted_at TIMESTAMPTZ NOT NULL,
	signature TEXT NOT NULL,
	payload_json JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (height, round, vote_type, voter, block_hash)
);

CREATE TABLE IF NOT EXISTS consensus_certificates (
	block_hash TEXT NOT NULL,
	chain_id TEXT NOT NULL,
	height BIGINT NOT NULL,
	round INTEGER NOT NULL,
	vote_type TEXT NOT NULL,
	power BIGINT NOT NULL,
	threshold_power BIGINT NOT NULL,
	certified_at TIMESTAMPTZ NOT NULL,
	payload_json JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (block_hash, vote_type)
);

CREATE TABLE IF NOT EXISTS consensus_round_changes (
	chain_id TEXT NOT NULL,
	height BIGINT NOT NULL,
	round INTEGER NOT NULL,
	validator TEXT NOT NULL,
	reason TEXT NOT NULL,
	requested_at TIMESTAMPTZ NOT NULL,
	signature TEXT NOT NULL,
	payload_json JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (height, round, validator)
);

CREATE TABLE IF NOT EXISTS consensus_evidence (
	id BIGSERIAL PRIMARY KEY,
	evidence_type TEXT NOT NULL,
	validator TEXT NOT NULL,
	height BIGINT NOT NULL,
	round INTEGER NOT NULL,
	vote_type TEXT NOT NULL DEFAULT '',
	block_hash TEXT NOT NULL,
	conflicting_block_hash TEXT NOT NULL,
	details TEXT NOT NULL DEFAULT '',
	observed_at TIMESTAMPTZ NOT NULL,
	processed_at TIMESTAMPTZ NULL,
	applied_balance_penalty DOUBLE PRECISION NOT NULL DEFAULT 0,
	applied_reputation_penalty DOUBLE PRECISION NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT consensus_evidence_unique UNIQUE (evidence_type, validator, height, round, vote_type, block_hash, conflicting_block_hash)
);

CREATE INDEX IF NOT EXISTS idx_tx_pool_status_sequence ON tx_pool(status, sequence);
CREATE INDEX IF NOT EXISTS idx_tasks_status_deadline ON tasks(status, deadline);
CREATE INDEX IF NOT EXISTS idx_submissions_task_id ON submissions(task_id);
CREATE INDEX IF NOT EXISTS idx_task_roles_task_id ON task_roles(task_id);
CREATE INDEX IF NOT EXISTS idx_task_proposals_task_round ON task_proposals(task_id, round, created_at);
CREATE INDEX IF NOT EXISTS idx_task_evaluations_task_round ON task_evaluations(task_id, round, created_at);
CREATE INDEX IF NOT EXISTS idx_task_votes_task_round ON task_votes(task_id, round, created_at);
CREATE INDEX IF NOT EXISTS idx_task_debate_state_stage ON task_debate_state(current_stage, stage_deadline);
CREATE INDEX IF NOT EXISTS idx_proof_artifacts_task_round_stage ON proof_artifacts(task_id, round, stage, created_at);
CREATE INDEX IF NOT EXISTS idx_consensus_proposals_height ON consensus_proposals(height, round);
CREATE INDEX IF NOT EXISTS idx_consensus_votes_height ON consensus_votes(height, round, vote_type, block_hash);
CREATE INDEX IF NOT EXISTS idx_consensus_certificates_height ON consensus_certificates(height, round, vote_type);
CREATE INDEX IF NOT EXISTS idx_consensus_round_changes_height ON consensus_round_changes(height, round, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_consensus_evidence_height ON consensus_evidence(height, round, observed_at DESC);

ALTER TABLE agents ADD COLUMN IF NOT EXISTS public_key TEXT NULL;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS next_nonce BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS debate_rounds INTEGER NOT NULL DEFAULT 1;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS worker_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS miner_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE task_results ADD COLUMN IF NOT EXISTS winning_proposal_id BIGINT NULL;
ALTER TABLE task_results ADD COLUMN IF NOT EXISTS winning_agent TEXT NULL;
ALTER TABLE tx_pool ADD COLUMN IF NOT EXISTS nonce BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tx_pool ADD COLUMN IF NOT EXISTS public_key TEXT NULL;
ALTER TABLE tx_pool ADD COLUMN IF NOT EXISTS signature TEXT NULL;
ALTER TABLE block_transactions ADD COLUMN IF NOT EXISTS nonce BIGINT NOT NULL DEFAULT 0;
ALTER TABLE block_transactions ADD COLUMN IF NOT EXISTS public_key TEXT NULL;
ALTER TABLE block_transactions ADD COLUMN IF NOT EXISTS signature TEXT NULL;
ALTER TABLE proof_artifacts ADD COLUMN IF NOT EXISTS claim_root TEXT NOT NULL DEFAULT '';
ALTER TABLE proof_artifacts ADD COLUMN IF NOT EXISTS semantic_root TEXT NOT NULL DEFAULT '';
ALTER TABLE consensus_evidence ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ NULL;
ALTER TABLE consensus_evidence ADD COLUMN IF NOT EXISTS applied_balance_penalty DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE consensus_evidence ADD COLUMN IF NOT EXISTS applied_reputation_penalty DOUBLE PRECISION NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tx_pool_sender_nonce ON tx_pool(sender, nonce) WHERE nonce > 0;
`

type Store struct {
	db            *sql.DB
	cfg           config.Config
	schemaVersion int
}

type SealOptions struct {
	Proposer           string
	MaxTransactions    int
	MaxEffectiveWeight float64
	CreateEmptyBlocks  bool
	Now                time.Time
}

type chainMetadata struct {
	ChainID     string
	NodeID      string
	HeadHeight  int64
	HeadHash    string
	GenesisHash string
}

type pendingTx struct {
	Sequence   int64
	Hash       string
	Type       protocol.TxType
	Sender     string
	Nonce      int64
	PublicKey  string
	Signature  string
	Payload    []byte
	AcceptedAt time.Time
}

func New(cfg config.Config) (*Store, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{db: db, cfg: cfg}
	if err := store.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.initChain(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.syncValidatorRegistry(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) QueueCreateTask(ctx context.Context, req protocol.CreateTaskRequest) (protocol.TransactionStatus, error) {
	req.Creator = strings.TrimSpace(req.Creator)
	req.Type = strings.TrimSpace(req.Type)
	req.Question = strings.TrimSpace(req.Question)
	req.RoleSelectionPolicy = strings.TrimSpace(req.RoleSelectionPolicy)
	if req.Type == "" {
		req.Type = protocol.TaskTypePrediction
	}
	if req.RoleSelectionPolicy == "" {
		req.RoleSelectionPolicy = s.cfg.RoleSelectionPolicy
	}

	switch {
	case req.Creator == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: creator is required", ErrValidation)
	case req.Type != protocol.TaskTypePrediction && req.Type != protocol.TaskTypeBlockAgents:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: unsupported task type", ErrValidation)
	case req.Question == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: question is required", ErrValidation)
	case req.Deadline <= time.Now().Unix():
		return protocol.TransactionStatus{}, fmt.Errorf("%w: deadline must be in the future", ErrValidation)
	case req.RewardPool < 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: reward_pool must be >= 0", ErrValidation)
	case req.MinStake <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: min_stake must be > 0", ErrValidation)
	case req.Type == protocol.TaskTypeBlockAgents && req.DebateRounds <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: debate_rounds must be > 0 for blockagents tasks", ErrValidation)
	case req.Type == protocol.TaskTypeBlockAgents && req.WorkerCount <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: worker_count must be > 0 for blockagents tasks", ErrValidation)
	case req.Type == protocol.TaskTypeBlockAgents && req.MinerCount <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: miner_count must be > 0 for blockagents tasks", ErrValidation)
	case req.Type == protocol.TaskTypeBlockAgents && !isSupportedRoleSelectionPolicy(req.RoleSelectionPolicy):
		return protocol.TransactionStatus{}, fmt.Errorf("%w: unsupported role_selection_policy", ErrValidation)
	}

	payload := struct {
		Creator      string  `json:"creator"`
		Type         string  `json:"type,omitempty"`
		Question     string  `json:"question"`
		Deadline     int64   `json:"deadline"`
		RewardPool   float64 `json:"reward_pool"`
		MinStake     float64 `json:"min_stake"`
		DebateRounds int     `json:"debate_rounds,omitempty"`
		WorkerCount  int     `json:"worker_count,omitempty"`
		MinerCount   int     `json:"miner_count,omitempty"`
		RoleSelectionPolicy string `json:"role_selection_policy,omitempty"`
	}{
		Creator:      req.Creator,
		Type:         req.Type,
		Question:     req.Question,
		Deadline:     req.Deadline,
		RewardPool:   req.RewardPool,
		MinStake:     req.MinStake,
		DebateRounds: req.DebateRounds,
		WorkerCount:  req.WorkerCount,
		MinerCount:   req.MinerCount,
		RoleSelectionPolicy: req.RoleSelectionPolicy,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeCreateTask, req.Creator, req.Auth, payload, true)
}

func (s *Store) QueueSubmission(ctx context.Context, req protocol.SubmitRequest) (protocol.TransactionStatus, error) {
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Agent = strings.TrimSpace(req.Agent)

	switch {
	case req.TaskID == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	case req.Agent == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	case req.Value < 0 || req.Value > 1:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: value must be within [0,1]", ErrValidation)
	case req.Stake <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: stake must be > 0", ErrValidation)
	}

	payload := struct {
		TaskID string  `json:"task_id"`
		Agent  string  `json:"agent"`
		Value  float64 `json:"value"`
		Stake  float64 `json:"stake"`
	}{
		TaskID: req.TaskID,
		Agent:  req.Agent,
		Value:  req.Value,
		Stake:  req.Stake,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeSubmitInference, req.Agent, req.Auth, payload, true)
}

func (s *Store) QueueProposal(ctx context.Context, req protocol.SubmitProposalRequest) (protocol.TransactionStatus, error) {
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Agent = strings.TrimSpace(req.Agent)
	req.Content = strings.TrimSpace(req.Content)

	switch {
	case req.TaskID == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	case req.Agent == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	case req.Round <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: round must be > 0", ErrValidation)
	case req.Content == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: content is required", ErrValidation)
	}

	payload := struct {
		TaskID  string `json:"task_id"`
		Agent   string `json:"agent"`
		Round   int    `json:"round"`
		Content string `json:"content"`
	}{
		TaskID:  req.TaskID,
		Agent:   req.Agent,
		Round:   req.Round,
		Content: req.Content,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeSubmitProposal, req.Agent, req.Auth, payload, true)
}

func (s *Store) QueueEvaluation(ctx context.Context, req protocol.SubmitEvaluationRequest) (protocol.TransactionStatus, error) {
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Evaluator = strings.TrimSpace(req.Evaluator)
	req.Comments = strings.TrimSpace(req.Comments)

	switch {
	case req.TaskID == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	case req.ProposalID <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: proposal_id must be > 0", ErrValidation)
	case req.Evaluator == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: evaluator is required", ErrValidation)
	case req.Round <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: round must be > 0", ErrValidation)
	case req.FactualConsistency < 0 || req.FactualConsistency > 1:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: factual_consistency must be within [0,1]", ErrValidation)
	case req.RedundancyScore < 0 || req.RedundancyScore > 1:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: redundancy_score must be within [0,1]", ErrValidation)
	case req.CausalRelevance < 0 || req.CausalRelevance > 1:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: causal_relevance must be within [0,1]", ErrValidation)
	}

	payload := struct {
		TaskID             string  `json:"task_id"`
		ProposalID         int64   `json:"proposal_id"`
		Evaluator          string  `json:"evaluator"`
		Round              int     `json:"round"`
		FactualConsistency float64 `json:"factual_consistency"`
		RedundancyScore    float64 `json:"redundancy_score"`
		CausalRelevance    float64 `json:"causal_relevance"`
		Comments           string  `json:"comments,omitempty"`
	}{
		TaskID:             req.TaskID,
		ProposalID:         req.ProposalID,
		Evaluator:          req.Evaluator,
		Round:              req.Round,
		FactualConsistency: req.FactualConsistency,
		RedundancyScore:    req.RedundancyScore,
		CausalRelevance:    req.CausalRelevance,
		Comments:           req.Comments,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeSubmitEvaluation, req.Evaluator, req.Auth, payload, true)
}

func (s *Store) QueueVote(ctx context.Context, req protocol.SubmitVoteRequest) (protocol.TransactionStatus, error) {
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Voter = strings.TrimSpace(req.Voter)
	req.Reason = strings.TrimSpace(req.Reason)

	switch {
	case req.TaskID == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	case req.ProposalID <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: proposal_id must be > 0", ErrValidation)
	case req.Voter == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: voter is required", ErrValidation)
	case req.Round <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: round must be > 0", ErrValidation)
	}

	payload := struct {
		TaskID     string `json:"task_id"`
		ProposalID int64  `json:"proposal_id"`
		Voter      string `json:"voter"`
		Round      int    `json:"round"`
		Reason     string `json:"reason,omitempty"`
	}{
		TaskID:     req.TaskID,
		ProposalID: req.ProposalID,
		Voter:      req.Voter,
		Round:      req.Round,
		Reason:     req.Reason,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeSubmitVote, req.Voter, req.Auth, payload, true)
}

func (s *Store) QueueProof(ctx context.Context, req protocol.SubmitProofRequest) (protocol.TransactionStatus, error) {
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Agent = strings.TrimSpace(req.Agent)
	req.Stage = strings.TrimSpace(req.Stage)
	req.ArtifactType = strings.TrimSpace(req.ArtifactType)
	req.Content = strings.TrimSpace(req.Content)
	req.ParentType = strings.TrimSpace(req.ParentType)

	switch {
	case req.TaskID == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	case req.Agent == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	case req.Round <= 0:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: round must be > 0", ErrValidation)
	case req.Stage != protocol.DebateStageProposal && req.Stage != protocol.DebateStageEvaluation && req.Stage != protocol.DebateStageVote:
		return protocol.TransactionStatus{}, fmt.Errorf("%w: unsupported debate stage", ErrValidation)
	case req.ArtifactType == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: artifact_type is required", ErrValidation)
	case req.Content == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: content is required", ErrValidation)
	}

	payload := struct {
		TaskID       string `json:"task_id"`
		Agent        string `json:"agent"`
		Round        int    `json:"round"`
		Stage        string `json:"stage"`
		ArtifactType string `json:"artifact_type"`
		Content      string `json:"content"`
		ParentType   string `json:"parent_type,omitempty"`
		ParentID     *int64 `json:"parent_id,omitempty"`
	}{
		TaskID:       req.TaskID,
		Agent:        req.Agent,
		Round:        req.Round,
		Stage:        req.Stage,
		ArtifactType: req.ArtifactType,
		Content:      req.Content,
		ParentType:   req.ParentType,
		ParentID:     req.ParentID,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeSubmitProof, req.Agent, req.Auth, payload, true)
}

func (s *Store) QueueFunding(ctx context.Context, req protocol.FundAgentRequest) (protocol.TransactionStatus, error) {
	if !s.cfg.EnableFaucet {
		return protocol.TransactionStatus{}, ErrFaucetDisabled
	}

	req.Agent = strings.TrimSpace(req.Agent)
	if req.Agent == "" {
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	}
	if req.Amount <= 0 {
		req.Amount = s.cfg.FaucetGrantAmount
	}

	payload := struct {
		Agent  string  `json:"agent"`
		Amount float64 `json:"amount"`
	}{
		Agent:  req.Agent,
		Amount: req.Amount,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeFundAgent, s.cfg.Genesis.FaucetAddress, protocol.TxAuth{}, payload, false)
}

func (s *Store) QueueBootstrapAgentKey(ctx context.Context, req protocol.BootstrapAgentKeyRequest) (protocol.TransactionStatus, error) {
	req.Agent = strings.TrimSpace(req.Agent)
	if req.Agent == "" {
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	}

	payload := struct {
		Agent string `json:"agent"`
	}{
		Agent: req.Agent,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeBootstrapAgentKey, req.Agent, req.Auth, payload, true)
}

func (s *Store) QueueRotateAgentKey(ctx context.Context, req protocol.RotateAgentKeyRequest) (protocol.TransactionStatus, error) {
	req.Agent = strings.TrimSpace(req.Agent)
	req.NewPublicKey = strings.ToLower(strings.TrimSpace(req.NewPublicKey))
	req.NewSignature = strings.ToLower(strings.TrimSpace(req.NewSignature))

	switch {
	case req.Agent == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: agent is required", ErrValidation)
	case req.NewPublicKey == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: new_public_key is required", ErrValidation)
	case req.NewSignature == "":
		return protocol.TransactionStatus{}, fmt.Errorf("%w: new_signature is required", ErrValidation)
	case req.NewPublicKey == strings.ToLower(strings.TrimSpace(req.Auth.PublicKey)):
		return protocol.TransactionStatus{}, fmt.Errorf("%w: new_public_key must differ from current public_key", ErrValidation)
	}

	payload := struct {
		Agent        string `json:"agent"`
		NewPublicKey string `json:"new_public_key"`
		NewSignature string `json:"new_signature"`
	}{
		Agent:        req.Agent,
		NewPublicKey: req.NewPublicKey,
		NewSignature: req.NewSignature,
	}

	return s.enqueueTransaction(ctx, protocol.TxTypeRotateAgentKey, req.Agent, req.Auth, payload, true)
}

func (s *Store) enqueueTransaction(ctx context.Context, txType protocol.TxType, sender string, auth protocol.TxAuth, payload any, requireAuth bool) (protocol.TransactionStatus, error) {
	tx, err := s.prepareTransaction(ctx, txType, sender, auth, payload, requireAuth)
	if err != nil {
		return protocol.TransactionStatus{}, err
	}

	var (
		sequence   int64
		acceptedAt time.Time
	)

	err = s.db.QueryRowContext(
		ctx,
		`INSERT INTO tx_pool (tx_hash, tx_type, sender, nonce, public_key, signature, payload, status, accepted_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8)
		 RETURNING sequence, accepted_at`,
		tx.Hash,
		string(tx.Type),
		tx.Sender,
		tx.Nonce,
		nullIfEmpty(tx.PublicKey),
		nullIfEmpty(tx.Signature),
		tx.Payload,
		tx.AcceptedAt,
	).Scan(&sequence, &acceptedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return protocol.TransactionStatus{}, ErrDuplicateTransaction
		}
		return protocol.TransactionStatus{}, fmt.Errorf("enqueue transaction: %w", err)
	}

	tx.Sequence = sequence
	tx.AcceptedAt = acceptedAt

	return protocol.TransactionStatus{
		Transaction: tx,
		Status:      "pending",
	}, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 8, 64)
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func isSupportedRoleSelectionPolicy(value string) bool {
	switch strings.TrimSpace(value) {
	case "balance_reputation", "reputation_balance", "round_robin_hash":
		return true
	default:
		return false
	}
}
