package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDerivedAssetBatchAggregateApprovedAndReviewRequiredIsPartialIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	batchID := insertDerivedAssetBatch(t, ctx, base, "explicit", "", "", 0)

	executableID := insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: batchID, InputOrdinal: 1, OriginalID: uuid.NewString(),
		Disposition: "executable", Status: "pending", Retryable: true,
	})
	insertDerivedAssetTerminalAttempt(t, ctx, base, batchID, executableID, "succeeded", "")
	insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: batchID, InputOrdinal: 2, OriginalID: uuid.NewString(),
		Disposition: "review_required", Status: "blocked", Retryable: true,
		ErrorCode: "DERIVED_ASSET_REVIEW_REQUIRED", ErrorMessage: "资产需求尚未审核通过",
	})

	output, err := base.Activities.CompleteDerivedAssetBatchWorkflowV2(ctx, base.Input, batchID)
	if err != nil {
		t.Fatalf("CompleteDerivedAssetBatchWorkflowV2: %v", err)
	}
	if output.Status != "partial_succeeded" || output.TotalItems != 2 || output.ExecutableItems != 1 ||
		output.SucceededItems != 1 || output.ReviewRequiredItems != 1 || output.CompletedItems != 1 || output.FailedItems != 1 {
		t.Fatalf("aggregate = %+v", output)
	}
}

func TestDerivedAssetExecutionRejectsStaleIdentityBeforeProviderIntegration(t *testing.T) {
	t.Run("old generation", func(t *testing.T) {
		ctx := context.Background()
		base := seedDerivedAssetExecutionBase(t, ctx)
		fixture := seedDerivedAssetExecutable(t, ctx, base)
		gateway, calls := countingDerivedAssetGateway(t)
		base.Activities.gateway = gateway

		tx, err := base.Pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin generation reset: %v", err)
		}
		if _, _, err := videoproduction.ResetActiveGeneration(ctx, tx, base.ProjectID); err != nil {
			tx.Rollback(ctx)
			t.Fatalf("ResetActiveGeneration: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit generation reset: %v", err)
		}

		lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-old-generation")
		if err == nil {
			_, _ = base.Activities.RunDerivedAssetProvider(ctx, DerivedAssetProviderExecutionInput{Lease: lease})
			t.Fatal("old generation execution was claimed")
		}
		assertDerivedAssetExecutionFailure(t, ctx, base.Pool, fixture.ExecutionItemID, "discarded", "PRODUCTION_GENERATION_MISMATCH")
		if got := calls.Load(); got != 0 {
			t.Fatalf("provider calls = %d, want 0", got)
		}
	})

	t.Run("target snapshot mismatch", func(t *testing.T) {
		ctx := context.Background()
		base := seedDerivedAssetExecutionBase(t, ctx)
		fixture := seedDerivedAssetExecutable(t, ctx, base)
		gateway, calls := countingDerivedAssetGateway(t)
		base.Activities.gateway = gateway
		if _, err := base.Pool.Exec(ctx, `
			UPDATE shot_asset_requirements
			SET prompt = 'changed after command', updated_at = now() + interval '1 second'
			WHERE id = $1
		`, fixture.RequirementID); err != nil {
			t.Fatalf("mutate requirement: %v", err)
		}

		lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-target-mismatch")
		if err == nil {
			_, _ = base.Activities.RunDerivedAssetProvider(ctx, DerivedAssetProviderExecutionInput{Lease: lease})
			t.Fatal("changed target execution was claimed")
		}
		assertDerivedAssetExecutionFailure(t, ctx, base.Pool, fixture.ExecutionItemID, "discarded", "DERIVED_ASSET_REQUIREMENT_CHANGED")
		if got := calls.Load(); got != 0 {
			t.Fatalf("provider calls = %d, want 0", got)
		}
	})
}

