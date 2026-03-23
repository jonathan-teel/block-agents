package consensus

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aichain/internal/protocol"
)

const (
	VoteTypePrevote   = "prevote"
	VoteTypePrecommit = "precommit"
)

type ValidatorSet struct {
	validators map[string]protocol.Validator
	ordered    []protocol.Validator
	totalPower int64
}

func NewValidatorSet(validators []protocol.Validator) ValidatorSet {
	copied := append([]protocol.Validator(nil), validators...)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].Address == copied[j].Address {
			return copied[i].PublicKey < copied[j].PublicKey
		}
		return copied[i].Address < copied[j].Address
	})

	index := make(map[string]protocol.Validator, len(copied))
	var total int64
	for _, validator := range copied {
		index[validator.Address] = validator
		total += validator.Power
	}

	return ValidatorSet{
		validators: index,
		ordered:    copied,
		totalPower: total,
	}
}

func (s ValidatorSet) Validators() []protocol.Validator {
	return append([]protocol.Validator(nil), s.ordered...)
}

func (s ValidatorSet) TotalPower() int64 {
	return s.totalPower
}

func (s ValidatorSet) QuorumPower() int64 {
	if s.totalPower == 0 {
		return 0
	}
	return ((2 * s.totalPower) / 3) + 1
}

func (s ValidatorSet) Get(address string) (protocol.Validator, bool) {
	validator, ok := s.validators[strings.TrimSpace(address)]
	return validator, ok
}

func (s ValidatorSet) Proposer(height int64, round int) (protocol.Validator, bool) {
	if len(s.ordered) == 0 {
		return protocol.Validator{}, false
	}

	index := int((height + int64(round)) % int64(len(s.ordered)))
	if index < 0 {
		index = 0
	}
	return s.ordered[index], true
}

func SignProposal(proposal protocol.ConsensusProposal, privateKey ed25519.PrivateKey) (string, error) {
	signBytes, err := proposalSignBytes(proposal)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, signBytes)
	return hex.EncodeToString(signature), nil
}

func VerifyProposal(set ValidatorSet, proposal protocol.ConsensusProposal) error {
	validator, ok := set.Get(proposal.Proposer)
	if !ok {
		return fmt.Errorf("unknown proposer %s", proposal.Proposer)
	}
	expected, ok := set.Proposer(proposal.Height, proposal.Round)
	if !ok {
		return fmt.Errorf("no proposer available for height=%d round=%d", proposal.Height, proposal.Round)
	}
	if expected.Address != proposal.Proposer {
		return fmt.Errorf("unexpected proposer %s for height=%d round=%d", proposal.Proposer, proposal.Height, proposal.Round)
	}
	if proposal.BlockHeight != proposal.Height {
		return fmt.Errorf("proposal block height must match consensus height")
	}
	signBytes, err := proposalSignBytes(proposal)
	if err != nil {
		return err
	}
	return verifySignature(validator.PublicKey, proposal.Signature, signBytes)
}

func SignVote(vote protocol.ConsensusVote, privateKey ed25519.PrivateKey) (string, error) {
	signBytes, err := voteSignBytes(vote)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, signBytes)
	return hex.EncodeToString(signature), nil
}

func VerifyVote(set ValidatorSet, vote protocol.ConsensusVote) error {
	validator, ok := set.Get(vote.Voter)
	if !ok {
		return fmt.Errorf("unknown voter %s", vote.Voter)
	}
	if vote.Type != VoteTypePrevote && vote.Type != VoteTypePrecommit {
		return fmt.Errorf("unsupported vote type %s", vote.Type)
	}
	signBytes, err := voteSignBytes(vote)
	if err != nil {
		return err
	}
	return verifySignature(validator.PublicKey, vote.Signature, signBytes)
}

func SignRoundChange(message protocol.ConsensusRoundChange, privateKey ed25519.PrivateKey) (string, error) {
	signBytes, err := roundChangeSignBytes(message)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, signBytes)
	return hex.EncodeToString(signature), nil
}

