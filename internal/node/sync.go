package node

import (
	"context"
	"log"
	"sort"
	"time"

	"aichain/internal/protocol"
)

func (s *Service) syncLoop(ctx context.Context) {
	if s.peers == nil || s.engine == nil {
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	s.syncOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

func (s *Service) syncOnce(ctx context.Context) {
	info, err := s.store.GetChainInfo(ctx)
	if err != nil {
		return
	}

	peerStatuses := make([]struct {
		peer   string
		status protocol.PeerStatus
	}, 0)
	for _, peer := range s.peers.Peers() {
		status, err := s.peers.FetchPeerStatus(ctx, peer.ListenAddr)
		if err != nil {
			continue
		}
		s.peers.RememberPeer(status)
		peerStatuses = append(peerStatuses, struct {
			peer   string
			status protocol.PeerStatus
		}{peer: peer.ListenAddr, status: status})
	}

	for {
		nextHeight := info.HeadHeight + 1
		branches := s.collectCertifiedBranches(ctx, peerStatuses, nextHeight, info.HeadHash)
		if len(branches) == 0 {
			return
		}

		bestBranch := branches[0]
		imported := 0
		for _, bundle := range bestBranch.Bundles {
			if bundle.Block.Header.Height != info.HeadHeight+1 {
				break
			}
			if err := s.store.ImportCertifiedBlock(ctx, bundle); err != nil {
				log.Printf("sync import certified block height=%d from=%s error=%v", bundle.Block.Header.Height, bestBranch.Source.NodeID, err)
				return
			}
			info.HeadHeight = bundle.Block.Header.Height
			info.HeadHash = bundle.Block.Hash
			imported++
		}
		if imported == 0 {
			return
		}
	}
}

type certifiedBranch struct {
	Source protocol.PeerStatus
	Bundles []protocol.CertifiedBlock
}

func (s *Service) collectCertifiedBranches(ctx context.Context, peerStatuses []struct {
	peer   string
	status protocol.PeerStatus
}, nextHeight int64, parentHash string) []certifiedBranch {
	branches := make([]certifiedBranch, 0, len(peerStatuses))
	for _, peer := range peerStatuses {
		if peer.status.HeadHeight < nextHeight {
			continue
		}

		bundles, err := s.peers.FetchCertifiedBlocksRange(ctx, peer.status.ListenAddr, nextHeight, s.cfg.SyncLookaheadBlocks)
		if err != nil || len(bundles) == 0 {
			continue
		}

		branch := certifiedBranch{Source: peer.status}
		for index, bundle := range bundles {
			if err := s.engine.VerifyCertifiedBlock(bundle); err != nil {
				log.Printf("sync verify certified block height=%d from=%s error=%v", bundle.Block.Header.Height, peer.status.NodeID, err)
				break
			}
			expectedHeight := nextHeight + int64(index)
			if bundle.Block.Header.Height != expectedHeight {
				break
			}
			if index == 0 && bundle.Block.Header.ParentHash != parentHash {
				break
			}
			if index > 0 {
				parent := branch.Bundles[index-1].Block.Hash
				if bundle.Block.Header.ParentHash != parent {
					break
				}
			}
			branch.Bundles = append(branch.Bundles, bundle)
		}
		if len(branch.Bundles) > 0 {
			branches = append(branches, branch)
		}
	}

	sort.Slice(branches, func(i, j int) bool {
		return betterBranch(branches[i], branches[j])
	})
	return branches
}

func betterBranch(candidate certifiedBranch, current certifiedBranch) bool {
	if len(candidate.Bundles) != len(current.Bundles) {
		return len(candidate.Bundles) > len(current.Bundles)
	}
	if len(candidate.Bundles) == 0 {
		return false
	}

	candidateTip := candidate.Bundles[len(candidate.Bundles)-1].Certificate
	currentTip := current.Bundles[len(current.Bundles)-1].Certificate
	if preferredCertificate(candidateTip, currentTip) {
		return true
	}
	if preferredCertificate(currentTip, candidateTip) {
		return false
	}

	var candidatePower int64
	for _, bundle := range candidate.Bundles {
		candidatePower += bundle.Certificate.Power
	}
	var currentPower int64
	for _, bundle := range current.Bundles {
		currentPower += bundle.Certificate.Power
	}
	if candidatePower != currentPower {
		return candidatePower > currentPower
	}

	return candidate.Source.NodeID < current.Source.NodeID
}

func preferredCertificate(candidate protocol.QuorumCertificate, current protocol.QuorumCertificate) bool {
	if candidate.Height != current.Height {
		return candidate.Height > current.Height
	}
	if candidate.Round != current.Round {
		return candidate.Round > current.Round
	}
	if candidate.Power != current.Power {
		return candidate.Power > current.Power
	}
	if !candidate.CertifiedAt.Equal(current.CertifiedAt) {
		return candidate.CertifiedAt.Before(current.CertifiedAt)
	}
	return candidate.BlockHash < current.BlockHash
}
