package node

import (
	"testing"
	"time"

	"aichain/internal/protocol"
)

func TestTrimCommonCertifiedPrefix(t *testing.T) {
	local := []protocol.CertifiedBlock{
		testCertifiedBlock(4, "parent-3", "block-4a", 1, 2),
		testCertifiedBlock(5, "block-4a", "block-5a", 1, 2),
	}
	remote := []protocol.CertifiedBlock{
		testCertifiedBlock(4, "parent-3", "block-4a", 1, 2),
		testCertifiedBlock(5, "block-4a", "block-5b", 2, 2),
		testCertifiedBlock(6, "block-5b", "block-6b", 2, 2),
	}

	trimmed := trimCommonCertifiedPrefix(local, remote)
	if len(trimmed) != 2 {
		t.Fatalf("expected two divergent bundles, got %d", len(trimmed))
	}
	if trimmed[0].Block.Header.Height != 5 {
		t.Fatalf("expected divergence at height 5, got %d", trimmed[0].Block.Header.Height)
	}
	if trimmed[0].Block.Hash != "block-5b" {
		t.Fatalf("expected divergent block hash block-5b, got %s", trimmed[0].Block.Hash)
	}
}

func TestBetterSyncPlanPrefersHigherTipHeight(t *testing.T) {
	current := syncPlan{
		ForkHeight: 5,
		Source:     protocol.PeerStatus{NodeID: "peer-a"},
		Bundles: []protocol.CertifiedBlock{
			testCertifiedBlock(5, "block-4", "block-5a", 1, 2),
			testCertifiedBlock(6, "block-5a", "block-6a", 1, 2),
		},
	}
	candidate := syncPlan{
		ForkHeight: 5,
		Source:     protocol.PeerStatus{NodeID: "peer-b"},
		Bundles: []protocol.CertifiedBlock{
			testCertifiedBlock(5, "block-4", "block-5b", 2, 2),
			testCertifiedBlock(6, "block-5b", "block-6b", 2, 2),
			testCertifiedBlock(7, "block-6b", "block-7b", 2, 2),
		},
	}

	if !betterSyncPlan(candidate, current) {
		t.Fatal("expected higher-tip branch to be preferred")
	}
}

func testCertifiedBlock(height int64, parentHash string, hash string, round int, power int64) protocol.CertifiedBlock {
	timestamp := time.Unix(1_700_000_000+height, 0).UTC()
	return protocol.CertifiedBlock{
		Block: protocol.Block{
			Hash: hash,
			Header: protocol.BlockHeader{
				ChainID:    "blockagents-devnet-1",
				Height:     height,
				ParentHash: parentHash,
				Timestamp:  timestamp,
				Proposer:   "validator-1",
			},
		},
		Certificate: protocol.QuorumCertificate{
			ChainID:     "blockagents-devnet-1",
			Height:      height,
			Round:       round,
			BlockHash:   hash,
			VoteType:    "precommit",
			Signers:     []string{"validator-1", "validator-2"},
			Power:       power,
			Threshold:   2,
			CertifiedAt: timestamp,
		},
	}
}
