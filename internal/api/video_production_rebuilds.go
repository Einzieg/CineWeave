package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type projectVideoProductionRebuild struct {
	ID                         string          `json:"id"`
	OrganizationID             string          `json:"organizationId"`
	ProjectID                  string          `json:"projectId"`
	SourceBindingID            string          `json:"sourceBindingId"`
	SourceGenerationID         string          `json:"sourceGenerationId"`
	SourceVideoProductionState string          `json:"sourceVideoProductionState"`
	TargetProfileVersionID     string          `json:"targetProfileVersionId"`
	TargetBindingID            *string         `json:"targetBindingId,omitempty"`
	TargetGenerationID         *string         `json:"targetGenerationId,omitempty"`
	Status                     string          `json:"status"`
	Reason                     string          `json:"reason"`
	TargetConfiguration        json.RawMessage `json:"targetConfiguration"`
	TargetConfigurationHash    string          `json:"targetConfigurationHash"`
	ImpactSnapshot             json.RawMessage `json:"impactSnapshot"`
	ImpactToken                string          `json:"impactToken"`
	ExpectedProjectRevision    int64           `json:"expectedProjectRevision"`
	EpisodeCount               int             `json:"episodeCount"`
	RetainedAssetCount         int             `json:"retainedAssetCount"`
	WorkflowRunID              *string         `json:"workflowRunId,omitempty"`
	IdempotencyKey             string          `json:"idempotencyKey"`
	RequestedBy                string          `json:"requestedBy"`
	RequestedAt                time.Time       `json:"requestedAt"`
	ApprovedAt                 *time.Time      `json:"approvedAt,omitempty"`
	StartedAt                  *time.Time      `json:"startedAt,omitempty"`
	CompletedAt                *time.Time      `json:"completedAt,omitempty"`
	FailureCode                *string         `json:"failureCode,omitempty"`
	FailureMessage             *string         `json:"failureMessage,omitempty"`
}

type projectVideoProductionRebuildTargetRequest struct {
	TargetProfileKey     string                              `json:"targetProfileKey"`
	TargetProfileVersion *int                                `json:"targetProfileVersion"`
	TargetConfiguration  videoProductionConfigurationRequest `json:"targetConfiguration"`
}

type createProjectVideoProductionRebuildRequest struct {
	projectVideoProductionRebuildTargetRequest
	ExpectedProjectRevision int64  `json:"expectedProjectRevision"`
	ImpactToken             string `json:"impactToken"`
}

type projectVideoProductionRebuildActionResult struct {
	Rebuild          projectVideoProductionRebuild `json:"rebuild"`
	WorkflowRunID    string                        `json:"workflowRunId"`
	IdempotentReplay bool                          `json:"idempotentReplay"`
}

type projectVideoProductionRebuildItem struct {
	ID                       string          `json:"id"`
	RebuildID                string          `json:"rebuildId"`
	ProjectID                string          `json:"projectId"`
	ScriptEpisodeID          string          `json:"scriptEpisodeId"`
	EpisodeOrdinal           int             `json:"episodeOrdinal"`
	ScriptEpisodeRevision    int64           `json:"scriptEpisodeRevision"`
	ScriptEpisodeContentHash string          `json:"scriptEpisodeContentHash"`
	SourceStoryboardPlanID   *string         `json:"sourceStoryboardPlanId,omitempty"`
	TargetStoryboardPlanID   *string         `json:"targetStoryboardPlanId,omitempty"`
	WorkflowRunID            *string         `json:"workflowRunId,omitempty"`
	Status                   string          `json:"status"`
	Checkpoint               json.RawMessage `json:"checkpoint"`
	AttemptCount             int             `json:"attemptCount"`
	StartedAt                *time.Time      `json:"startedAt,omitempty"`
	CompletedAt              *time.Time      `json:"completedAt,omitempty"`
	FailureCode              *string         `json:"failureCode,omitempty"`
	FailureMessage           *string         `json:"failureMessage,omitempty"`
}

func (s *Server) getProjectVideoProductionRebuildImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: project.ID}) {
		return
	}
	var req projectVideoProductionRebuildTargetRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := s.projectVideoProductionRebuildImpactAction(r.Context(), s.db, project, req)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) createProjectVideoProductionRebuild(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectVideoProductionRebuild, authz.Resource{ProjectID: project.ID}) {
		return
	}
	var req createProjectVideoProductionRebuildRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := s.createProjectVideoProductionRebuildCore(
		r.Context(), principal, project, req, strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result.Rebuild, map[string]any{"operationId": result.WorkflowRunID})
}

