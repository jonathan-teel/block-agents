package execution

import (
	"crypto/sha256"
	"math"

	"aichain/internal/protocol"
)

type WeightedSubmission struct {
	SubmissionID int64
	Agent        string
	Value        float64
	Stake        float64
	Reputation   float64
}

type ScoredSubmission struct {
	SubmissionID  int64
	Agent         string
	Stake         float64
	OldReputation float64
	Score         float64
}

func ResolveOutcome(task protocol.Task) float64 {
	sum := sha256.Sum256([]byte(task.ID + ":" + task.Input.Question))
	if sum[0]%2 == 0 {
		return 0
	}
	return 1
}

func ComputeWeightedConsensus(submissions []WeightedSubmission, maxWeight float64) (float64, bool) {
	var numerator float64
	var denominator float64

	for _, submission := range submissions {
		weight := submission.Stake * Clamp01(submission.Reputation)
		if maxWeight > 0 && weight > maxWeight {
			weight = maxWeight
		}
		if weight <= 0 {
			continue
		}

		numerator += submission.Value * weight
		denominator += weight
	}

	if denominator == 0 {
		return 0, false
	}

	return numerator / denominator, true
}

func ScoreSubmissions(submissions []WeightedSubmission, outcome float64) []ScoredSubmission {
	scored := make([]ScoredSubmission, 0, len(submissions))
	for _, submission := range submissions {
		score := 1 - math.Abs(submission.Value-outcome)
		scored = append(scored, ScoredSubmission{
			SubmissionID:  submission.SubmissionID,
			Agent:         submission.Agent,
			Stake:         submission.Stake,
			OldReputation: Clamp01(submission.Reputation),
			Score:         Clamp01(score),
		})
	}
	return scored
}

func RewardWeight(scores []ScoredSubmission) float64 {
	var total float64
	for _, score := range scores {
		total += score.Score * score.Stake
	}
	return total
}

func ComputeRewards(scores []ScoredSubmission, rewardPool float64) map[int64]float64 {
	rewards := make(map[int64]float64, len(scores))
	total := RewardWeight(scores)

	if total == 0 {
		for _, score := range scores {
			rewards[score.SubmissionID] = 0
		}
		return rewards
	}

	for _, score := range scores {
		rewards[score.SubmissionID] = ((score.Score * score.Stake) / total) * rewardPool
	}

	return rewards
}

func BlendReputation(oldReputation float64, score float64) float64 {
	return Clamp01(oldReputation*0.9 + Clamp01(score)*0.1)
}

func Clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}
