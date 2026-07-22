package workflows

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	codeSourceToScriptReplanRequired      = "SOURCE_TO_SCRIPT_REPLAN_REQUIRED"
	defaultSourceToScriptStagingRetention = 30 * 24 * time.Hour
	defaultSourceToScriptCleanupBatchSize = 100
	minSourceToScriptStagingRetention     = time.Hour
)

type SourceToScriptPayloadPurgeResult struct {
	Generations int
	Items       int
	Results     int
}

func sourceToScriptStagingRetention() time.Duration {
	retention := config.Duration("CINEWEAVE_SOURCE_TO_SCRIPT_STAGING_RETENTION", defaultSourceToScriptStagingRetention)
	if retention < minSourceToScriptStagingRetention {
		return defaultSourceToScriptStagingRetention
	}
	return retention
}

func sourceToScriptRetentionDeadline() time.Time {
	return time.Now().UTC().Add(sourceToScriptStagingRetention())
}

// PurgeExpiredSourceToScriptPayloads removes only large, reproducible payloads.
// Generation identity, hashes, provider/prompt provenance, and formal script content remain durable.
func PurgeExpiredSourceToScriptPayloads(ctx context.Context, db *pgxpool.Pool, batchSize int) (SourceToScriptPayloadPurgeResult, error) {
	if db == nil {
		return SourceToScriptPayloadPurgeResult{}, fmt.Errorf("source-to-script payload cleanup requires database")
	}
	if batchSize <= 0 {
		batchSize = defaultSourceToScriptCleanupBatchSize
	}
	var result SourceToScriptPayloadPurgeResult
	err := db.QueryRow(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT id
			FROM source_to_script_generations
			WHERE payload_purged_at IS NULL
			  AND retention_expires_at <= now()
			  AND status = ANY (ARRAY['succeeded', 'partial_succeeded', 'failed', 'replan_required'])
			ORDER BY retention_expires_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), purged_items AS (
			UPDATE source_to_script_generation_items
			SET source_content = NULL, payload_purged_at = now()
			WHERE generation_id IN (SELECT id FROM candidates)
			  AND payload_purged_at IS NULL
			RETURNING 1
		), purged_results AS (
			UPDATE script_episode_generation_results
			SET content = NULL, payload_purged_at = now(), updated_at = now()
			WHERE generation_id IN (SELECT id FROM candidates)
			  AND payload_purged_at IS NULL
			RETURNING 1
		), purged_generations AS (
			UPDATE source_to_script_generations
			SET payload_purged_at = now()
			WHERE id IN (SELECT id FROM candidates)
			RETURNING 1
		)
		SELECT
			(SELECT count(*) FROM purged_generations),
			(SELECT count(*) FROM purged_items),
			(SELECT count(*) FROM purged_results)
	`, batchSize).Scan(&result.Generations, &result.Items, &result.Results)
	return result, err
}

type sourceToScriptQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type SourceToScriptPromptSnapshot struct {
	TemplateKey string `json:"templateKey"`
	VersionID   string `json:"versionId"`
	ContentHash string `json:"contentHash"`
	Source      string `json:"source"`
}

type SourceToScriptManifestItem struct {
	ItemKey                string `json:"itemKey"`
	SourceChapterID        string `json:"sourceChapterId,omitempty"`
	ManifestOrdinal        int    `json:"manifestOrdinal"`
	SourceRevision         int64  `json:"sourceRevision"`
	SourceContentHash      string `json:"sourceContentHash"`
	VolumeIndex            int    `json:"volumeIndex,omitempty"`
	SectionIndex           int    `json:"sectionIndex,omitempty"`
	VolumeTitle            string `json:"volumeTitle,omitempty"`
	ChapterTitle           string `json:"chapterTitle"`
	IsTarget               bool   `json:"isTarget"`
	BaseEpisodeID          string `json:"baseEpisodeId,omitempty"`
	BaseEpisodeRevision    int64  `json:"baseEpisodeRevision,omitempty"`
	BaseEpisodeContentHash string `json:"baseEpisodeContentHash,omitempty"`
}

type SourceToScriptGenerationManifest struct {
	SchemaVersion            int                               `json:"schemaVersion"`
	SourceID                 string                            `json:"sourceId"`
	SourceType               string                            `json:"sourceType"`
	SourceTitle              string                            `json:"sourceTitle"`
	SourceRevision           int64                             `json:"sourceRevision"`
	SourceContentHash        string                            `json:"sourceContentHash"`
	SourceSnapshotHash       string                            `json:"sourceSnapshotHash"`
	ScriptID                 string                            `json:"scriptId"`
	ExpectedActiveScriptID   string                            `json:"expectedActiveScriptId,omitempty"`
	ExpectedCurrentVersionID string                            `json:"expectedCurrentVersionId,omitempty"`
	ExpectedScriptRevision   int64                             `json:"expectedScriptRevision"`
	BaseScriptVersionID      string                            `json:"baseScriptVersionId,omitempty"`
	Prompt                   SourceToScriptPromptSnapshot      `json:"prompt"`
	Project                  ProjectProductionSettings         `json:"project"`
	ManualBindings           []AssetBatchManualBindingSnapshot `json:"manualBindings"`
	ModelBindings            []AssetBatchModelBindingSnapshot  `json:"modelBindings"`
	Items                    []SourceToScriptManifestItem      `json:"items"`
}

type sourceToScriptSnapshotItem struct {
	SourceToScriptManifestItem
	SourceContent string
}

type sourceToScriptSourceSnapshot struct {
	Source       ProjectSourceRecord
	Items        []sourceToScriptSnapshotItem
	SnapshotHash string
}

type sourceToScriptGenerationRecord struct {
	ID                       string
	RootGenerationID         string
	RetryOfGenerationID      string
	OrganizationID           string
	ProjectID                string
	WorkflowRunID            string
	AttemptGeneration        int
	SourceID                 string
	SourceType               string
	SourceRevision           int64
	SourceContentHash        string
	SourceSnapshotHash       string
	ScriptID                 string
	ExpectedActiveScriptID   string
	ExpectedCurrentVersionID string
	ExpectedScriptRevision   int64
	BaseScriptVersionID      string
	ResultScriptVersionID    string
	PromptTemplateKey        string
	PromptVersionID          string
	PromptContentHash        string
	ModelProfileKey          string
	ProviderModelID          string
	Status                   string
	Project                  ProjectProductionSettings
	Manifest                 SourceToScriptGenerationManifest
}

type sourceToScriptGenerationItemRecord struct {
	SourceToScriptManifestItem
	SourceContent string
}

type sourceToScriptBaseEpisode struct {
	ID              string
	SourceChapterID string
	EpisodeIndex    int
	VolumeIndex     int
	SectionIndex    int
	VolumeTitle     string
	EpisodeTitle    string
	Content         string
	ContentFormat   string
	PromptVersionID string
	PromptHash      string
	ProviderCallID  string
	ReviewStatus    string
	ManualOverride  bool
	StaleState      string
	Metadata        json.RawMessage
	CreatedBy       string
	Revision        int64
	ContentHash     string
}

func sourceToScriptTextHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func loadSourceToScriptSourceSnapshot(ctx context.Context, db sourceToScriptQueryer, projectID, sourceID string) (sourceToScriptSourceSnapshot, error) {
	var snapshot sourceToScriptSourceSnapshot
	if err := db.QueryRow(ctx, `
		SELECT id::text, source_type, title, content, content_format, COALESCE(status, 'ready'),
		       content_revision, content_hash
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID).Scan(
		&snapshot.Source.ID,
		&snapshot.Source.SourceType,
		&snapshot.Source.Title,
		&snapshot.Source.Content,
		&snapshot.Source.ContentFormat,
		&snapshot.Source.Status,
		&snapshot.Source.ContentRevision,
		&snapshot.Source.ContentHash,
	); err != nil {
		return sourceToScriptSourceSnapshot{}, err
	}
	if snapshot.Source.Status == "archived" {
		return sourceToScriptSourceSnapshot{}, workflowError{Code: provider.CodeInvalidRequest, Message: "archived source cannot be used for script generation"}
	}

	if snapshot.Source.SourceType == "novel" {
		rows, err := db.Query(ctx, `
			SELECT id::text, chapter_index, volume_index, section_index,
			       COALESCE(volume_title, ''), COALESCE(chapter_title, ''), content,
			       content_revision, content_hash
			FROM novel_chapters
			WHERE project_id = $1 AND source_id = $2
			ORDER BY COALESCE(volume_index, 0), COALESCE(section_index, chapter_index), chapter_index, id
		`, projectID, sourceID)
		if err != nil {
			return sourceToScriptSourceSnapshot{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var item sourceToScriptSnapshotItem
			var chapterIndex int
			var volumeIndex, sectionIndex sql.NullInt32
			if err := rows.Scan(
				&item.SourceChapterID,
				&chapterIndex,
				&volumeIndex,
				&sectionIndex,
				&item.VolumeTitle,
				&item.ChapterTitle,
				&item.SourceContent,
				&item.SourceRevision,
				&item.SourceContentHash,
			); err != nil {
				return sourceToScriptSourceSnapshot{}, err
			}
			item.ItemKey = item.SourceChapterID
			item.ManifestOrdinal = len(snapshot.Items) + 1
			if volumeIndex.Valid {
				item.VolumeIndex = int(volumeIndex.Int32)
			}
			if sectionIndex.Valid {
				item.SectionIndex = int(sectionIndex.Int32)
			}
			if strings.TrimSpace(item.ChapterTitle) == "" {
				item.ChapterTitle = "第 " + fmt.Sprint(chapterIndex) + " 节"
			}
			snapshot.Items = append(snapshot.Items, item)
		}
		if err := rows.Err(); err != nil {
			return sourceToScriptSourceSnapshot{}, err
		}
		if len(snapshot.Items) == 0 {
			return sourceToScriptSourceSnapshot{}, workflowError{Code: provider.CodeInvalidRequest, Message: "source has no chapters to generate"}
		}
	} else {
		snapshot.Items = []sourceToScriptSnapshotItem{{
			SourceToScriptManifestItem: SourceToScriptManifestItem{
				ItemKey: snapshot.Source.ID, ManifestOrdinal: 1,
				SourceRevision:    snapshot.Source.ContentRevision,
				SourceContentHash: snapshot.Source.ContentHash,
				ChapterTitle:      snapshot.Source.Title,
			},
			SourceContent: snapshot.Source.Content,
		}}
	}

	identity := struct {
		SourceID          string                       `json:"sourceId"`
		SourceType        string                       `json:"sourceType"`
		SourceTitle       string                       `json:"sourceTitle"`
		ContentFormat     string                       `json:"contentFormat"`
		SourceRevision    int64                        `json:"sourceRevision"`
		SourceContentHash string                       `json:"sourceContentHash"`
		Items             []SourceToScriptManifestItem `json:"items"`
	}{
		SourceID: snapshot.Source.ID, SourceType: snapshot.Source.SourceType,
		SourceTitle: snapshot.Source.Title, ContentFormat: snapshot.Source.ContentFormat,
		SourceRevision: snapshot.Source.ContentRevision, SourceContentHash: snapshot.Source.ContentHash,
		Items: make([]SourceToScriptManifestItem, 0, len(snapshot.Items)),
	}
	for _, item := range snapshot.Items {
		identity.Items = append(identity.Items, item.SourceToScriptManifestItem)
	}
	hash, err := videoproduction.HashCanonicalContract(identity)
	if err != nil {
		return sourceToScriptSourceSnapshot{}, err
	}
	snapshot.SnapshotHash = hash
	return snapshot, nil
}

