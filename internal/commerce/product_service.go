package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *CatalogService) ClaimProductReferenceUpload(
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
	if strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(storageKey) == "" || strings.TrimSpace(fileName) == "" {
		return ProductReferenceUpload{}, false, errors.New("product reference upload identity is incomplete")
	}
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
		return ProductReferenceUpload{}, false, errors.New("product reference upload mime type is invalid")
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductReferenceUpload{}, false, err
	}
	if !found || product.ID != productID {
		return ProductReferenceUpload{}, false, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	if setupSessionID != nil {
		setup, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, *setupSessionID, true)
		if err != nil {
			return ProductReferenceUpload{}, false, err
		}
		if setupTerminal(setup.State) {
			return ProductReferenceUpload{}, false, Error{Code: CodeSetupAbandoned, Message: "当前创建会话不能再上传商品图片"}
		}
	}
	return s.repository.ClaimProductReferenceUpload(ctx, tx, organizationID, projectID, productID,
		setupSessionID, storageKey, mimeType, fileName, idempotencyKey, createdBy, expiresAt)
}

func (s *CatalogService) GetProductReferenceUpload(ctx context.Context, db rowQuerier, organizationID, projectID, uploadID string, lock bool) (ProductReferenceUpload, error) {
	return s.repository.LoadProductReferenceUpload(ctx, db, organizationID, projectID, uploadID, lock)
}

func (s *CatalogService) CompleteProductReferenceUpload(ctx context.Context, tx pgx.Tx, upload ProductReferenceUpload, referenceID string) (ProductReferenceUpload, error) {
	return s.repository.CompleteProductReferenceUpload(ctx, tx, upload, referenceID)
}

func (s *CatalogService) AbandonProductReferenceUpload(ctx context.Context, tx pgx.Tx, upload ProductReferenceUpload) (ProductReferenceUpload, error) {
	return s.repository.AbandonProductReferenceUpload(ctx, tx, upload)
}

func (s *CatalogService) GetProduct(ctx context.Context, db rowQuerier, organizationID, projectID string) (Product, error) {
	item, err := s.repository.LoadProduct(ctx, db, organizationID, projectID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, Error{Code: CodeProductRequired, Message: "请先填写商品信息", Cause: err}
	}
	return item, err
}

func (s *CatalogService) CreateProductVersion(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	createdBy string,
	expectedRevision *int64,
	input ProductVersionInput,
) (ProductMutationResult, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Brand = strings.TrimSpace(input.Brand)
	if input.Name == "" {
		return ProductMutationResult{}, Error{Code: CodeProductRequired, Message: "产品名称不能为空"}
	}
	if len(input.SellingPoints) == 0 {
		input.SellingPoints = json.RawMessage(`[]`)
	}
	if len(input.ImmutableFeatures) == 0 {
		input.ImmutableFeatures = json.RawMessage(`{}`)
	}
	if len(input.ProhibitedClaims) == 0 {
		input.ProhibitedClaims = json.RawMessage(`[]`)
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage(`{}`)
	}
	if err := validateJSONArray(input.SellingPoints); err != nil {
		return ProductMutationResult{}, errors.New("selling points must be a JSON array")
	}
	if err := validateJSONObject(input.ImmutableFeatures); err != nil {
		return ProductMutationResult{}, errors.New("immutable features must be a JSON object")
	}
	if err := validateJSONArray(input.ProhibitedClaims); err != nil {
		return ProductMutationResult{}, errors.New("prohibited claims must be a JSON array")
	}
	if err := validateJSONObject(input.Metadata); err != nil {
		return ProductMutationResult{}, errors.New("product metadata must be a JSON object")
	}

	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductMutationResult{}, err
	}
	if !found {
		if expectedRevision != nil && *expectedRevision != 0 {
			return ProductMutationResult{}, Error{Code: CodeProductVersionStale, Message: "商品资料已变化，请刷新后重试"}
		}
		product, err = s.repository.InsertProduct(ctx, tx, organizationID, projectID, createdBy, input.Metadata)
		if err != nil {
			return ProductMutationResult{}, err
		}
	} else if expectedRevision != nil && product.Revision != *expectedRevision {
		return ProductMutationResult{}, Error{Code: CodeProductVersionStale, Message: "商品资料已变化，请刷新后重试"}
	}

	facts := map[string]any{
		"name":              input.Name,
		"brand":             input.Brand,
		"sellingPoints":     json.RawMessage(input.SellingPoints),
		"immutableFeatures": json.RawMessage(input.ImmutableFeatures),
		"prohibitedClaims":  json.RawMessage(input.ProhibitedClaims),
	}
	factsRaw, err := json.Marshal(facts)
	if err != nil {
		return ProductMutationResult{}, err
	}
	factsHash, err := hashJSON(factsRaw)
	if err != nil {
		return ProductMutationResult{}, err
	}
	if product.CurrentVersion != nil && product.CurrentVersion.FactsHash == factsHash {
		return ProductMutationResult{Product: product, Version: *product.CurrentVersion, Activated: true}, nil
	}

	version, err := s.repository.InsertProductVersion(ctx, tx, product, input, factsRaw, factsHash, createdBy)
	if err != nil {
		return ProductMutationResult{}, err
	}
	activeUnits, err := s.repository.CountActiveUnitGenerations(ctx, tx, organizationID, projectID, product.ID)
	if err != nil {
		return ProductMutationResult{}, err
	}
	if activeUnits > 0 {
		if err := s.repository.RetireLegacyUnitGenerationsForProductUpdate(
			ctx, tx, organizationID, projectID, product.ID,
		); err != nil {
			return ProductMutationResult{}, err
		}
	}
	product, err = s.repository.ActivateProductVersion(ctx, tx, product, version, input.Metadata)
	if err != nil {
		return ProductMutationResult{}, err
	}
	return ProductMutationResult{
		Product: product, Version: version, Activated: true, RequiresRebuild: false,
	}, nil
}

