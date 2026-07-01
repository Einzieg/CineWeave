package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type NovelEvent struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	SourceID       string          `json:"sourceId"`
	ChapterID      *string         `json:"chapterId,omitempty"`
	ChapterIndex   int             `json:"chapterIndex,omitempty"`
	EventIndex     int             `json:"eventIndex"`
	SequenceNo     int             `json:"sequenceNo"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	EventType      *string         `json:"eventType,omitempty"`
	Importance     int             `json:"importance"`
	TimelineHint   *string         `json:"timelineHint,omitempty"`
	LocationHint   *string         `json:"locationHint,omitempty"`
	EmotionalTone  *string         `json:"emotionalTone,omitempty"`
	Conflict       *string         `json:"conflict,omitempty"`
	Outcome        *string         `json:"outcome,omitempty"`
	AdaptationHint *string         `json:"adaptationHint,omitempty"`
	Characters     json.RawMessage `json:"characters"`
	Scenes         json.RawMessage `json:"scenes"`
	Props          json.RawMessage `json:"props"`
	Keywords       json.RawMessage `json:"keywords"`
	RawExcerpt     *string         `json:"rawExcerpt,omitempty"`
	ReviewStatus   string          `json:"reviewStatus"`
	ManualOverride bool            `json:"manualOverride"`
	StaleState     string          `json:"staleState"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedBy      *string         `json:"createdBy,omitempty"`
	EditedBy       *string         `json:"editedBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	EditedAt       *time.Time      `json:"editedAt,omitempty"`
}

type NovelEventLink struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	SourceEventID  string          `json:"sourceEventId"`
	TargetEventID  string          `json:"targetEventId"`
	LinkType       string          `json:"linkType"`
	Description    *string         `json:"description,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type AdaptationPlan struct {
	ID                    string          `json:"id"`
	OrganizationID        string          `json:"organizationId"`
	ProjectID             string          `json:"projectId"`
	SourceID              *string         `json:"sourceId,omitempty"`
	ScriptID              *string         `json:"scriptId,omitempty"`
	Title                 string          `json:"title"`
	Status                string          `json:"status"`
	TargetFormat          string          `json:"targetFormat"`
	TargetDurationSeconds *int            `json:"targetDurationSeconds,omitempty"`
	MaxShots              *int            `json:"maxShots,omitempty"`
	SelectedEventIDs      json.RawMessage `json:"selectedEventIds"`
	Structure             json.RawMessage `json:"structure"`
	Content               string          `json:"content"`
	PromptVersionID       *string         `json:"promptVersionId,omitempty"`
	PromptHash            *string         `json:"promptHash,omitempty"`
	ReviewStatus          string          `json:"reviewStatus"`
	ManualOverride        bool            `json:"manualOverride"`
	Metadata              json.RawMessage `json:"metadata"`
	CreatedBy             *string         `json:"createdBy,omitempty"`
	EditedBy              *string         `json:"editedBy,omitempty"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
	EditedAt              *time.Time      `json:"editedAt,omitempty"`
}

type extractNovelEventsRequest struct {
	ChapterIDs []string `json:"chapterIds"`
	Force      bool     `json:"force"`
}

type generateAdaptationPlanRequest struct {
	EventIDs              []string `json:"eventIds"`
	TargetFormat          string   `json:"targetFormat"`
	TargetDurationSeconds int      `json:"targetDurationSeconds"`
	MaxShots              int      `json:"maxShots"`
	Instruction           string   `json:"instruction"`
}

type generateScriptFromAdaptationPlanRequest struct {
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
}

func (s *Server) extractNovelEvents(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req extractNovelEventsRequest
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionSourceWrite,
		authz.PermissionScriptWrite,
		authz.PermissionNovelEventWrite,
	})
	if !ok {
		return
	}
	source, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if source.SourceType != "novel" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType must be novel", nil, false)
		return
	}
	input := map[string]any{
		"sourceId": source.ID,
		"force":    req.Force,
	}
	if len(req.ChapterIDs) > 0 {
		input["chapterIds"] = normalizeStringSlice(req.ChapterIDs)
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, "extract_novel_events", input, workflows.ExtractNovelEventsWorkflow)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) listSourceNovelEvents(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionNovelEventRead,
		authz.PermissionSourceRead,
		authz.PermissionScriptRead,
	})
	if !ok {
		return
	}
	source, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	events, err := s.novelEventsBySource(r, project.ID, source.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	links, err := s.novelEventLinksBySource(r, project.ID, source.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": events, "links": links}, nil)
}

func (s *Server) getNovelEvent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionNovelEventRead,
		authz.PermissionSourceRead,
		authz.PermissionScriptRead,
	})
	if !ok {
		return
	}
	item, err := s.novelEvent(r, project.ID, r.PathValue("eventId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateNovelEvent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Title          *string   `json:"title"`
		Summary        *string   `json:"summary"`
		EventType      *string   `json:"eventType"`
		Importance     *int      `json:"importance"`
		TimelineHint   *string   `json:"timelineHint"`
		LocationHint   *string   `json:"locationHint"`
		EmotionalTone  *string   `json:"emotionalTone"`
		Conflict       *string   `json:"conflict"`
		Outcome        *string   `json:"outcome"`
		AdaptationHint *string   `json:"adaptationHint"`
		Characters     *[]string `json:"characters"`
		Scenes         *[]string `json:"scenes"`
		Props          *[]string `json:"props"`
		Keywords       *[]string `json:"keywords"`
		RawExcerpt     *string   `json:"rawExcerpt"`
		ReviewStatus   *string   `json:"reviewStatus"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionNovelEventWrite,
		authz.PermissionSourceWrite,
		authz.PermissionScriptWrite,
	})
	if !ok {
		return
	}
	item, err := s.novelEvent(r, project.ID, r.PathValue("eventId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Title != nil {
		item.Title = strings.TrimSpace(*req.Title)
		if item.Title == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "title is required", nil, false)
			return
		}
	}
	if req.Summary != nil {
		item.Summary = strings.TrimSpace(*req.Summary)
		if item.Summary == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "summary is required", nil, false)
			return
		}
	}
	if req.EventType != nil {
		item.EventType = stringPtrFromValue(*req.EventType)
	}
	if req.Importance != nil {
		if *req.Importance < 1 || *req.Importance > 5 {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "importance must be between 1 and 5", nil, false)
			return
		}
		item.Importance = *req.Importance
	}
	if req.TimelineHint != nil {
		item.TimelineHint = stringPtrFromValue(*req.TimelineHint)
	}
	if req.LocationHint != nil {
		item.LocationHint = stringPtrFromValue(*req.LocationHint)
	}
	if req.EmotionalTone != nil {
		item.EmotionalTone = stringPtrFromValue(*req.EmotionalTone)
	}
	if req.Conflict != nil {
		item.Conflict = stringPtrFromValue(*req.Conflict)
	}
	if req.Outcome != nil {
		item.Outcome = stringPtrFromValue(*req.Outcome)
	}
	if req.AdaptationHint != nil {
		item.AdaptationHint = stringPtrFromValue(*req.AdaptationHint)
	}
	if req.RawExcerpt != nil {
		item.RawExcerpt = stringPtrFromValue(*req.RawExcerpt)
	}
	if req.Characters != nil {
		item.Characters = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Characters)))
	}
	if req.Scenes != nil {
		item.Scenes = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Scenes)))
	}
	if req.Props != nil {
		item.Props = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Props)))
	}
	if req.Keywords != nil {
		item.Keywords = json.RawMessage(mustMarshal(normalizeStringSlice(*req.Keywords)))
	}
	reviewStatus := "pending"
	if req.ReviewStatus != nil {
		reviewStatus = strings.TrimSpace(*req.ReviewStatus)
		if !validReviewStatus(reviewStatus) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus is invalid", nil, false)
			return
		}
	}
	result, err := s.updateNovelEventRow(r, project.ID, item, reviewStatus, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) reviewNovelEvent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionNovelEventWrite,
		authz.PermissionSourceWrite,
		authz.PermissionScriptWrite,
	})
	if !ok {
		return
	}
	resp, ok := s.updateReviewStatus(w, r, "novel_events", project.ID, r.PathValue("eventId"), principal.UserID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) listAdaptationPlans(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanRead,
		authz.PermissionScriptRead,
		authz.PermissionSourceRead,
	})
	if !ok {
		return
	}
	sourceID := strings.TrimSpace(r.URL.Query().Get("sourceId"))
	plans, err := s.adaptationPlans(r, project.ID, sourceID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": plans}, nil)
}

