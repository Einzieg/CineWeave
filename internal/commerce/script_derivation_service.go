package commerce

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var scriptDerivationPromptKeys = []string{
	"commerce_script_derivation_candidate_planner",
	"commerce_script_derivation_generator",
	"commerce_script_derivation_reviewer",
	"commerce_script_derivation_reviser",
}

func (s *ScriptDerivationService) GetBatch(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	batchID string,
	includeLineage bool,
) (ScriptDerivationBatch, error) {
	return s.repository.LoadScriptDerivationDetail(
		ctx, db, organizationID, projectID, batchID, includeLineage,
	)
}

func (s *ScriptDerivationService) ListBatches(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
	sourceScriptUnitID string,
	cursor string,
	limit int,
) (ScriptDerivationBatchList, error) {
	status = strings.TrimSpace(status)
	switch status {
	case "", "all", "queued", "running", "partial_succeeded", "succeeded", "failed", "cancelling", "cancelled":
	default:
		return ScriptDerivationBatchList{}, Error{Code: CodeScriptDerivationInvalid, Message: "裂变任务状态筛选无效"}
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	cursorTime, cursorID, err := decodeScriptDerivationCursor(cursor)
	if err != nil {
		return ScriptDerivationBatchList{}, Error{Code: CodeScriptDerivationInvalid, Message: "裂变任务游标无效", Cause: err}
	}
	items, err := s.repository.ListScriptDerivationBatches(
		ctx, db, organizationID, projectID, status, sourceScriptUnitID,
		cursorTime, cursorID, limit+1,
	)
	if err != nil {
		return ScriptDerivationBatchList{}, err
	}
	result := ScriptDerivationBatchList{Items: items}
	if len(items) > limit {
		result.HasMore = true
		result.Items = items[:limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = encodeScriptDerivationCursor(last.CreatedAt, last.ID)
	}
	return result, nil
}

func (s *ScriptDerivationService) PrepareRetryBatch(
	ctx context.Context,
	tx pgx.Tx,
	batchID string,
	workflowRunID string,
	organizationID string,
	projectID string,
	createdBy string,
	idempotencyKey string,
	requestHash string,
) (PreparedScriptDerivation, error) {
	source, err := s.repository.LoadScriptDerivationBatch(
		ctx, tx, organizationID, projectID, batchID, true,
	)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	items, err := s.repository.ListScriptDerivationItems(ctx, tx, source.ID)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	retryItems := make([]ScriptDerivationItem, 0)
	variations := make([]ScriptDerivationVariation, 0)
	for _, item := range items {
		if item.Status != "failed_retryable" {
			continue
		}
		retryItems = append(retryItems, item)
		variations = append(variations, ScriptDerivationVariation{
			Ordinal: len(variations) + 1, Key: item.VariationKey,
			Label: item.VariationLabel, Brief: item.VariationBrief,
		})
	}
	if len(retryItems) == 0 {
		return PreparedScriptDerivation{}, Error{Code: CodeScriptDerivationState, Message: "当前裂变任务没有可重试的失败条目"}
	}
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	if !found {
		return PreparedScriptDerivation{}, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	var configurationEnvelope struct {
		ProductionConfiguration videoproduction.ProductionConfigurationSnapshot `json:"productionConfiguration"`
	}
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &configurationEnvelope); err != nil {
		return PreparedScriptDerivation{}, Error{Code: CodeScriptDerivationInvalid, Message: "带货项目生产配置无法解析", Cause: err}
	}
	profileKey := strings.TrimSpace(configurationEnvelope.ProductionConfiguration.ScriptModelProfileKey)
	if profileKey == "" {
		profileKey = "script_agent_default"
	}
	routing, err := s.repository.ResolveScriptDerivationRoute(ctx, tx, organizationID, profileKey)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	promptContract, err := resolveScriptDerivationPromptContract(ctx, tx, organizationID, projectID)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	routingHash, err := DirectVideoHash(routing)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	positions := make([]ScriptUnitPosition, len(retryItems))
	for index, item := range retryItems {
		positions[index] = ScriptUnitPosition{
			UnitNo: item.ReservedUnitNo, SortOrder: item.ReservedSortOrder,
		}
	}
	newBatchID := uuid.NewString()
	rootID := source.ID
	if source.RootBatchID != nil {
		rootID = *source.RootBatchID
	}
	batch := ScriptDerivationBatch{
		ID: newBatchID, OrganizationID: organizationID, ProjectID: projectID,
		ProductID: source.ProductID, SourceScriptUnitID: source.SourceScriptUnitID,
		SourceContentSnapshot: source.SourceContentSnapshot, SourceContentHash: source.SourceContentHash,
		ProductVersionID: source.ProductVersionID, ProductSnapshotHash: source.ProductSnapshotHash,
		ProductionGenerationID:         production.Generation.ID,
		VideoProductionBindingID:       production.VideoBinding.ID,
		VideoProductionBindingRevision: production.VideoBinding.Revision,
		ProductionConfigurationHash:    production.CommerceBinding.ConfigurationHash,
		ScriptModelProfileKey:          profileKey, ModelProfileBindingID: &routing.ModelProfileBindingID,
		ModelProfileBindingRevision: routing.BindingRevision, ProviderModelID: &routing.ProviderModelID,
		RoutingSnapshotHash: routingHash, PromptContract: promptContract,
		Dimension: source.Dimension, Instruction: source.Instruction, Preserve: source.Preserve,
		Variations: variations, RequestedCount: len(variations), RootBatchID: &rootID,
		RetryOfBatchID: &source.ID, RetryDepth: source.RetryDepth + 1,
		WorkflowRunID: derivationStringPointer(workflowRunID), Status: "queued",
		QueuedCount: len(variations), CreatedBy: derivationStringPointer(createdBy),
	}
	if err := s.repository.InsertScriptDerivationBatch(
		ctx, tx, batch, idempotencyKey, requestHash, positions, retryItems,
	); err != nil {
		return PreparedScriptDerivation{}, err
	}
	batch, err = s.repository.LoadScriptDerivationBatch(ctx, tx, organizationID, projectID, batch.ID, true)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	return PreparedScriptDerivation{
		Batch: batch, Product: product, Production: production, Positions: positions,
	}, nil
}

func (s *ScriptDerivationService) CancelBatch(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	batchID string,
) (ScriptDerivationBatch, error) {
	item, err := s.repository.LoadScriptDerivationBatch(
		ctx, tx, organizationID, projectID, batchID, true,
	)
	if err != nil {
		return ScriptDerivationBatch{}, err
	}
	if err := s.repository.CancelScriptDerivationBatch(ctx, tx, item); err != nil {
		return ScriptDerivationBatch{}, err
	}
	return s.repository.LoadScriptDerivationBatch(ctx, tx, organizationID, projectID, batchID, true)
}

func encodeScriptDerivationCursor(createdAt time.Time, id string) string {
	raw, _ := json.Marshal(map[string]string{
		"createdAt": createdAt.UTC().Format(time.RFC3339Nano), "id": id,
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeScriptDerivationCursor(value string) (time.Time, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", err
	}
	var cursor struct {
		CreatedAt string `json:"createdAt"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return time.Time{}, "", err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	if err != nil || strings.TrimSpace(cursor.ID) == "" {
		if err == nil {
			err = errors.New("cursor id is required")
		}
		return time.Time{}, "", err
	}
	return createdAt, cursor.ID, nil
}

type ScriptDerivationService struct {
	repository *Repository
	catalog    *CatalogService
}

type PreparedScriptDerivationPreview struct {
	Input          ScriptDerivationPreviewInput
	Source         ScriptUnit
	ProductVersion ProductVersion
	Production     ProductionContext
	Routing        ScriptDerivationRoutingSnapshot
	Prompt         promptsvc.ResolvedPrompt
}

func NewScriptDerivationService(repository *Repository) *ScriptDerivationService {
	if repository == nil {
		repository = NewRepository()
	}
	return &ScriptDerivationService{repository: repository, catalog: NewCatalogService(repository)}
}

func NormalizeScriptDerivationInput(input *CreateScriptDerivationInput) error {
	if err := normalizeScriptDerivationCore(&input.Dimension, &input.Instruction, &input.Preserve); err != nil {
		return err
	}
	if len(input.Variations) < 1 || len(input.Variations) > ScriptDerivationMaxVariations {
		return Error{Code: CodeScriptDerivationInvalid, Message: "裂变条目数量必须为 1 到 20"}
	}
	seenKeys := make(map[string]bool, len(input.Variations))
	for index := range input.Variations {
		item := &input.Variations[index]
		item.Ordinal = index + 1
		item.Key = strings.TrimSpace(item.Key)
		item.Label = strings.TrimSpace(item.Label)
		item.Brief = strings.TrimSpace(item.Brief)
		if item.Key == "" || item.Label == "" || item.Brief == "" {
			return Error{Code: CodeScriptDerivationInvalid, Message: "裂变候选的标识、名称和说明不能为空"}
		}
		if input.Dimension == "language" {
			locale, err := NormalizeLocale(item.Key)
			if err != nil {
				return Error{
					Code:    CodeScriptDerivationInvalid,
					Message: "语言裂变候选标识必须是有效的 BCP 47 语言标记",
					Cause:   err,
				}
			}
			item.Key = locale
		}
		if seenKeys[item.Key] {
			return Error{Code: CodeScriptDerivationInvalid, Message: "裂变候选标识不能重复"}
		}
		seenKeys[item.Key] = true
	}
	return nil
}

func NormalizeScriptDerivationPreviewInput(input *ScriptDerivationPreviewInput) error {
	input.SourceScriptUnitID = strings.TrimSpace(input.SourceScriptUnitID)
	if input.SourceScriptUnitID == "" {
		return Error{Code: CodeScriptDerivationInvalid, Message: "必须指定源广告脚本"}
	}
	if input.Count < 1 || input.Count > ScriptDerivationMaxVariations {
		return Error{Code: CodeScriptDerivationInvalid, Message: "裂变候选数量必须为 1 到 20"}
	}
	if err := normalizeScriptDerivationCore(&input.Dimension, &input.Instruction, &input.Preserve); err != nil {
		return err
	}
	seen := make(map[string]bool, len(input.CandidateValues))
	values := make([]string, 0, len(input.CandidateValues))
	for _, value := range input.CandidateValues {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			return Error{Code: CodeScriptDerivationInvalid, Message: "裂变候选值不能重复"}
		}
		seen[key] = true
		values = append(values, value)
	}
	if len(values) > input.Count {
		return Error{Code: CodeScriptDerivationInvalid, Message: "候选值数量不能超过计划生成数量"}
	}
	input.CandidateValues = values
	return nil
}

func normalizeScriptDerivationCore(dimension, instruction *string, preserve *[]string) error {
	*dimension = strings.TrimSpace(*dimension)
	*instruction = strings.TrimSpace(*instruction)
	switch *dimension {
	case "scene", "hook", "audience", "tone", "language", "cta", "custom":
	default:
		return Error{Code: CodeScriptDerivationInvalid, Message: "裂变维度无效"}
	}
	if *instruction == "" {
		return Error{Code: CodeScriptDerivationInvalid, Message: "裂变要求不能为空"}
	}
	allowedPreserve := map[string]bool{
		"product_facts": true, "selling_points": true, "prohibited_claims": true,
		"language": true, "cta": true, "approximate_duration": true,
	}
	seen := make(map[string]bool)
	normalized := make([]string, 0, len(*preserve))
	for _, value := range *preserve {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if !allowedPreserve[value] {
			return Error{Code: CodeScriptDerivationInvalid, Message: "裂变保持项无效"}
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	*preserve = normalized
	return nil
}

func (s *ScriptDerivationService) PreparePreview(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	input ScriptDerivationPreviewInput,
) (PreparedScriptDerivationPreview, error) {
	if err := NormalizeScriptDerivationPreviewInput(&input); err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	production, err := s.repository.LoadActiveProductionContext(ctx, db, organizationID, projectID)
	if err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	if production.ProjectLocked || production.LifecycleStatus == "deleting" {
		return PreparedScriptDerivationPreview{}, Error{
			Code: CodeProjectLocked, Message: "项目当前不能预览脚本裂变", Retryable: true,
		}
	}
	product, err := s.repository.LoadProduct(ctx, db, organizationID, projectID, false)
	if errors.Is(err, pgx.ErrNoRows) || product.CurrentVersion == nil {
		return PreparedScriptDerivationPreview{}, Error{
			Code: CodeProductRequired, Message: "请先完成商品配置", Cause: err,
		}
	}
	if err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	source, err := s.repository.LoadScriptUnit(
		ctx, db, organizationID, projectID, input.SourceScriptUnitID, false,
	)
	if errors.Is(err, pgx.ErrNoRows) || source.Status == "archived" {
		return PreparedScriptDerivationPreview{}, Error{
			Code: CodeScriptUnitRequired, Message: "来源脚本不存在或已归档", Cause: err,
		}
	}
	if err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	currentContent, err := NewCatalogService(s.repository).ResolveCurrentScriptContent(
		ctx, db, organizationID, projectID, source.ID,
	)
	if err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	source.CurrentContent = currentContent.Content
	source.CurrentContentHash = currentContent.ContentHash
	var configurationEnvelope struct {
		ProductionConfiguration videoproduction.ProductionConfigurationSnapshot `json:"productionConfiguration"`
	}
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &configurationEnvelope); err != nil {
		return PreparedScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "带货项目生产配置无法解析", Cause: err,
		}
	}
	profileKey := strings.TrimSpace(configurationEnvelope.ProductionConfiguration.ScriptModelProfileKey)
	if profileKey == "" {
		profileKey = "script_agent_default"
	}
	routing, err := s.repository.ResolveScriptDerivationRoute(ctx, db, organizationID, profileKey)
	if err != nil {
		return PreparedScriptDerivationPreview{}, err
	}
	prompt, err := promptsvc.NewService(db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		TemplateKey:    scriptDerivationPromptKeys[0],
	})
	if err != nil {
		return PreparedScriptDerivationPreview{}, Error{
			Code:    CodeScriptDerivationInvalid,
			Message: fmt.Sprintf("脚本裂变 Prompt %s 不可用", scriptDerivationPromptKeys[0]),
			Cause:   err,
		}
	}
	return PreparedScriptDerivationPreview{
		Input: input, Source: source, ProductVersion: *product.CurrentVersion,
		Production: production, Routing: routing, Prompt: prompt,
	}, nil
}

func (prepared PreparedScriptDerivationPreview) PromptVariables() (map[string]any, error) {
	contextValue := map[string]any{
		"request": map[string]any{
			"count": prepared.Input.Count, "dimension": prepared.Input.Dimension,
			"instruction":     prepared.Input.Instruction,
			"candidateValues": prepared.Input.CandidateValues,
			"preserve":        prepared.Input.Preserve,
		},
		"sourceScript": map[string]any{
			"id": prepared.Source.ID, "unitNo": prepared.Source.UnitNo,
			"title": prepared.Source.Title, "content": prepared.Source.CurrentContent,
			"contentHash":            prepared.Source.CurrentContentHash,
			"languageMode":           prepared.Source.LanguageMode,
			"explicitTargetLanguage": prepared.Source.ExplicitTargetLanguage,
			"targetDurationSeconds":  prepared.Source.TargetDurationSeconds,
			"targetPlatform":         prepared.Source.TargetPlatform,
		},
		"product": prepared.ProductVersion,
	}
	raw, err := json.Marshal(contextValue)
	if err != nil {
		return nil, err
	}
	return map[string]any{"input": map[string]any{"context": string(raw)}}, nil
}

func DecodeScriptDerivationPreview(
	raw string,
	input ScriptDerivationPreviewInput,
	source ScriptUnit,
) (ScriptDerivationPreview, error) {
	var modelOutput struct {
		ContractVersion string                      `json:"contractVersion"`
		Dimension       string                      `json:"dimension"`
		Instruction     string                      `json:"instruction"`
		Preserve        []string                    `json:"preserve"`
		Variations      []ScriptDerivationVariation `json:"variations"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(stripScriptDerivationJSONFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&modelOutput); err != nil {
		return ScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "裂变预览模型输出不是有效 JSON", Cause: err,
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("JSON 后存在额外内容")
		}
		return ScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "裂变预览模型输出包含额外内容", Cause: err,
		}
	}
	if strings.TrimSpace(modelOutput.ContractVersion) != "commerce-script-derivation-preview/v1" {
		return ScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "裂变预览输出契约版本无效",
		}
	}
	if strings.TrimSpace(modelOutput.Dimension) != input.Dimension {
		return ScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "裂变预览输出维度与请求不一致",
		}
	}
	if len(modelOutput.Variations) != input.Count {
		return ScriptDerivationPreview{}, Error{
			Code: CodeScriptDerivationInvalid, Message: "裂变预览输出数量与请求不一致",
		}
	}
	normalized := CreateScriptDerivationInput{
		Dimension: input.Dimension, Instruction: input.Instruction,
		Preserve: input.Preserve, Variations: modelOutput.Variations,
	}
	if err := NormalizeScriptDerivationInput(&normalized); err != nil {
		return ScriptDerivationPreview{}, err
	}
	return ScriptDerivationPreview{
		ContractVersion:    "commerce-script-derivation-preview/v1",
		SourceScriptUnitID: source.ID, SourceScriptTitle: source.Title,
		SourceContentHash: source.CurrentContentHash,
		Dimension:         normalized.Dimension, Instruction: normalized.Instruction,
		Preserve: normalized.Preserve, Variations: normalized.Variations,
	}, nil
}

func stripScriptDerivationJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s *ScriptDerivationService) PrepareBatch(
	ctx context.Context,
	tx pgx.Tx,
	params PrepareScriptDerivationParams,
) (PreparedScriptDerivation, error) {
	if err := NormalizeScriptDerivationInput(&params.Input); err != nil {
		return PreparedScriptDerivation{}, err
	}
	production, err := s.repository.LockActiveProductionContext(
		ctx, tx, params.OrganizationID, params.ProjectID,
	)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	if production.ProjectLocked || production.LifecycleStatus == "deleting" {
		return PreparedScriptDerivation{}, Error{Code: CodeProjectLocked, Message: "项目当前不能启动脚本裂变", Retryable: true}
	}
	product, found, err := s.repository.LockProduct(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	if !found || product.CurrentVersion == nil {
		return PreparedScriptDerivation{}, Error{Code: CodeProductRequired, Message: "请先完成商品配置"}
	}
	source, err := s.repository.LoadScriptUnit(
		ctx, tx, params.OrganizationID, params.ProjectID, params.ScriptUnitID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) || source.Status == "archived" {
		return PreparedScriptDerivation{}, Error{Code: CodeScriptUnitRequired, Message: "来源脚本不存在或已归档", Cause: err}
	}
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	currentContent, err := NewCatalogService(s.repository).ResolveCurrentScriptContent(
		ctx, tx, params.OrganizationID, params.ProjectID, source.ID,
	)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	sourceContent := currentContent.Content
	sourceHash := currentContent.ContentHash
	var configurationEnvelope struct {
		ProductionConfiguration videoproduction.ProductionConfigurationSnapshot `json:"productionConfiguration"`
	}
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &configurationEnvelope); err != nil {
		return PreparedScriptDerivation{}, Error{Code: CodeScriptDerivationInvalid, Message: "带货项目生产配置无法解析", Cause: err}
	}
	profileKey := strings.TrimSpace(configurationEnvelope.ProductionConfiguration.ScriptModelProfileKey)
	if profileKey == "" {
		profileKey = "script_agent_default"
	}
	routing, err := s.repository.ResolveScriptDerivationRoute(ctx, tx, params.OrganizationID, profileKey)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	promptContract, err := resolveScriptDerivationPromptContract(
		ctx, tx, params.OrganizationID, params.ProjectID,
	)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	routingHash, err := DirectVideoHash(routing)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	productHash, err := DirectVideoHash(product.CurrentVersion)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	positions, err := s.repository.ReserveScriptUnitPositions(ctx, tx, product.ID, len(params.Input.Variations))
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	batchID := strings.TrimSpace(params.BatchID)
	if batchID == "" {
		batchID = uuid.NewString()
	}
	rootBatchID := batchID
	batch := ScriptDerivationBatch{
		ID: batchID, OrganizationID: params.OrganizationID, ProjectID: params.ProjectID,
		ProductID: product.ID, SourceScriptUnitID: source.ID,
		SourceContentSnapshot: sourceContent, SourceContentHash: sourceHash,
		ProductVersionID: product.CurrentVersion.ID, ProductSnapshotHash: productHash,
		ProductionGenerationID:         production.Generation.ID,
		VideoProductionBindingID:       production.VideoBinding.ID,
		VideoProductionBindingRevision: production.VideoBinding.Revision,
		ProductionConfigurationHash:    production.CommerceBinding.ConfigurationHash,
		ScriptModelProfileKey:          profileKey,
		ModelProfileBindingID:          &routing.ModelProfileBindingID,
		ModelProfileBindingRevision:    routing.BindingRevision,
		ProviderModelID:                &routing.ProviderModelID, RoutingSnapshotHash: routingHash,
		PromptContract: promptContract, Dimension: params.Input.Dimension,
		Instruction: params.Input.Instruction, Preserve: params.Input.Preserve,
		Variations: params.Input.Variations, RequestedCount: len(params.Input.Variations),
		RootBatchID: &rootBatchID, RetryDepth: 0, WorkflowRunID: derivationStringPointer(params.WorkflowRunID),
		Status: "queued", QueuedCount: len(params.Input.Variations),
		CreatedBy: derivationStringPointer(params.CreatedBy),
	}
	if err := s.repository.InsertScriptDerivationBatch(
		ctx, tx, batch, params.IdempotencyKey, params.RequestHash, positions, nil,
	); err != nil {
		return PreparedScriptDerivation{}, err
	}
	batch, err = s.repository.LoadScriptDerivationBatch(
		ctx, tx, params.OrganizationID, params.ProjectID, batch.ID, true,
	)
	if err != nil {
		return PreparedScriptDerivation{}, err
	}
	return PreparedScriptDerivation{
		Batch: batch, Product: product, Production: production, Positions: positions,
	}, nil
}

func derivationStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func resolveScriptDerivationPromptContract(
	ctx context.Context,
	db promptsvc.QueryRower,
	organizationID string,
	projectID string,
) (ScriptDerivationPromptContract, error) {
	service := promptsvc.NewService(db)
	resolved := make([]ScriptDerivationPromptBinding, 0, len(scriptDerivationPromptKeys))
	for _, key := range scriptDerivationPromptKeys {
		item, err := service.Resolve(ctx, promptsvc.ResolveRequest{
			OrganizationID: organizationID, ProjectID: projectID, TemplateKey: key,
		})
		if err != nil {
			return ScriptDerivationPromptContract{}, Error{
				Code: CodeScriptDerivationInvalid, Message: fmt.Sprintf("脚本裂变 Prompt %s 不可用", key), Cause: err,
			}
		}
		resolved = append(resolved, ScriptDerivationPromptBinding{
			TemplateKey: item.TemplateKey, PromptVersionID: item.VersionID,
			ContentHash: item.ContentHash, Metadata: item.Metadata,
		})
	}
	return ScriptDerivationPromptContract{
		CandidatePlanner: resolved[0], Generator: resolved[1],
		Reviewer: resolved[2], Reviser: resolved[3],
	}, nil
}

func scriptDerivationKind(dimension string) string {
	switch dimension {
	case "scene":
		return "scene_variant"
	case "hook":
		return "hook_variant"
	case "audience":
		return "audience_variant"
	case "tone":
		return "tone_variant"
	case "language":
		return "language_variant"
	case "cta":
		return "cta_variant"
	default:
		return "custom_variant"
	}
}
