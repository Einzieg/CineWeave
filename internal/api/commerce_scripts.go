package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func (s *Server) listCommerceScriptUnits(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.commerceCatalog.ListScriptUnits(r.Context(), s.db, project.OrganizationID, project.ID,
		r.URL.Query().Get("filter[status]"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if commerceIncludeRequested(r.URL.Query().Get("include"), "productionSummary") {
		if err := s.attachCommerceProductionSummaries(r.Context(), project, items.Items); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, items, nil)
}

func commerceIncludeRequested(value, target string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func (s *Server) getCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetScriptUnit(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedScriptUnitsRevision int64   `json:"expectedScriptUnitsRevision"`
		Title                       string  `json:"title"`
		Content                     string  `json:"content"`
		LanguageMode                string  `json:"languageMode"`
		ExplicitTargetLanguage      *string `json:"explicitTargetLanguage"`
		TargetDurationSeconds       int     `json:"targetDurationSeconds"`
		TargetPlatform              string  `json:"targetPlatform"`
		SourceLanguageHint          *string `json:"sourceLanguageHint"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.validateCommerceScriptContentForCurrentVideoModel(r.Context(), project, req.Content); err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.CreateScriptUnit(r.Context(), tx, project.OrganizationID, project.ID, principal.UserID,
		req.ExpectedScriptUnitsRevision, commercepkg.CreateScriptUnitInput{
			Title: req.Title, Content: req.Content, LanguageMode: req.LanguageMode,
			ExplicitTargetLanguage: req.ExplicitTargetLanguage, TargetDurationSeconds: req.TargetDurationSeconds,
			TargetPlatform: req.TargetPlatform, SourceLanguageHint: req.SourceLanguageHint,
		})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptMutationEvents(
		r.Context(), tx, project.OrganizationID, project.ID, item, "commerce.script_unit.created",
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision       int64   `json:"expectedRevision"`
		Title                  *string `json:"title"`
		DraftContent           *string `json:"draftContent"`
		LanguageMode           *string `json:"languageMode"`
		ExplicitTargetLanguage *string `json:"explicitTargetLanguage"`
		TargetDurationSeconds  *int    `json:"targetDurationSeconds"`
		TargetPlatform         *string `json:"targetPlatform"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.DraftContent != nil {
		if err := s.validateCommerceScriptContentForCurrentVideoModel(r.Context(), project, *req.DraftContent); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.UpdateScriptUnit(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), req.ExpectedRevision, commercepkg.UpdateScriptUnitInput{
			Title: req.Title, DraftContent: req.DraftContent, LanguageMode: req.LanguageMode,
			ExplicitTargetLanguage: req.ExplicitTargetLanguage, TargetDurationSeconds: req.TargetDurationSeconds,
			TargetPlatform: req.TargetPlatform,
		})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptUnitEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script_unit.updated", item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) archiveCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
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
	item, err := s.commerceCatalog.ArchiveScriptUnit(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"), req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptUnitEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script_unit.archived", item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reorderCommerceScriptUnits(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedScriptUnitsRevision int64                               `json:"expectedScriptUnitsRevision"`
		Items                       []commercepkg.ReorderScriptUnitItem `json:"items"`
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
	revision, err := s.commerceCatalog.ReorderScriptUnits(r.Context(), tx, project.OrganizationID, project.ID, req.ExpectedScriptUnitsRevision, req.Items)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	scriptUnitIDs := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		scriptUnitIDs = append(scriptUnitIDs, item.ScriptUnitID)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.script_unit.reordered", "project", project.ID, mustRawJSON(map[string]any{
			"commerceScriptUnitIds": scriptUnitIDs,
			"scriptUnitsRevision":   revision,
		})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"scriptUnitsRevision": revision}, nil)
}

func (s *Server) duplicateCommerceScriptUnit(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.createCommerceScriptUnitDerivation(w, r, principal, nil)
}

func (s *Server) createCommerceScriptLanguageVariant(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		ExpectedScriptUnitsRevision int64  `json:"expectedScriptUnitsRevision"`
		TargetLanguage              string `json:"targetLanguage"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.createCommerceScriptUnitDerivationDecoded(w, r, principal, req.ExpectedScriptUnitsRevision, &req.TargetLanguage)
}

func (s *Server) createCommerceScriptUnitDerivation(w http.ResponseWriter, r *http.Request, principal auth.Principal, language *string) {
	var req struct {
		ExpectedScriptUnitsRevision int64 `json:"expectedScriptUnitsRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.createCommerceScriptUnitDerivationDecoded(w, r, principal, req.ExpectedScriptUnitsRevision, language)
}

func (s *Server) createCommerceScriptUnitDerivationDecoded(w http.ResponseWriter, r *http.Request, principal auth.Principal, expectedRevision int64, language *string) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.DuplicateScriptUnit(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), principal.UserID, expectedRevision, language)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptMutationEvents(
		r.Context(), tx, project.OrganizationID, project.ID, item, "commerce.script_unit.created",
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) validateCommerceScriptContentForCurrentVideoModel(
	ctx context.Context,
	project Project,
	content string,
) error {
	options, err := s.commerceDirect.Options(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		// Script editing remains available while a project is still being
		// configured or its video route is temporarily unavailable.
		if _, ok := commercepkg.AsError(err); ok {
			return nil
		}
		return err
	}
	return commercepkg.ValidateDirectVideoScript(content, options.ScriptPromptConstraint)
}

