package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type ScriptTimingAnalysis struct {
	ID                       string                              `json:"id"`
	ScriptID                 string                              `json:"scriptId"`
	ScriptVersionID          string                              `json:"scriptVersionId"`
	ScriptEpisodeID          string                              `json:"scriptEpisodeId"`
	Revision                 int                                 `json:"revision"`
	Status                   string                              `json:"status"`
	EstimatedDurationTicks   int64                               `json:"estimatedDurationTicks"`
	MinimumDurationTicks     int64                               `json:"minimumDurationTicks"`
	TargetDurationTicks      *int64                              `json:"targetDurationTicks,omitempty"`
	DialogueDurationTicks    int64                               `json:"dialogueDurationTicks"`
	ActionDurationTicks      int64                               `json:"actionDurationTicks"`
	PauseDurationTicks       int64                               `json:"pauseDurationTicks"`
	EstimatedDurationFrames  int64                               `json:"estimatedDurationFrames"`
	MinimumDurationFrames    int64                               `json:"minimumDurationFrames"`
	TargetDurationFrames     *int64                              `json:"targetDurationFrames,omitempty"`
	EstimatedDurationSeconds float64                             `json:"estimatedDurationSeconds"`
	MinimumDurationSeconds   float64                             `json:"minimumDurationSeconds"`
	TargetDurationSeconds    *float64                            `json:"targetDurationSeconds,omitempty"`
	TimelineTimebase         int64                               `json:"timelineTimebase"`
	FPSNumerator             int                                 `json:"fpsNumerator"`
	FPSDenominator           int                                 `json:"fpsDenominator"`
	MethodVersion            string                              `json:"methodVersion"`
	Scenes                   []storyboardpkg.AnalyzedTimingScene `json:"scenes"`
	ProviderCallID           *string                             `json:"providerCallId,omitempty"`
	ModelID                  *string                             `json:"modelId,omitempty"`
	PromptVersionID          *string                             `json:"promptVersionId,omitempty"`
	PromptHash               *string                             `json:"promptHash,omitempty"`
	Metadata                 json.RawMessage                     `json:"metadata"`
	CreatedAt                time.Time                           `json:"createdAt"`
}

type StoryboardPlan struct {
	ID                    string                 `json:"id"`
	OrganizationID        string                 `json:"organizationId"`
	ProjectID             string                 `json:"projectId"`
	ScriptID              string                 `json:"scriptId"`
	ScriptVersionID       string                 `json:"scriptVersionId"`
	ScriptEpisodeID       string                 `json:"scriptEpisodeId"`
	TimingAnalysisID      string                 `json:"timingAnalysisId"`
	Revision              int                    `json:"revision"`
	Status                string                 `json:"status"`
	PacingProfile         json.RawMessage        `json:"pacingProfile"`
	TargetDurationTicks   int64                  `json:"targetDurationTicks"`
	TargetDurationFrames  int64                  `json:"targetDurationFrames"`
	TargetDurationSeconds float64                `json:"targetDurationSeconds"`
	EstimatedShotCount    int                    `json:"estimatedShotCount"`
	ActualShotCount       int                    `json:"actualShotCount"`
	SceneCount            int                    `json:"sceneCount"`
	CompletedSceneCount   int                    `json:"completedSceneCount"`
	FailedSceneCount      int                    `json:"failedSceneCount"`
	Active                bool                   `json:"active"`
	StaleState            string                 `json:"staleState"`
	TimelineTimebase      int64                  `json:"timelineTimebase"`
	FPSNumerator          int                    `json:"fpsNumerator"`
	FPSDenominator        int                    `json:"fpsDenominator"`
	Metadata              json.RawMessage        `json:"metadata"`
	CreatedBy             *string                `json:"createdBy,omitempty"`
	CreatedAt             time.Time              `json:"createdAt"`
	ActivatedAt           *time.Time             `json:"activatedAt,omitempty"`
	ScenePlans            []StoryboardScenePlan  `json:"scenePlans,omitempty"`
	Reviews               []StoryboardPlanReview `json:"reviews,omitempty"`
	Shots                 []StoryboardShot       `json:"shots,omitempty"`
}

