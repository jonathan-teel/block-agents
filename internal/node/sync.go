package node

import (
	"context"
	"fmt"
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
		if s.engine != nil {
			if err := s.engine.VerifyPeerStatus(status); err != nil {
				continue
			}
		}
		s.peers.RememberPeer(status)
		_ = s.store.UpsertPeer(ctx, status)
		peerStatuses = append(peerStatuses, struct {
			peer   string
			status protocol.PeerStatus
		}{peer: peer.ListenAddr, status: status})
	}

	plan, ok := s.selectSyncPlan(ctx, info, peerStatuses)
	if !ok {
		s.tryStateSync(ctx, info, peerStatuses)
		return
	}

	if s.syncTracker != nil {
		targetHeight := plan.Bundles[len(plan.Bundles)-1].Block.Header.Height
		s.syncTracker.RecordAttempt("certified_branch", plan.Source.NodeID, plan.ForkHeight, targetHeight)
	}
	if err := s.store.ImportCertifiedBranch(ctx, plan.Bundles); err != nil {
		log.Printf("sync import certified branch fork_height=%d from=%s error=%v", plan.ForkHeight, plan.Source.NodeID, err)
		if s.syncTracker != nil {
			targetHeight := plan.Bundles[len(plan.Bundles)-1].Block.Header.Height
			s.syncTracker.RecordFailure("certified_branch", plan.Source.NodeID, plan.ForkHeight, targetHeight, err)
		}
		return
	}
	if err := s.engine.ReloadValidatorSet(ctx); err != nil {
		log.Printf("reload validator set after branch import error: %v", err)
	}
	if s.syncTracker != nil {
		importedHeight := plan.Bundles[len(plan.Bundles)-1].Block.Header.Height
		s.syncTracker.RecordSuccess("certified_branch", plan.Source.NodeID, plan.ForkHeight, importedHeight, importedHeight)
	}
}

type certifiedBranch struct {
	Source  protocol.PeerStatus
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

	for _, peer := range peerStatuses {
		plan, ok := s.buildSyncPlanForPeer(ctx, info, peer.status)
		if ok {
			plans = append(plans, plan)
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

func (s *Service) buildSyncPlanForPeer(ctx context.Context, info protocol.ChainInfo, peer protocol.PeerStatus) (syncPlan, bool) {
	if peer.HeadHeight <= 0 {
		return syncPlan{}, false
	}

	ancestorHeight, err := s.findCommonAncestor(ctx, info, peer)
	if err != nil {
		return syncPlan{}, false
	}
	if ancestorHeight >= peer.HeadHeight {
		return syncPlan{}, false
	}

	bundles, err := s.fetchCertifiedBranchFrom(ctx, peer.ListenAddr, ancestorHeight+1, peer.HeadHeight)
	if err != nil || len(bundles) == 0 {
		return syncPlan{}, false
	}
	if err := s.engine.VerifyCertifiedBranch(bundles); err != nil {
		log.Printf("sync verify certified branch from=%s fork=%d error=%v", peer.NodeID, ancestorHeight+1, err)
		return syncPlan{}, false
	}

	if ancestorHeight == info.HeadHeight {
		return syncPlan{
			ForkHeight: ancestorHeight + 1,
			Source:     peer,
			Bundles:    bundles,
		}, true
	}
	if s.cfg.ReorgPolicy != "best_certified" {
		return syncPlan{}, false
	}

	localBundles, err := s.store.ListCertifiedBlocksRange(ctx, ancestorHeight+1, int(info.HeadHeight-ancestorHeight))
	if err != nil {
		return syncPlan{}, false
	}
	if !betterBranch(certifiedBranch{Source: peer, Bundles: bundles}, certifiedBranch{
		Source:  protocol.PeerStatus{NodeID: s.cfg.NodeID},
		Bundles: localBundles,
	}) {
		return syncPlan{}, false
	}

	trimmed := trimCommonCertifiedPrefix(localBundles, bundles)
	if len(trimmed) == 0 {
		return syncPlan{}, false
	}
	return syncPlan{
		ForkHeight: trimmed[0].Block.Header.Height,
		Source:     peer,
		Bundles:    trimmed,
	}, true
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

	if s.syncTracker != nil {
		s.syncTracker.RecordAttempt("state_snapshot", best.NodeID, 0, best.HeadHeight)
	}
	snapshot, err := s.peers.FetchStateSnapshot(ctx, best.ListenAddr, s.cfg.SyncLookaheadBlocks)
	if err != nil {
		if s.syncTracker != nil {
			s.syncTracker.RecordFailure("state_snapshot", best.NodeID, 0, best.HeadHeight, err)
		}
		return false
	}
	if snapshot.ChainInfo.ChainID != s.cfg.ChainID {
		err := fmt.Errorf("snapshot chain_id %s does not match local chain", snapshot.ChainInfo.ChainID)
		if s.syncTracker != nil {
			s.syncTracker.RecordFailure("state_snapshot", best.NodeID, 0, best.HeadHeight, err)
		}
		return false
	}
	if err := s.engine.VerifyCertifiedBranch(snapshot.CertifiedWindow); err != nil {
		if s.syncTracker != nil {
			s.syncTracker.RecordFailure("state_snapshot", best.NodeID, 0, best.HeadHeight, err)
		}
		return false
	}
	if err := s.store.ImportStateSnapshot(ctx, snapshot); err != nil {
		log.Printf("sync state snapshot from=%s error=%v", best.NodeID, err)
		if s.syncTracker != nil {
			s.syncTracker.RecordFailure("state_snapshot", best.NodeID, 0, best.HeadHeight, err)
		}
		return false
	}
	if err := s.engine.ReloadValidatorSet(ctx); err != nil {
		log.Printf("reload validator set after snapshot import error: %v", err)
	}
	if s.syncTracker != nil {
		s.syncTracker.RecordSuccess("state_snapshot", best.NodeID, 0, best.HeadHeight, snapshot.HeadBlock.Header.Height)
	}
	return true
}

func (s *Service) findCommonAncestor(ctx context.Context, info protocol.ChainInfo, peer protocol.PeerStatus) (int64, error) {
	maxHeight := info.HeadHeight
	if peer.HeadHeight < maxHeight {
		maxHeight = peer.HeadHeight
	}

	for height := maxHeight; height >= 1; height-- {
		localBlock, err := s.store.GetBlockByHeight(ctx, height)
		if err != nil {
			continue
		}
		remoteBundle, err := s.peers.FetchCertifiedBlock(ctx, peer.ListenAddr, height)
		if err != nil {
			continue
		}
		if remoteBundle.Block.Hash == localBlock.Hash {
			return height, nil
		}
	}
	return 0, nil
}

func (s *Service) fetchCertifiedBranchFrom(ctx context.Context, listenAddr string, fromHeight int64, toHeight int64) ([]protocol.CertifiedBlock, error) {
	if toHeight < fromHeight {
		return nil, nil
	}

	pageSize := s.cfg.SyncLookaheadBlocks
	if pageSize <= 0 || pageSize > 64 {
		pageSize = 64
	}

	bundles := make([]protocol.CertifiedBlock, 0, toHeight-fromHeight+1)
	for current := fromHeight; current <= toHeight; {
		remaining := int(toHeight - current + 1)
		limit := pageSize
		if remaining < limit {
			limit = remaining
		}
		page, err := s.peers.FetchCertifiedBlocksRange(ctx, listenAddr, current, limit)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return nil, fmt.Errorf("peer returned an incomplete certified branch at height %d", current)
		}
		bundles = append(bundles, page...)
		current += int64(len(page))
	}

	return bundles, nil
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
