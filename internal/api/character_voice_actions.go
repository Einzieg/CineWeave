package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type characterVoiceCreateActionInput struct {
	CanonicalAssetID     string          `json:"canonicalAssetId,omitempty"`
	CharacterName        string          `json:"characterName"`
	DisplayName          string          `json:"displayName"`
	Language             string          `json:"language,omitempty"`
	ModelProfileKey      string          `json:"modelProfileKey,omitempty"`
	ProviderModelID      string          `json:"providerModelId,omitempty"`
	VoiceKey             string          `json:"voiceKey"`
	Instructions         string          `json:"instructions,omitempty"`
	ReferenceArtifactID  string          `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID string          `json:"referenceMediaFileId,omitempty"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	IsDefault            *bool           `json:"isDefault,omitempty"`
}

type characterVoicePatch struct {
	CanonicalAssetID     *string          `json:"canonicalAssetId,omitempty"`
	CharacterName        *string          `json:"characterName,omitempty"`
	DisplayName          *string          `json:"displayName,omitempty"`
	Language             *string          `json:"language,omitempty"`
	ModelProfileKey      *string          `json:"modelProfileKey,omitempty"`
	ProviderModelID      *string          `json:"providerModelId,omitempty"`
	VoiceKey             *string          `json:"voiceKey,omitempty"`
	Instructions         *string          `json:"instructions,omitempty"`
	ReferenceArtifactID  *string          `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID *string          `json:"referenceMediaFileId,omitempty"`
	Parameters           *json.RawMessage `json:"parameters,omitempty"`
	IsDefault            *bool            `json:"isDefault,omitempty"`
}

type characterVoiceUpdateActionInput struct {
	VoiceID          string              `json:"voiceId"`
	ExpectedRevision int64               `json:"expectedRevision"`
	Patch            characterVoicePatch `json:"patch"`
}

type characterVoiceDeleteActionInput struct {
	VoiceID          string `json:"voiceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type resolvedCharacterVoice struct {
	CanonicalAssetID     string
	CharacterName        string
	DisplayName          string
	Language             string
	ModelProfileKey      string
	ProviderModelID      string
	VoiceKey             string
	Instructions         string
	ReferenceArtifactID  string
	ReferenceMediaFileID string
	Parameters           json.RawMessage
	IsDefault            bool
}

func decodeCharacterVoiceCreateActionInput(raw json.RawMessage) (characterVoiceCreateActionInput, error) {
	var input characterVoiceCreateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.CanonicalAssetID = strings.TrimSpace(input.CanonicalAssetID)
	input.CharacterName = strings.TrimSpace(input.CharacterName)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Language = firstNonEmpty(strings.TrimSpace(input.Language), "zh-CN")
	input.ModelProfileKey = firstNonEmpty(strings.TrimSpace(input.ModelProfileKey), "tts_generation_default")
	input.ProviderModelID = strings.TrimSpace(input.ProviderModelID)
	input.VoiceKey = strings.TrimSpace(input.VoiceKey)
	input.Instructions = strings.TrimSpace(input.Instructions)
	input.ReferenceArtifactID = strings.TrimSpace(input.ReferenceArtifactID)
	input.ReferenceMediaFileID = strings.TrimSpace(input.ReferenceMediaFileID)
	if input.CharacterName == "" || input.DisplayName == "" || input.VoiceKey == "" {
		return input, controlValidationError("characterName、displayName 和 voiceKey 为必填项")
	}
	parameters, err := normalizeCharacterVoiceParameters(input.Parameters)
	if err != nil {
		return input, err
	}
	input.Parameters = parameters
	if err := validateCharacterVoiceIDs("", input.CanonicalAssetID, input.ProviderModelID, input.ReferenceArtifactID, input.ReferenceMediaFileID); err != nil {
		return input, err
	}
	return input, nil
}

