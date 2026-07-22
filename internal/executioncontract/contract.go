package executioncontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const SchemaVersionV2 = 2

type Status string

const (
	StatusQueued           Status = "queued"
	StatusRunning          Status = "running"
	StatusCancelling       Status = "cancelling"
	StatusSucceeded        Status = "succeeded"
	StatusPartialSucceeded Status = "partial_succeeded"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
	StatusDiscarded        Status = "discarded"
)

type BaseIdentity struct {
	SchemaVersion      int    `json:"schemaVersion"`
	OrganizationID     string `json:"organizationId"`
	ProjectID          string `json:"projectId"`
	WorkflowRunID      string `json:"workflowRunId"`
	TemporalWorkflowID string `json:"temporalWorkflowId,omitempty"`
	OperationID        string `json:"operationId"`
	OperationItemID    string `json:"operationItemId"`
	Attempt            int    `json:"attempt"`
}

type VideoIdentityV2 struct {
	BaseIdentity
	ProductionGenerationID         string `json:"productionGenerationId"`
	VideoProductionBindingID       string `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64  `json:"videoProductionBindingRevision"`
	StoryboardShotID               string `json:"storyboardShotId"`
	ConfigurationSnapshotHash      string `json:"configurationSnapshotHash"`
	ExecutionPlanID                string `json:"executionPlanId,omitempty"`
	RenderSegmentID                string `json:"renderSegmentId,omitempty"`
}

type ErrorEnvelope struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type EventEnvelope struct {
	SchemaVersion int            `json:"schemaVersion"`
	EventID       string         `json:"eventId"`
	EventType     string         `json:"eventType"`
	Identity      BaseIdentity   `json:"identity"`
	Revision      int64          `json:"revision"`
	Payload       map[string]any `json:"payload,omitempty"`
}

func (identity BaseIdentity) Validate() error {
	if identity.SchemaVersion != SchemaVersionV2 {
		return fmt.Errorf("unsupported execution identity schema version %d", identity.SchemaVersion)
	}
	for name, value := range map[string]string{
		"organizationId":  identity.OrganizationID,
		"projectId":       identity.ProjectID,
		"workflowRunId":   identity.WorkflowRunID,
		"operationId":     identity.OperationID,
		"operationItemId": identity.OperationItemID,
	} {
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("%s must be a UUID", name)
		}
	}
	if identity.Attempt <= 0 {
		return fmt.Errorf("attempt must be positive")
	}
	return nil
}

func (identity VideoIdentityV2) Validate(requirePlan, requireSegment bool) error {
	if err := identity.BaseIdentity.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"productionGenerationId":   identity.ProductionGenerationID,
		"videoProductionBindingId": identity.VideoProductionBindingID,
		"storyboardShotId":         identity.StoryboardShotID,
	} {
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("%s must be a UUID", name)
		}
	}
	if identity.VideoProductionBindingRevision <= 0 {
		return fmt.Errorf("videoProductionBindingRevision must be positive")
	}
	if !validHash(identity.ConfigurationSnapshotHash) {
		return fmt.Errorf("configurationSnapshotHash must be a lowercase sha256 hash")
	}
	if requirePlan {
		if _, err := uuid.Parse(strings.TrimSpace(identity.ExecutionPlanID)); err != nil {
			return fmt.Errorf("executionPlanId must be a UUID")
		}
	}
	if requireSegment {
		if _, err := uuid.Parse(strings.TrimSpace(identity.RenderSegmentID)); err != nil {
			return fmt.Errorf("renderSegmentId must be a UUID")
		}
	}
	return nil
}

func IsTerminal(status Status) bool {
	switch status {
	case StatusSucceeded, StatusPartialSucceeded, StatusFailed, StatusCancelled, StatusDiscarded:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	if IsTerminal(from) {
		return false
	}
	switch from {
	case StatusQueued:
		return to == StatusRunning || to == StatusCancelling || to == StatusFailed || to == StatusCancelled || to == StatusDiscarded
	case StatusRunning:
		return to == StatusCancelling || IsTerminal(to)
	case StatusCancelling:
		return to == StatusCancelled || to == StatusFailed
	default:
		return false
	}
}

func HashRequest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (event EventEnvelope) Validate() error {
	if event.SchemaVersion != SchemaVersionV2 {
		return fmt.Errorf("unsupported event schema version %d", event.SchemaVersion)
	}
	if _, err := uuid.Parse(strings.TrimSpace(event.EventID)); err != nil {
		return fmt.Errorf("eventId must be a UUID")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("eventType is required")
	}
	if event.Revision <= 0 {
		return fmt.Errorf("revision must be positive")
	}
	return event.Identity.Validate()
}

func validHash(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
