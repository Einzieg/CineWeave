package storyboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

var (
	ErrStoryboardPlanNotReady       = errors.New("storyboard plan is not ready")
	ErrStoryboardPlanCoverage       = errors.New("storyboard plan coverage is invalid")
	ErrStoryboardPlanFrameAlignment = errors.New("storyboard plan timing is not frame-aligned")
)

type ActivateStoryboardPlanRequest struct {
	ProjectID        string
	ScriptEpisodeID  string
	StoryboardPlanID string
	ActorID          string
}

type ActivateStoryboardPlanResult struct {
	StoryboardPlanID         string    `json:"storyboardPlanId"`
	PreviousStoryboardPlanID string    `json:"previousStoryboardPlanId,omitempty"`
	ScriptEpisodeID          string    `json:"scriptEpisodeId"`
	ShotCount                int       `json:"shotCount"`
	TargetDurationTicks      int64     `json:"targetDurationTicks"`
	ActivatedAt              time.Time `json:"activatedAt"`
}

type StoryboardPlanValidationReport struct {
	StoryboardPlanID    string `json:"storyboardPlanId"`
	ScriptEpisodeID     string `json:"scriptEpisodeId"`
	TimingAnalysisID    string `json:"timingAnalysisId"`
	ShotCount           int    `json:"shotCount"`
	TimingUnitCount     int    `json:"timingUnitCount"`
	TimingSpanCount     int    `json:"timingSpanCount"`
	TargetDurationTicks int64  `json:"targetDurationTicks"`
	TimelineTimebase    int64  `json:"timelineTimebase"`
	FPSNumerator        int    `json:"fpsNumerator"`
	FPSDenominator      int    `json:"fpsDenominator"`
	Valid               bool   `json:"valid"`
}

type activationShot struct {
	ID               string
	StartTick        int64
	EndTick          int64
	DurationMinTicks *int64
	DurationMaxTicks *int64
}

type activationTimingUnit struct {
	ID        string
	StartTick int64
	EndTick   int64
}

type activationTimingSpan struct {
	ShotID    string
	UnitID    string
	StartTick int64
	EndTick   int64
}

