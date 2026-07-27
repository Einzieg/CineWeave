package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type commerceDirectVideoExecutionContract struct {
	ContractVersion   string              `json:"contractVersion"`
	Route             commerceDirectRoute `json:"route"`
	InputContractHash string              `json:"inputContractHash"`
	DurationSeconds   int                 `json:"durationSeconds"`
	Resolution        string              `json:"resolution"`
	AspectRatio       string              `json:"aspectRatio"`
	GenerateAudio     bool                `json:"generateAudio"`
}

type commerceDirectRoute struct {
	RouteKey               string             `json:"routeKey"`
	ModelProfileID         string             `json:"modelProfileId"`
	ModelProfileKey        string             `json:"modelProfileKey"`
	ModelProfileBindingID  string             `json:"modelProfileBindingId"`
	ProviderModelID        string             `json:"providerModelId"`
	ProviderAccountID      string             `json:"providerAccountId"`
	ProviderModelKey       string             `json:"providerModelKey"`
	VariantKey             string             `json:"variantKey"`
	CapabilitySnapshotHash string             `json:"capabilitySnapshotHash"`
	InputContract          VideoInputContract `json:"inputContract"`
}

type commerceDirectReferenceIdentity struct {
	ID                     string          `json:"id"`
	SourceType             string          `json:"sourceType"`
	SourceID               string          `json:"sourceId"`
	ProductReferenceID     *string         `json:"productReferenceId,omitempty"`
	ScriptReferenceImageID *string         `json:"scriptReferenceImageId,omitempty"`
	ArtifactID             string          `json:"artifactId"`
	MediaFileID            string          `json:"mediaFileId"`
	StorageKey             string          `json:"storageKey"`
	MimeType               string          `json:"mimeType"`
	ReferenceRole          string          `json:"referenceRole"`
	Ordinal                int             `json:"ordinal"`
	ContentHash            string          `json:"contentHash"`
	SourceRevision         int64           `json:"sourceRevision"`
	Snapshot               json.RawMessage `json:"snapshot"`
}