func (s *Server) createAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		SourceID              string          `json:"sourceId"`
		Title                 string          `json:"title"`
		Status                string          `json:"status"`
		TargetFormat          string          `json:"targetFormat"`
		TargetDurationSeconds int             `json:"targetDurationSeconds"`
		MaxShots              int             `json:"maxShots"`
		SelectedEventIDs      []string        `json:"selectedEventIds"`
		Structure             json.RawMessage `json:"structure"`
		Content               string          `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	})
	if !ok {
		return
	}
	if strings.TrimSpace(req.SourceID) != "" {
		if _, err := s.projectSource(r, project.ID, strings.TrimSpace(req.SourceID)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if strings.TrimSpace(req.Title) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "title is required", nil, false)
		return
	}
	status := firstNonEmpty(req.Status, "draft")
	if !validAdaptationPlanStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "status is invalid", nil, false)
		return
	}
	structure := req.Structure
	if len(structure) == 0 {
		structure = json.RawMessage(`{}`)
	}
	var planID string
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO adaptation_plans(
			organization_id, project_id, source_id, title, status, target_format,
			target_duration_seconds, max_shots, selected_event_ids, structure, content,
			manual_override, created_by
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, NULLIF($7, 0), NULLIF($8, 0), $9, $10, $11, true, $12)
		RETURNING id::text
	`, project.OrganizationID, project.ID, strings.TrimSpace(req.SourceID), strings.TrimSpace(req.Title), status,
		firstNonEmpty(req.TargetFormat, "short_video"), req.TargetDurationSeconds, req.MaxShots,
		json.RawMessage(mustMarshal(normalizeStringSlice(req.SelectedEventIDs))), structure, req.Content, principal.UserID).Scan(&planID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.adaptationPlan(r, project.ID, planID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanRead,
		authz.PermissionScriptRead,
		authz.PermissionSourceRead,
	})
	if !ok {
		return
	}
	item, err := s.adaptationPlan(r, project.ID, r.PathValue("planId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Title                 *string          `json:"title"`
		Status                *string          `json:"status"`
		TargetFormat          *string          `json:"targetFormat"`
		TargetDurationSeconds *int             `json:"targetDurationSeconds"`
		MaxShots              *int             `json:"maxShots"`
		SelectedEventIDs      *[]string        `json:"selectedEventIds"`
		Structure             *json.RawMessage `json:"structure"`
		Content               *string          `json:"content"`
		ReviewStatus          *string          `json:"reviewStatus"`
	}
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	})
	if !ok {
		return
	}
	item, err := s.adaptationPlan(r, project.ID, r.PathValue("planId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Title != nil {
		item.Title = strings.TrimSpace(*req.Title)
		if item.Title == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "title is required", nil, false)
			return
		}
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validAdaptationPlanStatus(status) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "status is invalid", nil, false)
			return
		}
		item.Status = status
	}
	if req.TargetFormat != nil {
		item.TargetFormat = firstNonEmpty(*req.TargetFormat, "short_video")
	}
	if req.TargetDurationSeconds != nil {
		item.TargetDurationSeconds = intPtrIfPositive(*req.TargetDurationSeconds)
	}
	if req.MaxShots != nil {
		item.MaxShots = intPtrIfPositive(*req.MaxShots)
	}
	if req.SelectedEventIDs != nil {
		item.SelectedEventIDs = json.RawMessage(mustMarshal(normalizeStringSlice(*req.SelectedEventIDs)))
	}
	if req.Structure != nil {
		item.Structure = json.RawMessage(*req.Structure)
		if len(item.Structure) == 0 {
			item.Structure = json.RawMessage(`{}`)
		}
	}
	if req.Content != nil {
		item.Content = strings.TrimSpace(*req.Content)
	}
	reviewStatus := "pending"
	if req.ReviewStatus != nil {
		reviewStatus = strings.TrimSpace(*req.ReviewStatus)
		if !validReviewStatus(reviewStatus) {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "reviewStatus is invalid", nil, false)
			return
		}
	}
	updated, err := s.updateAdaptationPlanRow(r, project.ID, item, reviewStatus, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) reviewAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	})
	if !ok {
		return
	}
	resp, ok := s.updateReviewStatus(w, r, "adaptation_plans", project.ID, r.PathValue("planId"), principal.UserID)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) activateAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	})
	if !ok {
		return
	}
	plan, err := s.adaptationPlan(r, project.ID, r.PathValue("planId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if plan.SourceID != nil {
		if _, err := tx.Exec(r.Context(), `
			UPDATE adaptation_plans
			SET status = 'draft', updated_at = now()
			WHERE project_id = $1 AND source_id = $2 AND id <> $3 AND status = 'active'
		`, project.ID, *plan.SourceID, plan.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE adaptation_plans
		SET status = 'active', updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, plan.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.adaptationPlan(r, project.ID, plan.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) generateAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req generateAdaptationPlanRequest
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	})
	if !ok {
		return
	}
	source, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if source.SourceType != "novel" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType must be novel", nil, false)
		return
	}
	events, warning, err := s.selectNovelEventsForPlan(r, project.ID, source.ID, normalizeStringSlice(req.EventIDs))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(events) == 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "no novel events are available for adaptation plan", nil, false)
		return
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "adaptation_plan_generation", map[string]any{
		"project": projectPromptVariables(project),
		"input": map[string]any{
			"targetFormat":          firstNonEmpty(req.TargetFormat, "short_video"),
			"targetDurationSeconds": req.TargetDurationSeconds,
			"maxShots":              req.MaxShots,
			"instruction":           strings.TrimSpace(req.Instruction),
		},
		"events": map[string]any{"items": string(mustMarshal(events))},
	}, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	draft, err := workflows.NormalizeAdaptationPlan(gatewayResp.Output.Text, workflowNovelEventRecords(events))
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadGateway, "PROVIDER_OUTPUT_INVALID", err.Error(), nil, false)
		return
	}
	plan, err := s.insertGeneratedAdaptationPlan(r, project, source.ID, req, rendered, gatewayResp, draft, warning, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, plan, nil)
}

