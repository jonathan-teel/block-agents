package consensus

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"aichain/internal/config"
	"aichain/internal/network/p2p"
	"aichain/internal/protocol"
)

type Engine struct {
	cfg        config.Config
	peers      *p2p.Manager
	recorder   Recorder
	committer  Committer
	validator  CandidateValidator
	chainState ChainStateReader
	set        ValidatorSet
	tracker    *VoteTracker
	privateKey ed25519.PrivateKey

	mu               sync.RWMutex
	proposals        map[string]protocol.ConsensusProposal
	proposalSlots    map[string]string
	voteSlots        map[string]protocol.ConsensusVote
	candidateBlocks  map[string]protocol.Block
	openHeights      map[int64]string
	certificates     map[string]protocol.QuorumCertificate
	preferredByHeight map[int64]protocol.QuorumCertificate
	committed        map[string]struct{}
	currentRounds    map[int64]int
	observedAt       map[int64]time.Time
	roundChanges     map[string]map[string]protocol.ConsensusRoundChange
}

type Recorder interface {
	RecordConsensusProposal(context.Context, protocol.ConsensusProposal) error
	RecordConsensusVote(context.Context, protocol.ConsensusVote) error
	RecordConsensusRoundChange(context.Context, protocol.ConsensusRoundChange) error
	RecordQuorumCertificate(context.Context, protocol.QuorumCertificate) error
	RecordConsensusEvidence(context.Context, protocol.ConsensusEvidence) error
}

type Committer interface {
	ImportCertifiedBlock(context.Context, protocol.CertifiedBlock) error
}

type CandidateValidator interface {
	ValidateCandidateBlock(context.Context, protocol.Block) error
}

type ChainStateReader interface {
	GetChainInfo(context.Context) (protocol.ChainInfo, error)
}

type Backend interface {
	Recorder
	Committer
	CandidateValidator
	ChainStateReader
}

type ValidatorSetReader interface {
	ListValidators(context.Context) ([]protocol.Validator, error)
}

type RecoveryReader interface {
	ListQuorumCertificates(context.Context, int) ([]protocol.QuorumCertificate, error)
	ListConsensusRoundChanges(context.Context, int) ([]protocol.ConsensusRoundChange, error)
	ListForkChoicePreferences(context.Context, int) ([]protocol.ForkChoicePreference, error)
}

func NewEngine(cfg config.Config, peers *p2p.Manager, backend Backend) (*Engine, error) {
	validators := make([]protocol.Validator, 0)
	if reader, ok := backend.(ValidatorSetReader); ok {
		loaded, err := reader.ListValidators(context.Background())
		if err != nil {
			return nil, fmt.Errorf("load validator registry: %w", err)
		}
		validators = append(validators, loaded...)
	}
	if len(validators) == 0 {
		validators = make([]protocol.Validator, 0, len(cfg.Genesis.Validators))
		for _, validator := range cfg.Genesis.Validators {
			validators = append(validators, protocol.Validator{
				Address:   validator.Address,
				PublicKey: validator.PublicKey,
				Power:     validator.Power,
			})
		}
	}
	set := NewValidatorSet(validators)

	engine := &Engine{
		cfg:              cfg,
		peers:            peers,
		recorder:         backend,
		committer:        backend,
		validator:        backend,
		chainState:       backend,
		set:              set,
		tracker:          NewVoteTracker(set),
		proposals:        make(map[string]protocol.ConsensusProposal),
		proposalSlots:    make(map[string]string),
		voteSlots:        make(map[string]protocol.ConsensusVote),
		candidateBlocks:  make(map[string]protocol.Block),
		openHeights:      make(map[int64]string),
		certificates:     make(map[string]protocol.QuorumCertificate),
		preferredByHeight: make(map[int64]protocol.QuorumCertificate),
		committed:        make(map[string]struct{}),
		currentRounds:    make(map[int64]int),
		observedAt:       make(map[int64]time.Time),
		roundChanges:     make(map[string]map[string]protocol.ConsensusRoundChange),
	}

	if strings.TrimSpace(cfg.ValidatorPrivateKey) != "" {
		privateKey, err := decodePrivateKey(cfg.ValidatorPrivateKey)
		if err != nil {
			return nil, err
		}
		engine.privateKey = privateKey
	}

	if recovery, ok := backend.(RecoveryReader); ok {
		if err := engine.recoverPersistentState(context.Background(), recovery); err != nil {
			return nil, err
		}
	}

	return engine, nil
}

