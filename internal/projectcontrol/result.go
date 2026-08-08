package projectcontrol

import "encoding/json"

type Error struct {
	Code        string          `json:"code"`
	UserMessage string          `json:"userMessage"`
	Retryable   bool            `json:"retryable"`
	Details     json.RawMessage `json:"details,omitempty"`
}

type NextAction struct {
	Label     string          `json:"label"`
	Reason    string          `json:"reason,omitempty"`
	Action    string          `json:"action,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type Result struct {
	SchemaVersion  string          `json:"schemaVersion"`
	CommandID      string          `json:"commandId,omitempty"`
	Status         string          `json:"status"`
	Summary        string          `json:"summary"`
	Data           json.RawMessage `json:"data,omitempty"`
	WorkflowRunIDs []string        `json:"workflowRunIds"`
	NextCursor     string          `json:"nextCursor,omitempty"`
	Retryable      bool            `json:"retryable"`
	Error          *Error          `json:"error,omitempty"`
	NextActions    []NextAction    `json:"nextActions"`
}

func NewResult(status, summary string) Result {
	return Result{
		SchemaVersion:  SchemaVersionV1,
		Status:         status,
		Summary:        summary,
		WorkflowRunIDs: []string{},
		NextActions:    []NextAction{},
	}
}
