package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

type scriptUpdateActionInput struct {
	ScriptID         string
	ExpectedRevision int64
	Patch            scriptUpdateActionPatch
}

type scriptUpdateActionPatch struct {
	SourceID    *string
	SourceIDSet bool
	Title       *string
	Status      *string
}

type scriptUpdateActionWire struct {
	ScriptID         string `json:"scriptId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Patch            struct {
		SourceID *string `json:"sourceId"`
		Title    *string `json:"title"`
		Status   *string `json:"status"`
	} `json:"patch"`
}

type scriptUpdateActionOutcome struct {
	Script        Script   `json:"script"`
	ChangedFields []string `json:"changedFields"`
}

type scriptEpisodeUpdateActionInput struct {
	EpisodeID        string
	ExpectedRevision int64
	Patch            scriptEpisodeUpdateActionPatch
}

type scriptEpisodeUpdateActionPatch struct {
	EpisodeTitle  *string
	Content       *string
	ContentFormat *string
	ReviewStatus  *string
	StaleState    *string
	Metadata      json.RawMessage
	MetadataSet   bool
}

type scriptEpisodeUpdateActionWire struct {
	EpisodeID        string `json:"episodeId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Patch            struct {
		EpisodeTitle  *string         `json:"episodeTitle"`
		Content       *string         `json:"content"`
		ContentFormat *string         `json:"contentFormat"`
		ReviewStatus  *string         `json:"reviewStatus"`
		StaleState    *string         `json:"staleState"`
		Metadata      json.RawMessage `json:"metadata"`
	} `json:"patch"`
}

type scriptEpisodeUpdateActionOutcome struct {
	Episode ScriptEpisode `json:"episode"`
}

