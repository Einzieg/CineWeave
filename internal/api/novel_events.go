package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
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
	Revision       int64           `json:"revision"`
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
	Revision              int64           `json:"revision"`
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

type generateScriptFromAdaptationPlanResult struct {
	ScriptID         string   `json:"scriptId"`
	VersionID        string   `json:"versionId"`
	AdaptationPlanID string   `json:"adaptationPlanId"`
	ProviderCallID   string   `json:"providerCallId,omitempty"`
	ProviderCallIDs  []string `json:"providerCallIds"`
	ModelID          string   `json:"modelId,omitempty"`
	ModelIDs         []string `json:"modelIds"`
	EpisodeCount     int      `json:"episodeCount"`
	Content          string   `json:"content"`
}

type scriptNovelChapterContext struct {
	ID           string `json:"id"`
	ChapterIndex int    `json:"chapterIndex"`
	VolumeIndex  int    `json:"volumeIndex,omitempty"`
	SectionIndex int    `json:"sectionIndex,omitempty"`
	Title        string `json:"title"`
	VolumeTitle  string `json:"volumeTitle,omitempty"`
	ChapterTitle string `json:"chapterTitle,omitempty"`
	Content      string `json:"content"`
}

type scriptNovelContext struct {
	SourceID             string                      `json:"sourceId"`
	SourceTitle          string                      `json:"sourceTitle"`
	EpisodeNumber        int                         `json:"episodeNumber"`
	ShotCount            int                         `json:"shotCount"`
	StartShotNumber      int                         `json:"startShotNumber"`
	PreviousSummary      string                      `json:"previousSummary"`
	CharacterPeriodTable string                      `json:"characterPeriodTable"`
	ReferenceText        string                      `json:"referenceText"`
	CurrentText          string                      `json:"currentText"`
	ChapterIDs           []string                    `json:"chapterIds"`
	Chapters             []scriptNovelChapterContext `json:"chapters"`
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
	if source.Status == "archived" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source is archived", nil, false)
		return
	}
	if source.SourceType != "novel" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType must be novel", nil, false)
		return
	}
	chapters, err := s.sourceChapters(r, project.ID, source.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(chapters) == 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "原文尚未完成分集/章节切分，请重新导入，或编辑保存时开启重新切分。", nil, false)
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
	chapterID := strings.TrimSpace(r.URL.Query().Get("chapterId"))
	events, err := s.novelEventsBySource(r, project.ID, source.ID, chapterID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	links, err := s.novelEventLinksBySource(r, project.ID, source.ID, chapterID)
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
		ExpectedRevision int64     `json:"expectedRevision"`
		Title            *string   `json:"title"`
		Summary          *string   `json:"summary"`
		EventType        *string   `json:"eventType"`
		Importance       *int      `json:"importance"`
		TimelineHint     *string   `json:"timelineHint"`
		LocationHint     *string   `json:"locationHint"`
		EmotionalTone    *string   `json:"emotionalTone"`
		Conflict         *string   `json:"conflict"`
		Outcome          *string   `json:"outcome"`
		AdaptationHint   *string   `json:"adaptationHint"`
		Characters       *[]string `json:"characters"`
		Scenes           *[]string `json:"scenes"`
		Props            *[]string `json:"props"`
		Keywords         *[]string `json:"keywords"`
		RawExcerpt       *string   `json:"rawExcerpt"`
		ReviewStatus     *string   `json:"reviewStatus"`
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
	actionInput := mustRawJSON(novelEventUpdateActionInput{
		EventID:          r.PathValue("eventId"),
		ExpectedRevision: req.ExpectedRevision,
		Patch: novelEventPatch{
			Title: req.Title, Summary: req.Summary, EventType: req.EventType, Importance: req.Importance,
			TimelineHint: req.TimelineHint, LocationHint: req.LocationHint, EmotionalTone: req.EmotionalTone,
			Conflict: req.Conflict, Outcome: req.Outcome, AdaptationHint: req.AdaptationHint,
			Characters: req.Characters, Scenes: req.Scenes, Props: req.Props, Keywords: req.Keywords,
			RawExcerpt: req.RawExcerpt, ReviewStatus: req.ReviewStatus,
		},
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "novel_event.update", actionInput, idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[NovelEvent](result, "event")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reviewNovelEvent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req revisionedReviewActionInput
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
	actionInput := mustRawJSON(novelEventReviewActionInput{EventID: r.PathValue("eventId"), revisionedReviewActionInput: req})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "novel_event.review", actionInput, idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resp, err := decodeAgentToolResultValue[ReviewResponse](result, "review")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
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
	var req adaptationPlanCreateActionInput
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
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "adaptation.create", mustRawJSON(req), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[AdaptationPlan](result, "plan")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
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
		ExpectedRevision      int64            `json:"expectedRevision"`
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
	actionInput := mustRawJSON(adaptationPlanUpdateActionInput{
		PlanID: r.PathValue("planId"), ExpectedRevision: req.ExpectedRevision,
		Patch: adaptationPlanPatch{
			Title: req.Title, Status: req.Status, TargetFormat: req.TargetFormat,
			TargetDurationSeconds: req.TargetDurationSeconds, MaxShots: req.MaxShots,
			SelectedEventIDs: req.SelectedEventIDs, Structure: req.Structure, Content: req.Content,
			ReviewStatus: req.ReviewStatus,
		},
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "adaptation.update", actionInput, idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[AdaptationPlan](result, "plan")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reviewAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req revisionedReviewActionInput
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
	actionInput := mustRawJSON(adaptationPlanReviewActionInput{PlanID: r.PathValue("planId"), revisionedReviewActionInput: req})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "adaptation.review", actionInput, idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resp, err := decodeAgentToolResultValue[ReviewResponse](result, "review")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) activateAdaptationPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req adaptationPlanActivateActionInput
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
	req.PlanID = r.PathValue("planId")
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "adaptation.activate", mustRawJSON(req), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolResultValue[AdaptationPlan](result, "plan")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
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
	if source.Status == "archived" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source is archived", nil, false)
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
	operationPermission := s.firstAuthorizedProjectPermission(
		r.Context(),
		principal,
		project.ID,
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
		authz.PermissionSourceWrite,
	)
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "adaptation_plan_generation", map[string]any{
		"project": projectPromptVariables(project),
		"input": map[string]any{
			"targetFormat":          firstNonEmpty(req.TargetFormat, "short_video"),
			"targetDurationSeconds": req.TargetDurationSeconds,
			"maxShots":              req.MaxShots,
			"instruction":           strings.TrimSpace(req.Instruction),
		},
		"events": map[string]any{"items": string(mustMarshal(events))},
	}, true, operationPermission, provider.BillingContextReasonManualProvider)
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
	result, _, err := s.generateScriptFromAdaptationPlanCore(
		r.Context(), principal, project, r.PathValue("planId"), req, "",
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, result, nil)
}

func (s *Server) generateScriptFromAdaptationPlanCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	planID string,
	req generateScriptFromAdaptationPlanRequest,
	commandID string,
) (generateScriptFromAdaptationPlanResult, bool, error) {
	commandID = strings.TrimSpace(commandID)
	planID = strings.TrimSpace(planID)
	if commandID != "" {
		existing, found, err := s.scriptFromAdaptationPlanCommand(ctx, project.ID, planID, commandID)
		if err != nil {
			return generateScriptFromAdaptationPlanResult{}, false, err
		}
		if found {
			return existing, true, nil
		}
	}
	r := requestWithContext(ctx)
	plan, err := s.adaptationPlan(r, project.ID, planID)
	if err != nil {
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	eventIDs := stringSliceFromRaw(plan.SelectedEventIDs)
	events, err := s.novelEventsByIDs(r, project.ID, optionalStringPtrValue(plan.SourceID), eventIDs)
	if err != nil {
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	novelContext, err := s.scriptNovelContextForPlan(r, project.ID, plan, events)
	if err != nil {
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	operationPermission := s.firstAuthorizedProjectPermission(
		ctx,
		principal,
		project.ID,
		authz.PermissionAdaptationPlanWrite,
		authz.PermissionScriptWrite,
	)
	episodeDrafts, providerCallIDs, modelIDs, versionPromptVersionID, versionPromptHash, err := s.generateScriptEpisodeDraftsFromPlan(
		r,
		project,
		plan,
		req,
		events,
		novelContext,
		operationPermission,
	)
	if err != nil {
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	content := scriptVersionContentFromEpisodeDrafts(episodeDrafts)
	if content == "" {
		return generateScriptFromAdaptationPlanResult{}, false, newAPIError(
			http.StatusBadGateway, "PROVIDER_OUTPUT_EMPTY", "provider gateway returned empty script content",
		)
	}
	scriptID, versionID, err := s.createScriptFromAdaptationPlan(r, principal, project, plan, req.Title, content, episodeDrafts, versionPromptVersionID, versionPromptHash, providerCallIDs, modelIDs, commandID)
	if err != nil {
		if commandID != "" {
			existing, found, replayErr := s.scriptFromAdaptationPlanCommand(ctx, project.ID, plan.ID, commandID)
			if replayErr != nil {
				return generateScriptFromAdaptationPlanResult{}, false, replayErr
			}
			if found {
				return existing, true, nil
			}
		}
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	return generateScriptFromAdaptationPlanResult{
		ScriptID:         scriptID,
		VersionID:        versionID,
		AdaptationPlanID: plan.ID,
		ProviderCallID:   firstString(providerCallIDs),
		ProviderCallIDs:  providerCallIDs,
		ModelID:          firstString(modelIDs),
		ModelIDs:         uniqueNonEmptyStrings(modelIDs),
		EpisodeCount:     len(episodeDrafts),
		Content:          content,
	}, false, nil
}

func (s *Server) generateScriptEpisodeDraftsFromPlan(
	r *http.Request,
	project Project,
	plan AdaptationPlan,
	req generateScriptFromAdaptationPlanRequest,
	events []NovelEvent,
	novelContext scriptNovelContext,
	operationPermission string,
) ([]scriptEpisodeDraft, []string, []string, string, string, error) {
	if len(novelContext.Chapters) == 0 {
		rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "script_from_adaptation_plan", map[string]any{
			"project": projectPromptVariables(project),
			"input":   map[string]any{"instruction": strings.TrimSpace(req.Instruction)},
			"plan":    map[string]any{"id": plan.ID, "title": plan.Title, "content": plan.Content, "structure": string(plan.Structure)},
			"events":  map[string]any{"items": string(mustMarshal(events))},
			"novel":   novelContext,
		}, false, operationPermission, provider.BillingContextReasonManualProvider)
		if err != nil {
			return nil, nil, nil, "", "", err
		}
		content := strings.TrimSpace(gatewayResp.Output.Text)
		if content == "" {
			content = strings.TrimSpace(string(gatewayResp.Output.Raw))
		}
		if content == "" {
			return nil, nil, nil, "", "", newAPIError(http.StatusBadGateway, "PROVIDER_OUTPUT_EMPTY", "provider gateway returned empty script content")
		}
		sourceID := optionalStringPtrValue(plan.SourceID)
		var sourceIDPtr *string
		if sourceID != "" {
			sourceIDPtr = &sourceID
		}
		return []scriptEpisodeDraft{
				defaultScriptEpisodeDraft(sourceIDPtr, "第 1 集", content, "markdown", rendered.PromptVersionID, rendered.RenderedHash, gatewayResp.ProviderCallID, mustRawJSON(map[string]any{
					"source":            "adaptation_plan_to_script",
					"adaptationPlanId":  plan.ID,
					"sourceId":          sourceID,
					"providerCallId":    gatewayResp.ProviderCallID,
					"modelId":           gatewayResp.ModelID,
					"promptTemplateKey": rendered.TemplateKey,
					"promptVersionId":   rendered.PromptVersionID,
					"promptHash":        rendered.RenderedHash,
				})),
			},
			[]string{gatewayResp.ProviderCallID},
			[]string{gatewayResp.ModelID},
			rendered.PromptVersionID,
			rendered.RenderedHash,
			nil
	}

	episodeDrafts := make([]scriptEpisodeDraft, 0, len(novelContext.Chapters))
	providerCallIDs := make([]string, 0, len(novelContext.Chapters))
	modelIDs := make([]string, 0, len(novelContext.Chapters))
	versionPromptVersionID := ""
	versionPromptHash := ""
	for i, chapter := range novelContext.Chapters {
		perChapterContext := scriptNovelContextForSingleChapter(novelContext, chapter)
		chapterEvents := novelEventsForChapter(events, chapter.ID)
		instruction := scriptEpisodeGenerationInstruction(req.Instruction, i+1, len(novelContext.Chapters), chapter)
		rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "script_from_adaptation_plan", map[string]any{
			"project": projectPromptVariables(project),
			"input":   map[string]any{"instruction": instruction},
			"plan":    map[string]any{"id": plan.ID, "title": plan.Title, "content": plan.Content, "structure": string(plan.Structure)},
			"events":  map[string]any{"items": string(mustMarshal(chapterEvents))},
			"novel":   perChapterContext,
		}, false, operationPermission, provider.BillingContextReasonManualProvider)
		if err != nil {
			return nil, nil, nil, "", "", err
		}
		content := strings.TrimSpace(gatewayResp.Output.Text)
		if content == "" {
			content = strings.TrimSpace(string(gatewayResp.Output.Raw))
		}
		if content == "" {
			return nil, nil, nil, "", "", newAPIError(http.StatusBadGateway, "PROVIDER_OUTPUT_EMPTY", "provider gateway returned empty script content")
		}
		if versionPromptVersionID == "" {
			versionPromptVersionID = rendered.PromptVersionID
			versionPromptHash = rendered.RenderedHash
		}
		providerCallIDs = append(providerCallIDs, gatewayResp.ProviderCallID)
		modelIDs = append(modelIDs, gatewayResp.ModelID)
		episodeDrafts = append(episodeDrafts, scriptEpisodeDraftFromNovelChapter(i+1, novelContext.SourceID, chapter, content, rendered.PromptVersionID, rendered.RenderedHash, gatewayResp.ProviderCallID, mustRawJSON(map[string]any{
			"source":             "adaptation_plan_to_script",
			"adaptationPlanId":   plan.ID,
			"sourceId":           novelContext.SourceID,
			"sourceChapterId":    chapter.ID,
			"sourceChapterTitle": chapter.Title,
			"providerCallId":     gatewayResp.ProviderCallID,
			"modelId":            gatewayResp.ModelID,
			"promptTemplateKey":  rendered.TemplateKey,
			"promptVersionId":    rendered.PromptVersionID,
			"promptHash":         rendered.RenderedHash,
		})))
	}
	return episodeDrafts, providerCallIDs, modelIDs, versionPromptVersionID, versionPromptHash, nil
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
	if !s.enforceProjectRouteKind(w, r, project) {
		return Project{}, false
	}
	return project, true
}

func (s *Server) runTextGatewayPrompt(
	r *http.Request,
	project Project,
	templateKey string,
	variables map[string]any,
	jsonResponse bool,
	operationPermission string,
	reason string,
) (promptsvc.RenderedPrompt, provider.GatewayTextResponse, error) {
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
		GatewayBillingIdentity: gatewayBillingIdentityFromContext(
			r.Context(),
			operationPermission,
			reason,
		),
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             json.RawMessage(mustMarshal(input)),
		Options: provider.GatewayTextOptions{
			IdempotencyKey: gatewayProviderIdempotencyKey(
				r.Context(),
				provider.TaskTypeTextGenerate,
				project.ID,
				templateKey,
				rendered.RenderedHash,
			),
		},
	})
	return rendered, resp, err
}

func (s *Server) scriptNovelContextForPlan(r *http.Request, projectID string, plan AdaptationPlan, events []NovelEvent) (scriptNovelContext, error) {
	context := scriptNovelContext{
		SourceID:             optionalStringPtrValue(plan.SourceID),
		EpisodeNumber:        1,
		ShotCount:            shotCountFromAdaptationPlan(plan),
		StartShotNumber:      1,
		PreviousSummary:      "无",
		CharacterPeriodTable: characterPeriodTableFromNovelEvents(events),
		ReferenceText:        "全文较长时不直接注入；请以小说正文为唯一转换范围，并以改编计划和事件摘要理解上下文。",
	}
	if context.SourceID == "" {
		return context, nil
	}
	source, err := s.projectSource(r, projectID, context.SourceID)
	if err != nil {
		return scriptNovelContext{}, err
	}
	context.SourceTitle = source.Title
	chapterIDs := chapterIDsFromNovelEvents(events)
	chapters, err := s.scriptNovelChapters(r, projectID, context.SourceID, chapterIDs)
	if err != nil {
		return scriptNovelContext{}, err
	}
	context.Chapters = scriptNovelChapterContexts(chapters)
	context.ChapterIDs = make([]string, 0, len(context.Chapters))
	for _, chapter := range context.Chapters {
		context.ChapterIDs = append(context.ChapterIDs, chapter.ID)
	}
	context.CurrentText = scriptNovelCurrentText(context.Chapters)
	if context.CurrentText == "" {
		context.CurrentText = strings.TrimSpace(source.Content)
	}
	if len(context.Chapters) > 0 {
		context.EpisodeNumber = firstPositiveInt(context.Chapters[0].SectionIndex, context.Chapters[0].ChapterIndex, 1)
	}
	if compactSource := strings.TrimSpace(source.Content); compactSource != "" && len([]rune(compactSource)) <= 20000 {
		context.ReferenceText = compactSource
	}
	return context, nil
}

func scriptNovelContextForSingleChapter(base scriptNovelContext, chapter scriptNovelChapterContext) scriptNovelContext {
	next := base
	next.ChapterIDs = []string{chapter.ID}
	next.Chapters = []scriptNovelChapterContext{chapter}
	next.CurrentText = scriptNovelCurrentText(next.Chapters)
	next.EpisodeNumber = firstPositiveInt(chapter.SectionIndex, chapter.ChapterIndex, 1)
	if strings.TrimSpace(next.CurrentText) != "" && len([]rune(next.CurrentText)) <= 20000 {
		next.ReferenceText = next.CurrentText
	} else {
		next.ReferenceText = "仅使用当前分集正文进行剧本化；上下文只用于人物和风格一致性。"
	}
	return next
}

func novelEventsForChapter(events []NovelEvent, chapterID string) []NovelEvent {
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return events
	}
	items := make([]NovelEvent, 0, len(events))
	emptyChapterItems := make([]NovelEvent, 0)
	for _, event := range events {
		eventChapterID := optionalStringPtrValue(event.ChapterID)
		if eventChapterID == chapterID {
			items = append(items, event)
			continue
		}
		if eventChapterID == "" {
			emptyChapterItems = append(emptyChapterItems, event)
		}
	}
	if len(items) == 0 {
		return append(items, emptyChapterItems...)
	}
	return append(items, emptyChapterItems...)
}

func (s *Server) scriptNovelChapters(r *http.Request, projectID, sourceID string, chapterIDs []string) ([]NovelChapter, error) {
	return s.scriptNovelChaptersContext(r.Context(), projectID, sourceID, chapterIDs)
}

func (s *Server) scriptNovelChaptersContext(ctx context.Context, projectID, sourceID string, chapterIDs []string) ([]NovelChapter, error) {
	all, err := s.sourceChaptersContext(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if len(chapterIDs) == 0 {
		return all, nil
	}
	wanted := map[string]bool{}
	for _, id := range chapterIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	items := make([]NovelChapter, 0, len(wanted))
	for _, chapter := range all {
		if wanted[chapter.ID] {
			items = append(items, chapter)
		}
	}
	return items, nil
}

func scriptNovelChapterContexts(chapters []NovelChapter) []scriptNovelChapterContext {
	out := make([]scriptNovelChapterContext, 0, len(chapters))
	for _, chapter := range chapters {
		out = append(out, scriptNovelChapterContext{
			ID:           chapter.ID,
			ChapterIndex: chapter.ChapterIndex,
			VolumeIndex:  optionalIntValue(chapter.VolumeIndex),
			SectionIndex: optionalIntValue(chapter.SectionIndex),
			Title:        novelChapterPromptTitle(chapter),
			VolumeTitle:  optionalStringPtrValue(chapter.VolumeTitle),
			ChapterTitle: optionalStringPtrValue(chapter.ChapterTitle),
			Content:      chapter.Content,
		})
	}
	return out
}

func scriptNovelCurrentText(chapters []scriptNovelChapterContext) string {
	var builder strings.Builder
	for _, chapter := range chapters {
		content := strings.TrimSpace(chapter.Content)
		if content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("【")
		builder.WriteString(chapter.Title)
		builder.WriteString("】\n")
		builder.WriteString(content)
	}
	return strings.TrimSpace(builder.String())
}

func chapterIDsFromNovelEvents(events []NovelEvent) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, event := range events {
		id := optionalStringPtrValue(event.ChapterID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func characterPeriodTableFromNovelEvents(events []NovelEvent) string {
	names := []string{}
	seen := map[string]bool{}
	for _, event := range events {
		var characters []string
		if err := json.Unmarshal(rawOrDefault(event.Characters, `[]`), &characters); err != nil {
			continue
		}
		for _, name := range normalizeStringSlice(characters) {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "未配置人物时期表；按小说正文中的标准名称和时期线索标注，禁止新增人物、改名或使用别名。"
	}
	return "未配置正式人物时期表；候选人物：" + strings.Join(names, "、") + "。按小说正文中的标准名称和时期线索标注，禁止新增人物、改名或使用别名。"
}

func shotCountFromAdaptationPlan(plan AdaptationPlan) int {
	if plan.MaxShots != nil && *plan.MaxShots > 0 {
		return *plan.MaxShots
	}
	var metadata map[string]any
	if err := json.Unmarshal(rawOrDefault(plan.Metadata, `{}`), &metadata); err == nil {
		switch value := metadata["estimatedShots"].(type) {
		case float64:
			if value > 0 {
				return int(value)
			}
		case int:
			if value > 0 {
				return value
			}
		}
	}
	return 8
}

func novelChapterPromptTitle(chapter NovelChapter) string {
	parts := []string{}
	if chapter.VolumeIndex != nil && *chapter.VolumeIndex > 0 {
		parts = append(parts, "第 "+strconv.Itoa(*chapter.VolumeIndex)+"卷")
	}
	if chapter.SectionIndex != nil && *chapter.SectionIndex > 0 {
		parts = append(parts, "第 "+strconv.Itoa(*chapter.SectionIndex)+"节")
	}
	if chapter.VolumeTitle != nil && strings.TrimSpace(*chapter.VolumeTitle) != "" {
		parts = append(parts, strings.TrimSpace(*chapter.VolumeTitle))
	}
	if chapter.ChapterTitle != nil && strings.TrimSpace(*chapter.ChapterTitle) != "" {
		parts = append(parts, strings.TrimSpace(*chapter.ChapterTitle))
	}
	if len(parts) == 0 {
		return "第 " + strconv.Itoa(chapter.ChapterIndex) + " 集"
	}
	return strings.Join(parts, " ")
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

func (s *Server) createScriptFromAdaptationPlan(r *http.Request, principal auth.Principal, project Project, plan AdaptationPlan, requestedTitle, content string, episodeDrafts []scriptEpisodeDraft, versionPromptVersionID, versionPromptHash string, providerCallIDs, modelIDs []string, commandID string) (string, string, error) {
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
	metadata := map[string]any{
		"source":           "adaptation_plan_to_script",
		"adaptationPlanId": plan.ID,
		"sourceId":         sourceID,
		"providerCallId":   firstString(providerCallIDs),
		"providerCallIds":  providerCallIDs,
		"modelId":          firstString(modelIDs),
		"modelIds":         uniqueNonEmptyStrings(modelIDs),
		"promptTemplate":   "script_from_adaptation_plan",
		"promptVersionId":  versionPromptVersionID,
		"promptHash":       versionPromptHash,
		"episodeCount":     len(episodeDrafts),
	}
	if commandID = strings.TrimSpace(commandID); commandID != "" {
		metadata["projectControlCommandId"] = commandID
		metadata["controllerType"] = "project_control"
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, 1, content, "markdown", &sourceType, versionPromptVersionID, versionPromptHash, json.RawMessage(mustMarshal(metadata)), principal.UserID)
	if err != nil {
		return "", "", err
	}
	if _, err := insertScriptEpisodesTx(r, tx, project, script.ID, version.ID, principal.UserID, episodeDrafts); err != nil {
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

func (s *Server) scriptFromAdaptationPlanCommand(ctx context.Context, projectID, planID, commandID string) (generateScriptFromAdaptationPlanResult, bool, error) {
	var result generateScriptFromAdaptationPlanResult
	var providerCallIDsRaw, modelIDsRaw []byte
	err := s.db.QueryRow(ctx, `
		SELECT version.script_id::text, version.id::text, version.content,
		       COALESCE(version.metadata->>'providerCallId', ''),
		       COALESCE(version.metadata->'providerCallIds', '[]'::jsonb),
		       COALESCE(version.metadata->>'modelId', ''),
		       COALESCE(version.metadata->'modelIds', '[]'::jsonb),
		       (SELECT COUNT(*) FROM script_episodes episode WHERE episode.script_version_id = version.id)
		FROM script_versions version
		WHERE version.project_id = $1
		  AND version.metadata->>'adaptationPlanId' = $2
		  AND version.metadata->>'projectControlCommandId' = $3
		ORDER BY version.created_at DESC
		LIMIT 1
	`, projectID, planID, commandID).Scan(
		&result.ScriptID, &result.VersionID, &result.Content,
		&result.ProviderCallID, &providerCallIDsRaw,
		&result.ModelID, &modelIDsRaw, &result.EpisodeCount,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return generateScriptFromAdaptationPlanResult{}, false, nil
		}
		return generateScriptFromAdaptationPlanResult{}, false, err
	}
	result.AdaptationPlanID = planID
	result.ProviderCallIDs = stringSliceFromRaw(providerCallIDsRaw)
	result.ModelIDs = stringSliceFromRaw(modelIDsRaw)
	return result, true, nil
}

func (s *Server) novelEvent(r *http.Request, projectID, eventID string) (NovelEvent, error) {
	return scanNovelEvent(s.db.QueryRow(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1 AND e.id = $2
	`), projectID, eventID))
}

func (s *Server) novelEventsBySource(r *http.Request, projectID, sourceID string, chapterID string) ([]NovelEvent, error) {
	rows, err := s.db.Query(r.Context(), novelEventSelectSQL(`
		WHERE e.project_id = $1 AND e.source_id = $2
		  AND ($3 = '' OR e.chapter_id = $3::uuid)
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID, strings.TrimSpace(chapterID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNovelEvents(rows)
}

func (s *Server) novelEventsByIDs(r *http.Request, projectID, sourceID string, eventIDs []string) ([]NovelEvent, error) {
	if len(eventIDs) == 0 && sourceID != "" {
		return s.novelEventsBySource(r, projectID, sourceID, "")
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

func (s *Server) novelEventLinksBySource(r *http.Request, projectID, sourceID string, chapterID string) ([]NovelEventLink, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT l.id, l.organization_id, l.project_id, l.source_event_id, l.target_event_id,
		       l.link_type, l.description, l.metadata, l.created_at
		FROM novel_event_links l
		JOIN novel_events e ON e.id = l.source_event_id
		JOIN novel_events target ON target.id = l.target_event_id
		WHERE l.project_id = $1 AND e.source_id = $2
		  AND ($3 = '' OR (e.chapter_id = $3::uuid AND target.chapter_id = $3::uuid))
		ORDER BY e.sequence_no ASC, l.created_at ASC
	`, projectID, sourceID, strings.TrimSpace(chapterID))
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
		&item.Revision,
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
		&item.Revision,
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
		       e.created_at, e.updated_at, e.edited_at, e.revision
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
		       p.edited_at, p.revision
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

func optionalIntValue(value *int) int {
	if value == nil {
		return 0
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