func VerifyRoundChange(set ValidatorSet, message protocol.ConsensusRoundChange) error {
	validator, ok := set.Get(message.Validator)
	if !ok {
		return fmt.Errorf("unknown round-change validator %s", message.Validator)
	}
	if message.Round <= 0 {
		return fmt.Errorf("round-change round must be > 0")
	}
	if strings.TrimSpace(message.Reason) == "" {
		return fmt.Errorf("round-change reason is required")
	}
	signBytes, err := roundChangeSignBytes(message)
	if err != nil {
		return err
	}
	return verifySignature(validator.PublicKey, message.Signature, signBytes)
}

func SignPeerHello(message protocol.PeerHello, privateKey ed25519.PrivateKey) (string, error) {
	signBytes, err := peerHelloSignBytes(message)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, signBytes)
	return hex.EncodeToString(signature), nil
}

func VerifyPeerHello(set ValidatorSet, message protocol.PeerHello) error {
	validator, ok := set.Get(message.ValidatorAddress)
	if !ok {
		return fmt.Errorf("unknown peer validator %s", message.ValidatorAddress)
	}
	if strings.TrimSpace(message.NodeID) == "" {
		return fmt.Errorf("peer hello node_id is required")
	}
	if strings.TrimSpace(message.ListenAddr) == "" {
		return fmt.Errorf("peer hello listen_addr is required")
	}
	signBytes, err := peerHelloSignBytes(message)
	if err != nil {
		return err
	}
	return verifySignature(validator.PublicKey, message.Signature, signBytes)
}

func SignPeerStatus(status protocol.PeerStatus, privateKey ed25519.PrivateKey) (string, error) {
	signBytes, err := peerStatusSignBytes(status)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, signBytes)
	return hex.EncodeToString(signature), nil
}

func VerifyPeerStatus(set ValidatorSet, status protocol.PeerStatus) error {
	validator, ok := set.Get(status.ValidatorAddress)
	if !ok {
		return fmt.Errorf("unknown peer validator %s", status.ValidatorAddress)
	}
	if strings.TrimSpace(status.NodeID) == "" {
		return fmt.Errorf("peer status node_id is required")
	}
	if strings.TrimSpace(status.ListenAddr) == "" {
		return fmt.Errorf("peer status listen_addr is required")
	}
	signBytes, err := peerStatusSignBytes(status)
	if err != nil {
		return err
	}
	return verifySignature(validator.PublicKey, status.Signature, signBytes)
}

type VoteTracker struct {
	mu    sync.Mutex
	set   ValidatorSet
	votes map[string]map[string]protocol.ConsensusVote
}

func NewVoteTracker(set ValidatorSet) *VoteTracker {
	return &VoteTracker{
		set:   set,
		votes: make(map[string]map[string]protocol.ConsensusVote),
	}
}

func (t *VoteTracker) AddVote(vote protocol.ConsensusVote) (*protocol.QuorumCertificate, error) {
	if err := VerifyVote(t.set, vote); err != nil {
		return nil, err
	}

	key := voteKey(vote.Height, vote.Round, vote.Type, vote.BlockHash)

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.votes[key]; !ok {
		t.votes[key] = make(map[string]protocol.ConsensusVote)
	}
	t.votes[key][vote.Voter] = vote

	var (
		power   int64
		signers []string
	)
	for voter := range t.votes[key] {
		validator, ok := t.set.Get(voter)
		if !ok {
			continue
		}
		signers = append(signers, voter)
		power += validator.Power
	}
	sort.Strings(signers)

	if power < t.set.QuorumPower() {
		return nil, nil
	}

	return &protocol.QuorumCertificate{
		ChainID:     vote.ChainID,
		Height:      vote.Height,
		Round:       vote.Round,
		BlockHash:   vote.BlockHash,
		VoteType:    vote.Type,
		Signers:     signers,
		Power:       power,
		Threshold:   t.set.QuorumPower(),
		CertifiedAt: time.Now().UTC(),
	}, nil
}