func selectSourceToScriptTargets(snapshot *sourceToScriptSourceSnapshot, chapterIDs []string) ([]SourceToScriptChapterRef, error) {
	selected := normalizeStringSlice(chapterIDs)
	if snapshot.Source.SourceType != "novel" && len(selected) > 0 {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "chapterIds can only be used with novel sources"}
	}
	wanted := make(map[string]bool, len(selected))
	for _, id := range selected {
		wanted[id] = true
	}
	refs := make([]SourceToScriptChapterRef, 0, len(snapshot.Items))
	for index := range snapshot.Items {
		item := &snapshot.Items[index]
		item.IsTarget = len(wanted) == 0 || wanted[item.SourceChapterID]
		if !item.IsTarget {
			continue
		}
		delete(wanted, item.SourceChapterID)
		refs = append(refs, SourceToScriptChapterRef{
			ID: item.SourceChapterID, ItemKey: item.ItemKey, ManifestOrdinal: item.ManifestOrdinal,
			ChapterIndex: item.ManifestOrdinal, VolumeIndex: item.VolumeIndex, SectionIndex: item.SectionIndex,
			VolumeTitle: item.VolumeTitle, Title: item.ChapterTitle,
			ContentRevision: item.SourceRevision, ContentHash: item.SourceContentHash,
		})
	}
	if len(wanted) > 0 {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "chapterIds do not match source chapters"}
	}
	if len(refs) == 0 {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "source has no target chapters to generate"}
	}
	return refs, nil
}

func sourceToScriptManualBindings(ctx context.Context, db sourceToScriptQueryer, projectID string) ([]AssetBatchManualBindingSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT b.id::text, b.manual_kind, pt.template_key, pv.id::text, pv.content_hash
		FROM project_manual_bindings b
		JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
		WHERE b.project_id = $1 AND b.status = 'active' AND pv.status = 'active'
		ORDER BY b.manual_kind, b.updated_at DESC, b.id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AssetBatchManualBindingSnapshot, 0, 2)
	seen := map[string]bool{}
	for rows.Next() {
		var item AssetBatchManualBindingSnapshot
		if err := rows.Scan(&item.BindingID, &item.ManualKind, &item.TemplateKey, &item.PromptVersionID, &item.ContentHash); err != nil {
			return nil, err
		}
		if !seen[item.ManualKind] {
			seen[item.ManualKind] = true
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func sourceToScriptModelBindings(ctx context.Context, db sourceToScriptQueryer, organizationID, profileKey string) ([]AssetBatchModelBindingSnapshot, error) {
	rows, err := db.Query(ctx, `
		SELECT b.id::text, p.id::text, p.profile_key, m.id::text, m.model_key,
		       m.modality, b.priority, b.weight, m.updated_at
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE p.organization_id = $1 AND p.profile_key = $2
		  AND b.enabled = true AND m.status = 'active' AND a.status = 'active'
		ORDER BY b.priority ASC, b.weight DESC, b.created_at, b.id
	`, organizationID, profileKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AssetBatchModelBindingSnapshot, 0)
	for rows.Next() {
		var item AssetBatchModelBindingSnapshot
		var updatedAt time.Time
		if err := rows.Scan(
			&item.BindingID, &item.ProfileID, &item.ProfileKey, &item.ProviderModelID,
			&item.ModelKey, &item.Modality, &item.Priority, &item.Weight, &updatedAt,
		); err != nil {
			return nil, err
		}
		item.ModelUpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		items = append(items, item)
	}
	return items, rows.Err()
}

func sourceToScriptBaseEpisodes(ctx context.Context, db sourceToScriptQueryer, projectID, versionID, sourceID string) (map[string]sourceToScriptBaseEpisode, error) {
	items := map[string]sourceToScriptBaseEpisode{}
	if strings.TrimSpace(versionID) == "" {
		return items, nil
	}
	rows, err := db.Query(ctx, `
		SELECT id::text, COALESCE(source_chapter_id::text, ''), episode_index,
		       volume_index, section_index, COALESCE(volume_title, ''), episode_title,
		       content, content_format, COALESCE(prompt_version_id::text, ''), COALESCE(prompt_hash, ''),
		       COALESCE(provider_call_id::text, ''), review_status, manual_override, stale_state,
		       metadata, COALESCE(created_by::text, ''), revision, content_hash
		FROM script_episodes
		WHERE project_id = $1 AND script_version_id = $2
		ORDER BY episode_index, id
	`, projectID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item sourceToScriptBaseEpisode
		var volumeIndex, sectionIndex sql.NullInt32
		if err := rows.Scan(
			&item.ID, &item.SourceChapterID, &item.EpisodeIndex,
			&volumeIndex, &sectionIndex, &item.VolumeTitle, &item.EpisodeTitle,
			&item.Content, &item.ContentFormat, &item.PromptVersionID, &item.PromptHash,
			&item.ProviderCallID, &item.ReviewStatus, &item.ManualOverride, &item.StaleState,
			&item.Metadata, &item.CreatedBy, &item.Revision, &item.ContentHash,
		); err != nil {
			return nil, err
		}
		if volumeIndex.Valid {
			item.VolumeIndex = int(volumeIndex.Int32)
		}
		if sectionIndex.Valid {
			item.SectionIndex = int(sectionIndex.Int32)
		}
		key := item.SourceChapterID
		if key == "" && item.EpisodeIndex == 1 {
			key = sourceID
		}
		if key != "" {
			items[key] = item
		}
	}
	return items, rows.Err()
}

func sourceToScriptPromptKey(sourceType string) string {
	if sourceType == "brief" {
		return promptKeyBriefToScript
	}
	return promptKeyScriptAgentGenerate
}

func normalizePromptContentHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func sourceToScriptApplicationError(code, message string) error {
	return newWorkflowApplicationError(workflowError{Code: code, Message: message}, code, message)
}

func sourceToScriptPlanFromManifest(generationID, rootGenerationID string, attemptGeneration int, title string, manifest SourceToScriptGenerationManifest) (SourceToScriptPlan, error) {
	manifestHash, err := videoproduction.HashCanonicalContract(manifest)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	chapters := make([]SourceToScriptChapterRef, 0)
	for _, item := range manifest.Items {
		if !item.IsTarget {
			continue
		}
		chapters = append(chapters, SourceToScriptChapterRef{
			ID: item.SourceChapterID, ItemKey: item.ItemKey, ManifestOrdinal: item.ManifestOrdinal,
			ChapterIndex: item.ManifestOrdinal, VolumeIndex: item.VolumeIndex, SectionIndex: item.SectionIndex,
			VolumeTitle: item.VolumeTitle, Title: item.ChapterTitle,
			ContentRevision: item.SourceRevision, ContentHash: item.SourceContentHash,
		})
	}
	return SourceToScriptPlan{
		GenerationID: generationID, RootGenerationID: rootGenerationID, AttemptGeneration: attemptGeneration,
		SourceID: manifest.SourceID, SourceType: manifest.SourceType, SourceTitle: manifest.SourceTitle,
		SourceRevision: manifest.SourceRevision, SourceContentHash: manifest.SourceContentHash,
		SourceSnapshotHash: manifest.SourceSnapshotHash, ManifestHash: manifestHash,
		ScriptID: manifest.ScriptID, BaseScriptVersionID: manifest.BaseScriptVersionID,
		PreviousScriptVersionID: manifest.ExpectedCurrentVersionID,
		PreviousActiveScriptID:  manifest.ExpectedActiveScriptID,
		ExpectedScriptRevision:  manifest.ExpectedScriptRevision,
		PromptTemplateKey:       manifest.Prompt.TemplateKey, PromptVersionID: manifest.Prompt.VersionID,
		PromptContentHash: manifest.Prompt.ContentHash,
		ModelProfileKey:   manifest.Project.ScriptModelProfileKey,
		ProviderModelID:   firstSourceToScriptProviderModelID(manifest.ModelBindings, manifest.Project.ScriptModelProfileKey),
		Title:             title, EpisodeTotal: len(chapters), SeriesEpisodeTotal: len(manifest.Items), Chapters: chapters,
	}, nil
}

func firstSourceToScriptProviderModelID(bindings []AssetBatchModelBindingSnapshot, profileKey string) string {
	for _, binding := range bindings {
		if binding.ProfileKey == profileKey {
			return binding.ProviderModelID
		}
	}
	return ""
}

func renderSourceToScriptPromptSnapshot(ctx context.Context, tx pgx.Tx, organizationID, projectID, templateKey string) (promptsvc.ResolvedPrompt, error) {
	resolved, err := promptsvc.NewService(tx).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: organizationID, ProjectID: projectID, TemplateKey: templateKey,
	})
	if err != nil {
		return promptsvc.ResolvedPrompt{}, workflowErrorFromPrompt(err)
	}
	resolved.ContentHash = normalizePromptContentHash(resolved.ContentHash)
	return resolved, nil
}

