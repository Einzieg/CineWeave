package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

const scriptActionMaximumInlineBytes = 48 * 1024
const scriptActionMaximumCommandBytes = 60 * 1024

type scriptCreateActionInput struct {
	SourceID      *string         `json:"sourceId,omitempty"`
	Title         string          `json:"title"`
	Content       string          `json:"content,omitempty"`
	ContentFormat string          `json:"contentFormat,omitempty"`
	SourceType    *string         `json:"sourceType,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

type scriptCreateActionOutcome struct {
	Script  Script         `json:"script"`
	Version *ScriptVersion `json:"version,omitempty"`
	Episode *ScriptEpisode `json:"episode,omitempty"`
}

type scriptCreateVersionActionInput struct {
	ScriptID         string          `json:"scriptId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Content          string          `json:"content"`
	ContentFormat    string          `json:"contentFormat,omitempty"`
	SourceType       *string         `json:"sourceType,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Activate         bool            `json:"activate,omitempty"`
}

type scriptCreateVersionActionOutcome struct {
	Script            Script        `json:"script"`
	Version           ScriptVersion `json:"version"`
	Episode           ScriptEpisode `json:"episode"`
	PreviousVersionID string        `json:"previousVersionId,omitempty"`
}

type scriptActivateVersionActionInput struct {
	ScriptID         string `json:"scriptId"`
	VersionID        string `json:"versionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type scriptActivateVersionActionOutcome struct {
	Script            Script        `json:"script"`
	Version           ScriptVersion `json:"version"`
	PreviousVersionID string        `json:"previousVersionId,omitempty"`
	Changed           bool          `json:"changed"`
}

type scriptArchiveVersionActionInput struct {
	ScriptID         string `json:"scriptId"`
	VersionID        string `json:"versionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason,omitempty"`
}

type scriptArchiveVersionActionOutcome struct {
	Deleted        bool   `json:"deleted"`
	Mode           string `json:"mode"`
	ScriptID       string `json:"scriptId"`
	VersionID      string `json:"versionId"`
	Version        int    `json:"version"`
	ScriptRevision int64  `json:"scriptRevision"`
}

func decodeScriptCreateActionInput(raw json.RawMessage) (scriptCreateActionInput, error) {
	if len(raw) > scriptActionMaximumCommandBytes {
		return scriptCreateActionInput{}, contentStagingRequired("创建剧本")
	}
	var input scriptCreateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptCreateActionInput{}, controlValidationError("script.create 输入格式无效")
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	input.ContentFormat = strings.TrimSpace(input.ContentFormat)
	if len([]byte(input.Content)) > scriptActionMaximumInlineBytes {
		return scriptCreateActionInput{}, contentStagingRequired("剧本正文")
	}
	return input, nil
}

func decodeScriptCreateVersionActionInput(raw json.RawMessage) (scriptCreateVersionActionInput, error) {
	if len(raw) > scriptActionMaximumCommandBytes {
		return scriptCreateVersionActionInput{}, contentStagingRequired("创建剧本版本")
	}
	var input scriptCreateVersionActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptCreateVersionActionInput{}, controlValidationError("script.create_version 输入格式无效")
	}
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.Content = strings.TrimSpace(input.Content)
	input.ContentFormat = strings.TrimSpace(input.ContentFormat)
	if len([]byte(input.Content)) > scriptActionMaximumInlineBytes {
		return scriptCreateVersionActionInput{}, contentStagingRequired("剧本版本正文")
	}
	if input.ScriptID == "" || input.ExpectedRevision < 1 {
		return scriptCreateVersionActionInput{}, controlValidationError("scriptId 和 expectedRevision 不能为空")
	}
	return input, nil
}

func decodeScriptActivateVersionActionInput(raw json.RawMessage) (scriptActivateVersionActionInput, error) {
	var input scriptActivateVersionActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptActivateVersionActionInput{}, controlValidationError("script.activate_version 输入格式无效")
	}
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.ScriptID == "" || input.VersionID == "" || input.ExpectedRevision < 1 {
		return scriptActivateVersionActionInput{}, controlValidationError("scriptId、versionId 和 expectedRevision 不能为空")
	}
	return input, nil
}

func decodeScriptArchiveVersionActionInput(raw json.RawMessage) (scriptArchiveVersionActionInput, error) {
	var input scriptArchiveVersionActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptArchiveVersionActionInput{}, controlValidationError("script.archive_version 输入格式无效")
	}
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ScriptID == "" || input.VersionID == "" || input.ExpectedRevision < 1 {
		return scriptArchiveVersionActionInput{}, controlValidationError("scriptId、versionId 和 expectedRevision 不能为空")
	}
	return input, nil
}