func (s *Server) createProjectVideoProductionRebuildCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	req createProjectVideoProductionRebuildRequest,
	idempotencyKey string,
) (projectVideoProductionRebuildActionResult, error) {
	r := requestWithContext(ctx)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	req.TargetProfileKey = strings.TrimSpace(req.TargetProfileKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 || req.ExpectedProjectRevision <= 0 || strings.TrimSpace(req.TargetProfileKey) == "" || strings.TrimSpace(req.ImpactToken) == "" {
		return projectVideoProductionRebuildActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "重建请求缺少有效的 Idempotency-Key、项目 revision、目标方案或影响令牌")
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	defer tx.Rollback(r.Context())
	if existing, found, err := findVideoProductionRebuildByIdempotencyKey(r.Context(), tx, project.ID, idempotencyKey); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	} else if found {
		if existing.ExpectedProjectRevision != req.ExpectedProjectRevision || existing.ImpactToken != req.ImpactToken {
			return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildConflict, "Idempotency-Key 已用于不同的重建请求", false)
		}
		if err := tx.Commit(r.Context()); err != nil {
			return projectVideoProductionRebuildActionResult{}, err
		}
		return projectVideoProductionRebuildActionResult{
			Rebuild: existing, WorkflowRunID: stringValue(existing.WorkflowRunID), IdempotentReplay: true,
		}, nil
	}
	var currentRevision int64
	var locked bool
	var sourceVideoProductionState string
	var activeRebuildID sql.NullString
	if err := tx.QueryRow(r.Context(), `
		SELECT revision, video_production_locked, video_production_state,
		       active_video_production_rebuild_id::text
		FROM projects
		WHERE id = $1
		FOR UPDATE
	`, project.ID).Scan(&currentRevision, &locked, &sourceVideoProductionState, &activeRebuildID); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if locked || activeRebuildID.Valid {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildConflict, "项目已有视频生产方案重建正在执行", true)
	}
	if currentRevision != req.ExpectedProjectRevision {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildImpactStale, "项目已变化，请重新确认重建影响", false)
	}
	target, err := videoproduction.ResolveProfileVersion(r.Context(), tx, req.TargetProfileKey, req.TargetProfileVersion, true)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	targetConfiguration, err := s.resolveTargetProductionConfiguration(r.Context(), tx, project.OrganizationID, project.ID, req.TargetConfiguration)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	impact, err := videoproduction.BuildRebuildImpact(r.Context(), tx, project.ID, target, targetConfiguration)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if err := videoproduction.VerifyRebuildImpact(impact, req.ExpectedProjectRevision, req.ImpactToken); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	compatibility, err := s.loadVideoProductionCompatibility(r.Context(), tx, projectWithProductionConfiguration(project, targetConfiguration), target)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if !compatibility.Compatible {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeProfileIncompatible, "当前业务视频模型与目标生产方案不兼容", false)
	}
	rebuildID := uuid.NewString()
	impactSnapshot, err := json.Marshal(impact)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	targetConfigurationJSON, err := json.Marshal(impact.TargetConfiguration)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	var sourceCommerceBindingID *string
	var sourceCommerceConfigurationHash *string
	if project.ProjectKind.IsCommerce() {
		var bindingID string
		var configurationHash string
		if err := tx.QueryRow(r.Context(), `
			SELECT generation.commerce_workflow_binding_id::text, binding.configuration_hash
			FROM project_video_production_generations generation
			JOIN project_commerce_workflow_bindings binding
			  ON binding.id = generation.commerce_workflow_binding_id
			 AND binding.project_id = generation.project_id
			 AND binding.organization_id = generation.organization_id
			WHERE generation.id = $1
			  AND generation.project_id = $2
			  AND generation.organization_id = $3
		`, impact.SourceGenerationID, project.ID, project.OrganizationID).Scan(
			&bindingID,
			&configurationHash,
		); err != nil {
			return projectVideoProductionRebuildActionResult{}, err
		}
		sourceCommerceBindingID = &bindingID
		sourceCommerceConfigurationHash = &configurationHash
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO project_video_production_rebuilds(
			id, organization_id, project_id, source_binding_id, source_generation_id,
			source_video_production_state,
			source_commerce_workflow_binding_id, source_commerce_configuration_hash,
			target_profile_version_id, status, reason, target_configuration, target_configuration_hash,
			impact_snapshot, impact_token,
			expected_project_revision, episode_count, retained_asset_count,
			idempotency_key, requested_by, approved_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			'approved', $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, now()
		)
	`, rebuildID, project.OrganizationID, project.ID, impact.SourceBindingID, impact.SourceGenerationID,
		sourceVideoProductionState, sourceCommerceBindingID, sourceCommerceConfigurationHash,
		target.ID, impact.Reason, targetConfigurationJSON, impact.TargetConfigurationHash,
		impactSnapshot, impact.ImpactToken, impact.ExpectedProjectRevision,
		len(impact.Episodes), impact.Counts.RetainedAssets, idempotencyKey, principal.UserID); err != nil {
		return projectVideoProductionRebuildActionResult{}, normalizeVideoProductionRebuildDatabaseError(err)
	}
	for _, episode := range impact.Episodes {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO project_video_production_rebuild_items(
				rebuild_id, project_id, script_episode_id, episode_ordinal,
				script_episode_revision, script_episode_content_hash, source_storyboard_plan_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, rebuildID, project.ID, episode.ScriptEpisodeID, episode.EpisodeOrdinal,
			episode.ScriptEpisodeRevision, episode.ScriptEpisodeHash, episode.SourceStoryboardPlanID); err != nil {
			return projectVideoProductionRebuildActionResult{}, err
		}
	}
	command, err := tx.Exec(r.Context(), `
		UPDATE projects
		SET video_production_locked = true,
		    video_production_state = 'rebuilding',
		    active_video_production_rebuild_id = $3,
		    updated_at = now()
		WHERE id = $1 AND revision = $2 AND video_production_locked = false
		  AND active_video_production_rebuild_id IS NULL
	`, project.ID, req.ExpectedProjectRevision, rebuildID)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if command.RowsAffected() != 1 {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildImpactStale, "项目已变化，请重新确认重建影响", false)
	}
	workflowInput := workflows.ProjectVideoProductionRebuildInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		RebuildID:      rebuildID,
		RequestedBy:    principal.UserID,
	}
	runInput := mustRawJSON(map[string]any{"rebuildId": rebuildID, "targetProfileKey": target.ProfileKey, "impactToken": impact.ImpactToken})
	run, err := s.enqueueProjectWorkflowTx(r.Context(), tx, principal, project, "project_video_production_rebuild", runInput,
		workflows.ScriptTaskQueue, workflows.ProjectVideoProductionRebuildWorkflow,
		func(run WorkflowRun) any {
			workflowInput.WorkflowRunID = run.ID
			return workflowInput
		}, nil)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_video_production_rebuilds SET workflow_run_id = $2 WHERE id = $1
	`, rebuildID, run.ID); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO project_video_production_rebuild_attempts(
			rebuild_id, project_id, attempt_no, workflow_run_id, idempotency_key,
			retry_failed_only, status, created_by
		) VALUES ($1, $3, 1, $2, $4, false, 'queued', $5)
	`, rebuildID, run.ID, project.ID, idempotencyKey, principal.UserID); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if err := events.AppendTx(r.Context(), tx, project.OrganizationID, project.ID,
		"video.production.rebuild.requested", "video_production_rebuild", rebuildID,
		mustRawJSON(map[string]any{
			"bindingId":               impact.SourceBindingID,
			"bindingRevision":         impact.SourceBindingRevision,
			"productionGenerationId":  impact.SourceGenerationID,
			"rebuildId":               rebuildID,
			"workflowRunId":           run.ID,
			"targetProfileKey":        target.ProfileKey,
			"targetProfileVersion":    target.Version,
			"reason":                  impact.Reason,
			"targetConfigurationHash": impact.TargetConfigurationHash,
			"episodeCount":            len(impact.Episodes),
		}),
	); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	item, err := s.projectVideoProductionRebuild(r.Context(), project.ID, rebuildID)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	return projectVideoProductionRebuildActionResult{Rebuild: item, WorkflowRunID: run.ID}, nil
}

func (s *Server) getProjectVideoProductionRebuild(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: projectID}) {
		return
	}
	item, err := s.projectVideoProductionRebuild(r.Context(), projectID, r.PathValue("rebuildId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) getCurrentProjectVideoProductionRebuild(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: projectID}) {
		return
	}
	item, err := scanProjectVideoProductionRebuild(s.db.QueryRow(r.Context(), projectVideoProductionRebuildSelectSQL+`
		WHERE project_id = $1
		  AND id = (
		    SELECT active_video_production_rebuild_id
		    FROM projects
		    WHERE id = $1
		  )
	`, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, r, http.StatusOK, nil, nil)
		return
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listProjectVideoProductionRebuildItems(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: projectID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), projectVideoProductionRebuildItemSelectSQL+`
		WHERE rebuild_id = $1 AND project_id = $2 ORDER BY episode_ordinal, id
	`, r.PathValue("rebuildId"), projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]projectVideoProductionRebuildItem, 0)
	for rows.Next() {
		item, err := scanProjectVideoProductionRebuildItem(rows)
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

func (s *Server) retryFailedProjectVideoProductionRebuildItems(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectVideoProductionRebuild, authz.Resource{ProjectID: project.ID}) {
		return
	}
	result, err := s.retryProjectVideoProductionRebuildCore(
		r.Context(), principal, project, r.PathValue("rebuildId"), strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result.Rebuild, map[string]any{"operationId": result.WorkflowRunID})
}

func (s *Server) retryProjectVideoProductionRebuildCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	rebuildID string,
	idempotencyKey string,
) (projectVideoProductionRebuildActionResult, error) {
	r := requestWithContext(ctx)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	rebuildID = strings.TrimSpace(rebuildID)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return projectVideoProductionRebuildActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Idempotency-Key 不能为空且不能超过 200 个字符")
	}
	if rebuildID == "" {
		return projectVideoProductionRebuildActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "rebuildId 不能为空")
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	defer tx.Rollback(r.Context())
	var existingRunID string
	err = tx.QueryRow(r.Context(), `
		SELECT workflow_run_id::text
		FROM project_video_production_rebuild_attempts
		WHERE rebuild_id = $1 AND project_id = $2 AND idempotency_key = $3
	`, rebuildID, project.ID, idempotencyKey).Scan(&existingRunID)
	if err == nil {
		if err := tx.Commit(r.Context()); err != nil {
			return projectVideoProductionRebuildActionResult{}, err
		}
		item, loadErr := s.projectVideoProductionRebuild(r.Context(), project.ID, rebuildID)
		if loadErr != nil {
			return projectVideoProductionRebuildActionResult{}, loadErr
		}
		return projectVideoProductionRebuildActionResult{
			Rebuild: item, WorkflowRunID: existingRunID, IdempotentReplay: true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return projectVideoProductionRebuildActionResult{}, err
	}
	var status string
	var targetGenerationID, targetBindingID sql.NullString
	var activeGenerationID, activeRebuildID sql.NullString
	var targetGenerationStatus, targetGenerationBindingID, targetBindingStatus sql.NullString
	var failedItems, staleItems, attemptNo int
	if err := tx.QueryRow(r.Context(), `
		SELECT rebuild.status, rebuild.target_generation_id::text, rebuild.target_binding_id::text,
		       project.active_video_production_generation_id::text,
		       project.active_video_production_rebuild_id::text,
		       target_generation.status, target_generation.binding_id::text, target_binding.status,
		       (SELECT count(*) FROM project_video_production_rebuild_items item WHERE item.rebuild_id = rebuild.id AND item.status = 'failed'),
		       (
		         SELECT count(*)
		         FROM project_video_production_rebuild_items item
		         JOIN script_episodes episode ON episode.id = item.script_episode_id
		         WHERE item.rebuild_id = rebuild.id
		           AND (episode.revision <> item.script_episode_revision OR episode.content_hash <> item.script_episode_content_hash)
		       ),
		       COALESCE((SELECT max(attempt_no) FROM project_video_production_rebuild_attempts attempt WHERE attempt.rebuild_id = rebuild.id), 0) + 1
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		LEFT JOIN project_video_production_generations target_generation
		  ON target_generation.id = rebuild.target_generation_id
		 AND target_generation.project_id = rebuild.project_id
		LEFT JOIN project_video_production_bindings target_binding
		  ON target_binding.id = rebuild.target_binding_id
		 AND target_binding.project_id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $2
		FOR UPDATE OF rebuild, project
	`, rebuildID, project.ID).Scan(
		&status, &targetGenerationID, &targetBindingID,
		&activeGenerationID, &activeRebuildID,
		&targetGenerationStatus, &targetGenerationBindingID, &targetBindingStatus,
		&failedItems, &staleItems, &attemptNo,
	); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	validTargetIdentity := targetGenerationID.Valid && targetBindingID.Valid &&
		activeGenerationID.Valid && activeGenerationID.String == targetGenerationID.String &&
		targetGenerationStatus.String == "active" &&
		targetGenerationBindingID.String == targetBindingID.String &&
		targetBindingStatus.String == "active"
	validOwner := activeRebuildID.Valid && activeRebuildID.String == rebuildID
	if staleItems > 0 {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(
			videoproduction.CodeRebuildImpactStale,
			"重建分集内容已变化，请重新确认视频生产配置影响",
			false,
		)
	}
	if !validTargetIdentity || !validOwner || failedItems == 0 || (status != "partial_succeeded" && status != "storyboard_required" && status != "failed") {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildConflict, "当前重建没有可重试的失败分集", false)
	}
	command, err := tx.Exec(r.Context(), `
		UPDATE projects
		SET video_production_locked = true,
		    video_production_state = 'rebuilding',
		    active_video_production_rebuild_id = $2,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $3
		  AND active_video_production_rebuild_id = $2
	`, project.ID, rebuildID, targetGenerationID.String)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if command.RowsAffected() != 1 {
		return projectVideoProductionRebuildActionResult{}, videoproduction.NewError(videoproduction.CodeRebuildConflict, "当前重建已被新的生产代替代", false)
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_video_production_rebuilds
		SET status = 'approved', completed_at = NULL, failure_code = NULL, failure_message = NULL
		WHERE id = $1 AND project_id = $2 AND target_generation_id = $3
	`, rebuildID, project.ID, targetGenerationID.String); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	workflowInput := workflows.ProjectVideoProductionRebuildInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		RebuildID:      rebuildID,
		RequestedBy:    principal.UserID,
		RetryFailed:    true,
	}
	run, err := s.enqueueProjectWorkflowTx(r.Context(), tx, principal, project, "project_video_production_rebuild", mustRawJSON(map[string]any{
		"rebuildId": rebuildID, "retryFailed": true, "attempt": attemptNo,
	}), workflows.ScriptTaskQueue, workflows.ProjectVideoProductionRebuildWorkflow, func(run WorkflowRun) any {
		workflowInput.WorkflowRunID = run.ID
		return workflowInput
	}, nil)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_video_production_rebuilds
		SET workflow_run_id = $2
		WHERE id = $1 AND project_id = $3 AND status = 'approved'
	`, rebuildID, run.ID, project.ID); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO project_video_production_rebuild_attempts(
			rebuild_id, project_id, attempt_no, workflow_run_id, idempotency_key,
			retry_failed_only, status, created_by
		) VALUES ($1, $3, $4, $2, $5, true, 'queued', $6)
	`, rebuildID, run.ID, project.ID, attemptNo, idempotencyKey, principal.UserID); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	activeContext, err := videoproduction.LoadActiveContext(r.Context(), tx, project.ID)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if err := events.AppendTx(r.Context(), tx, project.OrganizationID, project.ID,
		"video.production.rebuild.requested", "video_production_rebuild", rebuildID,
		mustRawJSON(map[string]any{
			"bindingId":              activeContext.Binding.ID,
			"bindingRevision":        activeContext.Binding.Revision,
			"productionGenerationId": activeContext.Generation.ID,
			"rebuildId":              rebuildID,
			"workflowRunId":          run.ID,
			"retryFailed":            true,
			"attempt":                attemptNo,
			"failedItemCount":        failedItems,
		}),
	); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	item, err := s.projectVideoProductionRebuild(r.Context(), project.ID, rebuildID)
	if err != nil {
		return projectVideoProductionRebuildActionResult{}, err
	}
	return projectVideoProductionRebuildActionResult{Rebuild: item, WorkflowRunID: run.ID}, nil
}