func (s *Server) generateScriptFromAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req generateScriptFromAdaptationPlanRequest
	if !decode(w, r, &req) {
		return
	}
	project, ok := s.requireProjectAccessAny(w, r, principal, r.PathValue("projectId"), []string{
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
	})
	if !ok {
		return
	}
	plan, err := s.adaptationPlan(r, project.ID, r.PathValue("planId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	eventIDs := stringSliceFromRaw(plan.SelectedEventIDs)
	events, err := s.novelEventsByIDs(r, project.ID, optionalStringPtrValue(plan.SourceID), eventIDs)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "script_from_adaptation_plan", map[string]any{
		"project": projectPromptVariables(project),
		"input":   map[string]any{"instruction": strings.TrimSpace(req.Instruction)},
		"plan":    map[string]any{"id": plan.ID, "title": plan.Title, "content": plan.Content, "structure": string(plan.Structure)},
		"events":  map[string]any{"items": string(mustMarshal(events))},
	}, false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	content := strings.TrimSpace(gatewayResp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	if content == "" {
		httpx.WriteError(w, r, http.StatusBadGateway, "PROVIDER_OUTPUT_EMPTY", "provider gateway returned empty script content", nil, false)
		return
	}
	scriptID, versionID, err := s.createScriptFromAdaptationPlan(r, principal, project, plan, req.Title, content, rendered, gatewayResp)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"scriptId":         scriptID,
		"versionId":        versionID,
		"adaptationPlanId": plan.ID,
		"providerCallId":   gatewayResp.ProviderCallID,
		"modelId":          gatewayResp.ModelID,
		"content":          content,
	}, nil)
}

