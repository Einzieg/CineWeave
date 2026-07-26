package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) ResolvePublishedWorkflowTemplate(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	templateKey string,
) (WorkflowTemplateVersion, error) {
	var item WorkflowTemplateVersion
	err := tx.QueryRow(ctx, `
		SELECT version.id::text, template.id::text, template.template_key, version.version,
		       version.content_hash, version.configuration_snapshot, version.prompt_bindings,
		       version.agent_model_contracts, version.language_contract,
		       version.image_capability_contract, version.video_capability_contract,
		       profile.profile_key, profile_version.version
		FROM commerce_workflow_template_versions version
		JOIN commerce_workflow_templates template ON template.id = version.template_id
		JOIN video_production_profile_versions profile_version
		  ON profile_version.id = version.video_production_profile_version_id
		JOIN video_production_profiles profile ON profile.id = profile_version.profile_id
		WHERE template.template_key = $2
		  AND template.status = 'active'
		  AND version.status = 'published'
		  AND profile_version.lifecycle_state = 'published'
		  AND profile_version.implementation_state = 'available'
		  AND (template.organization_id IS NULL OR template.organization_id = $1)
		ORDER BY CASE WHEN template.organization_id = $1 THEN 1 ELSE 0 END DESC,
		         version.version DESC
		LIMIT 1
		FOR SHARE OF version, template, profile_version, profile
	`, organizationID, templateKey).Scan(
		&item.ID,
		&item.TemplateID,
		&item.TemplateKey,
		&item.Version,
		&item.ContentHash,
		&item.ConfigurationSnapshot,
		&item.PromptBindings,
		&item.AgentModelContracts,
		&item.LanguageContract,
		&item.ImageCapabilityContract,
		&item.VideoCapabilityContract,
		&item.VideoProfileKey,
		&item.VideoProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowTemplateVersion{}, ErrWorkflowTemplateUnavailable
	}
	return item, err
}

func (r *Repository) ResolvePublishedWorkflowTemplateVersion(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	versionID string,
) (WorkflowTemplateVersion, error) {
	var item WorkflowTemplateVersion
	err := tx.QueryRow(ctx, `
		SELECT version.id::text, template.id::text, template.template_key, version.version,
		       version.content_hash, version.configuration_snapshot, version.prompt_bindings,
		       version.agent_model_contracts, version.language_contract,
		       version.image_capability_contract, version.video_capability_contract,
		       profile.profile_key, profile_version.version
		FROM commerce_workflow_template_versions version
		JOIN commerce_workflow_templates template ON template.id = version.template_id
		JOIN video_production_profile_versions profile_version
		  ON profile_version.id = version.video_production_profile_version_id
		JOIN video_production_profiles profile ON profile.id = profile_version.profile_id
		WHERE version.id = $2
		  AND template.status = 'active'
		  AND version.status = 'published'
		  AND profile_version.lifecycle_state = 'published'
		  AND profile_version.implementation_state = 'available'
		  AND (template.organization_id IS NULL OR template.organization_id = $1)
		FOR SHARE OF version, template, profile_version, profile
	`, organizationID, versionID).Scan(
		&item.ID,
		&item.TemplateID,
		&item.TemplateKey,
		&item.Version,
		&item.ContentHash,
		&item.ConfigurationSnapshot,
		&item.PromptBindings,
		&item.AgentModelContracts,
		&item.LanguageContract,
		&item.ImageCapabilityContract,
		&item.VideoCapabilityContract,
		&item.VideoProfileKey,
		&item.VideoProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowTemplateVersion{}, ErrWorkflowTemplateUnavailable
	}
	return item, err
}

func (r *Repository) ResolveWorkflowTemplateVersionForRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	versionID string,
) (WorkflowTemplateVersion, error) {
	var item WorkflowTemplateVersion
	err := tx.QueryRow(ctx, `
		SELECT version.id::text, template.id::text, template.template_key, version.version,
		       version.content_hash, version.configuration_snapshot, version.prompt_bindings,
		       version.agent_model_contracts, version.language_contract,
		       version.image_capability_contract, version.video_capability_contract,
		       profile.profile_key, profile_version.version
		FROM commerce_workflow_template_versions version
		JOIN commerce_workflow_templates template ON template.id = version.template_id
		JOIN video_production_profile_versions profile_version
		  ON profile_version.id = version.video_production_profile_version_id
		JOIN video_production_profiles profile ON profile.id = profile_version.profile_id
		WHERE version.id = $2
		  AND template.status = 'active'
		  AND version.status IN ('published', 'retired')
		  AND profile_version.lifecycle_state = 'published'
		  AND profile_version.implementation_state = 'available'
		  AND (template.organization_id IS NULL OR template.organization_id = $1)
		FOR SHARE OF version, template, profile_version, profile
	`, organizationID, versionID).Scan(
		&item.ID,
		&item.TemplateID,
		&item.TemplateKey,
		&item.Version,
		&item.ContentHash,
		&item.ConfigurationSnapshot,
		&item.PromptBindings,
		&item.AgentModelContracts,
		&item.LanguageContract,
		&item.ImageCapabilityContract,
		&item.VideoCapabilityContract,
		&item.VideoProfileKey,
		&item.VideoProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowTemplateVersion{}, ErrWorkflowTemplateUnavailable
	}
	return item, err
}

