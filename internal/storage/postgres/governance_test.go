package postgres

import "testing"

func TestValidateGovernanceParameterValue(t *testing.T) {
	if err := validateGovernanceParameterValue("task_dispute_bond", "25"); err != nil {
		t.Fatalf("expected task_dispute_bond to validate, got %v", err)
	}
	if err := validateGovernanceParameterValue("role_selection_policy", "balance_reputation"); err != nil {
		t.Fatalf("expected role_selection_policy to validate, got %v", err)
	}
}

func TestValidateGovernanceParameterValueRejectsInvalidValue(t *testing.T) {
	if err := validateGovernanceParameterValue("miner_vote_policy", "weighted_by_magic"); err == nil {
		t.Fatal("expected unsupported miner_vote_policy to fail")
	}
	if err := validateGovernanceParameterValue("unknown_parameter", "1"); err == nil {
		t.Fatal("expected unknown governance parameter to fail")
	}
}