func contentStagingRequired(subject string) error {
	return newAPIError(http.StatusRequestEntityTooLarge, "CONTENT_STAGING_REQUIRED", subject+"超过内联命令上限，请使用长内容暂存协议")
}

func (s *Server) createScriptActionTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	input scriptCreateActionInput,
) (scriptCreateActionOutcome, error) {
	if input.Title == "" {
		return scriptCreateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本标题不能为空")
	}
	if input.ContentFormat == "" {
		input.ContentFormat = "markdown"
	}
	if !validScriptContentFormat(input.ContentFormat) {
		return scriptCreateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本正文格式无效")
	}
	if input.SourceID != nil {
		value := strings.TrimSpace(*input.SourceID)
		if value == "" {
			input.SourceID = nil
		} else {
			input.SourceID = &value
			var exists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM project_sources WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'ready') <> 'archived')
			`, project.ID, value).Scan(&exists); err != nil {
				return scriptCreateActionOutcome{}, err
			}
			if !exists {
				return scriptCreateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "SOURCE_NOT_AVAILABLE", "关联原文不存在或已归档")
			}
		}
	}
	metadata, err := scriptActionMetadata(input.Metadata)
	if err != nil {
		return scriptCreateActionOutcome{}, err
	}
	r := requestWithContext(ctx)
	item, err := scanScript(tx.QueryRow(ctx, scriptInsertSQL(), project.OrganizationID, project.ID, input.SourceID, input.Title, "draft", principal.UserID))
	if err != nil {
		return scriptCreateActionOutcome{}, err
	}
	outcome := scriptCreateActionOutcome{Script: item}
	if input.Content != "" {
		version, err := insertScriptVersionTx(r, tx, project, item.ID, 1, input.Content, input.ContentFormat, input.SourceType, "", "", metadata, principal.UserID)
		if err != nil {
			return scriptCreateActionOutcome{}, err
		}
		episodes, err := insertScriptEpisodesTx(r, tx, project, item.ID, version.ID, principal.UserID, []scriptEpisodeDraft{
			defaultScriptEpisodeDraft(input.SourceID, "第 1 集", input.Content, input.ContentFormat, "", "", "", metadata),
		})
		if err != nil {
			return scriptCreateActionOutcome{}, err
		}
		if _, err := activateScriptVersionTx(r, tx, project, item, version); err != nil {
			return scriptCreateActionOutcome{}, err
		}
		outcome.Version = &version
		if len(episodes) > 0 {
			outcome.Episode = &episodes[0]
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.created", "script_version", version.ID, mustRawJSON(map[string]any{
			"scriptId": item.ID, "scriptVersionId": version.ID, "version": version.Version, "createdBy": principal.UserID,
		})); err != nil {
			return scriptCreateActionOutcome{}, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.activated", "script_version", version.ID, mustRawJSON(map[string]any{
			"scriptId": item.ID, "scriptVersionId": version.ID, "previousVersionId": "", "createdBy": principal.UserID,
		})); err != nil {
			return scriptCreateActionOutcome{}, err
		}
	}
	item, err = scanScript(tx.QueryRow(ctx, scriptSelectSQL(`WHERE s.project_id = $1 AND s.id = $2`), project.ID, item.ID))
	if err != nil {
		return scriptCreateActionOutcome{}, err
	}
	if outcome.Version != nil {
		item.CurrentVersion = outcome.Version
	}
	outcome.Script = item
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.created", "script", item.ID, mustRawJSON(map[string]any{
		"scriptId": item.ID, "revision": item.Revision, "createdBy": principal.UserID,
	})); err != nil {
		return scriptCreateActionOutcome{}, err
	}
	return outcome, nil
}

func (s *Server) createScriptVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptCreateVersionActionInput,
) (scriptCreateVersionActionOutcome, error) {
	script, err := lockScriptActionTx(ctx, tx, project.ID, input.ScriptID)
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	if err := requireScriptActionRevision(script.Revision, input.ExpectedRevision); err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	if script.Status == "archived" || input.Content == "" {
		return scriptCreateVersionActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "归档剧本不能创建版本，且版本正文不能为空")
	}
	if input.ContentFormat == "" {
		input.ContentFormat = "markdown"
	}
	if !validScriptContentFormat(input.ContentFormat) {
		return scriptCreateVersionActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本正文格式无效")
	}
	metadata, err := scriptActionMetadata(input.Metadata)
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	r := requestWithContext(ctx)
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, nextVersion, input.Content, input.ContentFormat, input.SourceType, "", "", metadata, actorUserID)
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	episodes, err := insertScriptEpisodesTx(r, tx, project, script.ID, version.ID, actorUserID, []scriptEpisodeDraft{
		defaultScriptEpisodeDraft(script.SourceID, "第 1 集", input.Content, input.ContentFormat, "", "", "", metadata),
	})
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	outcome := scriptCreateVersionActionOutcome{Version: version}
	if len(episodes) > 0 {
		outcome.Episode = episodes[0]
	}
	if input.Activate {
		outcome.PreviousVersionID, err = activateScriptVersionTx(r, tx, project, script, version)
		if err != nil {
			return scriptCreateVersionActionOutcome{}, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.activated", "script_version", version.ID, mustRawJSON(map[string]any{
			"scriptId": script.ID, "scriptVersionId": version.ID, "previousVersionId": outcome.PreviousVersionID, "changedBy": actorUserID,
		})); err != nil {
			return scriptCreateVersionActionOutcome{}, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.created", "script_version", version.ID, mustRawJSON(map[string]any{
		"scriptId": script.ID, "scriptVersionId": version.ID, "version": version.Version, "createdBy": actorUserID,
	})); err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	script, err = scanScript(tx.QueryRow(ctx, scriptSelectSQL(`WHERE s.project_id = $1 AND s.id = $2`), project.ID, script.ID))
	if err != nil {
		return scriptCreateVersionActionOutcome{}, err
	}
	if input.Activate {
		script.CurrentVersion = &version
	}
	outcome.Script = script
	return outcome, nil
}

func (s *Server) activateScriptVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptActivateVersionActionInput,
) (scriptActivateVersionActionOutcome, error) {
	script, err := lockScriptActionTx(ctx, tx, project.ID, input.ScriptID)
	if err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	if err := requireScriptActionRevision(script.Revision, input.ExpectedRevision); err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	version, err := scriptVersionActionTx(ctx, tx, project.ID, script.ID, input.VersionID)
	if err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	if script.CurrentVersionID != nil && *script.CurrentVersionID == version.ID {
		script.CurrentVersion = &version
		return scriptActivateVersionActionOutcome{Script: script, Version: version, Changed: false}, nil
	}
	r := requestWithContext(ctx)
	previousVersionID, err := activateScriptVersionTx(r, tx, project, script, version)
	if err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.activated", "script_version", version.ID, mustRawJSON(map[string]any{
		"scriptId": script.ID, "scriptVersionId": version.ID, "previousVersionId": previousVersionID, "changedBy": actorUserID,
	})); err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	script, err = scanScript(tx.QueryRow(ctx, scriptSelectSQL(`WHERE s.project_id = $1 AND s.id = $2`), project.ID, script.ID))
	if err != nil {
		return scriptActivateVersionActionOutcome{}, err
	}
	script.CurrentVersion = &version
	return scriptActivateVersionActionOutcome{Script: script, Version: version, PreviousVersionID: previousVersionID, Changed: true}, nil
}

func (s *Server) archiveScriptVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptArchiveVersionActionInput,
) (scriptArchiveVersionActionOutcome, error) {
	script, err := lockScriptActionTx(ctx, tx, project.ID, input.ScriptID)
	if err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	if err := requireScriptActionRevision(script.Revision, input.ExpectedRevision); err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	version, err := scriptVersionActionTx(ctx, tx, project.ID, script.ID, input.VersionID)
	if err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	if script.CurrentVersionID != nil && *script.CurrentVersionID == version.ID {
		return scriptArchiveVersionActionOutcome{}, newAPIError(http.StatusConflict, "CURRENT_SCRIPT_VERSION", "当前激活版本不能归档")
	}
	if err := markScriptVersionDownstreamStale(ctx, tx, project.ID, version.ID); err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_scenes
		SET deleted_at = now(), stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1 AND script_version_id = $2 AND deleted_at IS NULL
	`, project.ID, version.ID); err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE script_versions SET status = 'archived'
		WHERE project_id = $1 AND script_id = $2 AND id = $3 AND COALESCE(status, 'active') <> 'archived'
	`, project.ID, script.ID, version.ID)
	if err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	if tag.RowsAffected() == 0 {
		return scriptArchiveVersionActionOutcome{}, newAPIError(http.StatusNotFound, "NOT_FOUND", "剧本版本不存在")
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.archived", "script_version", version.ID, mustRawJSON(map[string]any{
		"scriptId": script.ID, "scriptVersionId": version.ID, "version": version.Version,
		"archivedBy": actorUserID, "reason": input.Reason,
	})); err != nil {
		return scriptArchiveVersionActionOutcome{}, err
	}
	return scriptArchiveVersionActionOutcome{
		Deleted: true, Mode: "archive", ScriptID: script.ID, VersionID: version.ID,
		Version: version.Version, ScriptRevision: script.Revision,
	}, nil
}

func scriptVersionActionTx(ctx context.Context, tx pgx.Tx, projectID, scriptID, versionID string) (ScriptVersion, error) {
	return scanScriptVersion(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, script_id, version, content, content_format, COALESCE(status, 'active'),
		       source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND id = $3 AND COALESCE(status, 'active') <> 'archived'
		FOR UPDATE
	`, projectID, scriptID, versionID))
}

