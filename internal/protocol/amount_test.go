package protocol

import (
	"encoding/json"
	"testing"
)

func TestAmountJSONRoundTrip(t *testing.T) {
	type payload struct {
		Amount Amount `json:"amount"`
	}

	var value payload
	if err := json.Unmarshal([]byte(`{"amount": "12.345678"}`), &value); err != nil {
		t.Fatalf("unmarshal amount: %v", err)
	}
	if value.Amount != Amount(12_345_678) {
		t.Fatalf("unexpected amount units: got %d want %d", value.Amount, Amount(12_345_678))
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal amount: %v", err)
	}
	if string(encoded) != `{"amount":12.345678}` {
		t.Fatalf("unexpected encoded amount: %s", encoded)
	}
}

func TestParseAmountString(t *testing.T) {
	value, err := ParseAmountString("100.25")
	if err != nil {
		t.Fatalf("parse amount: %v", err)
	}
	if value != Amount(100_250_000) {
		t.Fatalf("unexpected amount: got %d want %d", value, Amount(100_250_000))
	}
	if value.String() != "100.25" {
		t.Fatalf("unexpected amount string: %s", value.String())
	}
}