// ActivateStoryboardPlanTx validates and activates one complete episode plan.
// The caller owns commit/rollback so activation can be composed with workflow state changes.
func ActivateStoryboardPlanTx(ctx context.Context, tx pgx.Tx, req ActivateStoryboardPlanRequest) (ActivateStoryboardPlanResult, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ScriptEpisodeID = strings.TrimSpace(req.ScriptEpisodeID)
	req.StoryboardPlanID = strings.TrimSpace(req.StoryboardPlanID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.ProjectID == "" || req.ScriptEpisodeID == "" || req.StoryboardPlanID == "" {
		return ActivateStoryboardPlanResult{}, fmt.Errorf("projectId, scriptEpisodeId, and storyboardPlanId are required")
	}

	var organizationID string
	if err := tx.QueryRow(ctx, `
		SELECT organization_id::text
		FROM script_episodes
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, req.ProjectID, req.ScriptEpisodeID).Scan(&organizationID); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}

	lockedPlans, err := tx.Query(ctx, `
		SELECT id
		FROM storyboard_plans
		WHERE project_id = $1 AND script_episode_id = $2
		ORDER BY id
		FOR UPDATE
	`, req.ProjectID, req.ScriptEpisodeID)
	if err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	for lockedPlans.Next() {
		var ignored string
		if err := lockedPlans.Scan(&ignored); err != nil {
			lockedPlans.Close()
			return ActivateStoryboardPlanResult{}, err
		}
	}
	if err := lockedPlans.Err(); err != nil {
		lockedPlans.Close()
		return ActivateStoryboardPlanResult{}, err
	}
	lockedPlans.Close()

	var timingAnalysisID, status string
	var active bool
	var targetDurationTicks, timelineTimebase int64
	var fpsNumerator, fpsDenominator int
	if err := tx.QueryRow(ctx, `
		SELECT plan.timing_analysis_id::text, plan.status, plan.active, plan.target_duration_ticks,
		       analysis.timeline_timebase, analysis.fps_numerator, analysis.fps_denominator
		FROM storyboard_plans plan
		JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		WHERE plan.project_id = $1
		  AND plan.script_episode_id = $2
		  AND plan.id = $3
	`, req.ProjectID, req.ScriptEpisodeID, req.StoryboardPlanID).Scan(
		&timingAnalysisID,
		&status,
		&active,
		&targetDurationTicks,
		&timelineTimebase,
		&fpsNumerator,
		&fpsDenominator,
	); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	if status != "ready" {
		return ActivateStoryboardPlanResult{}, fmt.Errorf("%w: status=%s", ErrStoryboardPlanNotReady, status)
	}
	timebase := Timebase{
		TicksPerSecond: timelineTimebase,
		FPSNumerator:   int64(fpsNumerator),
		FPSDenominator: int64(fpsDenominator),
	}
	if err := timebase.Validate(); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}

	shots, err := loadActivationShots(ctx, tx, req.StoryboardPlanID)
	if err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	units, err := loadActivationTimingUnits(ctx, tx, timingAnalysisID)
	if err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	spans, err := loadActivationTimingSpans(ctx, tx, req.StoryboardPlanID)
	if err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	if err := validateStoryboardPlanActivation(timebase, targetDurationTicks, shots, units, spans); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}

	result := ActivateStoryboardPlanResult{
		StoryboardPlanID:    req.StoryboardPlanID,
		ScriptEpisodeID:     req.ScriptEpisodeID,
		ShotCount:           len(shots),
		TargetDurationTicks: targetDurationTicks,
		ActivatedAt:         time.Now().UTC(),
	}
	if active {
		return result, nil
	}

	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((
		  SELECT id::text
		  FROM storyboard_plans
		  WHERE project_id = $1
		    AND script_episode_id = $2
		    AND active = true
		    AND id <> $3
		  LIMIT 1
		), '')
	`, req.ProjectID, req.ScriptEpisodeID, req.StoryboardPlanID).Scan(&result.PreviousStoryboardPlanID); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}

	if result.PreviousStoryboardPlanID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_plans
			SET active = false,
			    status = 'archived',
			    stale_state = 'upstream_changed',
			    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
			      'supersededByPlanId', $2::text,
			      'supersededAt', now()
			    )
			WHERE id = $1
		`, result.PreviousStoryboardPlanID, req.StoryboardPlanID); err != nil {
			return ActivateStoryboardPlanResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET stale_state = 'upstream_changed',
			    image_status = CASE
			      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
			      ELSE image_status
			    END,
			    video_status = CASE
			      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
			      ELSE video_status
			    END,
			    updated_at = now()
			WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		`, result.PreviousStoryboardPlanID); err != nil {
			return ActivateStoryboardPlanResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE timeline_clips clip
			SET stale_state = 'upstream_changed', updated_at = now()
			WHERE clip.storyboard_shot_id IN (
			  SELECT id FROM storyboard_shots WHERE storyboard_plan_id = $1
			)
		`, result.PreviousStoryboardPlanID); err != nil {
			return ActivateStoryboardPlanResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE project_timelines timeline
			SET stale_state = 'needs_regeneration', updated_at = now()
			WHERE timeline.project_id = $1
			  AND EXISTS (
			    SELECT 1
			    FROM timeline_clips clip
			    JOIN storyboard_shots shot ON shot.id = clip.storyboard_shot_id
			    WHERE clip.timeline_id = timeline.id AND shot.storyboard_plan_id = $2
			  )
		`, req.ProjectID, result.PreviousStoryboardPlanID); err != nil {
			return ActivateStoryboardPlanResult{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET active = true,
		    actual_shot_count = $2,
		    stale_state = 'fresh',
		    activated_at = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'activatedBy', NULLIF($4::text, ''),
		      'activationValidatedAt', $3::timestamptz
		    )
		WHERE id = $1 AND status = 'ready'
	`, req.StoryboardPlanID, len(shots), result.ActivatedAt, req.ActorID); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'fresh', updated_at = now()
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
	`, req.StoryboardPlanID); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, req.ProjectID, ""); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"storyboardPlanId":         req.StoryboardPlanID,
		"previousStoryboardPlanId": result.PreviousStoryboardPlanID,
		"scriptEpisodeId":          req.ScriptEpisodeID,
		"timingAnalysisId":         timingAnalysisID,
		"shotCount":                len(shots),
		"targetDurationTicks":      targetDurationTicks,
		"timelineTimebase":         timelineTimebase,
		"fpsNumerator":             fpsNumerator,
		"fpsDenominator":           fpsDenominator,
		"activatedBy":              req.ActorID,
	})
	if err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	if err := events.AppendTx(ctx, tx, organizationID, req.ProjectID, "storyboard.plan.activated", "storyboard_plan", req.StoryboardPlanID, payload); err != nil {
		return ActivateStoryboardPlanResult{}, err
	}
	return result, nil
}

