package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	CodeIdempotencyKeyReused = "IDEMPOTENCY_KEY_REUSED"
	CodeRunStateConflict     = "COMMERCE_RUN_STATE_CONFLICT"
)

type ProductionRunType string

const (
	RunTypeStoryboardPlan  ProductionRunType = "storyboard_plan"
	RunTypeReferenceImages ProductionRunType = "reference_images"
	RunTypeVideoPrompts    ProductionRunType = "video_prompts"
	RunTypeShotVideos      ProductionRunType = "shot_videos"
	RunTypeFinalCompose    ProductionRunType = "final_compose"
)

type ProductionSubjectType string

const (
	SubjectPlanPhase      ProductionSubjectType = "plan_phase"
	SubjectCandidateShot  ProductionSubjectType = "candidate_shot"
	SubjectStoryboardShot ProductionSubjectType = "storyboard_shot"
	SubjectFinalCompose   ProductionSubjectType = "final_compose"
)

type ProductionItemStatus string

const (
	ItemQueued          ProductionItemStatus = "queued"
	ItemRunning         ProductionItemStatus = "running"
	ItemSucceeded       ProductionItemStatus = "succeeded"
	ItemFailedRetryable ProductionItemStatus = "failed_retryable"
	ItemFailedTerminal  ProductionItemStatus = "failed_terminal"
	ItemCancelled       ProductionItemStatus = "cancelled"
	ItemDiscarded       ProductionItemStatus = "discarded"
	ItemSkipped         ProductionItemStatus = "skipped"
)

type ProductionRunStatus string

const (
	RunQueued             ProductionRunStatus = "queued"
	RunRunning            ProductionRunStatus = "running"
	RunPartiallySucceeded ProductionRunStatus = "partially_succeeded"
	RunSucceeded          ProductionRunStatus = "succeeded"
	RunFailed             ProductionRunStatus = "failed"
	RunCancelling         ProductionRunStatus = "cancelling"
	RunCancelled          ProductionRunStatus = "cancelled"
)

type ProductionSubject struct {
	Type             ProductionSubjectType `json:"type"`
	Key              string                `json:"key"`
	StoryboardShotID string                `json:"storyboardShotId,omitempty"`
	InputHash        string                `json:"inputHash"`
}

func (subject ProductionSubject) Validate(runType ProductionRunType) error {
	if strings.TrimSpace(subject.Key) == "" {
		return errors.New("commerce production subject key is required")
	}
	if !validSHA256(subject.InputHash) {
		return errors.New("commerce production subject input hash is invalid")
	}
	switch runType {
	case RunTypeStoryboardPlan:
		if subject.Type != SubjectPlanPhase && subject.Type != SubjectCandidateShot {
			return fmt.Errorf("%s run requires plan_phase or candidate_shot subjects", runType)
		}
		if subject.StoryboardShotID != "" {
			return fmt.Errorf("%s subjects cannot reference a committed storyboard shot", runType)
		}
	case RunTypeReferenceImages, RunTypeVideoPrompts, RunTypeShotVideos:
		if subject.Type != SubjectStoryboardShot || strings.TrimSpace(subject.StoryboardShotID) == "" {
			return fmt.Errorf("%s run requires storyboard_shot subjects", runType)
		}
	case RunTypeFinalCompose:
		if subject.Type != SubjectFinalCompose || subject.StoryboardShotID != "" {
			return fmt.Errorf("%s run requires one final_compose subject", runType)
		}
	default:
		return fmt.Errorf("unsupported commerce production run type %q", runType)
	}
	return nil
}

type CreateProductionRunParams struct {
	Identity          UnitGenerationIdentity
	RunType           ProductionRunType
	IdempotencyScope  string
	IdempotencyKey    string
	InputSnapshot     json.RawMessage
	Subjects          []ProductionSubject
	WorkflowRunID     string
	CoordinatorItemID string
	CreatedBy         string
}

