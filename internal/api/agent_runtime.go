package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
)

const (
	agentRuntimeMaxActions        = 24
	agentRuntimeMaxInvalidPlans   = 3
	agentRuntimeMaxRepeatedAction = 2
	agentRuntimeObservationLimit  = 12
)

type agentRuntimeObservation struct {
	StepIndex           int                 `json:"stepIndex"`
	Tool                string              `json:"tool"`
	Status              string              `json:"status"`
	Summary             string              `json:"summary,omitempty"`
	ErrorCode           string              `json:"errorCode,omitempty"`
	ErrorMessage        string              `json:"errorMessage,omitempty"`
	EntityReferences    map[string][]string `json:"entityReferences,omitempty"`
	ChildWorkflowRunIDs []string            `json:"childWorkflowRunIds,omitempty"`
	Data                any                 `json:"data,omitempty"`
	OutputHash          string              `json:"outputHash"`
}

type agentRuntimeSnapshot struct {
	ActionCount        int                         `json:"actionCount"`
	MaxActions         int                         `json:"maxActions"`
	InvalidPlanCount   int                         `json:"invalidPlanCount"`
	MaxInvalidPlans    int                         `json:"maxInvalidPlans"`
	Observations       []agentRuntimeObservation   `json:"observations"`
	EntityReferences   map[string][]string         `json:"entityReferences"`
	CompletedWorkflows []agentCompletedWorkflowRun `json:"completedWorkflows,omitempty"`
	ObservationHash    string                      `json:"observationHash"`
}

func (s *Server) loadAgentRuntimeSnapshot(ctx context.Context, projectID, taskID string) (agentRuntimeSnapshot, error) {
	rows, err := s.db.Query(ctx, `
		SELECT step_index, tool_name, status, output
		FROM agent_steps
		WHERE task_id = $1
		ORDER BY step_index ASC
	`, taskID)
	if err != nil {
		return agentRuntimeSnapshot{}, err
	}
	defer rows.Close()

	observations := make([]agentRuntimeObservation, 0)
	entities := map[string][]string{}
	actionCount := 0
	for rows.Next() {
		var stepIndex int
		var toolName, status string
		var raw json.RawMessage
		if err := rows.Scan(&stepIndex, &toolName, &status, &raw); err != nil {
			return agentRuntimeSnapshot{}, err
		}
		if status == "succeeded" || status == "failed" || status == "blocked" || status == "cancelled" {
			actionCount++
		}
		if len(raw) == 0 || string(raw) == "{}" {
			continue
		}
		var result agentToolResult
		if err := json.Unmarshal(raw, &result); err != nil {
			continue
		}
		refs := extractAgentEntityReferences(map[string]any{
			"arguments": result.Arguments,
			"data":      result.Data,
		})
		mergeAgentEntityReferences(entities, refs)
		observation := agentRuntimeObservation{
			StepIndex:           stepIndex,
			Tool:                firstNonEmpty(result.Name, toolName),
			Status:              firstNonEmpty(result.Status, status),
			Summary:             strings.TrimSpace(result.Summary),
			ErrorCode:           strings.TrimSpace(result.ErrorCode),
			ErrorMessage:        strings.TrimSpace(result.ErrorMessage),
			EntityReferences:    refs,
			ChildWorkflowRunIDs: append([]string(nil), result.ChildWorkflowRunIDs...),
			Data:                compactAgentObservationValue(result.Data, 0),
			OutputHash:          promptsvc.HashText(string(raw)),
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return agentRuntimeSnapshot{}, err
	}
	if len(observations) > agentRuntimeObservationLimit {
		observations = observations[len(observations)-agentRuntimeObservationLimit:]
	}
	completedWorkflows, err := s.agentTaskCompletedWorkflowRuns(ctx, projectID, taskID)
	if err != nil {
		return agentRuntimeSnapshot{}, err
	}
	var taskSummary json.RawMessage
	if err := s.db.QueryRow(ctx, `
		SELECT summary
		FROM agent_tasks
		WHERE id = $1 AND project_id = $2
	`, taskID, projectID).Scan(&taskSummary); err != nil {
		return agentRuntimeSnapshot{}, err
	}
	invalidPlanCount := int(float64Value(rawObject(taskSummary)["runtimeInvalidPlanCount"]))
	snapshot := agentRuntimeSnapshot{
		ActionCount:        actionCount,
		MaxActions:         agentRuntimeMaxActions,
		InvalidPlanCount:   invalidPlanCount,
		MaxInvalidPlans:    agentRuntimeMaxInvalidPlans,
		Observations:       observations,
		EntityReferences:   entities,
		CompletedWorkflows: completedWorkflows,
	}
	var latestObservation any
	if len(snapshot.Observations) > 0 {
		latest := snapshot.Observations[len(snapshot.Observations)-1]
		latestObservation = map[string]any{
			"tool":                latest.Tool,
			"status":              latest.Status,
			"outputHash":          latest.OutputHash,
			"entityReferences":    latest.EntityReferences,
			"childWorkflowRunIds": latest.ChildWorkflowRunIDs,
			"data":                latest.Data,
		}
	}
	hashPayload := map[string]any{
		"latestObservation":  latestObservation,
		"entityReferences":   snapshot.EntityReferences,
		"completedWorkflows": snapshot.CompletedWorkflows,
	}
	snapshot.ObservationHash = promptsvc.HashText(string(mustMarshal(hashPayload)))
	return snapshot, nil
}

func extractAgentEntityReferences(value any) map[string][]string {
	result := map[string][]string{}
	var visit func(any, string, int)
	visit = func(current any, key string, depth int) {
		if depth > 6 || current == nil {
			return
		}
		switch typed := current.(type) {
		case map[string]any:
			for childKey, childValue := range typed {
				visit(childValue, childKey, depth+1)
			}
		case []any:
			for _, child := range typed {
				visit(child, key, depth+1)
			}
		case []string:
			if agentEntityReferenceKey(key) {
				for _, item := range typed {
					appendAgentEntityReference(result, key, item)
				}
			}
		case string:
			if agentEntityReferenceKey(key) {
				appendAgentEntityReference(result, key, typed)
			}
		}
	}
	visit(value, "", 0)
	return result
}

func agentEntityReferenceKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.EqualFold(key, "id") ||
		strings.HasSuffix(key, "Id") ||
		strings.HasSuffix(key, "ID") ||
		strings.HasSuffix(key, "Ids") ||
		strings.HasSuffix(key, "IDs")
}

