package videoproduction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type profileVersionScanner interface {
	Scan(...any) error
}

const profileVersionSelect = `
	SELECT version.id::text, profile.id::text, profile.profile_key, profile.name,
	       profile.strategy_family, profile.description, version.version,
	       version.lifecycle_state, version.implementation_state,
	       version.configuration, version.capability_requirements, version.prompt_contract,
	       version.input_contract_version, version.configuration_hash, version.prompt_contract_hash,
	       version.created_at, version.published_at, version.retired_at
	FROM video_production_profiles profile
	JOIN video_production_profile_versions version ON version.profile_id = profile.id
`

func NewIdentity() Identity {
	return Identity{
		ProjectID:    uuid.NewString(),
		BindingID:    uuid.NewString(),
		GenerationID: uuid.NewString(),
	}
}

func ResolveProfileVersion(ctx context.Context, db queryer, profileKey string, version *int, requireAvailable bool) (ProfileVersion, error) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		profileKey = ProfileSingleFrameI2V
	}
	query := profileVersionSelect + " WHERE profile.profile_key = $1"
	args := []any{profileKey}
	if version != nil {
		query += " AND version.version = $2"
		args = append(args, *version)
	} else {
		query += " ORDER BY version.version DESC LIMIT 1"
	}
	return scanResolvedProfileVersion(db.QueryRow(ctx, query, args...), requireAvailable)
}

func ResolveProfileVersionByID(ctx context.Context, db queryer, profileVersionID string, requireAvailable bool) (ProfileVersion, error) {
	parsedID, err := uuid.Parse(strings.TrimSpace(profileVersionID))
	if err != nil {
		return ProfileVersion{}, Error{Code: CodeProfileNotFound, Message: "视频生产方案版本不存在", Cause: err}
	}
	query := profileVersionSelect + " WHERE version.id = $1"
	return scanResolvedProfileVersion(db.QueryRow(ctx, query, parsedID), requireAvailable)
}

func scanResolvedProfileVersion(row profileVersionScanner, requireAvailable bool) (ProfileVersion, error) {
	var item ProfileVersion
	var publishedAt, retiredAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.ProfileID,
		&item.ProfileKey,
		&item.ProfileName,
		&item.StrategyFamily,
		&item.Description,
		&item.Version,
		&item.LifecycleState,
		&item.ImplementationState,
		&item.Configuration,
		&item.CapabilityRequirements,
		&item.PromptContract,
		&item.InputContractVersion,
		&item.ConfigurationHash,
		&item.PromptContractHash,
		&item.CreatedAt,
		&publishedAt,
		&retiredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfileVersion{}, Error{Code: CodeProfileNotFound, Message: "视频生产方案不存在", Cause: err}
	}
	if err != nil {
		return ProfileVersion{}, err
	}
	if publishedAt.Valid {
		item.PublishedAt = &publishedAt.Time
	}
	if retiredAt.Valid {
		item.RetiredAt = &retiredAt.Time
	}
	if requireAvailable && !item.Available() {
		return ProfileVersion{}, Error{
			Code:    CodeProfileUnavailable,
			Message: fmt.Sprintf("视频生产方案 %s v%d 暂不可用", item.ProfileName, item.Version),
		}
	}
	return item, nil
}

