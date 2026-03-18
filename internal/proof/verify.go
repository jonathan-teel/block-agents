package proof

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"aichain/internal/protocol"
)

type Document struct {
	SchemaVersion int         `json:"schema_version"`
	Summary       string      `json:"summary"`
	Claims        []Claim     `json:"claims"`
	References    []Reference `json:"references,omitempty"`
	Conclusion    string      `json:"conclusion"`
}

type Claim struct {
	Kind      string `json:"kind"`
	Statement string `json:"statement"`
	Digest    string `json:"digest"`
}

type Reference struct {
	Type   string `json:"type"`
	ID     int64  `json:"id"`
	Digest string `json:"digest"`
}

type VerifiedArtifact struct {
	NormalizedContent string
	ContentHash       string
	ClaimRoot         string
	SemanticRoot      string
	References        []Reference
}

func VerifyArtifact(stage string, artifactType string, content string) (VerifiedArtifact, error) {
	document, err := parseDocument(stage, artifactType, content)
	if err != nil {
		return VerifiedArtifact{}, err
	}
	return buildVerifiedArtifact(stage, artifactType, document)
}

func FinalizeArtifact(stage string, artifactType string, content string, references []Reference) (VerifiedArtifact, error) {
	document, err := parseDocument(stage, artifactType, content)
	if err != nil {
		return VerifiedArtifact{}, err
	}
	if len(references) > 0 {
		document.References = references
	}
	return buildVerifiedArtifact(stage, artifactType, document)
}

func parseDocument(stage string, artifactType string, content string) (Document, error) {
	if !isAllowedArtifactType(stage, artifactType) {
		return Document{}, fmt.Errorf("artifact_type %s is not allowed for stage %s", artifactType, stage)
	}

	var document Document
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return Document{}, fmt.Errorf("proof content must be valid JSON: %w", err)
	}

	document.Summary = strings.TrimSpace(document.Summary)
	document.Conclusion = strings.TrimSpace(document.Conclusion)
	if document.SchemaVersion == 0 {
		document.SchemaVersion = 1
	}
	if document.SchemaVersion != 1 {
		return Document{}, fmt.Errorf("unsupported proof schema_version %d", document.SchemaVersion)
	}
	if document.Summary == "" {
		return Document{}, fmt.Errorf("proof summary is required")
	}
	if document.Conclusion == "" {
		return Document{}, fmt.Errorf("proof conclusion is required")
	}
	if len(document.Claims) == 0 {
		return Document{}, fmt.Errorf("proof must contain at least one claim")
	}

	for index := range document.Claims {
		document.Claims[index].Kind = strings.ToLower(strings.TrimSpace(document.Claims[index].Kind))
		document.Claims[index].Statement = strings.TrimSpace(document.Claims[index].Statement)
		document.Claims[index].Digest = protocol.HashStrings([]string{document.Claims[index].Kind, document.Claims[index].Statement})

		if document.Claims[index].Kind == "" {
			return Document{}, fmt.Errorf("claim %d kind is required", index)
		}
		if document.Claims[index].Statement == "" {
			return Document{}, fmt.Errorf("claim %d statement is required", index)
		}
		if !isAllowedClaimKind(stage, document.Claims[index].Kind) {
			return Document{}, fmt.Errorf("claim %d kind %s is not allowed for stage %s", index, document.Claims[index].Kind, stage)
		}
	}
	sort.Slice(document.Claims, func(i, j int) bool {
		if document.Claims[i].Digest == document.Claims[j].Digest {
			return document.Claims[i].Kind < document.Claims[j].Kind
		}
		return document.Claims[i].Digest < document.Claims[j].Digest
	})
	for index := 1; index < len(document.Claims); index++ {
		if document.Claims[index-1].Digest == document.Claims[index].Digest {
			return Document{}, fmt.Errorf("proof claims must be unique")
		}
	}

	for index := range document.References {
		document.References[index].Type = strings.ToLower(strings.TrimSpace(document.References[index].Type))
		if document.References[index].Type == "" {
			return Document{}, fmt.Errorf("reference %d type is required", index)
		}
		if document.References[index].ID <= 0 {
			return Document{}, fmt.Errorf("reference %d id must be > 0", index)
		}
	}

	sort.Slice(document.References, func(i, j int) bool {
		if document.References[i].Type == document.References[j].Type {
			return document.References[i].ID < document.References[j].ID
		}
		return document.References[i].Type < document.References[j].Type
	})
	for index := 1; index < len(document.References); index++ {
		if document.References[index-1].Type == document.References[index].Type && document.References[index-1].ID == document.References[index].ID {
			return Document{}, fmt.Errorf("proof references must be unique")
		}
	}

	if requiresReferences(stage, artifactType) && len(document.References) == 0 {
		return Document{}, fmt.Errorf("proof artifact_type %s requires at least one reference", artifactType)
	}
	if stage == protocol.DebateStageVote && len(document.References) == 0 {
		return Document{}, fmt.Errorf("vote-stage proofs require at least one reference")
	}
	if err := validateStageSemantics(stage, artifactType, document); err != nil {
		return Document{}, err
	}

	return document, nil
}

