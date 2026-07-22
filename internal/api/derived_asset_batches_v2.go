package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/assetprompts"
	"github.com/Einzieg/cineweave/internal/auth"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	derivedAssetBatchModeExplicit  = "explicit"
	derivedAssetBatchModeSelectAll = "select_all"
	derivedAssetBatchModeRetry     = "retry"

	derivedAssetBatchWorkflowType = "batch_generate_derived_asset_images"
	derivedAssetBatchScope        = "derived_asset_batch_v2"
	maxDerivedAssetBatchItems     = 500
)

// DerivedAssetBatchFilters is the durable selector used by select_all. Empty
// slices mean no restriction. The selected IDs are materialized as request
// items in the command transaction, so later database changes cannot alter the
// workset.
type DerivedAssetBatchFilters struct {
	AssetTypes       []string `json:"assetTypes,omitempty"`
	RequirementTypes []string `json:"requirementTypes,omitempty"`
	ReviewStatuses   []string `json:"reviewStatuses,omitempty"`
	Statuses         []string `json:"statuses,omitempty"`
	ShotIDs          []string `json:"shotIds,omitempty"`
	ScriptEpisodeID  string   `json:"scriptEpisodeId,omitempty"`
}

// DerivedAssetBatchCreateOptions is shared by the single-item, select-all and
// retry command adapters. Callers must authorize the project before invoking
// createDerivedAssetBatchRun.
type DerivedAssetBatchCreateOptions struct {
	Mode                    string                   `json:"mode"`
	RequirementIDs          []string                 `json:"requirementIds,omitempty"`
	Filters                 DerivedAssetBatchFilters `json:"filters,omitempty"`
	RetryOfBatchID          string                   `json:"retryOfBatchId,omitempty"`
	MaxConcurrency          int                      `json:"maxConcurrency,omitempty"`
	Force                   bool                     `json:"force,omitempty"`
	ExpectedProjectRevision int64                    `json:"expectedProjectRevision,omitempty"`
	IdempotencyKey          string                   `json:"idempotencyKey,omitempty"`
	AgentTaskID             string                   `json:"agentTaskId,omitempty"`
	AgentStepID             string                   `json:"agentStepId,omitempty"`
}

type DerivedAssetExecutionProjection struct {
	ID                     string          `json:"id"`
	NodeRunID              string          `json:"nodeRunId"`
	NodeKey                string          `json:"nodeKey"`
	AttemptNo              int             `json:"attemptNo"`
	Status                 string          `json:"status"`
	Revision               int64           `json:"revision"`
	ProviderRequestID      *string         `json:"providerRequestId,omitempty"`
	ProviderCallID         *string         `json:"providerCallId,omitempty"`
	SelectedCredentialID   *string         `json:"selectedCredentialId,omitempty"`
	ArtifactID             *string         `json:"artifactId,omitempty"`
	MediaFileID            *string         `json:"mediaFileId,omitempty"`
	StorageKey             *string         `json:"storageKey,omitempty"`
	ErrorCode              *string         `json:"errorCode,omitempty"`
	ErrorMessage           *string         `json:"errorMessage,omitempty"`
	Diagnostic             json.RawMessage `json:"diagnostic"`
	LateResultCount        int             `json:"lateResultCount"`
	LateResultDiagnostics  json.RawMessage `json:"lateResultDiagnostics"`
	CreatedAt              time.Time       `json:"createdAt"`
	StartedAt              *time.Time      `json:"startedAt,omitempty"`
	CompletedAt            *time.Time      `json:"completedAt,omitempty"`
	ProductionGenerationID string          `json:"productionGenerationId"`
}

type DerivedAssetRequestItemProjection struct {
	ID                       string                           `json:"id"`
	InputOrdinal             int                              `json:"inputOrdinal"`
	OriginalID               string                           `json:"originalId"`
	RequirementID            *string                          `json:"requirementId,omitempty"`
	DuplicateOfRequestItemID *string                          `json:"duplicateOfRequestItemId,omitempty"`
	RootRequestItemID        *string                          `json:"rootRequestItemId,omitempty"`
	RetryOfRequestItemID     *string                          `json:"retryOfRequestItemId,omitempty"`
	Disposition              string                           `json:"disposition"`
	DispositionDetail        json.RawMessage                  `json:"dispositionDetail"`
	ErrorCode                *string                          `json:"errorCode,omitempty"`
	ErrorMessage             *string                          `json:"errorMessage,omitempty"`
	Retryable                bool                             `json:"retryable"`
	InputSnapshot            json.RawMessage                  `json:"inputSnapshot"`
	InputHash                string                           `json:"inputHash"`
	Status                   string                           `json:"status"`
	CurrentAttemptID         *string                          `json:"currentAttemptId,omitempty"`
	CurrentAttemptNo         *int                             `json:"currentAttemptNo,omitempty"`
	Revision                 int64                            `json:"revision"`
	CreatedAt                time.Time                        `json:"createdAt"`
	UpdatedAt                time.Time                        `json:"updatedAt"`
	Execution                *DerivedAssetExecutionProjection `json:"execution,omitempty"`
}