func ListProfiles(ctx context.Context, db queryer) ([]ProfileVersion, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (profile.id)
		       version.id::text, profile.id::text, profile.profile_key, profile.name,
		       profile.strategy_family, profile.description, version.version,
		       version.lifecycle_state, version.implementation_state,
		       version.configuration, version.capability_requirements, version.prompt_contract,
		       version.input_contract_version, version.configuration_hash, version.prompt_contract_hash,
		       version.created_at, version.published_at, version.retired_at
		FROM video_production_profiles profile
		JOIN video_production_profile_versions version ON version.profile_id = profile.id
		WHERE version.lifecycle_state = 'published'
		ORDER BY profile.id, version.version DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProfileVersion, 0, 4)
	for rows.Next() {
		var item ProfileVersion
		var publishedAt, retiredAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.ProfileKey,
			&item.ProfileName,
			&item.StrategyFamily,
			&item.Description,
			&item.Version,
			&item.LifecycleState,
			&item.ImplementationState,
			&item.Configuration,
			&item.CapabilityRequirements,
			&item.PromptContract,
			&item.InputContractVersion,
			&item.ConfigurationHash,
			&item.PromptContractHash,
			&item.CreatedAt,
			&publishedAt,
			&retiredAt,
		); err != nil {
			return nil, err
		}
		if publishedAt.Valid {
			item.PublishedAt = &publishedAt.Time
		}
		if retiredAt.Valid {
			item.RetiredAt = &retiredAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func NormalizeProductionConfiguration(input ProductionConfigurationSnapshot) (ProductionConfigurationSnapshot, error) {
	input.SchemaVersion = ProductionConfigurationSnapshotVersion
	input.ProjectType = strings.TrimSpace(input.ProjectType)
	input.ContentType = strings.TrimSpace(input.ContentType)
	input.AspectRatio = strings.TrimSpace(input.AspectRatio)
	input.VideoRatio = strings.TrimSpace(input.VideoRatio)
	input.ArtStyle = strings.TrimSpace(input.ArtStyle)
	input.DirectorManual = strings.TrimSpace(input.DirectorManual)
	input.VisualManual = strings.TrimSpace(input.VisualManual)
	input.ImageModelProfileKey = strings.TrimSpace(input.ImageModelProfileKey)
	input.VideoModelProfileKey = strings.TrimSpace(input.VideoModelProfileKey)
	input.ScriptModelProfileKey = strings.TrimSpace(input.ScriptModelProfileKey)
	input.TTSModelProfileKey = strings.TrimSpace(input.TTSModelProfileKey)
	input.ASRModelProfileKey = strings.TrimSpace(input.ASRModelProfileKey)
	input.AudioStrategy = strings.TrimSpace(input.AudioStrategy)
	input.AudioRequirement = strings.TrimSpace(input.AudioRequirement)
	input.ImageQuality = strings.TrimSpace(input.ImageQuality)
	if input.VideoRatio == "" {
		input.VideoRatio = "16:9"
	}
	if input.AspectRatio == "" {
		input.AspectRatio = input.VideoRatio
	}
	if input.ImageModelProfileKey == "" {
		input.ImageModelProfileKey = "image_generation_default"
	}
	if input.VideoModelProfileKey == "" {
		input.VideoModelProfileKey = "video_generation_default"
	}
	if input.ScriptModelProfileKey == "" {
		input.ScriptModelProfileKey = "script_agent_default"
	}
	if input.TTSModelProfileKey == "" {
		input.TTSModelProfileKey = "tts_generation_default"
	}
	if input.ASRModelProfileKey == "" {
		input.ASRModelProfileKey = "audio_transcription_default"
	}
	if input.AudioStrategy == "" {
		input.AudioStrategy = "native_av"
	}
	if input.AudioRequirement == "" {
		input.AudioRequirement = "preferred"
	}
	if input.ImageQuality == "" {
		input.ImageQuality = "standard"
	}
	if input.TimelineTimebase <= 0 {
		input.TimelineTimebase = 90_000
	}
	if input.FPSNumerator <= 0 {
		input.FPSNumerator = 24
	}
	if input.FPSDenominator <= 0 {
		input.FPSDenominator = 1
	}
	if len(input.Settings) == 0 || string(input.Settings) == "null" {
		input.Settings = json.RawMessage(`{}`)
	}
	var settings map[string]any
	if err := json.Unmarshal(input.Settings, &settings); err != nil {
		return ProductionConfigurationSnapshot{}, fmt.Errorf("decode production settings: %w", err)
	}
	normalizedSettings, err := json.Marshal(settings)
	if err != nil {
		return ProductionConfigurationSnapshot{}, fmt.Errorf("normalize production settings: %w", err)
	}
	input.Settings = normalizedSettings
	if input.ManualBindings == nil {
		input.ManualBindings = map[string]ManualBindingSnapshot{}
	}
	for kind, binding := range input.ManualBindings {
		binding.PromptVersionID = strings.TrimSpace(binding.PromptVersionID)
		binding.TemplateKey = strings.TrimSpace(binding.TemplateKey)
		binding.ContentHash = strings.TrimSpace(binding.ContentHash)
		input.ManualBindings[kind] = binding
	}
	return input, nil
}

func SetProductionManualVersion(ctx context.Context, db queryer, input ProductionConfigurationSnapshot, organizationID, kind, promptVersionID string) (ProductionConfigurationSnapshot, error) {
	if kind != "director" && kind != "visual" {
		return ProductionConfigurationSnapshot{}, fmt.Errorf("unsupported production manual kind %q", kind)
	}
	promptVersionID = strings.TrimSpace(promptVersionID)
	if promptVersionID == "" {
		return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildConflict, Message: "导演手册和视觉手册不能为空"}
	}
	var snapshot ManualBindingSnapshot
	var purpose, status, content string
	if err := db.QueryRow(ctx, `
		SELECT version.id::text, template.template_key, version.content_hash,
		       template.purpose, version.status, version.content
		FROM prompt_versions version
		JOIN prompt_templates template ON template.id = COALESCE(version.template_id, version.prompt_template_id)
		WHERE version.id = $1
		  AND template.status = 'active'
		  AND (template.organization_id IS NULL OR template.organization_id = NULLIF($2, '')::uuid)
	`, promptVersionID, organizationID).Scan(
		&snapshot.PromptVersionID,
		&snapshot.TemplateKey,
		&snapshot.ContentHash,
		&purpose,
		&status,
		&content,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildConflict, Message: "所选项目手册版本不可用"}
		}
		return ProductionConfigurationSnapshot{}, err
	}
	expectedPurpose := kind + "_manual"
	if status != "active" {
		return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildConflict, Message: "所选项目手册版本未启用"}
	}
	if purpose != expectedPurpose {
		return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildConflict, Message: "所选项目手册类型不匹配"}
	}
	if input.ManualBindings == nil {
		input.ManualBindings = map[string]ManualBindingSnapshot{}
	}
	input.ManualBindings[kind] = snapshot
	if kind == "director" {
		input.DirectorManual = content
	} else {
		input.VisualManual = content
	}
	return NormalizeProductionConfiguration(input)
}

func ProductionConfigurationHash(input ProductionConfigurationSnapshot) (ProductionConfigurationSnapshot, string, error) {
	normalized, err := NormalizeProductionConfiguration(input)
	if err != nil {
		return ProductionConfigurationSnapshot{}, "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ProductionConfigurationSnapshot{}, "", fmt.Errorf("marshal production configuration: %w", err)
	}
	return normalized, hashBytes(raw), nil
}

