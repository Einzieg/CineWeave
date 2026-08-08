package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type commerceProductVersionActionInput struct {
	ExpectedRevision  *int64          `json:"expectedRevision"`
	Name              string          `json:"name"`
	Brand             string          `json:"brand"`
	SellingPoints     json.RawMessage `json:"sellingPoints"`
	ImmutableFeatures json.RawMessage `json:"immutableFeatures"`
	ProhibitedClaims  json.RawMessage `json:"prohibitedClaims"`
	Metadata          json.RawMessage `json:"metadata"`
}

type commerceProductUpdateActionInput struct {
	ExpectedRevision  int64           `json:"expectedRevision"`
	Name              *string         `json:"name"`
	Brand             *string         `json:"brand"`
	SellingPoints     json.RawMessage `json:"sellingPoints"`
	ImmutableFeatures json.RawMessage `json:"immutableFeatures"`
	ProhibitedClaims  json.RawMessage `json:"prohibitedClaims"`
	Metadata          json.RawMessage `json:"metadata"`
}

type commerceProductReferenceActionInput struct {
	ReferenceID      string  `json:"referenceId"`
	ExpectedRevision int64   `json:"expectedRevision"`
	ReferenceRole    *string `json:"referenceRole"`
	Ordinal          *int    `json:"ordinal"`
	SetPrimary       *bool   `json:"setPrimary"`
}

type commerceProductRebuildImpactActionInput struct {
	TargetProductVersionID  string   `json:"targetProductVersionId"`
	TargetReferenceIDs      []string `json:"targetReferenceIds"`
	ExpectedProductRevision int64    `json:"expectedProductRevision"`
}

type commerceProductRebuildActionInput struct {
	ImpactToken             string `json:"impactToken"`
	ExpectedProductRevision int64  `json:"expectedProductRevision"`
}

func decodeCommerceProductVersionActionInput(raw json.RawMessage) (commerceProductVersionActionInput, error) {
	var input commerceProductVersionActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, controlValidationError("商品版本参数无效")
	}
	return input, nil
}

func decodeCommerceProductUpdateActionInput(raw json.RawMessage) (commerceProductUpdateActionInput, error) {
	var input commerceProductUpdateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, controlValidationError("商品修改参数无效")
	}
	if input.ExpectedRevision <= 0 {
		return input, controlValidationError("expectedRevision 必须为正整数")
	}
	if input.Name == nil && input.Brand == nil && len(input.SellingPoints) == 0 &&
		len(input.ImmutableFeatures) == 0 && len(input.ProhibitedClaims) == 0 && len(input.Metadata) == 0 {
		return input, controlValidationError("至少需要提供一个商品字段")
	}
	return input, nil
}

func decodeCommerceProductReferenceActionInput(raw json.RawMessage) (commerceProductReferenceActionInput, error) {
	var input commerceProductReferenceActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, controlValidationError("商品参考图参数无效")
	}
	input.ReferenceID = strings.TrimSpace(input.ReferenceID)
	if input.ReferenceID == "" || input.ExpectedRevision <= 0 {
		return input, controlValidationError("referenceId 和 expectedRevision 不能为空")
	}
	return input, nil
}

func (s *Server) createCommerceProductVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceProductVersionActionInput,
) (commercepkg.ProductMutationResult, error) {
	result, err := s.commerceCatalog.CreateProductVersion(
		ctx, tx, project.OrganizationID, project.ID, actorUserID, input.ExpectedRevision,
		commercepkg.ProductVersionInput{
			Name: input.Name, Brand: input.Brand, SellingPoints: input.SellingPoints,
			ImmutableFeatures: input.ImmutableFeatures, ProhibitedClaims: input.ProhibitedClaims,
			Metadata: input.Metadata,
		},
	)
	if err != nil {
		return commercepkg.ProductMutationResult{}, err
	}
	if err := appendCommerceProductMutationEvents(ctx, tx, project.OrganizationID, project.ID, result); err != nil {
		return commercepkg.ProductMutationResult{}, err
	}
	return result, nil
}

