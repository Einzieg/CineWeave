package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlAdaptationActionsShareRevisionedDomainPath(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var controlKeyID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text
		FROM user_control_keys
		WHERE user_id = $1 AND status = 'active'
	`, seed.ownerUserID).Scan(&controlKeyID); err != nil {
		t.Fatalf("read control key: %v", err)
	}
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: controlKeyID},
	}
	sourceID := seed.insertProjectSource(t, "novel", "统一改编原文")
	chapterID := seed.insertNovelChapter(t, sourceID)
	eventID := seed.insertNovelEvent(t, sourceID, chapterID, 1, "旧事件", "旧摘要", "pending")

	updatedEvent := executeProjectControlTestAction(t, seed, identity, "novel_event.update", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "novel-event-update-shared",
		"eventId": eventID, "expectedRevision": 1,
		"patch": map[string]any{"title": "新事件", "summary": "新摘要", "importance": 5},
	})
	replayedEvent := executeProjectControlTestAction(t, seed, identity, "novel_event.update", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "novel-event-update-shared",
		"eventId": eventID, "expectedRevision": 1,
		"patch": map[string]any{"title": "新事件", "summary": "新摘要", "importance": 5},
	})
	if replayedEvent.CommandID != updatedEvent.CommandID {
		t.Fatalf("event replay command=%s want=%s", replayedEvent.CommandID, updatedEvent.CommandID)
	}
	var eventRevision int64
	var eventTitle string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision, title FROM novel_events WHERE id = $1`, eventID).Scan(&eventRevision, &eventTitle); err != nil {
		t.Fatalf("read updated event: %v", err)
	}
	if eventRevision != 2 || eventTitle != "新事件" {
		t.Fatalf("event title=%q revision=%d", eventTitle, eventRevision)
	}

	reviewedEvent := executeProjectControlTestAction(t, seed, identity, "novel_event.review", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "novel-event-review-shared",
		"eventId": eventID, "expectedRevision": eventRevision, "reviewStatus": "approved", "note": "通过",
	})
	var reviewedEventData struct {
		Review ReviewResponse `json:"review"`
	}
	decodeProjectControlResultData(t, reviewedEvent, &reviewedEventData)
	if reviewedEventData.Review.Revision != 3 || reviewedEventData.Review.ReviewStatus != "approved" {
		t.Fatalf("event review=%+v", reviewedEventData.Review)
	}

	createdPlan := executeProjectControlTestAction(t, seed, identity, "adaptation.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "adaptation-create-shared",
		"sourceId": sourceID, "title": "统一改编计划", "selectedEventIds": []string{eventID},
		"targetFormat": "short_video", "targetDurationSeconds": 30, "maxShots": 6,
	})
	var createdPlanData struct {
		Plan AdaptationPlan `json:"plan"`
	}
	decodeProjectControlResultData(t, createdPlan, &createdPlanData)
	if createdPlanData.Plan.ID == "" || createdPlanData.Plan.Revision != 1 {
		t.Fatalf("created plan=%+v", createdPlanData.Plan)
	}

	updatedPlan := executeProjectControlTestAction(t, seed, identity, "adaptation.update", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "adaptation-update-shared",
		"planId": createdPlanData.Plan.ID, "expectedRevision": createdPlanData.Plan.Revision,
		"patch": map[string]any{"title": "统一改编计划 2", "maxShots": 8},
	})
	var updatedPlanData struct {
		Plan AdaptationPlan `json:"plan"`
	}
	decodeProjectControlResultData(t, updatedPlan, &updatedPlanData)
	if updatedPlanData.Plan.Revision != 2 || updatedPlanData.Plan.Title != "统一改编计划 2" {
		t.Fatalf("updated plan=%+v", updatedPlanData.Plan)
	}

	reviewedPlan := executeProjectControlTestAction(t, seed, identity, "adaptation.review", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "adaptation-review-shared",
		"planId": createdPlanData.Plan.ID, "expectedRevision": updatedPlanData.Plan.Revision,
		"reviewStatus": "approved", "note": "通过",
	})
	var reviewedPlanData struct {
		Review ReviewResponse `json:"review"`
	}
	decodeProjectControlResultData(t, reviewedPlan, &reviewedPlanData)
	if reviewedPlanData.Review.Revision != 3 || reviewedPlanData.Review.ReviewStatus != "approved" {
		t.Fatalf("plan review=%+v", reviewedPlanData.Review)
	}

	activatedPlan := executeProjectControlTestAction(t, seed, identity, "adaptation.activate", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "adaptation-activate-shared",
		"planId": createdPlanData.Plan.ID, "expectedRevision": reviewedPlanData.Review.Revision,
	})
	var activatedPlanData struct {
		Plan AdaptationPlan `json:"plan"`
	}
	decodeProjectControlResultData(t, activatedPlan, &activatedPlanData)
	if activatedPlanData.Plan.Status != "active" || activatedPlanData.Plan.Revision != 4 {
		t.Fatalf("activated plan=%+v", activatedPlanData.Plan)
	}

	staleRaw, err := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "adaptation-update-stale",
		"planId": createdPlanData.Plan.ID, "expectedRevision": 1,
		"patch": map[string]any{"title": "不应覆盖"},
	})
	if err != nil {
		t.Fatalf("marshal stale plan update: %v", err)
	}
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "adaptation.update", staleRaw)
	if err != nil {
		t.Fatalf("execute stale plan update: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "ADAPTATION_PLAN_REVISION_CONFLICT" {
		t.Fatalf("stale plan result=%+v", stale)
	}
}

