package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) LoadProduct(ctx context.Context, db rowQuerier, organizationID, projectID string, lock bool) (Product, error) {
	query := productSelectSQL + ` WHERE product.organization_id = $1 AND product.project_id = $2`
	if lock {
		query += " FOR UPDATE OF product"
	}
	return scanProduct(db.QueryRow(ctx, query, organizationID, projectID))
}

func (r *Repository) LockProduct(ctx context.Context, tx pgx.Tx, organizationID, projectID string) (Product, bool, error) {
	item, err := r.LoadProduct(ctx, tx, organizationID, projectID, true)
	if err == pgx.ErrNoRows {
		return Product{}, false, nil
	}
	return item, err == nil, err
}

func (r *Repository) InsertProduct(ctx context.Context, tx pgx.Tx, organizationID, projectID, createdBy string, metadata json.RawMessage) (Product, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, metadata, created_by)
		VALUES ($1, $2, $3, $4)
	`, organizationID, projectID, metadata, createdBy)
	if err != nil {
		return Product{}, err
	}
	return r.LoadProduct(ctx, tx, organizationID, projectID, true)
}

func (r *Repository) InsertProductVersion(ctx context.Context, tx pgx.Tx, product Product, input ProductVersionInput, facts json.RawMessage, factsHash, createdBy string) (ProductVersion, error) {
	var nextVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) + 1 FROM commerce_product_versions WHERE product_id = $1`, product.ID).Scan(&nextVersion); err != nil {
		return ProductVersion{}, err
	}
	return scanProductVersion(tx.QueryRow(ctx, `
		INSERT INTO commerce_product_versions(
			organization_id, project_id, product_id, version, name, brand,
			selling_points, immutable_features, prohibited_claims,
			facts_snapshot, facts_hash, source_version_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id::text, organization_id::text, project_id::text, product_id::text,
		          version, name, brand, selling_points, immutable_features,
		          prohibited_claims, facts_snapshot, facts_hash, source_version_id::text, created_at
	`, product.OrganizationID, product.ProjectID, product.ID, nextVersion, input.Name, input.Brand,
		input.SellingPoints, input.ImmutableFeatures, input.ProhibitedClaims, facts, factsHash,
		product.CurrentVersionID, createdBy))
}

func (r *Repository) ActivateProductVersion(ctx context.Context, tx pgx.Tx, product Product, version ProductVersion, metadata json.RawMessage) (Product, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET current_version_id = $2, metadata = $3, revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, product.ID, version.ID, metadata); err != nil {
		return Product{}, err
	}
	return r.LoadProduct(ctx, tx, product.OrganizationID, product.ProjectID, true)
}

func (r *Repository) CountActiveUnitGenerations(ctx context.Context, db rowQuerier, organizationID, projectID, productID string) (int, error) {
	var count int
	err := db.QueryRow(ctx, `
		SELECT count(*)
		FROM commerce_script_units
		WHERE organization_id = $1 AND project_id = $2 AND product_id = $3
		  AND active_unit_generation_id IS NOT NULL AND status <> 'archived'
	`, organizationID, projectID, productID).Scan(&count)
	return count, err
}