type sourceToScriptParentGeneration struct {
	ID                       string
	RootGenerationID         string
	SourceID                 string
	SourceSnapshotHash       string
	ScriptID                 string
	ExpectedActiveScriptID   string
	ExpectedCurrentVersionID string
	ResultScriptVersionID    string
}

func loadSourceToScriptParentGeneration(ctx context.Context, tx pgx.Tx, workflowRunID string) (sourceToScriptParentGeneration, bool, error) {
	var item sourceToScriptParentGeneration
	err := tx.QueryRow(ctx, `
		SELECT generation.id::text,
		       COALESCE(generation.root_generation_id::text, generation.id::text),
		       generation.source_id::text,
		       generation.source_snapshot_hash,
		       generation.script_id::text,
		       COALESCE(generation.expected_active_script_id::text, ''),
		       COALESCE(generation.expected_current_version_id::text, ''),
		       COALESCE(generation.result_script_version_id::text, '')
		FROM workflow_runs run
		JOIN source_to_script_generations generation
		  ON generation.workflow_run_id = run.retry_of_workflow_run_id
		WHERE run.id = $1
		ORDER BY generation.attempt_generation DESC, generation.created_at DESC
		LIMIT 1
	`, workflowRunID).Scan(
		&item.ID, &item.RootGenerationID, &item.SourceID, &item.SourceSnapshotHash,
		&item.ScriptID, &item.ExpectedActiveScriptID, &item.ExpectedCurrentVersionID,
		&item.ResultScriptVersionID,
	)
	if err == pgx.ErrNoRows {
		return sourceToScriptParentGeneration{}, false, nil
	}
	if err != nil {
		return sourceToScriptParentGeneration{}, false, err
	}
	return item, true, nil
}

func lockSourceToScriptScript(ctx context.Context, tx pgx.Tx, projectID, sourceID, scriptID string) (title, currentVersionID, status string, revision int64, found bool, err error) {
	scriptID = strings.TrimSpace(scriptID)
	if scriptID == "" {
		return "", "", "", 0, false, nil
	}
	err = tx.QueryRow(ctx, `
		SELECT title, COALESCE(current_version_id::text, ''), status, revision
		FROM scripts
		WHERE project_id = $1 AND source_id = $2 AND id = $3
		  AND COALESCE(status, 'active') <> 'archived'
		FOR UPDATE
	`, projectID, sourceID, scriptID).Scan(&title, &currentVersionID, &status, &revision)
	if err == pgx.ErrNoRows {
		return "", "", "", 0, false, nil
	}
	return title, currentVersionID, status, revision, err == nil, err
}

func lockReusableSourceToScriptScript(ctx context.Context, tx pgx.Tx, projectID, sourceID, activeScriptID string) (scriptID, title, currentVersionID, status string, revision int64, found bool, err error) {
	if strings.TrimSpace(activeScriptID) != "" {
		title, currentVersionID, status, revision, found, err = lockSourceToScriptScript(ctx, tx, projectID, sourceID, activeScriptID)
		if err != nil || found {
			return activeScriptID, title, currentVersionID, status, revision, found, err
		}
	}
	err = tx.QueryRow(ctx, `
		SELECT id::text, title, COALESCE(current_version_id::text, ''), status, revision
		FROM scripts
		WHERE project_id = $1 AND source_id = $2
		  AND COALESCE(status, 'active') <> 'archived'
		ORDER BY CASE WHEN current_version_id IS NOT NULL THEN 0 ELSE 1 END,
		         updated_at DESC, created_at DESC, id
		LIMIT 1
		FOR UPDATE
	`, projectID, sourceID).Scan(&scriptID, &title, &currentVersionID, &status, &revision)
	if err == pgx.ErrNoRows {
		return "", "", "", "", 0, false, nil
	}
	return scriptID, title, currentVersionID, status, revision, err == nil, err
}

