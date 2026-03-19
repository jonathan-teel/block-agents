package txauth

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"aichain/internal/protocol"
)

func TestVerifyTransaction(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	tx, err := protocol.NewTransaction(
		protocol.TxTypeCreateTask,
		"alice",
		struct {
			Creator    string          `json:"creator"`
			Type       string          `json:"type,omitempty"`
			Question   string          `json:"question"`
			Deadline   int64           `json:"deadline"`
			RewardPool protocol.Amount `json:"reward_pool"`
			MinStake   protocol.Amount `json:"min_stake"`
		}{
			Creator:    "alice",
			Type:       protocol.TaskTypeBlockAgents,
			Question:   "test question",
			Deadline:   1893456000,
			RewardPool: 10,
			MinStake:   1,
		},
		protocol.TxAuth{
			Nonce:     1,
			PublicKey: hex.EncodeToString(publicKey),
		},
		time.Unix(1700000000, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}

	signBytes, err := SignBytes("blockagents-devnet-1", tx)
	if err != nil {
		t.Fatalf("sign bytes: %v", err)
	}
	signature := ed25519.Sign(privateKey, signBytes)
	tx.Signature = hex.EncodeToString(signature)
	tx.Hash = protocol.BuildTransactionHash(tx)

	if err := VerifyTransaction("blockagents-devnet-1", tx); err != nil {
		t.Fatalf("verify transaction: %v", err)
	}
}
