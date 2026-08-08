package api

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlTimelineLifecycleIsIdempotentAndRevisionSafe(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, handler, seed, "Codex Timeline Project")

	var controlKeyID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text FROM user_control_keys WHERE user_id = $1 AND status = 'active'
	`, seed.ownerUserID).Scan(&controlKeyID); err != nil {
		t.Fatalf("read control key: %v", err)
	}
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: controlKeyID},
	}

	createInput := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-create-codex",
		"title": "Codex 主时间线", "aspectRatio": "16:9", "resolution": "720p",
	}
	created := executeProjectControlTestAction(t, seed, identity, "timeline.create", createInput)
	var createdData struct {
		Timeline ProjectTimeline `json:"timeline"`
		Revision int64           `json:"revision"`
	}
	decodeProjectControlResultData(t, created, &createdData)
	if createdData.Timeline.ID == "" || createdData.Timeline.Revision != 1 || createdData.Revision != 1 {
		t.Fatalf("created timeline=%+v", createdData)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "timeline.create", createInput)
	if replayed.CommandID != created.CommandID {
		t.Fatalf("replayed command=%s want=%s", replayed.CommandID, created.CommandID)
	}

	updated := executeProjectControlTestAction(t, seed, identity, "timeline.update", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-update-codex",
		"timelineId": createdData.Timeline.ID, "expectedRevision": createdData.Timeline.Revision,
		"patch": map[string]any{"title": "Codex 成片时间线"},
	})
	var updatedData struct {
		Timeline ProjectTimeline `json:"timeline"`
	}
	decodeProjectControlResultData(t, updated, &updatedData)
	if updatedData.Timeline.Title != "Codex 成片时间线" || updatedData.Timeline.Revision <= createdData.Timeline.Revision {
		t.Fatalf("updated timeline=%+v", updatedData)
	}

	firstCreated := executeProjectControlTestAction(t, seed, identity, "timeline.clip.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-create-first",
		"timelineId": createdData.Timeline.ID, "expectedTimelineRevision": updatedData.Timeline.Revision,
		"title": "第一段", "durationTicks": int64(450000),
	})
	var firstData struct {
		Clip TimelineClip `json:"clip"`
	}
	decodeProjectControlResultData(t, firstCreated, &firstData)
	if firstData.Clip.ID == "" || firstData.Clip.Revision != 1 || firstData.Clip.TimelineRevision <= updatedData.Timeline.Revision {
		t.Fatalf("first clip=%+v", firstData)
	}

	firstUpdated := executeProjectControlTestAction(t, seed, identity, "timeline.update_clip", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-update-first",
		"timelineId": createdData.Timeline.ID, "clipId": firstData.Clip.ID,
		"expectedTimelineRevision": firstData.Clip.TimelineRevision, "expectedRevision": firstData.Clip.Revision,
		"patch": map[string]any{"title": "第一段修订", "notes": "由 Codex 修改"},
	})
	var firstUpdatedData struct {
		Clip TimelineClip `json:"clip"`
	}
	decodeProjectControlResultData(t, firstUpdated, &firstUpdatedData)
	if firstUpdatedData.Clip.Title != "第一段修订" || firstUpdatedData.Clip.Revision <= firstData.Clip.Revision || firstUpdatedData.Clip.TimelineRevision <= firstData.Clip.TimelineRevision {
		t.Fatalf("updated first clip=%+v", firstUpdatedData)
	}

	secondCreated := executeProjectControlTestAction(t, seed, identity, "timeline.clip.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-create-second",
		"timelineId": createdData.Timeline.ID, "expectedTimelineRevision": firstUpdatedData.Clip.TimelineRevision,
		"title": "第二段", "durationTicks": int64(450000),
	})
	var secondData struct {
		Clip TimelineClip `json:"clip"`
	}
	decodeProjectControlResultData(t, secondCreated, &secondData)
	if secondData.Clip.ID == "" || secondData.Clip.ClipIndex != 1 {
		t.Fatalf("second clip=%+v", secondData)
	}

	stale := executeProjectControlTestActionAllowFailure(t, seed, identity, "timeline.update_clip", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-update-stale",
		"timelineId": createdData.Timeline.ID, "clipId": firstUpdatedData.Clip.ID,
		"expectedTimelineRevision": firstUpdatedData.Clip.TimelineRevision,
		"expectedRevision":         firstUpdatedData.Clip.Revision,
		"patch":                    map[string]any{"title": "不应写入"},
	})
	if stale.Error == nil || stale.Error.Code != "TIMELINE_REVISION_CONFLICT" {
		t.Fatalf("stale clip update=%+v", stale)
	}

	reordered := executeProjectControlTestAction(t, seed, identity, "timeline.clip.reorder", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-reorder-codex",
		"timelineId": createdData.Timeline.ID, "expectedTimelineRevision": secondData.Clip.TimelineRevision,
		"items": []map[string]any{
			{"clipId": firstUpdatedData.Clip.ID, "expectedRevision": firstUpdatedData.Clip.Revision, "clipIndex": 1},
			{"clipId": secondData.Clip.ID, "expectedRevision": secondData.Clip.Revision, "clipIndex": 0},
		},
	})
	var reorderedData timelineClipReorderActionResult
	decodeProjectControlResultData(t, reordered, &reorderedData)
	if len(reorderedData.Items) != 2 || reorderedData.Items[0].ID != secondData.Clip.ID || reorderedData.Items[1].ID != firstUpdatedData.Clip.ID {
		t.Fatalf("reordered clips=%+v", reorderedData)
	}

	deletedClip := executeProjectControlTestAction(t, seed, identity, "timeline.clip.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-clip-delete-codex",
		"timelineId": createdData.Timeline.ID, "clipId": reorderedData.Items[1].ID,
		"expectedTimelineRevision": reorderedData.TimelineRevision,
		"expectedRevision":         reorderedData.Items[1].Revision,
	})
	var deletedClipData timelineClipDeleteActionResult
	decodeProjectControlResultData(t, deletedClip, &deletedClipData)
	if !deletedClipData.Deleted || deletedClipData.TimelineRevision <= reorderedData.TimelineRevision {
		t.Fatalf("deleted clip=%+v", deletedClipData)
	}

	deletedTimeline := executeProjectControlTestAction(t, seed, identity, "timeline.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-delete-codex",
		"timelineId": createdData.Timeline.ID, "expectedRevision": deletedClipData.TimelineRevision,
	})
	var deletedTimelineData timelineDeleteActionResult
	decodeProjectControlResultData(t, deletedTimeline, &deletedTimelineData)
	if !deletedTimelineData.Deleted || deletedTimelineData.TimelineID != createdData.Timeline.ID {
		t.Fatalf("deleted timeline=%+v", deletedTimelineData)
	}

	var commandCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_commands
		WHERE project_id = $1 AND controller_type = 'codex_mcp'
		  AND action_name LIKE 'timeline.%' AND status = 'succeeded'
	`, seed.projectID).Scan(&commandCount); err != nil {
		t.Fatalf("count timeline commands: %v", err)
	}
	if commandCount != 8 {
		t.Fatalf("succeeded timeline command count=%d, want 8", commandCount)
	}
}
