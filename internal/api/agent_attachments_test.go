package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentTaskImageReferencesUseArtifactIdentityOnly(t *testing.T) {
	task := AgentTask{Constraints: json.RawMessage(`{
		"attachments": [
			{
				"attachmentId": "bb8dcc12-e999-449d-aef1-31cd3ceac9eb",
				"artifactId": "2abfb888-168b-4657-a492-f7ed9a835e06",
				"mediaFileId": "95210f85-2a83-46d8-8fea-f2ba60b21440",
				"fileName": "helmet.png",
				"usage": "product_common"
			},
			{
				"attachmentId": "1d6bed76-5bd0-402e-8892-ed043283bd65",
				"fileName": "not-completed.png",
				"usage": "unspecified"
			}
		]
	}`)}
	references := agentTaskImageReferences(task)
	if len(references) != 1 {
		t.Fatalf("references = %+v", references)
	}
	if references[0].Type != "image" ||
		references[0].ArtifactID != "2abfb888-168b-4657-a492-f7ed9a835e06" {
		t.Fatalf("reference = %+v", references[0])
	}
	metadata := rawObject(references[0].Metadata)
	if metadata["attachmentId"] != "bb8dcc12-e999-449d-aef1-31cd3ceac9eb" ||
		metadata["fileName"] != "helmet.png" ||
		metadata["usage"] != "product_common" {
		t.Fatalf("reference metadata = %+v", metadata)
	}
	if !agentTaskHasImageAttachment(task, "bb8dcc12-e999-449d-aef1-31cd3ceac9eb") ||
		agentTaskHasImageAttachment(task, "90c40221-64aa-4522-868b-16bf93d97c96") {
		t.Fatal("task attachment membership did not follow the frozen constraints")
	}
}

func TestAgentImageAttachmentCanonicalizationIntegration(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	contentHash := strings.Repeat("e", 64)
	storageKey := "agent-attachments/" + randomStorageSegment() + "/helmet.png"
	var artifactID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'agent_image_attachment', $3, 'image/png', $4, '{}', $5)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, storageKey, contentHash, seed.ownerUserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	var mediaFileID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', 2048, 640, 640, $5, '{}', $6)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, artifactID, storageKey, contentHash, seed.ownerUserID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert media file: %v", err)
	}
	var attachmentID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO agent_image_attachments(
			organization_id, project_id, storage_key, original_file_name,
			requested_mime_type, byte_size, width, height, content_hash,
			status, idempotency_key, artifact_id, media_file_id,
			created_by, expires_at, completed_at
		)
		VALUES (
			$1, $2, $3, 'helmet.png', 'image/png', 2048, 640, 640, $4,
			'completed', $5, $6, $7, $8, now() + interval '15 minutes', now()
		)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, storageKey, contentHash,
		"agent-image-test-"+randomStorageSegment(), artifactID, mediaFileID, seed.ownerUserID,
	).Scan(&attachmentID); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	taskID := seed.insertAgentTask(t, "queued")
	tx, err := seed.pool.Begin(seed.ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(seed.ctx)
	project, err := seed.apiServer.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	canonical, links, err := canonicalizeAgentTaskImageAttachments(
		seed.ctx,
		tx,
		project,
		mustMarshal(map[string]any{
			"permissionMode": "require_approval",
			"attachments": []any{map[string]any{
				"attachmentId": attachmentID,
				"usage":        "unspecified",
			}},
		}),
	)
	if err != nil {
		t.Fatalf("canonicalize attachments: %v", err)
	}
	if len(links) != 1 || links[0].AttachmentID != attachmentID || links[0].Ordinal != 0 {
		t.Fatalf("attachment links = %+v", links)
	}
	constraints := rawObject(canonical)
	items, ok := constraints["attachments"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("canonical constraints = %+v", constraints)
	}
	item, ok := mapFromAny(items[0])
	if !ok ||
		item["artifactId"] != artifactID ||
		item["mediaFileId"] != mediaFileID ||
		item["contentHash"] != contentHash ||
		item["usage"] != "unspecified" {
		t.Fatalf("canonical attachment = %+v", item)
	}
	if _, err := tx.Exec(seed.ctx, `
		UPDATE agent_tasks SET constraints = $2 WHERE id = $1
	`, taskID, canonical); err != nil {
		t.Fatalf("store canonical constraints: %v", err)
	}
	if err := insertAgentTaskImageAttachmentLinks(seed.ctx, tx, taskID, links); err != nil {
		t.Fatalf("insert task attachment link: %v", err)
	}
	if err := tx.Commit(seed.ctx); err != nil {
		t.Fatalf("commit task attachment link: %v", err)
	}
	if err := seed.apiServer.recordAgentTaskImageAttachmentUsage(
		seed.ctx, taskID, attachmentID, "product_common",
	); err != nil {
		t.Fatalf("record attachment usage: %v", err)
	}
	var linkedUsage string
	var linkedOrdinal int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT usage, ordinal
		FROM agent_task_image_attachments
		WHERE task_id = $1 AND attachment_id = $2
	`, taskID, attachmentID).Scan(&linkedUsage, &linkedOrdinal); err != nil {
		t.Fatalf("load task attachment link: %v", err)
	}
	if linkedUsage != "product_common" || linkedOrdinal != 0 {
		t.Fatalf("task attachment link usage=%s ordinal=%d", linkedUsage, linkedOrdinal)
	}
	var storedConstraints json.RawMessage
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT constraints FROM agent_tasks WHERE id = $1
	`, taskID).Scan(&storedConstraints); err != nil {
		t.Fatalf("load task constraints: %v", err)
	}
	storedItems, _ := rawObject(storedConstraints)["attachments"].([]any)
	if len(storedItems) != 1 {
		t.Fatalf("stored task attachments = %+v", storedItems)
	}
	storedItem, _ := mapFromAny(storedItems[0])
	if storedItem["usage"] != "product_common" {
		t.Fatalf("stored task attachment usage = %+v", storedItem)
	}
}

