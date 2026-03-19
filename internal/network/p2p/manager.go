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

type Options struct {
	BaseBackoff      time.Duration
	MaxBackoff       time.Duration
	BroadcastDedupTTL time.Duration
	HelloMinInterval time.Duration
	AllowPrivateEndpoints bool
	MaxResponseBytes      int64
}

type peerRuntime struct {
	score               int
	consecutiveFailures int
	lastAttemptAt       *time.Time
	lastSuccessAt       *time.Time
	lastFailureAt       *time.Time
	lastInboundHelloAt  *time.Time
	backoffUntil        *time.Time
	lastError           string
}

type Manager struct {
	client     *http.Client
	listenAddr string
	opts       Options

	mu              sync.RWMutex
	peers           map[string]protocol.PeerStatus
	runtime         map[string]*peerRuntime
	broadcastDedups map[string]time.Time
}

func New(listenAddr string, opts Options) *Manager {
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 2 * time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 1 * time.Minute
	}
	if opts.BroadcastDedupTTL <= 0 {
		opts.BroadcastDedupTTL = 30 * time.Second
	}
	if opts.HelloMinInterval <= 0 {
		opts.HelloMinInterval = 3 * time.Second
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = 16 << 20
	}

	manager := &Manager{
		listenAddr: strings.TrimSpace(listenAddr),
		opts:       opts,
		peers:      make(map[string]protocol.PeerStatus),
		runtime:    make(map[string]*peerRuntime),
		broadcastDedups: make(map[string]time.Time),
	}
	client := &http.Client{Timeout: 5 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		_, err := normalizePeerBaseURL(req.Context(), req.URL.String(), opts.AllowPrivateEndpoints)
		return err
	}
	manager.client = client
	return manager
}

func (m *Manager) RememberPeer(status protocol.PeerStatus) {
	status.NodeID = strings.TrimSpace(status.NodeID)
	status.ChainID = strings.TrimSpace(status.ChainID)
	status.GenesisHash = strings.TrimSpace(status.GenesisHash)
	status.ValidatorAddress = strings.TrimSpace(status.ValidatorAddress)
	status.Signature = strings.TrimSpace(status.Signature)
	if status.NodeID == "" {
		return
	}
	listenAddr, err := normalizePeerBaseURL(context.Background(), status.ListenAddr, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return
	}
	status.ListenAddr = listenAddr

	m.mu.Lock()
	defer m.mu.Unlock()

	current, exists := m.peers[status.NodeID]
	if exists && !status.ObservedAt.IsZero() && !current.ObservedAt.IsZero() && status.ObservedAt.Before(current.ObservedAt) {
		return
	}
	m.peers[status.NodeID] = status
	runtime := m.ensureRuntimeLocked(status.NodeID)
	baseKey := strings.TrimRight(status.ListenAddr, "/")
	if baseKey != "" && baseKey != status.NodeID {
		if pending := m.runtime[baseKey]; pending != nil && runtime != pending {
			m.runtime[status.NodeID] = pending
			delete(m.runtime, baseKey)
		}
	}
}

func (m *Manager) Peers() []protocol.PeerStatus {
	return m.listPeers(false)
}

func (m *Manager) AdmittedPeers() []protocol.PeerStatus {
	return m.listPeers(true)
}

func (m *Manager) listPeers(admittedOnly bool) []protocol.PeerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peers := make([]protocol.PeerStatus, 0, len(m.peers))
	for _, peer := range m.peers {
		if admittedOnly && strings.TrimSpace(peer.Signature) == "" {
			continue
		}
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool {
		left := m.runtime[peers[i].NodeID]
		right := m.runtime[peers[j].NodeID]
		if left != nil && right != nil && left.score != right.score {
			return left.score > right.score
		}
		return peers[i].NodeID < peers[j].NodeID
	})
	return peers
}

func (m *Manager) PeerTelemetry() []protocol.PeerTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]protocol.PeerTelemetry, 0, len(m.peers))
	for nodeID, peer := range m.peers {
		runtime := m.runtime[nodeID]
		if runtime == nil {
			runtime = &peerRuntime{}
		}
		item := protocol.PeerTelemetry{
			Peer:                peer,
			Score:               runtime.score,
			ConsecutiveFailures: runtime.consecutiveFailures,
			LastError:           runtime.lastError,
		}
		item.LastAttemptAt = cloneTimePtr(runtime.lastAttemptAt)
		item.LastSuccessAt = cloneTimePtr(runtime.lastSuccessAt)
		item.LastFailureAt = cloneTimePtr(runtime.lastFailureAt)
		item.BackoffUntil = cloneTimePtr(runtime.backoffUntil)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Peer.NodeID < items[j].Peer.NodeID
	})
	return items
}

