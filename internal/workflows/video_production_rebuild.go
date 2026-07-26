package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ProjectVideoProductionRebuildInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	RebuildID      string `json:"rebuildId"`
	RequestedBy    string `json:"requestedBy"`
	RetryFailed    bool   `json:"retryFailed,omitempty"`
}

type RebuildExternalWorkflow struct {
	WorkflowRunID      string `json:"workflowRunId"`
	TemporalWorkflowID string `json:"temporalWorkflowId"`
}

type PrepareProjectVideoProductionRebuildOutput struct {
	ExternalWorkflows  []RebuildExternalWorkflow `json:"externalWorkflows"`
	GenerationSwitched bool                      `json:"generationSwitched"`
}

type ProjectVideoProductionDrainOutput struct {
	Drained                 bool `json:"drained"`
	ActiveWorkflowCount     int  `json:"activeWorkflowCount"`
	ActiveProviderTaskCount int  `json:"activeProviderTaskCount"`
}

type SwitchProjectVideoProductionGenerationOutput struct {
	Binding                 videoproduction.Binding    `json:"binding"`
	Generation              videoproduction.Generation `json:"generation"`
	CommerceRebuildComplete bool                       `json:"commerceRebuildComplete,omitempty"`
	SwitchedUnitCount       int                        `json:"switchedUnitCount,omitempty"`
}

type RebuildEpisodeWorkItem struct {
	ItemID                         string `json:"itemId"`
	ScriptID                       string `json:"scriptId"`
	ScriptVersionID                string `json:"scriptVersionId"`
	ScriptEpisodeID                string `json:"scriptEpisodeId"`
	EpisodeIndex                   int    `json:"episodeIndex"`
	EpisodeTotal                   int    `json:"episodeTotal"`
	EpisodeTitle                   string `json:"episodeTitle"`
	ExpectedEpisodeRevision        int64  `json:"expectedEpisodeRevision"`
	ExpectedEpisodeContentHash     string `json:"expectedEpisodeContentHash"`
	WorkflowRunID                  string `json:"workflowRunId"`
	TemporalWorkflowID             string `json:"temporalWorkflowId"`
	Attempt                        int    `json:"attempt"`
	TimelineTimebase               int64  `json:"timelineTimebase"`
	FPSNumerator                   int    `json:"fpsNumerator"`
	FPSDenominator                 int    `json:"fpsDenominator"`
	ProductionGenerationID         string `json:"productionGenerationId"`
	VideoProductionBindingID       string `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64  `json:"videoProductionBindingRevision"`
}

type ProjectVideoProductionRebuildOutput struct {
	RebuildID      string `json:"rebuildId"`
	Status         string `json:"status"`
	EpisodeCount   int    `json:"episodeCount"`
	SucceededItems int    `json:"succeededItems"`
	FailedItems    int    `json:"failedItems"`
}

func ProjectVideoProductionRebuildWorkflow(ctx workflow.Context, input ProjectVideoProductionRebuildInput) (output ProjectVideoProductionRebuildOutput, resultErr error) {
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	defer func() {
		if resultErr == nil {
			return
		}
		failureCtx, _ := workflow.NewDisconnectedContext(ctx)
		failureCtx = workflow.WithActivityOptions(failureCtx, defaultActivityOptions())
		_ = workflow.ExecuteActivity(failureCtx, "FailProjectVideoProductionRebuild", input, "PROJECT_VIDEO_PRODUCTION_REBUILD_FAILED", resultErr.Error()).Get(failureCtx, nil)
	}()

	var preparation PrepareProjectVideoProductionRebuildOutput
	if err := workflow.ExecuteActivity(activityCtx, "PrepareProjectVideoProductionRebuild", input).Get(activityCtx, &preparation); err != nil {
		return output, err
	}
	if !preparation.GenerationSwitched {
		for _, external := range preparation.ExternalWorkflows {
			if strings.TrimSpace(external.TemporalWorkflowID) == "" {
				continue
			}
			_ = workflow.RequestCancelExternalWorkflow(ctx, external.TemporalWorkflowID, "").Get(ctx, nil)
		}
		for {
			var drain ProjectVideoProductionDrainOutput
			if err := workflow.ExecuteActivity(activityCtx, "CheckProjectVideoProductionDrain", input).Get(activityCtx, &drain); err != nil {
				return output, err
			}
			if drain.Drained {
				break
			}
			if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
				return output, err
			}
		}

		var switched SwitchProjectVideoProductionGenerationOutput
		if err := workflow.ExecuteActivity(activityCtx, "SwitchProjectVideoProductionGeneration", input).Get(activityCtx, &switched); err != nil {
			return output, err
		}
		if switched.CommerceRebuildComplete {
			return ProjectVideoProductionRebuildOutput{
				RebuildID:      input.RebuildID,
				Status:         "succeeded",
				EpisodeCount:   switched.SwitchedUnitCount,
				SucceededItems: switched.SwitchedUnitCount,
			}, nil
		}
	}
	var items []RebuildEpisodeWorkItem
	if err := workflow.ExecuteActivity(activityCtx, "ListProjectVideoProductionRebuildItems", input).Get(activityCtx, &items); err != nil {
		return output, err
	}
	for _, candidate := range items {
		var item RebuildEpisodeWorkItem
		if err := workflow.ExecuteActivity(activityCtx, "StartProjectVideoProductionRebuildItem", input, candidate.ItemID).Get(activityCtx, &item); err != nil {
			return output, err
		}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:        item.TemporalWorkflowID,
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    5 * time.Second,
				BackoffCoefficient: 2,
				MaximumInterval:    time.Minute,
				MaximumAttempts:    1,
			},
		})
		var episodeOutput ScriptStoryboardOutput
		err := workflow.ExecuteChildWorkflow(childCtx, ScriptEpisodeToStoryboardWorkflow, ScriptEpisodeToStoryboardInput{
			OrganizationID:             input.OrganizationID,
			ProjectID:                  input.ProjectID,
			WorkflowRunID:              item.WorkflowRunID,
			CreatedBy:                  input.RequestedBy,
			ScriptID:                   item.ScriptID,
			ScriptVersionID:            item.ScriptVersionID,
			ScriptEpisodeID:            item.ScriptEpisodeID,
			EpisodeIndex:               item.EpisodeIndex,
			EpisodeTotal:               item.EpisodeTotal,
			EpisodeTitle:               item.EpisodeTitle,
			ExpectedEpisodeRevision:    item.ExpectedEpisodeRevision,
			ExpectedEpisodeContentHash: item.ExpectedEpisodeContentHash,
			TimelineTimebase:           item.TimelineTimebase,
			FPSNumerator:               item.FPSNumerator,
			FPSDenominator:             item.FPSDenominator,
		}).Get(ctx, &episodeOutput)
		if err != nil {
			if markErr := workflow.ExecuteActivity(activityCtx, "FailProjectVideoProductionRebuildItem", input, item, "STORYBOARD_EPISODE_REBUILD_FAILED", err.Error()).Get(activityCtx, nil); markErr != nil {
				return output, markErr
			}
			continue
		}
		if err := workflow.ExecuteActivity(activityCtx, "CompleteProjectVideoProductionRebuildItem", input, item, episodeOutput).Get(activityCtx, nil); err != nil {
			return output, err
		}
	}
	if err := workflow.ExecuteActivity(activityCtx, "FinalizeProjectVideoProductionRebuild", input).Get(activityCtx, &output); err != nil {
		return output, err
	}
	if output.EpisodeCount > 0 && output.SucceededItems == 0 && output.FailedItems > 0 {
		return output, temporal.NewNonRetryableApplicationError(
			"所有分集分镜重建均失败，请重试失败分集",
			"VIDEO_PRODUCTION_REBUILD_ALL_EPISODES_FAILED",
			nil,
		)
	}
	return output, nil
}

func (a Activities) PrepareProjectVideoProductionRebuild(ctx context.Context, input ProjectVideoProductionRebuildInput) (PrepareProjectVideoProductionRebuildOutput, error) {
	if err := validateProjectVideoProductionRebuildInput(input); err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	defer tx.Rollback(ctx)
	var sourceGenerationID string
	var targetGenerationID *string
	var rebuildStatus string
	err = tx.QueryRow(ctx, `
		SELECT rebuild.source_generation_id::text, rebuild.target_generation_id::text, rebuild.status
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $2
		  AND rebuild.workflow_run_id = $3
		  AND rebuild.status IN ('approved', 'running')
		  AND project.active_video_production_rebuild_id = rebuild.id
		  AND project.video_production_locked = true
		FOR UPDATE OF rebuild, project
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID).Scan(&sourceGenerationID, &targetGenerationID, &rebuildStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PrepareProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
		}
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	output := PrepareProjectVideoProductionRebuildOutput{
		ExternalWorkflows:  make([]RebuildExternalWorkflow, 0),
		GenerationSwitched: targetGenerationID != nil,
	}
	command, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    failure_code = NULL, failure_message = NULL
		WHERE id = $1 AND project_id = $2 AND workflow_run_id = $3
		  AND status IN ('approved', 'running')
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return PrepareProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	command, err = tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_attempts
		SET status = 'running', completed_at = NULL, failure_code = NULL, failure_message = NULL
		WHERE rebuild_id = $1 AND workflow_run_id = $2
		  AND status IN ('queued', 'running')
	`, input.RebuildID, input.WorkflowRunID)
	if err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return PrepareProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	active, err := videoproduction.LoadActiveContext(ctx, tx, input.ProjectID)
	if err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	if rebuildStatus == "approved" {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
			"video.production.rebuild.started", "video_production_rebuild", input.RebuildID,
			mustJSON(videoProductionLifecyclePayload(input, active.Binding.ID, active.Binding.Revision, active.Generation.ID, map[string]any{
				"generationAlreadySwitched": output.GenerationSwitched,
			})),
		); err != nil {
			return PrepareProjectVideoProductionRebuildOutput{}, err
		}
	}
	if output.GenerationSwitched {
		if err := tx.Commit(ctx); err != nil {
			return PrepareProjectVideoProductionRebuildOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'cancelled', completed_at = now(), last_error_code = 'PRODUCTION_PROFILE_REBUILD_STARTED',
		    last_error_message = '项目视频生产方案开始重建', updated_at = now()
		WHERE project_id = $1 AND production_generation_id = $2
		  AND status = 'pending' AND workflow_run_id <> $3
	`, input.ProjectID, sourceGenerationID, input.WorkflowRunID); err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs run
		SET status = 'cancelled', error_code = 'PRODUCTION_PROFILE_REBUILD_STARTED',
		    error_message = '项目视频生产方案开始重建', completed_at = now(), cancelled_at = now(),
		    terminalized_at = now(), settled_at = now(), revision = revision + 1, updated_at = now()
		WHERE run.project_id = $1 AND run.production_generation_id = $2
		  AND run.id <> $3 AND run.status IN ('pending', 'queued')
	`, input.ProjectID, sourceGenerationID, input.WorkflowRunID); err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, temporal_workflow_id
		FROM workflow_runs
		WHERE project_id = $1 AND production_generation_id = $2 AND id <> $3
		  AND status IN ('running', 'cancelling', 'waiting_review')
		ORDER BY created_at
	`, input.ProjectID, sourceGenerationID, input.WorkflowRunID)
	if err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	for rows.Next() {
		var item RebuildExternalWorkflow
		if err := rows.Scan(&item.WorkflowRunID, &item.TemporalWorkflowID); err != nil {
			rows.Close()
			return PrepareProjectVideoProductionRebuildOutput{}, err
		}
		output.ExternalWorkflows = append(output.ExternalWorkflows, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PrepareProjectVideoProductionRebuildOutput{}, err
	}
	return output, nil
}

