package txauth

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func RotationSignBytes(chainID string, agent string, oldPublicKey string, newPublicKey string, nonce int64) ([]byte, error) {
	type signableRotation struct {
		ChainID      string `json:"chain_id"`
		Agent        string `json:"agent"`
		OldPublicKey string `json:"old_public_key"`
		NewPublicKey string `json:"new_public_key"`
		Nonce        int64  `json:"nonce"`
	}

	return json.Marshal(signableRotation{
		ChainID:      strings.TrimSpace(chainID),
		Agent:        strings.TrimSpace(agent),
		OldPublicKey: NormalizePublicKey(oldPublicKey),
		NewPublicKey: NormalizePublicKey(newPublicKey),
		Nonce:        nonce,
	})
}

func VerifyRotationProof(chainID string, agent string, oldPublicKey string, newPublicKey string, nonce int64, signatureHex string) error {
	publicKey, err := decodeHex(newPublicKey, ed25519.PublicKeySize)
	if err != nil {
		return fmt.Errorf("decode new_public_key: %w", err)
	}
	signature, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("decode new_signature: invalid signature")
	}
	signBytes, err := RotationSignBytes(chainID, agent, oldPublicKey, newPublicKey, nonce)
	if err != nil {
		return fmt.Errorf("build rotation sign bytes: %w", err)
	}
	if !ed25519.Verify(publicKey, signBytes, signature) {
		return fmt.Errorf("new_signature verification failed")
	}
	return nil
}