func TestCreateAgentImageAttachmentUploadIsDurablyIdempotentIntegration(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	requestBody := []byte(`{"fileName":"helmet.png","mimeType":"image/png"}`)
	idempotencyKey := "agent-upload-" + randomStorageSegment()
	send := func(token, key string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/"+seed.projectID+"/agent/image-attachments/upload-url",
			bytes.NewReader(requestBody),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Organization-Id", seed.organizationID)
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	first := send(seed.ownerToken, idempotencyKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("first upload ticket status=%d body=%s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Data struct {
			AttachmentID string              `json:"attachmentId"`
			UploadURL    string              `json:"uploadUrl"`
			Method       string              `json:"method"`
			Headers      map[string][]string `json:"headers"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatalf("decode first upload ticket: %v", err)
	}
	if firstEnvelope.Data.AttachmentID == "" ||
		firstEnvelope.Data.UploadURL == "" ||
		firstEnvelope.Data.Method != http.MethodPut ||
		len(firstEnvelope.Data.Headers) == 0 {
		t.Fatalf("first upload ticket = %+v", firstEnvelope.Data)
	}

	replay := send(seed.ownerToken, idempotencyKey)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay upload ticket status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayEnvelope struct {
		Data struct {
			AttachmentID string `json:"attachmentId"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayEnvelope); err != nil {
		t.Fatalf("decode replay upload ticket: %v", err)
	}
	if replayEnvelope.Data.AttachmentID != firstEnvelope.Data.AttachmentID ||
		replayEnvelope.Meta["idempotentReplay"] != true {
		t.Fatalf("replay upload ticket = %+v", replayEnvelope)
	}

	forbidden := send(seed.otherToken, "agent-upload-other-"+randomStorageSegment())
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("cross-organization upload status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	var pendingCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*)
		FROM agent_image_attachments
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status = 'pending' AND artifact_id IS NULL AND media_file_id IS NULL
	`, firstEnvelope.Data.AttachmentID, seed.organizationID, seed.projectID).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending attachment: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending attachment count = %d, want 1", pendingCount)
	}

	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE agent_image_attachments
		SET status = 'abandoned', abandoned_at = now()
		WHERE id = $1
	`, firstEnvelope.Data.AttachmentID); err != nil {
		t.Fatalf("abandon attachment: %v", err)
	}
	terminalReplay := send(seed.ownerToken, idempotencyKey)
	if terminalReplay.Code != http.StatusConflict ||
		!strings.Contains(terminalReplay.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf(
			"terminal replay status=%d body=%s",
			terminalReplay.Code,
			terminalReplay.Body.String(),
		)
	}
}
