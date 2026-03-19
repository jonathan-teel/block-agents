package txauth

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"aichain/internal/protocol"
)

func IsSigned(tx protocol.Transaction) bool {
	return tx.Nonce > 0 || strings.TrimSpace(tx.PublicKey) != "" || strings.TrimSpace(tx.Signature) != ""
}

func VerifyTransaction(chainID string, tx protocol.Transaction) error {
	if strings.TrimSpace(chainID) == "" {
		return fmt.Errorf("chain_id is required")
	}
	if strings.TrimSpace(tx.Sender) == "" {
		return fmt.Errorf("sender is required")
	}
	if tx.Nonce <= 0 {
		return fmt.Errorf("nonce must be > 0")
	}
	if strings.TrimSpace(tx.PublicKey) == "" {
		return fmt.Errorf("public_key is required")
	}
	if strings.TrimSpace(tx.Signature) == "" {
		return fmt.Errorf("signature is required")
	}

	publicKey, err := decodeHex(tx.PublicKey, ed25519.PublicKeySize)
	if err != nil {
		return fmt.Errorf("decode public_key: %w", err)
	}
	signature, err := decodeHex(tx.Signature, ed25519.SignatureSize)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	signBytes, err := SignBytes(chainID, tx)
	if err != nil {
		return fmt.Errorf("build sign bytes: %w", err)
	}
	if !ed25519.Verify(publicKey, signBytes, signature) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

func SignBytes(chainID string, tx protocol.Transaction) ([]byte, error) {
	type signableTransaction struct {
		ChainID   string          `json:"chain_id"`
		Type      protocol.TxType `json:"type"`
		Sender    string          `json:"sender"`
		Nonce     int64           `json:"nonce"`
		PublicKey string          `json:"public_key"`
		Payload   json.RawMessage `json:"payload"`
	}

	return json.Marshal(signableTransaction{
		ChainID:   strings.TrimSpace(chainID),
		Type:      tx.Type,
		Sender:    strings.TrimSpace(tx.Sender),
		Nonce:     tx.Nonce,
		PublicKey: strings.ToLower(strings.TrimSpace(tx.PublicKey)),
		Payload:   tx.Payload,
	})
}

func NormalizePublicKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func decodeHex(value string, expectedLen int) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(decoded) != expectedLen {
		return nil, fmt.Errorf("expected %d bytes, got %d", expectedLen, len(decoded))
	}
	return decoded, nil
}