func TestDerivedAssetLateProviderResultAfterGenerationSwitchIsDiscardedIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	fixture := seedDerivedAssetExecutable(t, ctx, base)
	response := seedDerivedAssetGatewayImageResponse(t, ctx, base, "late-generation")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		tx, err := base.Pool.Begin(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, _, err := videoproduction.ResetActiveGeneration(ctx, tx, base.ProjectID); err != nil {
			tx.Rollback(ctx)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": response}); err != nil {
			t.Errorf("encode gateway response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	base.Activities.gateway = &provider.GatewayClient{BaseURL: server.URL, Token: "test-token", Client: server.Client()}

	lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-generation-switch")
	if err != nil {
		t.Fatalf("claim execution: %v", err)
	}
	if _, err := base.Activities.RunDerivedAssetProvider(ctx, DerivedAssetProviderExecutionInput{Lease: lease}); err == nil || !isWorkflowWriteFenced(err) {
		t.Fatalf("late provider result error = %v, want workflow write fence", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}

	var executionStatus, requestStatus, errorCode, requirementStatus, artifactID, mediaID, storageKey, activeGenerationID string
	var lateResultCount int
	if err := base.Pool.QueryRow(ctx, `
		SELECT execution.status, request.status, COALESCE(execution.error_code, ''),
		       execution.late_result_count, requirement.status,
		       COALESCE(requirement.derived_artifact_id::text, ''),
		       COALESCE(requirement.derived_media_file_id::text, ''),
		       COALESCE(requirement.derived_storage_key, ''),
		       project.active_video_production_generation_id::text
		FROM derived_asset_execution_items execution
		JOIN derived_asset_request_items request ON request.id = execution.request_item_id
		JOIN shot_asset_requirements requirement ON requirement.id = execution.requirement_id
		JOIN projects project ON project.id = execution.project_id
		WHERE execution.id = $1
	`, fixture.ExecutionItemID).Scan(
		&executionStatus, &requestStatus, &errorCode, &lateResultCount, &requirementStatus,
		&artifactID, &mediaID, &storageKey, &activeGenerationID,
	); err != nil {
		t.Fatalf("load late-result outcome: %v", err)
	}
	if executionStatus != "discarded" || requestStatus != "discarded" || errorCode != "PRODUCTION_GENERATION_MISMATCH" || lateResultCount != 1 {
		t.Fatalf("late-result terminal state = execution %s request %s code %s lateResults %d", executionStatus, requestStatus, errorCode, lateResultCount)
	}
	if requirementStatus != "image_running" || artifactID != "" || mediaID != "" || storageKey != "" {
		t.Fatalf("late result polluted requirement: status=%s artifact=%s media=%s storage=%s", requirementStatus, artifactID, mediaID, storageKey)
	}
	if activeGenerationID == base.GenerationID {
		t.Fatalf("active generation did not change: %s", activeGenerationID)
	}
	var newGenerationWrites int
	if err := base.Pool.QueryRow(ctx, `
		SELECT count(*)
		FROM shot_asset_requirements
		WHERE project_id = $1
		  AND production_generation_id = $2
		  AND (derived_artifact_id = $3 OR derived_media_file_id = $4 OR derived_storage_key = $5)
	`, base.ProjectID, activeGenerationID, response.Output.ArtifactID, response.Output.MediaFileID, response.Output.StorageKey).Scan(&newGenerationWrites); err != nil {
		t.Fatalf("count new-generation writes: %v", err)
	}
	if newGenerationWrites != 0 {
		t.Fatalf("late result wrote %d new-generation requirements", newGenerationWrites)
	}
}

func TestDerivedAssetExecutionLeaseCASDoesNotStealUnexpiredLeaseIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	fixture := seedDerivedAssetExecutable(t, ctx, base)

	first, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-a")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first.LeaseOwner != "worker-a" || first.LeaseToken == "" {
		t.Fatalf("first lease = %+v", first)
	}
	if _, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-b"); err == nil {
		t.Fatal("second worker stole an unexpired lease")
	}

	var owner, token string
	var expiresAt time.Time
	if err := base.Pool.QueryRow(ctx, `
		SELECT lease_owner, lease_token::text, lease_expires_at
		FROM derived_asset_execution_items WHERE id = $1
	`, fixture.ExecutionItemID).Scan(&owner, &token, &expiresAt); err != nil {
		t.Fatalf("load lease: %v", err)
	}
	if owner != "worker-a" || token != first.LeaseToken || !expiresAt.After(time.Now()) {
		t.Fatalf("lease changed after competing claim: owner=%q token=%q expiresAt=%s", owner, token, expiresAt)
	}
}

func TestDerivedAssetExecutionFailureSynchronizesImmutableRequestOutcomeIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	fixture := seedDerivedAssetExecutable(t, ctx, base)
	lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-failure-sync")
	if err != nil {
		t.Fatalf("claim execution: %v", err)
	}

	applied, err := base.Activities.FailDerivedAssetExecution(ctx, DerivedAssetExecutionFailure{
		Lease: lease, ErrorCode: "UPSTREAM_TIMEOUT", ErrorMessage: "provider request timed out", Retryable: true,
	})
	if err != nil {
		t.Fatalf("FailDerivedAssetExecution: %v", err)
	}
	if !applied {
		t.Fatal("failure transition was not applied")
	}

	var requestStatus, requestCode, requestMessage, executionStatus string
	if err := base.Pool.QueryRow(ctx, `
		SELECT request.status, request.error_code, request.error_message, execution.status
		FROM derived_asset_request_items request
		JOIN derived_asset_execution_items execution ON execution.id = request.current_attempt_id
		WHERE request.id = $1
	`, fixture.RequestItemID).Scan(&requestStatus, &requestCode, &requestMessage, &executionStatus); err != nil {
		t.Fatalf("load synchronized failure outcome: %v", err)
	}
	if requestStatus != "failed_retryable" || executionStatus != "failed_retryable" ||
		requestCode != "UPSTREAM_TIMEOUT" || requestMessage != "provider request timed out" {
		t.Fatalf("synchronized failure = request %s/%s/%q execution %s", requestStatus, requestCode, requestMessage, executionStatus)
	}

	_, err = base.Pool.Exec(ctx, `
		UPDATE derived_asset_request_items
		SET error_message = 'rewritten', revision = revision + 1
		WHERE id = $1
	`, fixture.RequestItemID)
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("terminal error rewrite = %v, want SQLSTATE 55000", err)
	}
}