func DecodeProductionConfiguration(profileSnapshot json.RawMessage) (ProductionConfigurationSnapshot, error) {
	var envelope struct {
		SchemaVersion           int             `json:"schemaVersion"`
		ProductionConfiguration json.RawMessage `json:"productionConfiguration"`
	}
	if err := json.Unmarshal(profileSnapshot, &envelope); err != nil {
		return ProductionConfigurationSnapshot{}, fmt.Errorf("decode profile snapshot: %w", err)
	}
	if envelope.SchemaVersion != ProductionConfigurationSnapshotVersion || len(envelope.ProductionConfiguration) == 0 {
		return ProductionConfigurationSnapshot{}, Error{
			Code:    CodeConfigurationRebuildRequired,
			Message: "项目视频生产配置快照需要重新构建",
		}
	}
	var configuration ProductionConfigurationSnapshot
	if err := json.Unmarshal(envelope.ProductionConfiguration, &configuration); err != nil {
		return ProductionConfigurationSnapshot{}, fmt.Errorf("decode production configuration snapshot: %w", err)
	}
	if configuration.SchemaVersion != ProductionConfigurationSnapshotVersion {
		return ProductionConfigurationSnapshot{}, Error{
			Code:    CodeConfigurationRebuildRequired,
			Message: "项目视频生产配置快照版本不受支持",
		}
	}
	return NormalizeProductionConfiguration(configuration)
}

func LoadProductionConfiguration(ctx context.Context, db queryer, projectID string) (ProductionConfigurationSnapshot, error) {
	var item ProductionConfigurationSnapshot
	if err := db.QueryRow(ctx, `
		SELECT COALESCE(project_type, ''), COALESCE(content_type, ''), COALESCE(aspect_ratio, ''),
		       COALESCE(video_ratio, '16:9'), COALESCE(art_style, ''),
		       COALESCE(director_manual, ''), COALESCE(visual_manual, ''),
		       COALESCE(image_model_profile_key, 'image_generation_default'),
		       COALESCE(video_model_profile_key, 'video_generation_default'),
		       COALESCE(script_model_profile_key, 'script_agent_default'),
		       COALESCE(tts_model_profile_key, 'tts_generation_default'),
		       COALESCE(asr_model_profile_key, 'audio_transcription_default'),
		       COALESCE(audio_strategy, 'native_av'), COALESCE(audio_requirement, 'preferred'),
		       COALESCE(image_quality, 'standard'), timeline_timebase, fps_numerator, fps_denominator,
		       COALESCE(settings, '{}'::jsonb)
		FROM projects WHERE id = $1
	`, projectID).Scan(
		&item.ProjectType, &item.ContentType, &item.AspectRatio, &item.VideoRatio, &item.ArtStyle,
		&item.DirectorManual, &item.VisualManual, &item.ImageModelProfileKey, &item.VideoModelProfileKey,
		&item.ScriptModelProfileKey, &item.TTSModelProfileKey, &item.ASRModelProfileKey,
		&item.AudioStrategy, &item.AudioRequirement, &item.ImageQuality,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator, &item.Settings,
	); err != nil {
		return ProductionConfigurationSnapshot{}, err
	}
	manuals, err := loadManualBindingSnapshots(ctx, db, projectID)
	if err != nil {
		return ProductionConfigurationSnapshot{}, err
	}
	item.ManualBindings = manuals
	return NormalizeProductionConfiguration(item)
}

