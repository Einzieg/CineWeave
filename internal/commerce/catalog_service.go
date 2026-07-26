package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CatalogService struct {
	repository *Repository
}

func NewCatalogService(repository *Repository) *CatalogService {
	if repository == nil {
		repository = NewRepository()
	}
	return &CatalogService{repository: repository}
}

func (s *CatalogService) GetSetupSession(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	setupSessionID string,
) (SetupSession, error) {
	item, err := s.repository.LoadSetupSession(ctx, db, organizationID, projectID, setupSessionID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupSession{}, Error{Code: CodeSetupIncomplete, Message: "带货视频创建会话不存在", Cause: err}
	}
	return item, err
}

func (s *CatalogService) TrackSetupUpload(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	storageKey string,
) (SetupSession, error) {
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return SetupSession{}, errors.New("setup upload storage key is required")
	}
	item, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupSession{}, err
	}
	if setupTerminal(item.State) {
		return SetupSession{}, Error{Code: CodeSetupAbandoned, Message: "当前创建会话不能再上传商品图片"}
	}
	return s.repository.UpdateSetupUploadKeys(ctx, tx, item, storageKey, true)
}

func (s *CatalogService) CompleteSetupUpload(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	storageKey string,
) (SetupSession, error) {
	item, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupSession{}, err
	}
	if !setupContainsUpload(item.InputSnapshot, storageKey) {
		return SetupSession{}, Error{Code: CodeSetupIncomplete, Message: "商品图片上传凭据不属于当前创建会话"}
	}
	return s.repository.UpdateSetupUploadKeys(ctx, tx, item, storageKey, false)
}

func (s *CatalogService) AbandonSetupSession(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	expectedRevision int64,
) (SetupSession, []string, error) {
	item, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupSession{}, nil, err
	}
	if item.Revision != expectedRevision {
		return SetupSession{}, nil, Error{Code: CodeSetupRevisionConflict, Message: "创建会话已更新，请刷新后重试"}
	}
	if item.State == "abandoned" {
		return item, setupUploadKeys(item.InputSnapshot), nil
	}
	if item.State == "completed" || item.State == "started" {
		return SetupSession{}, nil, Error{Code: CodeSetupAbandoned, Message: "已经开始生产的项目不能放弃创建"}
	}
	keys := setupUploadKeys(item.InputSnapshot)
	uploadKeys, err := s.repository.AbandonPendingProductUploads(ctx, tx, organizationID, projectID, setupSessionID)
	if err != nil {
		return SetupSession{}, nil, err
	}
	keys = appendUniqueStrings(keys, uploadKeys...)
	updated, err := s.repository.AbandonSetupSession(ctx, tx, item)
	return updated, keys, err
}

func (s *CatalogService) RestartSetupSessionWithTemplate(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	expectedRevision int64,
	workflowTemplateVersionID string,
) (SetupSession, error) {
	item, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupSession{}, err
	}
	if item.Revision != expectedRevision {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "项目准备状态已变化，请刷新后重试"}
	}
	if item.State == "completed" || item.State == "abandoned" {
		return SetupSession{}, Error{Code: CodeSetupAbandoned, Message: "当前项目准备任务不能重新启动"}
	}
	var run *SetupRun
	if item.SetupWorkflowRunID != nil {
		current, loadErr := s.repository.LoadSetupRun(ctx, tx, organizationID, projectID, *item.SetupWorkflowRunID)
		if loadErr != nil {
			return SetupSession{}, loadErr
		}
		run = &current
	}
	return s.repository.RestartSetupSessionWithTemplate(ctx, tx, item, run, strings.TrimSpace(workflowTemplateVersionID))
}

