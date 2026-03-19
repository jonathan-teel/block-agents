package consensus

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"aichain/internal/config"
	"aichain/internal/network/p2p"
	"aichain/internal/protocol"
)

type mockBackend struct {
	proposals    []protocol.ConsensusProposal
	votes        []protocol.ConsensusVote
	roundChanges []protocol.ConsensusRoundChange
	certificates []protocol.QuorumCertificate
	evidence     []protocol.ConsensusEvidence
	imported     []protocol.CertifiedBlock
}

func (m *mockBackend) RecordConsensusProposal(_ context.Context, proposal protocol.ConsensusProposal) error {
	m.proposals = append(m.proposals, proposal)
	return nil
}

func (m *mockBackend) RecordConsensusVote(_ context.Context, vote protocol.ConsensusVote) error {
	m.votes = append(m.votes, vote)
	return nil
}

func (m *mockBackend) RecordConsensusRoundChange(_ context.Context, message protocol.ConsensusRoundChange) error {
	m.roundChanges = append(m.roundChanges, message)
	return nil
}

func (m *mockBackend) RecordQuorumCertificate(_ context.Context, certificate protocol.QuorumCertificate) error {
	m.certificates = append(m.certificates, certificate)
	return nil
}

func (m *mockBackend) RecordConsensusEvidence(_ context.Context, evidence protocol.ConsensusEvidence) error {
	m.evidence = append(m.evidence, evidence)
	return nil
}

func (m *mockBackend) ImportCertifiedBlock(_ context.Context, bundle protocol.CertifiedBlock) error {
	m.imported = append(m.imported, bundle)
	return nil
}

func (m *mockBackend) ValidateCandidateBlock(_ context.Context, _ protocol.Block) error {
	return nil
}

func (m *mockBackend) GetChainInfo(_ context.Context) (protocol.ChainInfo, error) {
	return protocol.ChainInfo{
		ChainID:    "blockagents-devnet-1",
		HeadHeight: 0,
		HeadHash:   "parent",
	}, nil
}

func (m *mockBackend) ListQuorumCertificates(_ context.Context, _ int) ([]protocol.QuorumCertificate, error) {
	return append([]protocol.QuorumCertificate(nil), m.certificates...), nil
}

func (m *mockBackend) ListConsensusRoundChanges(_ context.Context, _ int) ([]protocol.ConsensusRoundChange, error) {
	return append([]protocol.ConsensusRoundChange(nil), m.roundChanges...), nil
}

func (m *mockBackend) ListForkChoicePreferences(_ context.Context, _ int) ([]protocol.ForkChoicePreference, error) {
	return nil, nil
}