type StoryboardScenePlan struct {
	ID               string          `json:"id"`
	StoryboardPlanID string          `json:"storyboardPlanId"`
	BlueprintID      string          `json:"blueprintId"`
	ScriptSceneID    *string         `json:"scriptSceneId,omitempty"`
	SceneKey         string          `json:"sceneKey"`
	SceneOrdinal     int             `json:"sceneOrdinal"`
	DependencyGroup  *string         `json:"dependencyGroup,omitempty"`
	Status           string          `json:"status"`
	RetryGeneration  int             `json:"retryGeneration"`
	StartTick        int64           `json:"startTick"`
	EndTick          int64           `json:"endTick"`
	DurationTicks    int64           `json:"durationTicks"`
	ShotCount        int             `json:"shotCount"`
	PlannerInput     json.RawMessage `json:"plannerInput"`
	PlannerOutput    json.RawMessage `json:"plannerOutput"`
	ReviewerOutput   json.RawMessage `json:"reviewerOutput"`
	EntryState       json.RawMessage `json:"entryState"`
	ExitState        json.RawMessage `json:"exitState"`
	ErrorCode        *string         `json:"errorCode,omitempty"`
	ErrorMessage     *string         `json:"errorMessage,omitempty"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	CompletedAt      *time.Time      `json:"completedAt,omitempty"`
}

type StoryboardPlanReview struct {
	ID                  string          `json:"id"`
	StoryboardPlanID    string          `json:"storyboardPlanId"`
	Revision            int             `json:"revision"`
	Status              string          `json:"status"`
	Approved            bool            `json:"approved"`
	Issues              json.RawMessage `json:"issues"`
	Corrections         json.RawMessage `json:"corrections"`
	DeterministicReport json.RawMessage `json:"deterministicReport"`
	ProviderCallID      *string         `json:"providerCallId,omitempty"`
	ModelID             *string         `json:"modelId,omitempty"`
	ErrorCode           *string         `json:"errorCode,omitempty"`
	ErrorMessage        *string         `json:"errorMessage,omitempty"`
	Metadata            json.RawMessage `json:"metadata"`
	CreatedAt           time.Time       `json:"createdAt"`
	CompletedAt         *time.Time      `json:"completedAt,omitempty"`
}

func (s *Server) analyzeScriptEpisodeTiming(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	episode, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		TargetDurationSeconds *float64 `json:"targetDurationSeconds,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.TargetDurationSeconds != nil && *req.TargetDurationSeconds <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetDurationSeconds must be positive", nil, false)
		return
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, "script_episode_timing", map[string]any{
		"scriptId":              episode.ScriptID,
		"scriptVersionId":       episode.ScriptVersionID,
		"scriptEpisodeId":       episode.ID,
		"targetDurationSeconds": req.TargetDurationSeconds,
	}, workflows.AnalyzeScriptEpisodeTimingWorkflow)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) getScriptEpisodeTiming(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	episode, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.latestScriptTimingAnalysis(r.Context(), project.ID, episode.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) latestScriptTimingAnalysis(ctx context.Context, projectID, episodeID string) (ScriptTimingAnalysis, error) {
	var item ScriptTimingAnalysis
	var targetTicks sql.NullInt64
	var activityOutput json.RawMessage
	err := s.db.QueryRow(ctx, `
		SELECT analysis.id::text, analysis.script_id::text, analysis.script_version_id::text,
		       analysis.script_episode_id::text, analysis.revision, analysis.status,
		       analysis.estimated_duration_ticks, analysis.minimum_duration_ticks,
		       analysis.target_duration_ticks, analysis.timeline_timebase,
		       analysis.fps_numerator, analysis.fps_denominator, analysis.method_version,
		       analysis.provider_call_id::text, analysis.model_id::text,
		       analysis.prompt_version_id::text, analysis.prompt_hash,
		       analysis.metadata, COALESCE(analysis.metadata->'activityOutput', '{}'::jsonb),
		       analysis.created_at
		FROM script_timing_analyses analysis
		WHERE analysis.project_id = $1 AND analysis.script_episode_id = $2
		ORDER BY (analysis.status = 'ready') DESC, analysis.revision DESC
		LIMIT 1
	`, projectID, episodeID).Scan(
		&item.ID, &item.ScriptID, &item.ScriptVersionID, &item.ScriptEpisodeID,
		&item.Revision, &item.Status, &item.EstimatedDurationTicks, &item.MinimumDurationTicks,
		&targetTicks, &item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&item.MethodVersion, &item.ProviderCallID, &item.ModelID, &item.PromptVersionID,
		&item.PromptHash, &item.Metadata, &activityOutput, &item.CreatedAt,
	)
	if err != nil {
		return ScriptTimingAnalysis{}, err
	}
	if targetTicks.Valid {
		value := targetTicks.Int64
		item.TargetDurationTicks = &value
	}
	var stored workflows.TimingAnalysisActivityOutput
	if len(activityOutput) > 0 && string(activityOutput) != "{}" {
		if err := json.Unmarshal(activityOutput, &stored); err != nil {
			return ScriptTimingAnalysis{}, err
		}
		item.Scenes = stored.Scenes
	}
	item.populateDurations()
	return item, nil
}

func (item *ScriptTimingAnalysis) populateDurations() {
	if item == nil || item.TimelineTimebase <= 0 || item.FPSNumerator <= 0 || item.FPSDenominator <= 0 {
		return
	}
	frameTicks := item.TimelineTimebase * int64(item.FPSDenominator) / int64(item.FPSNumerator)
	item.EstimatedDurationFrames = item.EstimatedDurationTicks / frameTicks
	item.MinimumDurationFrames = item.MinimumDurationTicks / frameTicks
	item.EstimatedDurationSeconds = float64(item.EstimatedDurationTicks) / float64(item.TimelineTimebase)
	item.MinimumDurationSeconds = float64(item.MinimumDurationTicks) / float64(item.TimelineTimebase)
	if item.TargetDurationTicks != nil {
		frames := *item.TargetDurationTicks / frameTicks
		seconds := float64(*item.TargetDurationTicks) / float64(item.TimelineTimebase)
		item.TargetDurationFrames = &frames
		item.TargetDurationSeconds = &seconds
	}
	for _, scene := range item.Scenes {
		for _, unit := range scene.Units {
			switch unit.Type {
			case storyboardpkg.UnitDialogue, storyboardpkg.UnitVoiceover, storyboardpkg.UnitNarration, storyboardpkg.UnitSystem:
				item.DialogueDurationTicks += unit.DurationTicks
			case storyboardpkg.UnitPause, storyboardpkg.UnitAmbientHold, storyboardpkg.UnitTransition:
				item.PauseDurationTicks += unit.DurationTicks
			default:
				item.ActionDurationTicks += unit.DurationTicks
			}
		}
	}
}

func (s *Server) listStoryboardPlans(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	episode, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), storyboardPlanSelectSQL(`
		WHERE plan.project_id = $1 AND plan.script_episode_id = $2
		ORDER BY plan.revision DESC
	`), project.ID, episode.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]StoryboardPlan, 0)
	for rows.Next() {
		item, err := scanStoryboardPlan(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getStoryboardPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.storyboardPlanDetail(r.Context(), project.ID, r.PathValue("planId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) activateStoryboardPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	planID := strings.TrimSpace(r.PathValue("planId"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := s.activateStoryboardPlanActionTx(r.Context(), tx, project, principal.UserID, planID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.storyboardPlanDetail(r.Context(), project.ID, planID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) splitStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req storyboardSplitShotActionInput
	if !decode(w, r, &req) {
		return
	}
	req.ShotID = r.PathValue("shotId")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := s.splitStoryboardShotActionTx(r.Context(), tx, project, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeStoryboardPlanEditResponse(w, r, project.ID, result)
}

func (s *Server) mergeStoryboardShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req storyboardMergeShotsActionInput
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := s.mergeStoryboardShotsActionTx(r.Context(), tx, project, principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeStoryboardPlanEditResponse(w, r, project.ID, result)
}

func (s *Server) updateStoryboardShotTiming(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req struct {
		StartTick      *int64 `json:"startTick,omitempty"`
		EndTick        *int64 `json:"endTick,omitempty"`
		DurationFrames *int64 `json:"durationFrames,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.StartTick == nil && req.EndTick == nil && req.DurationFrames == nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "a timing boundary or durationFrames is required", nil, false)
		return
	}
	if req.DurationFrames != nil {
		if *req.DurationFrames <= 0 || req.EndTick != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "durationFrames must be positive and cannot be combined with endTick", nil, false)
			return
		}
		startTick, frameTick, err := s.storyboardShotStartAndFrameTick(r.Context(), project.ID, r.PathValue("shotId"))
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if req.StartTick != nil {
			startTick = *req.StartTick
		}
		endTick := startTick + *req.DurationFrames*frameTick
		req.EndTick = &endTick
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := storyboardpkg.RetimeStoryboardShotTx(r.Context(), tx, storyboardpkg.RetimeStoryboardShotRequest{
		ProjectID: project.ID, ShotID: r.PathValue("shotId"), StartTick: req.StartTick,
		EndTick: req.EndTick, ActorID: principal.UserID,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "STORYBOARD_EDIT_INVALID", err.Error(), nil, false)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeStoryboardPlanEditResponse(w, r, project.ID, result)
}

func (s *Server) writeStoryboardPlanEditResponse(w http.ResponseWriter, r *http.Request, projectID string, result storyboardpkg.StoryboardPlanEditResult) {
	plan, err := s.storyboardPlanDetail(r.Context(), projectID, result.StoryboardPlanID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{"edit": result, "plan": plan}, nil)
}

func (s *Server) storyboardShotFrameTick(ctx context.Context, projectID, shotID string) (int64, error) {
	_, frameTick, err := s.storyboardShotStartAndFrameTick(ctx, projectID, shotID)
	return frameTick, err
}

func (s *Server) storyboardShotStartAndFrameTick(ctx context.Context, projectID, shotID string) (int64, int64, error) {
	return s.storyboardShotStartAndFrameTickWithDB(ctx, s.db, projectID, shotID)
}

func (s *Server) storyboardShotStartAndFrameTickWithDB(ctx context.Context, db snapshotQuerier, projectID, shotID string) (int64, int64, error) {
	var startTick, timebase int64
	var fpsNumerator, fpsDenominator int
	if err := db.QueryRow(ctx, `
		SELECT shot.start_tick, analysis.timeline_timebase, analysis.fps_numerator, analysis.fps_denominator
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		WHERE shot.project_id = $1 AND shot.id = $2 AND shot.deleted_at IS NULL
	`, projectID, shotID).Scan(&startTick, &timebase, &fpsNumerator, &fpsDenominator); err != nil {
		return 0, 0, err
	}
	frameTick, err := storyboardPlanFrameTick(timebase, fpsNumerator, fpsDenominator)
	return startTick, frameTick, err
}

func (s *Server) storyboardPlanDetail(ctx context.Context, projectID, planID string) (StoryboardPlan, error) {
	item, err := scanStoryboardPlan(s.db.QueryRow(ctx, storyboardPlanSelectSQL(`WHERE plan.project_id = $1 AND plan.id = $2`), projectID, planID))
	if err != nil {
		return StoryboardPlan{}, err
	}
	sceneRows, err := s.db.Query(ctx, `
		SELECT id::text, storyboard_plan_id::text, blueprint_id::text, script_scene_id::text,
		       scene_key, scene_ordinal, dependency_group, status, retry_generation,
		       start_tick, end_tick, shot_count, planner_input, planner_output, reviewer_output,
		       entry_state, exit_state, error_code, error_message, metadata,
		       created_at, updated_at, completed_at
		FROM storyboard_scene_plans
		WHERE storyboard_plan_id = $1
		ORDER BY scene_ordinal
	`, planID)
	if err != nil {
		return StoryboardPlan{}, err
	}
	for sceneRows.Next() {
		var scene StoryboardScenePlan
		if err := sceneRows.Scan(
			&scene.ID, &scene.StoryboardPlanID, &scene.BlueprintID, &scene.ScriptSceneID,
			&scene.SceneKey, &scene.SceneOrdinal, &scene.DependencyGroup, &scene.Status,
			&scene.RetryGeneration, &scene.StartTick, &scene.EndTick, &scene.ShotCount,
			&scene.PlannerInput, &scene.PlannerOutput, &scene.ReviewerOutput,
			&scene.EntryState, &scene.ExitState, &scene.ErrorCode, &scene.ErrorMessage,
			&scene.Metadata, &scene.CreatedAt, &scene.UpdatedAt, &scene.CompletedAt,
		); err != nil {
			sceneRows.Close()
			return StoryboardPlan{}, err
		}
		scene.DurationTicks = scene.EndTick - scene.StartTick
		item.ScenePlans = append(item.ScenePlans, scene)
	}
	if err := sceneRows.Err(); err != nil {
		sceneRows.Close()
		return StoryboardPlan{}, err
	}
	sceneRows.Close()

	reviewRows, err := s.db.Query(ctx, `
		SELECT id::text, storyboard_plan_id::text, revision, status, approved, issues,
		       corrections, deterministic_report, provider_call_id::text, model_id::text,
		       error_code, error_message, metadata, created_at, completed_at
		FROM storyboard_plan_reviews
		WHERE storyboard_plan_id = $1
		ORDER BY revision DESC
	`, planID)
	if err != nil {
		return StoryboardPlan{}, err
	}
	for reviewRows.Next() {
		var review StoryboardPlanReview
		if err := reviewRows.Scan(
			&review.ID, &review.StoryboardPlanID, &review.Revision, &review.Status,
			&review.Approved, &review.Issues, &review.Corrections, &review.DeterministicReport,
			&review.ProviderCallID, &review.ModelID, &review.ErrorCode, &review.ErrorMessage,
			&review.Metadata, &review.CreatedAt, &review.CompletedAt,
		); err != nil {
			reviewRows.Close()
			return StoryboardPlan{}, err
		}
		item.Reviews = append(item.Reviews, review)
	}
	if err := reviewRows.Err(); err != nil {
		reviewRows.Close()
		return StoryboardPlan{}, err
	}
	reviewRows.Close()

	shotRows, err := s.db.Query(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.storyboard_plan_id = $2 AND s.deleted_at IS NULL
		ORDER BY s.start_tick, s.end_tick, s.shot_index, s.id
	`), projectID, planID)
	if err != nil {
		return StoryboardPlan{}, err
	}
	for shotRows.Next() {
		shot, err := scanStoryboardShot(shotRows)
		if err != nil {
			shotRows.Close()
			return StoryboardPlan{}, err
		}
		publicIndex := len(item.Shots)
		shot.ShotIndex = publicIndex
		shot.ShotNo = publicIndex + 1
		shot.EpisodeShotIndex = &publicIndex
		item.Shots = append(item.Shots, shot)
	}
	if err := shotRows.Err(); err != nil {
		shotRows.Close()
		return StoryboardPlan{}, err
	}
	shotRows.Close()
	return item, nil
}

func storyboardPlanSelectSQL(where string) string {
	return `
		SELECT plan.id::text, plan.organization_id::text, plan.project_id::text,
		       plan.script_id::text, plan.script_version_id::text, plan.script_episode_id::text,
		       plan.timing_analysis_id::text, plan.revision, plan.status, plan.pacing_profile,
		       plan.target_duration_ticks, plan.estimated_shot_count, plan.actual_shot_count,
		       (SELECT COUNT(*) FROM storyboard_scene_plans scene WHERE scene.storyboard_plan_id = plan.id),
		       (SELECT COUNT(*) FROM storyboard_scene_plans scene WHERE scene.storyboard_plan_id = plan.id AND scene.status = 'ready'),
		       (SELECT COUNT(*) FROM storyboard_scene_plans scene WHERE scene.storyboard_plan_id = plan.id AND scene.status = 'failed'),
		       plan.active, plan.stale_state, analysis.timeline_timebase,
		       analysis.fps_numerator, analysis.fps_denominator, plan.metadata,
		       plan.created_by::text, plan.created_at, plan.activated_at
		FROM storyboard_plans plan
		JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		` + where
}

func scanStoryboardPlan(row rowScan) (StoryboardPlan, error) {
	var item StoryboardPlan
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ScriptID,
		&item.ScriptVersionID, &item.ScriptEpisodeID, &item.TimingAnalysisID,
		&item.Revision, &item.Status, &item.PacingProfile, &item.TargetDurationTicks,
		&item.EstimatedShotCount, &item.ActualShotCount, &item.SceneCount,
		&item.CompletedSceneCount, &item.FailedSceneCount, &item.Active, &item.StaleState,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator, &item.Metadata,
		&item.CreatedBy, &item.CreatedAt, &item.ActivatedAt,
	)
	if err != nil {
		return StoryboardPlan{}, err
	}
	if item.TimelineTimebase > 0 && item.FPSNumerator > 0 && item.FPSDenominator > 0 {
		frameTicks := item.TimelineTimebase * int64(item.FPSDenominator) / int64(item.FPSNumerator)
		item.TargetDurationFrames = item.TargetDurationTicks / frameTicks
		item.TargetDurationSeconds = float64(item.TargetDurationTicks) / float64(item.TimelineTimebase)
	}
	return item, nil
}

func storyboardPlanFrameTick(timebase int64, fpsNumerator, fpsDenominator int) (int64, error) {
	if timebase <= 0 || fpsNumerator <= 0 || fpsDenominator <= 0 {
		return 0, fmt.Errorf("invalid storyboard timebase")
	}
	value := timebase * int64(fpsDenominator)
	if value%int64(fpsNumerator) != 0 {
		return 0, fmt.Errorf("frame rate does not divide the timeline timebase exactly")
	}
	return value / int64(fpsNumerator), nil
}