func (a Activities) prepareScriptFromSourceGeneration(ctx context.Context, input PrepareScriptFromSourceInput, execution NodeExecution) (SourceToScriptPlan, error) {
	projectSnapshot, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return SourceToScriptPlan{}, err
	}

	var attemptGeneration int
	if err := tx.QueryRow(ctx, `
		SELECT attempt_generation
		FROM workflow_runs
		WHERE id = $1 AND project_id = $2
		FOR UPDATE
	`, input.WorkflowRunID, input.ProjectID).Scan(&attemptGeneration); err != nil {
		return SourceToScriptPlan{}, err
	}
	var existingManifest json.RawMessage
	var existingGenerationID, existingRootGenerationID, existingTitle string
	err = tx.QueryRow(ctx, `
		SELECT generation.id::text,
		       COALESCE(generation.root_generation_id::text, generation.id::text),
		       generation.manifest,
		       script.title
		FROM source_to_script_generations generation
		JOIN scripts script ON script.id = generation.script_id
		WHERE generation.workflow_run_id = $1 AND generation.attempt_generation = $2
	`, input.WorkflowRunID, attemptGeneration).Scan(
		&existingGenerationID, &existingRootGenerationID, &existingManifest, &existingTitle,
	)
	if err == nil {
		var manifest SourceToScriptGenerationManifest
		if err := json.Unmarshal(existingManifest, &manifest); err != nil {
			return SourceToScriptPlan{}, err
		}
		return sourceToScriptPlanFromManifest(existingGenerationID, existingRootGenerationID, attemptGeneration, existingTitle, manifest)
	}
	if err != pgx.ErrNoRows {
		return SourceToScriptPlan{}, err
	}

	sourceSnapshot, err := loadSourceToScriptSourceSnapshot(ctx, tx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	targetRefs, err := selectSourceToScriptTargets(&sourceSnapshot, input.ChapterIDs)
	if err != nil {
		return SourceToScriptPlan{}, err
	}

	var activeScriptID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(active_script_id::text, '')
		FROM projects
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.ProjectID, input.OrganizationID).Scan(&activeScriptID); err != nil {
		return SourceToScriptPlan{}, err
	}

	parent, hasParent, err := loadSourceToScriptParentGeneration(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	if hasParent && (parent.SourceID != sourceSnapshot.Source.ID || parent.SourceSnapshotHash != sourceSnapshot.SnapshotHash) {
		return SourceToScriptPlan{}, sourceToScriptApplicationError(
			codeSourceToScriptReplanRequired,
			"原文或章节在上次生成后已变化，请重新创建剧本生成任务",
		)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(sourceSnapshot.Source.Title) + " 改编剧本"
	}
	var scriptID, currentVersionID, scriptStatus string
	var scriptRevision int64
	var found bool
	if hasParent {
		if strings.TrimSpace(input.TargetScriptID) != "" && input.TargetScriptID != parent.ScriptID {
			return SourceToScriptPlan{}, sourceToScriptApplicationError(provider.CodeInvalidRequest, "retry target script does not match the original generation")
		}
		scriptID = parent.ScriptID
		title, currentVersionID, scriptStatus, scriptRevision, found, err = lockSourceToScriptScript(ctx, tx, input.ProjectID, input.SourceID, scriptID)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		if !found {
			return SourceToScriptPlan{}, sourceToScriptApplicationError("SCRIPT_VERSION_CONFLICT", "原剧本已被删除或归档，请重新生成")
		}
	} else if strings.TrimSpace(input.TargetScriptID) != "" {
		scriptID = strings.TrimSpace(input.TargetScriptID)
		title, currentVersionID, scriptStatus, scriptRevision, found, err = lockSourceToScriptScript(ctx, tx, input.ProjectID, input.SourceID, scriptID)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		if !found {
			return SourceToScriptPlan{}, sourceToScriptApplicationError(provider.CodeInvalidRequest, "target script is not available for this source")
		}
	} else if !input.CreateNewScript {
		scriptID, title, currentVersionID, scriptStatus, scriptRevision, found, err = lockReusableSourceToScriptScript(ctx, tx, input.ProjectID, input.SourceID, activeScriptID)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
	}
	if scriptID == "" {
		title, err = uniqueScriptTitle(ctx, tx, input.ProjectID, title)
		if err != nil {
			return SourceToScriptPlan{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
			VALUES ($1, $2, $3, $4, 'draft', NULLIF($5, '')::uuid)
			RETURNING id::text, revision
		`, input.OrganizationID, input.ProjectID, input.SourceID, title, input.CreatedBy).Scan(&scriptID, &scriptRevision); err != nil {
			return SourceToScriptPlan{}, err
		}
		currentVersionID = ""
		scriptStatus = "draft"
	}
	_ = scriptStatus

	rootGenerationID := ""
	retryOfGenerationID := ""
	if hasParent {
		retryOfGenerationID = parent.ID
		switch {
		case parent.ResultScriptVersionID != "" && currentVersionID == parent.ResultScriptVersionID:
			// The previous partial version was activated. The retry starts from that immutable version.
		case currentVersionID == parent.ExpectedCurrentVersionID:
			rootGenerationID = parent.RootGenerationID
		default:
			return SourceToScriptPlan{}, sourceToScriptApplicationError(
				"SCRIPT_VERSION_CONFLICT",
				"剧本版本已发生变化，失败分集不能覆盖新的用户版本",
			)
		}
	}

	baseEpisodes, err := sourceToScriptBaseEpisodes(ctx, tx, input.ProjectID, currentVersionID, input.SourceID)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	for index := range sourceSnapshot.Items {
		item := &sourceSnapshot.Items[index]
		if base, ok := baseEpisodes[item.ItemKey]; ok {
			item.BaseEpisodeID = base.ID
			item.BaseEpisodeRevision = base.Revision
			item.BaseEpisodeContentHash = base.ContentHash
		}
	}

	templateKey := sourceToScriptPromptKey(sourceSnapshot.Source.SourceType)
	resolvedPrompt, err := renderSourceToScriptPromptSnapshot(ctx, tx, input.OrganizationID, input.ProjectID, templateKey)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	manualBindings, err := sourceToScriptManualBindings(ctx, tx, input.ProjectID)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	modelBindings, err := sourceToScriptModelBindings(ctx, tx, input.OrganizationID, projectSnapshot.ScriptModelProfileKey)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	providerModelID := firstSourceToScriptProviderModelID(modelBindings, projectSnapshot.ScriptModelProfileKey)
	if providerModelID == "" {
		return SourceToScriptPlan{}, sourceToScriptApplicationError("MODEL_PROFILE_NOT_CONFIGURED", "剧本业务模型没有可用的模型绑定")
	}

	var generationID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&generationID); err != nil {
		return SourceToScriptPlan{}, err
	}
	manifestItems := make([]SourceToScriptManifestItem, 0, len(sourceSnapshot.Items))
	for _, item := range sourceSnapshot.Items {
		manifestItems = append(manifestItems, item.SourceToScriptManifestItem)
	}
	manifest := SourceToScriptGenerationManifest{
		SchemaVersion: 1,
		SourceID:      sourceSnapshot.Source.ID, SourceType: sourceSnapshot.Source.SourceType,
		SourceTitle: sourceSnapshot.Source.Title, SourceRevision: sourceSnapshot.Source.ContentRevision,
		SourceContentHash: sourceSnapshot.Source.ContentHash, SourceSnapshotHash: sourceSnapshot.SnapshotHash,
		ScriptID: scriptID, ExpectedActiveScriptID: activeScriptID,
		ExpectedCurrentVersionID: currentVersionID, ExpectedScriptRevision: scriptRevision,
		BaseScriptVersionID: currentVersionID,
		Prompt: SourceToScriptPromptSnapshot{
			TemplateKey: resolvedPrompt.TemplateKey, VersionID: resolvedPrompt.VersionID,
			ContentHash: resolvedPrompt.ContentHash, Source: resolvedPrompt.Source,
		},
		Project: projectSnapshot, ManualBindings: manualBindings, ModelBindings: modelBindings,
		Items: manifestItems,
	}
	manifestRaw := mustJSON(manifest)
	manifestHash, err := videoproduction.HashCanonicalContract(manifest)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	projectRaw := mustJSON(projectSnapshot)
	manualRaw := mustJSON(manualBindings)
	modelRaw := mustJSON(modelBindings)
	if _, err := tx.Exec(ctx, `
		INSERT INTO source_to_script_generations(
			id, organization_id, project_id, workflow_run_id, root_generation_id, retry_of_generation_id,
			attempt_generation, source_id, source_type, source_revision, source_content_hash, source_snapshot_hash,
			script_id, expected_active_script_id, expected_current_version_id, expected_script_revision,
			base_script_version_id, prompt_template_key, prompt_version_id, prompt_content_hash,
			model_profile_key, provider_model_id, project_snapshot, manual_bindings, model_bindings,
			manifest, manifest_hash, status, idempotency_key, created_by
		)
		VALUES (
			$1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
			$7, $8, $9, $10, $11, $12,
			$13, NULLIF($14, '')::uuid, NULLIF($15, '')::uuid, $16,
			NULLIF($17, '')::uuid, $18, $19, $20,
			$21, $22, $23, $24, $25,
			$26, $27, 'prepared', NULLIF($28, ''), NULLIF($29, '')::uuid
		)
	`, generationID, input.OrganizationID, input.ProjectID, input.WorkflowRunID, rootGenerationID, retryOfGenerationID,
		attemptGeneration, input.SourceID, sourceSnapshot.Source.SourceType, sourceSnapshot.Source.ContentRevision,
		sourceSnapshot.Source.ContentHash, sourceSnapshot.SnapshotHash,
		scriptID, activeScriptID, currentVersionID, scriptRevision, currentVersionID,
		resolvedPrompt.TemplateKey, resolvedPrompt.VersionID, resolvedPrompt.ContentHash,
		projectSnapshot.ScriptModelProfileKey, providerModelID, projectRaw, manualRaw, modelRaw,
		manifestRaw, manifestHash, input.IdempotencyKey, input.CreatedBy); err != nil {
		return SourceToScriptPlan{}, err
	}
	for _, item := range sourceSnapshot.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO source_to_script_generation_items(
				generation_id, source_chapter_id, live_source_chapter_id, item_key, manifest_ordinal,
				source_revision, source_content_hash, volume_index, section_index, volume_title,
				chapter_title, source_content, is_target, base_episode_id, base_episode_revision,
				base_episode_content_hash
			)
			VALUES (
				$1, NULLIF($2, '')::uuid, NULLIF($2, '')::uuid, $3, $4,
				$5, $6, NULLIF($7, 0), NULLIF($8, 0), NULLIF($9, ''),
				$10, $11, $12, NULLIF($13, '')::uuid, NULLIF($14, 0), NULLIF($15, '')
			)
		`, generationID, item.SourceChapterID, item.ItemKey, item.ManifestOrdinal,
			item.SourceRevision, item.SourceContentHash, item.VolumeIndex, item.SectionIndex,
			item.VolumeTitle, item.ChapterTitle, item.SourceContent, item.IsTarget,
			item.BaseEpisodeID, item.BaseEpisodeRevision, item.BaseEpisodeContentHash); err != nil {
			return SourceToScriptPlan{}, err
		}
	}
	planRootGenerationID := rootGenerationID
	if planRootGenerationID == "" {
		planRootGenerationID = generationID
	}
	plan, err := sourceToScriptPlanFromManifest(generationID, planRootGenerationID, attemptGeneration, title, manifest)
	if err != nil {
		return SourceToScriptPlan{}, err
	}
	if len(targetRefs) != len(plan.Chapters) {
		return SourceToScriptPlan{}, fmt.Errorf("source-to-script target manifest count changed while preparing")
	}
	if err := queueSourceScriptEpisodeNodesTx(ctx, tx, input.GenerateScriptFromSourceInput, plan); err != nil {
		return SourceToScriptPlan{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET status = CASE WHEN status <> 'archived' THEN 'processing' ELSE status END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, input.ProjectID, input.SourceID); err != nil {
		return SourceToScriptPlan{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generation.prepared", "script", scriptID, mustJSON(map[string]any{
		"generationId": generationID, "rootGenerationId": planRootGenerationID,
		"scriptId": scriptID, "baseScriptVersionId": currentVersionID,
		"sourceId": input.SourceID, "sourceSnapshotHash": sourceSnapshot.SnapshotHash,
		"workflowRunId": input.WorkflowRunID, "attemptGeneration": attemptGeneration,
		"episodeTotal": len(plan.Chapters), "seriesEpisodeTotal": len(manifest.Items),
	})); err != nil {
		return SourceToScriptPlan{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(plan)); err != nil {
		return SourceToScriptPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptPlan{}, err
	}
	return plan, nil
}

func (a Activities) loadSourceToScriptGenerationItem(ctx context.Context, input GenerateSourceScriptEpisodeInput) (sourceToScriptGenerationRecord, sourceToScriptGenerationItemRecord, error) {
	var generation sourceToScriptGenerationRecord
	var projectRaw, manifestRaw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT id::text, COALESCE(root_generation_id::text, id::text), COALESCE(retry_of_generation_id::text, ''),
		       organization_id::text, project_id::text, workflow_run_id::text, attempt_generation,
		       source_id::text, source_type, source_revision, source_content_hash, source_snapshot_hash,
		       script_id::text, COALESCE(expected_active_script_id::text, ''),
		       COALESCE(expected_current_version_id::text, ''), expected_script_revision,
		       COALESCE(base_script_version_id::text, ''), COALESCE(result_script_version_id::text, ''),
		       prompt_template_key, COALESCE(prompt_version_id::text, ''), prompt_content_hash,
		       model_profile_key, COALESCE(provider_model_id::text, ''), status, project_snapshot, manifest
		FROM source_to_script_generations
		WHERE id = $1 AND project_id = $2
	`, input.GenerationID, input.ProjectID).Scan(
		&generation.ID, &generation.RootGenerationID, &generation.RetryOfGenerationID,
		&generation.OrganizationID, &generation.ProjectID, &generation.WorkflowRunID,
		&generation.AttemptGeneration, &generation.SourceID, &generation.SourceType,
		&generation.SourceRevision, &generation.SourceContentHash, &generation.SourceSnapshotHash,
		&generation.ScriptID, &generation.ExpectedActiveScriptID, &generation.ExpectedCurrentVersionID,
		&generation.ExpectedScriptRevision, &generation.BaseScriptVersionID, &generation.ResultScriptVersionID,
		&generation.PromptTemplateKey, &generation.PromptVersionID, &generation.PromptContentHash,
		&generation.ModelProfileKey, &generation.ProviderModelID, &generation.Status, &projectRaw, &manifestRaw,
	)
	if err != nil {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, err
	}
	if generation.OrganizationID != input.OrganizationID || generation.WorkflowRunID != input.WorkflowRunID ||
		generation.AttemptGeneration != input.AttemptGeneration || generation.SourceID != input.SourceID ||
		generation.ScriptID != input.ScriptID {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, ErrWorkflowWriteFenced
	}
	if generation.Status != "prepared" && generation.Status != "running" {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, ErrWorkflowWriteFenced
	}
	if err := json.Unmarshal(projectRaw, &generation.Project); err != nil {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, err
	}
	if err := json.Unmarshal(manifestRaw, &generation.Manifest); err != nil {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, err
	}

	itemKey := strings.TrimSpace(input.ItemKey)
	if itemKey == "" {
		itemKey = strings.TrimSpace(input.Chapter.ID)
	}
	if itemKey == "" {
		itemKey = input.SourceID
	}
	var item sourceToScriptGenerationItemRecord
	var volumeIndex, sectionIndex sql.NullInt32
	err = a.db.QueryRow(ctx, `
		SELECT item_key, COALESCE(source_chapter_id::text, ''), manifest_ordinal,
		       source_revision, source_content_hash, volume_index, section_index,
		       COALESCE(volume_title, ''), chapter_title, source_content, is_target,
		       COALESCE(base_episode_id::text, ''), COALESCE(base_episode_revision, 0),
		       COALESCE(base_episode_content_hash, '')
		FROM source_to_script_generation_items
		WHERE generation_id = $1 AND item_key = $2 AND is_target = true
	`, generation.ID, itemKey).Scan(
		&item.ItemKey, &item.SourceChapterID, &item.ManifestOrdinal,
		&item.SourceRevision, &item.SourceContentHash, &volumeIndex, &sectionIndex,
		&item.VolumeTitle, &item.ChapterTitle, &item.SourceContent, &item.IsTarget,
		&item.BaseEpisodeID, &item.BaseEpisodeRevision, &item.BaseEpisodeContentHash,
	)
	if err != nil {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, err
	}
	if volumeIndex.Valid {
		item.VolumeIndex = int(volumeIndex.Int32)
	}
	if sectionIndex.Valid {
		item.SectionIndex = int(sectionIndex.Int32)
	}
	if strings.TrimSpace(input.Chapter.ID) != "" && item.SourceChapterID != input.Chapter.ID {
		return sourceToScriptGenerationRecord{}, sourceToScriptGenerationItemRecord{}, ErrWorkflowWriteFenced
	}
	return generation, item, nil
}

func (a Activities) ensureSourceToScriptGenerationItemCurrent(ctx context.Context, generation sourceToScriptGenerationRecord, item sourceToScriptGenerationItemRecord) error {
	var sourceRevision int64
	var sourceHash, sourceStatus string
	if err := a.db.QueryRow(ctx, `
		SELECT content_revision, content_hash, status
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, generation.ProjectID, generation.SourceID).Scan(&sourceRevision, &sourceHash, &sourceStatus); err != nil {
		return sourceToScriptApplicationError(codeSourceToScriptReplanRequired, "原文已被删除，请重新创建剧本生成任务")
	}
	if sourceStatus == "archived" || sourceRevision != generation.SourceRevision || sourceHash != generation.SourceContentHash {
		return sourceToScriptApplicationError(codeSourceToScriptReplanRequired, "原文在生成期间已变化，请重新创建剧本生成任务")
	}
	if item.SourceChapterID == "" {
		return nil
	}
	var revision int64
	var contentHash string
	if err := a.db.QueryRow(ctx, `
		SELECT content_revision, content_hash
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2 AND id = $3
	`, generation.ProjectID, generation.SourceID, item.SourceChapterID).Scan(&revision, &contentHash); err != nil {
		return sourceToScriptApplicationError(codeSourceToScriptReplanRequired, "目标章节已被删除，请重新创建剧本生成任务")
	}
	if revision != item.SourceRevision || contentHash != item.SourceContentHash {
		return sourceToScriptApplicationError(codeSourceToScriptReplanRequired, "目标章节在生成期间已变化，请重新创建剧本生成任务")
	}
	return nil
}

func (a Activities) completedSourceScriptGenerationResult(ctx context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, bool, error) {
	itemKey := firstNonEmptyString(strings.TrimSpace(input.ItemKey), strings.TrimSpace(input.Chapter.ID), input.SourceID)
	var output SourceScriptEpisodeOutput
	err := a.db.QueryRow(ctx, `
		SELECT result.id::text, result.source_id::text, COALESCE(result.source_chapter_id::text, ''),
		       result.generation_id::text, generation.script_id::text,
		       item.manifest_ordinal, COALESCE(result.episode_title, ''), COALESCE(result.content, ''),
		       COALESCE(result.agent_run_id::text, ''), COALESCE(result.provider_call_id::text, ''),
		       COALESCE(result.provider_model_id::text, ''), COALESCE(result.prompt_version_id::text, ''),
		       COALESCE(result.prompt_hash, '')
		FROM script_episode_generation_results result
		JOIN source_to_script_generations generation ON generation.id = result.generation_id
		JOIN source_to_script_generation_items item
		  ON item.generation_id = result.generation_id AND item.item_key = result.item_key
		WHERE result.workflow_run_id = $1 AND result.attempt_generation = $2
		  AND result.item_key = $3 AND result.status = 'succeeded'
	`, input.WorkflowRunID, input.AttemptGeneration, itemKey).Scan(
		&output.GenerationResultID, &output.SourceID, &output.SourceChapterID,
		&output.GenerationID, &output.ScriptID, &output.EpisodeIndex,
		&output.EpisodeTitle, &output.Content, &output.AgentRunID, &output.ProviderCallID,
		&output.ModelID, &output.PromptVersionID, &output.PromptHash,
	)
	if err == pgx.ErrNoRows {
		return SourceScriptEpisodeOutput{}, false, nil
	}
	if err != nil {
		return SourceScriptEpisodeOutput{}, false, err
	}
	output.Skipped = true
	return output, true, nil
}

func (a Activities) storeSourceScriptGenerationResult(
	ctx context.Context,
	input GenerateSourceScriptEpisodeInput,
	generation sourceToScriptGenerationRecord,
	item sourceToScriptGenerationItemRecord,
	execution NodeExecution,
	rendered promptsvc.RenderedPrompt,
	gatewayResp provider.GatewayTextResponse,
	agentRunID, content string,
) (SourceScriptEpisodeOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	content = strings.TrimSpace(content)
	contentHash := sourceToScriptTextHash(content)
	providerModelID := firstNonEmptyString(gatewayResp.ModelID, generation.ProviderModelID)
	provenance := mustJSON(map[string]any{
		"generationId": generation.ID, "rootGenerationId": generation.RootGenerationID,
		"sourceSnapshotHash": generation.SourceSnapshotHash,
		"sourceRevision":     item.SourceRevision, "sourceContentHash": item.SourceContentHash,
		"promptTemplateKey": rendered.TemplateKey, "promptVersionId": rendered.PromptVersionID,
		"promptHash": rendered.RenderedHash, "promptSource": rendered.Source,
		"providerCallId": gatewayResp.ProviderCallID, "providerModelId": providerModelID,
		"agentRunId": agentRunID,
	})
	var resultID string
	err = tx.QueryRow(ctx, `
		INSERT INTO script_episode_generation_results(
			organization_id, project_id, workflow_run_id, generation_id, attempt_generation,
			source_id, source_chapter_id, item_key, status, episode_title, content, content_hash,
			prompt_template_key, prompt_version_id, prompt_hash, prompt_source,
			provider_call_id, provider_model_id, agent_run_id, provenance
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, NULLIF($7, '')::uuid, $8, 'succeeded', $9, $10, $11,
			$12, NULLIF($13, '')::uuid, $14, $15,
			NULLIF($16, '')::uuid, NULLIF($17, '')::uuid, NULLIF($18, '')::uuid, $19
		)
		ON CONFLICT (workflow_run_id, attempt_generation, item_key) DO NOTHING
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, generation.ID, input.AttemptGeneration,
		input.SourceID, item.SourceChapterID, item.ItemKey, item.ChapterTitle, content, contentHash,
		rendered.TemplateKey, rendered.PromptVersionID, rendered.RenderedHash, rendered.Source,
		gatewayResp.ProviderCallID, providerModelID, agentRunID, provenance).Scan(&resultID)
	if err == pgx.ErrNoRows {
		var status string
		if err := tx.QueryRow(ctx, `
			SELECT id::text, status
			FROM script_episode_generation_results
			WHERE workflow_run_id = $1 AND attempt_generation = $2 AND item_key = $3
		`, input.WorkflowRunID, input.AttemptGeneration, item.ItemKey).Scan(&resultID, &status); err != nil {
			return SourceScriptEpisodeOutput{}, err
		}
		if status != "succeeded" {
			return SourceScriptEpisodeOutput{}, sourceToScriptApplicationError("SOURCE_SCRIPT_RESULT_CONFLICT", "分集生成结果已进入失败终态，请通过失败重试重新生成")
		}
	} else if err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	output := SourceScriptEpisodeOutput{
		SourceID: input.SourceID, SourceChapterID: item.SourceChapterID,
		ScriptID: input.ScriptID, GenerationID: generation.ID, GenerationResultID: resultID,
		EpisodeIndex: item.ManifestOrdinal, EpisodeTitle: item.ChapterTitle,
		AgentRunID: agentRunID, ProviderCallID: gatewayResp.ProviderCallID, ModelID: providerModelID,
		PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash, Content: content,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE source_to_script_generations
		SET status = CASE WHEN status = 'prepared' THEN 'running' ELSE status END
		WHERE id = $1 AND status IN ('prepared', 'running')
	`, generation.ID); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''), completed_at = now()
		WHERE id = $1
	`, agentRunID, mustJSON(output), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.episode.generation.staged", "script_episode_generation_result", resultID, mustJSON(map[string]any{
		"generationId": generation.ID, "scriptId": input.ScriptID, "sourceId": input.SourceID,
		"sourceChapterId": item.SourceChapterID, "manifestOrdinal": item.ManifestOrdinal,
		"workflowRunId": input.WorkflowRunID, "attemptGeneration": input.AttemptGeneration,
		"agentRunId": agentRunID, "providerCallId": gatewayResp.ProviderCallID, "staged": true,
	})); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	checkpoint := output
	checkpoint.Content = ""
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(checkpoint)); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceScriptEpisodeOutput{}, err
	}
	return output, nil
}

func storeSourceScriptGenerationFailureTx(ctx context.Context, tx pgx.Tx, episode GenerateSourceScriptEpisodeInput, code, message string) error {
	itemKey := firstNonEmptyString(strings.TrimSpace(episode.ItemKey), strings.TrimSpace(episode.Chapter.ID), episode.SourceID)
	var sourceChapterID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(source_chapter_id::text, '')
		FROM source_to_script_generation_items
		WHERE generation_id = $1 AND item_key = $2 AND is_target = true
	`, episode.GenerationID, itemKey).Scan(&sourceChapterID); err != nil {
		return err
	}
	provenance := mustJSON(map[string]any{
		"generationId": episode.GenerationID, "sourceChapterId": sourceChapterID,
		"attemptGeneration": episode.AttemptGeneration,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO script_episode_generation_results(
			organization_id, project_id, workflow_run_id, generation_id, attempt_generation,
			source_id, source_chapter_id, item_key, status, error_code, error_message, provenance
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, NULLIF($7, '')::uuid, $8, 'failed', $9, $10, $11
		)
		ON CONFLICT (workflow_run_id, attempt_generation, item_key) DO NOTHING
	`, episode.OrganizationID, episode.ProjectID, episode.WorkflowRunID, episode.GenerationID,
		episode.AttemptGeneration, episode.SourceID, sourceChapterID, itemKey, code, message, provenance)
	return err
}

type sourceToScriptStagedResult struct {
	ID              string
	ItemKey         string
	Status          string
	EpisodeTitle    string
	Content         string
	ContentHash     string
	ErrorCode       string
	ErrorMessage    string
	PromptVersionID string
	PromptHash      string
	ProviderCallID  string
	ProviderModelID string
	AgentRunID      string
	Provenance      json.RawMessage
}

type sourceToScriptAssembledEpisode struct {
	Manifest SourceToScriptManifestItem
	Result   *sourceToScriptStagedResult
	Fallback *sourceToScriptBaseEpisode
	Stale    bool
}

func loadSourceToScriptGenerationForUpdate(ctx context.Context, tx pgx.Tx, generationID, workflowRunID string) (sourceToScriptGenerationRecord, error) {
	var generation sourceToScriptGenerationRecord
	var projectRaw, manifestRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(root_generation_id::text, id::text), COALESCE(retry_of_generation_id::text, ''),
		       organization_id::text, project_id::text, workflow_run_id::text, attempt_generation,
		       source_id::text, source_type, source_revision, source_content_hash, source_snapshot_hash,
		       script_id::text, COALESCE(expected_active_script_id::text, ''),
		       COALESCE(expected_current_version_id::text, ''), expected_script_revision,
		       COALESCE(base_script_version_id::text, ''), COALESCE(result_script_version_id::text, ''),
		       prompt_template_key, COALESCE(prompt_version_id::text, ''), prompt_content_hash,
		       model_profile_key, COALESCE(provider_model_id::text, ''), status, project_snapshot, manifest
		FROM source_to_script_generations
		WHERE id = $1 AND workflow_run_id = $2
		FOR UPDATE
	`, generationID, workflowRunID).Scan(
		&generation.ID, &generation.RootGenerationID, &generation.RetryOfGenerationID,
		&generation.OrganizationID, &generation.ProjectID, &generation.WorkflowRunID,
		&generation.AttemptGeneration, &generation.SourceID, &generation.SourceType,
		&generation.SourceRevision, &generation.SourceContentHash, &generation.SourceSnapshotHash,
		&generation.ScriptID, &generation.ExpectedActiveScriptID, &generation.ExpectedCurrentVersionID,
		&generation.ExpectedScriptRevision, &generation.BaseScriptVersionID, &generation.ResultScriptVersionID,
		&generation.PromptTemplateKey, &generation.PromptVersionID, &generation.PromptContentHash,
		&generation.ModelProfileKey, &generation.ProviderModelID, &generation.Status, &projectRaw, &manifestRaw,
	); err != nil {
		return sourceToScriptGenerationRecord{}, err
	}
	if err := json.Unmarshal(projectRaw, &generation.Project); err != nil {
		return sourceToScriptGenerationRecord{}, err
	}
	if err := json.Unmarshal(manifestRaw, &generation.Manifest); err != nil {
		return sourceToScriptGenerationRecord{}, err
	}
	return generation, nil
}

