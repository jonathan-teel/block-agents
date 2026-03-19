package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aichain/internal/protocol"
)

type Manager struct {
	client     *http.Client
	listenAddr string

	mu    sync.RWMutex
	peers map[string]protocol.PeerStatus
}

func New(listenAddr string) *Manager {
	return &Manager{
		client: &http.Client{Timeout: 5 * time.Second},
		listenAddr: strings.TrimSpace(listenAddr),
		peers:  make(map[string]protocol.PeerStatus),
	}
}

func (m *Manager) RememberPeer(status protocol.PeerStatus) {
	if strings.TrimSpace(status.ListenAddr) == "" || strings.TrimSpace(status.NodeID) == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[status.NodeID] = status
}

func (m *Manager) Peers() []protocol.PeerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peers := make([]protocol.PeerStatus, 0, len(m.peers))
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].NodeID < peers[j].NodeID
	})
	return peers
}

func (m *Manager) FindPeerByValidator(address string) (protocol.PeerStatus, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return protocol.PeerStatus{}, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, peer := range m.peers {
		if peer.ValidatorAddress == address {
			return peer, true
		}
	}
	return protocol.PeerStatus{}, false
}

func (m *Manager) BroadcastHello(ctx context.Context, hello protocol.PeerHello) {
	for _, peer := range m.Peers() {
		_ = m.postJSON(ctx, peer.ListenAddr, "/v1/p2p/hello", hello)
	}
}

func (m *Manager) BroadcastProposal(ctx context.Context, proposal protocol.ConsensusProposal) {
	for _, peer := range m.Peers() {
		_ = m.postJSON(ctx, peer.ListenAddr, "/v1/p2p/consensus/proposals", proposal)
	}
}

func (m *Manager) BroadcastVote(ctx context.Context, vote protocol.ConsensusVote) {
	for _, peer := range m.Peers() {
		_ = m.postJSON(ctx, peer.ListenAddr, "/v1/p2p/consensus/votes", vote)
	}
}

func (m *Manager) BroadcastRoundChange(ctx context.Context, roundChange protocol.ConsensusRoundChange) {
	for _, peer := range m.Peers() {
		_ = m.postJSON(ctx, peer.ListenAddr, "/v1/p2p/consensus/round-changes", roundChange)
	}
}

func (m *Manager) BroadcastCertifiedBlock(ctx context.Context, bundle protocol.CertifiedBlock) {
	for _, peer := range m.Peers() {
		_ = m.postJSON(ctx, peer.ListenAddr, "/v1/p2p/blocks/import", bundle)
	}
}

func (m *Manager) ListenAddr() string {
	return m.listenAddr
}

func (m *Manager) FetchPeerStatus(ctx context.Context, baseURL string) (protocol.PeerStatus, error) {
	var status protocol.PeerStatus
	if err := m.getJSON(ctx, baseURL, "/v1/p2p/status", &status); err != nil {
		return protocol.PeerStatus{}, err
	}
	return status, nil
}

func (m *Manager) FetchPeers(ctx context.Context, baseURL string) ([]protocol.PeerStatus, error) {
	peers := make([]protocol.PeerStatus, 0)
	if err := m.getJSON(ctx, baseURL, "/v1/p2p/peers", &peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func (m *Manager) FetchCertifiedBlock(ctx context.Context, baseURL string, height int64) (protocol.CertifiedBlock, error) {
	var bundle protocol.CertifiedBlock
	path := "/v1/p2p/blocks/" + strconv.FormatInt(height, 10) + "/certified"
	if err := m.getJSON(ctx, baseURL, path, &bundle); err != nil {
		return protocol.CertifiedBlock{}, err
	}
	return bundle, nil
}

func (m *Manager) FetchCertifiedBlocksRange(ctx context.Context, baseURL string, from int64, limit int) ([]protocol.CertifiedBlock, error) {
	bundles := make([]protocol.CertifiedBlock, 0)
	path := "/v1/p2p/blocks/certified?from=" + strconv.FormatInt(from, 10) + "&limit=" + strconv.Itoa(limit)
	if err := m.getJSON(ctx, baseURL, path, &bundles); err != nil {
		return nil, err
	}
	return bundles, nil
}

func (m *Manager) FetchCandidateBlock(ctx context.Context, baseURL string, hash string) (protocol.ConsensusCandidateBlock, error) {
	var bundle protocol.ConsensusCandidateBlock
	path := "/v1/p2p/candidates/" + strings.TrimSpace(hash)
	if err := m.getJSON(ctx, baseURL, path, &bundle); err != nil {
		return protocol.ConsensusCandidateBlock{}, err
	}
	return bundle, nil
}

func (m *Manager) FetchStateSnapshot(ctx context.Context, baseURL string, window int) (protocol.StateSnapshot, error) {
	var snapshot protocol.StateSnapshot
	path := "/v1/p2p/state/snapshot?window=" + strconv.Itoa(window)
	if err := m.getJSON(ctx, baseURL, path, &snapshot); err != nil {
		return protocol.StateSnapshot{}, err
	}
	return snapshot, nil
}

func (m *Manager) postJSON(ctx context.Context, baseURL string, path string, body any) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		return fmt.Errorf("p2p post %s returned status %d", path, response.StatusCode)
	}

	return nil
}

func (m *Manager) getJSON(ctx context.Context, baseURL string, path string, target any) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}

	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("p2p get %s returned status %d: %s", path, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(target)
}