func TestDerivedAssetReconcilerRecoversEveryDurableStageIntegration(t *testing.T) {
	testCases := []struct {
		name              string
		stage             string
		wantProviderCalls int32
	}{
		{name: "command created before worker claim", stage: "queued", wantProviderCalls: 1},
		{name: "worker claimed before provider call", stage: "leased", wantProviderCalls: 1},
		{name: "gateway succeeded before media verification", stage: "transferring", wantProviderCalls: 0},
		{name: "media verified before business commit", stage: "committing", wantProviderCalls: 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			base := seedDerivedAssetExecutionBase(t, ctx)
			fixture := seedDerivedAssetExecutable(t, ctx, base)
			response := seedDerivedAssetGatewayImageResponse(t, ctx, base, tc.stage)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"data": response}); err != nil {
					t.Errorf("encode gateway response: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			base.Activities.gateway = &provider.GatewayClient{BaseURL: server.URL, Token: "test-token", Client: server.Client()}

			if tc.stage == "queued" {
				if _, err := base.Pool.Exec(ctx, `
					UPDATE derived_asset_execution_items
					SET queued_at = now() - interval '10 minutes', revision = revision + 1
					WHERE id = $1
				`, fixture.ExecutionItemID); err != nil {
					t.Fatalf("backdate queued execution: %v", err)
				}
			} else {
				lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "crashed-worker")
				if err != nil {
					t.Fatalf("claim execution: %v", err)
				}
				if tc.stage == "transferring" || tc.stage == "committing" {
					if _, err := base.Activities.prepareDerivedAssetProviderRequest(ctx, lease); err != nil {
						t.Fatalf("prepare provider request: %v", err)
					}
					generated, err := base.Activities.persistReconciledDerivedAssetProviderResult(ctx, lease, response)
					if err != nil {
						t.Fatalf("persist provider result: %v", err)
					}
					if tc.stage == "committing" {
						if _, err := base.Activities.VerifyDerivedAssetMedia(ctx, generated); err != nil {
							t.Fatalf("verify media: %v", err)
						}
					}
				}
				if _, err := base.Pool.Exec(ctx, `
					UPDATE derived_asset_execution_items
					SET lease_expires_at = now() - interval '1 second',
					    heartbeat_at = now() - interval '2 seconds',
					    revision = revision + 1
					WHERE id = $1
				`, fixture.ExecutionItemID); err != nil {
					t.Fatalf("expire execution lease: %v", err)
				}
			}

			reconciled, err := ReconcileExpiredDerivedAssetExecutions(ctx, base.Activities, 10)
			if err != nil {
				t.Fatalf("ReconcileExpiredDerivedAssetExecutions: %v", err)
			}
			if reconciled != 1 {
				t.Fatalf("reconciled items = %d, want 1", reconciled)
			}
			if got := calls.Load(); got != tc.wantProviderCalls {
				t.Fatalf("provider calls = %d, want %d", got, tc.wantProviderCalls)
			}

			var executionStatus, requestStatus, requirementStatus, artifactID string
			if err := base.Pool.QueryRow(ctx, `
				SELECT execution.status, request.status, requirement.status,
				       COALESCE(requirement.derived_artifact_id::text, '')
				FROM derived_asset_execution_items execution
				JOIN derived_asset_request_items request ON request.id = execution.request_item_id
				JOIN shot_asset_requirements requirement ON requirement.id = execution.requirement_id
				WHERE execution.id = $1
			`, fixture.ExecutionItemID).Scan(&executionStatus, &requestStatus, &requirementStatus, &artifactID); err != nil {
				t.Fatalf("load reconciled outcome: %v", err)
			}
			if executionStatus != "succeeded" || requestStatus != "succeeded" ||
				requirementStatus != "image_succeeded" || artifactID != response.Output.ArtifactID {
				t.Fatalf("reconciled outcome = execution %s request %s requirement %s artifact %s", executionStatus, requestStatus, requirementStatus, artifactID)
			}
		})
	}
}

