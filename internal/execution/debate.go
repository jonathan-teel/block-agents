package execution

import "aichain/internal/protocol"

const stagesPerRound = 4

func ComputeStageDurationSeconds(nowUnix int64, deadlineUnix int64, rounds int) int64 {
	totalStages := maxInt(rounds, 1) * stagesPerRound
	if deadlineUnix <= nowUnix {
		return 1
	}

	duration := (deadlineUnix - nowUnix) / int64(totalStages)
	if duration <= 0 {
		return 1
	}
	return duration
}

func NextDebateState(currentRound int, currentStage string, maxRounds int) (int, string, bool) {
	switch currentStage {
	case protocol.DebateStageProposal:
		return currentRound, protocol.DebateStageEvaluation, false
	case protocol.DebateStageEvaluation:
		return currentRound, protocol.DebateStageRebuttal, false
	case protocol.DebateStageRebuttal:
		return currentRound, protocol.DebateStageVote, false
	case protocol.DebateStageVote:
		if currentRound >= maxInt(maxRounds, 1) {
			return currentRound, protocol.DebateStageComplete, true
		}
		return currentRound + 1, protocol.DebateStageProposal, false
	default:
		return currentRound, protocol.DebateStageComplete, true
	}
}

func ClampStageDeadline(nextStartedAt int64, stageDurationSec int64, taskDeadline int64) int64 {
	nextDeadline := nextStartedAt + maxInt64(stageDurationSec, 1)
	if nextDeadline > taskDeadline {
		return taskDeadline
	}
	return nextDeadline
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