func (s *CatalogService) ListProductVersions(ctx context.Context, db rowsQuerier, organizationID, projectID string) ([]ProductVersion, error) {
	return s.repository.ListProductVersions(ctx, db, organizationID, projectID)
}

func (s *CatalogService) GetProductVersion(ctx context.Context, db rowQuerier, organizationID, projectID, versionID string) (ProductVersion, error) {
	return s.repository.LoadProductVersion(ctx, db, organizationID, projectID, versionID)
}

func (s *CatalogService) CreateProductReference(ctx context.Context, tx pgx.Tx, params CreateProductReferenceParams) (ProductReference, error) {
	params.ReferenceRole = strings.TrimSpace(params.ReferenceRole)
	if params.ReferenceRole == "" {
		params.ReferenceRole = "other"
	}
	if !validProductReferenceRole(params.ReferenceRole) || !strings.HasPrefix(params.MimeType, "image/") ||
		params.Width <= 0 || params.Height <= 0 || !validSHA256(params.ContentHash) {
		return ProductReference{}, errors.New("product reference metadata is invalid")
	}
	if len(params.QualityReview) == 0 {
		params.QualityReview = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(params.QualityReview); err != nil {
		return ProductReference{}, errors.New("product reference quality review must be an object")
	}
	product, found, err := s.repository.LockProduct(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return ProductReference{}, err
	}
	if !found || product.ID != params.ProductID {
		return ProductReference{}, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	return s.repository.InsertProductReference(ctx, tx, product, params)
}

func (s *CatalogService) ListProductReferences(ctx context.Context, db rowsQuerier, organizationID, projectID, status string) ([]ProductReference, error) {
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		return nil, errors.New("product reference status filter is invalid")
	}
	return s.repository.ListProductReferences(ctx, db, organizationID, projectID, status)
}

func (s *CatalogService) FindProductReferenceByHash(ctx context.Context, db rowQuerier, organizationID, projectID, contentHash string) (ProductReference, bool, error) {
	product, err := s.GetProduct(ctx, db, organizationID, projectID)
	if err != nil {
		return ProductReference{}, false, err
	}
	return s.repository.findActiveProductReferenceByHash(ctx, db, product.ID, contentHash)
}

func (s *CatalogService) UpdateProductReference(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	referenceID string,
	expectedRevision int64,
	referenceRole string,
	ordinal *int,
	setPrimary *bool,
) (ProductReference, error) {
	referenceRole = strings.TrimSpace(referenceRole)
	if referenceRole != "" && !validProductReferenceRole(referenceRole) {
		return ProductReference{}, errors.New("product reference role is invalid")
	}
	return s.repository.UpdateProductReference(ctx, tx, organizationID, projectID, referenceID, expectedRevision, referenceRole, ordinal, setPrimary)
}

func (s *CatalogService) ArchiveProductReference(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	referenceID string,
	expectedRevision int64,
) (ProductReference, error) {
	return s.repository.ArchiveProductReference(ctx, tx, organizationID, projectID, referenceID, expectedRevision)
}

func validateJSONArray(raw json.RawMessage) error {
	var value []any
	return json.Unmarshal(raw, &value)
}

func validProductReferenceRole(value string) bool {
	switch value {
	case "primary", "front", "back", "detail", "usage", "logo", "other":
		return true
	default:
		return false
	}
}