func (s *Server) requireProjectAccessAny(w http.ResponseWriter, r *http.Request, principal auth.Principal, projectID string, permissions []string) (Project, bool) {
	project, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return Project{}, false
	}
	if !s.authorizeAny(w, r, principal, permissions, authz.Resource{ProjectID: project.ID}) {
		return Project{}, false
	}
	return project, true
}

func (s *Server) runTextGatewayPrompt(r *http.Request, project Project, templateKey string, variables map[string]any, jsonResponse bool) (promptsvc.RenderedPrompt, provider.GatewayTextResponse, error) {
	resolved, err := promptsvc.NewService(s.db).Resolve(r.Context(), promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, err
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, err
	}
	input := map[string]any{"prompt": rendered.RenderedText}
	if jsonResponse {
		input["responseFormat"] = "json"
	}
	resp, err := provider.NewGatewayClientFromEnv().GenerateText(r.Context(), provider.GatewayTextRequest{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             json.RawMessage(mustMarshal(input)),
	})
	return rendered, resp, err
}

func (s *Server) insertGeneratedAdaptationPlan(r *http.Request, project Project, sourceID string, req generateAdaptationPlanRequest, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, draft workflows.AdaptationPlanDraft, warning, userID string) (AdaptationPlan, error) {
	metadata := map[string]any{
		"source":            "adaptation_plan_generation",
		"providerCallId":    gatewayResp.ProviderCallID,
		"modelId":           gatewayResp.ModelID,
		"promptTemplateKey": rendered.TemplateKey,
		"promptVersionId":   rendered.PromptVersionID,
		"promptHash":        rendered.RenderedHash,
		"logline":           draft.Logline,
		"theme":             draft.Theme,
		"omittedEvents":     json.RawMessage(draft.OmittedEvents),
		"visualStrategy":    draft.VisualStrategy,
		"characterStrategy": draft.CharacterStrategy,
		"shotStrategy":      draft.ShotStrategy,
		"estimatedShots":    draft.EstimatedShots,
		"notes":             draft.Notes,
	}
	if warning != "" {
		metadata["warning"] = warning
	}
	var planID string
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO adaptation_plans(
			organization_id, project_id, source_id, title, target_format, target_duration_seconds,
			max_shots, selected_event_ids, structure, content, prompt_version_id, prompt_hash,
			metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), NULLIF($7, 0), $8, $9, $10, NULLIF($11, '')::uuid, NULLIF($12, ''), $13, $14)
		RETURNING id::text
	`, project.OrganizationID, project.ID, sourceID, draft.Title, firstNonEmpty(req.TargetFormat, "short_video"),
		req.TargetDurationSeconds, req.MaxShots, json.RawMessage(mustMarshal(draft.SelectedEvents)),
		rawOrDefault(draft.Structure, `{}`), string(draft.Raw), rendered.PromptVersionID, rendered.RenderedHash,
		json.RawMessage(mustMarshal(metadata)), userID).Scan(&planID)
	if err != nil {
		return AdaptationPlan{}, err
	}
	return s.adaptationPlan(r, project.ID, planID)
}

func (s *Server) createScriptFromAdaptationPlan(r *http.Request, principal auth.Principal, project Project, plan AdaptationPlan, requestedTitle, content string, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse) (string, string, error) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(r.Context())
	title := strings.TrimSpace(requestedTitle)
	if title == "" {
		title = plan.Title + " Script"
	}
	title, err = uniqueScriptTitleTx(r, tx, project.ID, title)
	if err != nil {
		return "", "", err
	}
	sourceID := optionalStringPtrValue(plan.SourceID)
	script, err := scanScript(tx.QueryRow(r.Context(), scriptInsertSQL(), project.OrganizationID, project.ID, stringPtrFromValue(sourceID), title, "active", principal.UserID))
	if err != nil {
		return "", "", err
	}
	sourceType := "agent_generated"
	version, err := insertScriptVersionTx(r, tx, project, script.ID, 1, content, "markdown", &sourceType, rendered.PromptVersionID, rendered.RenderedHash, json.RawMessage(mustMarshal(map[string]any{
		"source":           "adaptation_plan_to_script",
		"adaptationPlanId": plan.ID,
		"sourceId":         sourceID,
		"providerCallId":   gatewayResp.ProviderCallID,
		"modelId":          gatewayResp.ModelID,
		"promptTemplate":   rendered.TemplateKey,
		"promptVersionId":  rendered.PromptVersionID,
		"promptHash":       rendered.RenderedHash,
	})), principal.UserID)
	if err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE adaptation_plans SET script_id = $2, updated_at = now() WHERE project_id = $1 AND id = $3`, project.ID, script.ID, plan.ID); err != nil {
		return "", "", err
	}
	if sourceID != "" {
		if _, err := tx.Exec(r.Context(), `UPDATE project_sources SET status = 'processed' WHERE project_id = $1 AND id = $2`, project.ID, sourceID); err != nil {
			return "", "", err
		}
	}
	return script.ID, version.ID, tx.Commit(r.Context())
}

