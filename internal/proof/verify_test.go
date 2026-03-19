package proof

import "testing"

func TestVerifyArtifact(t *testing.T) {
	content := `{
		"summary": "draft reasoning",
		"claims": [
			{"kind": "critique", "statement": "proposal A references the task question", "reference_ids": [1]}
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
			{"kind": "ranking", "statement": "proposal A is strongest", "reference_ids": [1]}
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
			{"kind": "ranking", "statement": "proposal A is strongest", "reference_ids": [2]}
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

func TestVoteArtifactClaimReferenceMustExistInDocumentReferences(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "vote reasoning",
		"claims": [
			{"kind": "ranking", "statement": "proposal A is strongest", "reference_ids": [99]}
		],
		"references": [
			{"type": "proposal", "id": 1}
		],
		"conclusion": "proposal A should win"
	}`

	if _, err := VerifyArtifact("vote", "ballot_rationale", content); err == nil {
		t.Fatal("expected claim-level reference binding to fail")
	}
}

func TestClaimReferencesRequireDocumentReferences(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "proposal reasoning",
		"claims": [
			{"kind": "plan", "statement": "break the task into steps", "reference_ids": [1]}
		],
		"conclusion": "proposal should proceed"
	}`

	if _, err := VerifyArtifact("proposal", "plan", content); err == nil {
		t.Fatal("expected claim reference ids without document references to fail")
	}
}

func TestEvaluationScoreClaimRequiresProposalReference(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "score reasoning",
		"claims": [
			{"kind": "score", "statement": "the proposal deserves a high score", "reference_ids": [7]}
		],
		"references": [
			{"type": "proof", "id": 7}
		],
		"conclusion": "score should be high"
	}`

	if _, err := VerifyArtifact("evaluation", "score_justification", content); err == nil {
		t.Fatal("expected score claim without proposal reference to fail")
	}
}

func TestEvaluationConsistencyClaimRequiresTwoReferences(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "consistency reasoning",
		"claims": [
			{"kind": "consistency", "statement": "the support is internally consistent", "reference_ids": [1]}
		],
		"references": [
			{"type": "proposal", "id": 1}
		],
		"conclusion": "the record is coherent"
	}`

	if _, err := VerifyArtifact("evaluation", "critique", content); err == nil {
		t.Fatal("expected single-reference consistency claim to fail")
	}
}

func TestVoteRankingArtifactRequiresRankingClaim(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "vote reasoning",
		"claims": [
			{"kind": "support", "statement": "proposal A is well supported", "reference_ids": [1]}
		],
		"references": [
			{"type": "proposal", "id": 1}
		],
		"conclusion": "proposal A should win"
	}`

	if _, err := VerifyArtifact("vote", "ranking", content); err == nil {
		t.Fatal("expected ranking artifact without ranking claim to fail")
	}
}

func TestVotePreferenceClaimRequiresProposalReference(t *testing.T) {
	content := `{
		"schema_version": 1,
		"summary": "vote preference",
		"claims": [
			{"kind": "preference", "statement": "the evaluation supports the outcome", "reference_ids": [2]}
		],
		"references": [
			{"type": "evaluation", "id": 2}
		],
		"conclusion": "proposal A should win"
	}`

	if _, err := VerifyArtifact("vote", "ballot_rationale", content); err == nil {
		t.Fatal("expected vote preference without proposal reference to fail")
	}
}