func scriptActionMetadata(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "metadata 必须是 JSON 对象")
	}
	return mustRawJSON(object), nil
}

func scriptCreateAgentResult(arguments map[string]any, outcome scriptCreateActionOutcome) agentToolResult {
	data := map[string]any{
		"scriptId": outcome.Script.ID, "title": outcome.Script.Title,
		"status": outcome.Script.Status, "revision": outcome.Script.Revision,
		"currentVersionId": outcome.Script.CurrentVersionID,
	}
	if outcome.Version != nil {
		data["version"] = scriptVersionSummary(*outcome.Version)
	}
	if outcome.Episode != nil {
		data["episodeId"] = outcome.Episode.ID
		data["episodeRevision"] = outcome.Episode.Revision
		data["episodeContentHash"] = outcome.Episode.ContentHash
	}
	delete(arguments, "content")
	delete(arguments, "metadata")
	return agentToolOK("script.create", arguments, "已创建剧本《"+outcome.Script.Title+"》。", data)
}

func scriptCreateVersionAgentResult(arguments map[string]any, outcome scriptCreateVersionActionOutcome) agentToolResult {
	delete(arguments, "content")
	delete(arguments, "metadata")
	return agentToolOK("script.create_version", arguments, fmt.Sprintf("已创建剧本版本 v%d。", outcome.Version.Version), map[string]any{
		"scriptId": outcome.Script.ID, "scriptRevision": outcome.Script.Revision,
		"version": scriptVersionSummary(outcome.Version), "episodeId": outcome.Episode.ID,
		"episodeRevision": outcome.Episode.Revision, "episodeContentHash": outcome.Episode.ContentHash,
		"previousVersionId": outcome.PreviousVersionID,
	})
}

