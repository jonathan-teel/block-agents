package consensus

import (
	"context"
	"log"
	"time"

	"aichain/internal/config"
	"aichain/internal/storage/postgres"
)

type Sequencer struct {
	cfg    config.Config
	store  *postgres.Store
	engine *Engine
}

func New(cfg config.Config, store *postgres.Store, engine *Engine) *Sequencer {
	return &Sequencer{
		cfg:   cfg,
		store: store,
		engine: engine,
	}
}

func (s *Sequencer) Run(ctx context.Context) {
	s.seal(ctx)

	ticker := time.NewTicker(s.cfg.BlockInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.seal(ctx)
		}
	}
}

func (s *Sequencer) seal(ctx context.Context) {
	if s.engine != nil {
		info, err := s.store.GetChainInfo(ctx)
		if err == nil && !s.engine.ShouldSealNext(info.HeadHeight+1) {
			return
		}
	}

	proposer := s.cfg.ValidatorAddress
	if proposer == "" {
		proposer = s.cfg.NodeID
	}

	if s.engine == nil {
		block, created, err := s.store.SealPendingBlock(ctx, postgres.SealOptions{
			Proposer:           proposer,
			MaxTransactions:    s.cfg.MaxTransactionsPerBlock,
			MaxEffectiveWeight: s.cfg.MaxEffectiveWeight,
			CreateEmptyBlocks:  s.cfg.CreateEmptyBlocks,
			Now:                time.Now().UTC(),
		})
		if err != nil {
			log.Printf("sequencer error: %v", err)
			return
		}
		if !created {
			return
		}

		log.Printf(
			"sealed block height=%d hash=%s txs=%d events=%d",
			block.Header.Height,
			block.Hash,
			len(block.Transactions),
			len(block.Events),
		)
		return
	}

	block, created, err := s.store.BuildCandidateBlock(ctx, postgres.SealOptions{
		Proposer:           proposer,
		MaxTransactions:    s.cfg.MaxTransactionsPerBlock,
		MaxEffectiveWeight: s.cfg.MaxEffectiveWeight,
		CreateEmptyBlocks:  s.cfg.CreateEmptyBlocks,
		Now:                time.Now().UTC(),
	})
	if err != nil {
		log.Printf("sequencer error: %v", err)
		return
	}
	if !created {
		return
	}
	s.engine.ObserveCandidate(ctx, *block)

	log.Printf(
		"built candidate block height=%d hash=%s txs=%d events=%d",
		block.Header.Height,
		block.Hash,
		len(block.Transactions),
		len(block.Events),
	)
}