func CreateInitialBindingAndGeneration(ctx context.Context, tx pgx.Tx, params InitialBindingParams) (Binding, Generation, error) {
	if params.Identity.ProjectID == "" || params.Identity.BindingID == "" || params.Identity.GenerationID == "" {
		return Binding{}, Generation{}, errors.New("project, binding and generation ids are required")
	}
	if !params.ProfileVersion.Available() {
		return Binding{}, Generation{}, Error{Code: CodeProfileUnavailable, Message: "所选视频生产方案暂不可用"}
	}
	if params.CompatibilityPolicy == "" {
		params.CompatibilityPolicy = CompatibilityStrict
	}
	if err := validateCompatibilityPolicy(params.CompatibilityPolicy); err != nil {
		return Binding{}, Generation{}, err
	}
	if len(params.Overrides) == 0 {
		params.Overrides = json.RawMessage(`{}`)
	}
	snapshot, snapshotHash, err := buildProfileSnapshot(ctx, tx, params)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	var binding Binding
	err = tx.QueryRow(ctx, `
		INSERT INTO project_video_production_bindings(
			id, project_id, profile_version_id, status, compatibility_policy, overrides,
			profile_snapshot, profile_snapshot_hash, revision, created_by
		)
		VALUES ($1, $2, $3, 'active', $4, $5, $6, $7, 1, NULLIF($8, '')::uuid)
		RETURNING id::text, project_id::text, profile_version_id::text, status,
		          compatibility_policy, overrides, profile_snapshot, profile_snapshot_hash,
		          revision, created_at, superseded_at
	`, params.Identity.BindingID, params.Identity.ProjectID, params.ProfileVersion.ID,
		params.CompatibilityPolicy, params.Overrides, snapshot, snapshotHash, params.CreatedBy).Scan(
		&binding.ID,
		&binding.ProjectID,
		&binding.ProfileVersionID,
		&binding.Status,
		&binding.CompatibilityPolicy,
		&binding.Overrides,
		&binding.ProfileSnapshot,
		&binding.ProfileSnapshotHash,
		&binding.Revision,
		&binding.CreatedAt,
		&binding.SupersededAt,
	)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	binding.ProfileKey = params.ProfileVersion.ProfileKey
	binding.ProfileName = params.ProfileVersion.ProfileName
	binding.ProfileVersion = params.ProfileVersion.Version
	binding.LifecycleState = params.ProfileVersion.LifecycleState
	binding.ImplementationState = params.ProfileVersion.ImplementationState

	var generation Generation
	err = tx.QueryRow(ctx, `
		INSERT INTO project_video_production_generations(
			id, organization_id, project_id, binding_id, generation_no, status, activated_at
		)
		VALUES ($1, $2, $3, $4, 1, 'active', now())
		RETURNING id::text, organization_id::text, project_id::text, binding_id::text,
		          generation_no, status, source_generation_id::text, rebuild_id::text,
		          created_at, activated_at, superseded_at
	`, params.Identity.GenerationID, params.OrganizationID, params.Identity.ProjectID, params.Identity.BindingID).Scan(
		&generation.ID,
		&generation.OrganizationID,
		&generation.ProjectID,
		&generation.BindingID,
		&generation.GenerationNo,
		&generation.Status,
		&generation.SourceGenerationID,
		&generation.RebuildID,
		&generation.CreatedAt,
		&generation.ActivatedAt,
		&generation.SupersededAt,
	)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = 1,
		    video_production_state = 'storyboard_required',
		    video_production_locked = false,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $2
	`, params.Identity.ProjectID, params.Identity.GenerationID)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if command.RowsAffected() != 1 {
		return Binding{}, Generation{}, Error{Code: CodeGenerationMismatch, Message: "项目视频生产代初始化失败"}
	}
	return binding, generation, nil
}

func SwitchRebuildGeneration(ctx context.Context, tx pgx.Tx, params RebuildSwitchParams) (Binding, Generation, error) {
	if params.RebuildID == "" || params.ProjectID == "" || params.OrganizationID == "" {
		return Binding{}, Generation{}, errors.New("rebuild, project, and organization ids are required")
	}
	if !params.Target.Available() {
		return Binding{}, Generation{}, Error{Code: CodeProfileUnavailable, Message: "目标视频生产方案暂不可用"}
	}
	current, err := scanContext(tx.QueryRow(ctx, activeContextSQL+" FOR UPDATE OF project", params.ProjectID))
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if !current.Locked || current.State != "rebuilding" {
		return Binding{}, Generation{}, Error{Code: CodeRebuildConflict, Message: "项目未处于视频生产方案重建状态"}
	}
	if current.Generation.ID != params.Source.Generation.ID ||
		current.Binding.ID != params.Source.Binding.ID ||
		current.Binding.Revision != params.Source.Binding.Revision {
		return Binding{}, Generation{}, Error{Code: CodeGenerationMismatch, Message: "重建来源视频生产代已变化"}
	}
	configuration, err := applyProductionManualBindings(ctx, tx, params.OrganizationID, params.ProjectID, params.CreatedBy, params.Configuration)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	identity := Identity{ProjectID: params.ProjectID, BindingID: uuid.NewString(), GenerationID: uuid.NewString()}
	snapshot, snapshotHash, err := buildProfileSnapshot(ctx, tx, InitialBindingParams{
		Identity:            identity,
		OrganizationID:      params.OrganizationID,
		CreatedBy:           params.CreatedBy,
		ProfileVersion:      params.Target,
		CompatibilityPolicy: current.Binding.CompatibilityPolicy,
		Overrides:           current.Binding.Overrides,
		Configuration:       configuration,
	})
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_bindings
		SET status = 'superseded', superseded_by_rebuild_id = $2, superseded_at = now()
		WHERE id = $1 AND project_id = $3 AND status = 'active'
	`, current.Binding.ID, params.RebuildID, params.ProjectID); err != nil {
		return Binding{}, Generation{}, err
	}
	var binding Binding
	err = tx.QueryRow(ctx, `
		INSERT INTO project_video_production_bindings(
			id, project_id, profile_version_id, status, compatibility_policy, overrides,
			profile_snapshot, profile_snapshot_hash, revision, created_by
		)
		VALUES ($1, $2, $3, 'active', $4, $5, $6, $7, $8, NULLIF($9, '')::uuid)
		RETURNING id::text, project_id::text, profile_version_id::text, status,
		          compatibility_policy, overrides, profile_snapshot, profile_snapshot_hash,
		          revision, created_at, superseded_at
	`, identity.BindingID, params.ProjectID, params.Target.ID, current.Binding.CompatibilityPolicy,
		current.Binding.Overrides, snapshot, snapshotHash, current.Binding.Revision+1, params.CreatedBy).Scan(
		&binding.ID,
		&binding.ProjectID,
		&binding.ProfileVersionID,
		&binding.Status,
		&binding.CompatibilityPolicy,
		&binding.Overrides,
		&binding.ProfileSnapshot,
		&binding.ProfileSnapshotHash,
		&binding.Revision,
		&binding.CreatedAt,
		&binding.SupersededAt,
	)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	binding.ProfileKey = params.Target.ProfileKey
	binding.ProfileName = params.Target.ProfileName
	binding.ProfileVersion = params.Target.Version
	binding.LifecycleState = params.Target.LifecycleState
	binding.ImplementationState = params.Target.ImplementationState

	var generation Generation
	err = tx.QueryRow(ctx, `
		INSERT INTO project_video_production_generations(
			id, organization_id, project_id, binding_id, generation_no, status,
			source_generation_id, rebuild_id
		)
		VALUES ($1, $2, $3, $4, $5, 'preparing', $6, $7)
		RETURNING id::text, organization_id::text, project_id::text, binding_id::text,
		          generation_no, status, source_generation_id::text, rebuild_id::text,
		          created_at, activated_at, superseded_at
	`, identity.GenerationID, params.OrganizationID, params.ProjectID, identity.BindingID,
		current.Generation.GenerationNo+1, current.Generation.ID, params.RebuildID).Scan(
		&generation.ID,
		&generation.OrganizationID,
		&generation.ProjectID,
		&generation.BindingID,
		&generation.GenerationNo,
		&generation.Status,
		&generation.SourceGenerationID,
		&generation.RebuildID,
		&generation.CreatedAt,
		&generation.ActivatedAt,
		&generation.SupersededAt,
	)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if err := archiveGenerationProductionData(ctx, tx, params.ProjectID, current.Generation.ID); err != nil {
		return Binding{}, Generation{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'active'
	`, current.Generation.ID, params.ProjectID); err != nil {
		return Binding{}, Generation{}, err
	}
	if err := tx.QueryRow(ctx, `
		UPDATE project_video_production_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
		RETURNING status, activated_at
	`, generation.ID, params.ProjectID).Scan(&generation.Status, &generation.ActivatedAt); err != nil {
		return Binding{}, Generation{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = $3,
		    video_production_state = 'storyboard_required',
		    project_type = $5,
		    content_type = $6,
		    aspect_ratio = $7,
		    video_ratio = $8,
		    art_style = $9,
		    director_manual = $10,
		    visual_manual = $11,
		    image_model_profile_key = $12,
		    video_model_profile_key = $13,
		    script_model_profile_key = $14,
		    tts_model_profile_key = $15,
		    asr_model_profile_key = $16,
		    audio_configuration_revision = audio_configuration_revision + CASE
		        WHEN audio_strategy IS DISTINCT FROM $17
		          OR audio_requirement IS DISTINCT FROM $18
		          OR tts_model_profile_key IS DISTINCT FROM $15
		          OR asr_model_profile_key IS DISTINCT FROM $16 THEN 1 ELSE 0 END,
		    audio_strategy = $17,
		    audio_requirement = $18,
		    image_quality = $19,
		    timeline_timebase = $20,
		    fps_numerator = $21,
		    fps_denominator = $22,
		    settings = $23,
		    active_final_video_version_id = NULL,
		    active_audio_mix_version_id = NULL,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $4
		  AND video_production_locked = true
		  AND active_video_production_rebuild_id = $24
	`, params.ProjectID, generation.ID, generation.GenerationNo, current.Generation.ID,
		configuration.ProjectType, configuration.ContentType, configuration.AspectRatio,
		configuration.VideoRatio, configuration.ArtStyle, configuration.DirectorManual,
		configuration.VisualManual, configuration.ImageModelProfileKey, configuration.VideoModelProfileKey,
		configuration.ScriptModelProfileKey, configuration.TTSModelProfileKey, configuration.ASRModelProfileKey,
		configuration.AudioStrategy, configuration.AudioRequirement, configuration.ImageQuality,
		configuration.TimelineTimebase, configuration.FPSNumerator, configuration.FPSDenominator, configuration.Settings,
		params.RebuildID)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if command.RowsAffected() != 1 {
		return Binding{}, Generation{}, Error{Code: CodeGenerationMismatch, Message: "重建切换视频生产代失败"}
	}
	command, err = tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET target_binding_id = $2, target_generation_id = $3,
		    status = 'running', failure_code = NULL, failure_message = NULL
		WHERE id = $1 AND project_id = $4 AND status = 'running'
	`, params.RebuildID, binding.ID, generation.ID, params.ProjectID)
	if err != nil {
		return Binding{}, Generation{}, err
	}
	if command.RowsAffected() != 1 {
		return Binding{}, Generation{}, Error{Code: CodeGenerationMismatch, Message: "重建状态已被其他执行修改"}
	}
	return binding, generation, nil
}

func archiveGenerationProductionData(ctx context.Context, tx pgx.Tx, projectID, generationID string) error {
	statements := []string{
		`UPDATE storyboard_shot_state_versions SET status = 'stale' WHERE project_id = $1 AND production_generation_id = $2 AND status IN ('draft', 'approved')`,
		`UPDATE storyboard_shot_transitions SET status = 'stale', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status = 'active'`,
		`UPDATE shot_reference_packs SET status = 'archived' WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
		`UPDATE shot_visual_anchors SET status = 'archived', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
		`UPDATE prompt_context_plans SET status = 'archived', archived_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status = 'active'`,
		`UPDATE video_prompt_plans SET status = 'archived', archived_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status NOT IN ('archived', 'stale')`,
		`UPDATE video_native_audio_contracts SET status = 'archived', archived_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status = 'active'`,
		`UPDATE video_render_segments SET status = 'stale', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status IN ('planned', 'queued', 'running')`,
		`UPDATE video_render_plans SET status = 'archived', active = false, updated_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
		`UPDATE shot_asset_requirements SET stale_state = 'needs_regeneration', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2`,
		`UPDATE storyboard_shots SET stale_state = 'needs_regeneration', active_video_render_plan_id = NULL, updated_at = now() WHERE project_id = $1 AND production_generation_id = $2`,
		`UPDATE storyboard_plans SET status = 'archived', active = false, stale_state = 'needs_regeneration' WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
		`UPDATE timeline_clips SET stale_state = 'needs_regeneration', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2`,
		`UPDATE project_timelines SET status = 'archived', stale_state = 'needs_regeneration', updated_at = now() WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
		`UPDATE final_video_versions SET status = 'archived' WHERE project_id = $1 AND production_generation_id = $2 AND status <> 'archived'`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, projectID, generationID); err != nil {
			return err
		}
	}
	return nil
}

