package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ZeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func HashValue(value any) string {
	payload, _ := json.Marshal(value)
	return HashBytes(payload)
}

func HashStrings(parts []string) string {
	return HashBytes([]byte(strings.Join(parts, "\n")))
}

func NewTransaction(txType TxType, sender string, payload any, auth TxAuth, acceptedAt time.Time) (Transaction, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return Transaction{}, fmt.Errorf("marshal transaction payload: %w", err)
	}

	tx := Transaction{
		Hash:       "",
		Type:       txType,
		Sender:     strings.TrimSpace(sender),
		Nonce:      auth.Nonce,
		PublicKey:  normalizeHexString(auth.PublicKey),
		Signature:  normalizeHexString(auth.Signature),
		Payload:    payloadBytes,
		AcceptedAt: acceptedAt.UTC(),
	}
	tx.Hash = BuildTransactionHash(tx)

	return tx, nil
}

func BuildTransactionHash(tx Transaction) string {
	if tx.Nonce == 0 && normalizeHexString(tx.PublicKey) == "" && normalizeHexString(tx.Signature) == "" {
		type canonicalUnsignedTransaction struct {
			Type       TxType          `json:"type"`
			Sender     string          `json:"sender"`
			Payload    json.RawMessage `json:"payload"`
			AcceptedAt time.Time       `json:"accepted_at"`
		}

		return HashValue(canonicalUnsignedTransaction{
			Type:       tx.Type,
			Sender:     strings.TrimSpace(tx.Sender),
			Payload:    tx.Payload,
			AcceptedAt: tx.AcceptedAt.UTC(),
		})
	}

	type canonicalTransaction struct {
		Type      TxType          `json:"type"`
		Sender    string          `json:"sender"`
		Nonce     int64           `json:"nonce"`
		PublicKey string          `json:"public_key,omitempty"`
		Signature string          `json:"signature,omitempty"`
		Payload   json.RawMessage `json:"payload"`
	}

	return HashValue(canonicalTransaction{
		Type:      tx.Type,
		Sender:    strings.TrimSpace(tx.Sender),
		Nonce:     tx.Nonce,
		PublicKey: normalizeHexString(tx.PublicKey),
		Signature: normalizeHexString(tx.Signature),
		Payload:   tx.Payload,
	})
}

func ComputeMerkleRoot(hashes []string) string {
	if len(hashes) == 0 {
		return HashBytes([]byte("aichain:empty"))
	}

	level := append([]string(nil), hashes...)
	for len(level) > 1 {
		next := make([]string, 0, (len(level)+1)/2)
		for index := 0; index < len(level); index += 2 {
			left := level[index]
			right := left
			if index+1 < len(level) {
				right = level[index+1]
			}
			next = append(next, HashBytes([]byte(left+right)))
		}
		level = next
	}

	return level[0]
}

func BuildAppHash(header BlockHeader) string {
	type canonicalAppHash struct {
		ChainID    string    `json:"chain_id"`
		Height     int64     `json:"height"`
		ParentHash string    `json:"parent_hash"`
		Timestamp  time.Time `json:"timestamp"`
		Proposer   string    `json:"proposer"`
		TxRoot     string    `json:"tx_root"`
		StateRoot  string    `json:"state_root"`
		TxCount    int       `json:"tx_count"`
	}

	return HashValue(canonicalAppHash{
		ChainID:    header.ChainID,
		Height:     header.Height,
		ParentHash: header.ParentHash,
		Timestamp:  header.Timestamp.UTC().Truncate(time.Second),
		Proposer:   header.Proposer,
		TxRoot:     header.TxRoot,
		StateRoot:  header.StateRoot,
		TxCount:    header.TxCount,
	})
}

func BuildBlockHash(header BlockHeader) string {
	type canonicalBlockHash struct {
		ChainID    string    `json:"chain_id"`
		Height     int64     `json:"height"`
		ParentHash string    `json:"parent_hash"`
		Timestamp  time.Time `json:"timestamp"`
		Proposer   string    `json:"proposer"`
		TxRoot     string    `json:"tx_root"`
		StateRoot  string    `json:"state_root"`
		AppHash    string    `json:"app_hash"`
		TxCount    int       `json:"tx_count"`
	}

	return HashValue(canonicalBlockHash{
		ChainID:    header.ChainID,
		Height:     header.Height,
		ParentHash: header.ParentHash,
		Timestamp:  header.Timestamp.UTC().Truncate(time.Second),
		Proposer:   header.Proposer,
		TxRoot:     header.TxRoot,
		StateRoot:  header.StateRoot,
		AppHash:    header.AppHash,
		TxCount:    header.TxCount,
	})
}

func ValidateBlock(parent *Block, block Block) error {
	if block.Header.ChainID == "" {
		return fmt.Errorf("block chain_id is required")
	}
	if block.Header.Height < 0 {
		return fmt.Errorf("block height must be >= 0")
	}
	if block.Header.TxCount != len(block.Transactions) {
		return fmt.Errorf("block tx_count does not match transaction list")
	}
	if len(block.Receipts) != len(block.Transactions) {
		return fmt.Errorf("block receipt count does not match transaction list")
	}

	txHashes := make([]string, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		if BuildTransactionHash(tx) != tx.Hash {
			return fmt.Errorf("transaction hash mismatch for %s", tx.Hash)
		}
		txHashes = append(txHashes, tx.Hash)
	}

	if ComputeMerkleRoot(txHashes) != block.Header.TxRoot {
		return fmt.Errorf("block tx_root mismatch")
	}
	if BuildAppHash(block.Header) != block.Header.AppHash {
		return fmt.Errorf("block app_hash mismatch")
	}
	if BuildBlockHash(block.Header) != block.Hash {
		return fmt.Errorf("block hash mismatch")
	}

	if parent == nil {
		if block.Header.Height != 0 {
			return fmt.Errorf("genesis block must be height 0")
		}
		if block.Header.ParentHash != ZeroHash {
			return fmt.Errorf("genesis block parent hash must be zero hash")
		}
		return nil
	}

	if block.Header.ChainID != parent.Header.ChainID {
		return fmt.Errorf("block chain_id does not match parent")
	}
	if block.Header.Height != parent.Header.Height+1 {
		return fmt.Errorf("block height is not sequential")
	}
	if block.Header.ParentHash != parent.Hash {
		return fmt.Errorf("block parent hash mismatch")
	}

	return nil
}

func normalizeHexString(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
