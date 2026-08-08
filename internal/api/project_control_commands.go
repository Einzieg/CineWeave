package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func (s *Server) executeManualAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	actionName string,
	actionInput any,
	idempotencyKey string,
) (projectcontrol.Result, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return projectcontrol.Result{}, newAPIError(
			http.StatusUnprocessableEntity,
			"IDEMPOTENCY_KEY_REQUIRED",
			"该写操作需要 Idempotency-Key",
		)
	}
	raw, err := json.Marshal(actionInput)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return projectcontrol.Result{}, err
	}
	envelope["projectId"] = project.ID
	envelope["idempotencyKey"] = idempotencyKey
	request, err := json.Marshal(envelope)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result, err := s.projectControl.Execute(ctx, controlmcp.Identity{
		Principal: principal, ControllerType: projectcontrol.ControllerManual,
	}, actionName, request)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if result.Error != nil {
		return projectcontrol.Result{}, apiError{
			Status: projectControlRESTStatus(result), Code: result.Error.Code,
			Message: result.Error.UserMessage, Retryable: result.Error.Retryable,
			Details: result.Error.Details,
		}
	}
	return result, nil
}

func (s *Server) writeManualAsyncActionResult(
	w http.ResponseWriter,
	r *http.Request,
	result projectcontrol.Result,
) {
	if strings.TrimSpace(result.CommandID) != "" {
		w.Header().Set("X-CineWeave-Command-ID", result.CommandID)
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result, nil)
}

func (s *Server) listProjectControlCommands(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	statuses := splitControlQueryValues(r.URL.Query()["filter[status]"])
	input := controlCommandListInput{
		ProjectID:      strings.TrimSpace(r.URL.Query().Get("filter[projectId]")),
		Statuses:       statuses,
		ControllerType: strings.TrimSpace(r.URL.Query().Get("filter[controllerType]")),
		CreatedAfter:   strings.TrimSpace(r.URL.Query().Get("filter[createdAfter]")),
		View:           strings.TrimSpace(r.URL.Query().Get("view")),
		Limit:          queryInt(r, "limit", projectControlDefaultPageSize),
		Cursor:         strings.TrimSpace(r.URL.Query().Get("cursor")),
	}
	s.executeProjectControlREST(w, r, principal, "control.command.list", input)
}

func (s *Server) getProjectControlCommand(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.executeProjectControlREST(w, r, principal, "control.command.get", controlCommandInput{
		CommandID: r.PathValue("commandId"),
	})
}

func (s *Server) listProjectControlCommandEvents(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.executeProjectControlREST(w, r, principal, "control.command.events", controlCommandEventsInput{
		CommandID:   r.PathValue("commandId"),
		AfterCursor: strings.TrimSpace(r.URL.Query().Get("afterCursor")),
		Limit:       queryInt(r, "limit", 100),
	})
}

func (s *Server) waitProjectControlCommand(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var input controlCommandWaitInput
	if !decode(w, r, &input) {
		return
	}
	input.CommandID = r.PathValue("commandId")
	s.executeProjectControlREST(w, r, principal, "control.command.wait", input)
}

func (s *Server) cancelProjectControlCommand(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var input controlCommandCancelInput
	if !decode(w, r, &input) {
		return
	}
	input.CommandID = r.PathValue("commandId")
	s.executeProjectControlREST(w, r, principal, "control.command.cancel", input)
}

func (s *Server) retryProjectControlCommand(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var input controlCommandRetryInput
	if !decode(w, r, &input) {
		return
	}
	input.CommandID = r.PathValue("commandId")
	s.executeProjectControlREST(w, r, principal, "control.command.retry", input)
}

func (s *Server) resolveProjectControlCommand(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var input controlCommandResolveInput
	if !decode(w, r, &input) {
		return
	}
	input.CommandID = r.PathValue("commandId")
	s.executeProjectControlREST(w, r, principal, "control.command.resolve", input)
}

func (s *Server) executeProjectControlREST(w http.ResponseWriter, r *http.Request, principal auth.Principal, action string, input any) {
	raw, err := json.Marshal(input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.projectControl.Execute(r.Context(), controlmcp.Identity{
		Principal: principal, ControllerType: projectcontrol.ControllerManual,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	}, action, raw)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	status := projectControlRESTStatus(result)
	httpx.WriteJSON(w, r, status, result, nil)
}

func projectControlRESTStatus(result projectcontrol.Result) int {
	if result.Error == nil {
		return http.StatusOK
	}
	switch result.Error.Code {
	case "PERMISSION_DENIED":
		return http.StatusForbidden
	case "NOT_FOUND":
		return http.StatusNotFound
	case "REVISION_CONFLICT", "IDEMPOTENCY_CONFLICT", "RETRY_ALREADY_ACTIVE",
		"PROMPT_ALREADY_RESOLVED", "ACTION_CONTRACT_UNAVAILABLE":
		return http.StatusConflict
	case "VALIDATION_FAILED", "RETRY_UNAVAILABLE", "PROMPT_EXPIRED":
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func splitControlQueryValues(values []string) []string {
	items := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, exists := seen[part]; exists {
				continue
			}
			seen[part] = struct{}{}
			items = append(items, part)
		}
	}
	return items
}
