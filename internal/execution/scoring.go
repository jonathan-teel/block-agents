package execution

import (
	"crypto/sha256"
	"math"
	"sort"

	"aichain/internal/protocol"
)

type WeightedSubmission struct {
	SubmissionID int64
	Agent        string
	Value        float64
	Stake        protocol.Amount
	Reputation   float64
}

type ScoredSubmission struct {
	SubmissionID  int64
	Agent         string
	Stake         protocol.Amount
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
		weight := submission.Stake.Float64() * Clamp01(submission.Reputation)
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
		total += score.Score * score.Stake.Float64()
	}
	return total
}

func ComputeRewards(scores []ScoredSubmission, rewardPool protocol.Amount) map[int64]protocol.Amount {
	rewards := make(map[int64]protocol.Amount, len(scores))
	total := RewardWeight(scores)

	if total == 0 || rewardPool <= 0 {
		for _, score := range scores {
			rewards[score.SubmissionID] = 0
		}
		return rewards
	}

	type rewardRemainder struct {
		SubmissionID int64
		Remainder    float64
	}

	remainders := make([]rewardRemainder, 0, len(scores))
	remaining := rewardPool
	for _, score := range scores {
		weight := score.Score * score.Stake.Float64()
		rawUnits := (weight / total) * float64(rewardPool)
		base := protocol.Amount(math.Floor(rawUnits))
		rewards[score.SubmissionID] = base
		remaining -= base
		remainders = append(remainders, rewardRemainder{
			SubmissionID: score.SubmissionID,
			Remainder:    rawUnits - float64(base),
		})
	}

	sort.Slice(remainders, func(i, j int) bool {
		if remainders[i].Remainder == remainders[j].Remainder {
			return remainders[i].SubmissionID < remainders[j].SubmissionID
		}
		return remainders[i].Remainder > remainders[j].Remainder
	})

	for index := protocol.Amount(0); index < remaining; index++ {
		target := remainders[int(index)%len(remainders)].SubmissionID
		rewards[target]++
	}

	return rewards
}

func ScaleAmount(amount protocol.Amount, factor float64) protocol.Amount {
	if amount <= 0 {
		return 0
	}
	return protocol.Amount(math.Round(float64(amount) * Clamp01(factor)))
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
