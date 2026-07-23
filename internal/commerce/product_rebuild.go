package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const productImpactLifetime = 15 * time.Minute

type productRebuildSeed struct {
	ScriptUnitID          string
	UnitNo                int64
	Title                 string
	ScriptUnitRevision    int64
	SourceGenerationID    string
	SourceGenerationNo    int64
	SourceReferencePackID string
	SourceScriptVersionID string
	LocalizationID        string
	ConfigurationSnapshot json.RawMessage
}

type persistedProductRebuild struct {
	ID                      string
	OrganizationID          string
	ProjectID               string
	ProductID               string
	ProjectGenerationID     string
	SourceProductVersionID  string
	TargetProductVersionID  string
	TargetReferenceSetHash  string
	ImpactSnapshot          json.RawMessage
	ImpactToken             string
	ExpectedProductRevision int64
	Status                  string
}

func (s *CatalogService) PlanProductRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	targetProductVersionID string,
	targetReferenceIDs []string,
	expectedProductRevision int64,
	requestedBy string,
) (ProductRebuildImpact, error) {
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductRebuildImpact{}, err
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductRebuildImpact{}, err
	}
	if !found || product.CurrentVersionID == nil {
		return ProductRebuildImpact{}, Error{Code: CodeProductRequired, Message: "商品资料尚未完成"}
	}
	if product.Revision != expectedProductRevision {
		return ProductRebuildImpact{}, Error{Code: CodeProductVersionStale, Message: "商品资料已变化，请刷新后重试"}
	}
	targetVersion, err := s.repository.LoadProductVersion(ctx, tx, organizationID, projectID, targetProductVersionID)
	if err != nil || targetVersion.ProductID != product.ID {
		return ProductRebuildImpact{}, Error{Code: CodeProductVersionStale, Message: "目标商品版本不存在", Cause: err}
	}
	references, referenceSetHash, err := s.validateTargetProductReferences(ctx, tx, organizationID, projectID, product.ID, targetReferenceIDs)
	if err != nil {
		return ProductRebuildImpact{}, err
	}
	seeds, err := s.repository.ListActiveProductRebuildSeeds(ctx, tx, production, product.ID)
	if err != nil {
		return ProductRebuildImpact{}, err
	}
	if len(seeds) == 0 {
		return ProductRebuildImpact{}, Error{Code: CodeProductReconfigure, Message: "当前没有需要换版的活动脚本生产代"}
	}
	affected := make([]ProductRebuildUnitImpact, 0, len(seeds))
	for _, seed := range seeds {
		affected = append(affected, ProductRebuildUnitImpact{
			ScriptUnitID: seed.ScriptUnitID, UnitNo: seed.UnitNo, Title: seed.Title,
			SourceUnitGenerationID: seed.SourceGenerationID, SourceReferencePackID: seed.SourceReferencePackID,
		})
	}
	expiresAt := time.Now().UTC().Add(productImpactLifetime)
	token := hashText(uuid.NewString() + ":" + product.ID + ":" + production.Generation.ID)
	impact := ProductRebuildImpact{
		ProjectID: projectID, ProjectGenerationID: production.Generation.ID, ProductID: product.ID,
		SourceProductVersionID: *product.CurrentVersionID, TargetProductVersionID: targetVersion.ID,
		ExpectedProductRevision: product.Revision, TargetReferenceIDs: referenceIDs(references),
		TargetReferenceSetHash: referenceSetHash, ImpactToken: token, ExpiresAt: expiresAt,
		AffectedUnits: affected, ReusableArtifactCount: 0, Blockers: []string{},
	}
	snapshot, err := json.Marshal(impact)
	if err != nil {
		return ProductRebuildImpact{}, err
	}
	if err := s.repository.InsertPlannedProductRebuild(ctx, tx, production, product, targetVersion.ID, referenceSetHash, snapshot, token, requestedBy); err != nil {
		return ProductRebuildImpact{}, err
	}
	return impact, nil
}

