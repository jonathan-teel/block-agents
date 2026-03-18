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
}

func NewHandler(store *postgres.Store, cfg config.Config, peers *p2p.Manager, engine *consensus.Engine) *Handler {
	return &Handler{
		store:  store,
		cfg:    cfg,
		peers:  peers,
		engine: engine,
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

	c.JSON(http.StatusOK, protocol.PeerStatus{
		NodeID:           h.cfg.NodeID,
		ChainID:          h.cfg.ChainID,
		ListenAddr:       h.cfg.P2PListenAddr,
		ValidatorAddress: h.cfg.ValidatorAddress,
		HeadHeight:       info.HeadHeight,
		HeadHash:         info.HeadHash,
		ObservedAt:       nowUTC(),
	})
}

func (h *Handler) ListPeers(c *gin.Context) {
	if h.peers == nil {
		c.JSON(http.StatusOK, []protocol.PeerStatus{})
		return
	}
	c.JSON(http.StatusOK, h.peers.Peers())
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

	bundles := make([]protocol.CertifiedBlock, 0, limit)
	for height := from; height < from+int64(limit); height++ {
		bundle, err := h.store.GetCertifiedBlockByHeight(c.Request.Context(), height)
		if err != nil {
			if errors.Is(err, postgres.ErrNotFound) {
				break
			}
			writeError(c, err)
			return
		}
		bundles = append(bundles, bundle)
	}

	c.JSON(http.StatusOK, bundles)
}

func (h *Handler) ListValidators(c *gin.Context) {
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
	if h.peers != nil {
		h.peers.RememberPeer(protocol.PeerStatus{
			NodeID:           hello.NodeID,
			ChainID:          hello.ChainID,
			ListenAddr:       hello.ListenAddr,
			ValidatorAddress: hello.ValidatorAddress,
			ObservedAt:       hello.SeenAt,
		})
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

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrInsufficientBalance):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrDuplicateSubmission):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrDuplicateTransaction):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrFaucetDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, postgres.ErrInvalidNonce):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
