package node

import (
	"context"
	"log"
	"time"

	"aichain/internal/protocol"
)

func (s *Service) oracleLoop(ctx context.Context) {
	if s.oracles == nil {
		return
	}

	ticker := time.NewTicker(s.cfg.OraclePollInterval)
	defer ticker.Stop()

	s.pollOracles(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOracles(ctx)
		}
	}
}

func (s *Service) pollOracles(ctx context.Context) {
	tasks, err := s.store.ListPendingOracleTasks(ctx, time.Now().UTC(), 64)
	if err != nil {
		log.Printf("oracle poll list tasks error: %v", err)
		return
	}

	for _, task := range tasks {
		result, err := s.oracles.Resolve(ctx, task)
		if err != nil {
			log.Printf("oracle resolve task=%s source=%s error=%v", task.ID, task.Input.OracleSource, err)
			continue
		}

		report := protocol.OracleReport{
			TaskID:     task.ID,
			Source:     task.Input.OracleSource,
			Endpoint:   task.Input.OracleEndpoint,
			Path:       task.Input.OraclePath,
			Value:      result.Value,
			ObservedAt: time.Unix(result.ObservedAt, 0).UTC(),
			RawHash:    result.RawHash,
		}
		if err := s.store.RecordOracleReport(ctx, report); err != nil {
			log.Printf("oracle persist task=%s source=%s error=%v", task.ID, task.Input.OracleSource, err)
		}
	}
}
