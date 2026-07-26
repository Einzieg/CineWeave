package workflows

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

type projectDeletionTestStorage struct {
	mu                sync.Mutex
	deleted           []string
	failuresRemaining map[string]int
}

func (storageClient *projectDeletionTestStorage) PutJSON(
	context.Context,
	string,
	any,
) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}

func (storageClient *projectDeletionTestStorage) DeleteObject(
	_ context.Context,
	key string,
) error {
	storageClient.mu.Lock()
	defer storageClient.mu.Unlock()
	if storageClient.failuresRemaining[key] > 0 {
		storageClient.failuresRemaining[key]--
		return errors.New("temporary object storage failure")
	}
	storageClient.deleted = append(storageClient.deleted, key)
	return nil
}

func TestProjectDeletionStorageBatchRecoversRetryAndExpiredLeaseIntegration(t *testing.T) {
	ctx, pool, organizationID, projectID, _ := seedNodeRunIntegrationTest(t)
	var workspaceID string
	var requestedBy string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT workspace_id::text, created_by::text
		FROM projects
		WHERE id = $1
	`, projectID).Scan(&workspaceID, &requestedBy))

	requestID := uuid.NewString()
	storageKey := "projects/" + projectID + "/deletion-recovery.bin"
	_, err := pool.Exec(ctx, `
		UPDATE projects
		SET lifecycle_status = 'deleting',
		    deletion_revision = 1,
		    deletion_requested_at = now(),
		    video_production_locked = true
		WHERE id = $1
	`, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO project_deletion_requests(
			id, organization_id, workspace_id, project_id, project_name,
			project_revision, deletion_revision, status, impact_snapshot, impact_hash,
			storage_object_count, temporal_workflow_id, idempotency_key, requested_by, drain_deadline_at
		)
		SELECT
			$1, organization_id, workspace_id, id, name,
			revision, 1, 'failed_retryable', '{}'::jsonb, $2,
			1, $3, $4, $5, now() + interval '15 minutes'
		FROM projects
		WHERE id = $6
	`, requestID, strings.Repeat("a", 64), "project-deletion-"+requestID,
		"project-deletion-recovery-"+requestID, requestedBy, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO project_deletion_objects(
			request_id, project_id, source_kind, storage_key,
			status, attempt_count, claim_token, claim_expires_at
		)
		VALUES ($1, $2, 'artifact', $3, 'deleting', 1, $4, now() + interval '1 hour')
	`, requestID, projectID, storageKey, uuid.NewString())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'requested',
		    retry_count = 1,
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $1
	`, requestID)
	require.NoError(t, err)

	input := ProjectDeletionInput{
		OrganizationID:   organizationID,
		WorkspaceID:      workspaceID,
		ProjectID:        projectID,
		RequestID:        requestID,
		DeletionRevision: 1,
		RequestedBy:      requestedBy,
	}
	storageClient := &projectDeletionTestStorage{}
	activities := Activities{db: pool, storage: storageClient}

	_, err = activities.PrepareProjectDeletion(ctx, input)
	require.NoError(t, err)
	var status string
	var claimToken *string
	var claimExpiresAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, claim_token::text, claim_expires_at
		FROM project_deletion_objects
		WHERE request_id = $1
	`, requestID).Scan(&status, &claimToken, &claimExpiresAt))
	require.Equal(t, "pending", status)
	require.Nil(t, claimToken)
	require.Nil(t, claimExpiresAt)

	expiredClaimToken := uuid.NewString()
	_, err = pool.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'deleting_storage'
		WHERE id = $1
	`, requestID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE project_deletion_objects
		SET status = 'deleting',
		    claim_token = $2,
		    claim_expires_at = now() - interval '1 minute',
		    updated_at = now()
		WHERE request_id = $1
	`, requestID, expiredClaimToken)
	require.NoError(t, err)
	shared, err := activities.projectDeletionStorageKeyShared(ctx, projectID, storageKey)
	require.NoError(t, err)
	require.False(t, shared)

	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestActivityEnvironment()
	environment.RegisterActivity(activities.DeleteProjectStorageBatch)
	encoded, err := environment.ExecuteActivity(
		DeleteProjectStorageBatchActivityName,
		input,
		1,
	)
	require.NoError(t, err)
	var output ProjectDeletionStorageBatchOutput
	require.NoError(t, encoded.Get(&output))
	require.Equal(t, 1, output.SelectedCount)
	require.Equal(t, 1, output.DeletedCount, "batch output: %+v", output)
	require.Zero(t, output.PendingCount)
	require.Zero(t, output.InFlightCount)
	require.Zero(t, output.TotalFailedCount)
	require.Equal(t, []string{storageKey}, storageClient.deleted)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, claim_token::text, claim_expires_at
		FROM project_deletion_objects
		WHERE request_id = $1
	`, requestID).Scan(&status, &claimToken, &claimExpiresAt))
	require.Equal(t, "deleted", status)
	require.Nil(t, claimToken)
	require.Nil(t, claimExpiresAt)
}