func (s *Server) updateNovelEventRow(r *http.Request, projectID string, item NovelEvent, reviewStatus, userID string) (NovelEvent, error) {
	if _, err := s.db.Exec(r.Context(), `
		UPDATE novel_events
		SET title = $3,
		    summary = $4,
		    event_type = $5,
		    importance = $6,
		    timeline_hint = $7,
		    location_hint = $8,
		    emotional_tone = $9,
		    conflict = $10,
		    outcome = $11,
		    adaptation_hint = $12,
		    characters = $13,
		    scenes = $14,
		    props = $15,
		    keywords = $16,
		    raw_excerpt = $17,
		    review_status = $18,
		    manual_override = true,
		    edited_by = $19,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, item.ID, item.Title, item.Summary, optionalStringPtrValue(item.EventType), item.Importance,
		optionalStringPtrValue(item.TimelineHint), optionalStringPtrValue(item.LocationHint), optionalStringPtrValue(item.EmotionalTone),
		optionalStringPtrValue(item.Conflict), optionalStringPtrValue(item.Outcome), optionalStringPtrValue(item.AdaptationHint),
		rawOrDefault(item.Characters, `[]`), rawOrDefault(item.Scenes, `[]`), rawOrDefault(item.Props, `[]`), rawOrDefault(item.Keywords, `[]`),
		optionalStringPtrValue(item.RawExcerpt), reviewStatus, userID); err != nil {
		return NovelEvent{}, err
	}
	return s.novelEvent(r, projectID, item.ID)
}

func (s *Server) updateAdaptationPlanRow(r *http.Request, projectID string, item AdaptationPlan, reviewStatus, userID string) (AdaptationPlan, error) {
	if _, err := s.db.Exec(r.Context(), `
		UPDATE adaptation_plans
		SET title = $3,
		    status = $4,
		    target_format = $5,
		    target_duration_seconds = $6,
		    max_shots = $7,
		    selected_event_ids = $8,
		    structure = $9,
		    content = $10,
		    review_status = $11,
		    manual_override = true,
		    edited_by = $12,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, item.ID, item.Title, item.Status, item.TargetFormat, optionalIntPtrValue(item.TargetDurationSeconds),
		optionalIntPtrValue(item.MaxShots), rawOrDefault(item.SelectedEventIDs, `[]`), rawOrDefault(item.Structure, `{}`),
		item.Content, reviewStatus, userID); err != nil {
		return AdaptationPlan{}, err
	}
	return s.adaptationPlan(r, projectID, item.ID)
}