func (a Activities) CheckProjectVideoProductionDrain(ctx context.Context, input ProjectVideoProductionRebuildInput) (ProjectVideoProductionDrainOutput, error) {
	var output ProjectVideoProductionDrainOutput
	if err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM workflow_runs run
			 WHERE run.project_id = rebuild.project_id
			   AND run.production_generation_id = rebuild.source_generation_id
			   AND run.id <> $2
			   AND run.status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')),
			(SELECT count(*) FROM provider_async_tasks task
			 WHERE task.project_id = rebuild.project_id
			   AND task.production_generation_id = rebuild.source_generation_id
			   AND task.status IN ('queued', 'running', 'cancelling'))
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $3
		  AND rebuild.workflow_run_id = $2
		  AND rebuild.status = 'running'
		  AND project.active_video_production_rebuild_id = rebuild.id
	`, input.RebuildID, input.WorkflowRunID, input.ProjectID).Scan(&output.ActiveWorkflowCount, &output.ActiveProviderTaskCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectVideoProductionDrainOutput{}, ErrWorkflowWriteFenced
		}
		return ProjectVideoProductionDrainOutput{}, err
	}
	output.Drained = output.ActiveWorkflowCount == 0 && output.ActiveProviderTaskCount == 0
	return output, nil
}

func (a Activities) SwitchProjectVideoProductionGeneration(ctx context.Context, input ProjectVideoProductionRebuildInput) (SwitchProjectVideoProductionGenerationOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	defer tx.Rollback(ctx)
	var targetProfileID, projectKind string
	var sourceGenerationID, sourceBindingID string
	var sourceCommerceBindingID sql.NullString
	var targetConfigurationJSON json.RawMessage
	var targetConfigurationHash string
	if err := tx.QueryRow(ctx, `
		SELECT rebuild.target_profile_version_id::text, rebuild.source_generation_id::text,
		       rebuild.source_binding_id::text, rebuild.target_configuration,
		       rebuild.target_configuration_hash, project.project_kind,
		       rebuild.source_commerce_workflow_binding_id::text
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $2 AND rebuild.status = 'running'
		  AND rebuild.workflow_run_id = $3
		  AND project.active_video_production_rebuild_id = rebuild.id
		  AND project.video_production_locked = true
		FOR UPDATE OF rebuild, project
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID).Scan(
		&targetProfileID, &sourceGenerationID, &sourceBindingID,
		&targetConfigurationJSON, &targetConfigurationHash, &projectKind,
		&sourceCommerceBindingID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SwitchProjectVideoProductionGenerationOutput{}, ErrWorkflowWriteFenced
		}
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	var targetConfiguration videoproduction.ProductionConfigurationSnapshot
	if err := json.Unmarshal(targetConfigurationJSON, &targetConfiguration); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, fmt.Errorf("decode target production configuration: %w", err)
	}
	targetConfiguration, actualConfigurationHash, err := videoproduction.ProductionConfigurationHash(targetConfiguration)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if actualConfigurationHash != targetConfigurationHash {
		return SwitchProjectVideoProductionGenerationOutput{}, videoproduction.NewError(
			videoproduction.CodeRebuildImpactStale,
			"目标视频生产配置已变化，请重新确认",
			false,
		)
	}
	var source videoproduction.Context
	source, err = videoproduction.LoadActiveContext(ctx, tx, input.ProjectID)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if source.Generation.ID != sourceGenerationID || source.Binding.ID != sourceBindingID {
		return SwitchProjectVideoProductionGenerationOutput{}, videoproduction.NewError(videoproduction.CodeGenerationMismatch, "重建来源视频生产代已变化", false)
	}
	if projectKind == string(commerce.ProjectKindCommerceVideo) {
		if !sourceCommerceBindingID.Valid {
			return SwitchProjectVideoProductionGenerationOutput{}, commerce.Error{
				Code:    commerce.CodeBindingMismatch,
				Message: "带货视频换代缺少来源工作流绑定",
			}
		}
		output, err := a.switchCommerceProjectVideoProductionGeneration(
			ctx,
			tx,
			input,
			targetConfiguration,
			targetProfileID,
			source,
			sourceCommerceBindingID.String,
		)
		if err != nil {
			return SwitchProjectVideoProductionGenerationOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SwitchProjectVideoProductionGenerationOutput{}, err
		}
		return output, nil
	}
	var targetKey string
	var targetVersion int
	if err := tx.QueryRow(ctx, `
		SELECT profile.profile_key, version.version
		FROM video_production_profile_versions version
		JOIN video_production_profiles profile ON profile.id = version.profile_id
		WHERE version.id = $1
	`, targetProfileID).Scan(&targetKey, &targetVersion); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	target, err := videoproduction.ResolveProfileVersion(ctx, tx, targetKey, &targetVersion, true)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	var invalidEpisodes int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM project_video_production_rebuild_items item
		JOIN script_episodes episode ON episode.id = item.script_episode_id
		WHERE item.rebuild_id = $1
		  AND (episode.revision <> item.script_episode_revision OR episode.content_hash <> item.script_episode_content_hash)
	`, input.RebuildID).Scan(&invalidEpisodes); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if invalidEpisodes > 0 {
		return SwitchProjectVideoProductionGenerationOutput{}, videoproduction.NewError(videoproduction.CodeRebuildImpactStale, "重建分集内容已变化，请重新确认", false)
	}
	binding, generation, err := videoproduction.SwitchRebuildGeneration(ctx, tx, videoproduction.RebuildSwitchParams{
		RebuildID:      input.RebuildID,
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		CreatedBy:      input.RequestedBy,
		Source:         source,
		Target:         target,
		Configuration:  targetConfiguration,
	})
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET production_generation_id = $2,
		    video_production_binding_id = $3,
		    video_production_binding_revision = $4,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1 AND project_id = $5
		  AND production_generation_id = $6
		  AND video_production_binding_id = $7
	`, input.WorkflowRunID, generation.ID, binding.ID, binding.Revision,
		input.ProjectID, sourceGenerationID, sourceBindingID)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return SwitchProjectVideoProductionGenerationOutput{}, videoproduction.NewError(
			videoproduction.CodeGenerationMismatch,
			"重建 Workflow 的视频生产代切换失败",
			false,
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET production_generation_id = $2, updated_at = now()
		WHERE workflow_run_id = $1 AND production_generation_id = $3
	`, input.WorkflowRunID, generation.ID, sourceGenerationID); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	for _, event := range []struct {
		name          string
		aggregateType string
		aggregateID   string
		bindingID     string
		revision      int64
		generationID  string
	}{
		{name: "video.production.binding.superseded", aggregateType: "video_production_binding", aggregateID: source.Binding.ID, bindingID: source.Binding.ID, revision: source.Binding.Revision, generationID: source.Generation.ID},
		{name: "video.production.generation.superseded", aggregateType: "production_generation", aggregateID: source.Generation.ID, bindingID: source.Binding.ID, revision: source.Binding.Revision, generationID: source.Generation.ID},
		{name: "video.production.binding.created", aggregateType: "video_production_binding", aggregateID: binding.ID, bindingID: binding.ID, revision: binding.Revision, generationID: generation.ID},
		{name: "video.production.generation.activated", aggregateType: "production_generation", aggregateID: generation.ID, bindingID: binding.ID, revision: binding.Revision, generationID: generation.ID},
	} {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, event.name, event.aggregateType, event.aggregateID,
			mustJSON(videoProductionLifecyclePayload(input, event.bindingID, event.revision, event.generationID, map[string]any{
				"sourceGenerationId": source.Generation.ID,
				"targetGenerationId": generation.ID,
			})),
		); err != nil {
			return SwitchProjectVideoProductionGenerationOutput{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	return SwitchProjectVideoProductionGenerationOutput{Binding: binding, Generation: generation}, nil
}

func (a Activities) switchCommerceProjectVideoProductionGeneration(
	ctx context.Context,
	tx pgx.Tx,
	input ProjectVideoProductionRebuildInput,
	targetConfiguration videoproduction.ProductionConfigurationSnapshot,
	targetProfileID string,
	source videoproduction.Context,
	sourceCommerceBindingID string,
) (SwitchProjectVideoProductionGenerationOutput, error) {
	repository := commerce.NewRepository()
	template, err := resolveCommerceRebuildTemplate(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		sourceCommerceBindingID,
		targetProfileID,
	)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	promptBindings, modelContracts, err := commerceSetupTemplateContracts(template)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	setupSnapshot := commerceSetupSnapshot{
		Session: commerce.SetupSession{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
		},
		Template: template,
		Prompts:  promptBindings,
		Models:   modelContracts,
	}
	setupRuntime := NewCommerceSetupRuntime(a.db, a.gateway)
	routing, capabilities, _, err := setupRuntime.resolveSetupRouting(
		ctx,
		setupSnapshot,
		targetConfiguration,
		"",
		"",
	)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	configurationSnapshot, err := setupCommerceConfigurationSnapshot(setupSnapshot, targetConfiguration)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	routingSnapshot, err := json.Marshal(routing)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	capabilitySnapshot, err := json.Marshal(capabilities)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	service := commerce.NewService(repository)
	prepared, err := service.PrepareProjectRebuild(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		input.RebuildID,
		commerce.InitialBindingParams{
			WorkflowTemplateVersion: template.ID,
			CreatedBy:               input.RequestedBy,
			CompatibilityPolicy:     videoproduction.CompatibilityStrict,
			VideoOverrides:          json.RawMessage(`{}`),
			ProductionConfiguration: targetConfiguration,
			ConfigurationSnapshot:   configurationSnapshot,
			ModelRoutingSnapshot:    routingSnapshot,
			CapabilitySnapshot:      capabilitySnapshot,
		},
	)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if prepared.Target.VideoProfileVersionID != targetProfileID {
		return SwitchProjectVideoProductionGenerationOutput{}, commerce.Error{
			Code:    commerce.CodeBindingMismatch,
			Message: "带货视频业务模板与目标视频生产方案不一致",
		}
	}
	activated, err := service.ActivatePreparedProjectRebuild(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		input.RebuildID,
	)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	active, err := videoproduction.LoadActiveContext(ctx, tx, input.ProjectID)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if active.Generation.ID != activated.ProjectGeneration.ID ||
		active.Binding.ID != activated.VideoBinding.ID ||
		active.Binding.Revision != activated.VideoBinding.Revision {
		return SwitchProjectVideoProductionGenerationOutput{}, commerce.Error{
			Code:    commerce.CodeBindingMismatch,
			Message: "带货视频换代激活结果与项目活动生产代不一致",
		}
	}
	command, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET production_generation_id = $2,
		    video_production_binding_id = $3,
		    video_production_binding_revision = $4,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1 AND project_id = $5
		  AND production_generation_id = $6
		  AND video_production_binding_id = $7
	`, input.WorkflowRunID, active.Generation.ID, active.Binding.ID, active.Binding.Revision,
		input.ProjectID, source.Generation.ID, source.Binding.ID)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return SwitchProjectVideoProductionGenerationOutput{}, videoproduction.NewError(
			videoproduction.CodeGenerationMismatch,
			"带货视频换代 Workflow 的生产代切换失败",
			false,
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET production_generation_id = $2, updated_at = now()
		WHERE workflow_run_id = $1 AND production_generation_id = $3
	`, input.WorkflowRunID, active.Generation.ID, source.Generation.ID); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	workflowOutput := ProjectVideoProductionRebuildOutput{
		RebuildID:      input.RebuildID,
		Status:         "succeeded",
		EpisodeCount:   activated.SwitchedUnitCount,
		SucceededItems: activated.SwitchedUnitCount,
	}
	command, err = tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_attempts
		SET status = 'succeeded', completed_at = now(),
		    failure_code = NULL, failure_message = NULL
		WHERE rebuild_id = $1 AND workflow_run_id = $2 AND status = 'running'
	`, input.RebuildID, input.WorkflowRunID)
	if err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return SwitchProjectVideoProductionGenerationOutput{}, ErrWorkflowWriteFenced
	}
	if _, transitioned, err := transitionWorkflowRunTx(
		ctx,
		tx,
		input.WorkflowRunID,
		"succeeded",
		"",
		"",
		mustJSON(workflowOutput),
	); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	} else if !transitioned {
		return SwitchProjectVideoProductionGenerationOutput{}, ErrWorkflowWriteFenced
	}
	for _, event := range []struct {
		name          string
		aggregateType string
		aggregateID   string
		bindingID     string
		revision      int64
		generationID  string
	}{
		{name: "video.production.binding.superseded", aggregateType: "video_production_binding", aggregateID: source.Binding.ID, bindingID: source.Binding.ID, revision: source.Binding.Revision, generationID: source.Generation.ID},
		{name: "video.production.generation.superseded", aggregateType: "production_generation", aggregateID: source.Generation.ID, bindingID: source.Binding.ID, revision: source.Binding.Revision, generationID: source.Generation.ID},
		{name: "video.production.binding.created", aggregateType: "video_production_binding", aggregateID: active.Binding.ID, bindingID: active.Binding.ID, revision: active.Binding.Revision, generationID: active.Generation.ID},
		{name: "video.production.generation.activated", aggregateType: "production_generation", aggregateID: active.Generation.ID, bindingID: active.Binding.ID, revision: active.Binding.Revision, generationID: active.Generation.ID},
	} {
		if err := insertEvent(
			ctx,
			tx,
			input.OrganizationID,
			input.ProjectID,
			event.name,
			event.aggregateType,
			event.aggregateID,
			mustJSON(videoProductionLifecyclePayload(input, event.bindingID, event.revision, event.generationID, map[string]any{
				"sourceGenerationId": source.Generation.ID,
				"targetGenerationId": active.Generation.ID,
			})),
		); err != nil {
			return SwitchProjectVideoProductionGenerationOutput{}, err
		}
	}
	if err := insertEvent(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		"commerce.workflow_binding.created",
		"commerce_workflow_binding",
		activated.CommerceBinding.ID,
		mustJSON(map[string]any{
			"projectProductionGenerationId":   activated.ProjectGeneration.ID,
			"videoProductionBindingId":        activated.VideoBinding.ID,
			"videoProductionBindingRevision":  activated.VideoBinding.Revision,
			"commerceWorkflowBindingId":       activated.CommerceBinding.ID,
			"commerceWorkflowBindingRevision": activated.CommerceBinding.Revision,
		}),
	); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if err := insertEvent(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		"commerce.project_generation.activated",
		"production_generation",
		activated.ProjectGeneration.ID,
		mustJSON(map[string]any{
			"projectProductionGenerationId":   activated.ProjectGeneration.ID,
			"projectProductionGenerationNo":   activated.ProjectGeneration.GenerationNo,
			"videoProductionBindingId":        activated.VideoBinding.ID,
			"videoProductionBindingRevision":  activated.VideoBinding.Revision,
			"commerceWorkflowBindingId":       activated.CommerceBinding.ID,
			"commerceWorkflowBindingRevision": activated.CommerceBinding.Revision,
		}),
	); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	if err := insertEvent(
		ctx,
		tx,
		input.OrganizationID,
		input.ProjectID,
		"video.production.rebuild.completed",
		"video_production_rebuild",
		input.RebuildID,
		mustJSON(videoProductionLifecyclePayload(
			input,
			active.Binding.ID,
			active.Binding.Revision,
			active.Generation.ID,
			map[string]any{
				"status":         "succeeded",
				"episodeCount":   activated.SwitchedUnitCount,
				"succeededItems": activated.SwitchedUnitCount,
				"failedItems":    0,
			},
		)),
	); err != nil {
		return SwitchProjectVideoProductionGenerationOutput{}, err
	}
	return SwitchProjectVideoProductionGenerationOutput{
		Binding:                 active.Binding,
		Generation:              active.Generation,
		CommerceRebuildComplete: true,
		SwitchedUnitCount:       activated.SwitchedUnitCount,
	}, nil
}

