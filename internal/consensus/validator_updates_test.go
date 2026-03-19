package consensus

import (
	"encoding/json"
	"testing"

	"aichain/internal/protocol"
)

func TestApplyValidatorUpdates(t *testing.T) {
	upsertPayload, err := json.Marshal(struct {
		Operator  string `json:"operator"`
		Validator string `json:"validator"`
		PublicKey string `json:"public_key"`
		Power     int64  `json:"power"`
	}{
		Operator:  "validator-1",
		Validator: "validator-2",
		PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Power:     2,
	})
	if err != nil {
		t.Fatalf("marshal upsert payload: %v", err)
	}
	deactivatePayload, err := json.Marshal(struct {
		Operator  string `json:"operator"`
		Validator string `json:"validator"`
	}{
		Operator:  "validator-1",
		Validator: "validator-1",
	})
	if err != nil {
		t.Fatalf("marshal deactivate payload: %v", err)
	}

	block := protocol.Block{
		Transactions: []protocol.Transaction{
			{Type: protocol.TxTypeUpsertValidator, Payload: upsertPayload},
			{Type: protocol.TxTypeDeactivateValidator, Payload: deactivatePayload},
		},
		Receipts: []protocol.Receipt{
			{Success: true},
			{Success: true},
		},
	}

	next, err := ApplyValidatorUpdates([]protocol.Validator{{
		Address:   "validator-1",
		PublicKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Power:     1,
		Active:    true,
	}}, block)
	if err != nil {
		t.Fatalf("apply validator updates: %v", err)
	}
	if len(next) != 1 {
		t.Fatalf("expected one validator after updates, got %d", len(next))
	}
	if next[0].Address != "validator-2" {
		t.Fatalf("expected validator-2 to remain active, got %s", next[0].Address)
	}
	if next[0].Power != 2 {
		t.Fatalf("expected validator-2 power 2, got %d", next[0].Power)
	}
}