type ProductionRun struct {
	ID             string                 `json:"id"`
	Identity       UnitGenerationIdentity `json:"identity"`
	WorkflowRunID  string                 `json:"workflowRunId,omitempty"`
	RunType        ProductionRunType      `json:"runType"`
	Status         ProductionRunStatus    `json:"status"`
	PayloadHash    string                 `json:"payloadHash"`
	InputSnapshot  json.RawMessage        `json:"inputSnapshot"`
	Revision       int64                  `json:"revision"`
	TotalItems     int                    `json:"totalItems"`
	CompletedItems int                    `json:"completedItems"`
	FailedItems    int                    `json:"failedItems"`
	CancelledItems int                    `json:"cancelledItems"`
	CreatedAt      time.Time              `json:"createdAt"`
	StartedAt      *time.Time             `json:"startedAt,omitempty"`
	CompletedAt    *time.Time             `json:"completedAt,omitempty"`
	ErrorCode      string                 `json:"errorCode,omitempty"`
	ErrorMessage   string                 `json:"errorMessage,omitempty"`
}

type ProductionRunItem struct {
	ID                        string                 `json:"id"`
	RunID                     string                 `json:"runId"`
	Identity                  UnitGenerationIdentity `json:"identity"`
	Subject                   ProductionSubject      `json:"subject"`
	Status                    ProductionItemStatus   `json:"status"`
	CurrentAttempt            int                    `json:"currentAttempt"`
	OutputSnapshot            json.RawMessage        `json:"outputSnapshot"`
	OutputArtifactID          string                 `json:"outputArtifactId,omitempty"`
	OutputMediaFileID         string                 `json:"outputMediaFileId,omitempty"`
	OutputStoryboardPlanID    string                 `json:"outputStoryboardPlanId,omitempty"`
	OutputVideoPromptPlanID   string                 `json:"outputVideoPromptPlanId,omitempty"`
	OutputVideoRenderPlanID   string                 `json:"outputVideoRenderPlanId,omitempty"`
	OutputFinalVideoVersionID string                 `json:"outputFinalVideoVersionId,omitempty"`
	ProviderRequestID         string                 `json:"providerRequestId,omitempty"`
	ProviderCallID            string                 `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID       string                 `json:"providerAsyncTaskId,omitempty"`
	ErrorCode                 string                 `json:"errorCode,omitempty"`
	ErrorMessage              string                 `json:"errorMessage,omitempty"`
	Retryable                 bool                   `json:"retryable"`
	StartedAt                 *time.Time             `json:"startedAt,omitempty"`
	CompletedAt               *time.Time             `json:"completedAt,omitempty"`
}

type ProductionRunDetail struct {
	Run   ProductionRun       `json:"run"`
	Items []ProductionRunItem `json:"items"`
}

type StartProductionAttemptParams struct {
	OrganizationID string
	ProjectID      string
	RunID          string
	ItemID         string
	InputHash      string
	WorkflowRunID  string
	NodeRunID      string
}

type ProductionAttempt struct {
	ID            string               `json:"id"`
	RunID         string               `json:"runId"`
	ItemID        string               `json:"itemId"`
	AttemptNumber int                  `json:"attemptNumber"`
	InputHash     string               `json:"inputHash"`
	Status        ProductionItemStatus `json:"status"`
}

type CompleteProductionAttemptParams struct {
	OrganizationID            string
	ProjectID                 string
	RunID                     string
	ItemID                    string
	AttemptID                 string
	Status                    ProductionItemStatus
	OutputSnapshot            json.RawMessage
	OutputArtifactID          string
	OutputMediaFileID         string
	OutputStoryboardPlanID    string
	OutputVideoPromptPlanID   string
	OutputVideoRenderPlanID   string
	OutputFinalVideoVersionID string
	ProviderRequestID         string
	ProviderCallID            string
	ProviderAsyncTaskID       string
	ErrorCode                 string
	ErrorMessage              string
	Retryable                 bool
}

type ProductionRunAggregate struct {
	Status    ProductionRunStatus
	Total     int
	Completed int
	Failed    int
	Cancelled int
	Active    int
}

type ProductionRunStore interface {
	Store
	FindProductionRunByIdempotencyKey(context.Context, pgx.Tx, string, string, string) (ProductionRun, bool, error)
	AssertNoActiveProductionSubjectOverlap(context.Context, pgx.Tx, UnitGenerationIdentity, ProductionRunType, []ProductionSubject) error
	InsertProductionRun(context.Context, pgx.Tx, string, string, CreateProductionRunParams) (ProductionRun, error)
	InsertProductionRunItems(context.Context, pgx.Tx, ProductionRun, []ProductionSubject) error
	LockProductionRunItem(context.Context, pgx.Tx, string, string, string, string) (ProductionRunItem, error)
	InsertProductionAttempt(context.Context, pgx.Tx, ProductionRunItem, StartProductionAttemptParams) (ProductionAttempt, error)
	CompleteProductionAttempt(context.Context, pgx.Tx, ProductionRunItem, CompleteProductionAttemptParams) error
	ReconcileProductionRun(context.Context, pgx.Tx, string, string, string) (ProductionRun, error)
}

type ProductionRunService struct {
	repository ProductionRunStore
	identity   *Service
}

func NewProductionRunService(repository ProductionRunStore) *ProductionRunService {
	if repository == nil {
		repository = NewRepository()
	}
	return &ProductionRunService{repository: repository, identity: NewService(repository)}
}

func (s *ProductionRunService) CreateRun(ctx context.Context, tx pgx.Tx, params CreateProductionRunParams) (ProductionRun, bool, error) {
	params.IdempotencyScope = strings.TrimSpace(params.IdempotencyScope)
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if params.IdempotencyScope == "" || params.IdempotencyKey == "" {
		return ProductionRun{}, false, errors.New("commerce production run idempotency identity is required")
	}
	if len(params.Subjects) == 0 {
		return ProductionRun{}, false, errors.New("commerce production run requires explicit subjects")
	}
	if len(params.InputSnapshot) == 0 {
		params.InputSnapshot = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(params.InputSnapshot); err != nil {
		return ProductionRun{}, false, errors.New("commerce production run input snapshot must be a JSON object")
	}
	seen := make(map[string]struct{}, len(params.Subjects))
	for _, subject := range params.Subjects {
		if err := subject.Validate(params.RunType); err != nil {
			return ProductionRun{}, false, err
		}
		identity := string(subject.Type) + "\x00" + strings.TrimSpace(subject.Key)
		if _, exists := seen[identity]; exists {
			return ProductionRun{}, false, errors.New("commerce production run contains duplicate subjects")
		}
		seen[identity] = struct{}{}
	}
	if params.RunType == RunTypeFinalCompose && len(params.Subjects) != 1 {
		return ProductionRun{}, false, errors.New("final compose run requires exactly one subject")
	}
	payloadHash, err := hashJSONValue(map[string]any{
		"runType":       params.RunType,
		"identity":      params.Identity,
		"inputSnapshot": json.RawMessage(params.InputSnapshot),
		"subjects":      params.Subjects,
	})
	if err != nil {
		return ProductionRun{}, false, err
	}
	if existing, found, err := s.repository.FindProductionRunByIdempotencyKey(
		ctx, tx, params.Identity.OrganizationID, params.IdempotencyScope, params.IdempotencyKey,
	); err != nil {
		return ProductionRun{}, false, err
	} else if found {
		if existing.PayloadHash != payloadHash {
			return ProductionRun{}, false, Error{Code: CodeIdempotencyKeyReused, Message: "幂等键已用于不同的生产请求"}
		}
		return existing, false, nil
	}
	if _, err := s.identity.AssertWritableUnitGeneration(ctx, tx, params.Identity); err != nil {
		return ProductionRun{}, false, err
	}
	if err := s.repository.AssertNoActiveProductionSubjectOverlap(ctx, tx, params.Identity, params.RunType, params.Subjects); err != nil {
		return ProductionRun{}, false, err
	}
	run, err := s.repository.InsertProductionRun(ctx, tx, newID(), payloadHash, params)
	if err != nil {
		return ProductionRun{}, false, err
	}
	if err := s.repository.InsertProductionRunItems(ctx, tx, run, params.Subjects); err != nil {
		return ProductionRun{}, false, err
	}
	return run, true, nil
}

func (s *ProductionRunService) StartAttempt(ctx context.Context, tx pgx.Tx, params StartProductionAttemptParams) (ProductionAttempt, error) {
	if !validSHA256(params.InputHash) {
		return ProductionAttempt{}, errors.New("commerce production attempt input hash is invalid")
	}
	item, err := s.repository.LockProductionRunItem(ctx, tx, params.OrganizationID, params.ProjectID, params.RunID, params.ItemID)
	if err != nil {
		return ProductionAttempt{}, err
	}
	if _, err := s.identity.AssertWritableUnitGeneration(ctx, tx, item.Identity); err != nil {
		return ProductionAttempt{}, err
	}
	if item.Subject.InputHash != params.InputHash {
		return ProductionAttempt{}, Error{
			Code:    CodeGenerationMismatch,
			Message: "生产项执行输入与冻结批次不一致；请创建新的生产批次",
		}
	}
	if item.Status != ItemQueued && item.Status != ItemFailedRetryable {
		return ProductionAttempt{}, Error{Code: CodeRunStateConflict, Message: "当前生产项不能启动新的执行尝试"}
	}
	return s.repository.InsertProductionAttempt(ctx, tx, item, params)
}

func (s *ProductionRunService) CompleteAttempt(ctx context.Context, tx pgx.Tx, params CompleteProductionAttemptParams) (ProductionRun, error) {
	if !terminalAttemptStatus(params.Status) {
		return ProductionRun{}, errors.New("commerce production attempt requires a terminal status")
	}
	if (params.Status == ItemFailedRetryable || params.Status == ItemFailedTerminal) && strings.TrimSpace(params.ErrorCode) == "" {
		return ProductionRun{}, errors.New("failed commerce production attempt requires an error code")
	}
	if len(params.OutputSnapshot) == 0 {
		params.OutputSnapshot = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(params.OutputSnapshot); err != nil {
		return ProductionRun{}, errors.New("commerce production attempt output snapshot must be a JSON object")
	}
	item, err := s.repository.LockProductionRunItem(ctx, tx, params.OrganizationID, params.ProjectID, params.RunID, params.ItemID)
	if err != nil {
		return ProductionRun{}, err
	}
	if _, err := s.identity.AssertWritableUnitGeneration(ctx, tx, item.Identity); err != nil {
		return ProductionRun{}, err
	}
	if item.Status != ItemRunning {
		return ProductionRun{}, Error{Code: CodeRunStateConflict, Message: "当前生产项没有运行中的执行尝试"}
	}
	if err := s.repository.CompleteProductionAttempt(ctx, tx, item, params); err != nil {
		return ProductionRun{}, err
	}
	return s.repository.ReconcileProductionRun(ctx, tx, params.OrganizationID, params.ProjectID, params.RunID)
}

func AggregateProductionRun(current ProductionRunStatus, statuses []ProductionItemStatus) ProductionRunAggregate {
	result := ProductionRunAggregate{Status: current, Total: len(statuses)}
	for _, status := range statuses {
		switch status {
		case ItemSucceeded, ItemSkipped:
			result.Completed++
		case ItemFailedRetryable, ItemFailedTerminal, ItemDiscarded:
			result.Failed++
		case ItemCancelled:
			result.Cancelled++
		default:
			result.Active++
		}
	}
	if result.Total == 0 {
		if current != RunCancelling {
			result.Status = RunQueued
		}
		return result
	}
	if result.Active > 0 {
		if current == RunCancelling {
			result.Status = RunCancelling
		} else if result.Completed == 0 && result.Failed == 0 && result.Cancelled == 0 && allQueued(statuses) {
			result.Status = RunQueued
		} else {
			result.Status = RunRunning
		}
		return result
	}
	switch {
	case result.Completed == result.Total:
		result.Status = RunSucceeded
	case result.Cancelled == result.Total:
		result.Status = RunCancelled
	case result.Failed == result.Total:
		result.Status = RunFailed
	default:
		result.Status = RunPartiallySucceeded
	}
	return result
}

func terminalAttemptStatus(status ProductionItemStatus) bool {
	switch status {
	case ItemSucceeded, ItemFailedRetryable, ItemFailedTerminal, ItemCancelled, ItemDiscarded:
		return true
	default:
		return false
	}
}

func allQueued(statuses []ProductionItemStatus) bool {
	for _, status := range statuses {
		if status != ItemQueued {
			return false
		}
	}
	return true
}

func hashJSONValue(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashJSON(raw)
}
