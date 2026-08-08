package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlScriptReadsArePagedAndContentAddressable(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var scriptID, versionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, '测试剧本', 'active', $3)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	versionContent := strings.Repeat("剧本版本正文🙂。", 40)
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, $4, 'markdown', 'active', '{}', $5)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionContent, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, seed.projectID, scriptID); err != nil {
		t.Fatalf("select active script: %v", err)
	}
	for index := 1; index <= 2; index++ {
		if _, err := seed.pool.Exec(seed.ctx, `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id, episode_index,
				episode_title, content, content_format, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'markdown', '{}', $8)
		`, seed.organizationID, seed.projectID, scriptID, versionID, index,
			"第 "+intToString(index)+" 集", strings.Repeat("单集正文", index*10), seed.ownerUserID); err != nil {
			t.Fatalf("insert script episode %d: %v", index, err)
		}
	}

	identity := controlmcp.Identity{Principal: auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}}
	list := executeProjectControlTestAction(t, seed, identity, "script.list", map[string]any{
		"projectId": seed.projectID, "limit": 10,
	})
	var listData struct {
		Items []scriptActionSummary `json:"items"`
	}
	if err := json.Unmarshal(list.Data, &listData); err != nil {
		t.Fatalf("decode script.list: %v", err)
	}
	if len(listData.Items) != 1 || listData.Items[0].ID != scriptID || listData.Items[0].Revision < 1 || listData.Items[0].EpisodeCount != 2 {
		t.Fatalf("script.list items=%+v", listData.Items)
	}

	get := executeProjectControlTestAction(t, seed, identity, "script.get", map[string]any{
		"projectId": seed.projectID, "scriptId": scriptID, "episodeLimit": 1,
	})
	var getData struct {
		Script            scriptActionSummary          `json:"script"`
		Version           scriptVersionActionSummary   `json:"version"`
		Episodes          []scriptEpisodeActionSummary `json:"episodes"`
		NextEpisodeCursor string                       `json:"nextEpisodeCursor"`
	}
	if err := json.Unmarshal(get.Data, &getData); err != nil {
		t.Fatalf("decode script.get: %v", err)
	}
	if getData.Script.ID != scriptID || getData.Version.ID != versionID || len(getData.Episodes) != 1 || getData.NextEpisodeCursor == "" {
		t.Fatalf("script.get data=%+v", getData)
	}
	if getData.Episodes[0].Revision < 1 || len(getData.Episodes[0].ContentHash) != 64 || getData.Episodes[0].ContentTarget.TargetType != "script_episode" {
		t.Fatalf("episode summary=%+v", getData.Episodes[0])
	}
	if getData.Version.ContentTarget.TargetType != "script_version" || len(getData.Version.ContentHash) != 64 {
		t.Fatalf("version summary=%+v", getData.Version)
	}

	read := executeProjectControlTestAction(t, seed, identity, "content.read", map[string]any{
		"projectId": seed.projectID, "targetType": "script_version", "targetId": versionID, "maxBytes": 23,
	})
	var readData struct {
		Content struct {
			ChunkText string `json:"chunkText"`
		} `json:"content"`
	}
	if err := json.Unmarshal(read.Data, &readData); err != nil {
		t.Fatalf("decode script version content: %v", err)
	}
	if readData.Content.ChunkText == "" || !strings.HasPrefix(versionContent, readData.Content.ChunkText) {
		t.Fatalf("script version chunk=%q", readData.Content.ChunkText)
	}
}

