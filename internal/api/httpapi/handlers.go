package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"aichain/internal/config"
	"aichain/internal/consensus"
	"aichain/internal/network/p2p"
	"aichain/internal/protocol"
	"aichain/internal/storage/postgres"
)

type Handler struct {
	store  *postgres.Store
	cfg    config.Config
	peers  *p2p.Manager
	engine *consensus.Engine
	syncProvider SyncStatusProvider
}

type SyncStatusProvider interface {
	SyncStatus() protocol.SyncStatus
}

func NewHandler(store *postgres.Store, cfg config.Config, peers *p2p.Manager, engine *consensus.Engine, syncProvider SyncStatusProvider) *Handler {
	return &Handler{
		store:  store,
		cfg:    cfg,
		peers:  peers,
		engine: engine,
		syncProvider: syncProvider,
	}
}

func (h *Handler) Health(c *gin.Context) {
	info, err := h.store.GetChainInfo(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"chain_id":    info.ChainID,
		"head_height": info.HeadHeight,
		"head_hash":   info.HeadHash,
	})
}

func (h *Handler) GetChainInfo(c *gin.Context) {
	info, err := h.store.GetChainInfo(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	info.BlockIntervalSeconds = int64(h.cfg.BlockInterval.Seconds())
	info.MaxTransactionsPerBlock = h.cfg.MaxTransactionsPerBlock
	info.FaucetEnabled = h.cfg.EnableFaucet

	c.JSON(http.StatusOK, info)
}

func (h *Handler) GetHeadBlock(c *gin.Context) {
	block, err := h.store.GetHeadBlock(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, block)
}

func (h *Handler) GetBlockByHeight(c *gin.Context) {
	height, err := strconv.ParseInt(c.Param("height"), 10, 64)
	if err != nil || height < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "height must be a non-negative integer"})
		return
	}

	block, err := h.store.GetBlockByHeight(c.Request.Context(), height)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, block)
}

func (h *Handler) GetTransaction(c *gin.Context) {
	status, err := h.store.GetTransactionStatus(c.Request.Context(), c.Param("hash"))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) GetAgent(c *gin.Context) {
	agent, err := h.store.GetAgent(c.Request.Context(), c.Param("address"))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, agent)
}

func (h *Handler) ListOpenTasks(c *gin.Context) {
	tasks, err := h.store.ListOpenTasks(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, tasks)
}

func (h *Handler) GetTask(c *gin.Context) {
	details, err := h.store.GetTaskDetails(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, details)
}

func (h *Handler) GetPeerStatus(c *gin.Context) {
	info, err := h.store.GetChainInfo(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	status := protocol.PeerStatus{
		NodeID:           h.cfg.NodeID,
		ChainID:          h.cfg.ChainID,
		GenesisHash:      info.GenesisHash,
		ListenAddr:       h.cfg.P2PListenAddr,
		ValidatorAddress: h.cfg.ValidatorAddress,
		HeadHeight:       info.HeadHeight,
		HeadHash:         info.HeadHash,
		ObservedAt:       nowUTC(),
	}
	if h.engine != nil {
		signed, err := h.engine.SignPeerStatus(status)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sign peer status"})
			return
		}
		status = signed
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) ListPeers(c *gin.Context) {
	if h.peers == nil {
		c.JSON(http.StatusOK, []protocol.PeerStatus{})
		return
	}
	c.JSON(http.StatusOK, h.peers.AdmittedPeers())
}

func (h *Handler) ListPeerTelemetry(c *gin.Context) {
	if h.peers == nil {
		c.JSON(http.StatusOK, []protocol.PeerTelemetry{})
		return
	}
	c.JSON(http.StatusOK, h.peers.PeerTelemetry())
}

func (h *Handler) GetSyncStatus(c *gin.Context) {
	if h.syncProvider == nil {
		c.JSON(http.StatusOK, protocol.SyncStatus{})
		return
	}
	c.JSON(http.StatusOK, h.syncProvider.SyncStatus())
}

func (h *Handler) ExportStateSnapshot(c *gin.Context) {
	window, err := strconv.Atoi(c.DefaultQuery("window", strconv.Itoa(h.cfg.SyncLookaheadBlocks)))
	if err != nil || window <= 0 || window > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "window must be an integer between 1 and 256"})
		return
	}

	snapshot, err := h.store.ExportStateSnapshot(c.Request.Context(), window)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, snapshot)
}