func (s *Service) validateCommerceDirectVideoExecutionRequest(
	ctx context.Context,
	req GatewayVideoCreateTaskRequest,
	input gatewayVideoInput,
) error {
	jobID := strings.TrimSpace(req.CommerceDirectVideoJobID)
	if jobID == "" {
		return nil
	}
	if strings.TrimSpace(req.ExecutionPlanID) != "" || strings.TrimSpace(req.RenderSegmentID) != "" ||
		strings.TrimSpace(req.StoryboardShotID) != "" || strings.TrimSpace(req.OperationID) != "" ||
		strings.TrimSpace(req.OperationItemID) != "" || req.OperationItemAttempt != 0 {
		return commerceDirectVideoContractError("带货视频直生成任务不能混用分镜或 Render Plan 身份")
	}
	var (
		organizationID, projectID, generationID, bindingID     string
		profileVersionID, profileSnapshotHash                  string
		modelProfileKey, modelProfileID, modelBindingID        string
		providerModelID, providerAccountID, providerModelKey   string
		variantKey, capabilityHash, scriptSnapshot, scriptHash string
		referenceSetHash, promptHash, workflowRunID, nodeRunID string
		status, executionContractHash                          string
		bindingRevision                                        int64
		duration                                               int
		aspectRatio, resolution                                string
		generateAudio                                          bool
		executionContractRaw                                   []byte
	)
	err := s.db.QueryRow(ctx, `
		SELECT organization_id::text, project_id::text,
		       project_production_generation_id::text,
		       video_production_binding_id::text, video_production_binding_revision,
		       video_profile_version_id::text, video_profile_snapshot_hash,
		       model_profile_key, COALESCE(model_profile_id::text, ''),
		       COALESCE(model_profile_binding_id::text, ''),
		       COALESCE(provider_model_id::text, ''), COALESCE(provider_account_id::text, ''),
		       provider_model_key, variant_key, capability_snapshot_hash,
		       requested_duration_seconds, aspect_ratio, resolution, generate_audio,
		       script_snapshot, script_hash, reference_set_hash, prompt_hash,
		       workflow_run_id::text, COALESCE(node_run_id::text, ''),
		       status, execution_contract, execution_contract_hash
		FROM commerce_direct_video_jobs
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`, jobID, strings.TrimSpace(req.OrganizationID), strings.TrimSpace(req.ProjectID)).Scan(
		&organizationID, &projectID, &generationID, &bindingID, &bindingRevision,
		&profileVersionID, &profileSnapshotHash, &modelProfileKey, &modelProfileID, &modelBindingID,
		&providerModelID, &providerAccountID, &providerModelKey, &variantKey, &capabilityHash,
		&duration, &aspectRatio, &resolution, &generateAudio, &scriptSnapshot, &scriptHash,
		&referenceSetHash, &promptHash, &workflowRunID, &nodeRunID, &status,
		&executionContractRaw, &executionContractHash,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return commerceDirectVideoContractError("带货视频直生成任务不存在或不属于当前项目")
		}
		return err
	}
	if status != "queued" && status != "running" {
		return commerceDirectVideoContractError("带货视频直生成任务已不再可执行")
	}
	if generationID != strings.TrimSpace(req.ProductionGenerationID) ||
		bindingID != strings.TrimSpace(req.VideoProductionBindingID) ||
		bindingRevision != req.VideoProductionBindingRevision ||
		profileVersionID != strings.TrimSpace(req.ProductionProfileVersionID) ||
		profileSnapshotHash != strings.TrimSpace(req.ProductionProfileSnapshotHash) ||
		workflowRunID != strings.TrimSpace(req.WorkflowRunID) ||
		nodeRunID == "" || nodeRunID != strings.TrimSpace(req.NodeRunID) {
		return commerceDirectVideoContractError("带货视频直生成任务的生产或工作流身份不一致")
	}
	if modelProfileID == "" || modelBindingID == "" || providerModelID == "" || providerAccountID == "" {
		return commerceDirectVideoContractError("带货视频直生成任务绑定的供应商模型已不可用")
	}
	if modelProfileKey != strings.TrimSpace(req.ModelProfileKey) ||
		providerModelID != strings.TrimSpace(req.ProviderModelID) ||
		capabilityHash != strings.TrimSpace(req.CapabilitySnapshotHash) {
		return commerceDirectVideoContractError("带货视频直生成任务的模型路由与冻结快照不一致")
	}
	contract, inputContractHash, err := decodeCommerceDirectVideoExecutionContract(
		executionContractRaw,
		executionContractHash,
	)
	if err != nil {
		return err
	}
	if contract.ContractVersion != "commerce-direct-video/v1" ||
		contract.Route.ModelProfileID != modelProfileID ||
		contract.Route.ModelProfileBindingID != modelBindingID ||
		contract.Route.ProviderModelID != providerModelID ||
		contract.Route.ProviderAccountID != providerAccountID ||
		contract.Route.ProviderModelKey != providerModelKey ||
		contract.Route.VariantKey != variantKey ||
		contract.Route.CapabilitySnapshotHash != capabilityHash {
		return commerceDirectVideoContractError("带货视频直生成执行路由与冻结快照不一致")
	}
	if inputContractHash != contract.InputContractHash ||
		strings.TrimSpace(req.InputContractKey) != contract.Route.InputContract.ContractKey ||
		strings.TrimSpace(req.InputContractHash) != contract.InputContractHash {
		return commerceDirectVideoContractError("带货视频直生成输入契约不一致")
	}
	if strings.TrimSpace(req.PromptSource) != "user_script" ||
		strings.TrimSpace(req.PromptHash) != promptHash || promptHash != scriptHash ||
		videoPromptTextHash(scriptSnapshot) != scriptHash ||
		input.Prompt != scriptSnapshot || videoPromptTextHash(input.Prompt) != scriptHash {
		return commerceDirectVideoContractError("实际发送的广告脚本与直生成任务快照不一致")
	}
	if math.Abs(input.DurationSeconds-float64(duration)) > 0.001 ||
		!equalVideoOption(input.AspectRatio, aspectRatio) ||
		!equalVideoOption(input.Resolution, resolution) {
		return commerceDirectVideoContractError("实际视频时长或分辨率与直生成任务快照不一致")
	}
	var rawInput struct {
		GenerateAudio bool `json:"generateAudio"`
	}
	if err := json.Unmarshal(req.Input, &rawInput); err != nil || rawInput.GenerateAudio != generateAudio {
		return commerceDirectVideoContractError("实际音频设置与直生成任务快照不一致")
	}
	if input.Mode != "" && !equalVideoOption(input.Mode, "image_to_video") {
		return commerceDirectVideoContractError("带货视频直生成任务必须使用图生视频请求模式")
	}
	model, err := s.GetModel(ctx, organizationID, providerModelID)
	if err != nil {
		return err
	}
	account, err := s.GetAccount(ctx, organizationID, providerAccountID)
	if err != nil {
		return err
	}
	if _, err := s.validateVideoInputContractsAdapterFixture(ctx, account, model, []VideoInputContract{contract.Route.InputContract}); err != nil {
		return err
	}
	if err := validateGatewayVideoReferencesForContract(req.References, contract.Route.InputContract); err != nil {
		return err
	}
	references, err := s.loadCommerceDirectVideoReferences(ctx, organizationID, projectID, jobID)
	if err != nil {
		return err
	}
	currentReferenceSetHash, err := stableJSONHash(references)
	if err != nil || currentReferenceSetHash != referenceSetHash {
		return commerceDirectVideoContractError("带货视频直生成参考图快照完整性校验失败")
	}
	if len(references) != len(req.References) {
		return commerceDirectVideoContractError("实际参考图数量与直生成任务快照不一致")
	}
	for index, expected := range references {
		actual := req.References[index]
		if gatewayVideoReferenceRole(actual) != expected.ReferenceRole ||
			actual.SourceType != expected.SourceType || actual.SourceID != expected.SourceID ||
			actual.SourceVersion != directVideoSourceRevision(expected.SourceRevision) ||
			actual.ArtifactID != expected.ArtifactID || actual.MediaFileID != expected.MediaFileID ||
			actual.ContentHash != expected.ContentHash {
			return commerceDirectVideoContractError("实际商品参考图与直生成任务快照不一致")
		}
	}
	_ = referenceSetHash
	return nil
}

