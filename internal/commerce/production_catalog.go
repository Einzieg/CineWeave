package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *CatalogService) CreateProductionRun(
	ctx context.Context,
	tx pgx.Tx,
	params CreateProductionRunParams,
) (ProductionRun, bool, error) {
	return NewProductionRunService(s.repository).CreateRun(ctx, tx, params)
}

func (s *CatalogService) AttachProductionRunWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	workflowRunID string,
) error {
	return s.repository.AttachProductionRunWorkflow(ctx, tx, organizationID, projectID, runID, workflowRunID)
}

func (s *CatalogService) ListProductionRuns(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
	runType string,
	limit int,
) ([]ProductionRun, error) {
	typedRunType := ProductionRunType(runType)
	if runType != "" {
		if err := validateProductionRunType(typedRunType); err != nil {
			return nil, Error{Code: CodeRunStateConflict, Message: "生产批次类型筛选无效", Cause: err}
		}
	}
	return s.repository.ListProductionRuns(ctx, db, organizationID, projectID, scriptUnitID, typedRunType, limit)
}

func (s *CatalogService) GetProductionRun(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	runID string,
) (ProductionRunDetail, error) {
	run, err := s.repository.GetProductionRun(ctx, db, organizationID, projectID, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductionRunDetail{}, Error{Code: CodeRunStateConflict, Message: "生产批次不存在", Cause: err}
	}
	if err != nil {
		return ProductionRunDetail{}, err
	}
	items, err := s.repository.ListProductionRunItems(ctx, db, organizationID, projectID, runID)
	if err != nil {
		return ProductionRunDetail{}, err
	}
	return ProductionRunDetail{Run: run, Items: items}, nil
}

func (s *CatalogService) CancelProductionRun(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	reason string,
) (ProductionRun, error) {
	return s.repository.CancelProductionRun(ctx, tx, organizationID, projectID, runID, reason)
}

func (s *CatalogService) FailActiveProductionRunItems(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	errorCode string,
	errorMessage string,
	retryable bool,
) (ProductionRun, error) {
	return s.repository.FailActiveProductionRunItems(
		ctx, tx, organizationID, projectID, runID, errorCode, errorMessage, retryable,
	)
}
