package commerce

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBindExistingImageReferenceReusesArtifactAndMediaIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_COMMERCE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_COMMERCE_INTEGRATION_TEST=1 to run attachment binding integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for attachment binding integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	fixture := createScriptDerivationIntegrationFixture(t, ctx, tx)
	contentHash := strings.Repeat("d", 64)
	storageKey := "agent-attachments/" + fixture.Suffix + "/reference.png"
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'agent_image_attachment', $3, 'image/png', $4, '{}', $5)
		RETURNING id::text
	`, fixture.OrganizationID, fixture.ProjectID, storageKey, contentHash, fixture.UserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert attachment artifact: %v", err)
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', 1024, 512, 512, $5, '{}', $6)
		RETURNING id::text
	`, fixture.OrganizationID, fixture.ProjectID, artifactID, storageKey, contentHash, fixture.UserID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert attachment media: %v", err)
	}

	source, err := LoadExistingImageReference(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID,
		artifactID, mediaFileID, "reference.png",
	)
	if err != nil {
		t.Fatalf("load existing image reference: %v", err)
	}
	catalog := NewCatalogService(NewRepository())
	product, err := catalog.GetProduct(ctx, tx, fixture.OrganizationID, fixture.ProjectID)
	if err != nil {
		t.Fatalf("load product: %v", err)
	}
	productReference, duplicate, err := catalog.BindExistingProductReference(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, product.ID,
		source, "primary", true, fixture.UserID,
	)
	if err != nil {
		t.Fatalf("bind product reference: %v", err)
	}
	if duplicate || productReference.ArtifactID != artifactID ||
		productReference.MediaFileID != mediaFileID || !productReference.IsPrimary {
		t.Fatalf("product reference = %+v, duplicate=%t", productReference, duplicate)
	}
	if _, duplicate, err = catalog.BindExistingProductReference(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, product.ID,
		source, "detail", false, fixture.UserID,
	); err != nil || !duplicate {
		t.Fatalf("repeat product binding duplicate=%t error=%v", duplicate, err)
	}

	direct := NewDirectVideoService(NewRepository())
	scriptReference, duplicate, err := direct.BindExistingScriptReference(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, product.ID,
		fixture.SourceUnit.ID, source, fixture.UserID,
	)
	if err != nil {
		t.Fatalf("bind script reference: %v", err)
	}
	if duplicate || scriptReference.ArtifactID != artifactID || scriptReference.MediaFileID != mediaFileID {
		t.Fatalf("script reference = %+v, duplicate=%t", scriptReference, duplicate)
	}
	if _, duplicate, err = direct.BindExistingScriptReference(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, product.ID,
		fixture.SourceUnit.ID, source, fixture.UserID,
	); err != nil || !duplicate {
		t.Fatalf("repeat script binding duplicate=%t error=%v", duplicate, err)
	}

	var artifacts, mediaFiles, productReferences, scriptReferences int
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM artifacts WHERE id = $1),
			(SELECT count(*) FROM media_files WHERE id = $2),
			(SELECT count(*) FROM commerce_product_references
			 WHERE product_id = $3 AND content_hash = $4 AND status = 'active'),
			(SELECT count(*) FROM commerce_script_reference_images
			 WHERE script_unit_id = $5 AND content_hash = $4 AND status = 'active')
	`, artifactID, mediaFileID, product.ID, contentHash, fixture.SourceUnit.ID).Scan(
		&artifacts, &mediaFiles, &productReferences, &scriptReferences,
	); err != nil {
		t.Fatalf("count durable image links: %v", err)
	}
	if artifacts != 1 || mediaFiles != 1 || productReferences != 1 || scriptReferences != 1 {
		t.Fatalf(
			"durable image counts artifact=%d media=%d productRef=%d scriptRef=%d",
			artifacts, mediaFiles, productReferences, scriptReferences,
		)
	}
}
