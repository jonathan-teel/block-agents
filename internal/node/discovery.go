package node

import (
	"context"
	"time"

	"aichain/internal/protocol"
)

func (s *Service) discoveryLoop(ctx context.Context) {
	if s.peers == nil {
		return
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	s.discoverOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.discoverOnce(ctx)
		}
	}
}

func (s *Service) discoverOnce(ctx context.Context) {
	if s.peers == nil {
		return
	}

	for _, peer := range s.peers.Peers() {
		discovered, err := s.peers.FetchPeers(ctx, peer.ListenAddr)
		if err != nil {
			continue
		}
		for _, candidate := range discovered {
			if candidate.NodeID == "" || candidate.ListenAddr == "" || candidate.ChainID != s.cfg.ChainID || candidate.NodeID == s.cfg.NodeID {
				continue
			}
			if s.engine != nil {
				if err := s.engine.VerifyPeerStatus(candidate); err != nil {
					continue
				}
			}
			candidate.ObservedAt = candidate.ObservedAt.UTC()
			if candidate.ObservedAt.IsZero() {
				candidate.ObservedAt = time.Now().UTC()
			}
			s.peers.RememberPeer(candidate)
			_ = s.store.UpsertPeer(ctx, candidate)
		}

		status, err := s.peers.FetchPeerStatus(ctx, peer.ListenAddr)
		if err != nil {
			continue
		}
		if status.NodeID == "" || status.NodeID == s.cfg.NodeID || status.ChainID != s.cfg.ChainID {
			continue
		}
		if s.engine != nil {
			if err := s.engine.VerifyPeerStatus(status); err != nil {
				continue
			}
		}
		if status.ObservedAt.IsZero() {
			status.ObservedAt = time.Now().UTC()
		}
		s.peers.RememberPeer(status)
		_ = s.store.UpsertPeer(ctx, status)
	}

	_ = s.store.UpsertPeer(ctx, protocol.PeerStatus{
		NodeID:           s.cfg.NodeID,
		ChainID:          s.cfg.ChainID,
		ListenAddr:       s.cfg.P2PListenAddr,
		ValidatorAddress: s.cfg.ValidatorAddress,
		ObservedAt:       time.Now().UTC(),
	})
}
