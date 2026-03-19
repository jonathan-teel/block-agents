package p2p

import (
	"errors"
	"testing"
	"time"

	"aichain/internal/protocol"
)

func TestAllowHelloRateLimit(t *testing.T) {
	manager := New("http://127.0.0.1:8080", Options{
		HelloMinInterval: 50 * time.Millisecond,
	})

	now := time.Now().UTC()
	if !manager.AllowHello("node-1", now) {
		t.Fatal("expected first hello to be admitted")
	}
	if manager.AllowHello("node-1", now.Add(10*time.Millisecond)) {
		t.Fatal("expected second hello inside interval to be rejected")
	}
	if !manager.AllowHello("node-1", now.Add(60*time.Millisecond)) {
		t.Fatal("expected hello after interval to be admitted")
	}
}

func TestBroadcastDedupWindow(t *testing.T) {
	manager := New("http://127.0.0.1:8080", Options{
		BroadcastDedupTTL: 20 * time.Millisecond,
	})

	if !manager.shouldBroadcast("proposal/1/0/block-a") {
		t.Fatal("expected first broadcast to pass dedup")
	}
	if manager.shouldBroadcast("proposal/1/0/block-a") {
		t.Fatal("expected duplicate broadcast inside TTL to be rejected")
	}
	time.Sleep(25 * time.Millisecond)
	if !manager.shouldBroadcast("proposal/1/0/block-a") {
		t.Fatal("expected broadcast after TTL to pass dedup")
	}
}

func TestPeerBackoffAndTelemetry(t *testing.T) {
	manager := New("http://127.0.0.1:8080", Options{
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  20 * time.Millisecond,
	})
	manager.RememberPeer(protocol.PeerStatus{
		NodeID:     "node-1",
		ChainID:    "blockagents-devnet-1",
		ListenAddr: "http://127.0.0.1:8081",
		ObservedAt: time.Now().UTC(),
	})

	manager.recordFailure("http://127.0.0.1:8081", errors.New("boom"))
	if manager.shouldAttempt("http://127.0.0.1:8081") {
		t.Fatal("expected peer to be in backoff after failure")
	}

	telemetry := manager.PeerTelemetry()
	if len(telemetry) != 1 {
		t.Fatalf("expected one telemetry record, got %d", len(telemetry))
	}
	if telemetry[0].ConsecutiveFailures != 1 {
		t.Fatalf("expected one failure, got %d", telemetry[0].ConsecutiveFailures)
	}
	if telemetry[0].LastError != "boom" {
		t.Fatalf("expected last error boom, got %q", telemetry[0].LastError)
	}

	time.Sleep(25 * time.Millisecond)
	manager.recordSuccess("http://127.0.0.1:8081")
	if !manager.shouldAttempt("http://127.0.0.1:8081") {
		t.Fatal("expected peer to leave backoff after success")
	}
}

