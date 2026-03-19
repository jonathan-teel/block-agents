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
	Kind         string  `json:"kind"`
	Statement    string  `json:"statement"`
	ReferenceIDs []int64 `json:"reference_ids,omitempty"`
	Digest       string  `json:"digest"`
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

		if document.Claims[index].Kind == "" {
			return Document{}, fmt.Errorf("claim %d kind is required", index)
		}
		if document.Claims[index].Statement == "" {
			return Document{}, fmt.Errorf("claim %d statement is required", index)
		}
		if !isAllowedClaimKind(stage, document.Claims[index].Kind) {
			return Document{}, fmt.Errorf("claim %d kind %s is not allowed for stage %s", index, document.Claims[index].Kind, stage)
		}
		sort.Slice(document.Claims[index].ReferenceIDs, func(i, j int) bool {
			return document.Claims[index].ReferenceIDs[i] < document.Claims[index].ReferenceIDs[j]
		})
		for refIndex, referenceID := range document.Claims[index].ReferenceIDs {
			if referenceID <= 0 {
				return Document{}, fmt.Errorf("claim %d reference_ids[%d] must be > 0", index, refIndex)
			}
			if refIndex > 0 && document.Claims[index].ReferenceIDs[refIndex-1] == referenceID {
				return Document{}, fmt.Errorf("claim %d reference_ids must be unique", index)
			}
		}
		document.Claims[index].Digest = protocol.HashStrings([]string{
			document.Claims[index].Kind,
			document.Claims[index].Statement,
			int64SliceDigest(document.Claims[index].ReferenceIDs),
		})
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
	if err := validateClaimReferenceBindings(document.Claims, document.References); err != nil {
		return Document{}, err
	}
	if err := validateClaimReferenceSemantics(stage, document); err != nil {
		return Document{}, err
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
		if artifactType == "plan" && !hasClaimKind(document.Claims, "plan") {
			return fmt.Errorf("proposal plan artifacts must include at least one plan claim")
		}
		if artifactType == "evidence" && !hasClaimKind(document.Claims, "evidence") {
			return fmt.Errorf("proposal evidence artifacts must include at least one evidence claim")
		}
		if hasClaimKind(document.Claims, "evidence") && !hasClaimReferenceKind(document.Claims, "evidence") {
			return fmt.Errorf("proposal evidence claims must bind to reference_ids")
		}
		if artifactType == "evidence" && len(document.References) == 0 {
			return fmt.Errorf("proposal evidence artifacts require at least one reference")
		}
		return validateReferenceTypes(document.References, map[string]struct{}{
			"proposal": {},
			"proof":    {},
		})
	case protocol.DebateStageEvaluation:
		if artifactType == "critique" && !hasClaimKind(document.Claims, "critique") && !hasClaimKind(document.Claims, "consistency") {
			return fmt.Errorf("evaluation critique artifacts must include critique or consistency claims")
		}
		if artifactType == "evidence" && !hasClaimKind(document.Claims, "evidence") {
			return fmt.Errorf("evaluation evidence artifacts must include at least one evidence claim")
		}
		if artifactType == "score_justification" && !hasClaimKind(document.Claims, "score") {
			return fmt.Errorf("score_justification artifacts must include at least one score claim")
		}
		if !hasClaimKind(document.Claims, "evidence") && !hasClaimKind(document.Claims, "critique") && !hasClaimKind(document.Claims, "score") {
			return fmt.Errorf("evaluation-stage proofs must include evidence, critique, or score claims")
		}
		if hasClaimKind(document.Claims, "evidence") && !hasClaimReferenceKind(document.Claims, "evidence") {
			return fmt.Errorf("evaluation evidence claims must bind to reference_ids")
		}
		if hasClaimKind(document.Claims, "score") && !hasClaimReferenceKind(document.Claims, "score") {
			return fmt.Errorf("evaluation score claims must bind to reference_ids")
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
		if artifactType == "ranking" && !hasClaimKind(document.Claims, "ranking") {
			return fmt.Errorf("vote ranking artifacts must include at least one ranking claim")
		}
		if !hasClaimKind(document.Claims, "ranking") && !hasClaimKind(document.Claims, "support") && !hasClaimKind(document.Claims, "preference") {
			return fmt.Errorf("vote-stage proofs must include ranking, support, or preference claims")
		}
		if (hasClaimKind(document.Claims, "ranking") || hasClaimKind(document.Claims, "support") || hasClaimKind(document.Claims, "preference")) &&
			!(hasClaimReferenceKind(document.Claims, "ranking") || hasClaimReferenceKind(document.Claims, "support") || hasClaimReferenceKind(document.Claims, "preference")) {
			return fmt.Errorf("vote-stage ranking claims must bind to reference_ids")
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

func validateClaimReferenceSemantics(stage string, document Document) error {
	if len(document.Claims) == 0 || len(document.References) == 0 {
		return nil
	}

	referencesByID := make(map[int64]Reference, len(document.References))
	for _, reference := range document.References {
		referencesByID[reference.ID] = reference
	}

	for index, claim := range document.Claims {
		switch stage {
		case protocol.DebateStageProposal:
			if claim.Kind == "evidence" {
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal", "proof"); err != nil {
					return err
				}
			}
		case protocol.DebateStageEvaluation:
			switch claim.Kind {
			case "evidence", "critique":
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal", "proof"); err != nil {
					return err
				}
			case "score":
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal"); err != nil {
					return err
				}
			case "consistency":
				if len(claim.ReferenceIDs) < 2 {
					return fmt.Errorf("evaluation consistency claims must bind at least two references")
				}
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal", "proof"); err != nil {
					return err
				}
			}
		case protocol.DebateStageVote:
			switch claim.Kind {
			case "ranking":
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal"); err != nil {
					return err
				}
			case "support", "preference":
				if err := requireClaimReferenceTypes(index, claim, referencesByID, "proposal", "evaluation", "proof"); err != nil {
					return err
				}
				if !claimHasReferenceType(claim, referencesByID, "proposal") {
					return fmt.Errorf("vote %s claims must bind at least one proposal reference", claim.Kind)
				}
			}
		}
	}

	return nil
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