func TestProjectControlScriptWritesUseCASAndArchiveWholeScript(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var scriptID, versionID, episodeID string
	var scriptRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, '待修改剧本', 'active', $3)
		RETURNING id::text, revision
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&scriptID, &scriptRevision); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, '旧正文', 'markdown', 'active', '{}', $4)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET active_script_id = $2 WHERE id = $1`, seed.projectID, scriptID); err != nil {
		t.Fatalf("select active script: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM scripts WHERE id = $1`, scriptID).Scan(&scriptRevision); err != nil {
		t.Fatalf("read script revision: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id, episode_index,
			episode_title, content, content_format, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第一集', '旧正文', 'markdown', '{}', $5)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, seed.ownerUserID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "test-key"},
	}
	updateArguments := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-update-cas-test",
		"scriptId": scriptID, "expectedRevision": scriptRevision,
		"patch": map[string]any{"title": "已修改剧本"},
	}
	updated := executeProjectControlTestAction(t, seed, identity, "script.update", updateArguments)
	if updated.CommandID == "" || updated.Status != string(projectcontrol.CommandSucceeded) {
		t.Fatalf("script.update result=%+v", updated)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "script.update", updateArguments)
	if replayed.CommandID != updated.CommandID {
		t.Fatalf("script.update replay command=%s want=%s", replayed.CommandID, updated.CommandID)
	}
	var title string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT title, revision FROM scripts WHERE id = $1`, scriptID).Scan(&title, &scriptRevision); err != nil {
		t.Fatalf("read updated script: %v", err)
	}
	if title != "已修改剧本" || scriptRevision < 2 {
		t.Fatalf("updated title=%q revision=%d", title, scriptRevision)
	}

	var episodeRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM script_episodes WHERE id = $1`, episodeID).Scan(&episodeRevision); err != nil {
		t.Fatalf("read episode revision: %v", err)
	}
	executeProjectControlTestAction(t, seed, identity, "script.update_episode", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-episode-update-cas-test",
		"episodeId": episodeID, "expectedRevision": episodeRevision,
		"patch": map[string]any{"content": "新正文", "reviewStatus": "approved"},
	})
	var content, reviewStatus string
	var updatedEpisodeRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT content, review_status, revision FROM script_episodes WHERE id = $1
	`, episodeID).Scan(&content, &reviewStatus, &updatedEpisodeRevision); err != nil {
		t.Fatalf("read updated episode: %v", err)
	}
	if content != "新正文" || reviewStatus != "approved" || updatedEpisodeRevision <= episodeRevision {
		t.Fatalf("updated episode content=%q review=%q revision=%d", content, reviewStatus, updatedEpisodeRevision)
	}

	staleRaw, _ := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-update-stale-test",
		"scriptId": scriptID, "expectedRevision": scriptRevision - 1,
		"patch": map[string]any{"title": "不应覆盖"},
	})
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "script.update", staleRaw)
	if err != nil {
		t.Fatalf("execute stale script.update: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "REVISION_CONFLICT" {
		t.Fatalf("stale script.update result=%+v", stale)
	}

	deleted := executeProjectControlTestAction(t, seed, identity, "script.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-delete-cas-test",
		"scriptId": scriptID, "expectedRevision": scriptRevision, "reason": "测试归档",
	})
	if deleted.CommandID == "" {
		t.Fatalf("script.delete result=%+v", deleted)
	}
	var scriptStatus, versionStatus string
	var activeScriptID *string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT script.status, version.status, project.active_script_id::text
		FROM scripts script
		JOIN script_versions version ON version.script_id = script.id
		JOIN projects project ON project.id = script.project_id
		WHERE script.id = $1 AND version.id = $2
	`, scriptID, versionID).Scan(&scriptStatus, &versionStatus, &activeScriptID); err != nil {
		t.Fatalf("read archived script: %v", err)
	}
	if scriptStatus != "archived" || versionStatus != "archived" || activeScriptID != nil {
		t.Fatalf("archive status script=%s version=%s active=%v", scriptStatus, versionStatus, activeScriptID)
	}
}

