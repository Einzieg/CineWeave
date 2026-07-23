package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) ClaimProductReferenceUpload(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productID string,
	setupSessionID *string,
	storageKey string,
	mimeType string,
	fileName string,
	idempotencyKey string,
	createdBy string,
	expiresAt time.Time,
) (ProductReferenceUpload, bool, error) {
	item, err := r.LoadProductReferenceUploadByKey(ctx, tx, organizationID, idempotencyKey, true)
	if err == nil {
		if item.ProjectID != projectID || item.ProductID != productID || item.RequestedMimeType != mimeType || item.OriginalFileName != fileName {
			return ProductReferenceUpload{}, false, Error{Code: CodeIdempotencyKeyReused, Message: "上传请求标识已用于其他商品图片"}
		}
		return item, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ProductReferenceUpload{}, false, err
	}
	item, err = scanProductReferenceUpload(tx.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_uploads(
			organization_id, project_id, product_id, setup_session_id,
			storage_key, requested_mime_type, original_file_name,
			idempotency_key, created_by, expires_at
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10)
		RETURNING `+productReferenceUploadColumns,
		organizationID, projectID, productID, optionalString(setupSessionID), storageKey,
		mimeType, fileName, idempotencyKey, createdBy, expiresAt))
	return item, false, err
}

func (r *Repository) LoadProductReferenceUpload(ctx context.Context, db rowQuerier, organizationID, projectID, uploadID string, lock bool) (ProductReferenceUpload, error) {
	query := `SELECT ` + productReferenceUploadColumns + ` FROM commerce_product_reference_uploads
		WHERE id = $1 AND organization_id = $2 AND project_id = $3`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanProductReferenceUpload(db.QueryRow(ctx, query, uploadID, organizationID, projectID))
}

func (r *Repository) LoadProductReferenceUploadByKey(ctx context.Context, db rowQuerier, organizationID, idempotencyKey string, lock bool) (ProductReferenceUpload, error) {
	query := `SELECT ` + productReferenceUploadColumns + ` FROM commerce_product_reference_uploads
		WHERE organization_id = $1 AND idempotency_key = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanProductReferenceUpload(db.QueryRow(ctx, query, organizationID, idempotencyKey))
}

func (r *Repository) CompleteProductReferenceUpload(ctx context.Context, tx pgx.Tx, upload ProductReferenceUpload, referenceID string) (ProductReferenceUpload, error) {
	return scanProductReferenceUpload(tx.QueryRow(ctx, `
		UPDATE commerce_product_reference_uploads
		SET status = 'completed', reference_id = $2, completed_at = now()
		WHERE id = $1 AND status = 'pending'
		RETURNING `+productReferenceUploadColumns,
		upload.ID, referenceID))
}

func (r *Repository) AbandonProductReferenceUpload(ctx context.Context, tx pgx.Tx, upload ProductReferenceUpload) (ProductReferenceUpload, error) {
	return scanProductReferenceUpload(tx.QueryRow(ctx, `
		UPDATE commerce_product_reference_uploads
		SET status = 'abandoned', abandoned_at = now()
		WHERE id = $1 AND status = 'pending'
		RETURNING `+productReferenceUploadColumns,
		upload.ID))
}

func (r *Repository) AbandonPendingProductUploads(ctx context.Context, tx pgx.Tx, organizationID, projectID, setupSessionID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE commerce_product_reference_uploads
		SET status = 'abandoned', abandoned_at = now()
		WHERE organization_id = $1 AND project_id = $2 AND setup_session_id = $3
		  AND status = 'pending'
		RETURNING storage_key
	`, organizationID, projectID, setupSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

const productReferenceUploadColumns = `
	id::text, organization_id::text, project_id::text, product_id::text,
	setup_session_id::text, storage_key, requested_mime_type, original_file_name,
	status, idempotency_key, reference_id::text, created_at, expires_at,
	completed_at, abandoned_at`

func scanProductReferenceUpload(row scanRow) (ProductReferenceUpload, error) {
	var item ProductReferenceUpload
	var setupSessionID, referenceID pgtype.Text
	var completedAt, abandonedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&setupSessionID, &item.StorageKey, &item.RequestedMimeType, &item.OriginalFileName,
		&item.Status, &item.IdempotencyKey, &referenceID, &item.CreatedAt, &item.ExpiresAt,
		&completedAt, &abandonedAt,
	)
	item.SetupSessionID = textPointer(setupSessionID)
	item.ReferenceID = textPointer(referenceID)
	item.CompletedAt = timestampPointer(completedAt)
	item.AbandonedAt = timestampPointer(abandonedAt)
	return item, err
}