func ValidateStoryboardPlanTx(ctx context.Context, tx pgx.Tx, projectID, scriptEpisodeID, storyboardPlanID string) (StoryboardPlanValidationReport, error) {
	var report StoryboardPlanValidationReport
	report.StoryboardPlanID = strings.TrimSpace(storyboardPlanID)
	report.ScriptEpisodeID = strings.TrimSpace(scriptEpisodeID)
	if strings.TrimSpace(projectID) == "" || report.ScriptEpisodeID == "" || report.StoryboardPlanID == "" {
		return report, fmt.Errorf("projectId, scriptEpisodeId, and storyboardPlanId are required")
	}
	if err := tx.QueryRow(ctx, `
		SELECT plan.timing_analysis_id::text, plan.target_duration_ticks,
		       analysis.timeline_timebase, analysis.fps_numerator, analysis.fps_denominator
		FROM storyboard_plans plan
		JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		WHERE plan.project_id = $1 AND plan.script_episode_id = $2 AND plan.id = $3
	`, projectID, report.ScriptEpisodeID, report.StoryboardPlanID).Scan(
		&report.TimingAnalysisID,
		&report.TargetDurationTicks,
		&report.TimelineTimebase,
		&report.FPSNumerator,
		&report.FPSDenominator,
	); err != nil {
		return report, err
	}
	shots, err := loadActivationShots(ctx, tx, report.StoryboardPlanID)
	if err != nil {
		return report, err
	}
	units, err := loadActivationTimingUnits(ctx, tx, report.TimingAnalysisID)
	if err != nil {
		return report, err
	}
	spans, err := loadActivationTimingSpans(ctx, tx, report.StoryboardPlanID)
	if err != nil {
		return report, err
	}
	report.ShotCount = len(shots)
	report.TimingUnitCount = len(units)
	report.TimingSpanCount = len(spans)
	err = validateStoryboardPlanActivation(Timebase{
		TicksPerSecond: report.TimelineTimebase,
		FPSNumerator:   int64(report.FPSNumerator),
		FPSDenominator: int64(report.FPSDenominator),
	}, report.TargetDurationTicks, shots, units, spans)
	if err != nil {
		return report, err
	}
	report.Valid = true
	return report, nil
}

func loadActivationShots(ctx context.Context, tx pgx.Tx, planID string) ([]activationShot, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, start_tick, end_tick, duration_min_ticks, duration_max_ticks
		FROM storyboard_shots
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		ORDER BY start_tick, end_tick, id
	`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shots := make([]activationShot, 0)
	for rows.Next() {
		var shot activationShot
		if err := rows.Scan(&shot.ID, &shot.StartTick, &shot.EndTick, &shot.DurationMinTicks, &shot.DurationMaxTicks); err != nil {
			return nil, err
		}
		shots = append(shots, shot)
	}
	return shots, rows.Err()
}

func loadActivationTimingUnits(ctx context.Context, tx pgx.Tx, analysisID string) ([]activationTimingUnit, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, start_tick, end_tick
		FROM script_timing_units
		WHERE timing_analysis_id = $1
		ORDER BY start_tick, end_tick, unit_ordinal, id
	`, analysisID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	units := make([]activationTimingUnit, 0)
	for rows.Next() {
		var unit activationTimingUnit
		if err := rows.Scan(&unit.ID, &unit.StartTick, &unit.EndTick); err != nil {
			return nil, err
		}
		units = append(units, unit)
	}
	return units, rows.Err()
}

