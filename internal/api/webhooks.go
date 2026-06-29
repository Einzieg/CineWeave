package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) providerWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	accountID := strings.TrimSpace(r.PathValue("providerAccountId"))
	secret := strings.TrimSpace(r.PathValue("webhookSecret"))
	if accountID == "" || secret == "" {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "webhook target was not found", nil, false)
		return
	}

	raw, err := readWebhookBody(w, r)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "webhook payload is invalid", err.Error(), false)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "webhook payload is invalid JSON", err.Error(), false)
		return
	}
	externalTaskID := webhookString(payload, "externalTaskId", "taskId", "id")
	if externalTaskID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "externalTaskId is required", nil, false)
		return
	}

	var organizationID string
	var config json.RawMessage
	if err := s.db.QueryRow(r.Context(), `
		SELECT organization_id, config
		FROM provider_accounts
		WHERE id = $1 AND status <> 'disabled'
	`, accountID).Scan(&organizationID, &config); err != nil {
		s.writeError(w, r, err)
		return
	}
	expectedSecret := providerWebhookSecret(config)
	if expectedSecret == "" || expectedSecret != secret {
		httpx.WriteError(w, r, http.StatusUnauthorized, "WEBHOOK_UNAUTHORIZED", "webhook secret is invalid", nil, false)
		return
	}

	result, err := s.recordProviderWebhook(r, accountID, externalTaskID, raw, payload)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result, nil)
}

type providerWebhookResult struct {
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ProviderCallID      string `json:"providerCallId"`
	Status              string `json:"status"`
	SignalStatus        string `json:"signalStatus"`
}

func (s *Server) recordProviderWebhook(r *http.Request, accountID, externalTaskID string, raw json.RawMessage, payload map[string]any) (providerWebhookResult, error) {
	var taskID, organizationID, providerCallID string
	var projectID, workflowRunID, temporalWorkflowID *string
	err := s.db.QueryRow(r.Context(), `
		SELECT t.id, t.organization_id, t.provider_call_id, l.project_id, l.workflow_run_id, w.temporal_workflow_id
		FROM provider_async_tasks t
		JOIN provider_call_logs l ON l.id = t.provider_call_id
		LEFT JOIN workflow_runs w ON w.id = l.workflow_run_id
		WHERE t.provider_account_id = $1 AND t.external_task_id = $2
	`, accountID, externalTaskID).Scan(&taskID, &organizationID, &providerCallID, &projectID, &workflowRunID, &temporalWorkflowID)
	if err != nil {
		return providerWebhookResult{}, err
	}

	status := normalizeWebhookStatus(webhookString(payload, "status", "state"))
	terminal := status == "succeeded" || status == "failed" || status == "cancelled"
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return providerWebhookResult{}, err
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `
		UPDATE provider_async_tasks
		SET status = $2,
			raw_status = $3,
			finalized_at = CASE WHEN $4 THEN now() ELSE finalized_at END,
			updated_at = now()
		WHERE id = $1
	`, taskID, status, raw, terminal); err != nil {
		return providerWebhookResult{}, err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE provider_call_logs
		SET status = $2,
			response_snapshot = $3,
			normalized_output = $4,
			completed_at = CASE WHEN $5 THEN now() ELSE completed_at END
		WHERE id = $1
	`, providerCallID, status, raw, raw, terminal); err != nil {
		return providerWebhookResult{}, err
	}
	eventPayload := mustMarshal(map[string]any{
		"providerAsyncTaskId": taskID,
		"providerCallId":      providerCallID,
		"externalTaskId":      externalTaskID,
		"status":              status,
		"payload":             payload,
	})
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, 'provider.webhook.received', 'provider_async_task', $3, $4)
	`, organizationID, projectID, taskID, eventPayload); err != nil {
		return providerWebhookResult{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return providerWebhookResult{}, err
	}

	signalStatus := "not_applicable"
	if workflowRunID != nil && temporalWorkflowID != nil && *temporalWorkflowID != "" {
		signalStatus = "sent"
		if err := s.temporal.SignalWorkflow(r.Context(), *temporalWorkflowID, "", "provider-webhook", map[string]any{
			"providerAsyncTaskId": taskID,
			"providerCallId":      providerCallID,
			"externalTaskId":      externalTaskID,
			"status":              status,
			"payload":             payload,
		}); err != nil {
			signalStatus = "failed"
		}
	}
	return providerWebhookResult{
		ProviderAsyncTaskID: taskID,
		ProviderCallID:      providerCallID,
		Status:              status,
		SignalStatus:        signalStatus,
	}, nil
}

func readWebhookBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, error) {
	defer r.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return raw, nil
}

func providerWebhookSecret(raw json.RawMessage) string {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return ""
	}
	return webhookString(config, "webhookSecret", "webhook_secret")
}

func webhookString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeWebhookStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "success", "completed", "complete", "done":
		return "succeeded"
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "running"
	}
}