func (e *Engine) Validators() []protocol.Validator {
	return e.set.Validators()
}

func (e *Engine) CurrentRound(height int64) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentRoundLocked(height)
}

func (e *Engine) ShouldSealNext(height int64) bool {
	round := e.CurrentRound(height)
	if strings.TrimSpace(e.cfg.ValidatorAddress) == "" {
		return !e.HasPendingHeight(height)
	}

	proposer, ok := e.set.Proposer(height, round)
	if !ok {
		return !e.HasPendingHeight(height)
	}
	return proposer.Address == e.cfg.ValidatorAddress && !e.HasPendingHeight(height)
}

func (e *Engine) HasPendingHeight(height int64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, ok := e.openHeights[height]
	return ok
}

func (e *Engine) ObserveCandidate(ctx context.Context, block protocol.Block) {
	round := e.CurrentRound(block.Header.Height)
	proposer, ok := e.set.Proposer(block.Header.Height, round)
	if !ok || proposer.Address != e.cfg.ValidatorAddress || len(e.privateKey) == 0 {
		return
	}

	e.mu.Lock()
	if _, exists := e.openHeights[block.Header.Height]; exists {
		e.mu.Unlock()
		return
	}
	e.candidateBlocks[block.Hash] = block
	e.openHeights[block.Header.Height] = block.Hash
	e.observedAt[block.Header.Height] = time.Now().UTC()
	e.mu.Unlock()

	proposal := protocol.ConsensusProposal{
		ChainID:     e.cfg.ChainID,
		Height:      block.Header.Height,
		Round:       round,
		Proposer:    e.cfg.ValidatorAddress,
		BlockHash:   block.Hash,
		BlockHeight: block.Header.Height,
		ParentHash:  block.Header.ParentHash,
		ProposedAt:  time.Now().UTC(),
	}
	signature, err := SignProposal(proposal, e.privateKey)
	if err != nil {
		log.Printf("consensus sign proposal error: %v", err)
		return
	}
	proposal.Signature = signature

	if err := e.HandleProposal(ctx, proposal); err != nil {
		e.mu.Lock()
		delete(e.openHeights, block.Header.Height)
		delete(e.candidateBlocks, block.Hash)
		e.mu.Unlock()
		log.Printf("consensus handle local proposal error: %v", err)
		return
	}
	if e.peers != nil {
		e.peers.BroadcastProposal(ctx, proposal)
	}
}

func (e *Engine) HandleProposal(ctx context.Context, proposal protocol.ConsensusProposal) error {
	if proposal.ChainID != e.cfg.ChainID {
		return fmt.Errorf("unexpected proposal chain_id %s", proposal.ChainID)
	}

	e.mu.Lock()
	currentRound := e.currentRoundLocked(proposal.Height)
	if proposal.Round < currentRound {
		e.mu.Unlock()
		return fmt.Errorf("stale proposal round %d for height=%d current_round=%d", proposal.Round, proposal.Height, currentRound)
	}
	if proposal.Round > currentRound {
		e.currentRounds[proposal.Height] = proposal.Round
		delete(e.openHeights, proposal.Height)
	}
	e.observedAt[proposal.Height] = time.Now().UTC()
	e.mu.Unlock()

	if err := VerifyProposal(e.set, proposal); err != nil {
		return err
	}
	if e.recorder != nil {
		if err := e.recorder.RecordConsensusProposal(ctx, proposal); err != nil {
			return err
		}
	}
	if err := e.trackProposal(ctx, proposal); err != nil {
		return err
	}
	if err := e.ensureCandidateBlock(ctx, proposal); err != nil {
		return err
	}

	if err := e.castLocalVote(ctx, proposal.Height, proposal.Round, proposal.BlockHash, VoteTypePrevote); err != nil {
		return err
	}
	return nil
}

