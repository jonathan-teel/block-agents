package operator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientChainInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chain/info" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chain_id":"blockagents-devnet-1","node_id":"node-1","head_height":7,"head_hash":"abc","genesis_hash":"gen","schema_version":4,"block_interval_seconds":5,"max_transactions_per_block":250,"faucet_enabled":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	info, err := client.ChainInfo(context.Background())
	if err != nil {
		t.Fatalf("chain info: %v", err)
	}
	if info.ChainID != "blockagents-devnet-1" {
		t.Fatalf("unexpected chain id %q", info.ChainID)
	}
	if info.HeadHeight != 7 {
		t.Fatalf("unexpected height %d", info.HeadHeight)
	}
}

func TestClientAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"missing tx","error_code":"not_found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	_, err := client.Transaction(context.Background(), "deadbeef")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("expected error code in %q", err.Error())
	}
}