func (r *Repository) InsertDraftProject(ctx context.Context, tx pgx.Tx, id string, params DraftProjectParams) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO projects(
			id, organization_id, workspace_id, name, description, project_kind, project_type, content_type,
			aspect_ratio, video_ratio, art_style, director_manual, visual_manual,
			image_model_profile_key, video_model_profile_key, script_model_profile_key,
			tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement,
			image_quality, timeline_timebase, fps_numerator, fps_denominator, settings, created_by,
			active_video_production_generation_id, video_production_generation_no,
			video_production_state, video_production_locked
		)
		VALUES (
			$1, $2, $3, $4, $5, 'commerce_video', 'commerce_video', NULL,
			$6, $7, '', '', '',
			'image_generation_default', 'video_generation_default', 'script_agent_default',
			'tts_generation_default', 'audio_transcription_default', $8, $9,
			$10, $11, $12, $13, $14, $15,
			NULL, NULL, 'unconfigured', false
		)
	`, id, params.OrganizationID, params.WorkspaceID, params.Name, params.Description,
		params.AspectRatio, params.VideoRatio, params.AudioStrategy,
		params.AudioRequirement, params.ImageQuality, params.TimelineTimebase,
		params.FPSNumerator, params.FPSDenominator, params.Settings, params.CreatedBy)
	return err
}

func (r *Repository) InsertProjectOwner(ctx context.Context, tx pgx.Tx, organizationID, projectID, userID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members(project_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, projectID, userID); err != nil {
		return err
	}
	var roleID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'project_owner' AND scope = 'project'
	`).Scan(&roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_project_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'project', $4, $3)
		ON CONFLICT DO NOTHING
	`, organizationID, roleID, userID, projectID)
	return err
}

func (r *Repository) InsertSetupSession(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	projectID string,
	template WorkflowTemplateVersion,
	params DraftProjectParams,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_setup_sessions(
			id, organization_id, workspace_id, project_id, workflow_template_version_id,
			idempotency_scope, client_request_id, request_hash, scope_type, state, step,
			input_snapshot, input_hash, created_by, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'project', 'draft', 'project_created', $9, $8, $10, $11)
	`, id, params.OrganizationID, params.WorkspaceID, projectID, template.ID,
		params.IdempotencyScope, params.ClientRequestID, params.RequestHash,
		params.InputSnapshot, params.CreatedBy, params.SetupExpiresAt)
	return err
}

func (r *Repository) CompleteDirectProjectSetup(
	ctx context.Context,
	tx pgx.Tx,
	setupSessionID string,
	projectID string,
	result InitialBindingResult,
) error {
	output, err := json.Marshal(map[string]any{
		"mode":                            "direct_video",
		"projectGenerationId":             result.ProjectGenerationID,
		"videoProductionBindingId":        result.VideoBindingID,
		"videoProductionBindingRevision":  result.VideoBindingRevision,
		"commerceWorkflowBindingId":       result.CommerceBindingID,
		"commerceWorkflowBindingRevision": result.CommerceBindingRevision,
	})
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'completed',
		    step = 'direct_video_ready',
		    input_snapshot = input_snapshot || jsonb_build_object('directVideoSetup', $3::jsonb),
		    completed_at = now(),
		    updated_at = now(),
		    revision = revision + 1
		WHERE id = $1 AND project_id = $2
		  AND state IN ('draft', 'uploading', 'failed', 'ready')
	`, setupSessionID, projectID, output)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeSetupRevisionConflict, Message: "带货视频项目初始化状态已变化"}
	}
	return nil
}

func (r *Repository) InsertPreparingVideoBinding(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	params InitialBindingParams,
	profileVersionID string,
	profileSnapshot []byte,
	profileSnapshotHash string,
	revision int64,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO project_video_production_bindings(
			id, project_id, profile_version_id, status, compatibility_policy, overrides,
			profile_snapshot, profile_snapshot_hash, revision, created_by
		)
		VALUES ($1, $2, $3, 'preparing', $4, $5, $6, $7, $8, NULLIF($9, '')::uuid)
	`, id, params.ProjectID, profileVersionID, params.CompatibilityPolicy, params.VideoOverrides,
		profileSnapshot, profileSnapshotHash, revision, params.CreatedBy)
	return err
}