// ArchiveGenerationProductionData invalidates every mutable production result
// owned by a superseded project generation. Domain-specific rebuild services use
// this inside the same transaction that switches the active generation.
func ArchiveGenerationProductionData(ctx context.Context, tx pgx.Tx, projectID, generationID string) error {
	return archiveGenerationProductionData(ctx, tx, projectID, generationID)
}

// ResetActiveGeneration archives the current production generation and switches the
// project to an empty generation under the same immutable profile binding.
func ResetActiveGeneration(ctx context.Context, tx pgx.Tx, projectID string) (string, Generation, error) {
	if strings.TrimSpace(projectID) == "" {
		return "", Generation{}, errors.New("project id is required")
	}
	current, err := scanContext(tx.QueryRow(ctx, activeContextSQL+" FOR UPDATE OF project", projectID))
	if err != nil {
		return "", Generation{}, err
	}
	if current.Locked || current.State == "rebuilding" {
		return "", Generation{}, Error{Code: CodeRebuildConflict, Message: "项目视频生产配置正在变更，暂时不能清空生产内容"}
	}
	newGenerationID := uuid.NewString()
	var generation Generation
	err = tx.QueryRow(ctx, `
		INSERT INTO project_video_production_generations(
			id, organization_id, project_id, binding_id, generation_no, status, source_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'preparing', $6)
		RETURNING id::text, organization_id::text, project_id::text, binding_id::text,
		          generation_no, status, source_generation_id::text, rebuild_id::text,
		          created_at, activated_at, superseded_at
	`, newGenerationID, current.Generation.OrganizationID, projectID, current.Binding.ID,
		current.Generation.GenerationNo+1, current.Generation.ID).Scan(
		&generation.ID,
		&generation.OrganizationID,
		&generation.ProjectID,
		&generation.BindingID,
		&generation.GenerationNo,
		&generation.Status,
		&generation.SourceGenerationID,
		&generation.RebuildID,
		&generation.CreatedAt,
		&generation.ActivatedAt,
		&generation.SupersededAt,
	)
	if err != nil {
		return "", Generation{}, err
	}
	if err := archiveGenerationProductionData(ctx, tx, projectID, current.Generation.ID); err != nil {
		return "", Generation{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'active'
	`, current.Generation.ID, projectID); err != nil {
		return "", Generation{}, err
	}
	if err := tx.QueryRow(ctx, `
		UPDATE project_video_production_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
		RETURNING status, activated_at
	`, generation.ID, projectID).Scan(&generation.Status, &generation.ActivatedAt); err != nil {
		return "", Generation{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = $3,
		    video_production_state = 'storyboard_required',
		    active_final_video_version_id = NULL,
		    active_audio_mix_version_id = NULL,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND active_video_production_generation_id = $4
		  AND video_production_locked = false
	`, projectID, generation.ID, generation.GenerationNo, current.Generation.ID)
	if err != nil {
		return "", Generation{}, err
	}
	if tag.RowsAffected() != 1 {
		return "", Generation{}, Error{Code: CodeGenerationMismatch, Message: "清空生产内容时项目生产代已变化"}
	}
	return current.Generation.ID, generation, nil
}

func buildProfileSnapshot(ctx context.Context, tx pgx.Tx, params InitialBindingParams) (json.RawMessage, string, error) {
	prompts, err := resolvePromptContractVersions(ctx, tx, params.ProfileVersion.PromptContract)
	if err != nil {
		return nil, "", err
	}
	productionConfiguration, _, err := ProductionConfigurationHash(params.Configuration)
	if err != nil {
		return nil, "", err
	}
	var profileConfiguration any
	var capabilityRequirements any
	var promptContract any
	var overrides any
	for raw, target := range map[string]*any{
		string(params.ProfileVersion.Configuration):          &profileConfiguration,
		string(params.ProfileVersion.CapabilityRequirements): &capabilityRequirements,
		string(params.ProfileVersion.PromptContract):         &promptContract,
		string(params.Overrides):                             &overrides,
	} {
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			return nil, "", fmt.Errorf("decode profile snapshot component: %w", err)
		}
	}
	snapshot := map[string]any{
		"schemaVersion":           ProductionConfigurationSnapshotVersion,
		"profileKey":              params.ProfileVersion.ProfileKey,
		"profileName":             params.ProfileVersion.ProfileName,
		"profileVersion":          params.ProfileVersion.Version,
		"profileVersionId":        params.ProfileVersion.ID,
		"configuration":           profileConfiguration,
		"capabilityRequirements":  capabilityRequirements,
		"promptContract":          promptContract,
		"promptVersions":          prompts,
		"inputContractVersion":    params.ProfileVersion.InputContractVersion,
		"configurationHash":       params.ProfileVersion.ConfigurationHash,
		"promptContractHash":      params.ProfileVersion.PromptContractHash,
		"compatibilityPolicy":     params.CompatibilityPolicy,
		"productionConfiguration": productionConfiguration,
		"overrides":               overrides,
	}
	raw, err := canonicalJSON(snapshot)
	if err != nil {
		return nil, "", err
	}
	return raw, hashBytes(raw), nil
}

