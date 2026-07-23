package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListActiveProductRebuildSeeds(ctx context.Context, tx pgx.Tx, production ProductionContext, productID string) ([]productRebuildSeed, error) {
	rows, err := tx.Query(ctx, `
		SELECT unit.id::text, unit.unit_no, unit.title, unit.revision,
		       generation.id::text, generation.unit_generation_no,
		       generation.reference_pack_id::text, generation.source_script_version_id::text,
		       generation.localization_id::text, generation.unit_configuration_snapshot
		FROM commerce_script_units unit
		JOIN commerce_script_unit_generations generation
		  ON generation.id = unit.active_unit_generation_id
		 AND generation.script_unit_id = unit.id
		 AND generation.product_id = unit.product_id
		WHERE unit.organization_id = $1 AND unit.project_id = $2 AND unit.product_id = $3
		  AND unit.status <> 'archived' AND generation.status = 'active'
		  AND generation.project_production_generation_id = $4
		  AND generation.commerce_workflow_binding_id = $5
		  AND generation.commerce_workflow_binding_revision = $6
		ORDER BY unit.unit_no
		FOR UPDATE OF unit, generation
	`, production.OrganizationID, production.ProjectID, productID, production.Generation.ID,
		production.CommerceBinding.ID, production.CommerceBinding.Revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]productRebuildSeed, 0)
	for rows.Next() {
		var item productRebuildSeed
		if err := rows.Scan(
			&item.ScriptUnitID, &item.UnitNo, &item.Title, &item.ScriptUnitRevision,
			&item.SourceGenerationID, &item.SourceGenerationNo, &item.SourceReferencePackID,
			&item.SourceScriptVersionID, &item.LocalizationID, &item.ConfigurationSnapshot,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) InsertPlannedProductRebuild(
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	product Product,
	targetVersionID string,
	referenceSetHash string,
	impactSnapshot json.RawMessage,
	token string,
	requestedBy string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_product_rebuilds(
			organization_id, project_id, product_id, project_production_generation_id,
			source_product_version_id, target_product_version_id, target_reference_set_hash,
			impact_snapshot, impact_token, expected_product_revision, status,
			idempotency_key, requested_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'planned', $11, $12)
	`, production.OrganizationID, production.ProjectID, product.ID, production.Generation.ID,
		*product.CurrentVersionID, targetVersionID, referenceSetHash, impactSnapshot, token,
		product.Revision, "impact:"+token, requestedBy)
	return err
}

func (r *Repository) LockProductRebuildByToken(ctx context.Context, tx pgx.Tx, organizationID, projectID, token string) (persistedProductRebuild, error) {
	var item persistedProductRebuild
	err := tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       project_production_generation_id::text, source_product_version_id::text,
		       target_product_version_id::text, target_reference_set_hash,
		       impact_snapshot, impact_token, expected_product_revision, status
		FROM commerce_product_rebuilds
		WHERE organization_id = $1 AND project_id = $2 AND impact_token = $3
		FOR UPDATE
	`, organizationID, projectID, token).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ProjectGenerationID, &item.SourceProductVersionID, &item.TargetProductVersionID,
		&item.TargetReferenceSetHash, &item.ImpactSnapshot, &item.ImpactToken,
		&item.ExpectedProductRevision, &item.Status,
	)
	return item, err
}

func (r *Repository) MarkProductRebuildPreparing(ctx context.Context, tx pgx.Tx, organizationID, rebuildID, executionKey string) error {
	var existingID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM commerce_product_rebuilds
		WHERE organization_id = $1 AND idempotency_key = $2 AND id <> $3
	`, organizationID, executionKey, rebuildID).Scan(&existingID)
	if err == nil {
		return Error{Code: CodeIdempotencyKeyReused, Message: "商品换版请求标识已被其他任务使用"}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE commerce_product_rebuilds
		SET status = 'preparing', idempotency_key = $2, started_at = now()
		WHERE id = $1 AND organization_id = $3 AND status = 'planned'
	`, rebuildID, executionKey, organizationID)
	return err
}

func (r *Repository) InsertProductReferencePack(ctx context.Context, tx pgx.Tx, product Product, version ProductVersion, references []ProductReference, referenceSetHash, createdBy string) (string, error) {
	packSnapshot := map[string]any{
		"productVersionId": version.ID,
		"productFactsHash": version.FactsHash,
		"referenceSetHash": referenceSetHash,
	}
	packRaw, err := json.Marshal(packSnapshot)
	if err != nil {
		return "", err
	}
	packHash, err := hashJSON(packRaw)
	if err != nil {
		return "", err
	}
	var packID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM commerce_product_reference_packs WHERE product_id = $1 AND pack_hash = $2`, product.ID, packHash).Scan(&packID)
	if err == nil {
		return packID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_product_reference_packs(
			organization_id, project_id, product_id, product_version_id,
			product_facts_hash, reference_set_hash, pack_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text
	`, product.OrganizationID, product.ProjectID, product.ID, version.ID,
		version.FactsHash, referenceSetHash, packHash, createdBy).Scan(&packID); err != nil {
		return "", err
	}
	for _, reference := range references {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_product_reference_pack_items(
				organization_id, project_id, product_id, product_version_id,
				reference_pack_id, product_reference_id, ordinal, reference_role,
				artifact_id, media_file_id, content_hash
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, product.OrganizationID, product.ProjectID, product.ID, version.ID, packID,
			reference.ID, reference.Ordinal, reference.ReferenceRole, reference.ArtifactID,
			reference.MediaFileID, reference.ContentHash); err != nil {
			return "", err
		}
	}
	return packID, nil
}

