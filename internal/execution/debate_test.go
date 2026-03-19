package execution

import (
	"testing"

	"aichain/internal/protocol"
)

func TestComputeStageDurationSeconds(t *testing.T) {
	duration := ComputeStageDurationSeconds(100, 190, 2)
	if duration != 11 {
		t.Fatalf("expected duration 11, got %d", duration)
	}
}

func TestNextDebateState(t *testing.T) {
	round, stage, terminal := NextDebateState(1, protocol.DebateStageProposal, 2)
	if round != 1 || stage != protocol.DebateStageEvaluation || terminal {
		t.Fatalf("unexpected proposal transition: round=%d stage=%s terminal=%v", round, stage, terminal)
	}

	round, stage, terminal = NextDebateState(1, protocol.DebateStageEvaluation, 2)
	if round != 1 || stage != protocol.DebateStageRebuttal || terminal {
		t.Fatalf("unexpected evaluation transition: round=%d stage=%s terminal=%v", round, stage, terminal)
	}

	round, stage, terminal = NextDebateState(1, protocol.DebateStageVote, 2)
	if round != 2 || stage != protocol.DebateStageProposal || terminal {
		t.Fatalf("unexpected vote transition: round=%d stage=%s terminal=%v", round, stage, terminal)
	}

	round, stage, terminal = NextDebateState(2, protocol.DebateStageVote, 2)
	if round != 2 || stage != protocol.DebateStageComplete || !terminal {
		t.Fatalf("unexpected terminal transition: round=%d stage=%s terminal=%v", round, stage, terminal)
	}
}