// BuildProfileSnapshot compiles the immutable runtime snapshot without writing
// a binding. Callers that coordinate multiple aligned bindings can persist the
// returned snapshot in their own transaction.
func BuildProfileSnapshot(ctx context.Context, tx pgx.Tx, params InitialBindingParams) (json.RawMessage, string, error) {
	return buildProfileSnapshot(ctx, tx, params)
}

func loadManualBindingSnapshots(ctx context.Context, db queryer, projectID string) (map[string]ManualBindingSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT binding.manual_kind, binding.prompt_version_id::text,
		       template.template_key, version.content_hash
		FROM project_manual_bindings binding
		JOIN prompt_versions version ON version.id = binding.prompt_version_id
		JOIN prompt_templates template ON template.id = COALESCE(version.template_id, version.prompt_template_id)
		WHERE binding.project_id = $1 AND binding.status = 'active' AND version.status = 'active'
		ORDER BY binding.manual_kind
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ManualBindingSnapshot, 2)
	for rows.Next() {
		var kind string
		var item ManualBindingSnapshot
		if err := rows.Scan(&kind, &item.PromptVersionID, &item.TemplateKey, &item.ContentHash); err != nil {
			return nil, err
		}
		result[kind] = item
	}
	return result, rows.Err()
}

func applyProductionManualBindings(ctx context.Context, tx pgx.Tx, organizationID, projectID, createdBy string, input ProductionConfigurationSnapshot) (ProductionConfigurationSnapshot, error) {
	configuration, err := NormalizeProductionConfiguration(input)
	if err != nil {
		return ProductionConfigurationSnapshot{}, err
	}
	for _, kind := range []string{"director", "visual"} {
		target, ok := configuration.ManualBindings[kind]
		if !ok || target.PromptVersionID == "" {
			return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildConflict, Message: "目标视频生产配置缺少项目手册版本"}
		}
		configuration, err = SetProductionManualVersion(ctx, tx, configuration, organizationID, kind, target.PromptVersionID)
		if err != nil {
			return ProductionConfigurationSnapshot{}, err
		}
		resolved := configuration.ManualBindings[kind]
		if resolved.TemplateKey != target.TemplateKey || resolved.ContentHash != target.ContentHash {
			return ProductionConfigurationSnapshot{}, Error{Code: CodeRebuildImpactStale, Message: "项目手册版本已变化，请重新确认重建影响"}
		}
		var activeVersionID string
		err = tx.QueryRow(ctx, `
			SELECT prompt_version_id::text
			FROM project_manual_bindings
			WHERE project_id = $1 AND manual_kind = $2 AND status = 'active'
			FOR UPDATE
		`, projectID, kind).Scan(&activeVersionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ProductionConfigurationSnapshot{}, err
		}
		if activeVersionID == resolved.PromptVersionID {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE project_manual_bindings
			SET status = 'disabled'
			WHERE project_id = $1 AND manual_kind = $2 AND status = 'active'
		`, projectID, kind); err != nil {
			return ProductionConfigurationSnapshot{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_manual_bindings(
				organization_id, project_id, manual_kind, prompt_version_id, status, created_by
			)
			VALUES ($1, $2, $3, $4, 'active', NULLIF($5, '')::uuid)
		`, organizationID, projectID, kind, resolved.PromptVersionID, createdBy); err != nil {
			return ProductionConfigurationSnapshot{}, err
		}
	}
	return configuration, nil
}