func decodeCharacterVoiceUpdateActionInput(raw json.RawMessage) (characterVoiceUpdateActionInput, error) {
	var input characterVoiceUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	if input.VoiceID == "" || input.ExpectedRevision < 1 {
		return input, controlValidationError("voiceId 和 expectedRevision 为必填项")
	}
	if err := validateCharacterVoiceIDs(input.VoiceID); err != nil {
		return input, err
	}
	if !characterVoicePatchHasChanges(input.Patch) {
		return input, controlValidationError("角色声音补丁不能为空")
	}
	if input.Patch.Parameters != nil {
		parameters, err := normalizeCharacterVoiceParameters(*input.Patch.Parameters)
		if err != nil {
			return input, err
		}
		input.Patch.Parameters = &parameters
	}
	if err := validateCharacterVoiceIDs("", trimmedStringPtr(input.Patch.CanonicalAssetID), trimmedStringPtr(input.Patch.ProviderModelID), trimmedStringPtr(input.Patch.ReferenceArtifactID), trimmedStringPtr(input.Patch.ReferenceMediaFileID)); err != nil {
		return input, err
	}
	return input, nil
}

func decodeCharacterVoiceDeleteActionInput(raw json.RawMessage) (characterVoiceDeleteActionInput, error) {
	var input characterVoiceDeleteActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, err
	}
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	if input.VoiceID == "" || input.ExpectedRevision < 1 {
		return input, controlValidationError("voiceId 和 expectedRevision 为必填项")
	}
	if err := validateCharacterVoiceIDs(input.VoiceID); err != nil {
		return input, err
	}
	return input, nil
}

func characterVoicePatchHasChanges(patch characterVoicePatch) bool {
	return patch.CanonicalAssetID != nil || patch.CharacterName != nil || patch.DisplayName != nil ||
		patch.Language != nil || patch.ModelProfileKey != nil || patch.ProviderModelID != nil ||
		patch.VoiceKey != nil || patch.Instructions != nil || patch.ReferenceArtifactID != nil ||
		patch.ReferenceMediaFileID != nil || patch.Parameters != nil || patch.IsDefault != nil
}

func validateCharacterVoiceIDs(values ...string) error {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := uuid.Parse(value); err != nil {
			return controlValidationError("角色声音引用必须使用有效 UUID")
		}
	}
	return nil
}

func normalizeCharacterVoiceParameters(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var parameters map[string]any
	if err := json.Unmarshal(raw, &parameters); err != nil || parameters == nil {
		return nil, controlValidationError("parameters 必须为 JSON 对象")
	}
	return raw, nil
}

