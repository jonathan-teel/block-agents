package oracle

import (
	"context"
	"testing"
)

func TestExtractNumericPath(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"markets": []any{
				map[string]any{"price": 0.25},
				map[string]any{"price": "0.75"},
			},
		},
	}

	value, err := extractNumericPath(payload, "data.markets.1.price")
	if err != nil {
		t.Fatalf("extractNumericPath returned error: %v", err)
	}
	if value != 0.75 {
		t.Fatalf("expected 0.75, got %v", value)
	}
}

func TestValidateURLRejectsLoopbackByDefault(t *testing.T) {
	adapter := NewHTTPJSONAdapter(1, false)
	err := adapter.validateURL(context.Background(), "http://127.0.0.1:8080/data")
	if err == nil {
		t.Fatalf("expected loopback oracle endpoint to be rejected")
	}
}

func TestValidateURLAllowsLoopbackWhenEnabled(t *testing.T) {
	adapter := NewHTTPJSONAdapter(1, true)
	if err := adapter.validateURL(context.Background(), "http://127.0.0.1:8080/data"); err != nil {
		t.Fatalf("expected loopback oracle endpoint to be allowed, got %v", err)
	}
}
