package proof

import "testing"

func TestVerifyArtifact(t *testing.T) {
	content := `{
		"summary": "draft reasoning",
		"claims": [
			{"kind": "observation", "statement": "proposal A references the task question"}
		],
		"references": [
			{"type": "proposal", "id": 1}
		],
		"conclusion": "proposal A is internally coherent"
	}`

	artifact, err := VerifyArtifact("evaluation", "critique", content)
	if err != nil {
		t.Fatalf("verify artifact: %v", err)
	}
	if artifact.ContentHash == "" || artifact.SemanticRoot == "" {
		t.Fatal("expected non-empty content hash and semantic root")
	}
	if artifact.ClaimRoot == "" {
		t.Fatal("expected non-empty claim root")
	}
	if len(artifact.References) != 1 {
		t.Fatalf("expected one reference, got %d", len(artifact.References))
	}
}

func TestVoteArtifactRequiresReference(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "vote reasoning",
		"claims": [
			{"kind": "ranking", "statement": "proposal A is strongest"}
		],
		"conclusion": "proposal A should win"
	}`

	if _, err := VerifyArtifact("vote", "ballot_rationale", content); err == nil {
		t.Fatal("expected vote-stage proof without references to fail")
	}
}

func TestEvaluationArtifactRequiresAllowedClaimKinds(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "evaluation reasoning",
		"claims": [
			{"kind": "ranking", "statement": "proposal A is strongest"}
		],
		"references": [
			{"type": "proposal", "id": 1}
		],
		"conclusion": "proposal A is stronger"
	}`

	if _, err := VerifyArtifact("evaluation", "critique", content); err == nil {
		t.Fatal("expected evaluation-stage proof with vote claim kind to fail")
	}
}

func TestVoteArtifactRequiresProposalReference(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "vote reasoning",
		"claims": [
			{"kind": "ranking", "statement": "proposal A is strongest"}
		],
		"references": [
			{"type": "evaluation", "id": 2}
		],
		"conclusion": "proposal A should win"
	}`

	if _, err := VerifyArtifact("vote", "ballot_rationale", content); err == nil {
		t.Fatal("expected vote-stage proof without proposal reference to fail")
	}
}