func (s *Server) createCharacterVoiceActionTx(ctx context.Context, tx pgx.Tx, project Project, actorUserID string, input characterVoiceCreateActionInput) (CharacterVoiceProfile, error) {
	if err := lockCharacterVoiceScopeTx(ctx, tx, project.ID); err != nil {
		return CharacterVoiceProfile{}, err
	}
	resolved := resolvedCharacterVoice{
		CanonicalAssetID: input.CanonicalAssetID, CharacterName: input.CharacterName, DisplayName: input.DisplayName,
		Language: input.Language, ModelProfileKey: input.ModelProfileKey, ProviderModelID: input.ProviderModelID,
		VoiceKey: input.VoiceKey, Instructions: input.Instructions, ReferenceArtifactID: input.ReferenceArtifactID,
		ReferenceMediaFileID: input.ReferenceMediaFileID, Parameters: input.Parameters,
	}
	if err := s.validateCharacterVoiceReferencesTx(ctx, tx, project, resolved); err != nil {
		return CharacterVoiceProfile{}, err
	}
	requestedDefault := input.IsDefault != nil && *input.IsDefault
	if !requestedDefault {
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM character_voice_profiles WHERE project_id = $1 AND status = 'active' AND is_default = true)`, project.ID).Scan(&requestedDefault); err != nil {
			return CharacterVoiceProfile{}, err
		}
	}
	if requestedDefault {
		if _, err := tx.Exec(ctx, `UPDATE character_voice_profiles SET is_default = false, updated_at = now() WHERE project_id = $1 AND status = 'active' AND is_default = true`, project.ID); err != nil {
			return CharacterVoiceProfile{}, err
		}
	}
	var item CharacterVoiceProfile
	err := scanCharacterVoice(tx.QueryRow(ctx, `
		INSERT INTO character_voice_profiles(
			organization_id, project_id, canonical_asset_id, character_name, display_name, language,
			model_profile_key, provider_model_id, voice_key, instructions, reference_artifact_id,
			reference_media_file_id, parameters, is_default, created_by
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, NULLIF($8, '')::uuid, $9,
		        NULLIF($10, ''), NULLIF($11, '')::uuid, NULLIF($12, '')::uuid, $13, $14, $15)
		RETURNING `+characterVoiceSelectColumns+`
	`, project.OrganizationID, project.ID, resolved.CanonicalAssetID, resolved.CharacterName, resolved.DisplayName,
		resolved.Language, resolved.ModelProfileKey, resolved.ProviderModelID, resolved.VoiceKey, resolved.Instructions,
		resolved.ReferenceArtifactID, resolved.ReferenceMediaFileID, resolved.Parameters, requestedDefault, actorUserID), &item)
	if err != nil {
		return CharacterVoiceProfile{}, characterVoiceStorageError(err)
	}
	if _, err := invalidateProjectAudioConfigurationTx(ctx, tx, project, "character_voice_created", actorUserID); err != nil {
		return CharacterVoiceProfile{}, err
	}
	return item, nil
}

func (s *Server) updateCharacterVoiceActionTx(ctx context.Context, tx pgx.Tx, project Project, actorUserID string, input characterVoiceUpdateActionInput) (CharacterVoiceProfile, error) {
	if err := lockCharacterVoiceScopeTx(ctx, tx, project.ID); err != nil {
		return CharacterVoiceProfile{}, err
	}
	current, err := characterVoiceTx(ctx, tx, project.ID, input.VoiceID, true)
	if err != nil {
		return CharacterVoiceProfile{}, characterVoiceNotFoundError(err)
	}
	if current.Revision != input.ExpectedRevision {
		return CharacterVoiceProfile{}, revisionConflictError("CHARACTER_VOICE_REVISION_CONFLICT", "角色声音已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	resolved := resolvedCharacterVoice{
		CanonicalAssetID: derefOptionalString(current.CanonicalAssetID), CharacterName: current.CharacterName,
		DisplayName: current.DisplayName, Language: current.Language, ModelProfileKey: current.ModelProfileKey,
		ProviderModelID: derefOptionalString(current.ProviderModelID), VoiceKey: current.VoiceKey,
		Instructions: derefOptionalString(current.Instructions), ReferenceArtifactID: derefOptionalString(current.ReferenceArtifactID),
		ReferenceMediaFileID: derefOptionalString(current.ReferenceMediaFileID), Parameters: current.Parameters, IsDefault: current.IsDefault,
	}
	applyCharacterVoicePatch(&resolved, input.Patch)
	if resolved.CharacterName == "" || resolved.DisplayName == "" || resolved.VoiceKey == "" {
		return CharacterVoiceProfile{}, controlValidationError("characterName、displayName 和 voiceKey 不能为空")
	}
	resolved.Language = firstNonEmpty(resolved.Language, "zh-CN")
	resolved.ModelProfileKey = firstNonEmpty(resolved.ModelProfileKey, "tts_generation_default")
	if current.IsDefault && !resolved.IsDefault {
		return CharacterVoiceProfile{}, newAPIError(http.StatusUnprocessableEntity, "CHARACTER_VOICE_DEFAULT_REQUIRED", "请先将另一条声音设为默认旁白")
	}
	if err := s.validateCharacterVoiceReferencesTx(ctx, tx, project, resolved); err != nil {
		return CharacterVoiceProfile{}, err
	}
	generationSettingsChanged := resolved.CanonicalAssetID != derefOptionalString(current.CanonicalAssetID) ||
		resolved.CharacterName != current.CharacterName || resolved.Language != current.Language ||
		resolved.ModelProfileKey != current.ModelProfileKey || resolved.ProviderModelID != derefOptionalString(current.ProviderModelID) ||
		resolved.VoiceKey != current.VoiceKey || resolved.Instructions != derefOptionalString(current.Instructions) ||
		resolved.ReferenceArtifactID != derefOptionalString(current.ReferenceArtifactID) ||
		resolved.ReferenceMediaFileID != derefOptionalString(current.ReferenceMediaFileID) || resolved.IsDefault != current.IsDefault ||
		!audioJSONEquivalent(resolved.Parameters, current.Parameters)
	if resolved.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE character_voice_profiles SET is_default = false, updated_at = now() WHERE project_id = $1 AND id <> $2 AND status = 'active' AND is_default = true`, project.ID, current.ID); err != nil {
			return CharacterVoiceProfile{}, err
		}
	}
	var item CharacterVoiceProfile
	err = scanCharacterVoice(tx.QueryRow(ctx, `
		UPDATE character_voice_profiles
		SET canonical_asset_id = NULLIF($3, '')::uuid, character_name = $4, display_name = $5,
		    language = $6, model_profile_key = $7, provider_model_id = NULLIF($8, '')::uuid,
		    voice_key = $9, instructions = NULLIF($10, ''), reference_artifact_id = NULLIF($11, '')::uuid,
		    reference_media_file_id = NULLIF($12, '')::uuid, parameters = $13, is_default = $14, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND status = 'active' AND revision = $15
		RETURNING `+characterVoiceSelectColumns+`
	`, project.ID, current.ID, resolved.CanonicalAssetID, resolved.CharacterName, resolved.DisplayName,
		resolved.Language, resolved.ModelProfileKey, resolved.ProviderModelID, resolved.VoiceKey, resolved.Instructions,
		resolved.ReferenceArtifactID, resolved.ReferenceMediaFileID, resolved.Parameters, resolved.IsDefault, input.ExpectedRevision), &item)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CharacterVoiceProfile{}, revisionConflictError("CHARACTER_VOICE_REVISION_CONFLICT", "角色声音已被其他操作修改", input.ExpectedRevision, current.Revision)
		}
		return CharacterVoiceProfile{}, characterVoiceStorageError(err)
	}
	if generationSettingsChanged {
		if _, err := invalidateProjectAudioConfigurationTx(ctx, tx, project, "character_voice_updated", actorUserID); err != nil {
			return CharacterVoiceProfile{}, err
		}
	}
	return item, nil
}