func (s *CatalogService) ExecuteProductRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	impactToken string,
	expectedProductRevision int64,
	idempotencyKey string,
	requestedBy string,
) (ProductRebuildResult, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return ProductRebuildResult{}, errors.New("product rebuild idempotency key is required")
	}
	rebuild, err := s.repository.LockProductRebuildByToken(ctx, tx, organizationID, projectID, impactToken)
	if err != nil {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "商品换版确认已失效，请重新检查影响", Cause: err}
	}
	if rebuild.Status == "succeeded" {
		packID, _ := s.repository.ProductRebuildTargetPackID(ctx, tx, rebuild.ID)
		return ProductRebuildResult{
			RebuildID: rebuild.ID, Status: rebuild.Status, ProductVersionID: rebuild.TargetProductVersionID,
			ReferencePackID: packID, IdempotentReplay: true,
		}, nil
	}
	var impact ProductRebuildImpact
	if err := json.Unmarshal(rebuild.ImpactSnapshot, &impact); err != nil {
		return ProductRebuildResult{}, err
	}
	if time.Now().After(impact.ExpiresAt) || rebuild.ExpectedProductRevision != expectedProductRevision {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "商品换版确认已过期，请重新检查影响"}
	}
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductRebuildResult{}, err
	}
	if production.Generation.ID != rebuild.ProjectGenerationID {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "项目生产配置已变化，请重新检查影响"}
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ProductRebuildResult{}, err
	}
	if !found || product.Revision != rebuild.ExpectedProductRevision || product.CurrentVersionID == nil || *product.CurrentVersionID != rebuild.SourceProductVersionID {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "商品资料已变化，请重新检查影响"}
	}
	references, currentSetHash, err := s.validateTargetProductReferences(ctx, tx, organizationID, projectID, product.ID, impact.TargetReferenceIDs)
	if err != nil || currentSetHash != rebuild.TargetReferenceSetHash {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "商品图片集合已变化，请重新检查影响", Cause: err}
	}
	targetVersion, err := s.repository.LoadProductVersion(ctx, tx, organizationID, projectID, rebuild.TargetProductVersionID)
	if err != nil {
		return ProductRebuildResult{}, err
	}
	seeds, err := s.repository.ListActiveProductRebuildSeeds(ctx, tx, production, product.ID)
	if err != nil {
		return ProductRebuildResult{}, err
	}
	if !sameProductRebuildSeeds(impact.AffectedUnits, seeds) {
		return ProductRebuildResult{}, Error{Code: CodeProductVersionStale, Message: "受影响脚本集合已变化，请重新检查影响"}
	}
	packID, err := s.repository.InsertProductReferencePack(ctx, tx, product, targetVersion, references, currentSetHash, requestedBy)
	if err != nil {
		return ProductRebuildResult{}, err
	}
	if err := s.repository.MarkProductRebuildPreparing(ctx, tx, organizationID, rebuild.ID, idempotencyKey); err != nil {
		return ProductRebuildResult{}, err
	}
	targets := make(map[string]string, len(seeds))
	for _, seed := range seeds {
		targetID, err := s.repository.InsertProductRebuildTargetGeneration(ctx, tx, production, product, targetVersion.ID, packID, seed, requestedBy)
		if err != nil {
			return ProductRebuildResult{}, err
		}
		targets[seed.ScriptUnitID] = targetID
		if err := s.repository.InsertProductRebuildItem(ctx, tx, rebuild.ID, product.ID, seed, targetID, packID); err != nil {
			return ProductRebuildResult{}, err
		}
	}
	if err := s.repository.ActivateProductRebuild(ctx, tx, rebuild, product, targetVersion.ID, packID, seeds, targets); err != nil {
		return ProductRebuildResult{}, err
	}
	return ProductRebuildResult{
		RebuildID: rebuild.ID, Status: "succeeded", ProductVersionID: targetVersion.ID,
		ReferencePackID: packID, AffectedUnitCount: len(seeds),
	}, nil
}

func (s *CatalogService) validateTargetProductReferences(ctx context.Context, db rowsQuerier, organizationID, projectID, productID string, targetIDs []string) ([]ProductReference, string, error) {
	if len(targetIDs) == 0 {
		return nil, "", Error{Code: CodeProductPrimaryImage, Message: "商品换版至少需要一张活动商品图"}
	}
	unique := make(map[string]bool, len(targetIDs))
	for _, id := range targetIDs {
		id = strings.TrimSpace(id)
		if id == "" || unique[id] {
			return nil, "", Error{Code: CodeProductVersionStale, Message: "商品图片集合包含重复或空标识"}
		}
		unique[id] = true
	}
	all, err := s.repository.ListProductReferences(ctx, db, organizationID, projectID, "active")
	if err != nil {
		return nil, "", err
	}
	selected := make([]ProductReference, 0, len(targetIDs))
	primary := false
	for _, item := range all {
		if unique[item.ID] {
			if item.ProductID != productID {
				return nil, "", Error{Code: CodeProductVersionStale, Message: "商品图片不属于当前商品"}
			}
			selected = append(selected, item)
			primary = primary || item.IsPrimary
		}
	}
	if len(selected) != len(unique) {
		return nil, "", Error{Code: CodeProductVersionStale, Message: "部分商品图片已归档或不存在"}
	}
	if !primary {
		return nil, "", Error{Code: CodeProductPrimaryImage, Message: "商品图片集合必须包含主图"}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Ordinal == selected[j].Ordinal {
			return selected[i].ID < selected[j].ID
		}
		return selected[i].Ordinal < selected[j].Ordinal
	})
	snapshot := make([]map[string]any, 0, len(selected))
	for _, item := range selected {
		snapshot = append(snapshot, map[string]any{
			"id": item.ID, "ordinal": item.Ordinal, "role": item.ReferenceRole,
			"contentHash": item.ContentHash, "artifactId": item.ArtifactID, "mediaFileId": item.MediaFileID,
		})
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	hash, err := hashJSON(raw)
	return selected, hash, err
}

func referenceIDs(items []ProductReference) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID)
	}
	return result
}

func sameProductRebuildSeeds(expected []ProductRebuildUnitImpact, actual []productRebuildSeed) bool {
	if len(expected) != len(actual) {
		return false
	}
	byID := make(map[string]ProductRebuildUnitImpact, len(expected))
	for _, item := range expected {
		byID[item.ScriptUnitID] = item
	}
	for _, item := range actual {
		expectedItem, ok := byID[item.ScriptUnitID]
		if !ok || expectedItem.SourceUnitGenerationID != item.SourceGenerationID || expectedItem.SourceReferencePackID != item.SourceReferencePackID {
			return false
		}
	}
	return true
}
