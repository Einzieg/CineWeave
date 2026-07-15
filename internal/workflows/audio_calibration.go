package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type RefreshTimingCalibrationProfileInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId,omitempty"`
}

type TimingCalibrationParameters struct {
	PunctuationPauseScale float64            `json:"punctuationPauseScale"`
	ActionDurationScales  map[string]float64 `json:"actionDurationScales"`
	ShotPacingScale       float64            `json:"shotPacingScale"`
	DialogueObservedScale float64            `json:"dialogueObservedScale"`
	SampleCounts          map[string]int     `json:"sampleCounts"`
}

type RefreshTimingCalibrationProfileOutput struct {
	ProfileID   string                      `json:"profileId"`
	Revision    int                         `json:"revision"`
	SampleCount int                         `json:"sampleCount"`
	Parameters  TimingCalibrationParameters `json:"parameters"`
}

func (a Activities) RefreshTimingCalibrationProfile(ctx context.Context, input RefreshTimingCalibrationProfileInput) (RefreshTimingCalibrationProfileOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" {
		return RefreshTimingCalibrationProfileOutput{}, fmt.Errorf("organizationId and projectId are required")
	}
	var audioConfigurationRevision int
	if err := a.db.QueryRow(ctx, `SELECT audio_configuration_revision FROM projects WHERE organization_id = $1 AND id = $2`, input.OrganizationID, input.ProjectID).Scan(&audioConfigurationRevision); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	rows, err := a.db.Query(ctx, `
		SELECT sample_kind, sample_key, expected_ticks, actual_ticks, confidence::float8
		FROM timing_calibration_samples
		WHERE organization_id = $1 AND project_id = $2 AND audio_configuration_revision = $3
		ORDER BY created_at DESC
		LIMIT 2000
	`, input.OrganizationID, input.ProjectID, audioConfigurationRevision)
	if err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	defer rows.Close()
	groups := map[string][]float64{}
	total := 0
	for rows.Next() {
		var kind, key string
		var expected, actual int64
		var confidence float64
		if err := rows.Scan(&kind, &key, &expected, &actual, &confidence); err != nil {
			return RefreshTimingCalibrationProfileOutput{}, err
		}
		if expected <= 0 || actual <= 0 || confidence < 0.5 {
			continue
		}
		ratio := clampCalibrationScale(float64(actual) / float64(expected))
		groups[kind+":"+strings.ToLower(strings.TrimSpace(key))] = append(groups[kind+":"+strings.ToLower(strings.TrimSpace(key))], ratio)
		groups[kind+":*"] = append(groups[kind+":*"], ratio)
		total++
	}
	if err := rows.Err(); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	parameters := TimingCalibrationParameters{
		PunctuationPauseScale: 1, ActionDurationScales: map[string]float64{}, ShotPacingScale: 1,
		DialogueObservedScale: 1, SampleCounts: map[string]int{},
	}
	for group, values := range groups {
		parameters.SampleCounts[group] = len(values)
		if len(values) < 3 {
			continue
		}
		scale := medianCalibrationScale(values)
		parts := strings.SplitN(group, ":", 2)
		kind, key := parts[0], parts[1]
		switch kind {
		case "punctuation_pause":
			if key == "*" {
				parameters.PunctuationPauseScale = scale
			}
		case "action_duration":
			if key != "*" {
				parameters.ActionDurationScales[key] = scale
			}
		case "shot_pacing":
			if key == "*" {
				parameters.ShotPacingScale = scale
			}
		case "dialogue_duration":
			if key == "*" {
				parameters.DialogueObservedScale = scale
			}
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	defer tx.Rollback(ctx)
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM timing_calibration_profiles WHERE project_id = $1`, input.ProjectID).Scan(&revision); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE timing_calibration_profiles SET status = 'archived', updated_at = now() WHERE project_id = $1 AND status = 'active'`, input.ProjectID); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	var profileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO timing_calibration_profiles(organization_id, project_id, revision, status, sample_count, parameters, audio_configuration_revision, metadata)
		VALUES ($1, $2, $3, 'active', $4, $5, $7, jsonb_build_object(
		  'workflowRunId', NULLIF($6, '')::uuid::text, 'method', 'median-clamped-v1', 'audioConfigurationRevision', $7::integer
		))
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, revision, total, mustJSON(parameters), input.WorkflowRunID, audioConfigurationRevision).Scan(&profileID); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	output := RefreshTimingCalibrationProfileOutput{ProfileID: profileID, Revision: revision, SampleCount: total, Parameters: parameters}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.timing.calibration.updated", "timing_calibration_profile", profileID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "profileId": profileID, "revision": revision, "sampleCount": total,
		"audioConfigurationRevision": audioConfigurationRevision, "parameters": parameters,
	})); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RefreshTimingCalibrationProfileOutput{}, err
	}
	return output, nil
}

func (a Activities) timingCalibrationParameters(ctx context.Context, projectID string) TimingCalibrationParameters {
	parameters := TimingCalibrationParameters{PunctuationPauseScale: 1, ActionDurationScales: map[string]float64{}, ShotPacingScale: 1, DialogueObservedScale: 1, SampleCounts: map[string]int{}}
	var raw json.RawMessage
	if err := a.db.QueryRow(ctx, `
		SELECT profile.parameters
		FROM timing_calibration_profiles profile JOIN projects project ON project.id = profile.project_id
		WHERE profile.project_id = $1 AND profile.status = 'active'
		  AND profile.audio_configuration_revision = project.audio_configuration_revision
		ORDER BY profile.revision DESC LIMIT 1
	`, projectID).Scan(&raw); err != nil {
		return parameters
	}
	if json.Unmarshal(raw, &parameters) != nil {
		return TimingCalibrationParameters{PunctuationPauseScale: 1, ActionDurationScales: map[string]float64{}, ShotPacingScale: 1, DialogueObservedScale: 1, SampleCounts: map[string]int{}}
	}
	parameters.PunctuationPauseScale = normalizedCalibrationScale(parameters.PunctuationPauseScale)
	parameters.ShotPacingScale = normalizedCalibrationScale(parameters.ShotPacingScale)
	parameters.DialogueObservedScale = normalizedCalibrationScale(parameters.DialogueObservedScale)
	if parameters.ActionDurationScales == nil {
		parameters.ActionDurationScales = map[string]float64{}
	}
	for key, value := range parameters.ActionDurationScales {
		parameters.ActionDurationScales[key] = normalizedCalibrationScale(value)
	}
	return parameters
}

func medianCalibrationScale(values []float64) float64 {
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	middle := len(copyValues) / 2
	if len(copyValues)%2 == 1 {
		return clampCalibrationScale(copyValues[middle])
	}
	return clampCalibrationScale((copyValues[middle-1] + copyValues[middle]) / 2)
}

func normalizedCalibrationScale(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 1
	}
	return clampCalibrationScale(value)
}

func clampCalibrationScale(value float64) float64 {
	if value < 0.75 {
		return 0.75
	}
	if value > 1.5 {
		return 1.5
	}
	return value
}