func loadSourceToScriptLatestResults(ctx context.Context, tx pgx.Tx, generation sourceToScriptGenerationRecord) (map[string]sourceToScriptStagedResult, map[string]bool, error) {
	rows, err := tx.Query(ctx, `
		WITH chain AS (
			SELECT id, attempt_generation
			FROM source_to_script_generations
			WHERE COALESCE(root_generation_id, id) = $1
			  AND source_snapshot_hash = $2
		), ranked AS (
			SELECT result.id::text AS id, result.item_key, result.status,
			       COALESCE(result.episode_title, '') AS episode_title,
			       COALESCE(result.content, '') AS content,
			       COALESCE(result.content_hash, '') AS content_hash,
			       COALESCE(result.error_code, '') AS error_code,
			       COALESCE(result.error_message, '') AS error_message,
			       COALESCE(result.prompt_version_id::text, '') AS prompt_version_id,
			       COALESCE(result.prompt_hash, '') AS prompt_hash,
			       COALESCE(result.provider_call_id::text, '') AS provider_call_id,
			       COALESCE(result.provider_model_id::text, '') AS provider_model_id,
			       COALESCE(result.agent_run_id::text, '') AS agent_run_id,
			       result.provenance,
			       row_number() OVER (
			         PARTITION BY result.item_key
			         ORDER BY chain.attempt_generation DESC, result.updated_at DESC, result.id DESC
			       ) AS rank
			FROM chain
			JOIN script_episode_generation_results result ON result.generation_id = chain.id
		)
		SELECT id, item_key, status, episode_title, content, content_hash, error_code,
		       error_message, prompt_version_id, prompt_hash, provider_call_id,
		       provider_model_id, agent_run_id, provenance
		FROM ranked
		WHERE rank = 1
	`, generation.RootGenerationID, generation.SourceSnapshotHash)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	results := map[string]sourceToScriptStagedResult{}
	for rows.Next() {
		var item sourceToScriptStagedResult
		if err := rows.Scan(
			&item.ID, &item.ItemKey, &item.Status, &item.EpisodeTitle, &item.Content,
			&item.ContentHash, &item.ErrorCode, &item.ErrorMessage, &item.PromptVersionID,
			&item.PromptHash, &item.ProviderCallID, &item.ProviderModelID, &item.AgentRunID,
			&item.Provenance,
		); err != nil {
			return nil, nil, err
		}
		results[item.ItemKey] = item
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	targetRows, err := tx.Query(ctx, `
		SELECT DISTINCT item.item_key
		FROM source_to_script_generations generation
		JOIN source_to_script_generation_items item ON item.generation_id = generation.id
		WHERE COALESCE(generation.root_generation_id, generation.id) = $1
		  AND generation.source_snapshot_hash = $2
		  AND item.is_target = true
	`, generation.RootGenerationID, generation.SourceSnapshotHash)
	if err != nil {
		return nil, nil, err
	}
	defer targetRows.Close()
	targets := map[string]bool{}
	for targetRows.Next() {
		var key string
		if err := targetRows.Scan(&key); err != nil {
			return nil, nil, err
		}
		targets[key] = true
	}
	return results, targets, targetRows.Err()
}

func sourceToScriptFailureOrdinals(manifest SourceToScriptGenerationManifest, targets map[string]bool, results map[string]sourceToScriptStagedResult) []int {
	failed := make([]int, 0)
	for _, item := range manifest.Items {
		if !targets[item.ItemKey] {
			continue
		}
		result, ok := results[item.ItemKey]
		if !ok || result.Status != "succeeded" {
			failed = append(failed, item.ManifestOrdinal)
		}
	}
	return failed
}

func finalizeSourceToScriptGenerationFailureTx(
	ctx context.Context,
	tx pgx.Tx,
	execution NodeExecution,
	generation sourceToScriptGenerationRecord,
	code, message string,
) error {
	retentionDeadline := sourceToScriptRetentionDeadline()
	if _, err := tx.Exec(ctx, `
		UPDATE source_to_script_generations
		SET status = 'replan_required', error_code = $2, error_message = $3,
		    finalized_at = now(), retention_expires_at = $4
		WHERE id = $1
	`, generation.ID, code, message, retentionDeadline); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET status = CASE WHEN status <> 'archived' THEN 'ready' ELSE status END, updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, generation.ProjectID, generation.SourceID); err != nil {
		return err
	}
	_, err := failNodeRunTx(ctx, tx, execution, code, message, mustJSON(map[string]any{
		"generationId": generation.ID, "sourceId": generation.SourceID,
		"sourceSnapshotHash": generation.SourceSnapshotHash,
		"errorCode":          code, "errorMessage": message,
	}))
	return err
}