func (e *Engine) HandleVote(ctx context.Context, vote protocol.ConsensusVote) (*protocol.QuorumCertificate, error) {
	if vote.ChainID != e.cfg.ChainID {
		return nil, fmt.Errorf("unexpected vote chain_id %s", vote.ChainID)
	}
	if err := VerifyVote(e.set, vote); err != nil {
		return nil, err
	}
	if e.recorder != nil {
		if err := e.recorder.RecordConsensusVote(ctx, vote); err != nil {
			return nil, err
		}
	}
	if err := e.trackVote(ctx, vote); err != nil {
		return nil, err
	}

	qc, err := e.tracker.AddVote(vote)
	if err != nil {
		return nil, err
	}
	if qc == nil {
		return nil, nil
	}

	if err := e.storeCertificate(ctx, *qc); err != nil {
		return nil, err
	}

	switch qc.VoteType {
	case VoteTypePrevote:
		if err := e.castLocalVote(ctx, qc.Height, qc.Round, qc.BlockHash, VoteTypePrecommit); err != nil {
			return qc, err
		}
	case VoteTypePrecommit:
		if err := e.commitCertifiedBlock(ctx, *qc); err != nil {
			return qc, err
		}
	}

	log.Printf("consensus quorum certified height=%d block=%s round=%d type=%s power=%d/%d", qc.Height, qc.BlockHash, qc.Round, qc.VoteType, qc.Power, qc.Threshold)
	return qc, nil
}

func (e *Engine) HandleRoundChange(ctx context.Context, message protocol.ConsensusRoundChange) error {
	if message.ChainID != e.cfg.ChainID {
		return fmt.Errorf("unexpected round-change chain_id %s", message.ChainID)
	}
	if err := VerifyRoundChange(e.set, message); err != nil {
		return err
	}
	if e.recorder != nil {
		if err := e.recorder.RecordConsensusRoundChange(ctx, message); err != nil {
			return err
		}
	}

	e.mu.Lock()
	e.applyRoundChangeLocked(message)
	e.mu.Unlock()

	return nil
}

func (e *Engine) RunTimeoutLoop(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.ConsensusRoundTimeout / 2)
	defer ticker.Stop()

	e.onTimeout(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.onTimeout(ctx)
		}
	}
}

func (e *Engine) Certificates() []protocol.QuorumCertificate {
	e.mu.RLock()
	defer e.mu.RUnlock()

	certificates := make([]protocol.QuorumCertificate, 0, len(e.certificates))
	for _, certificate := range e.certificates {
		certificates = append(certificates, certificate)
	}
	sort.Slice(certificates, func(i, j int) bool {
		if certificates[i].Height == certificates[j].Height {
			if certificates[i].Round == certificates[j].Round {
				return certificates[i].VoteType < certificates[j].VoteType
			}
			return certificates[i].Round < certificates[j].Round
		}
		return certificates[i].Height < certificates[j].Height
	})
	return certificates
}

func (e *Engine) CandidateBlock(hash string) (protocol.ConsensusCandidateBlock, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	block, ok := e.candidateBlocks[strings.TrimSpace(hash)]
	if !ok {
		return protocol.ConsensusCandidateBlock{}, false
	}
	proposal, ok := e.proposals[block.Hash]
	if !ok {
		return protocol.ConsensusCandidateBlock{}, false
	}
	return protocol.ConsensusCandidateBlock{
		Block:      block,
		Proposal:   proposal,
		ObservedAt: e.observedAt[block.Header.Height],
	}, true
}