type scriptDeleteActionInput struct {
	ScriptID         string `json:"scriptId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type scriptDeleteActionOutcome struct {
	Deleted      bool     `json:"deleted"`
	Mode         string   `json:"mode"`
	ScriptID     string   `json:"scriptId"`
	Revision     int64    `json:"revision"`
	VersionIDs   []string `json:"versionIds"`
	VersionCount int      `json:"versionCount"`
}

func decodeScriptUpdateActionInput(raw json.RawMessage) (scriptUpdateActionInput, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return scriptUpdateActionInput{}, controlValidationError("script.update 输入必须是 JSON 对象")
	}
	var wire scriptUpdateActionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return scriptUpdateActionInput{}, controlValidationError("script.update 输入格式无效")
	}
	patchRaw, exists := top["patch"]
	if !exists || string(patchRaw) == "null" {
		return scriptUpdateActionInput{}, controlValidationError("patch 不能为空")
	}
	var patchFields map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patchFields); err != nil || len(patchFields) == 0 {
		return scriptUpdateActionInput{}, controlValidationError("patch 必须是非空对象")
	}
	input := scriptUpdateActionInput{
		ScriptID: strings.TrimSpace(wire.ScriptID), ExpectedRevision: wire.ExpectedRevision,
		Patch: scriptUpdateActionPatch{SourceID: wire.Patch.SourceID, Title: wire.Patch.Title, Status: wire.Patch.Status},
	}
	_, input.Patch.SourceIDSet = patchFields["sourceId"]
	if input.ScriptID == "" {
		return scriptUpdateActionInput{}, controlValidationError("scriptId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return scriptUpdateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func decodeScriptEpisodeUpdateActionInput(raw json.RawMessage) (scriptEpisodeUpdateActionInput, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("script.update_episode 输入必须是 JSON 对象")
	}
	var wire scriptEpisodeUpdateActionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("script.update_episode 输入格式无效")
	}
	patchRaw, exists := top["patch"]
	if !exists || string(patchRaw) == "null" {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("patch 不能为空")
	}
	var patchFields map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patchFields); err != nil || len(patchFields) == 0 {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("patch 必须是非空对象")
	}
	input := scriptEpisodeUpdateActionInput{
		EpisodeID: strings.TrimSpace(wire.EpisodeID), ExpectedRevision: wire.ExpectedRevision,
		Patch: scriptEpisodeUpdateActionPatch{
			EpisodeTitle: wire.Patch.EpisodeTitle, Content: wire.Patch.Content,
			ContentFormat: wire.Patch.ContentFormat, ReviewStatus: wire.Patch.ReviewStatus,
			StaleState: wire.Patch.StaleState, Metadata: wire.Patch.Metadata,
		},
	}
	_, input.Patch.MetadataSet = patchFields["metadata"]
	if input.EpisodeID == "" {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("episodeId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return scriptEpisodeUpdateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func decodeScriptDeleteActionInput(raw json.RawMessage) (scriptDeleteActionInput, error) {
	var input scriptDeleteActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptDeleteActionInput{}, controlValidationError("script.delete 输入格式无效")
	}
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ScriptID == "" {
		return scriptDeleteActionInput{}, controlValidationError("scriptId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return scriptDeleteActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func (s *Server) updateScriptActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptUpdateActionInput,
) (scriptUpdateActionOutcome, error) {
	current, err := lockScriptActionTx(ctx, tx, project.ID, input.ScriptID)
	if err != nil {
		return scriptUpdateActionOutcome{}, err
	}
	if err := requireScriptActionRevision(current.Revision, input.ExpectedRevision); err != nil {
		return scriptUpdateActionOutcome{}, err
	}
	title := current.Title
	if input.Patch.Title != nil {
		title = strings.TrimSpace(*input.Patch.Title)
	}
	status := current.Status
	if input.Patch.Status != nil {
		status = strings.TrimSpace(*input.Patch.Status)
	}
	if title == "" || !validScriptStatus(status) {
		return scriptUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本字段无效")
	}
	if status == "archived" {
		return scriptUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "SCRIPT_ARCHIVE_ACTION_REQUIRED", "归档剧本请使用 script.delete")
	}
	sourceID := current.SourceID
	if input.Patch.SourceIDSet {
		sourceID = input.Patch.SourceID
		if sourceID != nil {
			value := strings.TrimSpace(*sourceID)
			if value == "" {
				sourceID = nil
			} else {
				sourceID = &value
				var exists bool
				if err := tx.QueryRow(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM project_sources
						WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'ready') <> 'archived'
					)
				`, project.ID, value).Scan(&exists); err != nil {
					return scriptUpdateActionOutcome{}, err
				}
				if !exists {
					return scriptUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "SOURCE_NOT_AVAILABLE", "关联原文不存在或已归档")
				}
			}
		}
	}
	changedFields := make([]string, 0, 3)
	if title != current.Title {
		changedFields = append(changedFields, "title")
	}
	if status != current.Status {
		changedFields = append(changedFields, "status")
	}
	if !sameOptionalString(sourceID, current.SourceID) {
		changedFields = append(changedFields, "sourceId")
	}
	if len(changedFields) == 0 {
		return scriptUpdateActionOutcome{Script: current, ChangedFields: changedFields}, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scripts
		SET title = $3, source_id = $4, status = $5, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $6
	`, project.ID, current.ID, title, sourceID, status, input.ExpectedRevision); err != nil {
		return scriptUpdateActionOutcome{}, err
	}
	item, err := scanScript(tx.QueryRow(ctx, scriptSelectSQL(`WHERE s.project_id = $1 AND s.id = $2`), project.ID, current.ID))
	if err != nil {
		return scriptUpdateActionOutcome{}, err
	}
	if item.Revision == input.ExpectedRevision {
		return scriptUpdateActionOutcome{}, newAPIError(http.StatusConflict, "REVISION_CONFLICT", "剧本已被其它操作修改，请刷新后重试")
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.updated", "script", item.ID, mustRawJSON(map[string]any{
		"scriptId": item.ID, "revision": item.Revision, "changedFields": changedFields, "changedBy": actorUserID,
	})); err != nil {
		return scriptUpdateActionOutcome{}, err
	}
	return scriptUpdateActionOutcome{Script: item, ChangedFields: changedFields}, nil
}

func (s *Server) updateScriptEpisodeActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptEpisodeUpdateActionInput,
) (scriptEpisodeUpdateActionOutcome, error) {
	current, err := scanScriptEpisode(tx.QueryRow(ctx, scriptEpisodeSelectSQL(`
		WHERE se.project_id = $1 AND se.id = $2
		FOR UPDATE OF se
	`), project.ID, input.EpisodeID))
	if err != nil {
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return scriptEpisodeUpdateActionOutcome{}, revisionConflict("剧本分集", input.ExpectedRevision, current.Revision)
	}
	title := current.EpisodeTitle
	if input.Patch.EpisodeTitle != nil {
		title = strings.TrimSpace(*input.Patch.EpisodeTitle)
	}
	content := current.Content
	if input.Patch.Content != nil {
		content = strings.TrimSpace(*input.Patch.Content)
	}
	contentFormat := current.ContentFormat
	if input.Patch.ContentFormat != nil {
		contentFormat = strings.TrimSpace(*input.Patch.ContentFormat)
	}
	reviewStatus := current.ReviewStatus
	if input.Patch.ReviewStatus != nil {
		reviewStatus = strings.TrimSpace(*input.Patch.ReviewStatus)
	}
	staleState := current.StaleState
	if input.Patch.StaleState != nil {
		staleState = strings.TrimSpace(*input.Patch.StaleState)
	}
	if title == "" || content == "" || !validScriptContentFormat(contentFormat) ||
		!validScriptReviewStatus(reviewStatus) || !validScriptStaleState(staleState) {
		return scriptEpisodeUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本分集字段无效")
	}
	metadata := current.Metadata
	if input.Patch.MetadataSet {
		var object map[string]any
		if err := json.Unmarshal(input.Patch.Metadata, &object); err != nil || object == nil {
			return scriptEpisodeUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "metadata 必须是 JSON 对象")
		}
		metadata = mustRawJSON(object)
	}
	item, err := scanScriptEpisode(tx.QueryRow(ctx, `
		UPDATE script_episodes
		SET episode_title = $3, content = $4, content_format = $5,
		    review_status = $6, stale_state = $7, metadata = $8,
		    manual_override = true, edited_by = $9, edited_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $10
		RETURNING id, organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
		          episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
		          revision, content_hash, prompt_version_id, prompt_hash, provider_call_id,
		          review_status, manual_override, stale_state, metadata,
		          created_by, edited_by, created_at, updated_at, edited_at
	`, project.ID, current.ID, title, content, contentFormat, reviewStatus, staleState, metadata, actorUserID, input.ExpectedRevision))
	if err != nil {
		if err == pgx.ErrNoRows {
			return scriptEpisodeUpdateActionOutcome{}, revisionConflict("剧本分集", input.ExpectedRevision, current.Revision)
		}
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	r := requestWithContext(ctx)
	if err := rebuildScriptVersionContentFromEpisodesTx(r, tx, project.ID, item.ScriptVersionID); err != nil {
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	if err := markScriptVersionDownstreamStale(ctx, tx, project.ID, item.ScriptVersionID); err != nil {
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.episode.updated", "script_episode", item.ID, mustRawJSON(map[string]any{
		"scriptId": item.ScriptID, "scriptVersionId": item.ScriptVersionID,
		"episodeIndex": item.EpisodeIndex, "revision": item.Revision, "changedBy": actorUserID,
	})); err != nil {
		return scriptEpisodeUpdateActionOutcome{}, err
	}
	return scriptEpisodeUpdateActionOutcome{Episode: item}, nil
}

func (s *Server) deleteScriptActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input scriptDeleteActionInput,
) (scriptDeleteActionOutcome, error) {
	current, err := lockScriptActionTx(ctx, tx, project.ID, input.ScriptID)
	if err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	if err := requireScriptActionRevision(current.Revision, input.ExpectedRevision); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND COALESCE(status, 'active') <> 'archived'
		ORDER BY version ASC, id ASC
	`, project.ID, current.ID)
	if err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	versionIDs := make([]string, 0)
	for rows.Next() {
		var versionID string
		if err := rows.Scan(&versionID); err != nil {
			rows.Close()
			return scriptDeleteActionOutcome{}, err
		}
		versionIDs = append(versionIDs, versionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return scriptDeleteActionOutcome{}, err
	}
	rows.Close()
	if current.Status == "archived" {
		return scriptDeleteActionOutcome{
			Deleted: true, Mode: "archive", ScriptID: current.ID, Revision: current.Revision,
			VersionIDs: versionIDs, VersionCount: len(versionIDs),
		}, nil
	}
	for _, versionID := range versionIDs {
		if err := markScriptVersionDownstreamStale(ctx, tx, project.ID, versionID); err != nil {
			return scriptDeleteActionOutcome{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_scenes
		SET deleted_at = COALESCE(deleted_at, now()), stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1 AND script_id = $2 AND deleted_at IS NULL
	`, project.ID, current.ID); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE script_versions SET status = 'archived'
		WHERE project_id = $1 AND script_id = $2 AND COALESCE(status, 'active') <> 'archived'
	`, project.ID, current.ID); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE scripts
		SET status = 'archived', updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $3
		RETURNING revision
	`, project.ID, current.ID, input.ExpectedRevision).Scan(&revision); err != nil {
		if err == pgx.ErrNoRows {
			return scriptDeleteActionOutcome{}, revisionConflict("剧本", input.ExpectedRevision, current.Revision)
		}
		return scriptDeleteActionOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET active_script_id = NULL WHERE id = $1 AND active_script_id = $2`, project.ID, current.ID); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.archived", "script", current.ID, mustRawJSON(map[string]any{
		"scriptId": current.ID, "versionIds": versionIDs, "revision": revision,
		"archivedBy": actorUserID, "reason": input.Reason, "previousStatus": current.Status,
	})); err != nil {
		return scriptDeleteActionOutcome{}, err
	}
	return scriptDeleteActionOutcome{
		Deleted: true, Mode: "archive", ScriptID: current.ID, Revision: revision,
		VersionIDs: versionIDs, VersionCount: len(versionIDs),
	}, nil
}

