package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Plan struct {
	Summary string     `json:"summary"`
	Steps   []PlanStep `json:"steps"`
}

type PlanStep struct {
	Tool             string          `json:"tool"`
	Args             json.RawMessage `json:"args"`
	Risk             ToolRisk        `json:"risk,omitempty"`
	RequiresApproval bool            `json:"requiresApproval,omitempty"`
	ExpectedResult   string          `json:"expectedResult,omitempty"`
}

type rawToolCall struct {
	Name      string          `json:"name"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Args      json.RawMessage `json:"args"`
}

func ParsePlan(raw string) (Plan, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Plan{}, fmt.Errorf("agent plan is empty")
	}
	text = stripJSONFence(text)
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		if extracted, ok := extractJSONObject(text); ok {
			text = extracted
		}
	}
	if strings.HasPrefix(text, "[") {
		var steps []PlanStep
		if err := json.Unmarshal([]byte(text), &steps); err != nil {
			return Plan{}, err
		}
		return Plan{Steps: normalizePlanSteps(steps)}, nil
	}
	var payload struct {
		Summary        string        `json:"summary"`
		Message        string        `json:"message"`
		Steps          []PlanStep    `json:"steps"`
		ToolCalls      []rawToolCall `json:"toolCalls"`
		ToolCallsSnake []rawToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return Plan{}, err
	}
	steps := payload.Steps
	if len(steps) == 0 {
		steps = appendToolCalls(steps, payload.ToolCalls)
	}
	if len(steps) == 0 {
		steps = appendToolCalls(steps, payload.ToolCallsSnake)
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = strings.TrimSpace(payload.Message)
	}
	return Plan{Summary: summary, Steps: normalizePlanSteps(steps)}, nil
}

func ValidatePlan(plan Plan, registry *Registry, maxSteps int) (Plan, error) {
	if maxSteps <= 0 {
		maxSteps = 20
	}
	steps := normalizePlanSteps(plan.Steps)
	if len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	for index, step := range steps {
		tool, ok := registry.Get(step.Tool)
		if !ok {
			return Plan{}, fmt.Errorf("unknown agent tool %q", step.Tool)
		}
		if step.Risk == "" {
			steps[index].Risk = tool.Risk
		}
		if !step.RequiresApproval {
			steps[index].RequiresApproval = tool.RequiresApproval
		}
		if len(step.Args) == 0 {
			steps[index].Args = json.RawMessage(`{}`)
		}
		if err := validateToolArgs(tool, steps[index].Args); err != nil {
			return Plan{}, fmt.Errorf("invalid args for %s: %w", step.Tool, err)
		}
	}
	plan.Steps = steps
	return plan, nil
}

func validateToolArgs(tool AgentTool, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("args must be a JSON object")
	}
	if args == nil {
		args = map[string]any{}
	}
	var schema map[string]any
	if len(tool.InputSchema) == 0 {
		return nil
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		return fmt.Errorf("tool input schema is invalid")
	}
	return validateObjectAgainstSchema(args, schema, "args", true)
}

func validateObjectAgainstSchema(args map[string]any, schema map[string]any, path string, strictAdditional bool) error {
	properties := schemaProperties(schema)
	for _, key := range schemaRequired(schema) {
		if _, exists := args[key]; !exists {
			return fmt.Errorf("%s.%s is required", path, key)
		}
	}
	if strictAdditional && schemaAdditionalProperties(schema) == false {
		for key := range args {
			if _, exists := properties[key]; !exists {
				return fmt.Errorf("%s.%s is not allowed", path, key)
			}
		}
	}
	for key, value := range args {
		property, exists := properties[key]
		if !exists {
			continue
		}
		if err := validateValueAgainstSchema(value, property, path+"."+key); err != nil {
			return err
		}
	}
	return nil
}

func validateValueAgainstSchema(value any, schema map[string]any, path string) error {
	if value == nil {
		return nil
	}
	valueType, _ := schema["type"].(string)
	switch valueType {
	case "", "any":
		return nil
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if values := schemaEnum(schema); len(values) > 0 && !stringInSet(text, values) {
			return fmt.Errorf("%s must be one of %s", path, strings.Join(values, ", "))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		number, ok := jsonNumberValue(value)
		if !ok || number != float64(int(number)) {
			return fmt.Errorf("%s must be an integer", path)
		}
		if min, ok := jsonNumberValue(schema["minimum"]); ok && number < min {
			return fmt.Errorf("%s must be >= %.0f", path, min)
		}
		if max, ok := jsonNumberValue(schema["maximum"]); ok && number > max {
			return fmt.Errorf("%s must be <= %.0f", path, max)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		childProperties := schemaProperties(schema)
		if len(childProperties) > 0 || len(schemaRequired(schema)) > 0 {
			return validateObjectAgainstSchema(object, schema, path, true)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema := schemaMap(schema["items"])
		if len(itemSchema) > 0 {
			for index, item := range items {
				if err := validateValueAgainstSchema(item, itemSchema, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	default:
		return nil
	}
	return nil
}

func schemaProperties(schema map[string]any) map[string]map[string]any {
	raw, _ := schema["properties"].(map[string]any)
	out := make(map[string]map[string]any, len(raw))
	for key, value := range raw {
		if typed := schemaMap(value); len(typed) > 0 {
			out[key] = typed
		}
	}
	return out
}

func schemaRequired(schema map[string]any) []string {
	raw, _ := schema["required"].([]any)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func schemaAdditionalProperties(schema map[string]any) bool {
	value, exists := schema["additionalProperties"]
	if !exists {
		return true
	}
	allowed, ok := value.(bool)
	return ok && allowed
}

func schemaEnum(schema map[string]any) []string {
	raw, _ := schema["enum"].([]any)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func schemaMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func jsonNumberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringInSet(value string, allowed []string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func appendToolCalls(steps []PlanStep, calls []rawToolCall) []PlanStep {
	for _, call := range calls {
		tool := strings.TrimSpace(call.Tool)
		if tool == "" {
			tool = strings.TrimSpace(call.Name)
		}
		args := call.Args
		if len(args) == 0 {
			args = call.Arguments
		}
		steps = append(steps, PlanStep{Tool: tool, Args: args})
	}
	return steps
}

func normalizePlanSteps(steps []PlanStep) []PlanStep {
	out := make([]PlanStep, 0, len(steps))
	for _, step := range steps {
		step.Tool = strings.TrimSpace(step.Tool)
		if step.Tool == "" {
			continue
		}
		if len(step.Args) == 0 {
			step.Args = json.RawMessage(`{}`)
		}
		step.ExpectedResult = strings.TrimSpace(step.ExpectedResult)
		out = append(out, step)
	}
	return out
}

func stripJSONFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```JSON")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func extractJSONObject(text string) (string, bool) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", false
	}
	return strings.TrimSpace(text[start : end+1]), true
}
