package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
)

const agentAskUserToolName = "agent.ask_user"

func isAgentAskUserTool(toolName string) bool {
	return strings.TrimSpace(toolName) == agentAskUserToolName
}

func forceAgentQuestionDecision(decision agent.SupervisionDecision) agent.SupervisionDecision {
	decision.Allowed = true
	decision.ExecutionAllowed = false
	decision.RequiresApproval = true
	if decision.Risk == "" {
		decision.Risk = agent.ToolRiskDraft
	}
	decision.Reasons = appendUniqueAgentReasons(agentReasonsWithout(decision.Reasons, "approval_required"), "approval_required", "user_question")
	return decision
}

func appendUniqueAgentReasons(items []string, values ...string) []string {
	out := append([]string{}, items...)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || agentReasonsContain(out, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func agentApprovalTypeForStep(toolName string, risk agent.ToolRisk) string {
	if isAgentAskUserTool(toolName) {
		return "question"
	}
	return string(risk)
}

func agentApprovalRequestedPayload(toolName string, risk agent.ToolRisk, permission string, rawArgs json.RawMessage, expectedResult string, decision agent.SupervisionDecision, permissionMode agent.PermissionMode, dryRunOutput map[string]any, extra map[string]any) map[string]any {
	if len(rawArgs) == 0 {
		rawArgs = json.RawMessage(`{}`)
	}
	payload := map[string]any{
		"tool":           toolName,
		"risk":           risk,
		"permission":     permission,
		"args":           json.RawMessage(rawArgs),
		"expectedResult": expectedResult,
		"decision":       decision,
		"permissionMode": permissionMode,
		"dryRunOutput":   dryRunOutput,
	}
	for key, value := range extra {
		payload[key] = value
	}
	if isAgentAskUserTool(toolName) {
		args, _ := agentStepArgs(rawArgs)
		payload["approvalType"] = "question"
		payload["interactionType"] = "question"
		payload["question"] = firstNonEmpty(agentStringArg(args, "question"), expectedResult, "请选择下一步。")
		payload["options"] = agentQuestionOptions(args["options"])
		payload["allowCustom"] = boolValueFromAny(args["allowCustom"])
		payload["defaultOptionId"] = agentStringArg(args, "defaultOptionId")
	}
	return payload
}

func (s *Server) agentToolAskUser(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	question := firstNonEmpty(agentStringArg(args, "question"), "请选择下一步。")
	decision := agentQuestionDecisionFromStep(step)
	options := agentQuestionOptions(args["options"])
	selectedID := firstNonEmpty(
		stringValueFromAny(decision["selectedOptionId"]),
		stringValueFromAny(decision["optionId"]),
		stringValueFromAny(decision["id"]),
	)
	selected := map[string]any{}
	for _, option := range options {
		if stringValueFromAny(option["id"]) == selectedID {
			selected = option
			break
		}
	}
	label := firstNonEmpty(
		stringValueFromAny(decision["selectedOptionLabel"]),
		stringValueFromAny(decision["label"]),
		stringValueFromAny(selected["label"]),
	)
	custom := firstNonEmpty(
		stringValueFromAny(decision["customAnswer"]),
		stringValueFromAny(decision["answer"]),
		stringValueFromAny(decision["note"]),
	)
	answerText := firstNonEmpty(custom, label, selectedID, "已确认")
	nextGoal := firstNonEmpty(
		stringValueFromAny(decision["nextGoal"]),
		stringValueFromAny(selected["nextGoal"]),
	)
	data := map[string]any{
		"question": question,
		"answer":   answerText,
		"decision": decision,
	}
	if selectedID != "" {
		data["selectedOptionId"] = selectedID
	}
	if label != "" {
		data["selectedOptionLabel"] = label
	}
	if custom != "" {
		data["customAnswer"] = custom
	}
	if nextGoal != "" {
		data["nextGoal"] = nextGoal
	}
	result := agentToolOK(agentAskUserToolName, args, "已记录你的选择："+answerText+"。", data)
	result.Label = "询问用户"
	if nextGoal != "" {
		result.NextActions = []agentToolNextAction{{
			Label:     "按选择继续：" + nextGoal,
			Reason:    "用户选择指定了下一步目标",
			Tool:      agentAskUserToolName,
			Arguments: map[string]any{"nextGoal": nextGoal},
		}}
	}
	return result
}

func agentQuestionDecisionFromStep(step AgentStep) map[string]any {
	supervisor := rawObject(step.SupervisorDecision)
	approval := agentObjectFromAny(supervisor["approval"])
	payload := agentObjectFromAny(approval["decisionPayload"])
	if nested := agentObjectFromAny(payload["decision"]); len(nested) > 0 {
		if note := stringValueFromAny(payload["note"]); note != "" {
			nested["note"] = note
		}
		return nested
	}
	return payload
}

func agentQuestionOptions(value any) []map[string]any {
	rawItems := []any{}
	switch typed := value.(type) {
	case []any:
		rawItems = typed
	case []map[string]any:
		for _, item := range typed {
			rawItems = append(rawItems, item)
		}
	case json.RawMessage:
		_ = json.Unmarshal(typed, &rawItems)
	default:
		if value != nil {
			encoded, err := json.Marshal(value)
			if err == nil {
				_ = json.Unmarshal(encoded, &rawItems)
			}
		}
	}
	options := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		option := agentObjectFromAny(raw)
		id := stringValueFromAny(option["id"])
		label := stringValueFromAny(option["label"])
		if id == "" || label == "" {
			continue
		}
		clean := map[string]any{
			"id":    id,
			"label": label,
		}
		if description := stringValueFromAny(option["description"]); description != "" {
			clean["description"] = description
		}
		if nextGoal := stringValueFromAny(option["nextGoal"]); nextGoal != "" {
			clean["nextGoal"] = nextGoal
		}
		if value, ok := option["value"]; ok {
			clean["value"] = value
		}
		options = append(options, clean)
	}
	return options
}

func agentObjectFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case json.RawMessage:
		out := map[string]any{}
		_ = json.Unmarshal(typed, &out)
		return out
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return map[string]any{}
		}
		out := map[string]any{}
		_ = json.Unmarshal(encoded, &out)
		return out
	}
}
