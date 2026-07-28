package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ExistingImageReference struct {
	ArtifactID       string
	MediaFileID      string
	StorageKey       string
	OriginalFileName string
	MimeType         string
	ContentHash      string
	ByteSize         int64
	Width            int
	Height           int
}

func LoadExistingImageReference(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	artifactID string,
	mediaFileID string,
	originalFileName string,
) (ExistingImageReference, error) {
	var item ExistingImageReference
	item.ArtifactID = strings.TrimSpace(artifactID)
	item.MediaFileID = strings.TrimSpace(mediaFileID)
	item.OriginalFileName = strings.TrimSpace(originalFileName)
	err := db.QueryRow(ctx, `
		SELECT artifact.storage_key, artifact.mime_type,
		       COALESCE(NULLIF(artifact.content_hash, ''), media.checksum, ''),
		       COALESCE(media.byte_size, 0), COALESCE(media.width, 0), COALESCE(media.height, 0)
		FROM artifacts artifact
		JOIN media_files media
		  ON media.id = $4
		 AND media.artifact_id = artifact.id
		 AND media.organization_id = artifact.organization_id
		 AND media.project_id = artifact.project_id
		WHERE artifact.organization_id = $1
		  AND artifact.project_id = $2
		  AND artifact.id = $3
	`, organizationID, projectID, item.ArtifactID, item.MediaFileID).Scan(
		&item.StorageKey, &item.MimeType, &item.ContentHash,
		&item.ByteSize, &item.Width, &item.Height,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExistingImageReference{}, Error{
			Code:    CodeDirectVideoInvalid,
			Message: "助手图片引用与当前项目不匹配",
			Cause:   err,
		}
	}
	if err != nil {
		return ExistingImageReference{}, err
	}
	if item.OriginalFileName == "" {
		item.OriginalFileName = "assistant-image"
	}
	if !validExistingImageReference(item) {
		return ExistingImageReference{}, Error{
			Code:    CodeDirectVideoInvalid,
			Message: "助手图片缺少可用的媒体元数据",
		}
	}
	return item, nil
}

func (s *CatalogService) BindExistingProductReference(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productID string,
	source ExistingImageReference,
	referenceRole string,
	setPrimary bool,
	createdBy string,
) (ProductReference, bool, error) {
	referenceRole = strings.TrimSpace(referenceRole)
	if referenceRole == "" {
		referenceRole = "other"
	}
	if !validProductReferenceRole(referenceRole) || !validExistingImageReference(source) {
		return ProductReference{}, false, Error{
			Code:    CodeDirectVideoInvalid,
			Message: "商品参考图绑定参数无效",
		}
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductReference{}, false, err
	}
	if !found || product.ID != productID {
		return ProductReference{}, false, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	if existing, found, err := s.repository.findActiveProductReferenceByHash(
		ctx, tx, product.ID, source.ContentHash,
	); err != nil {
		return ProductReference{}, false, err
	} else if found {
		return existing, true, nil
	}
	var nextOrdinal int
	var activeCount int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(ordinal), -1) + 1, count(*)
		FROM commerce_product_references
		WHERE product_id = $1 AND status = 'active'
	`, product.ID).Scan(&nextOrdinal, &activeCount); err != nil {
		return ProductReference{}, false, err
	}
	setPrimary = setPrimary || activeCount == 0
	if setPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_product_references
			SET is_primary = false, revision = revision + 1, updated_at = now()
			WHERE product_id = $1 AND status = 'active' AND is_primary
		`, product.ID); err != nil {
			return ProductReference{}, false, err
		}
		referenceRole = "primary"
	}
	qualityReview, _ := json.Marshal(map[string]any{
		"source": "agent_image_attachment",
		"status": "accepted",
	})
	item, err := scanProductReference(tx.QueryRow(
		ctx, productReferenceInsertSQL,
		organizationID, projectID, product.ID, source.ArtifactID, source.MediaFileID,
		referenceRole, nextOrdinal, setPrimary, source.Width, source.Height,
		source.MimeType, source.ContentHash, qualityReview, createdBy,
	))
	if err != nil {
		return ProductReference{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET status = CASE WHEN current_version_id IS NOT NULL THEN 'ready' ELSE 'draft' END,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, product.ID); err != nil {
		return ProductReference{}, false, err
	}
	return item, false, nil
}

func (s *DirectVideoService) BindExistingScriptReference(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	productID string,
	scriptUnitID string,
	source ExistingImageReference,
	createdBy string,
) (ScriptReferenceImage, bool, error) {
	if !validExistingImageReference(source) {
		return ScriptReferenceImage{}, false, Error{
			Code:    CodeDirectVideoInvalid,
			Message: "脚本参考图绑定参数无效",
		}
	}
	unit, err := s.catalog.GetScriptUnit(ctx, tx, organizationID, projectID, scriptUnitID)
	if err != nil {
		return ScriptReferenceImage{}, false, err
	}
	if unit.Status == "archived" || unit.ProductID != productID {
		return ScriptReferenceImage{}, false, Error{
			Code:    CodeScriptUnitArchived,
			Message: "已归档脚本不能绑定自定义参考图",
		}
	}
	if existing, found, err := s.FindScriptReferenceByHash(
		ctx, tx, organizationID, projectID, scriptUnitID, source.ContentHash,
	); err != nil {
		return ScriptReferenceImage{}, false, err
	} else if found {
		return existing, true, nil
	}
	var referenceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_reference_images(
			organization_id, project_id, product_id, script_unit_id,
			artifact_id, media_file_id, original_file_name, mime_type,
			width, height, byte_size, content_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id::text
	`, organizationID, projectID, productID, scriptUnitID,
		source.ArtifactID, source.MediaFileID, source.OriginalFileName,
		source.MimeType, source.Width, source.Height, source.ByteSize,
		source.ContentHash, createdBy,
	).Scan(&referenceID); err != nil {
		return ScriptReferenceImage{}, false, err
	}
	item, err := scanScriptReferenceImage(tx.QueryRow(ctx, scriptReferenceImageSelectSQL+`
		WHERE reference.id = $1
	`, referenceID))
	return item, false, err
}

func validExistingImageReference(item ExistingImageReference) bool {
	return strings.TrimSpace(item.ArtifactID) != "" &&
		strings.TrimSpace(item.MediaFileID) != "" &&
		strings.TrimSpace(item.StorageKey) != "" &&
		strings.HasPrefix(strings.TrimSpace(item.MimeType), "image/") &&
		validSHA256(strings.TrimSpace(item.ContentHash)) &&
		item.ByteSize > 0 &&
		item.Width > 0 &&
		item.Height > 0
}