func TestVoteTrackerFormsQuorumCertificate(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	set := NewValidatorSet([]protocol.Validator{
		{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
		{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
		{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
	})
	tracker := NewVoteTracker(set)

	vote1 := protocol.ConsensusVote{
		ChainID:   "blockagents-devnet-1",
		Height:    1,
		Round:     0,
		Type:      VoteTypePrecommit,
		Voter:     "alice",
		BlockHash: "abc",
		VotedAt:   time.Now().UTC(),
	}
	signature, _ := SignVote(vote1, priv1)
	vote1.Signature = signature

	qc, err := tracker.AddVote(vote1)
	if err != nil {
		t.Fatalf("add first vote: %v", err)
	}
	if qc != nil {
		t.Fatal("expected no quorum after first vote")
	}

	vote2 := protocol.ConsensusVote{
		ChainID:   "blockagents-devnet-1",
		Height:    1,
		Round:     0,
		Type:      VoteTypePrecommit,
		Voter:     "bob",
		BlockHash: "abc",
		VotedAt:   time.Now().UTC(),
	}
	signature, _ = SignVote(vote2, priv2)
	vote2.Signature = signature

	qc, err = tracker.AddVote(vote2)
	if err != nil {
		t.Fatalf("add second vote: %v", err)
	}
	if qc == nil {
		t.Fatal("expected quorum certificate")
	}
	if qc.Power != 2 {
		t.Fatalf("expected certified power 2, got %d", qc.Power)
	}
}

func TestVerifyCertifiedBlock(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	set := NewValidatorSet([]protocol.Validator{
		{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
		{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
		{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
	})

	block := protocol.Block{
		Hash: "abc",
		Header: protocol.BlockHeader{
			ChainID: "blockagents-devnet-1",
			Height:  3,
		},
	}
	proposal := protocol.ConsensusProposal{
		ChainID:     "blockagents-devnet-1",
		Height:      3,
		Round:       0,
		Proposer:    "alice",
		BlockHash:   block.Hash,
		BlockHeight: 3,
		ParentHash:  "parent",
		ProposedAt:  time.Now().UTC(),
	}
	signature, _ := SignProposal(proposal, priv1)
	proposal.Signature = signature

	vote1 := protocol.ConsensusVote{
		ChainID:   "blockagents-devnet-1",
		Height:    3,
		Round:     0,
		Type:      VoteTypePrecommit,
		Voter:     "alice",
		BlockHash: block.Hash,
		VotedAt:   time.Now().UTC(),
	}
	signature, _ = SignVote(vote1, priv1)
	vote1.Signature = signature

	vote2 := protocol.ConsensusVote{
		ChainID:   "blockagents-devnet-1",
		Height:    3,
		Round:     0,
		Type:      VoteTypePrecommit,
		Voter:     "bob",
		BlockHash: block.Hash,
		VotedAt:   time.Now().UTC(),
	}
	signature, _ = SignVote(vote2, priv2)
	vote2.Signature = signature

	bundle := protocol.CertifiedBlock{
		Block:    block,
		Proposal: proposal,
		Votes:    []protocol.ConsensusVote{vote1, vote2},
		Certificate: protocol.QuorumCertificate{
			ChainID:     "blockagents-devnet-1",
			Height:      3,
			Round:       0,
			BlockHash:   block.Hash,
			VoteType:    VoteTypePrecommit,
			Signers:     []string{"alice", "bob"},
			Power:       2,
			Threshold:   2,
			CertifiedAt: time.Now().UTC(),
		},
	}

	if err := VerifyCertifiedBlock(set, bundle); err != nil {
		t.Fatalf("verify certified block: %v", err)
	}
}

func TestEngineCommitsOnPrecommitQuorum(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	cfg := config.Config{
		ChainID:          "blockagents-devnet-1",
		ValidatorAddress: "alice",
		ValidatorPrivateKey: hex.EncodeToString(priv1),
		Genesis: protocol.Genesis{
			ChainID: "blockagents-devnet-1",
			Validators: []protocol.GenesisValidator{
				{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
				{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
				{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
			},
		},
	}

	backend := &mockBackend{}
	engine, err := NewEngine(cfg, p2p.New("http://127.0.0.1:8080", p2p.Options{}), backend)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	block := protocol.Block{
		Hash: "abc",
		Header: protocol.BlockHeader{
			ChainID:    cfg.ChainID,
			Height:     3,
			ParentHash: "parent",
			Proposer:   "alice",
		},
	}

	engine.ObserveCandidate(context.Background(), block)

	if len(backend.proposals) != 1 {
		t.Fatalf("expected one recorded proposal, got %d", len(backend.proposals))
	}

	prevote := protocol.ConsensusVote{
		ChainID:   cfg.ChainID,
		Height:    3,
		Round:     0,
		Type:      VoteTypePrevote,
		Voter:     "bob",
		BlockHash: block.Hash,
		VotedAt:   time.Now().UTC(),
	}
	signature, _ := SignVote(prevote, priv2)
	prevote.Signature = signature
	if _, err := engine.HandleVote(context.Background(), prevote); err != nil {
		t.Fatalf("handle prevote: %v", err)
	}

	precommit := protocol.ConsensusVote{
		ChainID:   cfg.ChainID,
		Height:    3,
		Round:     0,
		Type:      VoteTypePrecommit,
		Voter:     "bob",
		BlockHash: block.Hash,
		VotedAt:   time.Now().UTC(),
	}
	signature, _ = SignVote(precommit, priv2)
	precommit.Signature = signature
	if _, err := engine.HandleVote(context.Background(), precommit); err != nil {
		t.Fatalf("handle precommit: %v", err)
	}

	if len(backend.imported) != 1 {
		t.Fatalf("expected one imported certified block, got %d", len(backend.imported))
	}
	if backend.imported[0].Block.Hash != block.Hash {
		t.Fatalf("expected imported block hash %s, got %s", block.Hash, backend.imported[0].Block.Hash)
	}
}

func TestEngineRecordsDoubleVoteEvidence(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	cfg := config.Config{
		ChainID: "blockagents-devnet-1",
		Genesis: protocol.Genesis{
			ChainID: "blockagents-devnet-1",
			Validators: []protocol.GenesisValidator{
				{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
				{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
				{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
			},
		},
	}

	backend := &mockBackend{}
	engine, err := NewEngine(cfg, p2p.New("http://127.0.0.1:8080", p2p.Options{}), backend)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	voteA := protocol.ConsensusVote{
		ChainID:   cfg.ChainID,
		Height:    3,
		Round:     0,
		Type:      VoteTypePrevote,
		Voter:     "bob",
		BlockHash: "block-a",
		VotedAt:   time.Now().UTC(),
	}
	signature, _ := SignVote(voteA, priv2)
	voteA.Signature = signature
	if _, err := engine.HandleVote(context.Background(), voteA); err != nil {
		t.Fatalf("handle first vote: %v", err)
	}

	voteB := protocol.ConsensusVote{
		ChainID:   cfg.ChainID,
		Height:    3,
		Round:     0,
		Type:      VoteTypePrevote,
		Voter:     "bob",
		BlockHash: "block-b",
		VotedAt:   time.Now().UTC(),
	}
	signature, _ = SignVote(voteB, priv2)
	voteB.Signature = signature
	if _, err := engine.HandleVote(context.Background(), voteB); err == nil {
		t.Fatal("expected double-vote error")
	}

	if len(backend.evidence) != 1 {
		t.Fatalf("expected one evidence record, got %d", len(backend.evidence))
	}
	if backend.evidence[0].EvidenceType != protocol.ConsensusEvidenceDoubleVote {
		t.Fatalf("expected double-vote evidence, got %s", backend.evidence[0].EvidenceType)
	}
}

func TestHandleRoundChangeAdvancesRoundAfterQuorum(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	cfg := config.Config{
		ChainID:              "blockagents-devnet-1",
		ValidatorAddress:     "alice",
		ValidatorPrivateKey:  hex.EncodeToString(priv1),
		ConsensusRoundTimeout: 10 * time.Second,
		Genesis: protocol.Genesis{
			ChainID: "blockagents-devnet-1",
			Validators: []protocol.GenesisValidator{
				{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
				{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
				{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
			},
		},
	}

	backend := &mockBackend{}
	engine, err := NewEngine(cfg, p2p.New("http://127.0.0.1:8080", p2p.Options{}), backend)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	rc1 := protocol.ConsensusRoundChange{
		ChainID:     cfg.ChainID,
		Height:      1,
		Round:       1,
		Validator:   "alice",
		Reason:      "timeout",
		RequestedAt: time.Now().UTC(),
	}
	signature, _ := SignRoundChange(rc1, priv1)
	rc1.Signature = signature
	if err := engine.HandleRoundChange(context.Background(), rc1); err != nil {
		t.Fatalf("handle first round change: %v", err)
	}
	if engine.CurrentRound(1) != 0 {
		t.Fatalf("expected round to remain 0 until quorum, got %d", engine.CurrentRound(1))
	}

	rc2 := protocol.ConsensusRoundChange{
		ChainID:     cfg.ChainID,
		Height:      1,
		Round:       1,
		Validator:   "bob",
		Reason:      "timeout",
		RequestedAt: time.Now().UTC(),
	}
	signature, _ = SignRoundChange(rc2, priv2)
	rc2.Signature = signature
	if err := engine.HandleRoundChange(context.Background(), rc2); err != nil {
		t.Fatalf("handle second round change: %v", err)
	}
	if engine.CurrentRound(1) != 1 {
		t.Fatalf("expected round to advance to 1 after quorum, got %d", engine.CurrentRound(1))
	}
}

func TestEngineRecoversPersistedRoundChanges(t *testing.T) {
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, _, _ := ed25519.GenerateKey(nil)

	cfg := config.Config{
		ChainID: "blockagents-devnet-1",
		Genesis: protocol.Genesis{
			ChainID: "blockagents-devnet-1",
			Validators: []protocol.GenesisValidator{
				{Address: "alice", PublicKey: hex.EncodeToString(pub1), Power: 1},
				{Address: "bob", PublicKey: hex.EncodeToString(pub2), Power: 1},
				{Address: "carol", PublicKey: hex.EncodeToString(pub3), Power: 1},
			},
		},
	}

	rc1 := protocol.ConsensusRoundChange{
		ChainID:     cfg.ChainID,
		Height:      2,
		Round:       2,
		Validator:   "alice",
		Reason:      "timeout",
		RequestedAt: time.Now().UTC(),
	}
	signature, _ := SignRoundChange(rc1, priv1)
	rc1.Signature = signature

	rc2 := protocol.ConsensusRoundChange{
		ChainID:     cfg.ChainID,
		Height:      2,
		Round:       2,
		Validator:   "bob",
		Reason:      "timeout",
		RequestedAt: time.Now().UTC(),
	}
	signature, _ = SignRoundChange(rc2, priv2)
	rc2.Signature = signature

	backend := &mockBackend{
		roundChanges: []protocol.ConsensusRoundChange{rc1, rc2},
	}
	engine, err := NewEngine(cfg, p2p.New("http://127.0.0.1:8080", p2p.Options{}), backend)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if engine.CurrentRound(2) != 2 {
		t.Fatalf("expected recovered round 2 for height 2, got %d", engine.CurrentRound(2))
	}
}