func TestProjectDeletionCompletesAfterStorageRetryAndPreservesSharedObjectsIntegration(t *testing.T) {
	ctx, pool, organizationID, projectID, _ := seedNodeRunIntegrationTest(t)
	var workspaceID string
	var requestedBy string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT workspace_id::text, created_by::text
		FROM projects
		WHERE id = $1
	`, projectID).Scan(&workspaceID, &requestedBy))

	var siblingProjectID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, created_by, video_production_state
		)
		VALUES ($1, $2, 'Shared Storage Owner', $3, 'unconfigured')
		RETURNING id::text
	`, organizationID, workspaceID, requestedBy).Scan(&siblingProjectID))

	requestID := uuid.NewString()
	sharedKey := "projects/shared/catalog-object.bin"
	missingKey := "projects/" + projectID + "/already-missing.bin"
	retryKey := "projects/" + projectID + "/retry-once.bin"
	_, err := pool.Exec(ctx, `
		UPDATE projects
		SET lifecycle_status = 'deleting',
		    deletion_revision = 1,
		    deletion_requested_at = now(),
		    video_production_locked = true
		WHERE id = $1
	`, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO project_deletion_requests(
			id, organization_id, workspace_id, project_id, project_name,
			project_revision, deletion_revision, status, impact_snapshot, impact_hash,
			storage_object_count, temporal_workflow_id, idempotency_key, requested_by, drain_deadline_at
		)
		SELECT
			$1, organization_id, workspace_id, id, name,
			revision, 1, 'waiting_for_terminal', '{}'::jsonb, $2,
			3, $3, $4, $5, now() + interval '15 minutes'
		FROM projects
		WHERE id = $6
	`, requestID, strings.Repeat("b", 64), "project-deletion-"+requestID,
		"project-deletion-complete-"+requestID, requestedBy, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES
			($1, $2, 'storyboard_image', $3, 'application/octet-stream', '{}'::jsonb, $5),
			($1, $2, 'storyboard_image', $4, 'application/octet-stream', '{}'::jsonb, $5),
			($1, $2, 'storyboard_image', $6, 'application/octet-stream', '{}'::jsonb, $5),
			($1, $7, 'storyboard_image', $3, 'application/octet-stream', '{}'::jsonb, $5)
	`, organizationID, projectID, sharedKey, missingKey, requestedBy, retryKey, siblingProjectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key,
			mime_type, byte_size, checksum, metadata, created_by
		)
		SELECT organization_id, project_id, id, storage_key,
		       mime_type, 256, $3, '{}'::jsonb, created_by
		FROM artifacts
		WHERE project_id = $1
		  AND storage_key = $2
		LIMIT 1
	`, projectID, retryKey, strings.Repeat("c", 64))
	require.NoError(t, err)

	input := ProjectDeletionInput{
		OrganizationID:   organizationID,
		WorkspaceID:      workspaceID,
		ProjectID:        projectID,
		RequestID:        requestID,
		DeletionRevision: 1,
		RequestedBy:      requestedBy,
	}
	storageClient := &projectDeletionTestStorage{
		failuresRemaining: map[string]int{retryKey: 1},
	}
	activities := Activities{db: pool, storage: storageClient}
	var suite testsuite.WorkflowTestSuite
	activityEnvironment := suite.NewTestActivityEnvironment()
	activityEnvironment.RegisterActivity(activities.DeleteProjectStorageBatch)
	executeStorageBatch := func() ProjectDeletionStorageBatchOutput {
		t.Helper()
		encoded, executeErr := activityEnvironment.ExecuteActivity(
			DeleteProjectStorageBatchActivityName,
			input,
			128,
		)
		require.NoError(t, executeErr)
		var batch ProjectDeletionStorageBatchOutput
		require.NoError(t, encoded.Get(&batch))
		return batch
	}

	manifest, err := activities.BuildProjectDeletionManifest(ctx, input)
	require.NoError(t, err)
	require.Equal(t, 3, manifest.ObjectCount)
	var retryManifestSize int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT byte_size
		FROM project_deletion_objects
		WHERE request_id = $1 AND storage_key = $2
	`, requestID, retryKey).Scan(&retryManifestSize))
	require.Equal(t, int64(256), retryManifestSize)

	firstBatch := executeStorageBatch()
	require.Equal(t, 3, firstBatch.SelectedCount)
	require.Equal(t, 1, firstBatch.DeletedCount)
	require.Equal(t, 1, firstBatch.SkippedSharedCount)
	require.Equal(t, 1, firstBatch.FailedCount)
	require.Equal(t, 1, firstBatch.TotalFailedCount)

	require.NoError(t, activities.FailProjectDeletion(
		ctx,
		input,
		CodeProjectDeletionStorageFailed,
		"temporary object storage failure",
		true,
	))
	_, err = pool.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'requested',
		    retry_count = retry_count + 1,
		    error_code = NULL,
		    error_message = NULL,
		    drain_deadline_at = now() + interval '15 minutes',
		    updated_at = now()
		WHERE id = $1
	`, requestID)
	require.NoError(t, err)

	_, err = activities.PrepareProjectDeletion(ctx, input)
	require.NoError(t, err)
	require.NoError(t, activities.CancelProjectProviderTasks(ctx, input))
	drain, err := activities.CheckProjectDeletionDrain(ctx, input)
	require.NoError(t, err)
	require.True(t, drain.Drained)

	manifest, err = activities.BuildProjectDeletionManifest(ctx, input)
	require.NoError(t, err)
	require.Equal(t, 3, manifest.ObjectCount)
	secondBatch := executeStorageBatch()
	require.Equal(t, 1, secondBatch.SelectedCount)
	require.Equal(t, 1, secondBatch.DeletedCount)
	require.Zero(t, secondBatch.PendingCount)
	require.Zero(t, secondBatch.InFlightCount)
	require.Zero(t, secondBatch.TotalFailedCount)

	output, err := activities.CommitProjectDeletion(ctx, input)
	require.NoError(t, err)
	require.Equal(t, projectDeletionStatusCompleted, output.Status)
	require.Equal(t, 2, output.DeletedObjectCount)
	require.Equal(t, 1, output.SkippedObjectCount)

	storageClient.mu.Lock()
	deleted := append([]string(nil), storageClient.deleted...)
	storageClient.mu.Unlock()
	require.ElementsMatch(t, []string{missingKey, retryKey}, deleted)
	require.NotContains(t, deleted, sharedKey)

	var projectCount int
	var siblingArtifactCount int
	var requestStatus string
	var expiresAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM projects WHERE id = $1),
			(SELECT count(*) FROM artifacts WHERE project_id = $2 AND storage_key = $3),
			(SELECT status FROM project_deletion_requests WHERE id = $4),
			(SELECT expires_at FROM project_deletion_requests WHERE id = $4)
	`, projectID, siblingProjectID, sharedKey, requestID).Scan(
		&projectCount,
		&siblingArtifactCount,
		&requestStatus,
		&expiresAt,
	))
	require.Zero(t, projectCount)
	require.Equal(t, 1, siblingArtifactCount)
	require.Equal(t, projectDeletionStatusCompleted, requestStatus)
	require.NotNil(t, expiresAt)

	_, err = pool.Exec(ctx, `UPDATE project_deletion_requests SET expires_at = now() - interval '1 second' WHERE id = $1`, requestID)
	require.NoError(t, err)
	purged, err := PurgeExpiredProjectDeletionRequests(ctx, pool, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), purged)
	var requestCount int
	var objectCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM project_deletion_requests WHERE id = $1),
			(SELECT count(*) FROM project_deletion_objects WHERE request_id = $1)
	`, requestID).Scan(&requestCount, &objectCount))
	require.Zero(t, requestCount)
	require.Zero(t, objectCount)
}

func TestProjectDeletionCommitRemovesCommerceReferencesBeforeArtifactsIntegration(t *testing.T) {
	ctx, pool, organizationID, seedProjectID, _ := seedNodeRunIntegrationTest(t)
	var workspaceID string
	var requestedBy string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT workspace_id::text, created_by::text
		FROM projects
		WHERE id = $1
	`, seedProjectID).Scan(&workspaceID, &requestedBy))

	var projectID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, project_kind, project_type,
			content_type, created_by, video_production_state, video_production_locked,
			lifecycle_status, deletion_revision, deletion_requested_at
		)
		VALUES (
			$1, $2, 'Commerce Project Deletion Fixture', 'commerce_video', 'commerce_video',
			NULL, $3, 'unconfigured', true, 'deleting', 1, now()
		)
		RETURNING id::text
	`, organizationID, workspaceID, requestedBy).Scan(&projectID))

	var productID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, status, created_by)
		VALUES ($1, $2, 'draft', $3)
		RETURNING id::text
	`, organizationID, projectID, requestedBy).Scan(&productID))
	productFactsHash := strings.Repeat("b", 64)
	var productVersionID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_versions(
			organization_id, project_id, product_id, version, name,
			facts_snapshot, facts_hash, created_by
		)
		VALUES (
			$1, $2, $3, 1, 'Deletion Fixture Product',
			'{"name":"Deletion Fixture Product"}'::jsonb, $4, $5
		)
		RETURNING id::text
	`, organizationID, projectID, productID, productFactsHash, requestedBy).Scan(&productVersionID))
	_, err := pool.Exec(ctx, `
		UPDATE commerce_products
		SET current_version_id = $2, status = 'ready'
		WHERE id = $1
	`, productID, productVersionID)
	require.NoError(t, err)

	storageKey := "projects/" + projectID + "/product-reference.png"
	contentHash := strings.Repeat("c", 64)
	var artifactID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'commerce_product_reference', $3, 'image/png', $4, '{}'::jsonb, $5)
		RETURNING id::text
	`, organizationID, projectID, storageKey, contentHash, requestedBy).Scan(&artifactID))
	var mediaFileID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', 256, 16, 16, $5, '{}'::jsonb, $6)
		RETURNING id::text
	`, organizationID, projectID, artifactID, storageKey, contentHash, requestedBy).Scan(&mediaFileID))
	var referenceID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_references(
			organization_id, project_id, product_id, artifact_id, media_file_id,
			reference_role, ordinal, is_primary, width, height, mime_type,
			content_hash, quality_review, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5,
			'primary', 0, true, 16, 16, 'image/png',
			$6, '{}'::jsonb, $7
		)
		RETURNING id::text
	`, organizationID, projectID, productID, artifactID, mediaFileID, contentHash, requestedBy).Scan(&referenceID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_uploads(
			organization_id, project_id, product_id, storage_key,
			requested_mime_type, original_file_name, status, idempotency_key,
			reference_id, created_by, expires_at, completed_at
		)
		VALUES (
			$1, $2, $3, $4,
			'image/png', 'product-reference.png', 'completed', $5,
			$6, $7, now() + interval '1 hour', now()
		)
		RETURNING id::text
	`, organizationID, projectID, productID, storageKey,
		"commerce-reference-upload-"+projectID, referenceID, requestedBy).Scan(new(string)))
	var referencePackID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_packs(
			organization_id, project_id, product_id, product_version_id,
			product_facts_hash, reference_set_hash, pack_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text
	`, organizationID, projectID, productID, productVersionID, productFactsHash,
		strings.Repeat("e", 64), strings.Repeat("f", 64), requestedBy).Scan(&referencePackID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_pack_items(
			organization_id, project_id, product_id, product_version_id,
			reference_pack_id, product_reference_id, ordinal, reference_role,
			artifact_id, media_file_id, content_hash
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, 0, 'primary',
			$7, $8, $9
		)
		RETURNING id::text
	`, organizationID, projectID, productID, productVersionID,
		referencePackID, referenceID, artifactID, mediaFileID, contentHash).Scan(new(string)))

	_, err = pool.Exec(ctx, `DELETE FROM artifacts WHERE id = $1`, artifactID)
	require.Error(t, err, "project-owned cross references must remain immediate outside the deletion transaction")

	requestID := uuid.NewString()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO project_deletion_requests(
			id, organization_id, workspace_id, project_id, project_name,
			project_revision, deletion_revision, status, impact_snapshot, impact_hash,
			temporal_workflow_id, idempotency_key, requested_by, drain_deadline_at
		)
		SELECT
			$1, organization_id, workspace_id, id, name,
			revision, deletion_revision, 'deleting_storage', '{}'::jsonb, $2,
			$3, $4, $5, now() + interval '15 minutes'
		FROM projects
		WHERE id = $6
		RETURNING id::text
	`, requestID, strings.Repeat("d", 64), "project-deletion-"+requestID,
		"commerce-reference-delete-"+requestID, requestedBy, projectID).Scan(new(string)))

	output, err := (Activities{db: pool}).CommitProjectDeletion(ctx, ProjectDeletionInput{
		OrganizationID:   organizationID,
		WorkspaceID:      workspaceID,
		ProjectID:        projectID,
		RequestID:        requestID,
		DeletionRevision: 1,
		RequestedBy:      requestedBy,
	})
	require.NoError(t, err)
	require.Equal(t, projectDeletionStatusCompleted, output.Status)

	var projectCount int
	var referenceCount int
	var referencePackCount int
	var artifactCount int
	var mediaFileCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM projects WHERE id = $1),
			(SELECT count(*) FROM commerce_product_references WHERE project_id = $1),
			(SELECT count(*) FROM commerce_product_reference_packs WHERE project_id = $1),
			(SELECT count(*) FROM artifacts WHERE project_id = $1),
			(SELECT count(*) FROM media_files WHERE project_id = $1)
	`, projectID).Scan(
		&projectCount,
		&referenceCount,
		&referencePackCount,
		&artifactCount,
		&mediaFileCount,
	))
	require.Zero(t, projectCount)
	require.Zero(t, referenceCount)
	require.Zero(t, referencePackCount)
	require.Zero(t, artifactCount)
	require.Zero(t, mediaFileCount)
}