func buildVerifiedArtifact(stage string, artifactType string, document Document) (VerifiedArtifact, error) {
	normalizedBytes, err := json.Marshal(document)
	if err != nil {
		return VerifiedArtifact{}, fmt.Errorf("marshal normalized proof: %w", err)
	}

	parts := []string{
		stage,
		artifactType,
		document.Summary,
		document.Conclusion,
	}
	for _, claim := range document.Claims {
		parts = append(parts, claim.Kind, claim.Digest, claim.Statement)
	}
	for _, reference := range document.References {
		parts = append(parts, reference.Type, fmt.Sprintf("%d", reference.ID), reference.Digest)
	}

	claimDigests := make([]string, 0, len(document.Claims))
	for _, claim := range document.Claims {
		claimDigests = append(claimDigests, protocol.HashStrings([]string{claim.Kind, claim.Digest, claim.Statement}))
	}
	claimRoot := protocol.ComputeMerkleRoot(claimDigests)
	parts = append(parts, claimRoot)

	return VerifiedArtifact{
		NormalizedContent: string(normalizedBytes),
		ContentHash:       protocol.HashBytes(normalizedBytes),
		ClaimRoot:         claimRoot,
		SemanticRoot:      protocol.HashStrings(parts),
		References:        document.References,
	}, nil
}

func isAllowedArtifactType(stage string, artifactType string) bool {
	switch stage {
	case protocol.DebateStageProposal:
		return artifactType == "draft" || artifactType == "plan" || artifactType == "evidence"
	case protocol.DebateStageEvaluation:
		return artifactType == "critique" || artifactType == "evidence" || artifactType == "score_justification"
	case protocol.DebateStageVote:
		return artifactType == "ballot_rationale" || artifactType == "ranking"
	default:
		return false
	}
}

func requiresReferences(stage string, artifactType string) bool {
	if stage == protocol.DebateStageEvaluation || stage == protocol.DebateStageVote {
		return true
	}
	return artifactType == "evidence"
}

func isAllowedClaimKind(stage string, kind string) bool {
	switch stage {
	case protocol.DebateStageProposal:
		return kind == "observation" || kind == "hypothesis" || kind == "plan" || kind == "evidence"
	case protocol.DebateStageEvaluation:
		return kind == "evidence" || kind == "critique" || kind == "score" || kind == "consistency"
	case protocol.DebateStageVote:
		return kind == "ranking" || kind == "support" || kind == "preference"
	default:
		return false
	}
}

func validateStageSemantics(stage string, artifactType string, document Document) error {
	switch stage {
	case protocol.DebateStageProposal:
		if artifactType == "evidence" && len(document.References) == 0 {
			return fmt.Errorf("proposal evidence artifacts require at least one reference")
		}
		return validateReferenceTypes(document.References, map[string]struct{}{
			"proposal": {},
			"proof":    {},
		})
	case protocol.DebateStageEvaluation:
		if !hasClaimKind(document.Claims, "evidence") && !hasClaimKind(document.Claims, "critique") && !hasClaimKind(document.Claims, "score") {
			return fmt.Errorf("evaluation-stage proofs must include evidence, critique, or score claims")
		}
		if err := validateReferenceTypes(document.References, map[string]struct{}{
			"proposal": {},
			"proof":    {},
		}); err != nil {
			return err
		}
		if !hasReferenceType(document.References, "proposal") && !hasReferenceType(document.References, "proof") {
			return fmt.Errorf("evaluation-stage proofs must reference at least one proposal or proof")
		}
		return nil
	case protocol.DebateStageVote:
		if !hasClaimKind(document.Claims, "ranking") && !hasClaimKind(document.Claims, "support") && !hasClaimKind(document.Claims, "preference") {
			return fmt.Errorf("vote-stage proofs must include ranking, support, or preference claims")
		}
		if err := validateReferenceTypes(document.References, map[string]struct{}{
			"proposal":   {},
			"evaluation": {},
			"proof":      {},
		}); err != nil {
			return err
		}
		if !hasReferenceType(document.References, "proposal") {
			return fmt.Errorf("vote-stage proofs must reference at least one proposal")
		}
		return nil
	default:
		return fmt.Errorf("unsupported debate stage %s", stage)
	}
}

func validateReferenceTypes(references []Reference, allowed map[string]struct{}) error {
	for index, reference := range references {
		if _, ok := allowed[reference.Type]; !ok {
			return fmt.Errorf("reference %d type %s is not allowed for this stage", index, reference.Type)
		}
	}
	return nil
}

func hasClaimKind(claims []Claim, kind string) bool {
	for _, claim := range claims {
		if claim.Kind == kind {
			return true
		}
	}
	return false
}

func hasReferenceType(references []Reference, referenceType string) bool {
	for _, reference := range references {
		if reference.Type == referenceType {
			return true
		}
	}
	return false
}