func scriptActivateVersionAgentResult(arguments map[string]any, outcome scriptActivateVersionActionOutcome) agentToolResult {
	summary := fmt.Sprintf("已激活剧本版本 v%d。", outcome.Version.Version)
	if !outcome.Changed {
		summary = fmt.Sprintf("剧本版本 v%d 已是当前激活版本。", outcome.Version.Version)
	}
	return agentToolOK("script.activate_version", arguments, summary, map[string]any{
		"scriptId": outcome.Script.ID, "scriptRevision": outcome.Script.Revision,
		"version": scriptVersionSummary(outcome.Version), "previousVersionId": outcome.PreviousVersionID,
		"changed": outcome.Changed,
	})
}

func scriptArchiveVersionAgentResult(arguments map[string]any, outcome scriptArchiveVersionActionOutcome) agentToolResult {
	return agentToolOK("script.archive_version", arguments, fmt.Sprintf("已归档剧本版本 v%d。", outcome.Version), map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "scriptId": outcome.ScriptID,
		"versionId": outcome.VersionID, "version": outcome.Version, "scriptRevision": outcome.ScriptRevision,
	})
}

func scriptVersionSummary(version ScriptVersion) scriptVersionActionSummary {
	return scriptVersionActionSummary{
		ID: version.ID, Version: version.Version, Status: version.Status, SourceType: version.SourceType,
		ContentFormat: version.ContentFormat, ContentLength: len([]rune(version.Content)),
		ContentHash: contentSHA256(version.Content), CreatedAt: version.CreatedAt,
		ContentTarget: projectControlContentTarget{TargetType: "script_version", TargetID: version.ID},
	}
}

func contentSHA256(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
