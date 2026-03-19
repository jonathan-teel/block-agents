package node

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"aichain/internal/api/httpapi"
	"aichain/internal/config"
	"aichain/internal/consensus"
	"aichain/internal/network/p2p"
	"aichain/internal/protocol"
	"aichain/internal/storage/postgres"
)

type Service struct {
	cfg       config.Config
	store     *postgres.Store
	peers     *p2p.Manager
	engine    *consensus.Engine
	sequencer *consensus.Sequencer
	server    *http.Server
}

func New(cfg config.Config) (*Service, error) {
	store, err := postgres.New(cfg)
	if err != nil {
		return nil, err
	}

	peers := p2p.New(cfg.P2PListenAddr)
	loadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	knownPeers, err := store.ListKnownPeers(loadCtx, 256)
	if err == nil {
		for _, peer := range knownPeers {
			if peer.NodeID == cfg.NodeID {
				continue
			}
			peers.RememberPeer(peer)
		}
	}
	for _, seed := range cfg.SeedPeers {
		peers.RememberPeer(protocol.PeerStatus{
			NodeID:     seed,
			ChainID:    cfg.ChainID,
			ListenAddr: seed,
			ObservedAt: time.Now().UTC(),
		})
	}

	engine, err := consensus.NewEngine(cfg, peers, store)
	if err != nil {
		return nil, err
	}

	router := httpapi.NewRouter(store, cfg, peers, engine)
	server := &http.Server{
		Addr:              normalizeListenAddr(cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &Service{
		cfg:       cfg,
		store:     store,
		peers:     peers,
		engine:    engine,
		sequencer: consensus.New(cfg, store, engine),
		server:    server,
	}, nil
}

func (s *Service) Run(ctx context.Context) error {
	go s.sequencer.Run(ctx)
	go s.announceSelf(ctx)
	go s.discoveryLoop(ctx)
	go s.syncLoop(ctx)
	go s.engine.RunTimeoutLoop(ctx)

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	log.Printf("blockagents node=%s chain=%s listening on %s p2p=%s", s.cfg.NodeID, s.cfg.ChainID, s.server.Addr, s.cfg.P2PListenAddr)
	return s.server.ListenAndServe()
}

func (s *Service) Close() error {
	return s.store.Close()
}

func normalizeListenAddr(port string) string {
	if strings.HasPrefix(port, ":") {
		return port
	}
	return ":" + port
}

func (s *Service) announceSelf(ctx context.Context) {
	if s.peers == nil {
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	send := func() {
		s.peers.BroadcastHello(ctx, protocol.PeerHello{
			NodeID:           s.cfg.NodeID,
			ChainID:          s.cfg.ChainID,
			ListenAddr:       s.cfg.P2PListenAddr,
			ValidatorAddress: s.cfg.ValidatorAddress,
			SeenAt:           time.Now().UTC(),
		})
	}

	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}
