package commerce

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// FreezeScriptUnitPreparation locks the current project and script inputs and
// returns the immutable identity used by the preparation workflow. It creates
// or reuses a product reference pack before any provider call is queued.
func (s *CatalogService) FreezeScriptUnitPreparation(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	scriptUnitID string,
	expectedRevision int64,
	createdBy string,
) (ScriptUnitPreparationIdentity, error) {
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if production.ProjectLocked {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeProjectLocked, Message: "带货视频项目生产配置正在切换", Retryable: true}
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if !found || product.CurrentVersionID == nil {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeProductRequired, Message: "请先完成商品资料"}
	}
	productVersion, err := s.repository.LoadProductVersion(ctx, tx, organizationID, projectID, *product.CurrentVersionID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, scriptUnitID, true)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if unit.Status == "archived" {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptUnitArchived, Message: "已归档脚本不能启动生产"}
	}
	if unit.Revision != expectedRevision {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	if unit.ActiveUnitGenerationID != nil {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptVersionStale, Message: "当前脚本已有活动生产代，请先执行单元重建"}
	}
	if unit.ProductID != product.ID || unit.CurrentSourceVersionID == nil {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptRequired, Message: "请先保存并激活广告脚本"}
	}
	source, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unit.ID, *unit.CurrentSourceVersionID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	references, err := s.repository.ListProductReferences(ctx, tx, organizationID, projectID, "active")
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	ids := referenceIDs(references)
	selected, referenceSetHash, err := s.validateTargetProductReferences(ctx, tx, organizationID, projectID, product.ID, ids)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	packID, err := s.repository.InsertProductReferencePack(ctx, tx, product, productVersion, selected, referenceSetHash, createdBy)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	pack, err := s.repository.LoadProductReferencePack(ctx, tx, organizationID, projectID, packID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	return ScriptUnitPreparationIdentity{
		ExecutionIdentity:       production.ExecutionIdentity(),
		ProductID:               product.ID,
		ProductVersionID:        productVersion.ID,
		ProductFactsHash:        productVersion.FactsHash,
		ScriptUnitID:            unit.ID,
		ScriptUnitRevision:      unit.Revision,
		SourceScriptVersionID:   source.ID,
		SourceScriptContentHash: source.ContentHash,
		ReferencePackID:         pack.ID,
		ReferencePackHash:       pack.PackHash,
	}, nil
}