func resolveCommerceRebuildTemplate(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	sourceCommerceBindingID string,
	targetProfileVersionID string,
) (commerce.WorkflowTemplateVersion, error) {
	rows, err := tx.Query(ctx, `
		WITH source_template AS (
			SELECT version.id, template.template_key
			FROM project_commerce_workflow_bindings binding
			JOIN commerce_workflow_template_versions version
			  ON version.id = binding.template_version_id
			JOIN commerce_workflow_templates template
			  ON template.id = version.template_id
			WHERE binding.id = $1
			  AND binding.project_id = $2
			  AND binding.organization_id = $3
		)
		SELECT version.id::text, template.id::text, template.template_key, version.version,
		       version.content_hash, version.configuration_snapshot, version.prompt_bindings,
		       version.agent_model_contracts, version.language_contract,
		       version.image_capability_contract, version.video_capability_contract,
		       profile.profile_key, profile_version.version
		FROM source_template source
		JOIN commerce_workflow_templates template
		  ON template.template_key = source.template_key
		JOIN commerce_workflow_template_versions version
		  ON version.template_id = template.id
		JOIN video_production_profile_versions profile_version
		  ON profile_version.id = version.video_production_profile_version_id
		JOIN video_production_profiles profile
		  ON profile.id = profile_version.profile_id
		WHERE template.status = 'active'
		  AND version.status IN ('published', 'retired')
		  AND profile_version.id = $4
		  AND profile_version.lifecycle_state = 'published'
		  AND profile_version.implementation_state = 'available'
		  AND (template.organization_id IS NULL OR template.organization_id = $3)
		ORDER BY CASE WHEN version.id = source.id THEN 0 ELSE 1 END,
		         version.version DESC
		FOR SHARE OF version, template, profile_version, profile
	`, sourceCommerceBindingID, projectID, organizationID, targetProfileVersionID)
	if err != nil {
		return commerce.WorkflowTemplateVersion{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate commerce.WorkflowTemplateVersion
		if err := rows.Scan(
			&candidate.ID,
			&candidate.TemplateID,
			&candidate.TemplateKey,
			&candidate.Version,
			&candidate.ContentHash,
			&candidate.ConfigurationSnapshot,
			&candidate.PromptBindings,
			&candidate.AgentModelContracts,
			&candidate.LanguageContract,
			&candidate.ImageCapabilityContract,
			&candidate.VideoCapabilityContract,
			&candidate.VideoProfileKey,
			&candidate.VideoProfileVersion,
		); err != nil {
			return commerce.WorkflowTemplateVersion{}, err
		}
		return candidate, nil
	}
	if err := rows.Err(); err != nil {
		return commerce.WorkflowTemplateVersion{}, err
	}
	return commerce.WorkflowTemplateVersion{}, commerce.Error{
		Code:    commerce.CodeProjectRebuildBlocked,
		Message: "目标视频生产方案当前不可用",
	}
}

func (a Activities) ListProjectVideoProductionRebuildItems(ctx context.Context, input ProjectVideoProductionRebuildInput) ([]RebuildEpisodeWorkItem, error) {
	status := "pending"
	if input.RetryFailed {
		status = "failed"
	}
	rows, err := a.db.Query(ctx, `
		SELECT item.id::text
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		LEFT JOIN project_video_production_rebuild_items item
		  ON item.rebuild_id = rebuild.id
		 AND item.project_id = rebuild.project_id
		 AND item.status = $3
		WHERE rebuild.id = $1 AND rebuild.project_id = $2
		  AND rebuild.workflow_run_id = $4
		  AND rebuild.status = 'running'
		  AND project.active_video_production_rebuild_id = rebuild.id
		ORDER BY item.episode_ordinal, item.id
	`, input.RebuildID, input.ProjectID, status, input.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RebuildEpisodeWorkItem, 0)
	ownerMatched := false
	for rows.Next() {
		ownerMatched = true
		var itemID sql.NullString
		if err := rows.Scan(&itemID); err != nil {
			return nil, err
		}
		if itemID.Valid {
			items = append(items, RebuildEpisodeWorkItem{ItemID: itemID.String})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !ownerMatched {
		return nil, ErrWorkflowWriteFenced
	}
	return items, nil
}

func (a Activities) StartProjectVideoProductionRebuildItem(ctx context.Context, input ProjectVideoProductionRebuildInput, itemID string) (RebuildEpisodeWorkItem, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	defer tx.Rollback(ctx)
	var item RebuildEpisodeWorkItem
	var itemStatus string
	var generationID, bindingID string
	var bindingRevision int64
	var requestedBy string
	var profileSnapshot json.RawMessage
	var workflowRunID sql.NullString
	var currentEpisodeRevision int64
	var currentEpisodeContentHash string
	if err := tx.QueryRow(ctx, `
		SELECT item.id::text, item.script_episode_id::text, item.episode_ordinal,
		       item.attempt_count, item.status, item.script_episode_revision,
		       item.script_episode_content_hash, episode.revision, episode.content_hash,
		       rebuild.target_generation_id::text, rebuild.target_binding_id::text,
		       binding.revision, rebuild.requested_by::text,
		       episode.script_id::text, episode.script_version_id::text, episode.episode_title,
		       binding.profile_snapshot,
		       rebuild.episode_count,
		       item.workflow_run_id::text
		FROM project_video_production_rebuild_items item
		JOIN project_video_production_rebuilds rebuild ON rebuild.id = item.rebuild_id
		JOIN projects project ON project.id = rebuild.project_id
		JOIN project_video_production_bindings binding ON binding.id = rebuild.target_binding_id
		JOIN script_episodes episode ON episode.id = item.script_episode_id
		WHERE item.id = $1 AND item.rebuild_id = $2 AND item.project_id = $3
		  AND rebuild.workflow_run_id = $4
		  AND rebuild.status = 'running'
		  AND project.active_video_production_rebuild_id = rebuild.id
		FOR UPDATE OF item, rebuild, project
	`, itemID, input.RebuildID, input.ProjectID, input.WorkflowRunID).Scan(
		&item.ItemID, &item.ScriptEpisodeID, &item.EpisodeIndex,
		&item.Attempt, &itemStatus, &item.ExpectedEpisodeRevision,
		&item.ExpectedEpisodeContentHash, &currentEpisodeRevision, &currentEpisodeContentHash,
		&generationID, &bindingID, &bindingRevision, &requestedBy,
		&item.ScriptID, &item.ScriptVersionID, &item.EpisodeTitle,
		&profileSnapshot,
		&item.EpisodeTotal, &workflowRunID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RebuildEpisodeWorkItem{}, ErrWorkflowWriteFenced
		}
		return RebuildEpisodeWorkItem{}, err
	}
	if currentEpisodeRevision != item.ExpectedEpisodeRevision || currentEpisodeContentHash != item.ExpectedEpisodeContentHash {
		return RebuildEpisodeWorkItem{}, temporal.NewNonRetryableApplicationError(
			"重建分集内容已变化，请重新确认视频生产配置影响",
			videoproduction.CodeRebuildImpactStale,
			nil,
		)
	}
	if workflowRunID.Valid {
		item.WorkflowRunID = workflowRunID.String
	}
	configuration, err := videoproduction.DecodeProductionConfiguration(profileSnapshot)
	if err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	item.TimelineTimebase = configuration.TimelineTimebase
	item.FPSNumerator = configuration.FPSNumerator
	item.FPSDenominator = configuration.FPSDenominator
	item.ProductionGenerationID = generationID
	item.VideoProductionBindingID = bindingID
	item.VideoProductionBindingRevision = bindingRevision
	if itemStatus == "running" && item.WorkflowRunID != "" {
		if err := tx.QueryRow(ctx, `SELECT temporal_workflow_id FROM workflow_runs WHERE id = $1`, item.WorkflowRunID).Scan(&item.TemporalWorkflowID); err != nil {
			return RebuildEpisodeWorkItem{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return RebuildEpisodeWorkItem{}, err
		}
		return item, nil
	}
	if itemStatus != "pending" && itemStatus != "failed" {
		return RebuildEpisodeWorkItem{}, temporal.NewNonRetryableApplicationError("重建分集状态不可执行", "REBUILD_ITEM_STATE_INVALID", nil)
	}
	item.Attempt++
	item.WorkflowRunID = uuid.NewString()
	item.TemporalWorkflowID = fmt.Sprintf("video-rebuild-%s-episode-%s-attempt-%d", input.RebuildID, item.ScriptEpisodeID, item.Attempt)
	runInput := mustJSON(map[string]any{
		"rebuildId": input.RebuildID, "rebuildItemId": item.ItemID,
		"scriptEpisodeId": item.ScriptEpisodeID, "attempt": item.Attempt,
		"scriptEpisodeRevision":    item.ExpectedEpisodeRevision,
		"scriptEpisodeContentHash": item.ExpectedEpisodeContentHash,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			root_workflow_run_id, started_at
		)
		VALUES ($1, $2, $3, $4, 'video_production_rebuild_episode', 'running',
		        $5, '{}', $6, $7, $8, $9, $10, now())
	`, item.WorkflowRunID, input.OrganizationID, input.ProjectID, item.TemporalWorkflowID,
		runInput, requestedBy, generationID, bindingID, bindingRevision, input.WorkflowRunID); err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = 'running', workflow_run_id = $2::uuid, attempt_count = $3::integer,
		    started_at = now(), completed_at = NULL, failure_code = NULL, failure_message = NULL,
		    checkpoint = jsonb_build_object(
		      'workflowRunId', ($2::uuid)::text,
		      'attempt', $3::integer,
		      'scriptEpisodeRevision', $4::bigint,
		      'scriptEpisodeContentHash', $5::text
		    )
		WHERE id = $1
	`, item.ItemID, item.WorkflowRunID, item.Attempt, item.ExpectedEpisodeRevision, item.ExpectedEpisodeContentHash); err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"video.production.rebuild.item.started", "video_production_rebuild_item", item.ItemID,
		mustJSON(videoProductionLifecyclePayload(input, bindingID, bindingRevision, generationID, map[string]any{
			"rebuildItemId":     item.ItemID,
			"episodeId":         item.ScriptEpisodeID,
			"itemWorkflowRunId": item.WorkflowRunID,
			"attempt":           item.Attempt,
		})),
	); err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RebuildEpisodeWorkItem{}, err
	}
	return item, nil
}

func (a Activities) CompleteProjectVideoProductionRebuildItem(ctx context.Context, input ProjectVideoProductionRebuildInput, item RebuildEpisodeWorkItem, output ScriptStoryboardOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockProjectVideoProductionRebuildItemOwner(ctx, tx, input, item); err != nil {
		return err
	}
	var targetPlanID *string
	if err := tx.QueryRow(ctx, `
		SELECT plan.id::text
		FROM project_video_production_rebuilds rebuild
		JOIN storyboard_plans plan
		  ON plan.project_id = rebuild.project_id
		 AND plan.production_generation_id = rebuild.target_generation_id
		JOIN project_video_production_rebuild_items item ON item.rebuild_id = rebuild.id
		WHERE rebuild.id = $1 AND item.id = $2 AND plan.script_episode_id = item.script_episode_id
		  AND plan.active = true
		ORDER BY plan.revision DESC LIMIT 1
	`, input.RebuildID, item.ItemID).Scan(&targetPlanID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = 'succeeded', target_storyboard_plan_id = $3,
		    checkpoint = checkpoint || $4::jsonb, completed_at = now(),
		    failure_code = NULL, failure_message = NULL
		WHERE id = $1 AND workflow_run_id = $2 AND status = 'running'
	`, item.ItemID, item.WorkflowRunID, targetPlanID, mustJSON(map[string]any{
		"storyboardPlanId": targetPlanID,
		"shotCount":        len(output.Shots),
		"providerCallIds":  output.ProviderCallIDs,
	}))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, item.WorkflowRunID, "succeeded", "", "", mustJSON(output)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"video.production.rebuild.item.completed", "video_production_rebuild_item", item.ItemID,
		mustJSON(videoProductionLifecyclePayload(input, item.VideoProductionBindingID, item.VideoProductionBindingRevision, item.ProductionGenerationID, map[string]any{
			"rebuildItemId":     item.ItemID,
			"episodeId":         item.ScriptEpisodeID,
			"itemWorkflowRunId": item.WorkflowRunID,
			"storyboardPlanId":  targetPlanID,
			"shotCount":         len(output.Shots),
		})),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) FailProjectVideoProductionRebuildItem(ctx context.Context, input ProjectVideoProductionRebuildInput, item RebuildEpisodeWorkItem, code, message string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockProjectVideoProductionRebuildItemOwner(ctx, tx, input, item); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = 'failed', completed_at = now(), failure_code = $3, failure_message = $4,
		    checkpoint = checkpoint || jsonb_build_object('failedAt', now(), 'failureCode', $3::text)
		WHERE id = $1 AND workflow_run_id = $2 AND status = 'running'
	`, item.ItemID, item.WorkflowRunID, code, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, item.WorkflowRunID, "failed", code, message, json.RawMessage(`{}`)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"video.production.rebuild.item.failed", "video_production_rebuild_item", item.ItemID,
		mustJSON(videoProductionLifecyclePayload(input, item.VideoProductionBindingID, item.VideoProductionBindingRevision, item.ProductionGenerationID, map[string]any{
			"rebuildItemId":     item.ItemID,
			"episodeId":         item.ScriptEpisodeID,
			"itemWorkflowRunId": item.WorkflowRunID,
			"failureCode":       code,
			"failureMessage":    message,
		})),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockProjectVideoProductionRebuildItemOwner(ctx context.Context, tx pgx.Tx, input ProjectVideoProductionRebuildInput, item RebuildEpisodeWorkItem) error {
	var itemWorkflowRunID string
	var expectedRevision, currentRevision int64
	var expectedContentHash, currentContentHash string
	err := tx.QueryRow(ctx, `
		SELECT rebuild_item.workflow_run_id::text,
		       rebuild_item.script_episode_revision, rebuild_item.script_episode_content_hash,
		       episode.revision, episode.content_hash
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		JOIN project_video_production_rebuild_items rebuild_item
		  ON rebuild_item.rebuild_id = rebuild.id
		 AND rebuild_item.project_id = rebuild.project_id
		JOIN script_episodes episode ON episode.id = rebuild_item.script_episode_id
		WHERE rebuild.id = $1
		  AND rebuild.project_id = $2
		  AND rebuild.workflow_run_id = $3
		  AND rebuild.status = 'running'
		  AND project.active_video_production_rebuild_id = rebuild.id
		  AND rebuild_item.id = $4
		  AND rebuild_item.workflow_run_id = $5
		  AND rebuild_item.status = 'running'
		FOR UPDATE OF rebuild, project, rebuild_item
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID, item.ItemID, item.WorkflowRunID).Scan(
		&itemWorkflowRunID, &expectedRevision, &expectedContentHash, &currentRevision, &currentContentHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorkflowWriteFenced
	}
	if err != nil {
		return err
	}
	if currentRevision != expectedRevision || currentContentHash != expectedContentHash ||
		(item.ExpectedEpisodeRevision > 0 && item.ExpectedEpisodeRevision != expectedRevision) ||
		(strings.TrimSpace(item.ExpectedEpisodeContentHash) != "" && item.ExpectedEpisodeContentHash != expectedContentHash) {
		return videoproduction.NewError(
			videoproduction.CodeRebuildImpactStale,
			"重建分集内容已变化，请重新确认视频生产配置影响",
			false,
		)
	}
	return nil
}

const (
	videoProductionRebuildAllFailedCode    = "VIDEO_PRODUCTION_REBUILD_ALL_EPISODES_FAILED"
	videoProductionRebuildAllFailedMessage = "所有分集分镜重建均失败，请重试失败分集"
)

type projectVideoProductionRebuildDisposition struct {
	RebuildStatus  string
	AttemptStatus  string
	WorkflowStatus string
	ProjectState   string
	ProjectLocked  bool
	FailureCode    string
	FailureMessage string
}

func classifyProjectVideoProductionRebuild(episodeCount, succeededItems, failedItems int) (projectVideoProductionRebuildDisposition, error) {
	if episodeCount < 0 || succeededItems < 0 || failedItems < 0 || succeededItems+failedItems != episodeCount {
		return projectVideoProductionRebuildDisposition{}, errors.New("video production rebuild item counts are inconsistent")
	}
	if episodeCount == 0 {
		return projectVideoProductionRebuildDisposition{
			RebuildStatus: "succeeded", AttemptStatus: "succeeded", WorkflowStatus: "succeeded",
			ProjectState: "storyboard_required",
		}, nil
	}
	if failedItems == 0 {
		return projectVideoProductionRebuildDisposition{
			RebuildStatus: "succeeded", AttemptStatus: "succeeded", WorkflowStatus: "succeeded",
			ProjectState: "ready",
		}, nil
	}
	if succeededItems > 0 {
		return projectVideoProductionRebuildDisposition{
			RebuildStatus: "partial_succeeded", AttemptStatus: "partial_succeeded", WorkflowStatus: "partial_succeeded",
			ProjectState: "storyboard_required", ProjectLocked: true,
		}, nil
	}
	return projectVideoProductionRebuildDisposition{
		RebuildStatus: "storyboard_required", AttemptStatus: "failed", WorkflowStatus: "failed",
		ProjectState: "storyboard_required", ProjectLocked: true,
		FailureCode: videoProductionRebuildAllFailedCode, FailureMessage: videoProductionRebuildAllFailedMessage,
	}, nil
}

func (a Activities) FinalizeProjectVideoProductionRebuild(ctx context.Context, input ProjectVideoProductionRebuildInput) (ProjectVideoProductionRebuildOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	defer tx.Rollback(ctx)

	var rebuildStatus string
	var targetGenerationID, activeGenerationID, activeRebuildID sql.NullString
	err = tx.QueryRow(ctx, `
		SELECT rebuild.status, rebuild.target_generation_id::text,
		       project.active_video_production_generation_id::text,
		       project.active_video_production_rebuild_id::text
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $2 AND rebuild.workflow_run_id = $3
		FOR UPDATE OF rebuild, project
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID).Scan(
		&rebuildStatus, &targetGenerationID, &activeGenerationID, &activeRebuildID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}

	output := ProjectVideoProductionRebuildOutput{RebuildID: input.RebuildID}
	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status IN ('succeeded', 'skipped')),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status IN ('pending', 'running'))
		FROM project_video_production_rebuild_items
		WHERE rebuild_id = $1
	`, input.RebuildID).Scan(&output.EpisodeCount, &output.SucceededItems, &output.FailedItems, &pending); err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	if rebuildStatus == "succeeded" || rebuildStatus == "partial_succeeded" || rebuildStatus == "storyboard_required" {
		output.Status = rebuildStatus
		return output, nil
	}
	if rebuildStatus != "running" || !targetGenerationID.Valid || !activeGenerationID.Valid ||
		activeGenerationID.String != targetGenerationID.String || !activeRebuildID.Valid || activeRebuildID.String != input.RebuildID {
		return ProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	if pending > 0 {
		return ProjectVideoProductionRebuildOutput{}, errors.New("video production rebuild still has pending episode items")
	}
	disposition, err := classifyProjectVideoProductionRebuild(output.EpisodeCount, output.SucceededItems, output.FailedItems)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	output.Status = disposition.RebuildStatus

	command, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET status = $4, completed_at = now(),
		    failure_code = NULLIF($5, ''), failure_message = NULLIF($6, '')
		WHERE id = $1 AND project_id = $2 AND workflow_run_id = $3 AND status = 'running'
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID, disposition.RebuildStatus, disposition.FailureCode, disposition.FailureMessage)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return ProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	command, err = tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_attempts
		SET status = $3, completed_at = now(),
		    failure_code = NULLIF($4, ''), failure_message = NULLIF($5, '')
		WHERE rebuild_id = $1 AND workflow_run_id = $2 AND status = 'running'
	`, input.RebuildID, input.WorkflowRunID, disposition.AttemptStatus, disposition.FailureCode, disposition.FailureMessage)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return ProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	command, err = tx.Exec(ctx, `
		UPDATE projects
		SET video_production_locked = $2::boolean,
		    video_production_state = $3,
		    active_video_production_rebuild_id = CASE WHEN $2::boolean THEN $4::uuid ELSE NULL END,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $5
		  AND active_video_production_rebuild_id = $4
	`, input.ProjectID, disposition.ProjectLocked, disposition.ProjectState, input.RebuildID, targetGenerationID.String)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return ProjectVideoProductionRebuildOutput{}, ErrWorkflowWriteFenced
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, disposition.WorkflowStatus, disposition.FailureCode, disposition.FailureMessage, mustJSON(output)); err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	active, err := videoproduction.LoadActiveContext(ctx, tx, input.ProjectID)
	if err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	eventName := "video.production.rebuild.completed"
	if output.Status == "partial_succeeded" {
		eventName = "video.production.rebuild.partial"
	} else if output.Status == "storyboard_required" {
		eventName = "video.production.rebuild.storyboard_required"
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, eventName, "video_production_rebuild", input.RebuildID,
		mustJSON(videoProductionLifecyclePayload(input, active.Binding.ID, active.Binding.Revision, active.Generation.ID, map[string]any{
			"status":         output.Status,
			"episodeCount":   output.EpisodeCount,
			"succeededItems": output.SucceededItems,
			"failedItems":    output.FailedItems,
			"failureCode":    disposition.FailureCode,
		})),
	); err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectVideoProductionRebuildOutput{}, err
	}
	return output, nil
}