type DerivedAssetBatchProjection struct {
	ID                             string                              `json:"id"`
	OrganizationID                 string                              `json:"organizationId"`
	ProjectID                      string                              `json:"projectId"`
	WorkflowRunID                  string                              `json:"workflowRunId"`
	ProductionGenerationID         string                              `json:"productionGenerationId"`
	VideoProductionBindingID       string                              `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64                               `json:"videoProductionBindingRevision"`
	RootBatchID                    *string                             `json:"rootBatchId,omitempty"`
	RetryOfBatchID                 *string                             `json:"retryOfBatchId,omitempty"`
	RetryDepth                     int                                 `json:"retryDepth"`
	RequestMode                    string                              `json:"requestMode"`
	Filters                        json.RawMessage                     `json:"filters"`
	FiltersHash                    string                              `json:"filtersHash"`
	SelectorCandidateCount         int                                 `json:"selectorCandidateCount"`
	SelectorSkippedCount           int                                 `json:"selectorSkippedCount"`
	IdempotencyKey                 string                              `json:"idempotencyKey"`
	RequestHash                    string                              `json:"requestHash"`
	Status                         string                              `json:"status"`
	Revision                       int64                               `json:"revision"`
	TotalItems                     int                                 `json:"totalItems"`
	ExecutableItems                int                                 `json:"executableItems"`
	ReviewRequiredItems            int                                 `json:"reviewRequiredItems"`
	NotFoundItems                  int                                 `json:"notFoundItems"`
	GenerationMismatchItems        int                                 `json:"generationMismatchItems"`
	AlreadyRunningItems            int                                 `json:"alreadyRunningItems"`
	DuplicateItems                 int                                 `json:"duplicateItems"`
	SkippedItems                   int                                 `json:"skippedItems"`
	PendingItems                   int                                 `json:"pendingItems"`
	QueuedItems                    int                                 `json:"queuedItems"`
	RunningItems                   int                                 `json:"runningItems"`
	SucceededItems                 int                                 `json:"succeededItems"`
	FailedRetryableItems           int                                 `json:"failedRetryableItems"`
	FailedTerminalItems            int                                 `json:"failedTerminalItems"`
	CancelledItems                 int                                 `json:"cancelledItems"`
	DiscardedItems                 int                                 `json:"discardedItems"`
	ErrorCode                      *string                             `json:"errorCode,omitempty"`
	ErrorMessage                   *string                             `json:"errorMessage,omitempty"`
	CreatedBy                      *string                             `json:"createdBy,omitempty"`
	CreatedAt                      time.Time                           `json:"createdAt"`
	UpdatedAt                      time.Time                           `json:"updatedAt"`
	StartedAt                      *time.Time                          `json:"startedAt,omitempty"`
	CompletedAt                    *time.Time                          `json:"completedAt,omitempty"`
	Items                          []DerivedAssetRequestItemProjection `json:"items"`
}

type DerivedAssetBatchCommandResult struct {
	Batch            DerivedAssetBatchProjection `json:"batch"`
	WorkflowRun      WorkflowRun                 `json:"workflowRun"`
	IdempotentReplay bool                        `json:"idempotentReplay,omitempty"`
	OperationID      string                      `json:"operationId,omitempty"`
}

type derivedAssetRequestedItem struct {
	rawID                string
	originalID           string
	lookupID             string
	inputSnapshot        json.RawMessage
	inputHash            string
	retrySourceRequestID string
	rootRequestID        string
	retrySourceAttempt   *derivedAssetFrozenExecution
}

type derivedAssetDisposition struct {
	name      string
	status    string
	errorCode string
	message   string
	retryable bool
	detail    json.RawMessage
	candidate *derivedAssetCandidate
	frozen    *derivedAssetFrozenExecution
}

type preparedDerivedAssetRequest struct {
	requestItemID string
	ordinal       int
	requested     derivedAssetRequestedItem
	duplicateOf   string
	disposition   derivedAssetDisposition
}

type derivedAssetCandidate struct {
	requirementID        string
	organizationID       string
	projectID            string
	generationID         string
	storyboardShotID     string
	canonicalAssetID     string
	requirementType      string
	roleInShot           sql.NullString
	costume              sql.NullString
	pose                 sql.NullString
	expression           sql.NullString
	action               sql.NullString
	cameraRelation       sql.NullString
	sceneState           sql.NullString
	propState            sql.NullString
	prompt               sql.NullString
	status               string
	reviewStatus         string
	staleState           string
	metadata             json.RawMessage
	requirementUpdatedAt time.Time
	shotDeletedAt        sql.NullTime
	shotUpdatedAt        time.Time
	shotNo               int
	shotVisual           string
	shotCamera           string
	shotMotion           string
	shotMood             string
	asset                CanonicalAsset
	activeExecution      bool
}

type derivedAssetModelSelection struct {
	providerAccountID  string
	providerModelID    string
	modelProfileKey    string
	modelSnapshot      json.RawMessage
	modelHash          string
	capabilitySnapshot json.RawMessage
	capabilityHash     string
}

type derivedAssetFrozenExecution struct {
	requirementID       string
	storyboardShotID    string
	canonicalAssetID    string
	requirementSnapshot json.RawMessage
	requirementHash     string
	shotSnapshot        json.RawMessage
	shotHash            string
	assetSnapshot       json.RawMessage
	assetHash           string
	promptText          string
	promptSnapshot      json.RawMessage
	promptHash          string
	referenceSnapshot   json.RawMessage
	referenceHash       string
	modelProfileKey     string
	providerAccountID   string
	providerModelID     string
	modelSnapshot       json.RawMessage
	modelHash           string
	capabilitySnapshot  json.RawMessage
	capabilityHash      string
	requestSnapshot     json.RawMessage
	requestHash         string
	rootAttemptID       string
	retryOfAttemptID    string
	attemptNo           int
}

// createDerivedAssetBatchRun is the single command transaction used by all
// future HTTP, production-action and Agent adapters. It returns a projection
// suitable for an HTTP 202 response and never performs a Provider call.
func (s *Server) createDerivedAssetBatchRun(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	options DerivedAssetBatchCreateOptions,
) (DerivedAssetBatchCommandResult, error) {
	options, err := normalizeDerivedAssetBatchOptions(options)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId":               project.ID,
		"mode":                    options.Mode,
		"requirementIds":          options.RequirementIDs,
		"filters":                 options.Filters,
		"retryOfBatchId":          options.RetryOfBatchID,
		"maxConcurrency":          options.MaxConcurrency,
		"force":                   options.Force,
		"expectedProjectRevision": options.ExpectedProjectRevision,
		"agentTaskId":             options.AgentTaskID,
		"agentStepId":             options.AgentStepID,
	})
	if options.IdempotencyKey == "" {
		options.IdempotencyKey = uuid.NewString()
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	defer tx.Rollback(ctx)

	lockedProject, err := scanProject(tx.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	if lockedProject.OrganizationID != project.OrganizationID || principal.OrganizationID != lockedProject.OrganizationID {
		return DerivedAssetBatchCommandResult{}, newAPIError(http.StatusNotFound, "NOT_FOUND", "project was not found")
	}
	productionContext, err := videoproduction.LoadWritableContextTx(ctx, tx, lockedProject.ID, false)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	lockedProject.VideoProductionBinding = &productionContext.Binding
	lockedProject.ProductionGeneration = &productionContext.Generation
	lockedProject.VideoProductionState = productionContext.State
	lockedProject.VideoProductionLocked = productionContext.Locked
	if options.ExpectedProjectRevision > 0 && options.ExpectedProjectRevision != lockedProject.Revision {
		conflict := newAPIError(http.StatusConflict, "PROJECT_REVISION_CONFLICT", "project settings changed before the batch was created")
		conflict.Details = map[string]any{"expectedRevision": options.ExpectedProjectRevision, "currentRevision": lockedProject.Revision}
		return DerivedAssetBatchCommandResult{}, conflict
	}

	claim, err := claimIdempotencyTx(ctx, tx, lockedProject.OrganizationID, derivedAssetBatchScope, options.IdempotencyKey, requestHash)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay DerivedAssetBatchCommandResult
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return DerivedAssetBatchCommandResult{}, err
		}
		replay.IdempotentReplay = true
		replay.OperationID = claim.state.operationID
		return replay, nil
	}
	operationID, err := ensureRuntimeOperationTx(ctx, tx, &claim, lockedProject.OrganizationID, lockedProject.ID, derivedAssetBatchScope, requestHash)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}

	requestedItems, retryLineage, selectorCandidateCount, err := loadDerivedAssetRequestedItemsTx(ctx, tx, lockedProject, options)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	if len(requestedItems) == 0 {
		return DerivedAssetBatchCommandResult{}, newAPIError(http.StatusUnprocessableEntity, "DERIVED_ASSET_WORKSET_EMPTY", "没有可处理的镜头衍生资产需求")
	}
	if len(requestedItems) > maxDerivedAssetBatchItems {
		return DerivedAssetBatchCommandResult{}, newAPIError(http.StatusUnprocessableEntity, "DERIVED_ASSET_WORKSET_TOO_LARGE", "单次最多处理 500 个镜头衍生资产需求")
	}

	batchID := uuid.NewString()
	workflowOptions := workflows.DerivedAssetBatchOptions{
		BatchID: batchID, MaxConcurrency: options.MaxConcurrency, Force: options.Force,
		AgentTaskID: strings.TrimSpace(options.AgentTaskID), AgentStepID: strings.TrimSpace(options.AgentStepID),
	}
	inputJSON := mustRawJSON(workflowOptions)
	runInput := mustRawJSON(map[string]any{
		"prompt": derivedAssetBatchWorkflowType, "workflowType": derivedAssetBatchWorkflowType, "input": workflowOptions,
	})

	var commandResult DerivedAssetBatchCommandResult
	run, err := s.enqueueProjectWorkflowTx(
		ctx,
		tx,
		principal,
		lockedProject,
		derivedAssetBatchWorkflowType,
		runInput,
		workflows.ScriptTaskQueue,
		workflows.BatchGenerateDerivedAssetImagesWorkflow,
		func(run WorkflowRun) any {
			return workflows.TextToStoryboardInput{
				OrganizationID: lockedProject.OrganizationID,
				ProjectID:      lockedProject.ID,
				WorkflowRunID:  run.ID,
				Prompt:         derivedAssetBatchWorkflowType,
				CreatedBy:      principal.UserID,
				Input:          inputJSON,
			}
		},
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			projection, err := s.persistDerivedAssetBatchTx(
				ctx, tx, principal, lockedProject, run, batchID, options, requestHash,
				requestedItems, retryLineage, selectorCandidateCount,
			)
			if err != nil {
				return err
			}
			updatedRun, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), run.ID))
			if err != nil {
				return err
			}
			commandResult = DerivedAssetBatchCommandResult{Batch: projection, WorkflowRun: updatedRun, OperationID: operationID}
			if _, err := completeRuntimeOperationTx(ctx, tx, operationID, run.ID, commandResult); err != nil {
				return err
			}
			return completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, commandResult)
		},
	)
	if err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	if commandResult.WorkflowRun.ID == "" {
		commandResult.WorkflowRun = run
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetBatchCommandResult{}, err
	}
	return commandResult, nil
}

// retryDerivedAssetBatchRun creates a new immutable batch containing only
// retryable failed or blocked request items from sourceBatchID.
func (s *Server) retryDerivedAssetBatchRun(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	sourceBatchID string,
	maxConcurrency int,
	idempotencyKey string,
) (DerivedAssetBatchCommandResult, error) {
	return s.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode:           derivedAssetBatchModeRetry,
		RetryOfBatchID: strings.TrimSpace(sourceBatchID),
		MaxConcurrency: maxConcurrency,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
}

func normalizeDerivedAssetBatchOptions(options DerivedAssetBatchCreateOptions) (DerivedAssetBatchCreateOptions, error) {
	options.Mode = strings.ToLower(strings.TrimSpace(options.Mode))
	if options.Mode == "" {
		options.Mode = derivedAssetBatchModeExplicit
	}
	switch options.Mode {
	case derivedAssetBatchModeExplicit:
		if len(options.RequirementIDs) == 0 {
			return options, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "requirementIds is required")
		}
		for _, requirementID := range options.RequirementIDs {
			if strings.TrimSpace(requirementID) == "" {
				return options, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "requirementIds cannot contain empty values")
			}
		}
	case derivedAssetBatchModeSelectAll:
		if len(options.RequirementIDs) != 0 {
			return options, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "select_all does not accept requirementIds")
		}
	case derivedAssetBatchModeRetry:
		if strings.TrimSpace(options.RetryOfBatchID) == "" {
			return options, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "retryOfBatchId is required")
		}
	default:
		return options, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "derived asset batch mode is invalid")
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = workflows.DefaultDerivedAssetImageConcurrency
	}
	if options.MaxConcurrency > workflows.MaxDerivedAssetImageConcurrency {
		options.MaxConcurrency = workflows.MaxDerivedAssetImageConcurrency
	}
	options.RetryOfBatchID = strings.TrimSpace(options.RetryOfBatchID)
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	options.AgentTaskID = strings.TrimSpace(options.AgentTaskID)
	options.AgentStepID = strings.TrimSpace(options.AgentStepID)
	options.Filters = normalizeDerivedAssetBatchFilters(options.Filters)
	return options, nil
}

func normalizeDerivedAssetBatchFilters(filters DerivedAssetBatchFilters) DerivedAssetBatchFilters {
	filters.AssetTypes = sortedUniqueLowerStrings(filters.AssetTypes)
	filters.RequirementTypes = sortedUniqueLowerStrings(filters.RequirementTypes)
	filters.ReviewStatuses = sortedUniqueLowerStrings(filters.ReviewStatuses)
	filters.Statuses = sortedUniqueLowerStrings(filters.Statuses)
	filters.ShotIDs = sortedUniqueTrimmedStrings(filters.ShotIDs)
	filters.ScriptEpisodeID = strings.TrimSpace(filters.ScriptEpisodeID)
	return filters
}

func sortedUniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedUniqueLowerStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type derivedAssetRetryLineage struct {
	rootBatchID string
	retryDepth  int
}

func loadDerivedAssetRequestedItemsTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	options DerivedAssetBatchCreateOptions,
) ([]derivedAssetRequestedItem, derivedAssetRetryLineage, int, error) {
	switch options.Mode {
	case derivedAssetBatchModeExplicit:
		items := make([]derivedAssetRequestedItem, 0, len(options.RequirementIDs))
		for _, rawID := range options.RequirementIDs {
			items = append(items, newDerivedAssetRequestedItem(rawID))
		}
		return items, derivedAssetRetryLineage{}, len(items), nil
	case derivedAssetBatchModeSelectAll:
		ids, candidateCount, err := selectDerivedAssetRequirementIDsTx(ctx, tx, project, options.Filters)
		if err != nil {
			return nil, derivedAssetRetryLineage{}, 0, err
		}
		items := make([]derivedAssetRequestedItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, newDerivedAssetRequestedItem(id))
		}
		return items, derivedAssetRetryLineage{}, candidateCount, nil
	case derivedAssetBatchModeRetry:
		return loadDerivedAssetRetryItemsTx(ctx, tx, project, options.RetryOfBatchID)
	default:
		return nil, derivedAssetRetryLineage{}, 0, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "derived asset batch mode is invalid")
	}
}

func newDerivedAssetRequestedItem(rawID string) derivedAssetRequestedItem {
	rawID = strings.TrimSpace(rawID)
	originalID := rawID
	lookupID := ""
	if parsed, err := uuid.Parse(originalID); err == nil {
		lookupID = parsed.String()
	}
	snapshot, hash := derivedAssetSnapshot(map[string]any{
		"schemaVersion": 2,
		"requestedId":   rawID,
		"originalId":    originalID,
	})
	return derivedAssetRequestedItem{rawID: rawID, originalID: originalID, lookupID: lookupID, inputSnapshot: snapshot, inputHash: hash}
}

func selectDerivedAssetRequirementIDsTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	filters DerivedAssetBatchFilters,
) ([]string, int, error) {
	if project.ProductionGeneration == nil {
		return nil, 0, newAPIError(http.StatusConflict, "PRODUCTION_GENERATION_MISMATCH", "项目没有活动的视频生产代")
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM shot_asset_requirements requirement
		JOIN storyboard_shots shot ON shot.id = requirement.storyboard_shot_id
		JOIN canonical_assets asset ON asset.id = requirement.asset_id
		WHERE requirement.project_id = $1
		  AND requirement.production_generation_id = $2
		  AND (cardinality($3::text[]) = 0 OR asset.asset_type = ANY($3::text[]))
		  AND (cardinality($4::text[]) = 0 OR requirement.requirement_type = ANY($4::text[]))
		  AND (cardinality($5::text[]) = 0 OR requirement.review_status = ANY($5::text[]))
		  AND (cardinality($6::text[]) = 0 OR requirement.status = ANY($6::text[]))
		  AND ($7 = '' OR shot.script_episode_id::text = $7)
		  AND (cardinality($8::text[]) = 0 OR shot.id::text = ANY($8::text[]))
	`, project.ID, project.ProductionGeneration.ID, filters.AssetTypes, filters.RequirementTypes,
		filters.ReviewStatuses, filters.Statuses, filters.ScriptEpisodeID, filters.ShotIDs).Scan(&count); err != nil {
		return nil, 0, err
	}
	if count > maxDerivedAssetBatchItems {
		return nil, count, newAPIError(http.StatusUnprocessableEntity, "DERIVED_ASSET_WORKSET_TOO_LARGE", "筛选结果超过 500 项，请缩小筛选范围")
	}
	rows, err := tx.Query(ctx, `
		SELECT requirement.id::text
		FROM shot_asset_requirements requirement
		JOIN storyboard_shots shot ON shot.id = requirement.storyboard_shot_id
		JOIN canonical_assets asset ON asset.id = requirement.asset_id
		WHERE requirement.project_id = $1
		  AND requirement.production_generation_id = $2
		  AND (cardinality($3::text[]) = 0 OR asset.asset_type = ANY($3::text[]))
		  AND (cardinality($4::text[]) = 0 OR requirement.requirement_type = ANY($4::text[]))
		  AND (cardinality($5::text[]) = 0 OR requirement.review_status = ANY($5::text[]))
		  AND (cardinality($6::text[]) = 0 OR requirement.status = ANY($6::text[]))
		  AND ($7 = '' OR shot.script_episode_id::text = $7)
		  AND (cardinality($8::text[]) = 0 OR shot.id::text = ANY($8::text[]))
		ORDER BY COALESCE(shot.episode_index, 2147483647), COALESCE(shot.episode_shot_index, shot.shot_index),
		         requirement.created_at, requirement.id
	`, project.ID, project.ProductionGeneration.ID, filters.AssetTypes, filters.RequirementTypes,
		filters.ReviewStatuses, filters.Statuses, filters.ScriptEpisodeID, filters.ShotIDs)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := make([]string, 0, count)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, count, rows.Err()
}

func loadDerivedAssetRetryItemsTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	sourceBatchID string,
) ([]derivedAssetRequestedItem, derivedAssetRetryLineage, int, error) {
	var sourceGenerationID, sourceBindingID, rootBatchID string
	var sourceBindingRevision int64
	var retryDepth int
	err := tx.QueryRow(ctx, `
		SELECT production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision, COALESCE(root_batch_id, id)::text, retry_depth
		FROM derived_asset_batches
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		FOR SHARE
	`, sourceBatchID, project.OrganizationID, project.ID).Scan(
		&sourceGenerationID, &sourceBindingID, &sourceBindingRevision, &rootBatchID, &retryDepth,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, derivedAssetRetryLineage{}, 0, newAPIError(http.StatusNotFound, "NOT_FOUND", "derived asset batch was not found")
	}
	if err != nil {
		return nil, derivedAssetRetryLineage{}, 0, err
	}
	if project.ProductionGeneration == nil || project.VideoProductionBinding == nil ||
		sourceGenerationID != project.ProductionGeneration.ID ||
		sourceBindingID != project.VideoProductionBinding.ID ||
		sourceBindingRevision != project.VideoProductionBinding.Revision {
		return nil, derivedAssetRetryLineage{}, 0, newAPIError(http.StatusConflict, "PRODUCTION_GENERATION_MISMATCH", "原批次不属于当前视频生产代，不能重试")
	}
	rows, err := tx.Query(ctx, `
		SELECT item.id::text, COALESCE(item.root_request_item_id, item.id)::text,
		       item.original_id::text, item.input_snapshot, item.input_hash,
		       attempt.id::text, COALESCE(attempt.root_attempt_id, attempt.id)::text,
		       attempt.attempt_no,
		       attempt.requirement_id::text, attempt.storyboard_shot_id::text, attempt.canonical_asset_id::text,
		       attempt.requirement_snapshot, attempt.requirement_snapshot_hash,
		       attempt.storyboard_shot_snapshot, attempt.storyboard_shot_snapshot_hash,
		       attempt.canonical_asset_snapshot, attempt.canonical_asset_snapshot_hash,
		       attempt.prompt_text, attempt.prompt_snapshot, attempt.prompt_hash,
		       attempt.reference_snapshot, attempt.reference_snapshot_hash,
		       attempt.model_profile_key, attempt.provider_account_id::text, attempt.provider_model_id::text,
		       attempt.model_snapshot, attempt.model_snapshot_hash,
		       attempt.capability_snapshot, attempt.capability_snapshot_hash,
		       attempt.request_snapshot, attempt.request_hash
		FROM derived_asset_request_items item
		LEFT JOIN derived_asset_execution_items attempt ON attempt.id = item.current_attempt_id
		WHERE item.batch_id = $1
		  AND (
		    item.status = 'failed_retryable'
		    OR (item.status = 'blocked' AND item.retryable = true)
		  )
		ORDER BY item.input_ordinal
	`, sourceBatchID)
	if err != nil {
		return nil, derivedAssetRetryLineage{}, 0, err
	}
	defer rows.Close()
	items := make([]derivedAssetRequestedItem, 0)
	for rows.Next() {
		var item derivedAssetRequestedItem
		var attemptID, rootAttemptID sql.NullString
		var frozen derivedAssetFrozenExecution
		var attemptNo sql.NullInt32
		var requirementID, shotID, assetID sql.NullString
		var requirementSnapshot, shotSnapshot, assetSnapshot, promptSnapshot, referenceSnapshot []byte
		var modelSnapshot, capabilitySnapshot, requestSnapshot []byte
		var requirementHash, shotHash, assetHash, promptText, promptHash, referenceHash sql.NullString
		var modelProfileKey, accountID, modelID, modelHash, capabilityHash, requestHash sql.NullString
		if err := rows.Scan(
			&item.retrySourceRequestID, &item.rootRequestID, &item.originalID, &item.inputSnapshot, &item.inputHash,
			&attemptID, &rootAttemptID, &attemptNo,
			&requirementID, &shotID, &assetID,
			&requirementSnapshot, &requirementHash, &shotSnapshot, &shotHash, &assetSnapshot, &assetHash,
			&promptText, &promptSnapshot, &promptHash, &referenceSnapshot, &referenceHash,
			&modelProfileKey, &accountID, &modelID, &modelSnapshot, &modelHash,
			&capabilitySnapshot, &capabilityHash, &requestSnapshot, &requestHash,
		); err != nil {
			return nil, derivedAssetRetryLineage{}, 0, err
		}
		item.rawID = item.originalID
		if parsed, err := uuid.Parse(item.originalID); err == nil {
			item.lookupID = parsed.String()
		}
		if attemptID.Valid {
			frozen = derivedAssetFrozenExecution{
				requirementID: requirementID.String, storyboardShotID: shotID.String, canonicalAssetID: assetID.String,
				requirementSnapshot: requirementSnapshot, requirementHash: requirementHash.String,
				shotSnapshot: shotSnapshot, shotHash: shotHash.String,
				assetSnapshot: assetSnapshot, assetHash: assetHash.String,
				promptText: promptText.String, promptSnapshot: promptSnapshot, promptHash: promptHash.String,
				referenceSnapshot: referenceSnapshot, referenceHash: referenceHash.String,
				modelProfileKey: modelProfileKey.String, providerAccountID: accountID.String, providerModelID: modelID.String,
				modelSnapshot: modelSnapshot, modelHash: modelHash.String,
				capabilitySnapshot: capabilitySnapshot, capabilityHash: capabilityHash.String,
				requestSnapshot: requestSnapshot, requestHash: requestHash.String,
				rootAttemptID: rootAttemptID.String, retryOfAttemptID: attemptID.String, attemptNo: int(attemptNo.Int32) + 1,
			}
			item.retrySourceAttempt = &frozen
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, derivedAssetRetryLineage{}, 0, err
	}
	if len(items) == 0 {
		return nil, derivedAssetRetryLineage{}, 0, newAPIError(http.StatusConflict, "DERIVED_ASSET_RETRY_EMPTY", "原批次没有可重试的失败或阻塞项")
	}
	return items, derivedAssetRetryLineage{rootBatchID: rootBatchID, retryDepth: retryDepth + 1}, len(items), nil
}

func (s *Server) persistDerivedAssetBatchTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	run WorkflowRun,
	batchID string,
	options DerivedAssetBatchCreateOptions,
	requestHash string,
	requestedItems []derivedAssetRequestedItem,
	retryLineage derivedAssetRetryLineage,
	selectorCandidateCount int,
) (DerivedAssetBatchProjection, error) {
	filtersRaw, filtersHash := derivedAssetSnapshot(options.Filters)
	model, modelErr := loadDerivedAssetModelSelectionTx(ctx, tx, project)
	if modelErr != nil && !errors.Is(modelErr, pgx.ErrNoRows) {
		return DerivedAssetBatchProjection{}, modelErr
	}
	seen := make(map[string]string, len(requestedItems))
	prepared := make([]preparedDerivedAssetRequest, 0, len(requestedItems))
	selectorSkippedCount := 0
	for index, requested := range requestedItems {
		ordinal := index + 1
		requestItemID := uuid.NewString()
		entry := preparedDerivedAssetRequest{requestItemID: requestItemID, ordinal: ordinal, requested: requested}
		dedupeKey := requested.originalID
		if requested.lookupID != "" {
			dedupeKey = requested.lookupID
		}
		if duplicateOf, ok := seen[dedupeKey]; ok {
			entry.duplicateOf = duplicateOf
			entry.disposition = derivedAssetDisposition{
				name: "duplicate", status: "skipped",
				detail: mustRawJSON(map[string]any{"duplicateOfRequestItemId": duplicateOf}),
			}
		} else {
			seen[dedupeKey] = requestItemID
			disposition, err := s.classifyDerivedAssetRequestTx(
				ctx, tx, project, run, requestItemID, requested, options.Force, model, modelErr,
			)
			if err != nil {
				return DerivedAssetBatchProjection{}, err
			}
			entry.disposition = disposition
		}
		if options.Mode == derivedAssetBatchModeSelectAll && entry.disposition.name != "executable" {
			selectorSkippedCount++
		}
		prepared = append(prepared, entry)
	}
	var rootBatchID, retryOfBatchID any
	if options.Mode == derivedAssetBatchModeRetry {
		rootBatchID = retryLineage.rootBatchID
		retryOfBatchID = options.RetryOfBatchID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO derived_asset_batches(
			id, organization_id, project_id, workflow_run_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			root_batch_id, retry_of_batch_id, retry_depth, request_mode, filters, filters_hash,
			selector_candidate_count, selector_skipped_count, idempotency_key, request_hash,
			status, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, $10, $11, $12, $13,
			$14, $15, $16, $17, 'prepared', $18
		)
	`, batchID, project.OrganizationID, project.ID, run.ID, run.ProductionGenerationID,
		run.VideoProductionBindingID, run.VideoProductionBindingRevision,
		rootBatchID, retryOfBatchID, retryLineage.retryDepth, options.Mode, filtersRaw, filtersHash,
		selectorCandidateCount, selectorSkippedCount, options.IdempotencyKey, requestHash, principal.UserID); err != nil {
		return DerivedAssetBatchProjection{}, err
	}

	terminalNodes := 0
	failedNodes := 0
	for _, entry := range prepared {
		if err := insertDerivedAssetRequestAndNodeTx(
			ctx, tx, project, run, batchID, entry.requestItemID, entry.ordinal,
			entry.requested, entry.duplicateOf, entry.disposition,
		); err != nil {
			return DerivedAssetBatchProjection{}, err
		}
		if entry.disposition.name == "executable" {
			if err := insertDerivedAssetExecutionAttemptTx(ctx, tx, project, run, batchID, entry.requestItemID, entry.disposition.frozen); err != nil {
				return DerivedAssetBatchProjection{}, err
			}
		} else {
			terminalNodes++
			if entry.disposition.status == "blocked" {
				failedNodes++
			}
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE derived_asset_batches
		SET status = 'queued', revision = revision + 1
		WHERE id = $1
	`, batchID); err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	if err := insertAPIEvent(
		ctx,
		tx,
		project.OrganizationID,
		project.ID,
		"derived_asset.batch.created",
		"derived_asset_batch",
		batchID,
		mustRawJSON(map[string]any{
			"batchId": batchID, "workflowRunId": run.ID, "requestMode": options.Mode,
			"totalItems": len(requestedItems), "productionGenerationId": run.ProductionGenerationID,
		}),
	); err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	workflowOptions := workflows.DerivedAssetBatchOptions{
		BatchID: batchID, MaxConcurrency: options.MaxConcurrency, Force: options.Force,
	}
	startInput := workflows.TextToStoryboardInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		WorkflowRunID:  run.ID,
		Prompt:         derivedAssetBatchWorkflowType,
		CreatedBy:      principal.UserID,
		Input:          mustRawJSON(workflowOptions),
	}
	rootWorkflowRunID := run.ID
	retryWorkflowRunID := ""
	if options.Mode == derivedAssetBatchModeRetry {
		if err := tx.QueryRow(ctx, `SELECT workflow_run_id::text FROM derived_asset_batches WHERE id = $1`, retryLineage.rootBatchID).Scan(&rootWorkflowRunID); err != nil {
			return DerivedAssetBatchProjection{}, err
		}
		if err := tx.QueryRow(ctx, `SELECT workflow_run_id::text FROM derived_asset_batches WHERE id = $1`, options.RetryOfBatchID).Scan(&retryWorkflowRunID); err != nil {
			return DerivedAssetBatchProjection{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET input = $2, total_items = $3, completed_items = $4, failed_items = $5,
		    root_workflow_run_id = $6, retry_of_workflow_run_id = NULLIF($7, '')::uuid,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, run.ID, mustRawJSON(startInput), len(requestedItems), terminalNodes-failedNodes, failedNodes, rootWorkflowRunID, retryWorkflowRunID); err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	snapshotRaw, snapshotHash, err := marshalWorkflowStartInput(startInput)
	if err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_input_snapshots(
			workflow_run_id, organization_id, project_id, project_revision,
			snapshot, snapshot_hash, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, run.ID, project.OrganizationID, project.ID, project.Revision, snapshotRaw, snapshotHash, run.ProductionGenerationID); err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	return loadDerivedAssetBatchProjectionTx(ctx, tx, project.OrganizationID, project.ID, batchID)
}

func (s *Server) classifyDerivedAssetRequestTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	run WorkflowRun,
	requestItemID string,
	requested derivedAssetRequestedItem,
	force bool,
	model *derivedAssetModelSelection,
	modelErr error,
) (derivedAssetDisposition, error) {
	if requested.lookupID == "" {
		return blockedDerivedAssetDisposition("not_found", "DERIVED_ASSET_REQUIREMENT_NOT_FOUND", "镜头衍生资产需求不存在", false), nil
	}
	candidate, err := loadDerivedAssetCandidateTx(ctx, tx, project.ID, requested.lookupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return blockedDerivedAssetDisposition("not_found", "DERIVED_ASSET_REQUIREMENT_NOT_FOUND", "镜头衍生资产需求不存在", false), nil
	}
	if err != nil {
		return derivedAssetDisposition{}, err
	}
	if candidate.generationID != run.ProductionGenerationID {
		return blockedDerivedAssetDisposition("generation_mismatch", "PRODUCTION_GENERATION_MISMATCH", "镜头衍生资产需求不属于当前视频生产代", false), nil
	}
	if candidate.activeExecution || candidate.status == "image_running" {
		return blockedDerivedAssetDisposition("already_running", "DERIVED_ASSET_ALREADY_RUNNING", "镜头衍生资产图片正在生成", true), nil
	}
	if candidate.shotDeletedAt.Valid || candidate.asset.Status == "archived" || candidate.status == "skipped" {
		return derivedAssetDisposition{name: "skipped", status: "skipped", candidate: candidate, detail: mustRawJSON(map[string]any{"reason": "source_not_active"})}, nil
	}
	if candidate.status == "image_succeeded" && !force {
		return derivedAssetDisposition{name: "skipped", status: "skipped", candidate: candidate, detail: mustRawJSON(map[string]any{"reason": "already_succeeded"})}, nil
	}
	if candidate.reviewStatus != "approved" {
		return blockedDerivedAssetDisposition("review_required", "SHOT_ASSET_REQUIREMENT_REVIEW_REQUIRED", "镜头衍生资产需求尚未审核通过", true), nil
	}
	if !canonicalAssetHasPrimaryReference(candidate.asset) {
		return blockedDerivedAssetDisposition("review_required", "DERIVED_ASSET_BASE_REFERENCE_REQUIRED", "基础资产没有可用参考图", true), nil
	}
	if errors.Is(modelErr, pgx.ErrNoRows) || model == nil {
		return blockedDerivedAssetDisposition("review_required", "MODEL_PROFILE_NOT_CONFIGURED", "图片业务模型尚未配置可用模型", true), nil
	}
	gatewayIdempotencyKey := ""
	if requested.retrySourceAttempt != nil {
		var parentRequest provider.GatewayImageRequest
		if err := json.Unmarshal(requested.retrySourceAttempt.requestSnapshot, &parentRequest); err != nil {
			return derivedAssetDisposition{}, err
		}
		gatewayIdempotencyKey = firstNonEmpty(parentRequest.IdempotencyKey, parentRequest.Options.IdempotencyKey)
	}
	frozen, err := s.freezeDerivedAssetExecutionTx(ctx, tx, project, run, requestItemID, gatewayIdempotencyKey, candidate, model)
	if err != nil {
		var promptErr promptsvc.Error
		if errors.As(err, &promptErr) {
			return blockedDerivedAssetDisposition("review_required", promptErr.Code, "镜头衍生资产提示词尚未就绪", true), nil
		}
		return derivedAssetDisposition{}, err
	}
	if requested.retrySourceAttempt != nil {
		if !derivedAssetFrozenIdentityMatches(frozen, requested.retrySourceAttempt) {
			return blockedDerivedAssetDisposition("review_required", "DERIVED_ASSET_SOURCE_CHANGED", "生成输入已变化，请重新创建生成任务", false), nil
		}
		frozen.rootAttemptID = requested.retrySourceAttempt.rootAttemptID
		frozen.retryOfAttemptID = requested.retrySourceAttempt.retryOfAttemptID
		frozen.attemptNo = requested.retrySourceAttempt.attemptNo
	}
	return derivedAssetDisposition{
		name: "executable", status: "pending", candidate: candidate, frozen: frozen,
		detail: mustRawJSON(map[string]any{"requirementId": candidate.requirementID, "shotNo": candidate.shotNo}),
	}, nil
}

