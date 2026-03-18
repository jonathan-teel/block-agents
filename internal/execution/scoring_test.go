package execution

import "testing"

func TestComputeWeightedConsensus(t *testing.T) {
	submissions := []WeightedSubmission{
		{Value: 0.2, Stake: 10, Reputation: 0.5},
		{Value: 0.8, Stake: 20, Reputation: 1.0},
	}

	got, ok := ComputeWeightedConsensus(submissions, 100)
	if !ok {
		t.Fatal("expected consensus to be computed")
	}

	want := ((0.2 * 5) + (0.8 * 20)) / 25
	if got != want {
		t.Fatalf("unexpected consensus: got %.6f want %.6f", got, want)
	}
}

func TestComputeRewards(t *testing.T) {
	scores := []ScoredSubmission{
		{SubmissionID: 1, Stake: 10, Score: 1},
		{SubmissionID: 2, Stake: 30, Score: 0.5},
	}

	rewards := ComputeRewards(scores, 100)
	if rewards[1] != 40 {
		t.Fatalf("unexpected reward for submission 1: got %.2f want 40.00", rewards[1])
	}
	if rewards[2] != 60 {
		t.Fatalf("unexpected reward for submission 2: got %.2f want 60.00", rewards[2])
	}
}

func TestBlendReputation(t *testing.T) {
	got := BlendReputation(0.5, 1)
	if got != 0.55 {
		t.Fatalf("unexpected reputation: got %.2f want 0.55", got)
	}
}
