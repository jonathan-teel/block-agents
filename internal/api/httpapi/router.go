package httpapi

import (
	"github.com/gin-gonic/gin"

	"aichain/internal/config"
	"aichain/internal/consensus"
	"aichain/internal/network/p2p"
	"aichain/internal/storage/postgres"
)

func NewRouter(store *postgres.Store, cfg config.Config, peers *p2p.Manager, engine *consensus.Engine) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	handler := NewHandler(store, cfg, peers, engine)

	router.GET("/healthz", handler.Health)

	router.GET("/v1/chain/info", handler.GetChainInfo)
	router.GET("/v1/blocks/head", handler.GetHeadBlock)
	router.GET("/v1/blocks/:height", handler.GetBlockByHeight)
	router.GET("/v1/txs/:hash", handler.GetTransaction)
	router.GET("/v1/tasks/open", handler.ListOpenTasks)
	router.GET("/v1/tasks/:id", handler.GetTask)
	router.GET("/v1/agents/:address", handler.GetAgent)
	router.GET("/v1/p2p/status", handler.GetPeerStatus)
	router.GET("/v1/p2p/peers", handler.ListPeers)
	router.GET("/v1/p2p/candidates/:hash", handler.GetCandidateBlockByHash)
	router.GET("/v1/p2p/state/snapshot", handler.ExportStateSnapshot)
	router.GET("/v1/p2p/blocks/certified", handler.GetCertifiedBlocksRange)
	router.GET("/v1/p2p/blocks/:height/certified", handler.GetCertifiedBlockByHeight)
	router.GET("/v1/consensus/validators", handler.ListValidators)
	router.GET("/v1/consensus/certificates", handler.ListCertificates)
	router.GET("/v1/consensus/fork-choice", handler.ListForkChoice)
	router.GET("/v1/consensus/round-changes", handler.ListRoundChanges)
	router.GET("/v1/consensus/evidence", handler.ListEvidence)
	router.POST("/v1/txs/tasks", handler.CreateTaskTx)
	router.POST("/v1/txs/submissions", handler.CreateSubmissionTx)
	router.POST("/v1/txs/proposals", handler.CreateProposalTx)
	router.POST("/v1/txs/evaluations", handler.CreateEvaluationTx)
	router.POST("/v1/txs/votes", handler.CreateVoteTx)
	router.POST("/v1/txs/proofs", handler.CreateProofTx)
	router.POST("/v1/txs/agent/bootstrap", handler.BootstrapAgentKeyTx)
	router.POST("/v1/txs/agent/rotate-key", handler.RotateAgentKeyTx)
	router.POST("/v1/p2p/hello", handler.PeerHello)
	router.POST("/v1/p2p/consensus/proposals", handler.ReceiveConsensusProposal)
	router.POST("/v1/p2p/consensus/votes", handler.ReceiveConsensusVote)
	router.POST("/v1/p2p/consensus/round-changes", handler.ReceiveConsensusRoundChange)
	router.POST("/v1/p2p/state/import", handler.ImportStateSnapshot)
	router.POST("/v1/p2p/blocks/import", handler.ImportCertifiedBlock)
	router.POST("/v1/dev/faucet", handler.FaucetGrant)

	router.POST("/task", handler.CreateTaskTx)
	router.GET("/tasks/open", handler.ListOpenTasks)
	router.POST("/submit", handler.CreateSubmissionTx)
	router.GET("/tasks/:id", handler.GetTask)

	return router
}