func (s *Server) updateCommerceProductActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceProductUpdateActionInput,
) (commercepkg.ProductMutationResult, error) {
	product, err := s.commerceCatalog.GetProduct(ctx, tx, project.OrganizationID, project.ID)
	if err != nil {
		return commercepkg.ProductMutationResult{}, err
	}
	if product.CurrentVersion == nil {
		return commercepkg.ProductMutationResult{}, commercepkg.Error{Code: commercepkg.CodeProductRequired, Message: "商品当前版本不存在"}
	}
	current := product.CurrentVersion
	merged := commerceProductVersionActionInput{
		ExpectedRevision:  &input.ExpectedRevision,
		Name:              current.Name,
		Brand:             current.Brand,
		SellingPoints:     append(json.RawMessage(nil), current.SellingPoints...),
		ImmutableFeatures: append(json.RawMessage(nil), current.ImmutableFeatures...),
		ProhibitedClaims:  append(json.RawMessage(nil), current.ProhibitedClaims...),
		Metadata:          append(json.RawMessage(nil), product.Metadata...),
	}
	if input.Name != nil {
		merged.Name = *input.Name
	}
	if input.Brand != nil {
		merged.Brand = *input.Brand
	}
	if len(input.SellingPoints) > 0 {
		merged.SellingPoints = append(json.RawMessage(nil), input.SellingPoints...)
	}
	if len(input.ImmutableFeatures) > 0 {
		merged.ImmutableFeatures = append(json.RawMessage(nil), input.ImmutableFeatures...)
	}
	if len(input.ProhibitedClaims) > 0 {
		merged.ProhibitedClaims = append(json.RawMessage(nil), input.ProhibitedClaims...)
	}
	if len(input.Metadata) > 0 {
		merged.Metadata = append(json.RawMessage(nil), input.Metadata...)
	}
	return s.createCommerceProductVersionActionTx(ctx, tx, project, actorUserID, merged)
}

func (s *Server) setPrimaryCommerceProductReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceProductReferenceActionInput,
) (commercepkg.ProductReference, error) {
	setPrimary := true
	item, err := s.commerceCatalog.UpdateProductReference(
		ctx, tx, project.OrganizationID, project.ID, input.ReferenceID,
		input.ExpectedRevision, "", nil, &setPrimary,
	)
	if err != nil {
		return commercepkg.ProductReference{}, err
	}
	if err := appendCommerceProductReferenceEvent(
		ctx, tx, project.OrganizationID, project.ID, "commerce.product.reference.updated", item,
	); err != nil {
		return commercepkg.ProductReference{}, err
	}
	return item, nil
}

func (s *Server) updateCommerceProductReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceProductReferenceActionInput,
) (commercepkg.ProductReference, error) {
	referenceRole := ""
	if input.ReferenceRole != nil {
		referenceRole = *input.ReferenceRole
	}
	item, err := s.commerceCatalog.UpdateProductReference(
		ctx, tx, project.OrganizationID, project.ID, input.ReferenceID,
		input.ExpectedRevision, referenceRole, input.Ordinal, input.SetPrimary,
	)
	if err != nil {
		return commercepkg.ProductReference{}, err
	}
	if err := appendCommerceProductReferenceEvent(
		ctx, tx, project.OrganizationID, project.ID, "commerce.product.reference.updated", item,
	); err != nil {
		return commercepkg.ProductReference{}, err
	}
	return item, nil
}

func (s *Server) archiveCommerceProductReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	input commerceProductReferenceActionInput,
) (commercepkg.ProductReference, error) {
	item, err := s.commerceCatalog.ArchiveProductReference(
		ctx, tx, project.OrganizationID, project.ID, input.ReferenceID, input.ExpectedRevision,
	)
	if err != nil {
		return commercepkg.ProductReference{}, err
	}
	if err := appendCommerceProductReferenceEvent(
		ctx, tx, project.OrganizationID, project.ID, "commerce.product.reference.archived", item,
	); err != nil {
		return commercepkg.ProductReference{}, err
	}
	return item, nil
}

func (s *Server) planCommerceProductRebuildActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input commerceProductRebuildImpactActionInput,
) (commercepkg.ProductRebuildImpact, error) {
	if strings.TrimSpace(input.TargetProductVersionID) == "" || input.ExpectedProductRevision <= 0 {
		return commercepkg.ProductRebuildImpact{}, controlValidationError("目标商品版本和 expectedProductRevision 不能为空")
	}
	return s.commerceCatalog.PlanProductRebuild(
		ctx, tx, project.OrganizationID, project.ID, strings.TrimSpace(input.TargetProductVersionID),
		input.TargetReferenceIDs, input.ExpectedProductRevision, actorUserID,
	)
}

func (s *Server) executeCommerceProductRebuildActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	idempotencyKey string,
	input commerceProductRebuildActionInput,
) (commercepkg.ProductRebuildResult, error) {
	if strings.TrimSpace(input.ImpactToken) == "" || input.ExpectedProductRevision <= 0 {
		return commercepkg.ProductRebuildResult{}, controlValidationError("impactToken 和 expectedProductRevision 不能为空")
	}
	result, err := s.commerceCatalog.ExecuteProductRebuild(
		ctx, tx, project.OrganizationID, project.ID, strings.TrimSpace(input.ImpactToken),
		input.ExpectedProductRevision, idempotencyKey, actorUserID,
	)
	if err != nil || result.IdempotentReplay {
		return result, err
	}
	product, err := s.commerceCatalog.GetProduct(ctx, tx, project.OrganizationID, project.ID)
	if err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	version, err := s.commerceCatalog.GetProductVersion(ctx, tx, project.OrganizationID, project.ID, result.ProductVersionID)
	if err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	pack, err := s.commerceCatalog.GetProductReferencePack(ctx, tx, project.OrganizationID, project.ID, result.ReferencePackID)
	if err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
		"commerce.product.version.activated", "commerce_product_version", version.ID, mustRawJSON(map[string]any{
			"productId": product.ID, "productVersionId": version.ID, "version": version.Version,
		})); err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
		"commerce.product.updated", "commerce_product", product.ID, mustRawJSON(map[string]any{
			"productId": product.ID, "productVersionId": version.ID, "revision": product.Revision,
			"activated": true, "requiresRebuild": false,
		})); err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
		"commerce.reference_pack.created", "commerce_product_reference_pack", pack.ID, mustRawJSON(map[string]any{
			"productVersionId": pack.ProductVersionID, "referencePackId": pack.ID, "status": pack.Status,
		})); err != nil {
		return commercepkg.ProductRebuildResult{}, err
	}
	return result, nil
}

func decodeCommerceActionMap(raw json.RawMessage) (map[string]any, error) {
	arguments := map[string]any{}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, fmt.Errorf("decode commerce action arguments: %w", err)
	}
	return arguments, nil
}

func decodeAgentToolData[T any](data map[string]any) (T, error) {
	var value T
	raw, err := json.Marshal(data)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *Server) executeCommerceProductVersionCreateSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceProductVersionActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.createCommerceProductVersionActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.version.create", arguments, "商品版本已创建", projectOperationalReadData(result)), nil
}

func (s *Server) executeCommerceProductUpdateSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceProductUpdateActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.updateCommerceProductActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.update", arguments, "商品配置已更新", projectOperationalReadData(result)), nil
}

func (s *Server) executeCommerceProductReferenceArchiveSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceProductReferenceActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.archiveCommerceProductReferenceActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.reference.archive", arguments, "商品参考图已归档", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceProductReferenceSetPrimarySyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceProductReferenceActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.setPrimaryCommerceProductReferenceActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.reference.set_primary", arguments, "商品主图已更新", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceProductReferenceUpdateSyncAction(
	ctx context.Context, tx pgx.Tx, _ auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	input, err := decodeCommerceProductReferenceActionInput(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	item, err := s.updateCommerceProductReferenceActionTx(ctx, tx, project, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.reference.update", arguments, "商品参考图已更新", projectOperationalReadData(item)), nil
}

func (s *Server) executeCommerceProductRebuildImpactSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	_ projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	var input commerceProductRebuildImpactActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return agentToolResult{}, controlValidationError("商品换版影响参数无效")
	}
	impact, err := s.planCommerceProductRebuildActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.rebuild_impact", arguments, "商品换版影响已计算", projectOperationalReadData(impact)), nil
}

func (s *Server) executeCommerceProductRebuildSyncAction(
	ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project,
	command projectcontrol.Command, raw json.RawMessage,
) (agentToolResult, error) {
	var input commerceProductRebuildActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return agentToolResult{}, controlValidationError("商品换版参数无效")
	}
	result, err := s.executeCommerceProductRebuildActionTx(
		ctx, tx, project, principal.UserID, command.IdempotencyKey, input,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.product.rebuild", arguments, "商品换版已完成", projectOperationalReadData(result)), nil
}