func hasClaimReferenceKind(claims []Claim, kind string) bool {
	for _, claim := range claims {
		if claim.Kind == kind && len(claim.ReferenceIDs) > 0 {
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

func validateClaimReferenceBindings(claims []Claim, references []Reference) error {
	if len(claims) == 0 {
		return nil
	}
	if len(references) == 0 {
		for index, claim := range claims {
			if len(claim.ReferenceIDs) > 0 {
				return fmt.Errorf("claim %d references unknown reference ids", index)
			}
		}
		return nil
	}
	allowed := make(map[int64]struct{}, len(references))
	for _, reference := range references {
		allowed[reference.ID] = struct{}{}
	}
	for index, claim := range claims {
		for _, referenceID := range claim.ReferenceIDs {
			if _, ok := allowed[referenceID]; !ok {
				return fmt.Errorf("claim %d references unknown reference id %d", index, referenceID)
			}
		}
	}
	return nil
}

func requireClaimReferenceTypes(index int, claim Claim, referencesByID map[int64]Reference, allowedTypes ...string) error {
	if len(claim.ReferenceIDs) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(allowedTypes))
	for _, value := range allowedTypes {
		allowed[value] = struct{}{}
	}

	for _, referenceID := range claim.ReferenceIDs {
		reference, ok := referencesByID[referenceID]
		if !ok {
			return fmt.Errorf("claim %d references unknown reference id %d", index, referenceID)
		}
		if _, ok := allowed[reference.Type]; !ok {
			return fmt.Errorf("claim %d kind %s cannot bind reference type %s", index, claim.Kind, reference.Type)
		}
	}
	return nil
}

func claimHasReferenceType(claim Claim, referencesByID map[int64]Reference, referenceType string) bool {
	for _, referenceID := range claim.ReferenceIDs {
		reference, ok := referencesByID[referenceID]
		if ok && reference.Type == referenceType {
			return true
		}
	}
	return false
}

func int64SliceDigest(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ",")
}
