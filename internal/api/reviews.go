package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
	"github.com/jackc/pgx/v5"
)

const projectReviewPromptKey = "project_review_agent"

type ReviewRun struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	WorkflowRunID   *string         `json:"workflowRunId,omitempty"`
	ReviewType      string          `json:"reviewType"`
	Status          string          `json:"status"`
	Summary         json.RawMessage `json:"summary"`
	Input           json.RawMessage `json:"input"`
	Output          json.RawMessage `json:"output"`
	ProviderCallID  *string         `json:"providerCallId,omitempty"`
	PromptVersionID *string         `json:"promptVersionId,omitempty"`
	PromptHash      *string         `json:"promptHash,omitempty"`
	ErrorCode       *string         `json:"errorCode,omitempty"`
	ErrorMessage    *string         `json:"errorMessage,omitempty"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
}

type ReviewItem struct {
	ID                string             `json:"id"`
	OrganizationID    string             `json:"organizationId"`
	ProjectID         string             `json:"projectId"`
	ReviewRunID       *string            `json:"reviewRunId,omitempty"`
	ItemType          string             `json:"itemType"`
	Category          string             `json:"category"`
	Severity          string             `json:"severity"`
	Title             string             `json:"title"`
	Description       string             `json:"description"`
	Suggestion        *string            `json:"suggestion,omitempty"`
	EntityType        string             `json:"entityType"`
	EntityID          *string            `json:"entityId,omitempty"`
	RelatedEntityType *string            `json:"relatedEntityType,omitempty"`
	RelatedEntityID   *string            `json:"relatedEntityId,omitempty"`
	Status            string             `json:"status"`
	ResolutionNote    *string            `json:"resolutionNote,omitempty"`
	Metadata          json.RawMessage    `json:"metadata"`
	Actions           []reviewpkg.Action `json:"actions,omitempty"`
	CreatedBy         *string            `json:"createdBy,omitempty"`
	ResolvedBy        *string            `json:"resolvedBy,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	ResolvedAt        *time.Time         `json:"resolvedAt,omitempty"`
}

type RunProjectReviewResponse struct {
	ReviewRunID string          `json:"reviewRunId"`
	Status      string          `json:"status"`
	Summary     json.RawMessage `json:"summary"`
	ItemCount   int             `json:"itemCount"`
}

type runProjectReviewRequest struct {
	ReviewType                 string `json:"reviewType"`
	UseAgent                   bool   `json:"useAgent"`
	IncludeDeterministicChecks *bool  `json:"includeDeterministicChecks"`
	ProjectControlCommandID    string `json:"-"`
}

