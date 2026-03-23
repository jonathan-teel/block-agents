package operator

import (
	"bytes"
	"strings"
	"testing"

	"aichain/internal/protocol"
)

func TestRenderChainInfo(t *testing.T) {
	var out bytes.Buffer
	renderChainInfo(&out, protocol.ChainInfo{
		ChainID:                 "blockagents-devnet-1",
		NodeID:                  "node-1",
		HeadHeight:              42,
		HeadHash:                "abcdef",
		GenesisHash:             "123456",
		SchemaVersion:           4,
		BlockIntervalSeconds:    5,
		MaxTransactionsPerBlock: 250,
		FaucetEnabled:           true,
		RoleSelectionPolicy:     "balance_reputation",
		MinerVotePolicy:         "reputation_weighted",
		ReorgPolicy:             "best_certified",
	})

	rendered := out.String()
	for _, fragment := range []string{"Chain", "blockagents-devnet-1", "balance_reputation", "best_certified"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing fragment %q in output %q", fragment, rendered)
		}
	}
}

func TestSanitizeDatabaseURL(t *testing.T) {
	value := sanitizeDatabaseURL("postgres://postgres:secret@localhost:5432/blockagents?sslmode=disable")
	if strings.Contains(value, "secret") {
		t.Fatalf("expected password to be redacted: %s", value)
	}
	if !strings.Contains(value, "xxxxx") {
		t.Fatalf("expected redacted password marker: %s", value)
	}
}
