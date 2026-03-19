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
		if status.ChainID != s.cfg.ChainID || status.NodeID == s.cfg.NodeID {
			continue
		}
		s.peers.RememberPeer(status)
		_ = s.store.UpsertPeer(ctx, status)
		peerStatuses = append(peerStatuses, struct {
			peer   string
			status protocol.PeerStatus
		}{peer: peer.ListenAddr, status: status})
	}

	for {
		plan, ok := s.selectSyncPlan(ctx, info, peerStatuses)
		if !ok {
			if !s.tryStateSync(ctx, info, peerStatuses) {
				return
			}
			info, err = s.store.GetChainInfo(ctx)
			if err != nil {
				return
			}
			continue
		}

		if err := s.store.ImportCertifiedBranch(ctx, plan.Bundles); err != nil {
			log.Printf("sync import certified branch fork_height=%d from=%s error=%v", plan.ForkHeight, plan.Source.NodeID, err)
			return
		}
		info, err = s.store.GetChainInfo(ctx)
		if err != nil {
			return
		}
	}
}

type certifiedBranch struct {
	Source protocol.PeerStatus
	Bundles []protocol.CertifiedBlock
}

type syncPlan struct {
	ForkHeight int64
	Source     protocol.PeerStatus
	Bundles    []protocol.CertifiedBlock
}

func (s *Service) selectSyncPlan(ctx context.Context, info protocol.ChainInfo, peerStatuses []struct {
	peer   string
	status protocol.PeerStatus
}) (syncPlan, bool) {
	plans := make([]syncPlan, 0)

	nextHeight := info.HeadHeight + 1
	forwardBranches := s.collectCertifiedBranches(ctx, peerStatuses, nextHeight, info.HeadHash)
	if len(forwardBranches) > 0 {
		plans = append(plans, syncPlan{
			ForkHeight: nextHeight,
			Source:     forwardBranches[0].Source,
			Bundles:    forwardBranches[0].Bundles,
		})
	}

	if s.cfg.ReorgPolicy == "best_certified" {
		start := syncStartHeight(info.HeadHeight, s.cfg.SyncLookaheadBlocks)
		for forkHeight := start; forkHeight <= info.HeadHeight; forkHeight++ {
			parentHash, err := s.parentHashForFork(ctx, info, forkHeight)
			if err != nil {
				continue
			}

			remoteBranches := s.collectCertifiedBranches(ctx, peerStatuses, forkHeight, parentHash)
			if len(remoteBranches) == 0 {
				continue
			}

			localBundles, err := s.store.ListCertifiedBlocksRange(ctx, forkHeight, s.cfg.SyncLookaheadBlocks)
			if err != nil || len(localBundles) == 0 {
				continue
			}
			if !betterBranch(remoteBranches[0], certifiedBranch{Source: protocol.PeerStatus{NodeID: s.cfg.NodeID}, Bundles: localBundles}) {
				continue
			}

			trimmed := trimCommonCertifiedPrefix(localBundles, remoteBranches[0].Bundles)
			if len(trimmed) == 0 {
				continue
			}

			plans = append(plans, syncPlan{
				ForkHeight: trimmed[0].Block.Header.Height,
				Source:     remoteBranches[0].Source,
				Bundles:    trimmed,
			})
		}
	}

	if len(plans) == 0 {
		return syncPlan{}, false
	}
	sort.Slice(plans, func(i, j int) bool {
		return betterSyncPlan(plans[i], plans[j])
	})
	return plans[0], true
}

func (s *Service) tryStateSync(ctx context.Context, info protocol.ChainInfo, peerStatuses []struct {
	peer   string
	status protocol.PeerStatus
}) bool {
	var best *protocol.PeerStatus
	for _, peer := range peerStatuses {
		if peer.status.HeadHeight <= info.HeadHeight {
			continue
		}
		if best == nil || peer.status.HeadHeight > best.HeadHeight || (peer.status.HeadHeight == best.HeadHeight && peer.status.NodeID < best.NodeID) {
			status := peer.status
			best = &status
		}
	}
	if best == nil {
		return false
	}

	snapshot, err := s.peers.FetchStateSnapshot(ctx, best.ListenAddr, s.cfg.SyncLookaheadBlocks)
	if err != nil {
		return false
	}
	if snapshot.ChainInfo.ChainID != s.cfg.ChainID {
		return false
	}
	for _, bundle := range snapshot.CertifiedWindow {
		if err := s.engine.VerifyCertifiedBlock(bundle); err != nil {
			return false
		}
	}
	if err := s.store.ImportStateSnapshot(ctx, snapshot); err != nil {
		log.Printf("sync state snapshot from=%s error=%v", best.NodeID, err)
		return false
	}
	return true
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

func (s *Service) parentHashForFork(ctx context.Context, info protocol.ChainInfo, forkHeight int64) (string, error) {
	if forkHeight <= 1 {
		return info.GenesisHash, nil
	}
	block, err := s.store.GetBlockByHeight(ctx, forkHeight-1)
	if err != nil {
		return "", err
	}
	return block.Hash, nil
}

func syncStartHeight(headHeight int64, lookahead int) int64 {
	start := headHeight - int64(lookahead) + 1
	if start < 1 {
		return 1
	}
	return start
}

func trimCommonCertifiedPrefix(local []protocol.CertifiedBlock, remote []protocol.CertifiedBlock) []protocol.CertifiedBlock {
	index := 0
	for index < len(local) && index < len(remote) {
		if local[index].Block.Hash != remote[index].Block.Hash {
			break
		}
		index++
	}
	if index >= len(remote) {
		return nil
	}
	return append([]protocol.CertifiedBlock(nil), remote[index:]...)
}

func betterSyncPlan(candidate syncPlan, current syncPlan) bool {
	if len(candidate.Bundles) == 0 {
		return false
	}
	if len(current.Bundles) == 0 {
		return true
	}

	candidateTip := candidate.Bundles[len(candidate.Bundles)-1]
	currentTip := current.Bundles[len(current.Bundles)-1]
	if candidateTip.Block.Header.Height != currentTip.Block.Header.Height {
		return candidateTip.Block.Header.Height > currentTip.Block.Header.Height
	}
	if preferredCertificate(candidateTip.Certificate, currentTip.Certificate) {
		return true
	}
	if preferredCertificate(currentTip.Certificate, candidateTip.Certificate) {
		return false
	}
	if len(candidate.Bundles) != len(current.Bundles) {
		return len(candidate.Bundles) > len(current.Bundles)
	}
	if candidate.ForkHeight != current.ForkHeight {
		return candidate.ForkHeight < current.ForkHeight
	}
	return candidate.Source.NodeID < current.Source.NodeID
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
