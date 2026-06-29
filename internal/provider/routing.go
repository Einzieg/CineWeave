package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

var defaultFallbackOn = []string{
	CodeProviderRateLimited,
	CodeProviderConcurrencyLimited,
	CodeProviderCircuitOpen,
	CodeUpstreamTimeout,
	CodeUpstreamInternalError,
	"UPSTREAM_RATE_LIMITED",
	CodeRateLimited,
}

var defaultStopOn = []string{
	CodeAuthFailed,
	CodeModelNotFound,
	CodeInvalidRequest,
	CodeUnsupportedCapability,
	CodeContentRejected,
}

func (s *Service) ResolveRoutingCandidates(ctx context.Context, req RoutingRequest) ([]RoutingCandidate, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.ModelProfileKey) == "" {
		return nil, fmt.Errorf("%w: organizationId and modelProfileKey are required", ErrValidation)
	}
	modality := routingModality(req)
	rows, err := s.db.Query(ctx, `
		SELECT
			p.id,
			p.profile_key,
			p.routing_strategy,
			p.fallback_strategy,
			b.id,
			b.priority,
			b.weight,
			b.created_at,
			m.id,
			m.provider_account_id,
			m.model_key,
			m.modality
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE p.organization_id = $1
		  AND p.profile_key = $2
		  AND b.enabled = true
		  AND m.status = 'active'
		  AND a.status = 'active'
		  AND ($3 = '' OR m.modality = $3 OR m.modality = 'multimodal')
		ORDER BY b.priority ASC, b.weight DESC, b.created_at ASC
	`, req.OrganizationID, strings.TrimSpace(req.ModelProfileKey), modality)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]RoutingCandidate, 0)
	for rows.Next() {
		var candidate RoutingCandidate
		var fallbackRaw []byte
		if err := rows.Scan(
			&candidate.ModelProfileID,
			&candidate.ModelProfileKey,
			&candidate.RoutingStrategy,
			&fallbackRaw,
			&candidate.ModelProfileBindingID,
			&candidate.Priority,
			&candidate.Weight,
			&candidate.createdAt,
			&candidate.ProviderModelID,
			&candidate.ProviderAccountID,
			&candidate.ModelKey,
			&candidate.Modality,
		); err != nil {
			return nil, err
		}
		candidate.RoutingStrategy = normalizeRoutingStrategyValue(candidate.RoutingStrategy)
		fallback, err := parseFallbackStrategy(rawOrDefault(fallbackRaw, "{}"))
		if err != nil {
			return nil, err
		}
		candidate.FallbackStrategy = fallback
		candidate.Capabilities, err = s.listCapabilities(ctx, candidate.ProviderModelID)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrValidation, CodeModelProfileNotConfigured)
	}

	strategy := RoutingStrategy(candidates[0].RoutingStrategy)
	switch strategy {
	case RoutingPriority:
		return candidates[:1], nil
	case RoutingWeighted:
		return orderWeightedCandidates(candidates, rand.Float64), nil
	case RoutingCostOptimized:
		for i := range candidates {
			candidates[i].estimatedCost = estimateRoutingCost(req, candidates[i].Capabilities)
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].estimatedCost != candidates[j].estimatedCost {
				return candidates[i].estimatedCost < candidates[j].estimatedCost
			}
			return routingPriorityLess(candidates[i], candidates[j])
		})
	case RoutingLatencyOptimized:
		if err := s.attachRoutingLatency(ctx, req.OrganizationID, strings.TrimSpace(req.TaskType), candidates); err != nil {
			return nil, err
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].hasLatency != candidates[j].hasLatency {
				return candidates[i].hasLatency
			}
			if candidates[i].hasLatency && candidates[i].averageLatencyMS != candidates[j].averageLatencyMS {
				return candidates[i].averageLatencyMS < candidates[j].averageLatencyMS
			}
			return routingPriorityLess(candidates[i], candidates[j])
		})
	default:
		sort.SliceStable(candidates, func(i, j int) bool {
			return routingPriorityLess(candidates[i], candidates[j])
		})
	}
	return candidates, nil
}

