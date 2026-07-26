package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// BuildCommerceDirectVideoSnapshots freezes the currently executable video
// routes without making an upstream provider request.
func (s *Service) BuildCommerceDirectVideoSnapshots(
	ctx context.Context,
	organizationID string,
	modelProfileKey string,
	taskType string,
	modality string,
) (json.RawMessage, json.RawMessage, error) {
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  organizationID,
		ModelProfileKey: modelProfileKey,
		TaskType:        taskType,
		Modality:        modality,
	})
	if err != nil {
		return nil, nil, err
	}

	routes := make([]map[string]any, 0, len(candidates))
	capabilityCandidates := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		variants, variantErr := ExecutableVideoGenerationVariants(
			candidate.Capabilities,
			Model{
				ID:                candidate.ProviderModelID,
				ProviderAccountID: candidate.ProviderAccountID,
				ModelKey:          candidate.ModelKey,
				Modality:          candidate.Modality,
				Capabilities:      candidate.Capabilities,
			},
		)
		if variantErr != nil {
			continue
		}

		variantSnapshots := make([]map[string]any, 0, len(variants))
		for _, variant := range variants {
			durations, durationErr := ExecutableWholeSecondDurationsForVideoVariant(variant)
			if durationErr != nil || len(durations) == 0 || len(variant.Resolutions) == 0 {
				continue
			}
			hash, hashErr := VideoGenerationVariantSnapshotHash(variant)
			if hashErr != nil {
				return nil, nil, hashErr
			}
			variantSnapshots = append(variantSnapshots, map[string]any{
				"variantKey":                  variant.VariantKey,
				"capabilitySnapshotHash":      hash,
				"executableDurationSeconds":   durations,
				"resolutions":                 variant.Resolutions,
				"aspectRatios":                variant.AspectRatios,
				"supportsContinuousExtension": variant.Continuation.SupportsExtension,
				"capability":                  variant,
			})
		}
		if len(variantSnapshots) == 0 {
			continue
		}

		route := map[string]any{
			"modelProfileId":        candidate.ModelProfileID,
			"modelProfileKey":       candidate.ModelProfileKey,
			"modelProfileBindingId": candidate.ModelProfileBindingID,
			"providerModelId":       candidate.ProviderModelID,
			"providerAccountId":     candidate.ProviderAccountID,
			"modelKey":              candidate.ModelKey,
			"modality":              candidate.Modality,
			"priority":              candidate.Priority,
			"weight":                candidate.Weight,
		}
		routes = append(routes, route)
		capabilityCandidates = append(capabilityCandidates, map[string]any{
			"modelProfileId":          candidate.ModelProfileID,
			"modelProfileKey":         candidate.ModelProfileKey,
			"modelProfileBindingId":   candidate.ModelProfileBindingID,
			"providerModelId":         candidate.ProviderModelID,
			"providerAccountId":       candidate.ProviderAccountID,
			"modelKey":                candidate.ModelKey,
			"modality":                candidate.Modality,
			"priority":                candidate.Priority,
			"weight":                  candidate.Weight,
			"capabilities":            candidate.Capabilities,
			"videoGenerationVariants": variantSnapshots,
		})
	}
	if len(routes) == 0 {
		return nil, nil, fmt.Errorf(
			"%w: no configured video model exposes executable integer durations and resolutions",
			ErrValidation,
		)
	}

	routing, err := json.Marshal(map[string]any{
		"videoGenerator": map[string]any{
			"request": map[string]any{
				"organizationId":  organizationID,
				"modelProfileKey": modelProfileKey,
				"taskType":        taskType,
				"modality":        modality,
			},
			"candidates": routes,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	capabilities, err := json.Marshal(map[string]any{
		"videoGenerator": map[string]any{
			"providerModelId": capabilityCandidates[0]["providerModelId"],
			"candidates":      capabilityCandidates,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return routing, capabilities, nil
}