func (a Activities) FailProjectVideoProductionRebuild(ctx context.Context, input ProjectVideoProductionRebuildInput, code, message string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	code = strings.TrimSpace(code)
	if code == "" {
		code = "PROJECT_VIDEO_PRODUCTION_REBUILD_FAILED"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "视频生产配置重建失败"
	}

	var rebuildStatus, sourceGenerationID, sourceState string
	var targetGenerationID, activeGenerationID, activeRebuildID sql.NullString
	err = tx.QueryRow(ctx, `
		SELECT rebuild.status, rebuild.source_generation_id::text,
		       rebuild.target_generation_id::text, rebuild.source_video_production_state,
		       project.active_video_production_generation_id::text,
		       project.active_video_production_rebuild_id::text
		FROM project_video_production_rebuilds rebuild
		JOIN projects project ON project.id = rebuild.project_id
		WHERE rebuild.id = $1 AND rebuild.project_id = $2 AND rebuild.workflow_run_id = $3
		FOR UPDATE OF rebuild, project
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID).Scan(
		&rebuildStatus, &sourceGenerationID, &targetGenerationID, &sourceState,
		&activeGenerationID, &activeRebuildID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorkflowWriteFenced
	}
	if err != nil {
		return err
	}
	if rebuildStatus == "succeeded" || rebuildStatus == "partial_succeeded" ||
		rebuildStatus == "storyboard_required" || rebuildStatus == "failed" || rebuildStatus == "cancelled" {
		return nil
	}
	if (rebuildStatus != "approved" && rebuildStatus != "running") ||
		!activeRebuildID.Valid || activeRebuildID.String != input.RebuildID {
		return ErrWorkflowWriteFenced
	}
	expectedGenerationID := sourceGenerationID
	nextState := sourceState
	nextLocked := false
	if targetGenerationID.Valid {
		expectedGenerationID = targetGenerationID.String
		nextState = "storyboard_required"
		nextLocked = true
	}
	if !activeGenerationID.Valid || activeGenerationID.String != expectedGenerationID {
		return ErrWorkflowWriteFenced
	}

	rows, err := tx.Query(ctx, `
		UPDATE project_video_production_rebuild_items
		SET status = 'failed', completed_at = now(), failure_code = $2, failure_message = $3,
		    checkpoint = checkpoint || jsonb_build_object('failedAt', now(), 'failureCode', $2::text)
		WHERE rebuild_id = $1 AND status IN ('pending', 'running')
		RETURNING workflow_run_id::text
	`, input.RebuildID, code, message)
	if err != nil {
		return err
	}
	childWorkflowRunIDs := make([]string, 0)
	for rows.Next() {
		var childWorkflowRunID sql.NullString
		if err := rows.Scan(&childWorkflowRunID); err != nil {
			rows.Close()
			return err
		}
		if childWorkflowRunID.Valid {
			childWorkflowRunIDs = append(childWorkflowRunIDs, childWorkflowRunID.String)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, childWorkflowRunID := range childWorkflowRunIDs {
		if _, _, err := transitionWorkflowRunTx(ctx, tx, childWorkflowRunID, "failed", code, message, json.RawMessage(`{}`)); err != nil {
			return err
		}
	}

	command, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET status = 'failed', failure_code = $4, failure_message = $5, completed_at = now()
		WHERE id = $1 AND project_id = $2 AND workflow_run_id = $3
		  AND status IN ('approved', 'running')
	`, input.RebuildID, input.ProjectID, input.WorkflowRunID, code, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	command, err = tx.Exec(ctx, `
		UPDATE project_video_production_rebuild_attempts
		SET status = 'failed', completed_at = now(), failure_code = $3, failure_message = $4
		WHERE rebuild_id = $1 AND workflow_run_id = $2 AND status IN ('queued', 'running')
	`, input.RebuildID, input.WorkflowRunID, code, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	active, err := videoproduction.LoadActiveContext(ctx, tx, input.ProjectID)
	if err != nil {
		return err
	}
	command, err = tx.Exec(ctx, `
		UPDATE projects
		SET video_production_locked = $2::boolean,
		    video_production_state = $3,
		    active_video_production_rebuild_id = CASE WHEN $2::boolean THEN $4::uuid ELSE NULL END,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $5
		  AND active_video_production_rebuild_id = $4
	`, input.ProjectID, nextLocked, nextState, input.RebuildID, expectedGenerationID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, "failed", code, message, json.RawMessage(`{}`)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"video.production.rebuild.failed", "video_production_rebuild", input.RebuildID,
		mustJSON(videoProductionLifecyclePayload(input, active.Binding.ID, active.Binding.Revision, active.Generation.ID, map[string]any{
			"failureCode":    code,
			"failureMessage": message,
		})),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func videoProductionLifecyclePayload(input ProjectVideoProductionRebuildInput, bindingID string, bindingRevision int64, generationID string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"bindingId":              bindingID,
		"bindingRevision":        bindingRevision,
		"productionGenerationId": generationID,
		"rebuildId":              input.RebuildID,
		"workflowRunId":          input.WorkflowRunID,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func validateProjectVideoProductionRebuildInput(input ProjectVideoProductionRebuildInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.RebuildID) == "" || strings.TrimSpace(input.RequestedBy) == "" {
		return temporal.NewNonRetryableApplicationError("视频生产方案重建输入不完整", "INVALID_REQUEST", nil)
	}
	return nil
}