func lockScriptActionTx(ctx context.Context, tx pgx.Tx, projectID, scriptID string) (Script, error) {
	return scanScript(tx.QueryRow(ctx, scriptSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2
		FOR UPDATE OF s
	`), projectID, scriptID))
}

func requireScriptActionRevision(actual, expected int64) error {
	if actual != expected {
		return revisionConflict("剧本", expected, actual)
	}
	return nil
}

func revisionConflict(entity string, expected, actual int64) error {
	return apiError{
		Status: http.StatusConflict, Code: "REVISION_CONFLICT", Retryable: true,
		Message: entity + "已被其它操作修改，请刷新后重试",
		Details: map[string]any{"expectedRevision": expected, "actualRevision": actual},
	}
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func scriptUpdateAgentResult(arguments map[string]any, outcome scriptUpdateActionOutcome) agentToolResult {
	return agentToolOK("script.update", arguments, "已更新剧本信息。", map[string]any{
		"script": outcome.Script, "scriptId": outcome.Script.ID,
		"revision": outcome.Script.Revision, "changedFields": outcome.ChangedFields,
	})
}

func scriptEpisodeUpdateAgentResult(arguments map[string]any, outcome scriptEpisodeUpdateActionOutcome) agentToolResult {
	item := outcome.Episode
	return agentToolOK("script.update_episode", arguments, "已更新剧本分集并标记下游需要重生成。", map[string]any{
		"episodeId": item.ID, "scriptId": item.ScriptID, "scriptVersionId": item.ScriptVersionID,
		"episodeIndex": item.EpisodeIndex, "revision": item.Revision, "contentHash": item.ContentHash,
	})
}

func scriptDeleteAgentResult(arguments map[string]any, outcome scriptDeleteActionOutcome) agentToolResult {
	return agentToolOK("script.delete", arguments, "已归档剧本及其版本。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "scriptId": outcome.ScriptID,
		"revision": outcome.Revision, "versionIds": outcome.VersionIDs, "versionCount": outcome.VersionCount,
	})
}

func validateScriptActionCommand(commandProjectID, actorUserID, action string) error {
	if commandProjectID == "" || actorUserID == "" {
		return fmt.Errorf("%s command identity is incomplete", action)
	}
	return nil
}