func blockedDerivedAssetDisposition(name, code, message string, retryable bool) derivedAssetDisposition {
	return derivedAssetDisposition{
		name: name, status: "blocked", errorCode: code, message: message, retryable: retryable,
		detail: mustRawJSON(map[string]any{"code": code}),
	}
}

func loadDerivedAssetCandidateTx(ctx context.Context, tx pgx.Tx, projectID, requirementID string) (*derivedAssetCandidate, error) {
	var item derivedAssetCandidate
	var metadata []byte
	err := tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, production_generation_id::text,
		       storyboard_shot_id::text, asset_id::text, requirement_type, role_in_shot, costume,
		       pose, expression, action, camera_relation, scene_state, prop_state, prompt,
		       status, review_status, stale_state, metadata, updated_at
		FROM shot_asset_requirements
		WHERE id = $1 AND project_id = $2
		FOR UPDATE
	`, requirementID, projectID).Scan(
		&item.requirementID, &item.organizationID, &item.projectID, &item.generationID,
		&item.storyboardShotID, &item.canonicalAssetID, &item.requirementType, &item.roleInShot,
		&item.costume, &item.pose, &item.expression, &item.action, &item.cameraRelation,
		&item.sceneState, &item.propState, &item.prompt, &item.status, &item.reviewStatus,
		&item.staleState, &metadata, &item.requirementUpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.metadata = rawOrDefaultBytes(metadata, "{}")
	var shotNo sql.NullInt64
	if err := tx.QueryRow(ctx, `
		SELECT deleted_at, updated_at, COALESCE(shot_no, shot_index + 1),
		       COALESCE(visual, ''), COALESCE(camera, ''), COALESCE(motion, ''), COALESCE(mood, '')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2
		FOR SHARE
	`, item.storyboardShotID, projectID).Scan(
		&item.shotDeletedAt, &item.shotUpdatedAt, &shotNo,
		&item.shotVisual, &item.shotCamera, &item.shotMotion, &item.shotMood,
	); err != nil {
		return nil, err
	}
	item.shotNo = int(shotNo.Int64)
	asset, err := scanCanonicalAsset(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_type, name, description, profile,
		       base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key,
		       lock_reference, reference_artifact_id, reference_media_file_id, reference_storage_key,
		       status, review_status, manual_override, stale_state, edited_by, edited_at,
		       source_script_ids, metadata, created_by, created_at, updated_at, revision, prompt_revision
		FROM canonical_assets
		WHERE id = $1 AND project_id = $2
		FOR SHARE
	`, item.canonicalAssetID, projectID))
	if err != nil {
		return nil, err
	}
	item.asset = asset
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM derived_asset_execution_items execution
			WHERE execution.project_id = $1
			  AND execution.requirement_id = $2
			  AND execution.status IN (
				'prepared', 'queued', 'leased', 'provider_running', 'transferring',
				'committing', 'unknown_outcome'
			  )
		)
	`, projectID, requirementID).Scan(&item.activeExecution); err != nil {
		return nil, err
	}
	return &item, nil
}

func loadDerivedAssetModelSelectionTx(ctx context.Context, tx pgx.Tx, project Project) (*derivedAssetModelSelection, error) {
	var item derivedAssetModelSelection
	var model workflows.DerivedAssetModelSnapshot
	var capability workflows.DerivedAssetCapabilitySnapshot
	var modelUpdatedAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT account.id::text, model.id::text, profile.profile_key,
		       model.model_key, model.modality, model.status, model.updated_at,
		       capability.task_types, capability.input_limits, capability.output_limits,
		       capability.quality_tiers, capability.provider_options_schema, capability.pricing_policy
		FROM model_profiles profile
		JOIN model_profile_bindings binding ON binding.model_profile_id = profile.id AND binding.enabled = true
		JOIN provider_models model ON model.id = binding.provider_model_id AND model.status = 'active'
		JOIN provider_accounts account ON account.id = model.provider_account_id AND account.status = 'active'
		JOIN LATERAL (
		  SELECT c.task_types, c.input_limits, c.output_limits, c.quality_tiers,
		         c.provider_options_schema, c.pricing_policy
		  FROM provider_model_capabilities c
		  WHERE c.provider_model_id = model.id
		  ORDER BY c.created_at DESC, c.id DESC
		  LIMIT 1
		) capability ON true
		WHERE profile.organization_id = $1
		  AND profile.profile_key = $2
		  AND model.modality IN ('image', 'multimodal')
		ORDER BY binding.priority ASC, binding.weight DESC, binding.created_at, binding.id
		LIMIT 1
	`, project.OrganizationID, project.ImageModelProfileKey).Scan(
		&model.ProviderAccountID, &model.ProviderModelID, &model.ModelProfileKey,
		&model.ModelKey, &model.Modality, &model.Status, &modelUpdatedAt,
		&capability.TaskTypes, &capability.InputLimits, &capability.OutputLimits,
		&capability.QualityTiers, &capability.ProviderOptionsSchema, &capability.PricingPolicy,
	)
	if err != nil {
		return nil, err
	}
	model.UpdatedAt = modelUpdatedAt.UTC().Format(time.RFC3339Nano)
	item.providerAccountID = model.ProviderAccountID
	item.providerModelID = model.ProviderModelID
	item.modelProfileKey = model.ModelProfileKey
	item.modelSnapshot, item.modelHash = derivedAssetSnapshot(model)
	item.capabilitySnapshot, item.capabilityHash = derivedAssetSnapshot(capability)
	return &item, nil
}

func (s *Server) freezeDerivedAssetExecutionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	run WorkflowRun,
	requestItemID string,
	gatewayIdempotencyKey string,
	candidate *derivedAssetCandidate,
	model *derivedAssetModelSelection,
) (*derivedAssetFrozenExecution, error) {
	requirement := workflows.DerivedAssetRequirementSnapshot{
		ID:                     candidate.requirementID,
		ProjectID:              candidate.projectID,
		ProductionGenerationID: candidate.generationID,
		StoryboardShotID:       candidate.storyboardShotID,
		CanonicalAssetID:       candidate.canonicalAssetID,
		ReviewStatus:           candidate.reviewStatus,
		Status:                 candidate.status,
		Prompt:                 candidate.prompt.String,
		UpdatedAt:              candidate.requirementUpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	requirementSnapshot, requirementHash := derivedAssetSnapshot(requirement)
	shot := workflows.DerivedAssetStoryboardShotSnapshot{
		ID:                     candidate.storyboardShotID,
		ProjectID:              candidate.projectID,
		ProductionGenerationID: candidate.generationID,
		ShotNo:                 candidate.shotNo,
		UpdatedAt:              candidate.shotUpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if candidate.shotDeletedAt.Valid {
		shot.DeletedAt = candidate.shotDeletedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	shotSnapshot, shotHash := derivedAssetSnapshot(shot)
	asset := workflows.DerivedAssetCanonicalAssetSnapshot{
		ID:                           candidate.asset.ID,
		ProjectID:                    candidate.asset.ProjectID,
		Status:                       candidate.asset.Status,
		Revision:                     candidate.asset.Revision,
		PromptRevision:               candidate.asset.PromptRevision,
		UpdatedAt:                    candidate.asset.UpdatedAt.UTC().Format(time.RFC3339Nano),
		PrimaryReferenceArtifactID:   stringValue(candidate.asset.PrimaryReferenceArtifactID),
		PrimaryReferenceMediaFileID:  stringValue(candidate.asset.PrimaryReferenceMediaFileID),
		PrimaryReferenceStorageKey:   stringValue(candidate.asset.PrimaryReferenceStorageKey),
		FallbackReferenceArtifactID:  stringValue(candidate.asset.ReferenceArtifactID),
		FallbackReferenceMediaFileID: stringValue(candidate.asset.ReferenceMediaFileID),
		FallbackReferenceStorageKey:  stringValue(candidate.asset.ReferenceStorageKey),
	}
	assetSnapshot, assetHash := derivedAssetSnapshot(asset)
	rendered, components, err := renderDerivedAssetPromptTx(ctx, tx, project, candidate)
	if err != nil {
		return nil, err
	}
	promptSnapshot, promptHash := derivedAssetSnapshot(workflows.DerivedAssetPromptSnapshot{
		TemplateKey: rendered.TemplateKey,
		VersionID:   rendered.PromptVersionID,
		Hash:        rendered.RenderedHash,
		Source:      rendered.Source,
		Text:        rendered.RenderedText,
	})
	_ = components
	references := derivedAssetImageReferences(candidate.asset)
	referenceSnapshot, referenceHash := derivedAssetSnapshot(workflows.DerivedAssetReferenceSnapshot{Items: references})
	if gatewayIdempotencyKey == "" {
		gatewayIdempotencyKey = "derived-asset-request:" + requestItemID
	}
	gatewayRequest := provider.GatewayImageRequest{
		OrganizationID:    project.OrganizationID,
		WorkspaceID:       project.WorkspaceID,
		ProjectID:         project.ID,
		WorkflowRunID:     run.ID,
		ModelProfileKey:   model.modelProfileKey,
		ProviderModelID:   model.providerModelID,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		IdempotencyKey:    gatewayIdempotencyKey,
		Input: mustRawJSON(map[string]any{
			"prompt": rendered.RenderedText, "size": "1024x1024", "n": 1, "quality": project.ImageQuality,
		}),
		References: references,
		Options:    provider.GatewayImageOptions{IdempotencyKey: gatewayIdempotencyKey, Retry: true},
	}
	requestSnapshot, _ := derivedAssetSnapshot(gatewayRequest)
	requestHash := derivedAssetLogicalProviderRequestHash(gatewayRequest)
	return &derivedAssetFrozenExecution{
		requirementID: candidate.requirementID, storyboardShotID: candidate.storyboardShotID,
		canonicalAssetID:    candidate.canonicalAssetID,
		requirementSnapshot: requirementSnapshot, requirementHash: requirementHash,
		shotSnapshot: shotSnapshot, shotHash: shotHash,
		assetSnapshot: assetSnapshot, assetHash: assetHash,
		promptText: rendered.RenderedText, promptSnapshot: promptSnapshot, promptHash: promptHash,
		referenceSnapshot: referenceSnapshot, referenceHash: referenceHash,
		modelProfileKey: model.modelProfileKey, providerAccountID: model.providerAccountID,
		providerModelID: model.providerModelID, modelSnapshot: model.modelSnapshot, modelHash: model.modelHash,
		capabilitySnapshot: model.capabilitySnapshot, capabilityHash: model.capabilityHash,
		requestSnapshot: requestSnapshot, requestHash: requestHash,
		attemptNo: 1,
	}, nil
}

func renderDerivedAssetPromptTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	candidate *derivedAssetCandidate,
) (promptsvc.RenderedPrompt, map[string]any, error) {
	resolved, err := promptsvc.NewService(tx).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    "derived_asset_image_prompt",
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, nil, err
	}
	requirement := ShotAssetRequirement{
		RequirementType: candidate.requirementType,
		RoleInShot:      stringPtrFromNull(candidate.roleInShot), Costume: stringPtrFromNull(candidate.costume),
		Pose: stringPtrFromNull(candidate.pose), Expression: stringPtrFromNull(candidate.expression),
		Action: stringPtrFromNull(candidate.action), CameraRelation: stringPtrFromNull(candidate.cameraRelation),
		SceneState: stringPtrFromNull(candidate.sceneState), PropState: stringPtrFromNull(candidate.propState),
	}
	shot := StoryboardShot{ShotNo: candidate.shotNo, Visual: candidate.shotVisual, Camera: candidate.shotCamera, Motion: candidate.shotMotion, Mood: candidate.shotMood}
	rendered, err := promptsvc.Render(resolved, map[string]any{
		"project":     projectImagePromptVariables(project),
		"baseAsset":   map[string]any{"name": candidate.asset.Name, "description": candidate.asset.Description},
		"shot":        map[string]any{"summary": shotSummary(shot)},
		"requirement": map[string]any{"summary": requirementSummary(requirement)},
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, nil, err
	}
	components := map[string]any{"base": map[string]any{"templateKey": resolved.TemplateKey, "versionId": resolved.VersionID, "contentHash": resolved.ContentHash}}
	style := assetprompts.ToonflowStyleSlug(project.ArtStyle)
	suffix := assetprompts.ToonflowVisualTemplateSuffix(candidate.asset.AssetType, true)
	if style != "" && suffix != "" {
		prefixKey := "toonflow_visual_" + style + "_prefix"
		targetKey := "toonflow_visual_" + style + "_" + suffix
		prefix, prefixOK, err := systemPromptContentTx(ctx, tx, prefixKey)
		if err != nil {
			return promptsvc.RenderedPrompt{}, nil, err
		}
		target, targetOK, err := systemPromptContentTx(ctx, tx, targetKey)
		if err != nil {
			return promptsvc.RenderedPrompt{}, nil, err
		}
		if prefixOK && targetOK {
			prefix = assetprompts.RuntimeManualSummary(prefix, assetprompts.RuntimeToonflowPrefixMaxRunes)
			target = assetprompts.RuntimeManualSummary(target, assetprompts.RuntimeToonflowTemplateMaxRunes)
			toonflowPrompt := strings.TrimSpace(strings.Join(compactStrings([]string{prefix, target}), "\n\n"))
			if toonflowPrompt != "" {
				rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + "\n\n" + toonflowPrompt
				rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
				rendered.Source = firstNonEmpty(rendered.Source, "system_active") + "+toonflow_visual_compact"
				components["toonflow"] = map[string]any{"style": style, "prefixTemplateKey": prefixKey, "targetTemplateKey": targetKey}
			}
		}
	}
	rendered = withRuntimeImagePromptLimit(rendered)
	return rendered, components, nil
}

func systemPromptContentTx(ctx context.Context, tx pgx.Tx, templateKey string) (string, bool, error) {
	var content string
	err := tx.QueryRow(ctx, `
		SELECT pv.content
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
		WHERE pt.organization_id IS NULL
		  AND pt.template_key = $1
		  AND pt.status = 'active'
		  AND pv.status = 'active'
		ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
		LIMIT 1
	`, templateKey).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return content, err == nil, err
}

func derivedAssetFrozenIdentityMatches(current, previous *derivedAssetFrozenExecution) bool {
	if current == nil || previous == nil {
		return false
	}
	return current.requirementHash == previous.requirementHash &&
		current.shotHash == previous.shotHash &&
		current.assetHash == previous.assetHash &&
		current.promptHash == previous.promptHash &&
		current.referenceHash == previous.referenceHash &&
		current.modelHash == previous.modelHash &&
		current.capabilityHash == previous.capabilityHash &&
		current.requestHash == previous.requestHash
}

func insertDerivedAssetRequestAndNodeTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	run WorkflowRun,
	batchID, requestItemID string,
	ordinal int,
	requested derivedAssetRequestedItem,
	duplicateOf string,
	disposition derivedAssetDisposition,
) error {
	var requirementID any
	if disposition.candidate != nil {
		requirementID = disposition.candidate.requirementID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO derived_asset_request_items(
			id, batch_id, organization_id, project_id, input_ordinal, original_id,
			requirement_id, duplicate_of_request_item_id, root_request_item_id,
			retry_of_request_item_id, disposition, disposition_detail, error_code,
			error_message, retryable, input_snapshot, input_hash, status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid,
			NULLIF($10, '')::uuid, $11, $12, NULLIF($13, ''),
			NULLIF($14, ''), $15, $16, $17, $18
		)
	`, requestItemID, batchID, project.OrganizationID, project.ID, ordinal, requested.originalID,
		requirementID, duplicateOf, requested.rootRequestID, requested.retrySourceRequestID,
		disposition.name, rawOrDefaultBytes(disposition.detail, "{}"), disposition.errorCode,
		disposition.message, disposition.retryable, requested.inputSnapshot, requested.inputHash, disposition.status); err != nil {
		return err
	}
	nodeKey := derivedAssetNodeKey(batchID, requestItemID, ordinal)
	nodeStatus := "queued"
	var completedAt any
	if disposition.status == "blocked" {
		nodeStatus = "failed"
		completedAt = time.Now().UTC()
	} else if disposition.status == "skipped" {
		nodeStatus = "skipped"
		completedAt = time.Now().UTC()
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type,
			status, input, output, error_code, error_message, completed_at,
			attempt_generation, production_generation_id
		)
		VALUES ($1, $2, $3, $4, 'derived_asset.image.generate', $5, $6, $7,
		        NULLIF($8, ''), NULLIF($9, ''), $10, $11, $12)
	`, project.OrganizationID, project.ID, run.ID, nodeKey, nodeStatus,
		mustRawJSON(map[string]any{
			"batchId": batchID, "requestItemId": requestItemID, "inputOrdinal": ordinal,
			"originalId": requested.originalID, "requirementId": requirementID,
		}),
		mustRawJSON(map[string]any{"disposition": disposition.name, "detail": json.RawMessage(rawOrDefaultBytes(disposition.detail, "{}"))}),
		disposition.errorCode, disposition.message, completedAt, run.AttemptGeneration, run.ProductionGenerationID)
	return err
}

func insertDerivedAssetExecutionAttemptTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	run WorkflowRun,
	batchID, requestItemID string,
	frozen *derivedAssetFrozenExecution,
) error {
	if frozen == nil {
		return errors.New("derived asset execution snapshot is required")
	}
	var nodeRunID, nodeKey string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, node_key
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND input->>'requestItemId' = $2
		  AND status = 'queued'
	`, run.ID, requestItemID).Scan(&nodeRunID, &nodeKey); err != nil {
		return err
	}
	attemptNo := frozen.attemptNo
	if attemptNo <= 0 {
		attemptNo = 1
	}
	executionID := uuid.NewString()
	internalIdempotencyKey := fmt.Sprintf("derived-asset-execution:%s:%d", requestItemID, attemptNo)
	_, err := tx.Exec(ctx, `
		INSERT INTO derived_asset_execution_items(
			id, batch_id, request_item_id, organization_id, project_id, workflow_run_id,
			node_run_id, node_key, production_generation_id, video_production_binding_id,
			video_production_binding_revision, identity_version, root_attempt_id, retry_of_attempt_id,
			attempt_no, requirement_id, storyboard_shot_id, canonical_asset_id,
			requirement_snapshot, requirement_snapshot_hash,
			storyboard_shot_snapshot, storyboard_shot_snapshot_hash,
			canonical_asset_snapshot, canonical_asset_snapshot_hash,
			prompt_text, prompt_snapshot, prompt_hash,
			reference_snapshot, reference_snapshot_hash,
			model_profile_key, provider_account_id, provider_model_id,
			model_snapshot, model_snapshot_hash, capability_snapshot, capability_snapshot_hash,
			request_snapshot, request_hash, idempotency_key, status, queued_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, 2, NULLIF($12, '')::uuid, NULLIF($13, '')::uuid,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
			$24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35,
			$36, $37, $38, 'queued', now()
		)
	`, executionID, batchID, requestItemID, project.OrganizationID, project.ID, run.ID,
		nodeRunID, nodeKey, run.ProductionGenerationID, run.VideoProductionBindingID,
		run.VideoProductionBindingRevision, frozen.rootAttemptID, frozen.retryOfAttemptID,
		attemptNo, frozen.requirementID, frozen.storyboardShotID, frozen.canonicalAssetID,
		frozen.requirementSnapshot, frozen.requirementHash,
		frozen.shotSnapshot, frozen.shotHash,
		frozen.assetSnapshot, frozen.assetHash,
		frozen.promptText, frozen.promptSnapshot, frozen.promptHash,
		frozen.referenceSnapshot, frozen.referenceHash,
		frozen.modelProfileKey, frozen.providerAccountID, frozen.providerModelID,
		frozen.modelSnapshot, frozen.modelHash, frozen.capabilitySnapshot, frozen.capabilityHash,
		frozen.requestSnapshot, frozen.requestHash, internalIdempotencyKey)
	return err
}

func derivedAssetNodeKey(batchID, requestItemID string, ordinal int) string {
	return fmt.Sprintf("derived-asset/%s/request/%06d/%s", batchID, ordinal, requestItemID)
}

func derivedAssetImageReferences(asset CanonicalAsset) []provider.GatewayImageReference {
	artifactID := firstNonEmpty(stringValue(asset.PrimaryReferenceArtifactID), stringValue(asset.ReferenceArtifactID))
	storageKey := firstNonEmpty(stringValue(asset.PrimaryReferenceStorageKey), stringValue(asset.ReferenceStorageKey))
	if artifactID == "" && storageKey == "" {
		return nil
	}
	return []provider.GatewayImageReference{{
		Type:       "image",
		AssetID:    asset.ID,
		ArtifactID: artifactID,
		StorageKey: storageKey,
		Metadata: mustRawJSON(map[string]any{
			"source": "derived_asset_base_reference",
			"isPrimary": stringValue(asset.PrimaryReferenceArtifactID) != "" ||
				stringValue(asset.PrimaryReferenceStorageKey) != "",
		}),
	}}
}

func derivedAssetSnapshot(value any) (json.RawMessage, string) {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err == nil {
		raw, err = json.Marshal(canonical)
		if err != nil {
			panic(err)
		}
	}
	return raw, workflows.HashDerivedAssetSnapshot(value)
}

func derivedAssetLogicalProviderRequestHash(request provider.GatewayImageRequest) string {
	request.WorkflowRunID = ""
	request.NodeRunID = ""
	return workflows.HashDerivedAssetSnapshot(request)
}

// derivedAssetBatchProjectionByWorkflowRun is the route-facing projection
// helper. Organization and project are mandatory to avoid cross-tenant leaks.
func (s *Server) derivedAssetBatchProjectionByWorkflowRun(
	ctx context.Context,
	organizationID, projectID, workflowRunID string,
) (DerivedAssetBatchProjection, error) {
	var batchID string
	err := s.db.QueryRow(ctx, `
		SELECT id::text
		FROM derived_asset_batches
		WHERE organization_id = $1 AND project_id = $2 AND workflow_run_id = $3
	`, organizationID, projectID, workflowRunID).Scan(&batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DerivedAssetBatchProjection{}, newAPIError(http.StatusNotFound, "NOT_FOUND", "derived asset batch was not found")
	}
	if err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	return loadDerivedAssetBatchProjectionTx(ctx, s.db, organizationID, projectID, batchID)
}

func (s *Server) derivedAssetBatchProjection(
	ctx context.Context,
	organizationID, projectID, batchID string,
) (DerivedAssetBatchProjection, error) {
	return loadDerivedAssetBatchProjectionTx(ctx, s.db, organizationID, projectID, batchID)
}

type derivedAssetProjectionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadDerivedAssetBatchProjectionTx(
	ctx context.Context,
	db derivedAssetProjectionQuerier,
	organizationID, projectID, batchID string,
) (DerivedAssetBatchProjection, error) {
	var item DerivedAssetBatchProjection
	var rootBatchID, retryOfBatchID, errorCode, errorMessage, createdBy sql.NullString
	var startedAt, completedAt sql.NullTime
	var filters []byte
	err := db.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, workflow_run_id::text,
		       production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision, root_batch_id::text, retry_of_batch_id::text,
		       retry_depth, request_mode, filters, filters_hash, selector_candidate_count,
		       selector_skipped_count, idempotency_key, request_hash, status, revision,
		       total_items, executable_items, review_required_items, not_found_items,
		       generation_mismatch_items, already_running_items, duplicate_items, skipped_items,
		       pending_items, queued_items, running_items, succeeded_items,
		       failed_retryable_items, failed_terminal_items, cancelled_items, discarded_items,
		       error_code, error_message, created_by::text, created_at, updated_at, started_at, completed_at
		FROM derived_asset_batches
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`, batchID, organizationID, projectID).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID,
		&item.ProductionGenerationID, &item.VideoProductionBindingID,
		&item.VideoProductionBindingRevision, &rootBatchID, &retryOfBatchID,
		&item.RetryDepth, &item.RequestMode, &filters, &item.FiltersHash,
		&item.SelectorCandidateCount, &item.SelectorSkippedCount, &item.IdempotencyKey,
		&item.RequestHash, &item.Status, &item.Revision, &item.TotalItems,
		&item.ExecutableItems, &item.ReviewRequiredItems, &item.NotFoundItems,
		&item.GenerationMismatchItems, &item.AlreadyRunningItems, &item.DuplicateItems,
		&item.SkippedItems, &item.PendingItems, &item.QueuedItems, &item.RunningItems,
		&item.SucceededItems, &item.FailedRetryableItems, &item.FailedTerminalItems,
		&item.CancelledItems, &item.DiscardedItems, &errorCode, &errorMessage, &createdBy,
		&item.CreatedAt, &item.UpdatedAt, &startedAt, &completedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DerivedAssetBatchProjection{}, newAPIError(http.StatusNotFound, "NOT_FOUND", "derived asset batch was not found")
	}
	if err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	item.RootBatchID = stringPtrFromNull(rootBatchID)
	item.RetryOfBatchID = stringPtrFromNull(retryOfBatchID)
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.Filters = rawOrDefaultBytes(filters, "{}")
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	rows, err := db.Query(ctx, `
		SELECT request.id::text, request.input_ordinal, request.original_id::text,
		       request.requirement_id::text, request.duplicate_of_request_item_id::text,
		       request.root_request_item_id::text, request.retry_of_request_item_id::text,
		       request.disposition, request.disposition_detail, request.error_code,
		       request.error_message, request.retryable, request.input_snapshot, request.input_hash,
		       request.status, request.current_attempt_id::text, request.current_attempt_no,
		       request.revision, request.created_at, request.updated_at,
		       execution.id::text, execution.node_run_id::text, execution.node_key,
		       execution.attempt_no, execution.status, execution.revision,
		       execution.provider_request_id::text, execution.provider_call_id::text,
		       execution.selected_credential_id::text, execution.artifact_id::text,
		       execution.media_file_id::text, execution.storage_key,
		       execution.error_code, execution.error_message, execution.diagnostic,
		       execution.late_result_count, execution.late_result_diagnostics,
		       execution.created_at, execution.started_at, execution.completed_at,
		       execution.production_generation_id::text
		FROM derived_asset_request_items request
		LEFT JOIN derived_asset_execution_items execution ON execution.id = request.current_attempt_id
		WHERE request.batch_id = $1
		ORDER BY request.input_ordinal
	`, batchID)
	if err != nil {
		return DerivedAssetBatchProjection{}, err
	}
	defer rows.Close()
	item.Items = make([]DerivedAssetRequestItemProjection, 0, item.TotalItems)
	for rows.Next() {
		var request DerivedAssetRequestItemProjection
		var requirementID, duplicateID, rootID, retryID, requestErrorCode, requestErrorMessage sql.NullString
		var currentAttemptID sql.NullString
		var currentAttemptNo sql.NullInt32
		var dispositionDetail, inputSnapshot []byte
		var executionID, nodeRunID, nodeKey, executionStatus sql.NullString
		var executionAttemptNo sql.NullInt32
		var executionRevision sql.NullInt64
		var providerRequestID, providerCallID, credentialID, artifactID, mediaFileID, storageKey sql.NullString
		var executionErrorCode, executionErrorMessage sql.NullString
		var diagnostic, lateDiagnostics []byte
		var lateCount sql.NullInt32
		var executionCreatedAt, executionStartedAt, executionCompletedAt sql.NullTime
		var executionGenerationID sql.NullString
		if err := rows.Scan(
			&request.ID, &request.InputOrdinal, &request.OriginalID, &requirementID,
			&duplicateID, &rootID, &retryID, &request.Disposition, &dispositionDetail,
			&requestErrorCode, &requestErrorMessage, &request.Retryable, &inputSnapshot,
			&request.InputHash, &request.Status, &currentAttemptID, &currentAttemptNo,
			&request.Revision, &request.CreatedAt, &request.UpdatedAt,
			&executionID, &nodeRunID, &nodeKey, &executionAttemptNo, &executionStatus,
			&executionRevision, &providerRequestID, &providerCallID, &credentialID,
			&artifactID, &mediaFileID, &storageKey, &executionErrorCode,
			&executionErrorMessage, &diagnostic, &lateCount, &lateDiagnostics,
			&executionCreatedAt, &executionStartedAt, &executionCompletedAt,
			&executionGenerationID,
		); err != nil {
			return DerivedAssetBatchProjection{}, err
		}
		request.RequirementID = stringPtrFromNull(requirementID)
		request.DuplicateOfRequestItemID = stringPtrFromNull(duplicateID)
		request.RootRequestItemID = stringPtrFromNull(rootID)
		request.RetryOfRequestItemID = stringPtrFromNull(retryID)
		request.ErrorCode = stringPtrFromNull(requestErrorCode)
		request.ErrorMessage = stringPtrFromNull(requestErrorMessage)
		request.CurrentAttemptID = stringPtrFromNull(currentAttemptID)
		if currentAttemptNo.Valid {
			value := int(currentAttemptNo.Int32)
			request.CurrentAttemptNo = &value
		}
		request.DispositionDetail = rawOrDefaultBytes(dispositionDetail, "{}")
		request.InputSnapshot = rawOrDefaultBytes(inputSnapshot, "{}")
		if executionID.Valid {
			execution := &DerivedAssetExecutionProjection{
				ID: executionID.String, NodeRunID: nodeRunID.String, NodeKey: nodeKey.String,
				AttemptNo: int(executionAttemptNo.Int32), Status: executionStatus.String,
				Revision:          executionRevision.Int64,
				ProviderRequestID: stringPtrFromNull(providerRequestID), ProviderCallID: stringPtrFromNull(providerCallID),
				SelectedCredentialID: stringPtrFromNull(credentialID), ArtifactID: stringPtrFromNull(artifactID),
				MediaFileID: stringPtrFromNull(mediaFileID), StorageKey: stringPtrFromNull(storageKey),
				ErrorCode: stringPtrFromNull(executionErrorCode), ErrorMessage: stringPtrFromNull(executionErrorMessage),
				Diagnostic: rawOrDefaultBytes(diagnostic, "{}"), LateResultCount: int(lateCount.Int32),
				LateResultDiagnostics: rawOrDefaultBytes(lateDiagnostics, "[]"), CreatedAt: executionCreatedAt.Time,
				ProductionGenerationID: executionGenerationID.String,
			}
			if executionStartedAt.Valid {
				execution.StartedAt = &executionStartedAt.Time
			}
			if executionCompletedAt.Valid {
				execution.CompletedAt = &executionCompletedAt.Time
			}
			request.Execution = execution
		}
		item.Items = append(item.Items, request)
	}
	return item, rows.Err()
}