func (h *Handler) GetCandidateBlockByHash(c *gin.Context) {
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	candidate, ok := h.engine.CandidateBlock(c.Param("hash"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "candidate block not found"})
		return
	}
	c.JSON(http.StatusOK, candidate)
}

func (h *Handler) ListCertificates(c *gin.Context) {
	certificates, err := h.store.ListQuorumCertificates(c.Request.Context(), 100)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, certificates)
}

func (h *Handler) ListForkChoice(c *gin.Context) {
	preferences, err := h.store.ListForkChoicePreferences(c.Request.Context(), 256)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, preferences)
}

func (h *Handler) ListEvidence(c *gin.Context) {
	evidence, err := h.store.ListConsensusEvidence(c.Request.Context(), 100)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, evidence)
}

func (h *Handler) ListRoundChanges(c *gin.Context) {
	messages, err := h.store.ListConsensusRoundChanges(c.Request.Context(), 100)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, messages)
}

func (h *Handler) ListGovernanceProposals(c *gin.Context) {
	proposals, err := h.store.ListGovernanceProposals(c.Request.Context(), 100)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, proposals)
}

func (h *Handler) ListGovernanceParameters(c *gin.Context) {
	parameters, err := h.store.ListGovernanceParameters(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, parameters)
}

func (h *Handler) ListGovernanceVotes(c *gin.Context) {
	var proposalID *int64
	if raw := c.Query("proposal_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "proposal_id must be a positive integer"})
			return
		}
		proposalID = &value
	}

	votes, err := h.store.ListGovernanceVotes(c.Request.Context(), proposalID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, votes)
}

func (h *Handler) GetCertifiedBlockByHeight(c *gin.Context) {
	height, err := strconv.ParseInt(c.Param("height"), 10, 64)
	if err != nil || height < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "height must be a non-negative integer"})
		return
	}

	bundle, err := h.store.GetCertifiedBlockByHeight(c.Request.Context(), height)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

func (h *Handler) GetCertifiedBlocksRange(c *gin.Context) {
	from, err := strconv.ParseInt(c.DefaultQuery("from", "0"), 10, 64)
	if err != nil || from < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must be a non-negative integer"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil || limit <= 0 || limit > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be an integer between 1 and 64"})
		return
	}

	bundles, err := h.store.ListCertifiedBlocksRange(c.Request.Context(), from, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, bundles)
}

func (h *Handler) ListValidators(c *gin.Context) {
	validators, err := h.store.ListValidators(c.Request.Context())
	if err == nil {
		c.JSON(http.StatusOK, validators)
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusOK, []protocol.Validator{})
		return
	}
	c.JSON(http.StatusOK, h.engine.Validators())
}

func (h *Handler) PeerHello(c *gin.Context) {
	var hello protocol.PeerHello
	if err := c.ShouldBindJSON(&hello); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine != nil {
		if err := h.engine.VerifyPeerHello(hello); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
	}
	if h.peers != nil {
		if !h.peers.AllowHello(hello.NodeID, hello.SeenAt) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "peer hello rate limit exceeded"})
			return
		}
		status := protocol.PeerStatus{
			NodeID:           hello.NodeID,
			ChainID:          hello.ChainID,
			GenesisHash:      hello.GenesisHash,
			ListenAddr:       hello.ListenAddr,
			ValidatorAddress: hello.ValidatorAddress,
			ObservedAt:       hello.SeenAt,
		}
		h.peers.RememberPeer(status)
		_ = h.store.UpsertPeer(c.Request.Context(), status)
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "ok"})
}

func (h *Handler) ReceiveConsensusProposal(c *gin.Context) {
	var proposal protocol.ConsensusProposal
	if err := c.ShouldBindJSON(&proposal); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	if err := h.engine.HandleProposal(c.Request.Context(), proposal); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (h *Handler) ReceiveConsensusVote(c *gin.Context) {
	var vote protocol.ConsensusVote
	if err := c.ShouldBindJSON(&vote); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	qc, err := h.engine.HandleVote(c.Request.Context(), vote)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "certificate": qc})
}