func (r *Repository) InsertPreparingCommerceBinding(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	videoBindingID string,
	template WorkflowTemplateVersion,
	params InitialBindingParams,
	configurationHash string,
	videoProfileSnapshotHash string,
	revision int64,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO project_commerce_workflow_bindings(
			id, organization_id, project_id, template_version_id, video_production_binding_id,
			binding_revision, status, configuration_snapshot, configuration_hash,
			video_profile_snapshot_hash, model_routing_snapshot, capability_snapshot, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'preparing', $7, $8, $9, $10, $11, NULLIF($12, '')::uuid)
	`, id, params.OrganizationID, params.ProjectID, template.ID, videoBindingID, revision,
		params.ConfigurationSnapshot, configurationHash, videoProfileSnapshotHash,
		params.ModelRoutingSnapshot, params.CapabilitySnapshot, params.CreatedBy)
	return err
}

func (r *Repository) InsertPreparingProjectGeneration(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	videoBindingID string,
	commerceBindingID string,
	params InitialBindingParams,
	generationNo int64,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO project_video_production_generations(
			id, organization_id, project_id, binding_id, generation_no, status,
			commerce_workflow_binding_id, source_generation_id, rebuild_id
		)
		VALUES ($1, $2, $3, $4, $5, 'preparing', $6, NULLIF($7, '')::uuid, NULLIF($8, '')::uuid)
	`, id, params.OrganizationID, params.ProjectID, videoBindingID, generationNo, commerceBindingID,
		params.SourceGenerationID, params.RebuildID)
	return err
}

func (r *Repository) LockBindingPreparationProject(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
) (BindingPreparationState, error) {
	var item BindingPreparationState
	var activeGenerationID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT revision, video_production_state, video_production_locked,
		       active_video_production_generation_id::text
		FROM projects
		WHERE id = $1 AND organization_id = $2 AND project_kind = 'commerce_video'
		FOR UPDATE
	`, projectID, organizationID).Scan(
		&item.ProjectRevision,
		&item.ProductionState,
		&item.ProductionLocked,
		&activeGenerationID,
	)
	if activeGenerationID.Valid {
		item.ActiveGenerationID = &activeGenerationID.String
	}
	return item, err
}

func (r *Repository) NextBindingRevision(ctx context.Context, tx pgx.Tx, projectID string) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		SELECT GREATEST(
			COALESCE((SELECT max(revision) FROM project_video_production_bindings WHERE project_id = $1), 0),
			COALESCE((SELECT max(binding_revision) FROM project_commerce_workflow_bindings WHERE project_id = $1), 0)
		) + 1
	`, projectID).Scan(&revision)
	return revision, err
}

func (r *Repository) NextProjectGenerationNo(ctx context.Context, tx pgx.Tx, projectID string) (int64, error) {
	var generationNo int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(generation_no), 0) + 1
		FROM project_video_production_generations
		WHERE project_id = $1
	`, projectID).Scan(&generationNo)
	return generationNo, err
}

func (r *Repository) ActivateInitialBindings(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	result InitialBindingResult,
) error {
	var state string
	var activeGeneration pgtype.Text
	if err := tx.QueryRow(ctx, `
		SELECT video_production_state, active_video_production_generation_id::text
		FROM projects
		WHERE id = $1 AND project_kind = 'commerce_video'
		FOR UPDATE
	`, projectID).Scan(&state, &activeGeneration); err != nil {
		return err
	}
	if state != "unconfigured" || activeGeneration.Valid {
		return errors.New("commerce project already has an active production generation")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_bindings
		SET status = 'active'
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, result.VideoBindingID, projectID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return errors.New("preparing video production binding was not found")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_commerce_workflow_bindings
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, result.CommerceBindingID, projectID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return errors.New("preparing commerce workflow binding was not found")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, result.ProjectGenerationID, projectID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return errors.New("preparing project production generation was not found")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = $3,
		    video_production_state = 'storyboard_required',
		    video_production_locked = false,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id IS NULL
		  AND video_production_state = 'unconfigured'
	`, projectID, result.ProjectGenerationID, result.ProjectGenerationNo); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return errors.New("commerce project activation compare-and-swap failed")
	}
	return nil
}

func newID() string {
	return uuid.NewString()
}