func (r *Repository) RetireLegacyUnitGenerationsForProductUpdate(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productID string,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_storyboard_plans plan
		SET status = 'stale',
		    active = false,
		    stale_state = 'upstream_changed',
		    stale_at = COALESCE(stale_at, now())
		WHERE plan.organization_id = $1
		  AND plan.project_id = $2
		  AND plan.script_unit_generation_id IN (
		      SELECT unit.active_unit_generation_id
		      FROM commerce_script_units unit
		      WHERE unit.organization_id = $1
		        AND unit.project_id = $2
		        AND unit.product_id = $3
		        AND unit.status <> 'archived'
		        AND unit.active_unit_generation_id IS NOT NULL
		  )
		  AND plan.status <> 'archived'
	`, organizationID, projectID, productID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations generation
		SET status = 'archived', archived_at = COALESCE(archived_at, now())
		WHERE generation.id IN (
		      SELECT unit.active_unit_generation_id
		      FROM commerce_script_units unit
		      WHERE unit.organization_id = $1
		        AND unit.project_id = $2
		        AND unit.product_id = $3
		        AND unit.status <> 'archived'
		        AND unit.active_unit_generation_id IS NOT NULL
		  )
		  AND generation.status = 'active'
	`, organizationID, projectID, productID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET active_unit_generation_id = NULL,
		    current_localization_id = NULL,
		    status = 'draft',
		    revision = revision + 1,
		    updated_at = now()
		WHERE organization_id = $1
		  AND project_id = $2
		  AND product_id = $3
		  AND status <> 'archived'
		  AND active_unit_generation_id IS NOT NULL
	`, organizationID, projectID, productID)
	return err
}

func (r *Repository) ListProductVersions(ctx context.Context, db rowsQuerier, organizationID, projectID string) ([]ProductVersion, error) {
	rows, err := db.Query(ctx, productVersionSelectSQL+`
		WHERE organization_id = $1 AND project_id = $2
		ORDER BY version DESC
	`, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductVersion, 0)
	for rows.Next() {
		item, err := scanProductVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadProductVersion(ctx context.Context, db rowQuerier, organizationID, projectID, versionID string) (ProductVersion, error) {
	return scanProductVersion(db.QueryRow(ctx, productVersionSelectSQL+`
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`, versionID, organizationID, projectID))
}

func (r *Repository) InsertProductReference(ctx context.Context, tx pgx.Tx, product Product, params CreateProductReferenceParams) (ProductReference, error) {
	if existing, found, err := r.findActiveProductReferenceByHash(ctx, tx, product.ID, params.ContentHash); err != nil {
		return ProductReference{}, err
	} else if found {
		return existing, nil
	}
	var nextOrdinal int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(ordinal), -1) + 1
		FROM commerce_product_references
		WHERE product_id = $1 AND status = 'active'
	`, product.ID).Scan(&nextOrdinal); err != nil {
		return ProductReference{}, err
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM commerce_product_references WHERE product_id = $1 AND status = 'active'`, product.ID).Scan(&activeCount); err != nil {
		return ProductReference{}, err
	}
	setPrimary := params.SetPrimary || activeCount == 0
	if setPrimary {
		if _, err := tx.Exec(ctx, `UPDATE commerce_product_references SET is_primary = false, revision = revision + 1, updated_at = now() WHERE product_id = $1 AND status = 'active' AND is_primary`, product.ID); err != nil {
			return ProductReference{}, err
		}
	}
	role := params.ReferenceRole
	if setPrimary {
		role = "primary"
	}
	var artifactID string
	metadata := map[string]any{"commerceProductId": product.ID, "contentHash": params.ContentHash}
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, 'commerce_product_reference', $3, $4, $5, $6)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, params.StorageKey, params.MimeType, mustJSON(metadata), params.CreatedBy).Scan(&artifactID); err != nil {
		return ProductReference{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, artifactID, params.StorageKey, params.MimeType,
		params.ByteSize, params.Width, params.Height, params.ContentHash, mustJSON(metadata), params.CreatedBy).Scan(&mediaFileID); err != nil {
		return ProductReference{}, err
	}
	item, err := scanProductReference(tx.QueryRow(ctx, productReferenceInsertSQL, params.OrganizationID, params.ProjectID,
		product.ID, artifactID, mediaFileID, role, nextOrdinal, setPrimary, params.Width, params.Height,
		params.MimeType, params.ContentHash, params.QualityReview, params.CreatedBy))
	if err != nil {
		return ProductReference{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET status = CASE WHEN current_version_id IS NOT NULL THEN 'ready' ELSE 'draft' END,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, product.ID); err != nil {
		return ProductReference{}, err
	}
	return item, nil
}

func (r *Repository) findActiveProductReferenceByHash(ctx context.Context, db rowQuerier, productID, hash string) (ProductReference, bool, error) {
	item, err := scanProductReference(db.QueryRow(ctx, productReferenceSelectSQL+`
		WHERE product_id = $1 AND content_hash = $2 AND status = 'active'
	`, productID, hash))
	if err == pgx.ErrNoRows {
		return ProductReference{}, false, nil
	}
	return item, err == nil, err
}

func (r *Repository) ListProductReferences(ctx context.Context, db rowsQuerier, organizationID, projectID, status string) ([]ProductReference, error) {
	query := productReferenceSelectSQL + ` WHERE organization_id = $1 AND project_id = $2`
	args := []any{organizationID, projectID}
	if status != "all" {
		query += ` AND status = $3`
		args = append(args, status)
	}
	query += ` ORDER BY status, ordinal, created_at`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductReference, 0)
	for rows.Next() {
		item, err := scanProductReference(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpdateProductReference(ctx context.Context, tx pgx.Tx, organizationID, projectID, referenceID string, expectedRevision int64, role string, ordinal *int, setPrimary *bool) (ProductReference, error) {
	current, err := scanProductReference(tx.QueryRow(ctx, productReferenceSelectSQL+`
		WHERE id = $1 AND organization_id = $2 AND project_id = $3 FOR UPDATE
	`, referenceID, organizationID, projectID))
	if err != nil {
		return ProductReference{}, err
	}
	if current.Status == "archived" {
		return ProductReference{}, Error{Code: CodeProductVersionStale, Message: "商品图片已归档"}
	}
	if current.Revision != expectedRevision {
		return ProductReference{}, Error{Code: CodeProductVersionStale, Message: "商品图片已变化，请刷新后重试"}
	}
	if role == "" {
		role = current.ReferenceRole
	}
	targetOrdinal := current.Ordinal
	if ordinal != nil {
		if *ordinal < 0 {
			return ProductReference{}, errors.New("product reference ordinal is invalid")
		}
		targetOrdinal = *ordinal
		if targetOrdinal != current.Ordinal {
			var temporaryOrdinal int
			if err := tx.QueryRow(ctx, `
				SELECT COALESCE(max(ordinal), -1) + 1
				FROM commerce_product_references
				WHERE product_id = $1 AND status = 'active'
			`, current.ProductID).Scan(&temporaryOrdinal); err != nil {
				return ProductReference{}, err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE commerce_product_references
				SET ordinal = $2, updated_at = now()
				WHERE id = $1
			`, current.ID, temporaryOrdinal); err != nil {
				return ProductReference{}, err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE commerce_product_references
				SET ordinal = $2, revision = revision + 1, updated_at = now()
				WHERE product_id = $1 AND status = 'active' AND ordinal = $3 AND id <> $4
			`, current.ProductID, current.Ordinal, targetOrdinal, current.ID); err != nil {
				return ProductReference{}, err
			}
		}
	}
	primary := current.IsPrimary
	if setPrimary != nil {
		if current.IsPrimary && !*setPrimary {
			return ProductReference{}, Error{Code: CodeProductPrimaryImage, Message: "请先将其他商品图设为主图"}
		}
		primary = *setPrimary
	}
	if primary {
		role = "primary"
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_product_references
			SET is_primary = false,
			    reference_role = CASE WHEN reference_role = 'primary' THEN 'other' ELSE reference_role END,
			    revision = revision + 1, updated_at = now()
			WHERE product_id = $1 AND status = 'active' AND is_primary AND id <> $2
		`, current.ProductID, current.ID); err != nil {
			return ProductReference{}, err
		}
	}
	item, err := scanProductReference(tx.QueryRow(ctx, `
		UPDATE commerce_product_references
		SET reference_role = $4, ordinal = $5, is_primary = $6,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3 AND revision = $7
		RETURNING id::text, organization_id::text, project_id::text, product_id::text,
		          artifact_id::text, media_file_id::text, reference_role, ordinal,
		          is_primary, status, width, height, mime_type, content_hash,
		          quality_review, revision, created_at, updated_at, archived_at
	`, referenceID, organizationID, projectID, role, targetOrdinal, primary, expectedRevision))
	if err != nil {
		return ProductReference{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE commerce_products SET revision = revision + 1, updated_at = now() WHERE id = $1`, current.ProductID); err != nil {
		return ProductReference{}, err
	}
	return item, nil
}

func (r *Repository) ArchiveProductReference(ctx context.Context, tx pgx.Tx, organizationID, projectID, referenceID string, expectedRevision int64) (ProductReference, error) {
	current, err := scanProductReference(tx.QueryRow(ctx, productReferenceSelectSQL+`
		WHERE id = $1 AND organization_id = $2 AND project_id = $3 FOR UPDATE
	`, referenceID, organizationID, projectID))
	if err != nil {
		return ProductReference{}, err
	}
	if current.Status == "archived" {
		return current, nil
	}
	if current.Revision != expectedRevision {
		return ProductReference{}, Error{Code: CodeProductVersionStale, Message: "商品图片已变化，请刷新后重试"}
	}
	item, err := scanProductReference(tx.QueryRow(ctx, `
		UPDATE commerce_product_references
		SET status = 'archived', is_primary = false, archived_at = now(),
		    revision = revision + 1, updated_at = now()
		WHERE id = $1 AND revision = $2
		RETURNING id::text, organization_id::text, project_id::text, product_id::text,
		          artifact_id::text, media_file_id::text, reference_role, ordinal,
		          is_primary, status, width, height, mime_type, content_hash,
		          quality_review, revision, created_at, updated_at, archived_at
	`, current.ID, expectedRevision))
	if err != nil {
		return ProductReference{}, err
	}
	if current.IsPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_product_references
			SET is_primary = true, reference_role = 'primary', revision = revision + 1, updated_at = now()
			WHERE id = (
				SELECT id FROM commerce_product_references
				WHERE product_id = $1 AND status = 'active'
				ORDER BY ordinal, created_at LIMIT 1
			)
		`, current.ProductID); err != nil {
			return ProductReference{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products product
		SET status = CASE WHEN EXISTS (
			SELECT 1 FROM commerce_product_references reference
			WHERE reference.product_id = product.id AND reference.status = 'active' AND reference.is_primary
		) AND product.current_version_id IS NOT NULL THEN 'ready' ELSE 'draft' END,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, current.ProductID); err != nil {
		return ProductReference{}, err
	}
	return item, nil
}

const productSelectSQL = `
	SELECT product.id::text, product.organization_id::text, product.project_id::text,
	       product.current_version_id::text, product.status, product.revision,
	       product.script_units_revision, product.metadata, product.created_at, product.updated_at,
	       version.id::text, version.organization_id::text, version.project_id::text,
	       version.product_id::text, COALESCE(version.version, 0), COALESCE(version.name, ''),
	       COALESCE(version.brand, ''), COALESCE(version.selling_points, '[]'::jsonb),
	       COALESCE(version.immutable_features, '{}'::jsonb),
	       COALESCE(version.prohibited_claims, '[]'::jsonb),
	       COALESCE(version.facts_snapshot, '{}'::jsonb), COALESCE(version.facts_hash, ''),
	       version.source_version_id::text, COALESCE(version.created_at, product.created_at)
	FROM commerce_products product
	LEFT JOIN commerce_product_versions version ON version.id = product.current_version_id`

const productVersionSelectSQL = `
	SELECT id::text, organization_id::text, project_id::text, product_id::text,
	       version, name, brand, selling_points, immutable_features, prohibited_claims,
	       facts_snapshot, facts_hash, source_version_id::text, created_at
	FROM commerce_product_versions`

const productReferenceSelectSQL = `
	SELECT id::text, organization_id::text, project_id::text, product_id::text,
	       artifact_id::text, media_file_id::text, reference_role, ordinal,
	       is_primary, status, width, height, mime_type, content_hash,
	       quality_review, revision, created_at, updated_at, archived_at
	FROM commerce_product_references`

const productReferenceInsertSQL = `
	INSERT INTO commerce_product_references(
		organization_id, project_id, product_id, artifact_id, media_file_id,
		reference_role, ordinal, is_primary, width, height, mime_type,
		content_hash, quality_review, created_by
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	RETURNING id::text, organization_id::text, project_id::text, product_id::text,
	          artifact_id::text, media_file_id::text, reference_role, ordinal,
	          is_primary, status, width, height, mime_type, content_hash,
	          quality_review, revision, created_at, updated_at, archived_at`

type scanRow interface{ Scan(...any) error }

func scanProduct(row scanRow) (Product, error) {
	var item Product
	var currentVersionID pgtype.Text
	var version ProductVersion
	var versionID, versionOrganizationID, versionProjectID, versionProductID pgtype.Text
	var sourceVersionID pgtype.Text
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &currentVersionID,
		&item.Status, &item.Revision, &item.ScriptUnitsRevision, &item.Metadata,
		&item.CreatedAt, &item.UpdatedAt,
		&versionID, &versionOrganizationID, &versionProjectID, &versionProductID,
		&version.Version, &version.Name, &version.Brand, &version.SellingPoints,
		&version.ImmutableFeatures, &version.ProhibitedClaims, &version.FactsSnapshot,
		&version.FactsHash, &sourceVersionID, &version.CreatedAt,
	)
	if err != nil {
		return Product{}, err
	}
	item.CurrentVersionID = textPointer(currentVersionID)
	if versionID.Valid {
		version.ID = versionID.String
		version.OrganizationID = versionOrganizationID.String
		version.ProjectID = versionProjectID.String
		version.ProductID = versionProductID.String
		version.SourceVersionID = textPointer(sourceVersionID)
		item.CurrentVersion = &version
	}
	return item, nil
}

func scanProductVersion(row scanRow) (ProductVersion, error) {
	var item ProductVersion
	var sourceVersionID pgtype.Text
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.Version, &item.Name, &item.Brand, &item.SellingPoints,
		&item.ImmutableFeatures, &item.ProhibitedClaims, &item.FactsSnapshot,
		&item.FactsHash, &sourceVersionID, &item.CreatedAt,
	)
	item.SourceVersionID = textPointer(sourceVersionID)
	return item, err
}

func scanProductReference(row scanRow) (ProductReference, error) {
	var item ProductReference
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ArtifactID, &item.MediaFileID, &item.ReferenceRole, &item.Ordinal,
		&item.IsPrimary, &item.Status, &item.Width, &item.Height, &item.MimeType,
		&item.ContentHash, &item.QualityReview, &item.Revision,
		&item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt,
	)
	return item, err
}