func (a Activities) finalizeScriptFromSourceGeneration(
	ctx context.Context,
	input GenerateScriptFromSourceInput,
	plan SourceToScriptPlan,
	finalization SourceToScriptFinalization,
	execution NodeExecution,
) (SourceToScriptOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return SourceToScriptOutput{}, err
	}
	generation, err := loadSourceToScriptGenerationForUpdate(ctx, tx, plan.GenerationID, input.WorkflowRunID)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	if generation.OrganizationID != input.OrganizationID || generation.ProjectID != input.ProjectID ||
		generation.AttemptGeneration != plan.AttemptGeneration || generation.Manifest.SourceID != input.SourceID {
		return SourceToScriptOutput{}, ErrWorkflowWriteFenced
	}
	if generation.ResultScriptVersionID != "" && (generation.Status == "succeeded" || generation.Status == "partial_succeeded") {
		return SourceToScriptOutput{}, ErrWorkflowWriteFenced
	}
	if _, err := tx.Exec(ctx, `
		UPDATE source_to_script_generations SET status = 'finalizing' WHERE id = $1
	`, generation.ID); err != nil {
		return SourceToScriptOutput{}, err
	}

	currentSource, err := loadSourceToScriptSourceSnapshot(ctx, tx, generation.ProjectID, generation.SourceID)
	if err != nil || currentSource.SnapshotHash != generation.SourceSnapshotHash ||
		currentSource.Source.ContentRevision != generation.SourceRevision ||
		currentSource.Source.ContentHash != generation.SourceContentHash {
		message := "原文或章节在生成期间已变化，生成结果未写入正式剧本版本"
		if persistErr := finalizeSourceToScriptGenerationFailureTx(ctx, tx, execution, generation, codeSourceToScriptReplanRequired, message); persistErr != nil {
			return SourceToScriptOutput{}, persistErr
		}
		if err := tx.Commit(ctx); err != nil {
			return SourceToScriptOutput{}, err
		}
		return SourceToScriptOutput{}, sourceToScriptApplicationError(codeSourceToScriptReplanRequired, message)
	}

	var activeScriptID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(active_script_id::text, '')
		FROM projects
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, generation.ProjectID, generation.OrganizationID).Scan(&activeScriptID); err != nil {
		return SourceToScriptOutput{}, err
	}
	var currentVersionID string
	var scriptRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(current_version_id::text, ''), revision
		FROM scripts
		WHERE id = $1 AND project_id = $2 AND COALESCE(status, 'active') <> 'archived'
		FOR UPDATE
	`, generation.ScriptID, generation.ProjectID).Scan(&currentVersionID, &scriptRevision); err != nil {
		return SourceToScriptOutput{}, err
	}
	if activeScriptID != generation.ExpectedActiveScriptID || currentVersionID != generation.ExpectedCurrentVersionID ||
		scriptRevision != generation.ExpectedScriptRevision {
		message := "剧本或项目当前版本已变化，生成结果未覆盖新的用户版本"
		if err := finalizeSourceToScriptGenerationFailureTx(ctx, tx, execution, generation, "SCRIPT_VERSION_CONFLICT", message); err != nil {
			return SourceToScriptOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SourceToScriptOutput{}, err
		}
		return SourceToScriptOutput{}, sourceToScriptApplicationError("SCRIPT_VERSION_CONFLICT", message)
	}

	baseEpisodes, err := sourceToScriptBaseEpisodes(ctx, tx, generation.ProjectID, generation.BaseScriptVersionID, generation.SourceID)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	for _, item := range generation.Manifest.Items {
		if item.BaseEpisodeID == "" {
			continue
		}
		base, ok := baseEpisodes[item.ItemKey]
		if !ok || base.ID != item.BaseEpisodeID || base.Revision != item.BaseEpisodeRevision || base.ContentHash != item.BaseEpisodeContentHash {
			message := "基础剧本分集已变化，生成结果未写入正式版本"
			if err := finalizeSourceToScriptGenerationFailureTx(ctx, tx, execution, generation, "SCRIPT_VERSION_CONFLICT", message); err != nil {
				return SourceToScriptOutput{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return SourceToScriptOutput{}, err
			}
			return SourceToScriptOutput{}, sourceToScriptApplicationError("SCRIPT_VERSION_CONFLICT", message)
		}
	}

	results, targets, err := loadSourceToScriptLatestResults(ctx, tx, generation)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	assembled := make([]sourceToScriptAssembledEpisode, 0, len(generation.Manifest.Items))
	succeededTargets, failedTargets, missingTargets := 0, 0, 0
	providerCallIDs := make([]string, 0)
	modelIDs := make([]string, 0)
	for _, item := range generation.Manifest.Items {
		result, hasResult := results[item.ItemKey]
		base, hasBase := baseEpisodes[item.ItemKey]
		if targets[item.ItemKey] {
			if hasResult && result.Status == "succeeded" {
				resultCopy := result
				assembled = append(assembled, sourceToScriptAssembledEpisode{Manifest: item, Result: &resultCopy})
				succeededTargets++
				providerCallIDs = appendUniqueString(providerCallIDs, result.ProviderCallID)
				modelIDs = appendUniqueString(modelIDs, result.ProviderModelID)
				continue
			}
			failedTargets++
			if hasBase {
				baseCopy := base
				assembled = append(assembled, sourceToScriptAssembledEpisode{Manifest: item, Fallback: &baseCopy, Stale: true})
			} else {
				missingTargets++
			}
			continue
		}
		if hasBase {
			baseCopy := base
			assembled = append(assembled, sourceToScriptAssembledEpisode{Manifest: item, Fallback: &baseCopy})
		}
	}
	failedEpisodes := sourceToScriptFailureOrdinals(generation.Manifest, targets, results)

	status := "failed"
	activated := false
	projectActivated := false
	versionID := ""
	content := ""
	if succeededTargets > 0 {
		status = "succeeded"
		if failedTargets > 0 {
			status = "partial_succeeded"
		}
		versionStatus := "active"
		if missingTargets > 0 {
			versionStatus = "partial"
		}
		var nextVersion int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(COALESCE(version, version_no)), 0) + 1
			FROM script_versions
			WHERE script_id = $1
		`, generation.ScriptID).Scan(&nextVersion); err != nil {
			return SourceToScriptOutput{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO script_versions(
				organization_id, project_id, script_id, version_no, version, content,
				content_format, status, source_type, prompt_version_id, metadata, created_by,
				source_to_script_generation_id, generation_workflow_run_id, generation_attempt_generation
			)
			VALUES (
				$1, $2, $3, $4, $4, '',
				'markdown', $5, 'agent_generated', NULLIF($6, '')::uuid, $7, NULLIF($8, '')::uuid,
				$9, $10, $11
			)
			RETURNING id::text
		`, generation.OrganizationID, generation.ProjectID, generation.ScriptID, nextVersion,
			versionStatus, generation.PromptVersionID, mustJSON(map[string]any{
				"source": "source_to_script", "sourceId": generation.SourceID,
				"generationId": generation.ID, "rootGenerationId": generation.RootGenerationID,
				"workflowRunId": generation.WorkflowRunID, "attemptGeneration": generation.AttemptGeneration,
				"sourceSnapshotHash": generation.SourceSnapshotHash, "manifestHash": plan.ManifestHash,
				"generationStatus": status, "failedEpisodes": failedEpisodes,
				"missingTargetCount": missingTargets,
			}), input.CreatedBy, generation.ID, generation.WorkflowRunID, generation.AttemptGeneration).Scan(&versionID); err != nil {
			return SourceToScriptOutput{}, err
		}
		for episodeIndex, episode := range assembled {
			manifestItem := episode.Manifest
			episodeTitle := manifestItem.ChapterTitle
			episodeContent := ""
			promptVersionID, promptHash, providerCallID, generationResultID := "", "", "", ""
			reviewStatus := "pending"
			manualOverride := false
			staleState := "fresh"
			metadata := map[string]any{
				"source": "source_to_script", "generationId": generation.ID,
				"sourceSnapshotHash": generation.SourceSnapshotHash,
				"sourceChapterId":    manifestItem.SourceChapterID,
			}
			createdBy := input.CreatedBy
			if episode.Result != nil {
				episodeTitle = firstNonEmptyString(episode.Result.EpisodeTitle, episodeTitle)
				episodeContent = episode.Result.Content
				promptVersionID = episode.Result.PromptVersionID
				promptHash = episode.Result.PromptHash
				providerCallID = episode.Result.ProviderCallID
				generationResultID = episode.Result.ID
				metadata["provenance"] = json.RawMessage(episode.Result.Provenance)
			} else if episode.Fallback != nil {
				episodeTitle = firstNonEmptyString(episode.Fallback.EpisodeTitle, episodeTitle)
				episodeContent = episode.Fallback.Content
				promptVersionID = episode.Fallback.PromptVersionID
				promptHash = episode.Fallback.PromptHash
				providerCallID = episode.Fallback.ProviderCallID
				reviewStatus = episode.Fallback.ReviewStatus
				manualOverride = episode.Fallback.ManualOverride
				createdBy = firstNonEmptyString(episode.Fallback.CreatedBy, input.CreatedBy)
				metadata["copiedFromVersionId"] = generation.BaseScriptVersionID
				metadata["copiedFromEpisodeId"] = episode.Fallback.ID
				if episode.Stale {
					staleState = "needs_regeneration"
					metadata["generationFallback"] = true
					if failed, ok := results[manifestItem.ItemKey]; ok {
						metadata["generationErrorCode"] = failed.ErrorCode
						metadata["generationErrorMessage"] = failed.ErrorMessage
					}
				} else {
					staleState = episode.Fallback.StaleState
				}
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO script_episodes(
					organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
					episode_index, volume_index, section_index, volume_title, episode_title, content,
					content_format, prompt_version_id, prompt_hash, provider_call_id, review_status,
					manual_override, stale_state, metadata, created_by, generation_result_id
				)
				VALUES (
					$1, $2, $3, $4, $5, NULLIF($6, '')::uuid,
					$7, NULLIF($8, 0), NULLIF($9, 0), NULLIF($10, ''), $11, $12,
					'markdown', NULLIF($13, '')::uuid, NULLIF($14, ''), NULLIF($15, '')::uuid, $16,
					$17, $18, $19, NULLIF($20, '')::uuid, NULLIF($21, '')::uuid
				)
			`, generation.OrganizationID, generation.ProjectID, generation.ScriptID, versionID,
				generation.SourceID, manifestItem.SourceChapterID, episodeIndex+1,
				manifestItem.VolumeIndex, manifestItem.SectionIndex, manifestItem.VolumeTitle,
				episodeTitle, episodeContent, promptVersionID, promptHash, providerCallID,
				reviewStatus, manualOverride, staleState, mustJSON(metadata), createdBy, generationResultID); err != nil {
				return SourceToScriptOutput{}, err
			}
		}
		content, err = a.rebuildScriptVersionContentTx(ctx, tx, generation.ProjectID, versionID)
		if err != nil {
			return SourceToScriptOutput{}, err
		}
		if missingTargets == 0 {
			tag, err := tx.Exec(ctx, `
				UPDATE scripts
				SET current_version_id = $3, status = 'active', updated_at = now()
				WHERE id = $1 AND project_id = $2 AND revision = $4
				  AND current_version_id IS NOT DISTINCT FROM NULLIF($5, '')::uuid
			`, generation.ScriptID, generation.ProjectID, versionID,
				generation.ExpectedScriptRevision, generation.ExpectedCurrentVersionID)
			if err != nil {
				return SourceToScriptOutput{}, err
			}
			if tag.RowsAffected() != 1 {
				return SourceToScriptOutput{}, sourceToScriptApplicationError("SCRIPT_VERSION_CONFLICT", "剧本版本激活 CAS 失败")
			}
			activated = true
			projectTag, err := tx.Exec(ctx, `
				UPDATE projects
				SET active_script_id = $3, updated_at = now()
				WHERE id = $1 AND organization_id = $2
				  AND active_script_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid
			`, generation.ProjectID, generation.OrganizationID, generation.ScriptID, generation.ExpectedActiveScriptID)
			if err != nil {
				return SourceToScriptOutput{}, err
			}
			if projectTag.RowsAffected() != 1 {
				return SourceToScriptOutput{}, sourceToScriptApplicationError("SCRIPT_VERSION_CONFLICT", "项目当前剧本激活 CAS 失败")
			}
			projectActivated = true
			if generation.BaseScriptVersionID != "" {
				if err := production.MarkScriptVersionDownstreamStale(ctx, tx, generation.ProjectID, generation.BaseScriptVersionID); err != nil {
					return SourceToScriptOutput{}, err
				}
			}
			if err := production.MarkFinalVideoStale(ctx, tx, generation.ProjectID, ""); err != nil {
				return SourceToScriptOutput{}, err
			}
		}
	}

	retentionDeadline := sourceToScriptRetentionDeadline()
	if _, err := tx.Exec(ctx, `
		UPDATE source_to_script_generations
		SET status = $2, result_script_version_id = NULLIF($3, '')::uuid,
		    error_code = NULL, error_message = NULL, finalized_at = now(),
		    retention_expires_at = $4
		WHERE id = $1
	`, generation.ID, status, versionID, retentionDeadline); err != nil {
		return SourceToScriptOutput{}, err
	}
	sourceStatus := "ready"
	if activated {
		sourceStatus = "processed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources SET status = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2 AND status <> 'archived'
	`, generation.ProjectID, generation.SourceID, sourceStatus); err != nil {
		return SourceToScriptOutput{}, err
	}
	output := SourceToScriptOutput{
		Status: status, SourceID: generation.SourceID, ScriptID: generation.ScriptID,
		ScriptVersionID: firstNonEmptyString(versionID, generation.ExpectedCurrentVersionID),
		ProviderCallID:  firstStringValue(providerCallIDs), ProviderCallIDs: providerCallIDs,
		ModelID: firstStringValue(modelIDs), ModelIDs: modelIDs,
		EpisodeCount: len(assembled), TotalItems: len(targets), CompletedItems: succeededTargets,
		FailedItems: failedTargets, FailedEpisodes: failedEpisodes, MissingItems: missingTargets,
		Activated: activated, Content: content,
	}
	if err := insertEvent(ctx, tx, generation.OrganizationID, generation.ProjectID, "script.generated", "script", generation.ScriptID, mustJSON(map[string]any{
		"generationId": generation.ID, "rootGenerationId": generation.RootGenerationID,
		"scriptId": generation.ScriptID, "scriptVersionId": versionID,
		"sourceId": generation.SourceID, "workflowRunId": generation.WorkflowRunID,
		"status": status, "episodeCount": len(assembled), "completedItems": succeededTargets,
		"failedItems": failedTargets, "failedEpisodes": failedEpisodes,
		"activated": activated, "projectActivated": projectActivated,
		"missingTargetCount": missingTargets,
	})); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptOutput{}, err
	}
	_ = finalization
	return output, nil
}