func decodeCommerceDirectVideoExecutionContract(
	raw []byte,
	expectedHash string,
) (commerceDirectVideoExecutionContract, string, error) {
	var contract commerceDirectVideoExecutionContract
	hash, err := stableJSONHash(json.RawMessage(raw))
	if err != nil {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约已损坏")
	}
	if hash != strings.TrimSpace(expectedHash) {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约完整性校验失败")
	}
	var envelope struct {
		Route json.RawMessage `json:"route"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Route) == 0 {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约已损坏")
	}
	var routeEnvelope struct {
		InputContract json.RawMessage `json:"inputContract"`
	}
	if err := json.Unmarshal(envelope.Route, &routeEnvelope); err != nil || len(routeEnvelope.InputContract) == 0 {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约已损坏")
	}
	inputContractHash, err := stableJSONHash(routeEnvelope.InputContract)
	if err != nil {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约已损坏")
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		return contract, "", commerceDirectVideoContractError("带货视频直生成执行契约已损坏")
	}
	return contract, inputContractHash, nil
}

func (s *Service) loadCommerceDirectVideoReferences(
	ctx context.Context,
	organizationID string,
	projectID string,
	jobID string,
) ([]commerceDirectReferenceIdentity, error) {
	rows, err := s.db.Query(ctx, `
		SELECT reference.id::text, reference.source_type, reference.source_id::text,
		       reference.product_reference_id::text, reference.script_reference_image_id::text,
		       reference.artifact_id::text, reference.media_file_id::text,
		       artifact.storage_key, artifact.mime_type, reference.reference_role,
		       reference.ordinal, reference.content_hash, reference.source_revision, reference.snapshot
		FROM commerce_direct_video_job_references reference
		JOIN artifacts artifact ON artifact.id = reference.artifact_id
		WHERE reference.job_id = $1 AND reference.organization_id = $2 AND reference.project_id = $3
		ORDER BY reference.ordinal
	`, jobID, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]commerceDirectReferenceIdentity, 0)
	for rows.Next() {
		var item commerceDirectReferenceIdentity
		var productReferenceID, scriptReferenceID pgtype.Text
		if err := rows.Scan(&item.ID, &item.SourceType, &item.SourceID,
			&productReferenceID, &scriptReferenceID, &item.ArtifactID, &item.MediaFileID,
			&item.StorageKey, &item.MimeType, &item.ReferenceRole, &item.Ordinal,
			&item.ContentHash, &item.SourceRevision, &item.Snapshot); err != nil {
			return nil, err
		}
		item.ProductReferenceID = directVideoPGTextPointer(productReferenceID)
		item.ScriptReferenceImageID = directVideoPGTextPointer(scriptReferenceID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func commerceDirectVideoContractError(message string) error {
	return &StandardErrorError{Standard: StandardError{
		Code: CodeRenderPlanReplanRequired, Message: message, Retryable: false,
	}}
}

func directVideoSourceRevision(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func stableJSONHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return "", err
	}
	raw, err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func directVideoPGTextPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