func TestDerivedAssetCommitIsIdempotentAndLateResultCannotOverwriteRequirementIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	fixture := seedDerivedAssetExecutable(t, ctx, base)
	lease, err := base.Activities.ClaimDerivedAssetExecution(ctx, base.Input, fixture.WorkItem, "worker-commit")
	if err != nil {
		t.Fatalf("claim execution: %v", err)
	}
	firstArtifact, firstMedia, firstStorage := insertDerivedAssetGeneratedMedia(t, ctx, base, "first")
	secondArtifact, secondMedia, secondStorage := insertDerivedAssetGeneratedMedia(t, ctx, base, "late")
	providerCallID := uuid.NewString()
	providerResult := json.RawMessage(`{"status":"succeeded"}`)
	if _, err := base.Pool.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'committing', revision = revision + 1,
		    provider_call_id = $2, provider_result_snapshot = $3,
		    provider_result_hash = $4, artifact_id = $5, media_file_id = $6,
		    storage_key = $7, heartbeat_at = now(), lease_expires_at = now() + interval '45 minutes'
		WHERE id = $1 AND lease_token = $8
	`, fixture.ExecutionItemID, providerCallID, providerResult, HashDerivedAssetSnapshot(json.RawMessage(providerResult)),
		firstArtifact, firstMedia, firstStorage, lease.LeaseToken); err != nil {
		t.Fatalf("prepare committing execution: %v", err)
	}

	first := DerivedAssetMediaVerification{
		Lease: lease, ProviderCallID: providerCallID, ModelID: base.ImageModelID,
		ArtifactID: firstArtifact, MediaFileID: firstMedia, StorageKey: firstStorage,
	}
	if err := base.Activities.CommitDerivedAssetExecution(ctx, first); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if err := base.Activities.CommitDerivedAssetExecution(ctx, first); err != nil {
		t.Fatalf("duplicate commit: %v", err)
	}
	late := DerivedAssetMediaVerification{
		Lease: lease, ProviderCallID: uuid.NewString(), ModelID: base.ImageModelID,
		ArtifactID: secondArtifact, MediaFileID: secondMedia, StorageKey: secondStorage,
	}
	if err := base.Activities.CommitDerivedAssetExecution(ctx, late); err != nil {
		t.Fatalf("late commit must be a harmless replay: %v", err)
	}

	var artifactID, mediaID, storageKey, executionStatus string
	if err := base.Pool.QueryRow(ctx, `
		SELECT requirement.derived_artifact_id::text, requirement.derived_media_file_id::text,
		       requirement.derived_storage_key, execution.status
		FROM shot_asset_requirements requirement
		JOIN derived_asset_execution_items execution ON execution.id = $2
		WHERE requirement.id = $1
	`, fixture.RequirementID, fixture.ExecutionItemID).Scan(&artifactID, &mediaID, &storageKey, &executionStatus); err != nil {
		t.Fatalf("load committed target: %v", err)
	}
	if artifactID != firstArtifact || mediaID != firstMedia || storageKey != firstStorage || executionStatus != "succeeded" {
		t.Fatalf("late result overwrote target: artifact=%s media=%s storage=%s status=%s", artifactID, mediaID, storageKey, executionStatus)
	}
}

func TestDerivedAssetBatchTerminalAggregateCountsNotFoundDuplicateAndSkippedIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	batchID := insertDerivedAssetBatch(t, ctx, base, "explicit", "", "", 0)
	originalID := uuid.NewString()
	firstRequestID := insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: batchID, InputOrdinal: 1, OriginalID: originalID,
		Disposition: "not_found", Status: "blocked", ErrorCode: "DERIVED_ASSET_REQUIREMENT_NOT_FOUND", ErrorMessage: "镜头资产需求不存在",
	})
	insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: batchID, InputOrdinal: 2, OriginalID: originalID,
		Disposition: "duplicate", Status: "skipped", DuplicateOf: firstRequestID,
	})
	insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: batchID, InputOrdinal: 3, OriginalID: uuid.NewString(),
		Disposition: "skipped", Status: "skipped",
	})

	output, err := base.Activities.CompleteDerivedAssetBatchWorkflowV2(ctx, base.Input, batchID)
	if err != nil {
		t.Fatalf("CompleteDerivedAssetBatchWorkflowV2: %v", err)
	}
	if output.Status != "failed" || output.TotalItems != 3 || output.NotFoundItems != 1 ||
		output.DuplicateItems != 1 || output.SkippedItems != 1 || output.CompletedItems != 2 || output.FailedItems != 1 {
		t.Fatalf("terminal aggregate = %+v", output)
	}
}

func TestDerivedAssetRetryLineageKeepsOriginalFailureImmutableIntegration(t *testing.T) {
	ctx := context.Background()
	base := seedDerivedAssetExecutionBase(t, ctx)
	originalBatchID := insertDerivedAssetBatch(t, ctx, base, "explicit", "", "", 0)
	originalID := uuid.NewString()
	originalRequestID := insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: originalBatchID, InputOrdinal: 1, OriginalID: originalID,
		Disposition: "executable", Status: "pending", Retryable: true,
	})
	insertDerivedAssetTerminalAttempt(t, ctx, base, originalBatchID, originalRequestID, "failed_retryable", "UPSTREAM_TIMEOUT")

	retryBatchID := insertDerivedAssetBatch(t, ctx, base, "retry", originalBatchID, originalBatchID, 1)
	retryRequestID := insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: retryBatchID, InputOrdinal: 1, OriginalID: originalID,
		Disposition: "executable", Status: "pending", Retryable: true,
		RootRequestItemID: originalRequestID, RetryOfRequestItemID: originalRequestID,
		InputHash: derivedAssetRequestInputHash(originalID),
	})

	var originalDisposition, originalStatus, originalError string
	if err := base.Pool.QueryRow(ctx, `
		SELECT disposition, status, error_code
		FROM derived_asset_request_items WHERE id = $1
	`, originalRequestID).Scan(&originalDisposition, &originalStatus, &originalError); err != nil {
		t.Fatalf("load original request item: %v", err)
	}
	if originalDisposition != "executable" || originalStatus != "failed_retryable" || originalError != "UPSTREAM_TIMEOUT" {
		t.Fatalf("original request changed: %s/%s/%s", originalDisposition, originalStatus, originalError)
	}
	var retryStatus, retryRoot, retryOf string
	if err := base.Pool.QueryRow(ctx, `
		SELECT status, root_request_item_id::text, retry_of_request_item_id::text
		FROM derived_asset_request_items WHERE id = $1
	`, retryRequestID).Scan(&retryStatus, &retryRoot, &retryOf); err != nil {
		t.Fatalf("load retry request item: %v", err)
	}
	if retryStatus != "pending" || retryRoot != originalRequestID || retryOf != originalRequestID {
		t.Fatalf("retry lineage = status=%s root=%s retryOf=%s", retryStatus, retryRoot, retryOf)
	}

	_, err := base.Pool.Exec(ctx, `
		UPDATE derived_asset_request_items
		SET input_snapshot = '{"mutated":true}'::jsonb, revision = revision + 1
		WHERE id = $1
	`, originalRequestID)
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("immutable original update error = %v, want SQLSTATE 55000", err)
	}
}

type derivedAssetExecutionBase struct {
	Pool               *pgxpool.Pool
	Activities         Activities
	Input              TextToStoryboardInput
	OrganizationID     string
	UserID             string
	ProjectID          string
	WorkflowRunID      string
	GenerationID       string
	BindingID          string
	BindingRevision    int64
	ImageModelID       string
	ProviderAccountID  string
	ModelSnapshot      DerivedAssetModelSnapshot
	CapabilitySnapshot DerivedAssetCapabilitySnapshot
}

type derivedAssetExecutableFixture struct {
	BatchID         string
	RequestItemID   string
	ExecutionItemID string
	RequirementID   string
	ShotID          string
	AssetID         string
	WorkItem        DerivedAssetBatchWorkItem
}

type derivedAssetRequestSeed struct {
	BatchID              string
	InputOrdinal         int
	OriginalID           string
	RequirementID        string
	DuplicateOf          string
	RootRequestItemID    string
	RetryOfRequestItemID string
	Disposition          string
	Status               string
	Retryable            bool
	ErrorCode            string
	ErrorMessage         string
	InputHash            string
}

func seedDerivedAssetExecutionBase(t *testing.T, ctx context.Context) derivedAssetExecutionBase {
	t.Helper()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, _, imageModelID := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	base := derivedAssetExecutionBase{
		Pool: pool, OrganizationID: orgID, UserID: userID, ProjectID: projectID,
		WorkflowRunID: workflowRunID, ImageModelID: imageModelID,
	}
	base.Activities = NewActivities(pool, nil, nil)
	base.Input = TextToStoryboardInput{
		OrganizationID: orgID, ProjectID: projectID, WorkflowRunID: workflowRunID, CreatedBy: userID,
	}
	if err := pool.QueryRow(ctx, `
		SELECT project.active_video_production_generation_id::text,
		       generation.binding_id::text, binding.revision
		FROM projects project
		JOIN project_video_production_generations generation ON generation.id = project.active_video_production_generation_id
		JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		WHERE project.id = $1
	`, projectID).Scan(&base.GenerationID, &base.BindingID, &base.BindingRevision); err != nil {
		t.Fatalf("load production identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["image.generate"]', '{"referenceImageCount":4}',
		        '{"formats":["png"]}', '["standard"]', '{}', '{}')
	`, imageModelID); err != nil {
		t.Fatalf("insert image model capability: %v", err)
	}
	var modelUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT provider_account_id::text, id::text, model_key, modality, status, updated_at
		FROM provider_models WHERE id = $1
	`, imageModelID).Scan(
		&base.ProviderAccountID, &base.ModelSnapshot.ProviderModelID, &base.ModelSnapshot.ModelKey,
		&base.ModelSnapshot.Modality, &base.ModelSnapshot.Status, &modelUpdatedAt,
	); err != nil {
		t.Fatalf("load image model snapshot: %v", err)
	}
	base.ModelSnapshot.ProviderAccountID = base.ProviderAccountID
	base.ModelSnapshot.ModelProfileKey = imageGenerationModelProfileKey
	base.ModelSnapshot.UpdatedAt = modelUpdatedAt.UTC().Format(time.RFC3339Nano)
	if err := pool.QueryRow(ctx, `
		SELECT task_types, input_limits, output_limits, quality_tiers,
		       provider_options_schema, pricing_policy
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, imageModelID).Scan(
		&base.CapabilitySnapshot.TaskTypes, &base.CapabilitySnapshot.InputLimits,
		&base.CapabilitySnapshot.OutputLimits, &base.CapabilitySnapshot.QualityTiers,
		&base.CapabilitySnapshot.ProviderOptionsSchema, &base.CapabilitySnapshot.PricingPolicy,
	); err != nil {
		t.Fatalf("load image capability snapshot: %v", err)
	}
	base.CapabilitySnapshot = normalizeDerivedAssetCapability(base.CapabilitySnapshot)
	return base
}