func (s *CatalogService) PrepareSetupCompletion(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	expectedRevision int64,
) (SetupPreparation, error) {
	item, err := s.repository.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupPreparation{}, err
	}
	if item.SetupWorkflowRunID != nil {
		previous, loadErr := s.repository.LoadSetupRun(ctx, tx, organizationID, projectID, *item.SetupWorkflowRunID)
		if loadErr != nil {
			return SetupPreparation{}, loadErr
		}
		if previous.Status != "failed" && previous.Status != "cancelled" {
			return SetupPreparation{}, Error{Code: CodeSetupRevisionConflict, Message: "创建流程正在运行或已经完成，不能重复启动"}
		}
	}
	if item.Revision != expectedRevision {
		return SetupPreparation{}, Error{Code: CodeSetupRevisionConflict, Message: "创建会话已更新，请刷新后重试"}
	}
	if item.State == "abandoned" || item.State == "completed" {
		return SetupPreparation{}, Error{Code: CodeSetupAbandoned, Message: "当前创建会话不能启动"}
	}
	if time.Now().After(item.ExpiresAt) {
		return SetupPreparation{}, Error{Code: CodeSetupIncomplete, Message: "创建会话已过期，请重新创建项目"}
	}
	if len(setupUploadKeys(item.InputSnapshot)) > 0 {
		return SetupPreparation{}, Error{Code: CodeSetupIncomplete, Message: "仍有商品图片尚未完成入库"}
	}

	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return SetupPreparation{}, err
	}
	if !found || product.CurrentVersionID == nil {
		return SetupPreparation{}, Error{Code: CodeProductRequired, Message: "请先填写并保存商品信息"}
	}
	productVersion, err := s.repository.LoadProductVersion(ctx, tx, organizationID, projectID, *product.CurrentVersionID)
	if err != nil {
		return SetupPreparation{}, err
	}
	references, err := s.repository.ListProductReferences(ctx, tx, organizationID, projectID, "active")
	if err != nil {
		return SetupPreparation{}, err
	}
	primaryCount := 0
	for _, reference := range references {
		if reference.IsPrimary {
			primaryCount++
		}
	}
	if len(references) == 0 || primaryCount != 1 {
		return SetupPreparation{}, Error{Code: CodeProductPrimaryImage, Message: "请至少上传一张商品图片并设置唯一主图"}
	}

	units, err := s.repository.ListScriptUnits(ctx, tx, organizationID, projectID, "active", 0, "", 1)
	if err != nil {
		return SetupPreparation{}, err
	}
	if len(units) != 1 || units[0].CurrentSourceVersionID == nil {
		return SetupPreparation{}, Error{Code: CodeScriptRequired, Message: "请先保存第一条广告脚本"}
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, units[0].ID, true)
	if err != nil {
		return SetupPreparation{}, err
	}
	sourceVersion, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unit.ID, *unit.CurrentSourceVersionID)
	if err != nil {
		return SetupPreparation{}, err
	}
	return SetupPreparation{
		Session: item, Product: product, ProductVersion: productVersion,
		ScriptUnit: unit, SourceScriptVersion: sourceVersion, References: references,
	}, nil
}

func (s *CatalogService) AttachSetupRun(ctx context.Context, tx pgx.Tx, preparation SetupPreparation, runID string) (SetupSession, error) {
	return s.repository.AttachSetupRun(ctx, tx, preparation.Session, runID, preparation)
}

func (s *CatalogService) GetSetupRun(ctx context.Context, db rowQuerier, organizationID, projectID, runID string) (SetupRun, error) {
	item, err := s.repository.LoadSetupRun(ctx, db, organizationID, projectID, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupRun{}, Error{Code: CodeSetupIncomplete, Message: "创建任务不存在", Cause: err}
	}
	return item, err
}

func (s *CatalogService) MarkSetupWaitingForLanguage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	setupRunID string,
	resolutionID string,
) error {
	return s.repository.MarkSetupWaitingForLanguage(ctx, tx, organizationID, projectID, setupSessionID, setupRunID, resolutionID)
}

func (s *CatalogService) MarkSetupLanguageConfirmed(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	setupRunID string,
	expectedRevision int64,
) (SetupSession, error) {
	return s.repository.MarkSetupLanguageConfirmed(ctx, tx, organizationID, projectID, setupSessionID, setupRunID, expectedRevision)
}

