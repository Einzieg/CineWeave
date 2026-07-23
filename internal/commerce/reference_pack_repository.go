package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type catalogQuerier interface {
	rowQuerier
	rowsQuerier
}

func (r *Repository) ListProductReferencePacks(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
) ([]ProductReferencePack, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       product_version_id::text, product_facts_hash, reference_set_hash, pack_hash,
		       status, workflow_run_id::text, created_at, stale_at, archived_at
		FROM commerce_product_reference_packs
		WHERE organization_id = $1
		  AND project_id = $2
		  AND ($3 = 'all' OR status = $3)
		ORDER BY created_at DESC, id DESC
	`, organizationID, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductReferencePack, 0)
	for rows.Next() {
		item, err := scanProductReferencePack(rows)
		if err != nil {
			return nil, err
		}
		item.Items, err = r.ListProductReferencePackItems(ctx, db, organizationID, projectID, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadProductReferencePack(
	ctx context.Context,
	db catalogQuerier,
	organizationID string,
	projectID string,
	packID string,
) (ProductReferencePack, error) {
	item, err := scanProductReferencePack(db.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       product_version_id::text, product_facts_hash, reference_set_hash, pack_hash,
		       status, workflow_run_id::text, created_at, stale_at, archived_at
		FROM commerce_product_reference_packs
		WHERE organization_id = $1 AND project_id = $2 AND id = $3
	`, organizationID, projectID, packID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductReferencePack{}, Error{Code: CodeProductRequired, Message: "商品引用包不存在", Cause: err}
	}
	if err != nil {
		return ProductReferencePack{}, err
	}
	item.Items, err = r.ListProductReferencePackItems(ctx, db, organizationID, projectID, item.ID)
	return item, err
}

func (r *Repository) ListProductReferencePackItems(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	packID string,
) ([]ProductReferencePackItem, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, reference_pack_id::text, product_reference_id::text, ordinal,
		       reference_role, artifact_id::text, media_file_id::text, content_hash, created_at
		FROM commerce_product_reference_pack_items
		WHERE organization_id = $1 AND project_id = $2 AND reference_pack_id = $3
		ORDER BY ordinal, id
	`, organizationID, projectID, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductReferencePackItem, 0)
	for rows.Next() {
		var item ProductReferencePackItem
		if err := rows.Scan(&item.ID, &item.ReferencePackID, &item.ProductReferenceID, &item.Ordinal,
			&item.ReferenceRole, &item.ArtifactID, &item.MediaFileID, &item.ContentHash, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanProductReferencePack(row scanRow) (ProductReferencePack, error) {
	var item ProductReferencePack
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ProductVersionID, &item.ProductFactsHash, &item.ReferenceSetHash, &item.PackHash,
		&item.Status, &item.WorkflowRunID, &item.CreatedAt, &item.StaleAt, &item.ArchivedAt)
	return item, err
}