func (r *Repository) InsertProductRebuildTargetGeneration(
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	product Product,
	targetProductVersionID string,
	targetPackID string,
	seed productRebuildSeed,
	createdBy string,
) (string, error) {
	var snapshot map[string]any
	if err := json.Unmarshal(seed.ConfigurationSnapshot, &snapshot); err != nil {
		return "", err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["productVersionId"] = targetProductVersionID
	snapshot["referencePackId"] = targetPackID
	snapshot["sourceUnitGenerationId"] = seed.SourceGenerationID
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	hash, err := hashJSON(raw)
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_generations(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			source_unit_generation_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'preparing', $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id::text
	`, production.OrganizationID, production.ProjectID, product.ID, seed.ScriptUnitID,
		production.Generation.ID, seed.SourceGenerationNo+1, production.CommerceBinding.ID,
		production.CommerceBinding.Revision, targetProductVersionID, seed.SourceScriptVersionID,
		seed.LocalizationID, targetPackID, raw, hash, seed.SourceGenerationID, createdBy).Scan(&id)
	return id, err
}

func (r *Repository) InsertProductRebuildItem(ctx context.Context, tx pgx.Tx, rebuildID, productID string, seed productRebuildSeed, targetGenerationID, targetPackID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_product_rebuild_items(
			organization_id, project_id, product_id, rebuild_id, script_unit_id,
			source_unit_generation_id, target_unit_generation_id,
			source_reference_pack_id, target_reference_pack_id, status
		)
		SELECT organization_id, project_id, $2, $1, $3, $4, $5, $6, $7, 'ready'
		FROM commerce_product_rebuilds WHERE id = $1
	`, rebuildID, productID, seed.ScriptUnitID, seed.SourceGenerationID, targetGenerationID,
		seed.SourceReferencePackID, targetPackID)
	return err
}

func (r *Repository) ActivateProductRebuild(
	ctx context.Context,
	tx pgx.Tx,
	rebuild persistedProductRebuild,
	product Product,
	targetProductVersionID string,
	targetPackID string,
	seeds []productRebuildSeed,
	targets map[string]string,
) error {
	for _, seed := range seeds {
		targetID := targets[seed.ScriptUnitID]
		if tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_generations
			SET status = 'archived', archived_at = now()
			WHERE id = $1 AND status = 'active'
		`, seed.SourceGenerationID); err != nil || tag.RowsAffected() != 1 {
			if err != nil {
				return err
			}
			return Error{Code: CodeProductVersionStale, Message: "脚本生产代已变化，商品换版未执行"}
		}
		if tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_generations
			SET status = 'active', activated_at = now()
			WHERE id = $1 AND status = 'preparing'
		`, targetID); err != nil || tag.RowsAffected() != 1 {
			if err != nil {
				return err
			}
			return Error{Code: CodeProductVersionStale, Message: "目标脚本生产代准备失败"}
		}
		if tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_units
			SET active_unit_generation_id = $2, unit_generation_no = $3,
			    revision = revision + 1, updated_at = now()
			WHERE id = $1 AND revision = $4 AND active_unit_generation_id = $5
		`, seed.ScriptUnitID, targetID, seed.SourceGenerationNo+1, seed.ScriptUnitRevision, seed.SourceGenerationID); err != nil || tag.RowsAffected() != 1 {
			if err != nil {
				return err
			}
			return Error{Code: CodeProductVersionStale, Message: "脚本单元已变化，商品换版未执行"}
		}
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET current_version_id = $2, status = 'ready', revision = revision + 1, updated_at = now()
		WHERE id = $1 AND revision = $3 AND current_version_id = $4
	`, product.ID, targetProductVersionID, product.Revision, rebuild.SourceProductVersionID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return Error{Code: CodeProductVersionStale, Message: "商品资料已变化，换版未执行"}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_product_rebuild_items
		SET status = 'switched', switched_at = now()
		WHERE rebuild_id = $1 AND status = 'ready'
	`, rebuild.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_product_rebuilds
		SET status = 'succeeded', completed_at = now()
		WHERE id = $1 AND status = 'preparing'
	`, rebuild.ID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) ProductRebuildTargetPackID(ctx context.Context, db rowQuerier, rebuildID string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		SELECT target_reference_pack_id::text
		FROM commerce_product_rebuild_items
		WHERE rebuild_id = $1 AND target_reference_pack_id IS NOT NULL
		LIMIT 1
	`, rebuildID).Scan(&id)
	return id, err
}