func routingModality(req RoutingRequest) string {
	if modality := strings.ToLower(strings.TrimSpace(req.Modality)); modality != "" {
		return modality
	}
	switch strings.TrimSpace(req.TaskType) {
	case TaskTypeTextGenerate, TaskTypeTextStream:
		return "text"
	case TaskTypeImageGenerate:
		return "image"
	case TaskTypeVideoCreateTask:
		return "video"
	default:
		return ""
	}
}

func normalizeRoutingStrategyValue(value string) string {
	switch RoutingStrategy(strings.TrimSpace(value)) {
	case RoutingPriority, RoutingPriorityWithFallback, RoutingWeighted, RoutingCostOptimized, RoutingLatencyOptimized:
		return strings.TrimSpace(value)
	default:
		return string(RoutingPriorityWithFallback)
	}
}

func validateRoutingStrategy(value string) (string, error) {
	strategy := strings.TrimSpace(value)
	if strategy == "" {
		return string(RoutingPriorityWithFallback), nil
	}
	switch RoutingStrategy(strategy) {
	case RoutingPriority, RoutingPriorityWithFallback, RoutingWeighted, RoutingCostOptimized, RoutingLatencyOptimized:
		return strategy, nil
	default:
		return "", fmt.Errorf("%w: routingStrategy is invalid", ErrValidation)
	}
}