func resolvePromptContractVersions(ctx context.Context, tx pgx.Tx, raw json.RawMessage) (map[string]PromptVersionSnapshot, error) {
	var contract map[string]string
	if err := json.Unmarshal(raw, &contract); err != nil {
		return nil, fmt.Errorf("decode video production prompt contract: %w", err)
	}
	if len(contract) == 0 {
		return nil, Error{Code: CodePromptContractIncomplete, Message: "视频生产方案缺少 Prompt Contract"}
	}
	roles := make([]string, 0, len(contract))
	for role := range contract {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	result := make(map[string]PromptVersionSnapshot, len(roles))
	for _, role := range roles {
		templateKey := strings.TrimSpace(contract[role])
		var item PromptVersionSnapshot
		err := tx.QueryRow(ctx, `
			SELECT template.template_key, version.id::text, version.content_hash
			FROM prompt_templates template
			JOIN prompt_versions version ON version.template_id = template.id
			WHERE template.organization_id IS NULL
			  AND template.template_key = $1
			  AND template.status = 'active'
			  AND version.status = 'active'
			LIMIT 1
		`, templateKey).Scan(&item.TemplateKey, &item.PromptVersionID, &item.ContentHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, Error{
				Code:    CodePromptContractIncomplete,
				Message: fmt.Sprintf("视频生产方案缺少已启用 Prompt：%s", templateKey),
				Cause:   err,
			}
		}
		if err != nil {
			return nil, err
		}
		result[role] = item
	}
	return result, nil
}

func LoadActiveContext(ctx context.Context, db queryer, projectID string) (Context, error) {
	return scanContext(db.QueryRow(ctx, activeContextSQL, projectID))
}

func LoadActiveContextForOrganization(ctx context.Context, db queryer, organizationID, projectID string) (Context, error) {
	return scanContext(db.QueryRow(ctx, activeContextForOrganizationSQL, projectID, organizationID))
}

func LoadWritableContextTx(ctx context.Context, tx pgx.Tx, projectID string, allowLocked bool) (Context, error) {
	item, err := scanContext(tx.QueryRow(ctx, activeContextSQL+" FOR UPDATE OF project", projectID))
	if err != nil {
		return Context{}, err
	}
	if item.Locked && !allowLocked {
		return Context{}, Error{Code: CodeProjectLocked, Message: "项目视频生产配置正在重建，请稍后重试", Retryable: true}
	}
	if item.Generation.Status != "active" || item.Binding.Status != "active" {
		return Context{}, Error{Code: CodeGenerationMismatch, Message: "当前任务所属视频生产代已失效"}
	}
	return item, nil
}

func AssertGenerationWritableTx(ctx context.Context, tx pgx.Tx, projectID, generationID string, allowLocked bool) (Context, error) {
	item, err := LoadWritableContextTx(ctx, tx, projectID, allowLocked)
	if err != nil {
		return Context{}, err
	}
	if item.Generation.ID != generationID {
		return Context{}, Error{Code: CodeGenerationMismatch, Message: "当前任务所属视频生产代已失效"}
	}
	return item, nil
}

func AssertWritableTx(ctx context.Context, tx pgx.Tx, projectID, generationID, bindingID string, bindingRevision int64, allowLocked ...bool) (Context, error) {
	lockedWriteAllowed := len(allowLocked) > 0 && allowLocked[0]
	item, err := AssertGenerationWritableTx(ctx, tx, projectID, generationID, lockedWriteAllowed)
	if err != nil {
		return Context{}, err
	}
	if item.Binding.ID != bindingID || item.Binding.Revision != bindingRevision {
		return Context{}, Error{Code: CodeGenerationMismatch, Message: "当前任务所属视频生产代已失效"}
	}
	return item, nil
}

const activeContextSQL = `
	SELECT binding.id::text, binding.project_id::text, binding.profile_version_id::text,
	       profile.profile_key, profile.name, version.version, version.lifecycle_state,
	       version.implementation_state, binding.status, binding.compatibility_policy,
	       binding.overrides, binding.profile_snapshot, binding.profile_snapshot_hash,
	       binding.revision, binding.created_at, binding.superseded_at,
	       generation.id::text, generation.organization_id::text, generation.project_id::text,
	       generation.binding_id::text, generation.generation_no, generation.status,
	       generation.source_generation_id::text, generation.rebuild_id::text,
	       generation.created_at, generation.activated_at, generation.superseded_at,
	       project.video_production_locked, project.video_production_state
	FROM projects project
	JOIN project_video_production_generations generation
	  ON generation.id = project.active_video_production_generation_id
	 AND generation.project_id = project.id
	JOIN project_video_production_bindings binding
	  ON binding.id = generation.binding_id
	 AND binding.project_id = project.id
	JOIN video_production_profile_versions version ON version.id = binding.profile_version_id
	JOIN video_production_profiles profile ON profile.id = version.profile_id
	WHERE project.id = $1`

const activeContextForOrganizationSQL = activeContextSQL + `
	  AND project.organization_id = $2`

func scanContext(row pgx.Row) (Context, error) {
	var item Context
	var bindingSupersededAt, generationActivatedAt, generationSupersededAt sql.NullTime
	var sourceGenerationID, rebuildID sql.NullString
	err := row.Scan(
		&item.Binding.ID,
		&item.Binding.ProjectID,
		&item.Binding.ProfileVersionID,
		&item.Binding.ProfileKey,
		&item.Binding.ProfileName,
		&item.Binding.ProfileVersion,
		&item.Binding.LifecycleState,
		&item.Binding.ImplementationState,
		&item.Binding.Status,
		&item.Binding.CompatibilityPolicy,
		&item.Binding.Overrides,
		&item.Binding.ProfileSnapshot,
		&item.Binding.ProfileSnapshotHash,
		&item.Binding.Revision,
		&item.Binding.CreatedAt,
		&bindingSupersededAt,
		&item.Generation.ID,
		&item.Generation.OrganizationID,
		&item.Generation.ProjectID,
		&item.Generation.BindingID,
		&item.Generation.GenerationNo,
		&item.Generation.Status,
		&sourceGenerationID,
		&rebuildID,
		&item.Generation.CreatedAt,
		&generationActivatedAt,
		&generationSupersededAt,
		&item.Locked,
		&item.State,
	)
	if err != nil {
		return Context{}, err
	}
	if bindingSupersededAt.Valid {
		item.Binding.SupersededAt = &bindingSupersededAt.Time
	}
	if sourceGenerationID.Valid {
		item.Generation.SourceGenerationID = &sourceGenerationID.String
	}
	if rebuildID.Valid {
		item.Generation.RebuildID = &rebuildID.String
	}
	if generationActivatedAt.Valid {
		item.Generation.ActivatedAt = &generationActivatedAt.Time
	}
	if generationSupersededAt.Valid {
		item.Generation.SupersededAt = &generationSupersededAt.Time
	}
	return item, nil
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
