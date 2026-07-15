package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	PromptLengthUnitCharacters = "characters"
	PromptLengthUnitUTF8Bytes  = "utf8_bytes"
)

type PromptLengthConstraint struct {
	MaxLength int    `json:"maxLength"`
	Unit      string `json:"unit"`
}

type GatewayModelConstraintsRequest struct {
	OrganizationID  string `json:"organizationId"`
	ModelProfileKey string `json:"modelProfileKey"`
	TaskType        string `json:"taskType"`
	Modality        string `json:"modality"`
}

type GatewayModelConstraintCandidate struct {
	ProviderModelID string                 `json:"providerModelId"`
	ModelKey        string                 `json:"modelKey"`
	Modality        string                 `json:"modality"`
	Prompt          PromptLengthConstraint `json:"prompt"`
}

type GatewayModelConstraintsResponse struct {
	ModelProfileKey string                            `json:"modelProfileKey"`
	TaskType        string                            `json:"taskType"`
	Modality        string                            `json:"modality"`
	Candidates      []GatewayModelConstraintCandidate `json:"candidates"`
}

func (s *Service) ResolveModelConstraints(ctx context.Context, req GatewayModelConstraintsRequest) (GatewayModelConstraintsResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.ModelProfileKey) == "" {
		return GatewayModelConstraintsResponse{}, fmt.Errorf("%w: organizationId and modelProfileKey are required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  req.OrganizationID,
		ModelProfileKey: req.ModelProfileKey,
		TaskType:        req.TaskType,
		Modality:        req.Modality,
	})
	if err != nil {
		return GatewayModelConstraintsResponse{}, err
	}
	response := GatewayModelConstraintsResponse{
		ModelProfileKey: strings.TrimSpace(req.ModelProfileKey),
		TaskType:        strings.TrimSpace(req.TaskType),
		Modality:        strings.TrimSpace(req.Modality),
		Candidates:      make([]GatewayModelConstraintCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		response.Candidates = append(response.Candidates, GatewayModelConstraintCandidate{
			ProviderModelID: candidate.ProviderModelID,
			ModelKey:        candidate.ModelKey,
			Modality:        candidate.Modality,
			Prompt:          ModelPromptLengthConstraint(candidate.Capabilities),
		})
	}
	return response, nil
}

func ModelPromptLengthConstraint(capabilities []Capability) PromptLengthConstraint {
	constraint := PromptLengthConstraint{}
	for _, capability := range capabilities {
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			values := promptConstraintValues(raw)
			candidate := firstPositivePromptLimit(values)
			if candidate <= 0 || (constraint.MaxLength > 0 && candidate >= constraint.MaxLength) {
				continue
			}
			constraint.MaxLength = candidate
			constraint.Unit = normalizePromptLengthUnit(promptConstraintString(values, "promptLengthUnit", "promptLimitUnit"))
		}
	}
	if constraint.Unit == "" {
		constraint.Unit = PromptLengthUnitCharacters
	}
	return constraint
}

func MeasurePromptLength(prompt, unit string) int {
	switch normalizePromptLengthUnit(unit) {
	case PromptLengthUnitUTF8Bytes:
		return len([]byte(strings.TrimSpace(prompt)))
	default:
		return len([]rune(strings.TrimSpace(prompt)))
	}
}

func PromptWithinConstraint(prompt string, constraint PromptLengthConstraint) bool {
	return constraint.MaxLength <= 0 || MeasurePromptLength(prompt, constraint.Unit) <= constraint.MaxLength
}

func promptConstraintValues(raw json.RawMessage) map[string]any {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	if nested, ok := values["xCapabilities"].(map[string]any); ok {
		return nested
	}
	return values
}

func firstPositivePromptLimit(values map[string]any) int {
	for _, key := range []string{"promptMaxLength", "maxPromptLength", "maxPromptCharacters"} {
		if value := int(floatField(values[key], key)); value > 0 {
			return value
		}
	}
	return 0
}

func promptConstraintString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizePromptLengthUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "byte", "bytes", "utf8_byte", "utf8_bytes", "utf-8-byte", "utf-8-bytes":
		return PromptLengthUnitUTF8Bytes
	case "character", "characters", "char", "chars", "rune", "runes":
		return PromptLengthUnitCharacters
	default:
		return ""
	}
}