func loadActivationTimingSpans(ctx context.Context, tx pgx.Tx, planID string) ([]activationTimingSpan, error) {
	rows, err := tx.Query(ctx, `
		SELECT storyboard_shot_id::text, timing_unit_id::text, span_start_tick, span_end_tick
		FROM storyboard_shot_timing_spans
		WHERE storyboard_plan_id = $1
		ORDER BY timing_unit_id, span_start_tick, span_end_tick, id
	`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spans := make([]activationTimingSpan, 0)
	for rows.Next() {
		var span activationTimingSpan
		if err := rows.Scan(&span.ShotID, &span.UnitID, &span.StartTick, &span.EndTick); err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	return spans, rows.Err()
}

func validateStoryboardPlanActivation(timebase Timebase, targetDurationTicks int64, shots []activationShot, units []activationTimingUnit, spans []activationTimingSpan) error {
	if err := timebase.Validate(); err != nil {
		return err
	}
	if targetDurationTicks <= 0 || !timebase.IsFrameAligned(targetDurationTicks) {
		return fmt.Errorf("%w: target duration %d", ErrStoryboardPlanFrameAlignment, targetDurationTicks)
	}
	if len(shots) == 0 || len(units) == 0 || len(spans) == 0 {
		return fmt.Errorf("%w: shots, timing units, and timing spans are required", ErrStoryboardPlanCoverage)
	}
	shotByID := make(map[string]activationShot, len(shots))
	coveredShots := make(map[string]bool, len(shots))
	expectedStart := int64(0)
	for _, shot := range shots {
		if shot.StartTick != expectedStart || shot.EndTick <= shot.StartTick {
			return fmt.Errorf("%w: shot %s starts at %d, expected %d", ErrStoryboardPlanCoverage, shot.ID, shot.StartTick, expectedStart)
		}
		if !timebase.IsFrameAligned(shot.StartTick) || !timebase.IsFrameAligned(shot.EndTick) {
			return fmt.Errorf("%w: shot %s", ErrStoryboardPlanFrameAlignment, shot.ID)
		}
		duration := shot.EndTick - shot.StartTick
		if shot.DurationMinTicks != nil && duration < *shot.DurationMinTicks {
			return fmt.Errorf("%w: shot %s is shorter than its minimum", ErrStoryboardPlanCoverage, shot.ID)
		}
		if shot.DurationMaxTicks != nil && duration > *shot.DurationMaxTicks {
			return fmt.Errorf("%w: shot %s is longer than its maximum", ErrStoryboardPlanCoverage, shot.ID)
		}
		shotByID[shot.ID] = shot
		expectedStart = shot.EndTick
	}
	if expectedStart != targetDurationTicks {
		return fmt.Errorf("%w: shots end at %d, target is %d", ErrStoryboardPlanCoverage, expectedStart, targetDurationTicks)
	}

	unitByID := make(map[string]activationTimingUnit, len(units))
	spansByUnit := make(map[string][]activationTimingSpan, len(units))
	for _, unit := range units {
		if unit.EndTick <= unit.StartTick || unit.StartTick < 0 || unit.EndTick > targetDurationTicks {
			return fmt.Errorf("%w: timing unit %s is outside plan bounds", ErrStoryboardPlanCoverage, unit.ID)
		}
		if !timebase.IsFrameAligned(unit.StartTick) || !timebase.IsFrameAligned(unit.EndTick) {
			return fmt.Errorf("%w: timing unit %s", ErrStoryboardPlanFrameAlignment, unit.ID)
		}
		unitByID[unit.ID] = unit
	}
	for _, span := range spans {
		shot, shotExists := shotByID[span.ShotID]
		unit, unitExists := unitByID[span.UnitID]
		if !shotExists || !unitExists {
			return fmt.Errorf("%w: span references an unknown shot or timing unit", ErrStoryboardPlanCoverage)
		}
		if span.StartTick < shot.StartTick || span.EndTick > shot.EndTick || span.StartTick < unit.StartTick || span.EndTick > unit.EndTick || span.EndTick <= span.StartTick {
			return fmt.Errorf("%w: span for unit %s is outside shot or unit bounds", ErrStoryboardPlanCoverage, span.UnitID)
		}
		if !timebase.IsFrameAligned(span.StartTick) || !timebase.IsFrameAligned(span.EndTick) {
			return fmt.Errorf("%w: timing span for unit %s", ErrStoryboardPlanFrameAlignment, span.UnitID)
		}
		spansByUnit[span.UnitID] = append(spansByUnit[span.UnitID], span)
		coveredShots[span.ShotID] = true
	}
	for _, unit := range units {
		unitSpans := spansByUnit[unit.ID]
		if len(unitSpans) == 0 {
			return fmt.Errorf("%w: timing unit %s has no spans", ErrStoryboardPlanCoverage, unit.ID)
		}
		sort.Slice(unitSpans, func(i, j int) bool {
			if unitSpans[i].StartTick == unitSpans[j].StartTick {
				return unitSpans[i].EndTick < unitSpans[j].EndTick
			}
			return unitSpans[i].StartTick < unitSpans[j].StartTick
		})
		expected := unit.StartTick
		for _, span := range unitSpans {
			if span.StartTick != expected {
				return fmt.Errorf("%w: timing unit %s has a gap or overlap at %d", ErrStoryboardPlanCoverage, unit.ID, expected)
			}
			expected = span.EndTick
		}
		if expected != unit.EndTick {
			return fmt.Errorf("%w: timing unit %s ends at %d, expected %d", ErrStoryboardPlanCoverage, unit.ID, expected, unit.EndTick)
		}
	}
	for _, shot := range shots {
		if !coveredShots[shot.ID] {
			return fmt.Errorf("%w: shot %s has no timing spans", ErrStoryboardPlanCoverage, shot.ID)
		}
	}
	return nil
}