func (t *VoteTracker) VotesFor(height int64, round int, voteType string, blockHash string) []protocol.ConsensusVote {
	key := voteKey(height, round, voteType, blockHash)

	t.mu.Lock()
	defer t.mu.Unlock()

	votes := make([]protocol.ConsensusVote, 0, len(t.votes[key]))
	for _, vote := range t.votes[key] {
		votes = append(votes, vote)
	}
	sort.Slice(votes, func(i, j int) bool {
		return votes[i].Voter < votes[j].Voter
	})
	return votes
}

func VerifyCertifiedBlock(set ValidatorSet, bundle protocol.CertifiedBlock) error {
	if err := VerifyProposal(set, bundle.Proposal); err != nil {
		return fmt.Errorf("verify proposal: %w", err)
	}
	if bundle.Proposal.ChainID != bundle.Block.Header.ChainID {
		return fmt.Errorf("proposal chain id does not match block")
	}
	if bundle.Proposal.BlockHash != bundle.Block.Hash {
		return fmt.Errorf("proposal block hash does not match block")
	}
	if bundle.Proposal.Height != bundle.Block.Header.Height {
		return fmt.Errorf("proposal height does not match block")
	}
	if bundle.Certificate.BlockHash != bundle.Block.Hash {
		return fmt.Errorf("certificate block hash does not match block")
	}
	if bundle.Certificate.Height != bundle.Block.Header.Height {
		return fmt.Errorf("certificate height does not match block")
	}
	if bundle.Certificate.ChainID != bundle.Block.Header.ChainID {
		return fmt.Errorf("certificate chain id does not match block")
	}
	if bundle.Certificate.VoteType != VoteTypePrecommit {
		return fmt.Errorf("certificate vote type must be precommit")
	}

	seen := make(map[string]struct{}, len(bundle.Votes))
	var power int64
	signers := make([]string, 0, len(bundle.Votes))
	for _, vote := range bundle.Votes {
		if vote.Type != VoteTypePrecommit {
			return fmt.Errorf("bundle vote type must be precommit")
		}
		if vote.BlockHash != bundle.Block.Hash {
			return fmt.Errorf("bundle vote block hash mismatch")
		}
		if vote.Height != bundle.Block.Header.Height {
			return fmt.Errorf("bundle vote height mismatch")
		}
		if vote.ChainID != bundle.Block.Header.ChainID {
			return fmt.Errorf("bundle vote chain id mismatch")
		}
		if err := VerifyVote(set, vote); err != nil {
			return fmt.Errorf("verify vote: %w", err)
		}
		if _, ok := seen[vote.Voter]; ok {
			continue
		}
		seen[vote.Voter] = struct{}{}
		validator, ok := set.Get(vote.Voter)
		if !ok {
			return fmt.Errorf("unknown vote signer %s", vote.Voter)
		}
		power += validator.Power
		signers = append(signers, vote.Voter)
	}
	sort.Strings(signers)

	if power < set.QuorumPower() {
		return fmt.Errorf("bundle precommit power %d below quorum %d", power, set.QuorumPower())
	}
	if bundle.Certificate.Power != power {
		return fmt.Errorf("certificate power mismatch")
	}
	if bundle.Certificate.Threshold != set.QuorumPower() {
		return fmt.Errorf("certificate threshold mismatch")
	}
	if len(bundle.Certificate.Signers) != len(signers) {
		return fmt.Errorf("certificate signer count mismatch")
	}
	for index := range signers {
		if bundle.Certificate.Signers[index] != signers[index] {
			return fmt.Errorf("certificate signer list mismatch")
		}
	}

	return nil
}

func voteKey(height int64, round int, voteType string, blockHash string) string {
	return fmt.Sprintf("%d/%d/%s/%s", height, round, voteType, blockHash)
}

