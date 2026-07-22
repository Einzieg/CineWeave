package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type gatewayVideoProductionIdentity struct {
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
}

func videoProductionIdentity(generationID, bindingID string, bindingRevision int64) gatewayVideoProductionIdentity {
	return gatewayVideoProductionIdentity{
		ProductionGenerationID:         strings.TrimSpace(generationID),
		VideoProductionBindingID:       strings.TrimSpace(bindingID),
		VideoProductionBindingRevision: bindingRevision,
	}
}

func (identity gatewayVideoProductionIdentity) validateRequired() error {
	if identity.ProductionGenerationID == "" || identity.VideoProductionBindingID == "" || identity.VideoProductionBindingRevision <= 0 {
		return fmt.Errorf("%w: productionGenerationId, videoProductionBindingId, and videoProductionBindingRevision are required", ErrValidation)
	}
	return nil
}

func generationMismatchError(_ error) error {
	message := "视频生产代已切换，当前结果仅保留审计且不会写入活动生产数据"
	return &StandardErrorError{Standard: StandardError{
		Code: CodeProductionGenerationMismatch, Message: message, Retryable: false,
	}}
}

func (s *Service) validateGatewayVideoProductionIdentity(
	ctx context.Context,
	organizationID, projectID, generationID, bindingID string,
	bindingRevision int64,
	workflowRunID, nodeRunID string,
) error {
	organizationID = strings.TrimSpace(organizationID)
	projectID = strings.TrimSpace(projectID)
	if organizationID == "" || projectID == "" {
		return fmt.Errorf("%w: organizationId and projectId are required for video production", ErrValidation)
	}
	identity := videoProductionIdentity(generationID, bindingID, bindingRevision)
	if err := identity.validateRequired(); err != nil {
		return err
	}
	active, err := videoproduction.LoadActiveContext(ctx, s.db, projectID)
	if err != nil {
		return err
	}
	if active.Locked || active.Generation.OrganizationID != organizationID ||
		active.Generation.ID != identity.ProductionGenerationID ||
		active.Binding.ID != identity.VideoProductionBindingID ||
		active.Binding.Revision != identity.VideoProductionBindingRevision ||
		active.Generation.Status != "active" || active.Binding.Status != "active" {
		return generationMismatchError(nil)
	}
	workflowRunID = strings.TrimSpace(workflowRunID)
	nodeRunID = strings.TrimSpace(nodeRunID)
	if workflowRunID == "" && nodeRunID == "" {
		return nil
	}
	var matches bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workflow_runs run
			LEFT JOIN workflow_node_runs node
			  ON node.workflow_run_id = run.id
			 AND node.id = NULLIF($4, '')::uuid
			WHERE run.id = NULLIF($3, '')::uuid
			  AND run.organization_id = $1
			  AND run.project_id = $2
			  AND run.production_generation_id = $5
			  AND run.video_production_binding_id = $6
			  AND run.video_production_binding_revision = $7
			  AND ($4 = '' OR node.organization_id = $1)
			  AND ($4 = '' OR node.production_generation_id = $5)
		)
	`, organizationID, projectID, workflowRunID, nodeRunID, identity.ProductionGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return generationMismatchError(nil)
	}
	return nil
}

func assertGatewayVideoProductionIdentityTx(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, projectID string,
	identity gatewayVideoProductionIdentity,
) error {
	if err := identity.validateRequired(); err != nil {
		return err
	}
	active, err := videoproduction.AssertWritableTx(
		ctx, tx, projectID, identity.ProductionGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision,
	)
	if err != nil {
		var domainErr videoproduction.Error
		if errors.As(err, &domainErr) {
			return generationMismatchError(err)
		}
		return err
	}
	if active.Generation.OrganizationID != strings.TrimSpace(organizationID) {
		return generationMismatchError(nil)
	}
	return nil
}
