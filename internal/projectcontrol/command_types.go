package projectcontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CommandStatus string

const (
	CommandQueued           CommandStatus = "queued"
	CommandRunning          CommandStatus = "running"
	CommandWaitingWorkflow  CommandStatus = "waiting_workflow"
	CommandWaitingInput     CommandStatus = "waiting_input"
	CommandSucceeded        CommandStatus = "succeeded"
	CommandPartialSucceeded CommandStatus = "partial_succeeded"
	CommandFailed           CommandStatus = "failed"
	CommandCancelled        CommandStatus = "cancelled"
)

var (
	ErrCommandNotFound       = errors.New("project control command not found")
	ErrPromptNotFound        = errors.New("project control prompt not found")
	ErrPromptAlreadyResolved = errors.New("project control prompt was already resolved")
	ErrPromptExpired         = errors.New("project control prompt expired")
	ErrRevisionConflict      = errors.New("project control command revision conflict")
	ErrIdempotencyConflict   = errors.New("project control idempotency conflict")
	ErrRetryAlreadyActive    = errors.New("project control retry is already active")
	ErrRetryUnavailable      = errors.New("project control command has no retryable failure")
)

type Command struct {
	ID                            string             `json:"id"`
	OrganizationID                string             `json:"organizationId"`
	WorkspaceID                   string             `json:"workspaceId,omitempty"`
	ProjectID                     string             `json:"projectId,omitempty"`
	ActorUserID                   string             `json:"actorUserId"`
	ControllerType                ControllerType     `json:"controllerType"`
	ControlKeyID                  string             `json:"controlKeyId,omitempty"`
	AgentTaskID                   string             `json:"agentTaskId,omitempty"`
	AgentStepID                   string             `json:"agentStepId,omitempty"`
	ActionName                    string             `json:"actionName"`
	ActionLabel                   string             `json:"actionLabel,omitempty"`
	WorkflowRunIDs                []string           `json:"workflowRunIds,omitempty"`
	ActionVersion                 int                `json:"actionVersion"`
	ExecutionMode                 ExecutionMode      `json:"executionMode"`
	ActivityVisibility            ActivityVisibility `json:"activityVisibility"`
	Input                         json.RawMessage    `json:"input"`
	InputHash                     string             `json:"inputHash"`
	IdempotencyKey                string             `json:"idempotencyKey"`
	Status                        CommandStatus      `json:"status"`
	Output                        json.RawMessage    `json:"output"`
	ErrorCode                     string             `json:"errorCode,omitempty"`
	ErrorMessage                  string             `json:"errorMessage,omitempty"`
	ParentCommandID               string             `json:"parentCommandId,omitempty"`
	RetryOfCommandID              string             `json:"retryOfCommandId,omitempty"`
	CancellationRequestedAt       *time.Time         `json:"cancellationRequestedAt,omitempty"`
	CancellationRequestedByUserID string             `json:"cancellationRequestedByUserId,omitempty"`
	CancellationIdempotencyKey    string             `json:"-"`
	CancellationReason            string             `json:"cancellationReason,omitempty"`
	LeaseOwner                    string             `json:"leaseOwner,omitempty"`
	LeaseExpiresAt                *time.Time         `json:"leaseExpiresAt,omitempty"`
	NextReconcileAt               *time.Time         `json:"nextReconcileAt,omitempty"`
	WorkerReleaseID               string             `json:"workerReleaseId,omitempty"`
	CreatedAt                     time.Time          `json:"createdAt"`
	UpdatedAt                     time.Time          `json:"updatedAt"`
	StartedAt                     *time.Time         `json:"startedAt,omitempty"`
	CompletedAt                   *time.Time         `json:"completedAt,omitempty"`
	Revision                      int64              `json:"revision"`
}

func (c Command) Terminal() bool {
	return c.Status.Terminal()
}

func (s CommandStatus) Terminal() bool {
	switch s {
	case CommandSucceeded, CommandPartialSucceeded, CommandFailed, CommandCancelled:
		return true
	default:
		return false
	}
}