func seedDerivedAssetExecutable(t *testing.T, ctx context.Context, base derivedAssetExecutionBase) derivedAssetExecutableFixture {
	t.Helper()
	fixture := derivedAssetExecutableFixture{}
	var referenceArtifactID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, production_generation_id, metadata, created_by
		)
		VALUES ($1, $2, $3, 'asset_reference_image', $4, 'image/png', $5, $6, '{}', $7)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID,
		"tests/derived-assets/reference-"+uuid.NewString()+".png", fmt.Sprintf("%064x", time.Now().UnixNano()),
		base.GenerationID, base.UserID).Scan(&referenceArtifactID); err != nil {
		t.Fatalf("insert reference artifact: %v", err)
	}
	var referenceStorage string
	if err := base.Pool.QueryRow(ctx, `SELECT storage_key FROM artifacts WHERE id = $1`, referenceArtifactID).Scan(&referenceStorage); err != nil {
		t.Fatalf("load reference storage key: %v", err)
	}
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description,
			base_prompt, consistency_prompt, primary_reference_artifact_id,
			primary_reference_storage_key, status, review_status, metadata, created_by
		)
		VALUES ($1, $2, 'character', 'Derived Fixture', 'fixture asset',
		        'four-view character sheet', 'keep identity stable', $3, $4,
		        'prompt_ready', 'approved', '{}', $5)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, referenceArtifactID, referenceStorage, base.UserID).Scan(&fixture.AssetID); err != nil {
		t.Fatalf("insert canonical asset: %v", err)
	}
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, production_generation_id, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 0, 90000, 90000, 90000,
		        'fixture shot', 'static', 'subtle motion', 'quiet', 'image prompt', 'video prompt',
		        'ready', 'approved', $4, '{}')
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID, base.GenerationID).Scan(&fixture.ShotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, role_in_shot, prompt, status, review_status,
			production_generation_id, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'lead',
		        'derived image prompt', 'pending', 'approved', $6, '{}')
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID, fixture.ShotID,
		fixture.AssetID, base.GenerationID).Scan(&fixture.RequirementID); err != nil {
		t.Fatalf("insert shot asset requirement: %v", err)
	}

	requirementSnapshot := loadDerivedRequirementSnapshot(t, ctx, base.Pool, fixture.RequirementID)
	shotSnapshot := loadDerivedShotSnapshot(t, ctx, base.Pool, fixture.ShotID)
	assetSnapshot := loadDerivedCanonicalAssetSnapshot(t, ctx, base.Pool, fixture.AssetID)
	promptSnapshot := DerivedAssetPromptSnapshot{
		TemplateKey: "derived_asset_image_prompt", Hash: HashDerivedAssetSnapshot("derived image prompt"),
		Source: "fixture", Text: "derived image prompt",
	}
	referenceSnapshot := DerivedAssetReferenceSnapshot{Items: []provider.GatewayImageReference{{
		Type: "asset_primary", AssetID: fixture.AssetID, ArtifactID: referenceArtifactID, StorageKey: referenceStorage,
	}}}
	request := provider.GatewayImageRequest{
		OrganizationID: base.OrganizationID, ProjectID: base.ProjectID, WorkflowRunID: base.WorkflowRunID,
		ModelProfileKey: imageGenerationModelProfileKey, ProviderModelID: base.ImageModelID,
		PromptTemplateKey: promptSnapshot.TemplateKey, PromptHash: promptSnapshot.Hash, PromptSource: promptSnapshot.Source,
		IdempotencyKey: "derived-fixture-" + uuid.NewString(),
		Input:          json.RawMessage(`{"prompt":"derived image prompt"}`),
		References:     referenceSnapshot.Items,
		Options:        provider.GatewayImageOptions{TimeoutMS: 60000},
	}
	request.Options.IdempotencyKey = request.IdempotencyKey
	fixture.BatchID = insertDerivedAssetBatch(t, ctx, base, "explicit", "", "", 0)
	fixture.RequestItemID = insertDerivedAssetRequestItem(t, ctx, base, derivedAssetRequestSeed{
		BatchID: fixture.BatchID, InputOrdinal: 1, OriginalID: fixture.RequirementID,
		RequirementID: fixture.RequirementID, Disposition: "executable", Status: "pending", Retryable: true,
	})
	var attemptGeneration int
	if err := base.Pool.QueryRow(ctx, `SELECT attempt_generation FROM workflow_runs WHERE id = $1`, base.WorkflowRunID).Scan(&attemptGeneration); err != nil {
		t.Fatalf("load workflow attempt generation: %v", err)
	}
	nodeKey := "derived-asset:" + fixture.RequestItemID
	var nodeRunID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type,
			status, input, attempt_generation, production_generation_id
		)
		VALUES ($1, $2, $3, $4, 'shot_asset_requirement.derived_image.generate',
		        'queued', '{}', $5, $6)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID, nodeKey, attemptGeneration, base.GenerationID).Scan(&nodeRunID); err != nil {
		t.Fatalf("insert workflow node run: %v", err)
	}
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO derived_asset_execution_items(
			batch_id, request_item_id, organization_id, project_id, workflow_run_id,
			node_run_id, node_key, production_generation_id, video_production_binding_id,
			video_production_binding_revision, attempt_no, requirement_id, storyboard_shot_id,
			canonical_asset_id, requirement_snapshot, requirement_snapshot_hash,
			storyboard_shot_snapshot, storyboard_shot_snapshot_hash,
			canonical_asset_snapshot, canonical_asset_snapshot_hash,
			prompt_text, prompt_snapshot, prompt_hash, reference_snapshot, reference_snapshot_hash,
			model_profile_key, provider_account_id, provider_model_id, model_snapshot, model_snapshot_hash,
			capability_snapshot, capability_snapshot_hash, request_snapshot, request_hash,
			idempotency_key, status, queued_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24,
			$25, $26, $27, $28, $29, $30, $31, $32, $33, $34, 'queued', now()
		)
		RETURNING id::text
	`, fixture.BatchID, fixture.RequestItemID, base.OrganizationID, base.ProjectID, base.WorkflowRunID,
		nodeRunID, nodeKey, base.GenerationID, base.BindingID, base.BindingRevision,
		fixture.RequirementID, fixture.ShotID, fixture.AssetID,
		mustJSON(requirementSnapshot), HashDerivedAssetSnapshot(requirementSnapshot),
		mustJSON(shotSnapshot), HashDerivedAssetSnapshot(shotSnapshot),
		mustJSON(assetSnapshot), HashDerivedAssetSnapshot(assetSnapshot),
		promptSnapshot.Text, mustJSON(promptSnapshot), promptSnapshot.Hash,
		mustJSON(referenceSnapshot), HashDerivedAssetSnapshot(referenceSnapshot),
		imageGenerationModelProfileKey, base.ProviderAccountID, base.ImageModelID,
		mustJSON(base.ModelSnapshot), HashDerivedAssetSnapshot(base.ModelSnapshot),
		mustJSON(base.CapabilitySnapshot), HashDerivedAssetSnapshot(base.CapabilitySnapshot),
		mustJSON(request), HashDerivedAssetSnapshot(request), request.IdempotencyKey,
	).Scan(&fixture.ExecutionItemID); err != nil {
		t.Fatalf("insert derived asset execution item: %v", err)
	}
	fixture.WorkItem = DerivedAssetBatchWorkItem{
		ExecutionItemID: fixture.ExecutionItemID, RequestItemID: fixture.RequestItemID,
		BatchID: fixture.BatchID, InputOrdinal: 1, RequirementID: fixture.RequirementID,
		StoryboardShotID: fixture.ShotID, CanonicalAssetID: fixture.AssetID,
		NodeRunID: nodeRunID, NodeKey: nodeKey, AttemptNo: 1, Status: "queued",
	}
	return fixture
}

func insertDerivedAssetBatch(
	t *testing.T,
	ctx context.Context,
	base derivedAssetExecutionBase,
	requestMode string,
	rootBatchID string,
	retryOfBatchID string,
	retryDepth int,
) string {
	t.Helper()
	filters := map[string]any{"mode": requestMode}
	request := map[string]any{"mode": requestMode, "root": rootBatchID, "retryOf": retryOfBatchID, "depth": retryDepth, "nonce": uuid.NewString()}
	var batchID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO derived_asset_batches(
			organization_id, project_id, workflow_run_id, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			root_batch_id, retry_of_batch_id, retry_depth, request_mode,
			filters, filters_hash, idempotency_key, request_hash, status, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, NULLIF($8, '')::uuid,
		        $9, $10, $11, $12, $13, $14, 'queued', $15)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID, base.GenerationID,
		base.BindingID, base.BindingRevision, rootBatchID, retryOfBatchID, retryDepth, requestMode,
		mustJSON(filters), HashDerivedAssetSnapshot(filters), "batch-"+uuid.NewString(),
		HashDerivedAssetSnapshot(request), base.UserID).Scan(&batchID); err != nil {
		t.Fatalf("insert derived asset batch: %v", err)
	}
	return batchID
}

func insertDerivedAssetRequestItem(t *testing.T, ctx context.Context, base derivedAssetExecutionBase, seed derivedAssetRequestSeed) string {
	t.Helper()
	if seed.InputHash == "" {
		seed.InputHash = derivedAssetRequestInputHash(seed.OriginalID)
	}
	inputSnapshot := map[string]any{"originalId": seed.OriginalID}
	var requestItemID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO derived_asset_request_items(
			batch_id, organization_id, project_id, input_ordinal, original_id, requirement_id,
			duplicate_of_request_item_id, root_request_item_id, retry_of_request_item_id,
			disposition, disposition_detail, error_code, error_message, retryable,
			input_snapshot, input_hash, status
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid,
		        NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, $10, '{}',
		        NULLIF($11, ''), NULLIF($12, ''), $13, $14, $15, $16)
		RETURNING id::text
	`, seed.BatchID, base.OrganizationID, base.ProjectID, seed.InputOrdinal, seed.OriginalID,
		seed.RequirementID, seed.DuplicateOf, seed.RootRequestItemID, seed.RetryOfRequestItemID,
		seed.Disposition, seed.ErrorCode, seed.ErrorMessage, seed.Retryable,
		mustJSON(inputSnapshot), seed.InputHash, seed.Status).Scan(&requestItemID); err != nil {
		t.Fatalf("insert request item %d/%s: %v", seed.InputOrdinal, seed.Disposition, err)
	}
	return requestItemID
}