func (s *Server) listCommerceScriptVersions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListScriptVersions(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceScriptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetScriptVersion(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"), r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createCommerceScriptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision   int64   `json:"expectedRevision"`
		Content            string  `json:"content"`
		SourceLanguageHint *string `json:"sourceLanguageHint"`
		Activate           bool    `json:"activate"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.validateCommerceScriptContentForCurrentVideoModel(r.Context(), project, req.Content); err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.CreateScriptVersion(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), principal.UserID, req.ExpectedRevision, req.Content, req.SourceLanguageHint, req.Activate)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	unitEventName := ""
	if item.Activated {
		unitEventName = "commerce.script_unit.updated"
	}
	if err := appendCommerceScriptMutationEvents(
		r.Context(), tx, project.OrganizationID, project.ID, item, unitEventName,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) activateCommerceScriptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
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
	item, err := s.commerceCatalog.ActivateScriptVersion(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("versionId"), req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	version, err := s.commerceCatalog.GetScriptVersion(
		r.Context(), tx, project.OrganizationID, project.ID, item.ID, r.PathValue("versionId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptUnitEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script_unit.updated", item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptVersionEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script.version.activated", version,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) resolveCommerceScriptLanguage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.ResolveLanguage(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceLanguageEvent(r.Context(), tx, project.OrganizationID, project.ID, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) getCommerceScriptLanguageResolution(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetLanguageResolution(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) confirmCommerceScriptLanguage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		LanguageResolutionID string `json:"languageResolutionId"`
		TargetLanguage       string `json:"targetLanguage"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.LanguageResolutionID = strings.TrimSpace(req.LanguageResolutionID)
	targetLocale, err := commercepkg.NormalizeLocale(req.TargetLanguage)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.LanguageResolutionID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "语言确认参数不完整", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	unitID := r.PathValue("scriptUnitId")
	resolution, err := s.commerceCatalog.GetLanguageResolution(
		r.Context(), tx, project.OrganizationID, project.ID, unitID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if resolution.ID != req.LanguageResolutionID {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeScriptVersionStale, Message: "语言判断所依据的脚本已变化，请重新判断"})
		return
	}
	run, preparationInput, found, err := loadActiveCommercePreparationLanguageRun(
		r.Context(), tx, project.OrganizationID, project.ID, unitID, resolution.SourceScriptVersionID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if found && resolution.Status == "needs_confirmation" {
		if s.temporal == nil {
			s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal 服务不可用", Retryable: true})
			return
		}
		signal := workflows.CommerceLanguageConfirmationSignal{
			Identity: preparationInput.Identity, ResolutionID: resolution.ID,
			ExpectedRevision: resolution.Revision, InputHash: resolution.InputHash,
			TargetLanguage: targetLocale,
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := s.temporal.SignalWorkflow(
			r.Context(), run.TemporalWorkflowID, "",
			workflows.CommerceLanguageConfirmationSignalName, signal,
		); err != nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "WORKFLOW_SIGNAL_FAILED", "语言确认暂未送达工作流，请重试", nil, true)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, map[string]any{
			"languageResolution": resolution, "workflowRun": run,
		}, nil)
		return
	}
	item, err := s.commerceCatalog.ConfirmLanguage(
		r.Context(), tx, project.OrganizationID, project.ID,
		unitID, resolution.ID, targetLocale, principal.UserID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceLanguageEvent(r.Context(), tx, project.OrganizationID, project.ID, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceScriptLocalizations(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListLocalizations(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceScriptLocalization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetLocalization(r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("localizationId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createCommerceScriptLocalization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req commercepkg.LocalizationInput
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, timing, err := s.commerceCatalog.CreateLocalization(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), principal.UserID, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceLocalizationEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script.localization.created", item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if item.Status == "approved" || item.ReviewStatus == "approved" {
		if err := appendCommerceLocalizationEvent(
			r.Context(), tx, project.OrganizationID, project.ID, "commerce.script.localization.approved", item,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, map[string]any{"timing": timing})
}

func (s *Server) activateCommerceScriptLocalization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
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
	item, err := s.commerceCatalog.ActivateLocalization(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("localizationId"), req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	localization, err := s.commerceCatalog.GetLocalization(
		r.Context(), tx, project.OrganizationID, project.ID,
		item.ID, r.PathValue("localizationId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceLocalizationEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script.localization.activated", localization,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptUnitEvent(
		r.Context(), tx, project.OrganizationID, project.ID, "commerce.script_unit.updated", item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}