func (s *CatalogService) CommitInitialSetup(
	ctx context.Context,
	tx pgx.Tx,
	params InitialSetupCommitParams,
) (InitialSetupCommitResult, error) {
	item, err := s.repository.LoadSetupSession(ctx, tx, params.OrganizationID, params.ProjectID, params.SetupSessionID, true)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	if item.SetupWorkflowRunID == nil || *item.SetupWorkflowRunID != params.SetupRunID || item.State == "abandoned" {
		return InitialSetupCommitResult{}, Error{Code: CodeSetupRevisionConflict, Message: "创建任务身份已变化"}
	}
	if item.State == "completed" {
		return InitialSetupCommitResult{}, Error{Code: CodeSetupRevisionConflict, Message: "项目创建流程已经完成"}
	}
	if item.WorkflowTemplateVersionID != params.WorkflowTemplateVersionID {
		return InitialSetupCommitResult{}, Error{Code: CodeBindingMismatch, Message: "创建任务工作流模板已变化"}
	}

	product, found, err := s.repository.LockProduct(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	if !found || product.ID != params.ProductID || product.CurrentVersionID == nil || *product.CurrentVersionID != params.ProductVersionID {
		return InitialSetupCommitResult{}, Error{Code: CodeProductVersionStale, Message: "商品版本在创建期间已变化"}
	}
	version, err := s.repository.LoadProductVersion(ctx, tx, params.OrganizationID, params.ProjectID, params.ProductVersionID)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	references, referenceSetHash, err := s.validateTargetProductReferences(
		ctx, tx, params.OrganizationID, params.ProjectID, product.ID, params.ProductReferenceIDs,
	)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, params.OrganizationID, params.ProjectID, params.ScriptUnitID, true)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	if unit.Status == "archived" || unit.CurrentSourceVersionID == nil || *unit.CurrentSourceVersionID != params.SourceScriptVersionID ||
		unit.CurrentLocalizationID == nil || *unit.CurrentLocalizationID != params.LocalizationID || unit.ActiveUnitGenerationID != nil {
		return InitialSetupCommitResult{}, Error{Code: CodeScriptVersionStale, Message: "广告脚本或本地化版本在创建期间已变化"}
	}
	localization, err := s.repository.LoadLocalization(ctx, tx, params.OrganizationID, params.ProjectID, unit.ID, params.LocalizationID)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	if localization.Status != "approved" || localization.ReviewStatus != "approved" {
		return InitialSetupCommitResult{}, Error{Code: CodeLanguageConfirmation, Message: "脚本本地化结果尚未通过结构校验"}
	}

	bindingService := NewService(s.repository)
	bindings, err := bindingService.PrepareInitialBindings(ctx, tx, InitialBindingParams{
		OrganizationID: params.OrganizationID, ProjectID: params.ProjectID,
		WorkflowTemplateVersion: params.WorkflowTemplateVersionID, CreatedBy: params.CreatedBy,
		CompatibilityPolicy: "strict", VideoOverrides: json.RawMessage(`{}`),
		ProductionConfiguration: params.ProductionConfiguration,
		ConfigurationSnapshot:   params.CommerceConfiguration,
		ModelRoutingSnapshot:    params.ModelRoutingSnapshot,
		CapabilitySnapshot:      params.CapabilitySnapshot,
	})
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	referencePackID, err := s.repository.InsertProductReferencePack(ctx, tx, product, version, references, referenceSetHash, params.CreatedBy)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	unitGenerationID, unitGenerationNo, unitConfigurationHash, err := s.repository.InsertInitialUnitGeneration(ctx, tx, params, bindings, referencePackID, unit)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	if err := bindingService.ActivateInitialBindings(ctx, tx, params.ProjectID, bindings); err != nil {
		return InitialSetupCommitResult{}, err
	}
	output, err := json.Marshal(map[string]any{
		"setupSessionId":                  params.SetupSessionID,
		"projectGenerationId":             bindings.ProjectGenerationID,
		"videoProductionBindingId":        bindings.VideoBindingID,
		"videoProductionBindingRevision":  bindings.VideoBindingRevision,
		"commerceWorkflowBindingId":       bindings.CommerceBindingID,
		"commerceWorkflowBindingRevision": bindings.CommerceBindingRevision,
		"scriptUnitGenerationId":          unitGenerationID,
		"scriptUnitGenerationNo":          unitGenerationNo,
		"localizationId":                  params.LocalizationID,
		"referencePackId":                 referencePackID,
		"status":                          "completed",
	})
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	session, err := s.repository.ActivateInitialUnitGeneration(ctx, tx, params, unit, unitGenerationID, unitGenerationNo, output)
	if err != nil {
		return InitialSetupCommitResult{}, err
	}
	identity := UnitGenerationIdentity{
		ExecutionIdentity: ExecutionIdentity{
			OrganizationID: params.OrganizationID, ProjectID: params.ProjectID,
			ProjectGenerationID:             bindings.ProjectGenerationID,
			VideoProductionBindingID:        bindings.VideoBindingID,
			VideoProductionBindingRevision:  bindings.VideoBindingRevision,
			VideoProfileSnapshotHash:        bindings.VideoProfileSnapshotHash,
			CommerceWorkflowBindingID:       bindings.CommerceBindingID,
			CommerceWorkflowBindingRevision: bindings.CommerceBindingRevision,
			CommerceConfigurationHash:       bindings.CommerceConfigurationHash,
		},
		ProductID: params.ProductID, ScriptUnitID: params.ScriptUnitID,
		ScriptUnitRevision: unit.Revision + 1, UnitGenerationID: unitGenerationID,
		UnitGenerationNo: unitGenerationNo, UnitConfigurationHash: unitConfigurationHash,
	}
	return InitialSetupCommitResult{
		Session: session, Bindings: bindings, UnitGenerationID: unitGenerationID,
		UnitGenerationNo: unitGenerationNo, LocalizationID: params.LocalizationID,
		ReferencePackID: referencePackID, Identity: identity,
	}, nil
}

func setupTerminal(state string) bool {
	switch state {
	case "completed", "abandoned":
		return true
	default:
		return false
	}
}

func setupUploadKeys(raw json.RawMessage) []string {
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil
	}
	values, _ := snapshot["pendingUploadKeys"].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if key, ok := value.(string); ok && strings.TrimSpace(key) != "" {
			result = append(result, strings.TrimSpace(key))
		}
	}
	return result
}

func setupContainsUpload(raw json.RawMessage, storageKey string) bool {
	for _, key := range setupUploadKeys(raw) {
		if key == strings.TrimSpace(storageKey) {
			return true
		}
	}
	return false
}

func appendUniqueStrings(values []string, additional ...string) []string {
	seen := make(map[string]bool, len(values)+len(additional))
	result := make([]string, 0, len(values)+len(additional))
	for _, value := range append(values, additional...) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