func (m *Manager) AllowHello(nodeID string, seenAt time.Time) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := m.ensureRuntimeLocked(nodeID)
	if runtime.lastInboundHelloAt != nil && seenAt.Sub(*runtime.lastInboundHelloAt) < m.opts.HelloMinInterval {
		return false
	}
	value := seenAt.UTC()
	runtime.lastInboundHelloAt = &value
	return true
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
		_ = m.postJSONToPeer(ctx, peer, "/v1/p2p/hello", hello)
	}
}

func (m *Manager) BroadcastProposal(ctx context.Context, proposal protocol.ConsensusProposal) {
	key := fmt.Sprintf("proposal/%d/%d/%s", proposal.Height, proposal.Round, proposal.BlockHash)
	if !m.shouldBroadcast(key) {
		return
	}
	for _, peer := range m.Peers() {
		_ = m.postJSONToPeer(ctx, peer, "/v1/p2p/consensus/proposals", proposal)
	}
}

func (m *Manager) BroadcastVote(ctx context.Context, vote protocol.ConsensusVote) {
	key := fmt.Sprintf("vote/%d/%d/%s/%s/%s", vote.Height, vote.Round, vote.Type, vote.Voter, vote.BlockHash)
	if !m.shouldBroadcast(key) {
		return
	}
	for _, peer := range m.Peers() {
		_ = m.postJSONToPeer(ctx, peer, "/v1/p2p/consensus/votes", vote)
	}
}

func (m *Manager) BroadcastRoundChange(ctx context.Context, roundChange protocol.ConsensusRoundChange) {
	key := fmt.Sprintf("round-change/%d/%d/%s", roundChange.Height, roundChange.Round, roundChange.Validator)
	if !m.shouldBroadcast(key) {
		return
	}
	for _, peer := range m.Peers() {
		_ = m.postJSONToPeer(ctx, peer, "/v1/p2p/consensus/round-changes", roundChange)
	}
}

func (m *Manager) BroadcastCertifiedBlock(ctx context.Context, bundle protocol.CertifiedBlock) {
	key := fmt.Sprintf("certified/%d/%s", bundle.Block.Header.Height, bundle.Block.Hash)
	if !m.shouldBroadcast(key) {
		return
	}
	for _, peer := range m.Peers() {
		_ = m.postJSONToPeer(ctx, peer, "/v1/p2p/blocks/import", bundle)
	}
}

func (m *Manager) ListenAddr() string {
	return m.listenAddr
}

func (m *Manager) FetchPeerStatus(ctx context.Context, baseURL string) (protocol.PeerStatus, error) {
	var status protocol.PeerStatus
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return protocol.PeerStatus{}, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, "/v1/p2p/status", &status)
	}); err != nil {
		return protocol.PeerStatus{}, err
	}
	return status, nil
}

func (m *Manager) FetchPeers(ctx context.Context, baseURL string) ([]protocol.PeerStatus, error) {
	peers := make([]protocol.PeerStatus, 0)
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return nil, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, "/v1/p2p/peers", &peers)
	}); err != nil {
		return nil, err
	}
	return peers, nil
}

func (m *Manager) FetchCertifiedBlock(ctx context.Context, baseURL string, height int64) (protocol.CertifiedBlock, error) {
	var bundle protocol.CertifiedBlock
	path := "/v1/p2p/blocks/" + strconv.FormatInt(height, 10) + "/certified"
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return protocol.CertifiedBlock{}, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, path, &bundle)
	}); err != nil {
		return protocol.CertifiedBlock{}, err
	}
	return bundle, nil
}

func (m *Manager) FetchCertifiedBlocksRange(ctx context.Context, baseURL string, from int64, limit int) ([]protocol.CertifiedBlock, error) {
	bundles := make([]protocol.CertifiedBlock, 0)
	path := "/v1/p2p/blocks/certified?from=" + strconv.FormatInt(from, 10) + "&limit=" + strconv.Itoa(limit)
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return nil, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, path, &bundles)
	}); err != nil {
		return nil, err
	}
	return bundles, nil
}

func (m *Manager) FetchCandidateBlock(ctx context.Context, baseURL string, hash string) (protocol.ConsensusCandidateBlock, error) {
	var bundle protocol.ConsensusCandidateBlock
	path := "/v1/p2p/candidates/" + strings.TrimSpace(hash)
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return protocol.ConsensusCandidateBlock{}, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, path, &bundle)
	}); err != nil {
		return protocol.ConsensusCandidateBlock{}, err
	}
	return bundle, nil
}