func insertDerivedAssetTerminalAttempt(
	t *testing.T,
	ctx context.Context,
	base derivedAssetExecutionBase,
	batchID string,
	requestItemID string,
	status string,
	errorCode string,
) string {
	t.Helper()
	hash := HashDerivedAssetSnapshot(map[string]any{"requestItemId": requestItemID})
	providerCallID, artifactID, mediaFileID, storageKey, outputRaw, outputHash := "", "", "", "", json.RawMessage(nil), ""
	if status == "succeeded" {
		providerCallID, artifactID, mediaFileID, storageKey = uuid.NewString(), uuid.NewString(), uuid.NewString(), "tests/derived-assets/synthetic.png"
		outputRaw = mustJSON(map[string]any{"artifactId": artifactID, "mediaFileId": mediaFileID})
		outputHash = HashDerivedAssetSnapshot(json.RawMessage(outputRaw))
	}
	var executionID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO derived_asset_execution_items(
			batch_id, request_item_id, organization_id, project_id, workflow_run_id,
			node_key, production_generation_id, video_production_binding_id,
			video_production_binding_revision, attempt_no, requirement_id, storyboard_shot_id,
			canonical_asset_id, requirement_snapshot, requirement_snapshot_hash,
			storyboard_shot_snapshot, storyboard_shot_snapshot_hash,
			canonical_asset_snapshot, canonical_asset_snapshot_hash,
			prompt_text, prompt_snapshot, prompt_hash, reference_snapshot, reference_snapshot_hash,
			model_profile_key, provider_account_id, provider_model_id, model_snapshot, model_snapshot_hash,
			capability_snapshot, capability_snapshot_hash, request_snapshot, request_hash,
			idempotency_key, status, provider_call_id, artifact_id, media_file_id, storage_key,
			output_snapshot, output_hash, error_code, error_message, completed_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $11, $12,
			'{}', $13, '{}', $13, '{}', $13, 'terminal prompt', '{}', $13, '{}', $13,
			$14, $15, $16, '{}', $13, '{}', $13, '{}', $17, $18, $19,
			NULLIF($20, '')::uuid, NULLIF($21, '')::uuid, NULLIF($22, '')::uuid, NULLIF($23, ''),
			$24, NULLIF($25, ''), NULLIF($26, ''), NULLIF($27, ''), now()
		)
		RETURNING id::text
	`, batchID, requestItemID, base.OrganizationID, base.ProjectID, base.WorkflowRunID,
		"derived-terminal:"+requestItemID, base.GenerationID, base.BindingID, base.BindingRevision,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), hash,
		imageGenerationModelProfileKey, base.ProviderAccountID, base.ImageModelID,
		hash, "execution-"+uuid.NewString(), status,
		providerCallID, artifactID, mediaFileID, storageKey, outputRaw, outputHash,
		errorCode, errorCode,
	).Scan(&executionID); err != nil {
		t.Fatalf("insert terminal attempt %s: %v", status, err)
	}
	return executionID
}

func loadDerivedRequirementSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requirementID string) DerivedAssetRequirementSnapshot {
	t.Helper()
	var snapshot DerivedAssetRequirementSnapshot
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, production_generation_id::text,
		       storyboard_shot_id::text, asset_id::text, review_status, status,
		       COALESCE(prompt, ''), updated_at
		FROM shot_asset_requirements WHERE id = $1
	`, requirementID).Scan(
		&snapshot.ID, &snapshot.ProjectID, &snapshot.ProductionGenerationID,
		&snapshot.StoryboardShotID, &snapshot.CanonicalAssetID, &snapshot.ReviewStatus,
		&snapshot.Status, &snapshot.Prompt, &updatedAt,
	); err != nil {
		t.Fatalf("load requirement snapshot: %v", err)
	}
	snapshot.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return snapshot
}

func loadDerivedShotSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shotID string) DerivedAssetStoryboardShotSnapshot {
	t.Helper()
	var snapshot DerivedAssetStoryboardShotSnapshot
	var deletedAt *time.Time
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, production_generation_id::text,
		       COALESCE(shot_no, shot_index + 1), deleted_at, updated_at
		FROM storyboard_shots WHERE id = $1
	`, shotID).Scan(&snapshot.ID, &snapshot.ProjectID, &snapshot.ProductionGenerationID, &snapshot.ShotNo, &deletedAt, &updatedAt); err != nil {
		t.Fatalf("load shot snapshot: %v", err)
	}
	if deletedAt != nil {
		snapshot.DeletedAt = deletedAt.UTC().Format(time.RFC3339Nano)
	}
	snapshot.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return snapshot
}

func loadDerivedCanonicalAssetSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, assetID string) DerivedAssetCanonicalAssetSnapshot {
	t.Helper()
	var snapshot DerivedAssetCanonicalAssetSnapshot
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT id::text, project_id::text, status, revision, prompt_revision, updated_at,
		       COALESCE(primary_reference_artifact_id::text, ''),
		       COALESCE(primary_reference_media_file_id::text, ''),
		       COALESCE(primary_reference_storage_key, ''),
		       COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''),
		       COALESCE(reference_storage_key, '')
		FROM canonical_assets WHERE id = $1
	`, assetID).Scan(
		&snapshot.ID, &snapshot.ProjectID, &snapshot.Status, &snapshot.Revision,
		&snapshot.PromptRevision, &updatedAt, &snapshot.PrimaryReferenceArtifactID,
		&snapshot.PrimaryReferenceMediaFileID, &snapshot.PrimaryReferenceStorageKey,
		&snapshot.FallbackReferenceArtifactID, &snapshot.FallbackReferenceMediaFileID,
		&snapshot.FallbackReferenceStorageKey,
	); err != nil {
		t.Fatalf("load canonical asset snapshot: %v", err)
	}
	snapshot.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return snapshot
}