func (s *Server) runProjectReview(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{authz.PermissionProjectWrite, authz.PermissionProjectRead})
	if !ok {
		return
	}
	var req runProjectReviewRequest
	if !decode(w, r, &req) {
		return
	}
	response, err := s.runProjectReviewCore(r.Context(), principal, project, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (s *Server) runProjectReviewCore(ctx context.Context, principal auth.Principal, project Project, req runProjectReviewRequest) (RunProjectReviewResponse, error) {
	reviewType := strings.TrimSpace(req.ReviewType)
	if reviewType == "" {
		reviewType = "project"
	}
	if !validReviewType(reviewType) {
		return RunProjectReviewResponse{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewType is invalid")
	}
	includeDeterministic := true
	if req.IncludeDeterministicChecks != nil {
		includeDeterministic = *req.IncludeDeterministicChecks
	}
	input := map[string]any{
		"reviewType":                 reviewType,
		"useAgent":                   req.UseAgent,
		"includeDeterministicChecks": includeDeterministic,
	}
	commandID := strings.TrimSpace(req.ProjectControlCommandID)
	if commandID != "" {
		input["projectControlCommandId"] = commandID
	}
	var runID string
	if commandID != "" {
		var status string
		var summary, output json.RawMessage
		err := s.db.QueryRow(ctx, `
			SELECT id::text, status, summary, output
			FROM review_runs
			WHERE project_id = $1 AND project_control_command_id = $2
		`, project.ID, commandID).Scan(&runID, &status, &summary, &output)
		if err == nil {
			if status == "succeeded" {
				var completed struct {
					ItemCount int `json:"itemCount"`
				}
				_ = json.Unmarshal(output, &completed)
				return RunProjectReviewResponse{ReviewRunID: runID, Status: status, Summary: summary, ItemCount: completed.ItemCount}, nil
			}
			if _, err := s.db.Exec(ctx, `
				UPDATE review_runs
				SET status = 'running', error_code = NULL, error_message = NULL,
				    started_at = COALESCE(started_at, now()), completed_at = NULL
				WHERE id = $1
			`, runID); err != nil {
				return RunProjectReviewResponse{}, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return RunProjectReviewResponse{}, err
		}
	}
	if runID == "" {
		if err := s.db.QueryRow(ctx, `
			INSERT INTO review_runs(
				organization_id, project_id, review_type, status, input, output, created_by,
				project_control_command_id
			)
			VALUES ($1, $2, $3, 'running', $4, '{}', $5, NULLIF($6, '')::uuid)
			RETURNING id::text
		`, project.OrganizationID, project.ID, reviewType, mustMarshal(input), principal.UserID, commandID).Scan(&runID); err != nil {
			return RunProjectReviewResponse{}, err
		}
	}
	items := []reviewpkg.ReviewItemDraft{}
	if includeDeterministic {
		deterministic, err := reviewpkg.RunDeterministicProjectChecks(ctx, s.db, project.ID)
		if err != nil {
			s.failReviewRun(ctx, runID, "DETERMINISTIC_REVIEW_FAILED", err.Error())
			return RunProjectReviewResponse{}, err
		}
		items = append(items, deterministic...)
	}
	var providerCallID, promptVersionID, promptHash string
	if req.UseAgent {
		agentItems, callID, versionID, hash, err := s.runProjectReviewAgent(ctx, principal, project, items)
		if err != nil {
			s.failReviewRun(ctx, runID, "PROJECT_REVIEW_AGENT_FAILED", err.Error())
			return RunProjectReviewResponse{}, err
		}
		providerCallID = callID
		promptVersionID = versionID
		promptHash = hash
		items = append(items, agentItems...)
	}
	summary := reviewpkg.Summarize(items)
	output := map[string]any{
		"summary":   summary,
		"itemCount": len(items),
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RunProjectReviewResponse{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE review_runs
		SET status = 'succeeded',
		    summary = $2,
		    output = $3,
		    provider_call_id = NULLIF($4, '')::uuid,
		    prompt_version_id = NULLIF($5, '')::uuid,
		    prompt_hash = NULLIF($6, ''),
		    started_at = COALESCE(started_at, created_at),
		    completed_at = now()
		WHERE id = $1
	`, runID, mustRawJSON(summary), mustRawJSON(output), providerCallID, promptVersionID, promptHash); err != nil {
		return RunProjectReviewResponse{}, err
	}
	for _, item := range items {
		if err := insertReviewItem(ctx, tx, project.OrganizationID, project.ID, runID, item, principal.UserID); err != nil {
			return RunProjectReviewResponse{}, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "project.review.completed", "review_run", runID, mustRawJSON(output)); err != nil {
		return RunProjectReviewResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RunProjectReviewResponse{}, err
	}
	return RunProjectReviewResponse{
		ReviewRunID: runID,
		Status:      "succeeded",
		Summary:     mustRawJSON(summary),
		ItemCount:   len(items),
	}, nil
}

func (s *Server) runProjectReviewAgent(ctx context.Context, principal auth.Principal, project Project, deterministic []reviewpkg.ReviewItemDraft) ([]reviewpkg.ReviewItemDraft, string, string, string, error) {
	reviewContext, err := reviewpkg.BuildProjectReviewContext(ctx, s.db, project.ID)
	if err != nil {
		return nil, "", "", "", err
	}
	rendered, err := s.renderProjectPrompt(ctx, project, projectReviewPromptKey, map[string]any{
		"project": map[string]any{
			"json": string(rawFromContext(reviewContext, "project")),
		},
		"deterministic": map[string]any{
			"json": string(mustRawJSON(map[string]any{
				"summary": reviewpkg.Summarize(deterministic),
				"items":   deterministic,
			})),
		},
		"context": map[string]any{
			"json": string(mustRawJSON(reviewContext)),
		},
	})
	if err != nil {
		return nil, "", "", "", err
	}
	agentCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := provider.NewGatewayClientFromEnv().GenerateText(agentCtx, provider.GatewayTextRequest{
		GatewayBillingIdentity: provider.GatewayBillingIdentity{
			RequestedByUserID:          principal.UserID,
			BillingOperationPermission: authz.PermissionProjectRead,
			BillingContextReason:       provider.BillingContextReasonManualProvider,
		},
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustMarshal(map[string]any{
			"prompt":         rendered.RenderedText,
			"responseFormat": "json",
		}),
		Options: provider.GatewayTextOptions{
			TimeoutMS: 15000,
			IdempotencyKey: gatewayProviderIdempotencyKey(
				ctx,
				provider.TaskTypeTextGenerate,
				project.ID,
				"project-review",
				rendered.RenderedHash,
			),
		},
	})
	if err != nil {
		return nil, "", rendered.PromptVersionID, rendered.RenderedHash, err
	}
	items, err := normalizeAgentReviewItems(project.ID, resp.Output.Text)
	if err != nil {
		return nil, resp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash, err
	}
	return items, resp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash, nil
}

func (s *Server) renderProjectPrompt(ctx context.Context, project Project, templateKey string, variables map[string]any) (promptsvc.RenderedPrompt, error) {
	resolved, err := promptsvc.NewService(s.db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	return promptsvc.Render(resolved, variables)
}

func (s *Server) listReviewRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), reviewRunSelectSQL(`WHERE project_id = $1 ORDER BY created_at DESC LIMIT 100`), project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ReviewRun, 0)
	for rows.Next() {
		item, err := scanReviewRun(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getReviewRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := scanReviewRun(s.db.QueryRow(r.Context(), reviewRunSelectSQL(`WHERE project_id = $1 AND id = $2`), project.ID, r.PathValue("reviewRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listReviewItems(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	query := r.URL.Query()
	status := strings.TrimSpace(query.Get("status"))
	if status == "" {
		status = "all"
	}
	page, err := s.listReviewItemsAction(r.Context(), project, reviewItemListActionInput{
		Status:     status,
		Severity:   strings.TrimSpace(query.Get("severity")),
		Category:   strings.TrimSpace(query.Get("category")),
		EntityType: strings.TrimSpace(query.Get("entityType")),
		Limit:      queryInt(r, "limit", 200),
		Cursor:     strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items": page.Items, "hasMore": page.NextCursor != "", "nextCursor": page.NextCursor,
	}, nil)
}

func (s *Server) getReviewItem(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := scanReviewItem(s.db.QueryRow(r.Context(), reviewItemSelectSQL(`WHERE project_id = $1 AND id = $2`), project.ID, r.PathValue("itemId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) resolveReviewItem(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.updateReviewItemStatus(w, r, principal, "resolved")
}

func (s *Server) ignoreReviewItem(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.updateReviewItemStatus(w, r, principal, "ignored")
}

func (s *Server) reopenReviewItem(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.updateReviewItemStatus(w, r, principal, "open")
}

func (s *Server) updateReviewItemStatus(w http.ResponseWriter, r *http.Request, principal auth.Principal, status string) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	updated, err := s.updateReviewItemStatusActionTx(r.Context(), tx, principal, project, reviewItemStatusActionInput{
		ItemID: r.PathValue("itemId"), Status: status, Note: req.Note,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func insertReviewItem(ctx context.Context, tx pgx.Tx, organizationID, projectID, reviewRunID string, item reviewpkg.ReviewItemDraft, userID string) error {
	metadata := item.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if _, ok := metadata["actions"]; !ok {
		metadata["actions"] = reviewpkg.ActionsForItem(projectID, item)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO review_items(
			organization_id, project_id, review_run_id, item_type, category, severity, title, description, suggestion,
			entity_type, entity_id, related_entity_type, related_entity_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, NULLIF($11, '')::uuid, NULLIF($12, ''), NULLIF($13, '')::uuid, $14, $15)
	`, organizationID, projectID, reviewRunID, normalizeReviewItemType(item.ItemType), normalizeReviewCategory(item.Category), normalizeReviewSeverity(item.Severity), item.Title, item.Description, item.Suggestion, normalizeReviewEntityType(item.EntityType), item.EntityID, item.RelatedEntityType, item.RelatedEntityID, mustRawJSON(metadata), userID)
	return err
}

func (s *Server) failReviewRun(ctx context.Context, runID, code, message string) {
	_, _ = s.db.Exec(ctx, `
		UPDATE review_runs
		SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
		WHERE id = $1
	`, runID, code, message)
}

func reviewRunSelectSQL(where string) string {
	return `
		SELECT id, organization_id, project_id, workflow_run_id::text, review_type, status, summary, input, output,
		       provider_call_id::text, prompt_version_id::text, prompt_hash, error_code, error_message,
		       created_by::text, created_at, started_at, completed_at
		FROM review_runs
		` + where
}

func reviewItemSelectSQL(where string) string {
	return `
		SELECT id, organization_id, project_id, review_run_id::text, item_type, category, severity, title, description, suggestion,
		       entity_type, entity_id::text, related_entity_type, related_entity_id::text, status, resolution_note,
		       metadata, created_by::text, resolved_by::text, created_at, updated_at, resolved_at
		FROM review_items
		` + where
}

func scanReviewRun(row rowScan) (ReviewRun, error) {
	var item ReviewRun
	var workflowRunID, providerCallID, promptVersionID, promptHash, errorCode, errorMessage, createdBy sql.NullString
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &workflowRunID, &item.ReviewType, &item.Status, &item.Summary, &item.Input, &item.Output, &providerCallID, &promptVersionID, &promptHash, &errorCode, &errorMessage, &createdBy, &item.CreatedAt, &item.StartedAt, &item.CompletedAt)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.ProviderCallID = stringPtrFromNull(providerCallID)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanReviewItem(row rowScan) (ReviewItem, error) {
	var item ReviewItem
	var reviewRunID, suggestion, entityID, relatedEntityType, relatedEntityID, resolutionNote, createdBy, resolvedBy sql.NullString
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &reviewRunID, &item.ItemType, &item.Category, &item.Severity, &item.Title, &item.Description, &suggestion, &item.EntityType, &entityID, &relatedEntityType, &relatedEntityID, &item.Status, &resolutionNote, &item.Metadata, &createdBy, &resolvedBy, &item.CreatedAt, &item.UpdatedAt, &item.ResolvedAt)
	item.ReviewRunID = stringPtrFromNull(reviewRunID)
	item.Suggestion = stringPtrFromNull(suggestion)
	item.EntityID = stringPtrFromNull(entityID)
	item.RelatedEntityType = stringPtrFromNull(relatedEntityType)
	item.RelatedEntityID = stringPtrFromNull(relatedEntityID)
	item.ResolutionNote = stringPtrFromNull(resolutionNote)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.ResolvedBy = stringPtrFromNull(resolvedBy)
	item.Actions = actionsFromReviewMetadata(item.Metadata)
	return item, err
}

func actionsFromReviewMetadata(raw json.RawMessage) []reviewpkg.Action {
	var meta struct {
		Actions []reviewpkg.Action `json:"actions"`
	}
	_ = json.Unmarshal(raw, &meta)
	return meta.Actions
}

type agentReviewResponse struct {
	Summary map[string]any       `json:"summary"`
	Items   []agentReviewItemRaw `json:"items"`
}

type agentReviewItemRaw struct {
	ItemType    string `json:"itemType"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	EntityType  string `json:"entityType"`
	EntityID    string `json:"entityId"`
	EntityName  string `json:"entityName"`
}

func normalizeAgentReviewItems(projectID, text string) ([]reviewpkg.ReviewItemDraft, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var parsed agentReviewResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &parsed); err != nil {
		return nil, err
	}
	items := make([]reviewpkg.ReviewItemDraft, 0, len(parsed.Items))
	for _, raw := range parsed.Items {
		item := reviewpkg.ReviewItemDraft{
			ItemType:    normalizeReviewItemType(raw.ItemType),
			Category:    normalizeReviewCategory(raw.Category),
			Severity:    normalizeReviewSeverity(raw.Severity),
			Title:       strings.TrimSpace(raw.Title),
			Description: strings.TrimSpace(raw.Description),
			Suggestion:  strings.TrimSpace(raw.Suggestion),
			EntityType:  normalizeReviewEntityType(raw.EntityType),
			EntityID:    strings.TrimSpace(raw.EntityID),
			Metadata: map[string]any{
				"source":     "agent",
				"entityName": strings.TrimSpace(raw.EntityName),
			},
		}
		if item.Title == "" || item.Description == "" {
			continue
		}
		item.Metadata["actions"] = reviewpkg.ActionsForItem(projectID, item)
		items = append(items, item)
	}
	return items, nil
}

func rawFromContext(context map[string]any, key string) json.RawMessage {
	if value, ok := context[key].(json.RawMessage); ok {
		return value
	}
	return json.RawMessage(`{}`)
}

func validReviewType(value string) bool {
	switch value {
	case "project", "script", "assets", "storyboard", "production", "timeline", "final_video":
		return true
	default:
		return false
	}
}

func normalizeReviewItemType(value string) string {
	switch value {
	case "issue", "warning", "suggestion":
		return value
	default:
		return "issue"
	}
}

func normalizeReviewCategory(value string) string {
	switch value {
	case "script", "asset", "storyboard", "shot_asset", "shot_image", "shot_video", "timeline", "final_video":
		return value
	default:
		return "script"
	}
}

func normalizeReviewSeverity(value string) string {
	switch value {
	case "low", "medium", "high", "critical":
		return value
	default:
		return "medium"
	}
}

func normalizeReviewEntityType(value string) string {
	switch value {
	case "script_scene", "canonical_asset", "storyboard_shot", "shot_asset_requirement", "timeline_clip", "project_timeline", "final_video_version", "project":
		return value
	default:
		return "project"
	}
}