func appendAgentEntityReference(target map[string][]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return
	}
	values := target[key]
	for _, existing := range values {
		if existing == value {
			return
		}
	}
	target[key] = append(values, value)
	sort.Strings(target[key])
}

func mergeAgentEntityReferences(target, source map[string][]string) {
	for key, values := range source {
		for _, value := range values {
			appendAgentEntityReference(target, key, value)
		}
	}
}

func compactAgentObservationValue(value any, depth int) any {
	if depth > 4 || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 30 {
			keys = keys[:30]
		}
		for _, key := range keys {
			if compact := compactAgentObservationValue(typed[key], depth+1); compact != nil {
				out[key] = compact
			}
		}
		return out
	case []any:
		if len(typed) > 20 {
			typed = typed[:20]
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, compactAgentObservationValue(item, depth+1))
		}
		return out
	case string:
		if len([]rune(typed)) > 1000 {
			return string([]rune(typed)[:1000]) + "..."
		}
		return typed
	case bool, float64, int, int64, json.Number:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func agentRuntimeMaxPlanSteps(task AgentTask) int {
	if task.Mode == "plan_only" {
		return agentRuntimeMaxActions
	}
	return 1
}

func (s *Server) persistAgentRuntimeSnapshot(ctx context.Context, project Project, taskID string) (agentRuntimeSnapshot, error) {
	snapshot, err := s.loadAgentRuntimeSnapshot(ctx, project.ID, taskID)
	if err != nil {
		return agentRuntimeSnapshot{}, err
	}
	if err := s.mergeAgentTaskSummaryPatch(ctx, project.ID, taskID, map[string]any{
		"runtimeActionCount":        snapshot.ActionCount,
		"runtimeObservationHash":    snapshot.ObservationHash,
		"runtimeObservations":       snapshot.Observations,
		"runtimeEntityReferences":   snapshot.EntityReferences,
		"runtimeCompletedWorkflows": snapshot.CompletedWorkflows,
	}); err != nil {
		return agentRuntimeSnapshot{}, err
	}
	return snapshot, nil
}

func (s *Server) appendAgentRuntimeNextAction(
	r *http.Request,
	principal auth.Principal,
	project Project,
	taskID string,
) (appended bool, complete bool, stopped *AgentTask, err error) {
	task, err := s.agentTask(r, project.ID, taskID)
	if err != nil {
		return false, false, nil, err
	}
	snapshot, err := s.persistAgentRuntimeSnapshot(r.Context(), project, taskID)
	if err != nil {
		return false, false, nil, err
	}
	if snapshot.ActionCount >= agentRuntimeMaxActions {
		message := "助手已达到 24 个行动的上限，任务已停止以避免无限循环。"
		blocked, blockErr := s.finishAgentTaskState(r.Context(), project.ID, taskID, "failed", "AGENT_RUNTIME_ACTION_LIMIT", message)
		return false, false, &blocked, blockErr
	}
	if boolValue(rawObject(task.Summary)["runtimePlannerComplete"]) {
		return false, true, nil, nil
	}
	var stepCountBefore int
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM agent_steps WHERE task_id = $1`, taskID).Scan(&stepCountBefore); err != nil {
		return false, false, nil, err
	}
	planned, err := s.planAgentTask(r, principal, project, task)
	if err != nil {
		return false, false, nil, err
	}
	if planned.Status == "blocked" || planned.Status == "failed" {
		return false, false, &planned, nil
	}
	var stepCountAfter int
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM agent_steps WHERE task_id = $1`, taskID).Scan(&stepCountAfter); err != nil {
		return false, false, nil, err
	}
	if stepCountAfter > stepCountBefore {
		return true, false, nil, nil
	}
	if boolValue(rawObject(planned.Summary)["runtimePlannerComplete"]) {
		return false, true, nil, nil
	}
	message := "助手规划器没有返回下一步动作，也没有声明任务完成。"
	failed, failErr := s.finishAgentTaskState(r.Context(), project.ID, taskID, "failed", "AGENT_RUNTIME_INVALID_PLAN_LIMIT", message)
	return false, false, &failed, failErr
}