func (m *Manager) FetchStateSnapshot(ctx context.Context, baseURL string, window int) (protocol.StateSnapshot, error) {
	var snapshot protocol.StateSnapshot
	path := "/v1/p2p/state/snapshot?window=" + strconv.Itoa(window)
	baseURL, err := normalizePeerBaseURL(ctx, baseURL, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return protocol.StateSnapshot{}, err
	}
	if err := m.getJSON(baseURL, func() error {
		return m.getJSONInto(ctx, baseURL, path, &snapshot)
	}); err != nil {
		return protocol.StateSnapshot{}, err
	}
	return snapshot, nil
}

func (m *Manager) postJSONToPeer(ctx context.Context, peer protocol.PeerStatus, path string, body any) error {
	baseURL, err := normalizePeerBaseURL(ctx, peer.ListenAddr, m.opts.AllowPrivateEndpoints)
	if err != nil {
		return err
	}
	if !m.shouldAttempt(baseURL) {
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
		m.recordFailure(baseURL, err)
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		err := fmt.Errorf("p2p post %s returned status %d", path, response.StatusCode)
		m.recordFailure(baseURL, err)
		return err
	}

	m.recordSuccess(baseURL)
	return nil
}

func (m *Manager) getJSON(baseURL string, fetch func() error) error {
	if baseURL == "" {
		return fmt.Errorf("empty peer base URL")
	}
	if !m.shouldAttempt(baseURL) {
		return fmt.Errorf("peer %s is in backoff", baseURL)
	}
	if err := fetch(); err != nil {
		m.recordFailure(baseURL, err)
		return err
	}
	m.recordSuccess(baseURL)
	return nil
}

func (m *Manager) getJSONInto(ctx context.Context, baseURL string, path string, target any) error {
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
	body, err := io.ReadAll(io.LimitReader(response.Body, m.opts.MaxResponseBytes+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > m.opts.MaxResponseBytes {
		return fmt.Errorf("p2p response exceeded %d bytes", m.opts.MaxResponseBytes)
	}
	return json.Unmarshal(body, target)
}

func (m *Manager) shouldAttempt(baseURL string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runtime := m.runtime[m.resolvePeerKeyLocked(baseURL)]
	if runtime == nil || runtime.backoffUntil == nil {
		return true
	}
	return time.Now().UTC().After(*runtime.backoffUntil)
}

func (m *Manager) recordSuccess(baseURL string) {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := m.ensureRuntimeLocked(m.resolvePeerKeyLocked(baseURL))
	runtime.lastError = ""
	runtime.consecutiveFailures = 0
	runtime.backoffUntil = nil
	runtime.lastAttemptAt = &now
	runtime.lastSuccessAt = &now
	if runtime.score < 100 {
		runtime.score++
	}
}

func (m *Manager) recordFailure(baseURL string, err error) {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := m.ensureRuntimeLocked(m.resolvePeerKeyLocked(baseURL))
	runtime.consecutiveFailures++
	runtime.lastAttemptAt = &now
	runtime.lastFailureAt = &now
	if err != nil {
		runtime.lastError = err.Error()
	}
	if runtime.score > -100 {
		runtime.score--
	}

	backoff := m.opts.BaseBackoff
	for step := 1; step < runtime.consecutiveFailures; step++ {
		backoff *= 2
		if backoff >= m.opts.MaxBackoff {
			backoff = m.opts.MaxBackoff
			break
		}
	}
	until := now.Add(backoff)
	runtime.backoffUntil = &until
}

func (m *Manager) shouldBroadcast(key string) bool {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	for existing, expiry := range m.broadcastDedups {
		if now.After(expiry) {
			delete(m.broadcastDedups, existing)
		}
	}
	if expiry, exists := m.broadcastDedups[key]; exists && now.Before(expiry) {
		return false
	}
	m.broadcastDedups[key] = now.Add(m.opts.BroadcastDedupTTL)
	return true
}

func (m *Manager) resolvePeerKeyLocked(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for nodeID, peer := range m.peers {
		if strings.TrimRight(strings.TrimSpace(peer.ListenAddr), "/") == baseURL {
			return nodeID
		}
	}
	return baseURL
}

func (m *Manager) ensureRuntimeLocked(key string) *peerRuntime {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "_unknown"
	}
	runtime := m.runtime[key]
	if runtime == nil {
		runtime = &peerRuntime{}
		m.runtime[key] = runtime
	}
	return runtime
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
