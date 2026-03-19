package txauth

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestVerifyRotationProof(t *testing.T) {
	_, oldPrivateKey, _ := ed25519.GenerateKey(nil)
	newPublicKey, newPrivateKey, _ := ed25519.GenerateKey(nil)

	chainID := "blockagents-devnet-1"
	agent := "alice"
	oldPublicKeyHex := hex.EncodeToString(oldPrivateKey.Public().(ed25519.PublicKey))
	newPublicKeyHex := hex.EncodeToString(newPublicKey)
	nonce := int64(3)

	signature := ed25519.Sign(newPrivateKey, RotationSignBytes(chainID, agent, oldPublicKeyHex, newPublicKeyHex, nonce))
	if err := VerifyRotationProof(chainID, agent, oldPublicKeyHex, newPublicKeyHex, nonce, hex.EncodeToString(signature)); err != nil {
		t.Fatalf("verify rotation proof: %v", err)
	}
}