func (s *Server) deleteCharacterVoiceActionTx(ctx context.Context, tx pgx.Tx, project Project, actorUserID string, input characterVoiceDeleteActionInput) (CharacterVoiceProfile, error) {
	if err := lockCharacterVoiceScopeTx(ctx, tx, project.ID); err != nil {
		return CharacterVoiceProfile{}, err
	}
	current, err := characterVoiceTx(ctx, tx, project.ID, input.VoiceID, true)
	if err != nil {
		return CharacterVoiceProfile{}, characterVoiceNotFoundError(err)
	}
	if current.Revision != input.ExpectedRevision {
		return CharacterVoiceProfile{}, revisionConflictError("CHARACTER_VOICE_REVISION_CONFLICT", "角色声音已被其他操作修改", input.ExpectedRevision, current.Revision)
	}
	var archived CharacterVoiceProfile
	err = scanCharacterVoice(tx.QueryRow(ctx, `
		UPDATE character_voice_profiles
		SET status = 'archived', is_default = false,
		    metadata = metadata || jsonb_build_object('archivedAt', now(), 'archivedBy', $3::uuid::text),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND status = 'active' AND revision = $4
		RETURNING `+characterVoiceSelectColumns+`
	`, project.ID, current.ID, actorUserID, input.ExpectedRevision), &archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CharacterVoiceProfile{}, revisionConflictError("CHARACTER_VOICE_REVISION_CONFLICT", "角色声音已被其他操作修改", input.ExpectedRevision, current.Revision)
		}
		return CharacterVoiceProfile{}, err
	}
	if current.IsDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE character_voice_profiles SET is_default = true, updated_at = now()
			WHERE id = (
				SELECT id FROM character_voice_profiles
				WHERE project_id = $1 AND status = 'active'
				ORDER BY updated_at DESC, created_at DESC LIMIT 1
			)
		`, project.ID); err != nil {
			return CharacterVoiceProfile{}, err
		}
	}
	if _, err := invalidateProjectAudioConfigurationTx(ctx, tx, project, "character_voice_archived", actorUserID); err != nil {
		return CharacterVoiceProfile{}, err
	}
	return archived, nil
}

func applyCharacterVoicePatch(target *resolvedCharacterVoice, patch characterVoicePatch) {
	apply := func(destination *string, source *string) {
		if source != nil {
			*destination = strings.TrimSpace(*source)
		}
	}
	apply(&target.CanonicalAssetID, patch.CanonicalAssetID)
	apply(&target.CharacterName, patch.CharacterName)
	apply(&target.DisplayName, patch.DisplayName)
	apply(&target.Language, patch.Language)
	apply(&target.ModelProfileKey, patch.ModelProfileKey)
	apply(&target.ProviderModelID, patch.ProviderModelID)
	apply(&target.VoiceKey, patch.VoiceKey)
	apply(&target.Instructions, patch.Instructions)
	apply(&target.ReferenceArtifactID, patch.ReferenceArtifactID)
	apply(&target.ReferenceMediaFileID, patch.ReferenceMediaFileID)
	if patch.Parameters != nil {
		target.Parameters = *patch.Parameters
	}
	if patch.IsDefault != nil {
		target.IsDefault = *patch.IsDefault
	}
}

func lockCharacterVoiceScopeTx(ctx context.Context, tx pgx.Tx, projectID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('character-voice:' || $1, 0))`, projectID)
	return err
}