func insertDerivedAssetGeneratedMedia(
	t *testing.T,
	ctx context.Context,
	base derivedAssetExecutionBase,
	label string,
) (string, string, string) {
	t.Helper()
	storageKey := "tests/derived-assets/" + label + "-" + uuid.NewString() + ".png"
	contentHash := fmt.Sprintf("%064x", time.Now().UnixNano())
	var artifactID, mediaID string
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, production_generation_id, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated_image', $4, 'image/png', $5, $6, '{}', $7)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, base.WorkflowRunID, storageKey,
		contentHash, base.GenerationID, base.UserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert generated artifact: %v", err)
	}
	if err := base.Pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, production_generation_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', 1024, 1536, 864, $5, $6, '{}', $7)
		RETURNING id::text
	`, base.OrganizationID, base.ProjectID, artifactID, storageKey, contentHash,
		base.GenerationID, base.UserID).Scan(&mediaID); err != nil {
		t.Fatalf("insert generated media: %v", err)
	}
	return artifactID, mediaID, storageKey
}

func seedDerivedAssetGatewayImageResponse(
	t *testing.T,
	ctx context.Context,
	base derivedAssetExecutionBase,
	label string,
) provider.GatewayImageResponse {
	t.Helper()
	artifactID, mediaFileID, storageKey := insertDerivedAssetGeneratedMedia(t, ctx, base, label)
	width, height := 1536, 864
	return provider.GatewayImageResponse{
		ProviderRequestID: uuid.NewString(),
		ProviderCallID:    uuid.NewString(),
		ModelID:           base.ImageModelID,
		Status:            "succeeded",
		Output: provider.GatewayImageOutput{
			ArtifactID: artifactID, MediaFileID: mediaFileID, StorageKey: storageKey,
			MimeType: "image/png", Width: &width, Height: &height, AspectRatio: "16:9",
		},
	}
}

func countingDerivedAssetGateway(t *testing.T) (*provider.GatewayClient, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"failed"}}`))
	}))
	t.Cleanup(server.Close)
	return &provider.GatewayClient{BaseURL: server.URL, Token: "test-token", Client: server.Client()}, &calls
}

func assertDerivedAssetExecutionFailure(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	executionItemID string,
	wantStatus string,
	wantCode string,
) {
	t.Helper()
	var status, code string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, '')
		FROM derived_asset_execution_items WHERE id = $1
	`, executionItemID).Scan(&status, &code); err != nil {
		t.Fatalf("load execution failure: %v", err)
	}
	if status != wantStatus || code != wantCode {
		t.Fatalf("execution failure = %s/%s, want %s/%s", status, code, wantStatus, wantCode)
	}
}

func derivedAssetRequestInputHash(originalID string) string {
	return HashDerivedAssetSnapshot(map[string]any{"originalId": originalID})
}