func TestAdaptationGenerateScriptReplaysCommittedCommandResult(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	commandID := "3f96f241-a3c8-4bc7-9d85-9c25ced5cccf"
	sourceID := seed.insertProjectSource(t, "novel", "回放原文")
	planID := seed.insertAdaptationPlan(t, sourceID, "回放改编计划", "active", "approved")
	var scriptID, versionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, '崩溃前已生成剧本', 'active', $4)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, sourceID, seed.ownerUserID).Scan(&scriptID); err != nil {
		t.Fatalf("insert replay script: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES (
			$1, $2, $3, 1, 1, '已持久化剧本正文', 'markdown', 'active', 'agent_generated',
			jsonb_build_object(
				'adaptationPlanId', $4::text,
				'projectControlCommandId', $5::text,
				'providerCallId', 'provider-call-1',
				'providerCallIds', jsonb_build_array('provider-call-1'),
				'modelId', 'model-1',
				'modelIds', jsonb_build_array('model-1'),
				'episodeCount', 1
			),
			$6
		)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, planID, commandID, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert replay script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id, episode_index,
			episode_title, content, content_format, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', '已持久化剧本正文', 'markdown', '{}', $5)
	`, seed.organizationID, seed.projectID, scriptID, versionID, seed.ownerUserID); err != nil {
		t.Fatalf("insert replay episode: %v", err)
	}

	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	raw, err := json.Marshal(map[string]any{"planId": planID, "title": "不应再次生成"})
	if err != nil {
		t.Fatalf("marshal replay input: %v", err)
	}
	result, err := seed.apiServer.executeAdaptationGenerateScriptAsyncAction(
		seed.ctx,
		auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		project,
		projectcontrol.Command{ID: commandID},
		raw,
	)
	if err != nil {
		t.Fatalf("replay adaptation script generation: %v", err)
	}
	if result.Data["scriptId"] != scriptID || result.Data["versionId"] != versionID || result.Data["idempotentReplay"] != true {
		t.Fatalf("replay result=%+v", result.Data)
	}
	var versionCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*) FROM script_versions
		WHERE project_id = $1 AND metadata->>'projectControlCommandId' = $2
	`, seed.projectID, commandID).Scan(&versionCount); err != nil {
		t.Fatalf("count replay versions: %v", err)
	}
	if versionCount != 1 {
		t.Fatalf("replay version count=%d, want 1", versionCount)
	}
}

func decodeProjectControlResultData[T any](t *testing.T, result projectcontrol.Result, target *T) {
	t.Helper()
	var envelope struct {
		Result struct {
			Data json.RawMessage `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result.Data, &envelope); err != nil {
		t.Fatalf("decode project control result envelope: %v", err)
	}
	if len(envelope.Result.Data) == 0 {
		t.Fatalf("project control result data is empty: %s", result.Data)
	}
	if err := json.Unmarshal(envelope.Result.Data, target); err != nil {
		t.Fatalf("decode project control action data: %v", err)
	}
}