func proposalSignBytes(proposal protocol.ConsensusProposal) ([]byte, error) {
	type signableProposal struct {
		ChainID     string `json:"chain_id"`
		Height      int64  `json:"height"`
		Round       int    `json:"round"`
		Proposer    string `json:"proposer"`
		BlockHash   string `json:"block_hash"`
		BlockHeight int64  `json:"block_height"`
		ParentHash  string `json:"parent_hash"`
	}
	return json.Marshal(signableProposal{
		ChainID:     proposal.ChainID,
		Height:      proposal.Height,
		Round:       proposal.Round,
		Proposer:    proposal.Proposer,
		BlockHash:   proposal.BlockHash,
		BlockHeight: proposal.BlockHeight,
		ParentHash:  proposal.ParentHash,
	})
}

func voteSignBytes(vote protocol.ConsensusVote) ([]byte, error) {
	type signableVote struct {
		ChainID   string `json:"chain_id"`
		Height    int64  `json:"height"`
		Round     int    `json:"round"`
		Type      string `json:"type"`
		Voter     string `json:"voter"`
		BlockHash string `json:"block_hash"`
	}
	return json.Marshal(signableVote{
		ChainID:   vote.ChainID,
		Height:    vote.Height,
		Round:     vote.Round,
		Type:      vote.Type,
		Voter:     vote.Voter,
		BlockHash: vote.BlockHash,
	})
}

func roundChangeSignBytes(message protocol.ConsensusRoundChange) ([]byte, error) {
	type signableRoundChange struct {
		ChainID   string `json:"chain_id"`
		Height    int64  `json:"height"`
		Round     int    `json:"round"`
		Validator string `json:"validator"`
		Reason    string `json:"reason"`
	}
	return json.Marshal(signableRoundChange{
		ChainID:   message.ChainID,
		Height:    message.Height,
		Round:     message.Round,
		Validator: message.Validator,
		Reason:    strings.TrimSpace(message.Reason),
	})
}

func peerHelloSignBytes(message protocol.PeerHello) ([]byte, error) {
	type signablePeerHello struct {
		NodeID           string    `json:"node_id"`
		ChainID          string    `json:"chain_id"`
		GenesisHash      string    `json:"genesis_hash"`
		ListenAddr       string    `json:"listen_addr"`
		ValidatorAddress string    `json:"validator_address"`
		SeenAt           time.Time `json:"seen_at"`
	}
	return json.Marshal(signablePeerHello{
		NodeID:           strings.TrimSpace(message.NodeID),
		ChainID:          strings.TrimSpace(message.ChainID),
		GenesisHash:      strings.TrimSpace(message.GenesisHash),
		ListenAddr:       strings.TrimSpace(message.ListenAddr),
		ValidatorAddress: strings.TrimSpace(message.ValidatorAddress),
		SeenAt:           message.SeenAt.UTC(),
	})
}

func peerStatusSignBytes(status protocol.PeerStatus) ([]byte, error) {
	type signablePeerStatus struct {
		NodeID           string    `json:"node_id"`
		ChainID          string    `json:"chain_id"`
		GenesisHash      string    `json:"genesis_hash"`
		ListenAddr       string    `json:"listen_addr"`
		ValidatorAddress string    `json:"validator_address"`
		HeadHeight       int64     `json:"head_height"`
		HeadHash         string    `json:"head_hash"`
		ObservedAt       time.Time `json:"observed_at"`
	}
	return json.Marshal(signablePeerStatus{
		NodeID:           strings.TrimSpace(status.NodeID),
		ChainID:          strings.TrimSpace(status.ChainID),
		GenesisHash:      strings.TrimSpace(status.GenesisHash),
		ListenAddr:       strings.TrimSpace(status.ListenAddr),
		ValidatorAddress: strings.TrimSpace(status.ValidatorAddress),
		HeadHeight:       status.HeadHeight,
		HeadHash:         strings.TrimSpace(status.HeadHash),
		ObservedAt:       status.ObservedAt.UTC(),
	})
}

func verifySignature(publicKeyHex string, signatureHex string, payload []byte) error {
	publicKey, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key")
	}
	signature, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature")
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}