func characterVoiceTx(ctx context.Context, tx pgx.Tx, projectID, voiceID string, activeOnly bool) (CharacterVoiceProfile, error) {
	statusClause := ""
	if activeOnly {
		statusClause = " AND status = 'active'"
	}
	var item CharacterVoiceProfile
	err := scanCharacterVoice(tx.QueryRow(ctx, `
		SELECT `+characterVoiceSelectColumns+`
		FROM character_voice_profiles
		WHERE project_id = $1 AND id = $2`+statusClause+` FOR UPDATE
	`, projectID, voiceID), &item)
	return item, err
}

func (s *Server) validateCharacterVoiceReferencesTx(ctx context.Context, tx pgx.Tx, project Project, voice resolvedCharacterVoice) error {
	if voice.CanonicalAssetID != "" {
		var valid bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM canonical_assets
				WHERE id = $1 AND project_id = $2 AND asset_type = 'character' AND status <> 'archived'
			)
		`, voice.CanonicalAssetID, project.ID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return newAPIError(http.StatusUnprocessableEntity, "CHARACTER_VOICE_REFERENCE_INVALID", "关联资产必须是当前项目中未归档的角色资产")
		}
	}
	if voice.ProviderModelID != "" {
		var compatible bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM provider_models model
				JOIN provider_accounts account ON account.id = model.provider_account_id
				JOIN provider_model_capabilities capability ON capability.provider_model_id = model.id
				WHERE account.organization_id = $1 AND model.id = $2
				  AND model.status = 'active' AND account.status = 'active'
				  AND model.modality IN ('audio', 'multimodal') AND capability.task_types ? $3
			)
		`, project.OrganizationID, voice.ProviderModelID, provider.TaskTypeAudioTTS).Scan(&compatible); err != nil {
			return err
		}
		if !compatible {
			return newAPIError(http.StatusUnprocessableEntity, "CHARACTER_VOICE_REFERENCE_INVALID", "所选模型不支持角色配音")
		}
	}
	if voice.ReferenceArtifactID != "" {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE id = $1 AND organization_id = $2 AND project_id = $3)`, voice.ReferenceArtifactID, project.OrganizationID, project.ID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return newAPIError(http.StatusUnprocessableEntity, "CHARACTER_VOICE_REFERENCE_INVALID", "参考产物不属于当前项目")
		}
	}
	if voice.ReferenceMediaFileID != "" {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_files WHERE id = $1 AND organization_id = $2 AND project_id = $3)`, voice.ReferenceMediaFileID, project.OrganizationID, project.ID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return newAPIError(http.StatusUnprocessableEntity, "CHARACTER_VOICE_REFERENCE_INVALID", "参考媒体不属于当前项目")
		}
	}
	return nil
}

func characterVoiceStorageError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return newAPIError(http.StatusConflict, "CHARACTER_VOICE_ALREADY_EXISTS", "该角色已存在启用的声音配置")
	}
	return err
}

func characterVoiceNotFoundError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return newAPIError(http.StatusNotFound, "CHARACTER_VOICE_NOT_FOUND", "角色声音不存在或已归档")
	}
	return err
}

func characterVoiceAgentResult(action string, raw json.RawMessage, message string, item CharacterVoiceProfile) (agentToolResult, error) {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return agentToolResult{}, fmt.Errorf("decode %s arguments: %w", action, err)
	}
	return agentToolOK(action, arguments, message, map[string]any{"voice": item}), nil
}

func validateCharacterVoiceActionCommand(projectID, actorUserID, action string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(actorUserID) == "" {
		return newAPIError(http.StatusUnprocessableEntity, "PROJECT_CONTROL_CONTEXT_INVALID", action+" 缺少项目或执行用户")
	}
	return nil
}