func TestProjectControlScriptVersionLifecycleUsesSharedCommands(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "test-key"},
	}
	createArguments := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-create-lifecycle-test",
		"title": "共享生命周期剧本", "content": "第一版完整正文", "contentFormat": "markdown",
	}
	created := executeProjectControlTestAction(t, seed, identity, "script.create", createArguments)
	if created.CommandID == "" || strings.Contains(string(created.Data), "第一版完整正文") {
		t.Fatalf("script.create must return a compact durable result: %+v", created)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "script.create", createArguments)
	if replayed.CommandID != created.CommandID {
		t.Fatalf("script.create replay command=%s want=%s", replayed.CommandID, created.CommandID)
	}

	var createdData struct {
		ScriptID         string `json:"scriptId"`
		Revision         int64  `json:"revision"`
		CurrentVersionID string `json:"currentVersionId"`
	}
	if err := json.Unmarshal(created.Data, &createdData); err != nil {
		t.Fatalf("decode script.create result: %v", err)
	}
	if createdData.ScriptID == "" || createdData.CurrentVersionID == "" || createdData.Revision < 2 {
		t.Fatalf("script.create data=%+v", createdData)
	}

	createdVersion := executeProjectControlTestAction(t, seed, identity, "script.create_version", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-create-version-lifecycle-test",
		"scriptId": createdData.ScriptID, "expectedRevision": createdData.Revision,
		"content": "第二版完整正文", "contentFormat": "markdown", "activate": false,
	})
	if strings.Contains(string(createdVersion.Data), "第二版完整正文") {
		t.Fatalf("script.create_version leaked content: %s", createdVersion.Data)
	}
	var versionData struct {
		ScriptID       string                     `json:"scriptId"`
		ScriptRevision int64                      `json:"scriptRevision"`
		Version        scriptVersionActionSummary `json:"version"`
		EpisodeID      string                     `json:"episodeId"`
	}
	if err := json.Unmarshal(createdVersion.Data, &versionData); err != nil {
		t.Fatalf("decode script.create_version result: %v", err)
	}
	if versionData.ScriptID != createdData.ScriptID || versionData.Version.ID == "" || versionData.Version.Version != 2 || versionData.EpisodeID == "" {
		t.Fatalf("script.create_version data=%+v", versionData)
	}

	activated := executeProjectControlTestAction(t, seed, identity, "script.activate_version", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-activate-version-lifecycle-test",
		"scriptId": createdData.ScriptID, "versionId": versionData.Version.ID,
		"expectedRevision": versionData.ScriptRevision,
	})
	var activateData struct {
		ScriptRevision int64 `json:"scriptRevision"`
		Changed        bool  `json:"changed"`
	}
	if err := json.Unmarshal(activated.Data, &activateData); err != nil {
		t.Fatalf("decode script.activate_version result: %v", err)
	}
	if !activateData.Changed || activateData.ScriptRevision <= versionData.ScriptRevision {
		t.Fatalf("script.activate_version data=%+v", activateData)
	}

	archived := executeProjectControlTestAction(t, seed, identity, "script.archive_version", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-archive-version-lifecycle-test",
		"scriptId": createdData.ScriptID, "versionId": createdData.CurrentVersionID,
		"expectedRevision": activateData.ScriptRevision, "reason": "测试旧版本归档",
	})
	var archiveData struct {
		Deleted   bool   `json:"deleted"`
		VersionID string `json:"versionId"`
	}
	if err := json.Unmarshal(archived.Data, &archiveData); err != nil {
		t.Fatalf("decode script.archive_version result: %v", err)
	}
	if !archiveData.Deleted || archiveData.VersionID != createdData.CurrentVersionID {
		t.Fatalf("script.archive_version data=%+v", archiveData)
	}

	var currentVersionID, oldVersionStatus, currentVersionStatus string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT script.current_version_id::text, old_version.status, current_version.status
		FROM scripts script
		JOIN script_versions old_version ON old_version.id = $2
		JOIN script_versions current_version ON current_version.id = script.current_version_id
		WHERE script.id = $1
	`, createdData.ScriptID, createdData.CurrentVersionID).Scan(&currentVersionID, &oldVersionStatus, &currentVersionStatus); err != nil {
		t.Fatalf("read script version lifecycle: %v", err)
	}
	if currentVersionID != versionData.Version.ID || oldVersionStatus != "archived" || currentVersionStatus == "archived" {
		t.Fatalf("current=%s old=%s currentStatus=%s", currentVersionID, oldVersionStatus, currentVersionStatus)
	}

	staleRaw, _ := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-activate-stale-lifecycle-test",
		"scriptId": createdData.ScriptID, "versionId": versionData.Version.ID,
		"expectedRevision": versionData.ScriptRevision,
	})
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "script.activate_version", staleRaw)
	if err != nil {
		t.Fatalf("execute stale script.activate_version: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "REVISION_CONFLICT" {
		t.Fatalf("stale script.activate_version result=%+v", stale)
	}
}
