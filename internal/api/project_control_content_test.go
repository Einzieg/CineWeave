package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestUTF8ContentChunkPreservesRuneBoundaries(t *testing.T) {
	content := strings.Repeat("甲🙂乙", 17)
	offset := 0
	var rebuilt strings.Builder
	for offset < len([]byte(content)) {
		chunk, next, err := utf8ContentChunk(content, offset, 5)
		if err != nil {
			t.Fatalf("read chunk at %d: %v", offset, err)
		}
		if chunk == "" || next <= offset || !utf8.ValidString(chunk) {
			t.Fatalf("invalid chunk=%q offset=%d next=%d", chunk, offset, next)
		}
		rebuilt.WriteString(chunk)
		offset = next
	}
	if rebuilt.String() != content {
		t.Fatalf("rebuilt content differs: %q", rebuilt.String())
	}
}

func TestProjectControlContentReadUsesHashBoundCursor(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	sourceID := seed.insertProjectSource(t, "novel", "长文本")
	wanted := strings.Repeat("第一集🙂正文。", 30)
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE project_sources SET content = $2 WHERE id = $1`, sourceID, wanted); err != nil {
		t.Fatalf("update source content: %v", err)
	}
	identity := controlmcp.Identity{Principal: auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}}
	arguments := map[string]any{
		"projectId": seed.projectID, "targetType": "project_source", "targetId": sourceID, "maxBytes": 17,
	}
	var rebuilt strings.Builder
	firstCursor := ""
	for {
		raw, err := json.Marshal(arguments)
		if err != nil {
			t.Fatalf("marshal arguments: %v", err)
		}
		result, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "content.read", raw)
		if err != nil {
			t.Fatalf("read content: %v", err)
		}
		if result.Error != nil {
			t.Fatalf("read content result error: %+v", result.Error)
		}
		var data struct {
			Content struct {
				ChunkText string `json:"chunkText"`
			} `json:"content"`
		}
		if err := json.Unmarshal(result.Data, &data); err != nil {
			t.Fatalf("decode content data: %v", err)
		}
		if !utf8.ValidString(data.Content.ChunkText) {
			t.Fatalf("chunk is not valid UTF-8: %q", data.Content.ChunkText)
		}
		rebuilt.WriteString(data.Content.ChunkText)
		if firstCursor == "" {
			firstCursor = result.NextCursor
		}
		if result.NextCursor == "" {
			break
		}
		arguments["cursor"] = result.NextCursor
	}
	if rebuilt.String() != wanted {
		t.Fatalf("rebuilt content length=%d want=%d", rebuilt.Len(), len([]byte(wanted)))
	}
	if firstCursor == "" {
		t.Fatal("expected a cursor for multi-chunk content")
	}

	if _, err := seed.pool.Exec(seed.ctx, `UPDATE project_sources SET content = content || '已更新' WHERE id = $1`, sourceID); err != nil {
		t.Fatalf("update source after cursor: %v", err)
	}
	staleRaw, _ := json.Marshal(map[string]any{
		"projectId": seed.projectID, "targetType": "project_source", "targetId": sourceID, "cursor": firstCursor,
	})
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "content.read", staleRaw)
	if err != nil {
		t.Fatalf("read stale cursor: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "CONTENT_CURSOR_STALE" {
		t.Fatalf("stale result=%+v", stale)
	}
}

func TestProjectControlContentUploadCommitsCanonicalContentAndCommandAtomically(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	sourceID := seed.insertProjectSource(t, "novel", "待覆盖原文")
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT content_revision FROM project_sources WHERE id = $1`, sourceID).Scan(&revision); err != nil {
		t.Fatalf("read source revision: %v", err)
	}
	content := strings.Repeat("这是通过私有暂存提交的长正文🙂。", 100)
	fullHash := sha256.Sum256([]byte(content))
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerManual,
	}
	begin := executeProjectControlTestAction(t, seed, identity, "content.write.begin", map[string]any{
		"projectId": seed.projectID, "targetType": "project_source", "targetId": sourceID,
		"expectedRevision": revision, "contentHash": fmt.Sprintf("%x", fullHash[:]),
		"contentFormat": "plain_text", "expectedSizeBytes": len([]byte(content)),
		"expectedChunkCount": 2, "idempotencyKey": "content-upload-begin-test",
	})
	var beginData struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.Unmarshal(begin.Data, &beginData); err != nil || beginData.UploadID == "" {
		t.Fatalf("decode begin result: data=%s err=%v", begin.Data, err)
	}
	encoded := []byte(content)
	cut := len(encoded) / 2
	for cut < len(encoded) && !utf8.RuneStart(encoded[cut]) {
		cut++
	}
	chunks := [][]byte{encoded[:cut], encoded[cut:]}
	for index, chunk := range chunks {
		hash := sha256.Sum256(chunk)
		executeProjectControlTestAction(t, seed, identity, "content.write.chunk", map[string]any{
			"projectId": seed.projectID, "uploadId": beginData.UploadID, "chunkIndex": index,
			"chunkHash": fmt.Sprintf("%x", hash[:]), "chunkText": string(chunk),
		})
	}
	commitArguments := map[string]any{
		"projectId": seed.projectID, "uploadId": beginData.UploadID, "idempotencyKey": "content-upload-commit-test",
	}
	commit := executeProjectControlTestAction(t, seed, identity, "content.write.commit", commitArguments)
	if commit.CommandID == "" || commit.Status != string(projectcontrol.CommandSucceeded) {
		t.Fatalf("commit result=%+v", commit)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "content.write.commit", commitArguments)
	if replayed.CommandID != commit.CommandID {
		t.Fatalf("replayed command=%s want=%s", replayed.CommandID, commit.CommandID)
	}
	var storedContent string
	var storedRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT content, content_revision FROM project_sources WHERE id = $1`, sourceID).Scan(&storedContent, &storedRevision); err != nil {
		t.Fatalf("read committed source: %v", err)
	}
	if storedContent != content || storedRevision != revision+1 {
		t.Fatalf("stored content/revision mismatch: bytes=%d revision=%d", len([]byte(storedContent)), storedRevision)
	}
	command, err := seed.apiServer.projectControl.repository.Get(seed.ctx, commit.CommandID)
	if err != nil {
		t.Fatalf("read commit command: %v", err)
	}
	if strings.Contains(string(command.Input), content[:64]) || command.ControllerType != projectcontrol.ControllerManual {
		t.Fatalf("command input leaked content or controller mismatch: %s", command.Input)
	}
	var committedCommandID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT committed_command_id::text FROM project_control_content_uploads WHERE id = $1`, beginData.UploadID).Scan(&committedCommandID); err != nil {
		t.Fatalf("read committed upload: %v", err)
	}
	if committedCommandID != commit.CommandID {
		t.Fatalf("upload command=%s want=%s", committedCommandID, commit.CommandID)
	}
}

func executeProjectControlTestAction(
	t *testing.T,
	seed *artifactPreviewSeed,
	identity controlmcp.Identity,
	action string,
	arguments map[string]any,
) projectcontrol.Result {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal %s input: %v", action, err)
	}
	result, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, action, raw)
	if err != nil {
		t.Fatalf("execute %s: %v", action, err)
	}
	if result.Error != nil {
		t.Fatalf("execute %s returned error: %+v", action, result.Error)
	}
	return result
}