func parseFallbackStrategy(raw json.RawMessage) (FallbackStrategy, error) {
	defaults := defaultFallbackStrategy()
	raw, err := normalizeJSON(raw, "{}")
	if err != nil {
		return FallbackStrategy{}, fmt.Errorf("%w: fallbackStrategy must be valid JSON", ErrValidation)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return FallbackStrategy{}, fmt.Errorf("%w: fallbackStrategy must be a JSON object", ErrValidation)
	}
	if len(probe) == 0 {
		return defaults, nil
	}
	var decoded struct {
		Enabled     *bool    `json:"enabled"`
		MaxAttempts int      `json:"maxAttempts"`
		FallbackOn  []string `json:"fallbackOn"`
		StopOn      []string `json:"stopOn"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return FallbackStrategy{}, fmt.Errorf("%w: fallbackStrategy must be a JSON object", ErrValidation)
	}
	if decoded.Enabled != nil {
		defaults.Enabled = *decoded.Enabled
	}
	if decoded.MaxAttempts > 0 {
		defaults.MaxAttempts = decoded.MaxAttempts
	}
	if len(decoded.FallbackOn) > 0 {
		defaults.FallbackOn = normalizeErrorCodeList(decoded.FallbackOn)
	}
	if len(decoded.StopOn) > 0 {
		defaults.StopOn = normalizeErrorCodeList(decoded.StopOn)
	}
	return defaults, nil
}

func validateFallbackStrategy(raw json.RawMessage) (json.RawMessage, error) {
	normalized, err := normalizeJSON(raw, "{}")
	if err != nil {
		return nil, fmt.Errorf("%w: fallbackStrategy must be valid JSON", ErrValidation)
	}
	strategy, err := parseFallbackStrategy(normalized)
	if err != nil {
		return nil, err
	}
	if strategy.MaxAttempts < 1 || strategy.MaxAttempts > 10 {
		return nil, fmt.Errorf("%w: fallbackStrategy.maxAttempts must be between 1 and 10", ErrValidation)
	}
	return normalized, nil
}

func defaultFallbackStrategy() FallbackStrategy {
	return FallbackStrategy{
		Enabled:     true,
		MaxAttempts: 3,
		FallbackOn:  append([]string(nil), defaultFallbackOn...),
		StopOn:      append([]string(nil), defaultStopOn...),
	}
}

func shouldFallback(errCode string, strategy FallbackStrategy) bool {
	if !strategy.Enabled || shouldStop(errCode, strategy) {
		return false
	}
	return errorCodeInList(errCode, strategy.FallbackOn)
}

func shouldStop(errCode string, strategy FallbackStrategy) bool {
	return errorCodeInList(errCode, strategy.StopOn)
}

func fallbackMaxAttempts(strategy FallbackStrategy, candidateCount int) int {
	maxAttempts := strategy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if !strategy.Enabled {
		maxAttempts = 1
	}
	if candidateCount > 0 && maxAttempts > candidateCount {
		maxAttempts = candidateCount
	}
	return maxAttempts
}

func normalizeErrorCodeList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		code := normalizeErrorCode(value)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		result = append(result, code)
	}
	return result
}

func errorCodeInList(code string, values []string) bool {
	code = normalizeErrorCode(code)
	for _, value := range values {
		if normalizeErrorCode(value) == code {
			return true
		}
	}
	return false
}

func normalizeErrorCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func routingPriorityLess(a, b RoutingCandidate) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if a.Weight != b.Weight {
		return a.Weight > b.Weight
	}
	return a.createdAt.Before(b.createdAt)
}

func orderWeightedCandidates(candidates []RoutingCandidate, random func() float64) []RoutingCandidate {
	remaining := append([]RoutingCandidate(nil), candidates...)
	ordered := make([]RoutingCandidate, 0, len(candidates))
	for len(remaining) > 0 {
		total := 0
		for _, candidate := range remaining {
			if candidate.Weight > 0 {
				total += candidate.Weight
			}
		}
		if total <= 0 {
			sort.SliceStable(remaining, func(i, j int) bool {
				return routingPriorityLess(remaining[i], remaining[j])
			})
			ordered = append(ordered, remaining...)
			break
		}
		pick := int(random() * float64(total))
		if pick >= total {
			pick = total - 1
		}
		running := 0
		selected := -1
		for i, candidate := range remaining {
			if candidate.Weight <= 0 {
				continue
			}
			running += candidate.Weight
			if pick < running {
				selected = i
				break
			}
		}
		if selected < 0 {
			selected = 0
		}
		ordered = append(ordered, remaining[selected])
		remaining = append(remaining[:selected], remaining[selected+1:]...)
	}
	return ordered
}

func estimateRoutingCost(req RoutingRequest, capabilities []Capability) float64 {
	switch routingModality(req) {
	case "text":
		inputTokens := req.EstimatedInputTokens
		if inputTokens <= 0 {
			inputTokens = 1000
		}
		outputTokens := req.MaxOutputTokens
		if outputTokens <= 0 {
			outputTokens = 1000
		}
		for _, capability := range capabilities {
			var policy map[string]any
			if err := json.Unmarshal(capability.PricingPolicy, &policy); err != nil || len(policy) == 0 {
				continue
			}
			inputRate := firstFloatPolicyField(policy, "inputTokenPer1K", "inputTokenCostPer1K", "promptTokenPer1K", "promptTokenCostPer1K", "inputPer1K")
			outputRate := firstFloatPolicyField(policy, "outputTokenPer1K", "outputTokenCostPer1K", "completionTokenPer1K", "completionTokenCostPer1K", "outputPer1K")
			return (float64(inputTokens)/1000.0)*inputRate + (float64(outputTokens)/1000.0)*outputRate
		}
	case "image":
		input := gatewayImageInput{Size: req.ImageSize, Quality: req.ImageQuality, N: 1}
		return decimalValue(estimateImageCost(input, capabilities).EstimatedCost)
	case "video":
		input := gatewayVideoInput{DurationSeconds: req.VideoDurationSeconds, Resolution: req.VideoResolution}
		if input.DurationSeconds <= 0 {
			input.DurationSeconds = 5
		}
		return decimalValue(estimateVideoCost(input, nil, capabilities).EstimatedCost)
	}
	return 0
}

func (s *Service) attachRoutingLatency(ctx context.Context, organizationID, taskType string, candidates []RoutingCandidate) error {
	for i := range candidates {
		var avg sql.NullFloat64
		if err := s.db.QueryRow(ctx, `
			SELECT avg(latency_ms)::float8
			FROM (
				SELECT latency_ms
				FROM provider_call_logs
				WHERE organization_id = $1
				  AND provider_model_id = $2
				  AND task_type = $3
				  AND status = 'succeeded'
				  AND latency_ms IS NOT NULL
				  AND created_at >= now() - interval '24 hours'
				ORDER BY created_at DESC
				LIMIT 20
			) recent
		`, organizationID, candidates[i].ProviderModelID, taskType).Scan(&avg); err != nil {
			return err
		}
		if avg.Valid {
			candidates[i].hasLatency = true
			candidates[i].averageLatencyMS = avg.Float64
		}
	}
	return nil
}
