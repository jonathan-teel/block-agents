package protocol

import (
	"testing"
	"time"
)

func TestComputeMerkleRoot(t *testing.T) {
	root := ComputeMerkleRoot([]string{"aa", "bb", "cc"})
	if root == "" {
		t.Fatal("expected non-empty merkle root")
	}
}

func TestValidateBlock(t *testing.T) {
	parentHeader := BlockHeader{
		ChainID:    "blockagents-devnet-1",
		Height:     0,
		ParentHash: ZeroHash,
		Timestamp:  time.Unix(1700000000, 0).UTC(),
	}
	parentHeader.AppHash = BuildAppHash(parentHeader)
	parent := Block{
		Hash:   BuildBlockHash(parentHeader),
		Header: parentHeader,
	}

	tx, err := NewTransaction(TxTypeFundAgent, "faucet", struct {
		Agent  string `json:"agent"`
		Amount Amount `json:"amount"`
	}{
		Agent:  "alice",
		Amount: 100,
	}, TxAuth{}, parentHeader.Timestamp)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}

	header := BlockHeader{
		ChainID:    parent.Header.ChainID,
		Height:     1,
		ParentHash: parent.Hash,
		Timestamp:  parentHeader.Timestamp.Add(5 * time.Second),
		Proposer:   "node-1",
		TxRoot:     ComputeMerkleRoot([]string{tx.Hash}),
		StateRoot:  HashStrings([]string{"agent|alice|100|0.50000000"}),
		TxCount:    1,
	}
	header.AppHash = BuildAppHash(header)

	block := Block{
		Hash:         BuildBlockHash(header),
		Header:       header,
		Transactions: []Transaction{tx},
		Receipts:     []Receipt{{TxHash: tx.Hash, BlockHeight: 1, Success: true}},
	}

	if err := ValidateBlock(&parent, block); err != nil {
		t.Fatalf("validate block: %v", err)
	}
}