func (s *Server) novelEvent(r *http.Request, projectID, eventID string) (NovelEvent, error) {
	return scanNovelEvent(s.db.QueryRow(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1 AND e.id = $2
	`), projectID, eventID))
}

func (s *Server) novelEventsBySource(r *http.Request, projectID, sourceID string) ([]NovelEvent, error) {
	rows, err := s.db.Query(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1 AND e.source_id = $2
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNovelEvents(rows)
}

func (s *Server) novelEventsByIDs(r *http.Request, projectID, sourceID string, eventIDs []string) ([]NovelEvent, error) {
	if len(eventIDs) == 0 && sourceID != "" {
		return s.novelEventsBySource(r, projectID, sourceID)
	}
	rows, err := s.db.Query(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1
		  AND ($2 = '' OR e.source_id = $2::uuid)
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanNovelEvents(rows)
	if err != nil {
		return nil, err
	}
	if len(eventIDs) == 0 {
		return all, nil
	}
	wanted := map[string]bool{}
	for _, id := range eventIDs {
		wanted[strings.TrimSpace(id)] = true
	}
	out := make([]NovelEvent, 0, len(eventIDs))
	for _, event := range all {
		if wanted[event.ID] {
			out = append(out, event)
		}
	}
	return out, nil
}

func (s *Server) selectNovelEventsForPlan(r *http.Request, projectID, sourceID string, eventIDs []string) ([]NovelEvent, string, error) {
	if len(eventIDs) > 0 {
		events, err := s.novelEventsByIDs(r, projectID, sourceID, eventIDs)
		return events, "", err
	}
	approved, err := s.novelEventsByReviewStatus(r, projectID, sourceID, "approved")
	if err != nil {
		return nil, "", err
	}
	if len(approved) > 0 {
		return approved, "", nil
	}
	pending, err := s.novelEventsByReviewStatus(r, projectID, sourceID, "pending")
	if err != nil {
		return nil, "", err
	}
	if len(pending) > 0 {
		return pending, "No approved events were available, so pending events were used.", nil
	}
	return nil, "", nil
}

func (s *Server) novelEventsByReviewStatus(r *http.Request, projectID, sourceID, status string) ([]NovelEvent, error) {
	rows, err := s.db.Query(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1 AND e.source_id = $2 AND e.review_status = $3
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNovelEvents(rows)
}

func (s *Server) novelEventLinksBySource(r *http.Request, projectID, sourceID string) ([]NovelEventLink, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT l.id, l.organization_id, l.project_id, l.source_event_id, l.target_event_id,
		       l.link_type, l.description, l.metadata, l.created_at
		FROM novel_event_links l
		JOIN novel_events e ON e.id = l.source_event_id
		WHERE l.project_id = $1 AND e.source_id = $2
		ORDER BY e.sequence_no ASC, l.created_at ASC
	`, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]NovelEventLink, 0)
	for rows.Next() {
		var item NovelEventLink
		var description sql.NullString
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.SourceEventID, &item.TargetEventID, &item.LinkType, &description, &metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Description = stringPtrFromNull(description)
		item.Metadata = rawOrDefaultBytes(metadata, "{}")
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) adaptationPlans(r *http.Request, projectID, sourceID string) ([]AdaptationPlan, error) {
	args := []any{projectID}
	where := `WHERE p.project_id = $1`
	if strings.TrimSpace(sourceID) != "" {
		where += ` AND p.source_id = $2`
		args = append(args, strings.TrimSpace(sourceID))
	}
	rows, err := s.db.Query(r.Context(), adaptationPlanSelectSQL(where+`
		ORDER BY CASE WHEN p.status = 'active' THEN 0 ELSE 1 END, p.created_at DESC
	`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdaptationPlan, 0)
	for rows.Next() {
		item, err := scanAdaptationPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) adaptationPlan(r *http.Request, projectID, planID string) (AdaptationPlan, error) {
	return scanAdaptationPlan(s.db.QueryRow(r.Context(), adaptationPlanSelectSQL(`
		WHERE p.project_id = $1 AND p.id = $2
	`), projectID, planID))
}

func scanNovelEvents(rows pgx.Rows) ([]NovelEvent, error) {
	items := make([]NovelEvent, 0)
	for rows.Next() {
		item, err := scanNovelEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanNovelEvent(row rowScan) (NovelEvent, error) {
	var item NovelEvent
	var chapterID, eventType, timelineHint, locationHint, emotionalTone, conflict, outcome, adaptationHint, rawExcerpt, createdBy, editedBy sql.NullString
	var editedAt sql.NullTime
	var characters, scenes, props, keywords, metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.SourceID,
		&chapterID,
		&item.ChapterIndex,
		&item.EventIndex,
		&item.SequenceNo,
		&item.Title,
		&item.Summary,
		&eventType,
		&item.Importance,
		&timelineHint,
		&locationHint,
		&emotionalTone,
		&conflict,
		&outcome,
		&adaptationHint,
		&characters,
		&scenes,
		&props,
		&keywords,
		&rawExcerpt,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&metadata,
		&createdBy,
		&editedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&editedAt,
	)
	item.ChapterID = stringPtrFromNull(chapterID)
	item.EventType = stringPtrFromNull(eventType)
	item.TimelineHint = stringPtrFromNull(timelineHint)
	item.LocationHint = stringPtrFromNull(locationHint)
	item.EmotionalTone = stringPtrFromNull(emotionalTone)
	item.Conflict = stringPtrFromNull(conflict)
	item.Outcome = stringPtrFromNull(outcome)
	item.AdaptationHint = stringPtrFromNull(adaptationHint)
	item.RawExcerpt = stringPtrFromNull(rawExcerpt)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.EditedBy = stringPtrFromNull(editedBy)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	item.Characters = rawOrDefaultBytes(characters, "[]")
	item.Scenes = rawOrDefaultBytes(scenes, "[]")
	item.Props = rawOrDefaultBytes(props, "[]")
	item.Keywords = rawOrDefaultBytes(keywords, "[]")
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func scanAdaptationPlan(row rowScan) (AdaptationPlan, error) {
	var item AdaptationPlan
	var sourceID, scriptID, promptVersionID, promptHash, createdBy, editedBy sql.NullString
	var targetDuration, maxShots sql.NullInt64
	var editedAt sql.NullTime
	var selectedEventIDs, structure, metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&sourceID,
		&scriptID,
		&item.Title,
		&item.Status,
		&item.TargetFormat,
		&targetDuration,
		&maxShots,
		&selectedEventIDs,
		&structure,
		&item.Content,
		&promptVersionID,
		&promptHash,
		&item.ReviewStatus,
		&item.ManualOverride,
		&metadata,
		&createdBy,
		&editedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&editedAt,
	)
	item.SourceID = stringPtrFromNull(sourceID)
	item.ScriptID = stringPtrFromNull(scriptID)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.EditedBy = stringPtrFromNull(editedBy)
	if targetDuration.Valid {
		value := int(targetDuration.Int64)
		item.TargetDurationSeconds = &value
	}
	if maxShots.Valid {
		value := int(maxShots.Int64)
		item.MaxShots = &value
	}
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	item.SelectedEventIDs = rawOrDefaultBytes(selectedEventIDs, "[]")
	item.Structure = rawOrDefaultBytes(structure, "{}")
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func novelEventSelectSQL(where string) string {
	return `
		SELECT e.id, e.organization_id, e.project_id, e.source_id, e.chapter_id,
		       COALESCE(c.chapter_index, 0), e.event_index, e.sequence_no,
		       e.title, e.summary, e.event_type, e.importance,
		       e.timeline_hint, e.location_hint, e.emotional_tone, e.conflict,
		       e.outcome, e.adaptation_hint, e.characters, e.scenes, e.props,
		       e.keywords, e.raw_excerpt, e.review_status, e.manual_override,
		       e.stale_state, e.metadata, e.created_by, e.edited_by,
		       e.created_at, e.updated_at, e.edited_at
		FROM novel_events e
		LEFT JOIN novel_chapters c ON c.id = e.chapter_id
	` + where
}

func adaptationPlanSelectSQL(where string) string {
	return `
		SELECT p.id, p.organization_id, p.project_id, p.source_id, p.script_id,
		       p.title, p.status, p.target_format, p.target_duration_seconds,
		       p.max_shots, p.selected_event_ids, p.structure, p.content,
		       p.prompt_version_id, p.prompt_hash, p.review_status, p.manual_override,
		       p.metadata, p.created_by, p.edited_by, p.created_at, p.updated_at,
		       p.edited_at
		FROM adaptation_plans p
	` + where
}

func workflowNovelEventRecords(events []NovelEvent) []workflows.NovelEventRecord {
	out := make([]workflows.NovelEventRecord, 0, len(events))
	for _, event := range events {
		out = append(out, workflows.NovelEventRecord{
			ID:             event.ID,
			SourceID:       event.SourceID,
			ChapterID:      optionalStringPtrValue(event.ChapterID),
			ChapterIndex:   event.ChapterIndex,
			EventIndex:     event.EventIndex,
			SequenceNo:     event.SequenceNo,
			Title:          event.Title,
			Summary:        event.Summary,
			EventType:      optionalStringPtrValue(event.EventType),
			Importance:     event.Importance,
			TimelineHint:   optionalStringPtrValue(event.TimelineHint),
			LocationHint:   optionalStringPtrValue(event.LocationHint),
			EmotionalTone:  optionalStringPtrValue(event.EmotionalTone),
			Conflict:       optionalStringPtrValue(event.Conflict),
			Outcome:        optionalStringPtrValue(event.Outcome),
			AdaptationHint: optionalStringPtrValue(event.AdaptationHint),
			Characters:     rawOrDefault(event.Characters, `[]`),
			Scenes:         rawOrDefault(event.Scenes, `[]`),
			Props:          rawOrDefault(event.Props, `[]`),
			Keywords:       rawOrDefault(event.Keywords, `[]`),
			RawExcerpt:     optionalStringPtrValue(event.RawExcerpt),
			ReviewStatus:   event.ReviewStatus,
		})
	}
	return out
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func stringSliceFromRaw(raw json.RawMessage) []string {
	var values []string
	_ = json.Unmarshal(rawOrDefault(raw, `[]`), &values)
	return normalizeStringSlice(values)
}

func optionalStringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalIntPtrValue(value *int) any {
	if value == nil || *value <= 0 {
		return nil
	}
	return *value
}

func intPtrIfPositive(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func rawOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func validAdaptationPlanStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "draft", "active", "archived":
		return true
	default:
		return false
	}
}