func (h *Handler) ReceiveConsensusRoundChange(c *gin.Context) {
	var roundChange protocol.ConsensusRoundChange
	if err := c.ShouldBindJSON(&roundChange); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	if err := h.engine.HandleRoundChange(c.Request.Context(), roundChange); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (h *Handler) ImportCertifiedBlock(c *gin.Context) {
	var bundle protocol.CertifiedBlock
	if err := c.ShouldBindJSON(&bundle); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	if err := h.engine.VerifyCertifiedBlock(bundle); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.ImportCertifiedBlock(c.Request.Context(), bundle); err != nil {
		writeError(c, err)
		return
	}
	if err := h.engine.ReloadValidatorSet(c.Request.Context()); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "imported"})
}

func (h *Handler) ImportStateSnapshot(c *gin.Context) {
	var snapshot protocol.StateSnapshot
	if err := c.ShouldBindJSON(&snapshot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "consensus engine unavailable"})
		return
	}
	if err := h.engine.VerifyCertifiedBranch(snapshot.CertifiedWindow); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.ImportStateSnapshot(c.Request.Context(), snapshot); err != nil {
		writeError(c, err)
		return
	}
	if err := h.engine.ReloadValidatorSet(c.Request.Context()); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "imported"})
}

func (h *Handler) CreateTaskTx(c *gin.Context) {
	var req protocol.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueCreateTask(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateSubmissionTx(c *gin.Context) {
	var req protocol.SubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueSubmission(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateProposalTx(c *gin.Context) {
	var req protocol.SubmitProposalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueProposal(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateEvaluationTx(c *gin.Context) {
	var req protocol.SubmitEvaluationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueEvaluation(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateRebuttalTx(c *gin.Context) {
	var req protocol.SubmitRebuttalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueRebuttal(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateVoteTx(c *gin.Context) {
	var req protocol.SubmitVoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueVote(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) CreateProofTx(c *gin.Context) {
	var req protocol.SubmitProofRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueProof(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) FaucetGrant(c *gin.Context) {
	var req protocol.FundAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueFunding(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) BootstrapAgentKeyTx(c *gin.Context) {
	var req protocol.BootstrapAgentKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueBootstrapAgentKey(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) RotateAgentKeyTx(c *gin.Context) {
	var req protocol.RotateAgentKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueRotateAgentKey(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) OpenDisputeTx(c *gin.Context) {
	var req protocol.OpenDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueOpenDispute(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) ResolveDisputeTx(c *gin.Context) {
	var req protocol.ResolveDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueResolveDispute(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) SubmitGovernanceProposalTx(c *gin.Context) {
	var req protocol.SubmitGovernanceProposalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueSubmitGovernanceProposal(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func (h *Handler) SubmitGovernanceVoteTx(c *gin.Context) {
	var req protocol.SubmitGovernanceVoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status, err := h.store.QueueSubmitGovernanceVote(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, status)
}

func writeError(c *gin.Context, err error) {
	errorCode := postgresErrorCode(err)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrInsufficientBalance):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrDuplicateSubmission):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrDuplicateTransaction):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrFaucetDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "error_code": errorCode})
	case errors.Is(err, postgres.ErrInvalidNonce):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "error_code": errorCode})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "error_code": errorCode})
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func postgresErrorCode(err error) string {
	switch {
	case errors.Is(err, postgres.ErrValidation):
		return protocol.ReceiptCodeValidation
	case errors.Is(err, postgres.ErrUnauthorized):
		return protocol.ReceiptCodeUnauthorized
	case errors.Is(err, postgres.ErrInvalidNonce):
		return protocol.ReceiptCodeInvalidNonce
	case errors.Is(err, postgres.ErrInsufficientBalance):
		return protocol.ReceiptCodeInsufficientBalance
	case errors.Is(err, postgres.ErrNotFound):
		return protocol.ReceiptCodeNotFound
	case errors.Is(err, postgres.ErrDuplicateSubmission), errors.Is(err, postgres.ErrDuplicateTransaction):
		return protocol.ReceiptCodeConflict
	default:
		return protocol.ReceiptCodeInternal
	}
}
