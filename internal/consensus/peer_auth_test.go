package consensus

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"aichain/internal/protocol"
)

func TestSignAndVerifyPeerHello(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	set := NewValidatorSet([]protocol.Validator{{
		Address:   "validator-1",
		PublicKey: hex.EncodeToString(publicKey),
		Power:     1,
	}})

	message := protocol.PeerHello{
		NodeID:           "node-1",
		ChainID:          "blockagents-devnet-1",
		ListenAddr:       "http://127.0.0.1:8080",
		ValidatorAddress: "validator-1",
		SeenAt:           time.Unix(1_700_000_000, 0).UTC(),
	}

	signature, err := SignPeerHello(message, privateKey)
	if err != nil {
		t.Fatalf("sign peer hello: %v", err)
	}
	message.Signature = signature

	if err := VerifyPeerHello(set, message); err != nil {
		t.Fatalf("verify peer hello: %v", err)
	}
}

func TestSignAndVerifyPeerStatus(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	set := NewValidatorSet([]protocol.Validator{{
		Address:   "validator-1",
		PublicKey: hex.EncodeToString(publicKey),
		Power:     1,
	}})

	status := protocol.PeerStatus{
		NodeID:           "node-1",
		ChainID:          "blockagents-devnet-1",
		ListenAddr:       "http://127.0.0.1:8080",
		ValidatorAddress: "validator-1",
		HeadHeight:       42,
		HeadHash:         "block-42",
		ObservedAt:       time.Unix(1_700_000_100, 0).UTC(),
	}

	signature, err := SignPeerStatus(status, privateKey)
	if err != nil {
		t.Fatalf("sign peer status: %v", err)
	}
	status.Signature = signature

	if err := VerifyPeerStatus(set, status); err != nil {
		t.Fatalf("verify peer status: %v", err)
	}
}