func findVideoProductionRebuildByIdempotencyKey(ctx context.Context, tx pgx.Tx, projectID, key string) (projectVideoProductionRebuild, bool, error) {
	item, err := scanProjectVideoProductionRebuild(tx.QueryRow(ctx, projectVideoProductionRebuildSelectSQL+` WHERE project_id = $1 AND idempotency_key = $2`, projectID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return projectVideoProductionRebuild{}, false, nil
	}
	return item, err == nil, err
}

func (s *Server) projectVideoProductionRebuild(ctx context.Context, projectID, rebuildID string) (projectVideoProductionRebuild, error) {
	return scanProjectVideoProductionRebuild(s.db.QueryRow(ctx, projectVideoProductionRebuildSelectSQL+` WHERE project_id = $1 AND id = $2`, projectID, rebuildID))
}

const projectVideoProductionRebuildSelectSQL = `
	SELECT id::text, organization_id::text, project_id::text, source_binding_id::text,
	       source_generation_id::text, source_video_production_state, target_profile_version_id::text,
	       target_binding_id::text, target_generation_id::text, status,
	       reason, target_configuration, target_configuration_hash, impact_snapshot,
	       impact_token, expected_project_revision, episode_count, retained_asset_count,
	       workflow_run_id::text, idempotency_key, requested_by::text, requested_at,
	       approved_at, started_at, completed_at, failure_code, failure_message
	FROM project_video_production_rebuilds`

func scanProjectVideoProductionRebuild(row pgx.Row) (projectVideoProductionRebuild, error) {
	var item projectVideoProductionRebuild
	var approvedAt, startedAt, completedAt sql.NullTime
	var targetBindingID, targetGenerationID, workflowRunID sql.NullString
	var failureCode, failureMessage sql.NullString
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.SourceBindingID,
		&item.SourceGenerationID, &item.SourceVideoProductionState, &item.TargetProfileVersionID, &targetBindingID,
		&targetGenerationID, &item.Status, &item.Reason, &item.TargetConfiguration,
		&item.TargetConfigurationHash, &item.ImpactSnapshot, &item.ImpactToken,
		&item.ExpectedProjectRevision, &item.EpisodeCount, &item.RetainedAssetCount,
		&workflowRunID, &item.IdempotencyKey, &item.RequestedBy, &item.RequestedAt,
		&approvedAt, &startedAt, &completedAt, &failureCode, &failureMessage,
	)
	item.TargetBindingID = stringPtrFromNull(targetBindingID)
	item.TargetGenerationID = stringPtrFromNull(targetGenerationID)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.FailureCode = stringPtrFromNull(failureCode)
	item.FailureMessage = stringPtrFromNull(failureMessage)
	if approvedAt.Valid {
		item.ApprovedAt = &approvedAt.Time
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, err
}

const projectVideoProductionRebuildItemSelectSQL = `
	SELECT id::text, rebuild_id::text, project_id::text, script_episode_id::text,
	       episode_ordinal, script_episode_revision, script_episode_content_hash,
	       source_storyboard_plan_id::text, target_storyboard_plan_id::text,
	       workflow_run_id::text, status, checkpoint, attempt_count,
	       started_at, completed_at, failure_code, failure_message
	FROM project_video_production_rebuild_items`

func scanProjectVideoProductionRebuildItem(row pgx.Row) (projectVideoProductionRebuildItem, error) {
	var item projectVideoProductionRebuildItem
	var startedAt, completedAt sql.NullTime
	var sourceStoryboardPlanID, targetStoryboardPlanID, workflowRunID sql.NullString
	var failureCode, failureMessage sql.NullString
	err := row.Scan(
		&item.ID, &item.RebuildID, &item.ProjectID, &item.ScriptEpisodeID,
		&item.EpisodeOrdinal, &item.ScriptEpisodeRevision, &item.ScriptEpisodeContentHash,
		&sourceStoryboardPlanID, &targetStoryboardPlanID, &workflowRunID,
		&item.Status, &item.Checkpoint, &item.AttemptCount, &startedAt, &completedAt,
		&failureCode, &failureMessage,
	)
	item.SourceStoryboardPlanID = stringPtrFromNull(sourceStoryboardPlanID)
	item.TargetStoryboardPlanID = stringPtrFromNull(targetStoryboardPlanID)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.FailureCode = stringPtrFromNull(failureCode)
	item.FailureMessage = stringPtrFromNull(failureMessage)
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, err
}

func normalizeVideoProductionRebuildDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.ConstraintName == "project_video_production_rebuilds_one_active" || pgErr.ConstraintName == "project_video_production_rebuilds_project_id_idempotency_key_key") {
		return videoproduction.NewError(videoproduction.CodeRebuildConflict, "项目已有视频生产方案重建正在执行", true)
	}
	return err
}