func decodePrivateKey(value string) (ed25519.PrivateKey, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("decode validator private key: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("validator private key must be %d bytes", ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(decoded), nil
}

func (e *Engine) VerifyCertifiedBlock(bundle protocol.CertifiedBlock) error {
	return VerifyCertifiedBlock(e.set, bundle)
}

func (e *Engine) PreferredCertificate(height int64) (protocol.QuorumCertificate, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	certificate, ok := e.preferredByHeight[height]
	return certificate, ok
}

func (e *Engine) castLocalVote(ctx context.Context, height int64, round int, blockHash string, voteType string) error {
	if len(e.privateKey) == 0 {
		return nil
	}
	if _, ok := e.set.Get(e.cfg.ValidatorAddress); !ok {
		return nil
	}

	slot := validatorVoteSlotKey(height, round, voteType, e.cfg.ValidatorAddress)
	e.mu.RLock()
	existing, exists := e.voteSlots[slot]
	e.mu.RUnlock()
	if exists {
		if existing.BlockHash == blockHash {
			return nil
		}
		return fmt.Errorf("local validator already cast %s for height=%d round=%d", voteType, height, round)
	}

	vote := protocol.ConsensusVote{
		ChainID:   e.cfg.ChainID,
		Height:    height,
		Round:     round,
		Type:      voteType,
		Voter:     e.cfg.ValidatorAddress,
		BlockHash: blockHash,
		VotedAt:   time.Now().UTC(),
	}
	signature, err := SignVote(vote, e.privateKey)
	if err != nil {
		return err
	}
	vote.Signature = signature

	if _, err := e.HandleVote(ctx, vote); err != nil {
		return err
	}
	if e.peers != nil {
		e.peers.BroadcastVote(ctx, vote)
	}
	return nil
}

func (e *Engine) ensureCandidateBlock(ctx context.Context, proposal protocol.ConsensusProposal) error {
	e.mu.RLock()
	_, ok := e.candidateBlocks[proposal.BlockHash]
	e.mu.RUnlock()
	if ok {
		return nil
	}
	if e.peers == nil || e.validator == nil {
		return fmt.Errorf("candidate block %s unavailable for proposal", proposal.BlockHash)
	}

	peer, ok := e.peers.FindPeerByValidator(proposal.Proposer)
	if !ok {
		return fmt.Errorf("no peer found for proposer %s", proposal.Proposer)
	}
	bundle, err := e.peers.FetchCandidateBlock(ctx, peer.ListenAddr, proposal.BlockHash)
	if err != nil {
		return fmt.Errorf("fetch candidate block: %w", err)
	}
	if bundle.Proposal.BlockHash != proposal.BlockHash {
		return fmt.Errorf("candidate bundle proposal hash mismatch")
	}
	if bundle.Block.Hash != proposal.BlockHash {
		return fmt.Errorf("candidate bundle block hash mismatch")
	}
	if bundle.Proposal.Height != proposal.Height || bundle.Block.Header.Height != proposal.Height {
		return fmt.Errorf("candidate bundle height mismatch")
	}
	if err := e.validator.ValidateCandidateBlock(ctx, bundle.Block); err != nil {
		return fmt.Errorf("validate candidate block: %w", err)
	}

	e.mu.Lock()
	e.candidateBlocks[bundle.Block.Hash] = bundle.Block
	e.openHeights[bundle.Block.Header.Height] = bundle.Block.Hash
	e.observedAt[bundle.Block.Header.Height] = time.Now().UTC()
	e.mu.Unlock()
	return nil
}

func (e *Engine) trackProposal(ctx context.Context, proposal protocol.ConsensusProposal) error {
	slot := proposalSlotKey(proposal.Height, proposal.Round, proposal.Proposer)

	e.mu.Lock()
	existingHash, conflict := e.proposalSlots[slot]
	e.proposals[proposal.BlockHash] = proposal
	if !conflict {
		e.proposalSlots[slot] = proposal.BlockHash
	}
	e.mu.Unlock()

	if conflict && existingHash != proposal.BlockHash {
		e.recordEvidence(ctx, protocol.ConsensusEvidence{
			EvidenceType:         protocol.ConsensusEvidenceDoubleProposal,
			Validator:            proposal.Proposer,
			Height:               proposal.Height,
			Round:                proposal.Round,
			BlockHash:            existingHash,
			ConflictingBlockHash: proposal.BlockHash,
			ObservedAt:           time.Now().UTC(),
			Details:              "validator proposed multiple blocks for the same height and round",
		})
		return fmt.Errorf("detected double proposal from %s at height=%d round=%d", proposal.Proposer, proposal.Height, proposal.Round)
	}
	return nil
}

func (e *Engine) trackVote(ctx context.Context, vote protocol.ConsensusVote) error {
	slot := validatorVoteSlotKey(vote.Height, vote.Round, vote.Type, vote.Voter)

	e.mu.Lock()
	existing, conflict := e.voteSlots[slot]
	if !conflict || existing.BlockHash == vote.BlockHash {
		e.voteSlots[slot] = vote
	}
	e.mu.Unlock()

	if conflict && existing.BlockHash != vote.BlockHash {
		e.recordEvidence(ctx, protocol.ConsensusEvidence{
			EvidenceType:         protocol.ConsensusEvidenceDoubleVote,
			Validator:            vote.Voter,
			Height:               vote.Height,
			Round:                vote.Round,
			VoteType:             vote.Type,
			BlockHash:            existing.BlockHash,
			ConflictingBlockHash: vote.BlockHash,
			ObservedAt:           time.Now().UTC(),
			Details:              "validator voted for multiple blocks in the same vote step",
		})
		return fmt.Errorf("detected double vote from %s at height=%d round=%d type=%s", vote.Voter, vote.Height, vote.Round, vote.Type)
	}
	return nil
}

func (e *Engine) storeCertificate(ctx context.Context, certificate protocol.QuorumCertificate) error {
	key := voteKey(certificate.Height, certificate.Round, certificate.VoteType, certificate.BlockHash)

	e.mu.Lock()
	_, exists := e.certificates[key]
	e.certificates[key] = certificate
	currentPreferred, hasPreferred := e.preferredByHeight[certificate.Height]
	if !hasPreferred || betterCertificate(certificate, currentPreferred) {
		e.preferredByHeight[certificate.Height] = certificate
	}
	e.mu.Unlock()
	if exists {
		return nil
	}

	if e.recorder != nil {
		if err := e.recorder.RecordQuorumCertificate(ctx, certificate); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) commitCertifiedBlock(ctx context.Context, certificate protocol.QuorumCertificate) error {
	e.mu.RLock()
	if _, exists := e.committed[certificate.BlockHash]; exists {
		e.mu.RUnlock()
		return nil
	}
	preferred, ok := e.preferredByHeight[certificate.Height]
	if ok && preferred.BlockHash != certificate.BlockHash {
		e.mu.RUnlock()
		return nil
	}
	block, hasBlock := e.candidateBlocks[certificate.BlockHash]
	proposal, hasProposal := e.proposals[certificate.BlockHash]
	e.mu.RUnlock()
	if !hasBlock || !hasProposal || e.committer == nil {
		return nil
	}

	bundle := protocol.CertifiedBlock{
		Block:       block,
		Proposal:    proposal,
		Votes:       e.tracker.VotesFor(certificate.Height, certificate.Round, VoteTypePrecommit, certificate.BlockHash),
		Certificate: certificate,
	}
	if err := e.committer.ImportCertifiedBlock(ctx, bundle); err != nil {
		e.mu.Lock()
		delete(e.openHeights, certificate.Height)
		delete(e.candidateBlocks, certificate.BlockHash)
		e.mu.Unlock()
		return err
	}

	e.mu.Lock()
	e.committed[certificate.BlockHash] = struct{}{}
	delete(e.openHeights, certificate.Height)
	delete(e.candidateBlocks, certificate.BlockHash)
	delete(e.observedAt, certificate.Height)
	delete(e.currentRounds, certificate.Height)
	e.mu.Unlock()
	if e.peers != nil {
		e.peers.BroadcastCertifiedBlock(ctx, bundle)
	}
	return nil
}

func (e *Engine) recordEvidence(ctx context.Context, evidence protocol.ConsensusEvidence) {
	if e.recorder == nil {
		return
	}
	if err := e.recorder.RecordConsensusEvidence(ctx, evidence); err != nil {
		log.Printf("consensus evidence record error: %v", err)
	}
}

func (e *Engine) onTimeout(ctx context.Context) {
	if len(e.privateKey) == 0 || strings.TrimSpace(e.cfg.ValidatorAddress) == "" {
		return
	}

	info, err := e.chainState.GetChainInfo(ctx)
	if err != nil {
		return
	}

	height := info.HeadHeight + 1
	e.mu.RLock()
	currentRound := e.currentRoundLocked(height)
	observedAt, seen := e.observedAt[height]
	e.mu.RUnlock()
	if !seen {
		observedAt = time.Now().UTC().Add(-e.cfg.ConsensusRoundTimeout)
	}
	if time.Since(observedAt) < e.cfg.ConsensusRoundTimeout {
		return
	}

	nextRound := currentRound + 1
	roundChange := protocol.ConsensusRoundChange{
		ChainID:     e.cfg.ChainID,
		Height:      height,
		Round:       nextRound,
		Validator:   e.cfg.ValidatorAddress,
		Reason:      "timeout",
		RequestedAt: time.Now().UTC(),
	}
	signature, err := SignRoundChange(roundChange, e.privateKey)
	if err != nil {
		return
	}
	roundChange.Signature = signature

	if err := e.HandleRoundChange(ctx, roundChange); err != nil {
		return
	}
	if e.peers != nil {
		e.peers.BroadcastRoundChange(ctx, roundChange)
	}
}

func (e *Engine) recoverPersistentState(ctx context.Context, recovery RecoveryReader) error {
	certificates, err := recovery.ListQuorumCertificates(ctx, 256)
	if err != nil {
		return fmt.Errorf("recover quorum certificates: %w", err)
	}
	roundChanges, err := recovery.ListConsensusRoundChanges(ctx, 256)
	if err != nil {
		return fmt.Errorf("recover consensus round changes: %w", err)
	}
	preferences, err := recovery.ListForkChoicePreferences(ctx, 256)
	if err != nil {
		return fmt.Errorf("recover fork choice preferences: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, certificate := range certificates {
		key := voteKey(certificate.Height, certificate.Round, certificate.VoteType, certificate.BlockHash)
		e.certificates[key] = certificate
		currentPreferred, ok := e.preferredByHeight[certificate.Height]
		if !ok || betterCertificate(certificate, currentPreferred) {
			e.preferredByHeight[certificate.Height] = certificate
		}
	}
	for _, preference := range preferences {
		e.preferredByHeight[preference.Height] = preference.Certificate
	}

	for _, message := range roundChanges {
		if message.ChainID != e.cfg.ChainID {
			continue
		}
		if err := VerifyRoundChange(e.set, message); err != nil {
			continue
		}
		e.applyRoundChangeLocked(message)
	}

	return nil
}

func (e *Engine) currentRoundLocked(height int64) int {
	round := e.currentRounds[height]
	if round <= 0 {
		return 0
	}
	return round
}

func (e *Engine) applyRoundChangeLocked(message protocol.ConsensusRoundChange) {
	key := roundChangeKey(message.Height, message.Round)
	if _, ok := e.roundChanges[key]; !ok {
		e.roundChanges[key] = make(map[string]protocol.ConsensusRoundChange)
	}
	e.roundChanges[key][message.Validator] = message

	var power int64
	for validator := range e.roundChanges[key] {
		entry, ok := e.set.Get(validator)
		if ok {
			power += entry.Power
		}
	}
	if power >= e.set.QuorumPower() && message.Round > e.currentRoundLocked(message.Height) {
		e.currentRounds[message.Height] = message.Round
		delete(e.openHeights, message.Height)
		e.observedAt[message.Height] = time.Now().UTC()
	}
}

func betterCertificate(candidate protocol.QuorumCertificate, current protocol.QuorumCertificate) bool {
	if candidate.Height != current.Height {
		return candidate.Height > current.Height
	}
	if candidate.Round != current.Round {
		return candidate.Round > current.Round
	}
	if candidate.Power != current.Power {
		return candidate.Power > current.Power
	}
	if !candidate.CertifiedAt.Equal(current.CertifiedAt) {
		return candidate.CertifiedAt.Before(current.CertifiedAt)
	}
	return candidate.BlockHash < current.BlockHash
}

func proposalSlotKey(height int64, round int, proposer string) string {
	return fmt.Sprintf("%d/%d/%s", height, round, proposer)
}

func validatorVoteSlotKey(height int64, round int, voteType string, voter string) string {
	return fmt.Sprintf("%d/%d/%s/%s", height, round, voteType, voter)
}

func roundChangeKey(height int64, round int) string {
	return fmt.Sprintf("%d/%d", height, round)
}
