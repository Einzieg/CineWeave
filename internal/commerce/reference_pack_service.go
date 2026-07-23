package commerce

import (
	"context"
	"errors"
)

func (s *CatalogService) ListProductReferencePacks(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
) ([]ProductReferencePack, error) {
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "stale" && status != "archived" && status != "all" {
		return nil, errors.New("product reference pack status filter is invalid")
	}
	return s.repository.ListProductReferencePacks(ctx, db, organizationID, projectID, status)
}

func (s *CatalogService) GetProductReferencePack(
	ctx context.Context,
	db catalogQuerier,
	organizationID string,
	projectID string,
	packID string,
) (ProductReferencePack, error) {
	return s.repository.LoadProductReferencePack(ctx, db, organizationID, projectID, packID)
}