func (s CommandStatus) Valid() bool {
	switch s {
	case CommandQueued, CommandRunning, CommandWaitingWorkflow, CommandWaitingInput,
		CommandSucceeded, CommandPartialSucceeded, CommandFailed, CommandCancelled:
		return true
	default:
		return false
	}
}

type CreateCommand struct {
	OrganizationID   string
	WorkspaceID      string
	ProjectID        string
	ActorUserID      string
	ControllerType   ControllerType
	ControlKeyID     string
	AgentTaskID      string
	AgentStepID      string
	Descriptor       Descriptor
	Input            json.RawMessage
	IdempotencyKey   string
	ParentCommandID  string
	RetryOfCommandID string
	Items            []CreateCommandItem
}

type CreateCommandItem struct {
	ItemKey        string
	StableOrdinal  *int
	TargetType     string
	TargetID       string
	TargetRevision *int64
	Input          json.RawMessage
}

type CommandItem struct {
	ID             string          `json:"id"`
	CommandID      string          `json:"commandId"`
	ItemKey        string          `json:"itemKey"`
	StableOrdinal  *int            `json:"stableOrdinal,omitempty"`
	TargetType     string          `json:"targetType"`
	TargetID       string          `json:"targetId,omitempty"`
	TargetRevision *int64          `json:"targetRevision,omitempty"`
	Input          json.RawMessage `json:"input"`
	InputHash      string          `json:"inputHash"`
	Status         string          `json:"status"`
	Retryable      bool            `json:"retryable"`
	Output         json.RawMessage `json:"output"`
	ErrorCode      string          `json:"errorCode,omitempty"`
	ErrorMessage   string          `json:"errorMessage,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type CommandEvent struct {
	Sequence  int64           `json:"sequence"`
	CommandID string          `json:"commandId"`
	EventType string          `json:"eventType"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

type ListCommandsFilter struct {
	ActorUserID     string
	OrganizationID  string
	ProjectID       string
	ControllerType  ControllerType
	Statuses        []CommandStatus
	CreatedAfter    *time.Time
	BeforeCreatedAt *time.Time
	BeforeID        string
	ActivityView    bool
	Limit           int
}

type CommandCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type CommandPage struct {
	Commands   []Command      `json:"commands"`
	NextCursor *CommandCursor `json:"nextCursor,omitempty"`
}

type TransitionCommand struct {
	CommandID        string
	ExpectedRevision int64
	Status           CommandStatus
	Output           json.RawMessage
	ErrorCode        string
	ErrorMessage     string
	NextReconcileAt  *time.Time
	EventType        string
	EventPayload     map[string]any
}

type RequestCancellation struct {
	CommandID        string
	ExpectedRevision int64
	ActorUserID      string
	IdempotencyKey   string
	Reason           string
}

type RetryCommand struct {
	CommandID        string
	ExpectedRevision int64
	ActorUserID      string
	ControllerType   ControllerType
	ControlKeyID     string
	Descriptor       Descriptor
	IdempotencyKey   string
}

type WorkflowLink struct {
	ID                 string `json:"id,omitempty"`
	CommandItemID      string `json:"commandItemId,omitempty"`
	WorkflowRunID      string `json:"workflowRunId,omitempty"`
	TemporalWorkflowID string `json:"temporalWorkflowId"`
	TemporalRunID      string `json:"temporalRunId,omitempty"`
	RelationType       string `json:"relationType"`
}

const (
	// WorkflowRelationDeterministicChild is owned by the project-control
	// command and must use the workflow identity derived from that command.
	WorkflowRelationDeterministicChild = "deterministic_child"
	// WorkflowRelationDomainIdempotentChild reuses a workflow created through an
	// existing domain service. Its persisted workflow_run_id is the durable
	// identity; the domain service remains responsible for start idempotency.
	WorkflowRelationDomainIdempotentChild = "domain_idempotent_child"
)

type Claim struct {
	Command       Command `json:"command"`
	AttemptID     string  `json:"attemptId"`
	AttemptNumber int     `json:"attemptNumber"`
	AttemptKind   string  `json:"attemptKind"`
	LeaseIdentity string  `json:"leaseIdentity"`
	Reclaimed     bool    `json:"reclaimed"`
}

type ItemResult struct {
	CommandItemID string          `json:"commandItemId"`
	Status        string          `json:"status"`
	Retryable     bool            `json:"retryable"`
	Output        json.RawMessage `json:"output,omitempty"`
	ErrorCode     string          `json:"errorCode,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
}

type CommandPrompt struct {
	ID                      string          `json:"id"`
	CommandID               string          `json:"commandId"`
	PromptKind              string          `json:"promptKind"`
	Prompt                  string          `json:"prompt"`
	Options                 json.RawMessage `json:"options"`
	Status                  string          `json:"status"`
	ExpectedCommandRevision int64           `json:"expectedCommandRevision"`
	CandidateRevisions      json.RawMessage `json:"candidateRevisions"`
	ExpiresAt               time.Time       `json:"expiresAt"`
	Answer                  json.RawMessage `json:"answer,omitempty"`
	AnswerIdempotencyKey    string          `json:"answerIdempotencyKey,omitempty"`
	AnsweredByUserID        string          `json:"answeredByUserId,omitempty"`
	CreatedAt               time.Time       `json:"createdAt"`
	AnsweredAt              *time.Time      `json:"answeredAt,omitempty"`
}

type CreateCommandPrompt struct {
	CommandID          string
	ExpectedRevision   int64
	PromptKind         string
	Prompt             string
	Options            json.RawMessage
	CandidateRevisions json.RawMessage
	ExpiresAt          time.Time
}

type ResolveCommandPrompt struct {
	CommandID               string
	PromptID                string
	ActorUserID             string
	ExpectedCommandRevision int64
	IdempotencyKey          string
	Answer                  json.RawMessage
	ResumeStatus            CommandStatus
}

func (r CreateCommand) Validate() error {
	if strings.TrimSpace(r.OrganizationID) == "" || strings.TrimSpace(r.ActorUserID) == "" {
		return fmt.Errorf("organization and actor are required")
	}
	if err := r.Descriptor.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" || len(r.IdempotencyKey) > 200 {
		return fmt.Errorf("idempotency key must contain between 1 and 200 characters")
	}
	if r.ProjectID != "" && r.WorkspaceID == "" {
		return fmt.Errorf("project-scoped command requires workspace")
	}
	switch r.ControllerType {
	case ControllerEmbeddedAgent:
		if r.ControlKeyID != "" || r.AgentTaskID == "" || r.AgentStepID == "" {
			return fmt.Errorf("embedded agent command identity is invalid")
		}
	case ControllerCodexMCP:
		if r.ControlKeyID == "" || r.AgentTaskID != "" || r.AgentStepID != "" {
			return fmt.Errorf("Codex MCP command identity is invalid")
		}
	case ControllerManual:
		if r.ControlKeyID != "" || r.AgentTaskID != "" || r.AgentStepID != "" {
			return fmt.Errorf("manual command identity is invalid")
		}
	default:
		return fmt.Errorf("controller type %q is invalid", r.ControllerType)
	}
	seen := make(map[string]struct{}, len(r.Items))
	for _, item := range r.Items {
		key := strings.TrimSpace(item.ItemKey)
		if key == "" || strings.TrimSpace(item.TargetType) == "" {
			return fmt.Errorf("command item key and target type are required")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("command item key %q is duplicated", key)
		}
		seen[key] = struct{}{}
		if item.StableOrdinal != nil && *item.StableOrdinal < 1 {
			return fmt.Errorf("command item %q stable ordinal must be positive", key)
		}
		if item.TargetRevision != nil && *item.TargetRevision < 1 {
			return fmt.Errorf("command item %q target revision must be positive", key)
		}
	}
	return nil
}
