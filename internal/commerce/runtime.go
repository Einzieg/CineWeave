package commerce

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type rowsQuerier interface {
	rowQuerier
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (r *Repository) LoadActiveProductionContext(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
) (ProductionContext, error) {
	return r.loadActiveProductionContext(ctx, db, organizationID, projectID, false)
}

func (r *Repository) LockActiveProductionContext(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
) (ProductionContext, error) {
	return r.loadActiveProductionContext(ctx, tx, organizationID, projectID, true)
}

func (r *Repository) loadActiveProductionContext(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	lock bool,
) (ProductionContext, error) {
	projectQuery := `
		SELECT project_kind, revision, video_production_state, video_production_locked,
		       active_video_production_generation_id::text
		FROM projects
		WHERE id = $1 AND organization_id = $2`
	if lock {
		projectQuery += " FOR UPDATE"
	}
	var item ProductionContext
	var kind ProjectKind
	var activeGenerationID pgtype.Text
	err := db.QueryRow(ctx, projectQuery, projectID, organizationID).Scan(
		&kind,
		&item.ProjectRevision,
		&item.ProjectState,
		&item.ProjectLocked,
		&activeGenerationID,
	)
	if err != nil {
		return ProductionContext{}, err
	}
	if !kind.IsCommerce() {
		return ProductionContext{}, Error{
			Code:    CodeProjectKindMismatch,
			Message: "当前项目不是带货视频项目",
			Details: map[string]any{"actualProjectKind": kind, "expectedProjectKind": ProjectKindCommerceVideo},
		}
	}
	if !activeGenerationID.Valid {
		return ProductionContext{}, Error{Code: CodeProjectNotConfigured, Message: "带货视频项目尚未完成生产配置", Cause: ErrProjectNotConfigured}
	}

	item.OrganizationID = organizationID
	item.ProjectID = projectID
	var commerceBindingID pgtype.Text
	err = db.QueryRow(ctx, `
		SELECT generation.id::text, generation.generation_no, generation.status,
		       generation.binding_id::text, generation.commerce_workflow_binding_id::text,
		       video.id::text, video.revision, video.status, video.profile_version_id::text,
		       video.profile_snapshot_hash, video.profile_snapshot,
		       commerce.id::text, commerce.binding_revision, commerce.status,
		       commerce.template_version_id::text, commerce.video_production_binding_id::text,
		       commerce.video_profile_snapshot_hash, commerce.configuration_hash,
		       commerce.configuration_snapshot, commerce.model_routing_snapshot,
		       commerce.capability_snapshot
		FROM project_video_production_generations generation
		JOIN project_video_production_bindings video
		  ON video.id = generation.binding_id AND video.project_id = generation.project_id
		JOIN project_commerce_workflow_bindings commerce
		  ON commerce.id = generation.commerce_workflow_binding_id
		 AND commerce.project_id = generation.project_id
		 AND commerce.organization_id = generation.organization_id
		WHERE generation.id = $1
		  AND generation.project_id = $2
		  AND generation.organization_id = $3
	`, activeGenerationID.String, projectID, organizationID).Scan(
		&item.Generation.ID,
		&item.Generation.GenerationNo,
		&item.Generation.Status,
		&item.Generation.VideoBindingID,
		&commerceBindingID,
		&item.VideoBinding.ID,
		&item.VideoBinding.Revision,
		&item.VideoBinding.Status,
		&item.VideoBinding.ProfileVersionID,
		&item.VideoBinding.ProfileSnapshotHash,
		&item.VideoBinding.ProfileSnapshot,
		&item.CommerceBinding.ID,
		&item.CommerceBinding.Revision,
		&item.CommerceBinding.Status,
		&item.CommerceBinding.TemplateVersionID,
		&item.CommerceBinding.VideoBindingID,
		&item.CommerceBinding.VideoProfileSnapshotHash,
		&item.CommerceBinding.ConfigurationHash,
		&item.CommerceBinding.ConfigurationSnapshot,
		&item.CommerceBinding.ModelRoutingSnapshot,
		&item.CommerceBinding.CapabilitySnapshot,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductionContext{}, Error{Code: CodeBindingMismatch, Message: "带货视频生产绑定缺失或不完整", Cause: err}
	}
	if err != nil {
		return ProductionContext{}, err
	}
	if commerceBindingID.Valid {
		item.Generation.CommerceBindingID = commerceBindingID.String
	}
	if err := validateProductionContext(item); err != nil {
		return ProductionContext{}, err
	}
	return item, nil
}

func validateProductionContext(item ProductionContext) error {
	if item.Generation.Status != "active" || item.VideoBinding.Status != "active" || item.CommerceBinding.Status != "active" {
		return Error{Code: CodeBindingMismatch, Message: "带货视频当前生产绑定不是活动状态"}
	}
	if item.Generation.VideoBindingID != item.VideoBinding.ID ||
		item.Generation.CommerceBindingID == "" ||
		item.Generation.CommerceBindingID != item.CommerceBinding.ID ||
		item.CommerceBinding.VideoBindingID != item.VideoBinding.ID ||
		item.CommerceBinding.Revision != item.VideoBinding.Revision ||
		item.CommerceBinding.VideoProfileSnapshotHash != item.VideoBinding.ProfileSnapshotHash {
		return Error{Code: CodeBindingMismatch, Message: "带货视频 Commerce Binding 与 Video Binding 身份不一致"}
	}
	return nil
}

func (r *Repository) LockUnitGenerationContext(
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	identity UnitGenerationIdentity,
) (UnitGenerationContext, error) {
	var item UnitGenerationContext
	var activeUnitGenerationID pgtype.Text
	err := tx.QueryRow(ctx, `
		SELECT generation.organization_id::text, generation.project_id::text,
		       generation.product_id::text, generation.script_unit_id::text,
		       unit.revision, generation.id::text, generation.unit_generation_no, generation.status,
		       generation.project_production_generation_id::text,
		       generation.commerce_workflow_binding_id::text,
		       generation.commerce_workflow_binding_revision,
		       generation.product_version_id::text, generation.source_script_version_id::text,
		       generation.localization_id::text, generation.reference_pack_id::text,
		       generation.unit_configuration_snapshot, generation.unit_configuration_hash,
		       unit.active_unit_generation_id::text
		FROM commerce_script_unit_generations generation
		JOIN commerce_script_units unit
		  ON unit.id = generation.script_unit_id
		 AND unit.product_id = generation.product_id
		 AND unit.organization_id = generation.organization_id
		 AND unit.project_id = generation.project_id
		WHERE generation.id = $1
		  AND generation.script_unit_id = $2
		  AND generation.project_id = $3
		  AND generation.organization_id = $4
		FOR UPDATE OF unit
	`, identity.UnitGenerationID, identity.ScriptUnitID, production.ProjectID, production.OrganizationID).Scan(
		&item.Identity.OrganizationID,
		&item.Identity.ProjectID,
		&item.Identity.ProductID,
		&item.Identity.ScriptUnitID,
		&item.Identity.ScriptUnitRevision,
		&item.Identity.UnitGenerationID,
		&item.Identity.UnitGenerationNo,
		&item.Status,
		&item.Identity.ProjectGenerationID,
		&item.Identity.CommerceWorkflowBindingID,
		&item.Identity.CommerceWorkflowBindingRevision,
		&item.ProductVersionID,
		&item.SourceScriptVersionID,
		&item.LocalizationID,
		&item.ReferencePackID,
		&item.ConfigurationSnapshot,
		&item.Identity.UnitConfigurationHash,
		&activeUnitGenerationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产代不存在或已失效", Cause: err}
	}
	if err != nil {
		return UnitGenerationContext{}, err
	}
	item.Identity.VideoProductionBindingID = production.VideoBinding.ID
	item.Identity.VideoProductionBindingRevision = production.VideoBinding.Revision
	item.Identity.VideoProfileSnapshotHash = production.VideoBinding.ProfileSnapshotHash
	item.Identity.CommerceConfigurationHash = production.CommerceBinding.ConfigurationHash
	if item.Status != "active" || !activeUnitGenerationID.Valid || activeUnitGenerationID.String != item.Identity.UnitGenerationID {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产代已不再活动"}
	}
	if item.Identity.ProjectGenerationID != production.Generation.ID ||
		item.Identity.CommerceWorkflowBindingID != production.CommerceBinding.ID ||
		item.Identity.CommerceWorkflowBindingRevision != production.CommerceBinding.Revision {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产代与项目生产配置不一致"}
	}
	if identity.ProductID != "" && identity.ProductID != item.Identity.ProductID {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元商品身份已变化"}
	}
	if identity.ScriptUnitRevision > 0 && identity.ScriptUnitRevision != item.Identity.ScriptUnitRevision {
		return UnitGenerationContext{}, Error{Code: CodeRevisionConflict, Message: "脚本单元已被其他操作修改"}
	}
	if identity.UnitGenerationNo > 0 && identity.UnitGenerationNo != item.Identity.UnitGenerationNo {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产代序号已变化"}
	}
	if identity.UnitConfigurationHash != "" && identity.UnitConfigurationHash != item.Identity.UnitConfigurationHash {
		return UnitGenerationContext{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产配置已变化"}
	}
	var activeRebuildID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM commerce_script_unit_rebuilds
		WHERE organization_id = $1
		  AND project_id = $2
		  AND script_unit_id = $3
		  AND source_unit_generation_id = $4
		  AND status IN ('running', 'waiting_user_confirmation')
		LIMIT 1
	`, production.OrganizationID, production.ProjectID, item.Identity.ScriptUnitID,
		item.Identity.UnitGenerationID).Scan(&activeRebuildID)
	if err == nil {
		return UnitGenerationContext{}, Error{
			Code:      CodeScriptRebuildBlocked,
			Message:   "脚本正在换代，暂不能启动新的生产任务",
			Retryable: true,
			Details:   map[string]any{"scriptUnitRebuildId": activeRebuildID},
		}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UnitGenerationContext{}, err
	}
	return item, nil
}

func assertExecutionIdentity(actual ProductionContext, expected ExecutionIdentity) error {
	if expected.OrganizationID == "" || expected.ProjectID == "" || expected.ProjectGenerationID == "" ||
		expected.VideoProductionBindingID == "" || expected.VideoProductionBindingRevision <= 0 ||
		expected.VideoProfileSnapshotHash == "" || expected.CommerceWorkflowBindingID == "" ||
		expected.CommerceWorkflowBindingRevision <= 0 || expected.CommerceConfigurationHash == "" {
		return fmt.Errorf("commerce execution identity is incomplete")
	}
	actualIdentity := actual.ExecutionIdentity()
	if actualIdentity != expected {
		return Error{
			Code:    CodeBindingMismatch,
			Message: "带货视频任务使用的生产配置已失效",
			Details: map[string]any{
				"actualProjectGenerationId":   actualIdentity.ProjectGenerationID,
				"expectedProjectGenerationId": expected.ProjectGenerationID,
			},
		}
	}
	return nil
}
