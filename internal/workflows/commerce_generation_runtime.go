package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	commercePreparationWorkflowType        = "commerce_script_unit_preparation"
	commerceScriptOrganizationWorkflowType = "commerce_script_organization"
	commerceStoryboardWorkflowType         = "commerce_storyboard_planning"
	commerceImagePromptWorkflowType        = "commerce_reference_image_prompts"
	commerceReferenceImageWorkflowType     = "commerce_reference_images"
	commerceVideoPromptWorkflowType        = "commerce_video_prompts"
	commerceShotVideoWorkflowType          = "commerce_shot_videos"
	commerceFinalComposeWorkflowType       = "commerce_final_compose"
)

// CommerceGenerationRuntime is the durable persistence boundary for commerce
// generation workflows. It intentionally has no Provider Gateway dependency:
// provider calls stay in the existing agent activities and only validated,
// immutable results cross this boundary.
type CommerceGenerationRuntime struct {
	db         *pgxpool.Pool
	repository *commerce.Repository
	service    *commerce.Service
	catalog    *commerce.CatalogService
	runs       *commerce.ProductionRunService
}

func NewCommerceGenerationRuntime(db *pgxpool.Pool) *CommerceGenerationRuntime {
	repository := commerce.NewRepository()
	return &CommerceGenerationRuntime{
		db:         db,
		repository: repository,
		service:    commerce.NewService(repository),
		catalog:    commerce.NewCatalogService(repository),
		runs:       commerce.NewProductionRunService(repository),
	}
}

type commerceGenerationFrozenState struct {
	Production       commerce.ProductionContext
	Generation       commerce.UnitGenerationContext
	Product          commerce.Product
	ProductVersion   commerce.ProductVersion
	Unit             commerce.ScriptUnit
	SourceVersion    commerce.ScriptVersion
	SourceSegments   []commerce.ScriptSegment
	Localization     commerce.ScriptLocalization
	ReferencePack    commerce.ProductReferencePack
	Template         commerce.WorkflowTemplateVersion
	BindingConfig    commerceGenerationBindingConfiguration
	PromptBindings   map[string]commerceSetupPromptBinding
	ModelContracts   map[string]commerceSetupModelContract
	AgentBindings    map[string]CommerceAgentBinding
	StoryboardConfig commerceStoryboardFrozenConfiguration
}

type commercePreparationFrozenState struct {
	Production         commerce.ProductionContext
	Product            commerce.Product
	ProductVersion     commerce.ProductVersion
	Unit               commerce.ScriptUnit
	SourceVersion      commerce.ScriptVersion
	SourceSegments     []commerce.ScriptSegment
	ReferencePack      commerce.ProductReferencePack
	Template           commerce.WorkflowTemplateVersion
	BindingConfig      commerceGenerationBindingConfiguration
	AgentBindings      map[string]CommerceAgentBinding
	Snapshot           CommerceScriptUnitPreparationSnapshot
	StoryboardStrategy commerce.StoryboardStrategy
}

type commerceGenerationBindingConfiguration struct {
	SchemaVersion               int                                             `json:"schemaVersion"`
	WorkflowTemplateVersionID   string                                          `json:"workflowTemplateVersionId"`
	WorkflowTemplateContentHash string                                          `json:"workflowTemplateContentHash"`
	ProductionConfiguration     videoproduction.ProductionConfigurationSnapshot `json:"productionConfiguration"`
	PromptBindings              json.RawMessage                                 `json:"promptBindings"`
	LanguageContract            json.RawMessage                                 `json:"languageContract"`
}

type commerceUnitConfigurationSnapshot struct {
	SchemaVersion                   int    `json:"schemaVersion"`
	ProjectGenerationID             string `json:"projectGenerationId"`
	CommerceWorkflowBindingID       string `json:"commerceWorkflowBindingId"`
	CommerceWorkflowBindingRevision int64  `json:"commerceWorkflowBindingRevision"`
	VideoProductionBindingID        string `json:"videoProductionBindingId"`
	VideoProductionBindingRevision  int64  `json:"videoProductionBindingRevision"`
	WorkflowTemplateVersionID       string `json:"workflowTemplateVersionId"`
	ProductVersionID                string `json:"productVersionId"`
	SourceScriptVersionID           string `json:"sourceScriptVersionId"`
	LocalizationID                  string `json:"localizationId"`
	ReferencePackID                 string `json:"referencePackId"`
	TargetDurationSeconds           int    `json:"targetDurationSeconds"`
	TargetPlatform                  string `json:"targetPlatform"`
	StoryboardStrategy              string `json:"storyboardStrategy"`
	SegmentationPolicyVersion       string `json:"segmentationPolicyVersion"`
}

type commerceStoryboardFrozenConfiguration struct {
	AspectRatio                string
	TimelineTimebase           int64
	FPSNumerator               int
	FPSDenominator             int
	Strategy                   commerce.StoryboardStrategy
	SegmentationPolicyVersion  string
	VideoExecutionEnvelope     commerce.VideoExecutionEnvelope
	VideoExecutionEnvelopeHash string
	AllowedDurations           []int
	TimingPolicy               CommerceTimingPolicy
}

type commerceWorkflowRunRecord struct {
	OrganizationID                 string
	ProjectID                      string
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	WorkflowType                   string
	Status                         string
	Input                          json.RawMessage
	AttemptGeneration              int
	CreatedBy                      string
	OutboxInput                    json.RawMessage
	OutboxInputHash                string
}

type commerceLanguageResolutionRow struct {
	ID                    string
	SourceScriptVersionID string
	LanguageMode          string
	SourceLanguage        string
	TargetLanguage        string
	Confidence            float64
	Reasoning             string
	NeedsConfirmation     bool
	Status                string
	Revision              int64
}

func (r *CommerceGenerationRuntime) AssertCommerceWorkflowIdentity(
	ctx context.Context,
	input CommerceAgentCallInput,
) error {
	if r == nil || r.db == nil {
		return commerce.Error{Code: CommerceCodeActivityPortUnavailable, Message: "带货视频持久化运行时未配置"}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	switch input.Phase {
	case CommercePhasePreparation:
		if input.PreparationIdentity == nil || input.GenerationIdentity != nil {
			return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "脚本准备 Agent 身份无效"}
		}
		if _, err = r.lockPreparationState(ctx, tx, *input.PreparationIdentity); err != nil {
			return err
		}
		return r.lockCommercePreparationAgentWorkflow(ctx, tx, input)
	case CommercePhaseScriptOrganization, CommercePhaseStoryboard:
		if input.GenerationIdentity == nil || input.PreparationIdentity != nil {
			return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "销售脚本或分镜 Agent 身份无效"}
		}
		if _, err = r.lockGenerationState(ctx, tx, *input.GenerationIdentity); err != nil {
			return err
		}
		return r.lockCommerceSalesScriptAgentWorkflow(ctx, tx, input)
	case CommercePhaseImagePrompt, CommercePhaseImageFidelity, CommercePhaseVideoPrompt:
		if input.GenerationIdentity == nil || input.PreparationIdentity != nil || strings.TrimSpace(input.SubjectKey) == "" {
			return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "参考图 Agent 身份无效"}
		}
		if _, err = r.lockGenerationState(ctx, tx, *input.GenerationIdentity); err != nil {
			return err
		}
		return r.lockCommerceMediaAgentWorkflow(ctx, tx, input)
	default:
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "带货视频工作流阶段无效"}
	}
}

func (r *CommerceGenerationRuntime) lockCommerceMediaAgentWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceAgentCallInput,
) error {
	record, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return err
	}
	if input.GenerationIdentity == nil {
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "媒体 Agent 缺少脚本单元生产身份"}
	}
	var identity commerce.UnitGenerationIdentity
	var shotIDs []string
	switch input.Phase {
	case CommercePhaseImagePrompt, CommercePhaseImageFidelity:
		var workflowInput CommerceReferenceImageBatchInput
		if err := json.Unmarshal(record.Input, &workflowInput); err != nil {
			return generationMismatch("参考图 Workflow 输入无法解析", err)
		}
		if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, input.Phase, workflowInput); err != nil {
			return err
		}
		identity, shotIDs = workflowInput.Identity, workflowInput.ShotIDs
	case CommercePhaseVideoPrompt:
		var workflowInput CommerceVideoBatchInput
		if err := json.Unmarshal(record.Input, &workflowInput); err != nil {
			return generationMismatch("视频提示词 Workflow 输入无法解析", err)
		}
		if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, input.Phase, workflowInput); err != nil {
			return err
		}
		identity, shotIDs = workflowInput.Identity, workflowInput.ShotIDs
	default:
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "媒体 Agent 阶段无效"}
	}
	if err := assertCommerceSnapshotEqual(identity, *input.GenerationIdentity, "媒体 Agent 生产身份"); err != nil {
		return err
	}
	found := false
	for _, shotID := range shotIDs {
		if shotID == input.SubjectKey {
			found = true
			break
		}
	}
	if !found {
		return generationMismatch("媒体 Agent 镜头不属于当前批次", nil)
	}
	return nil
}

func (r *CommerceGenerationRuntime) lockCommercePreparationAgentWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceAgentCallInput,
) error {
	if input.PreparationIdentity == nil {
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "脚本准备 Agent 缺少生产身份"}
	}
	record, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return err
	}
	var workflowInput CommerceScriptUnitPreparationInput
	if err := json.Unmarshal(record.Input, &workflowInput); err != nil {
		return generationMismatch("脚本准备 Workflow 输入无法解析", err)
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhasePreparation, workflowInput); err != nil {
		return err
	}
	if err := assertCommerceSnapshotEqual(
		workflowInput.Identity,
		*input.PreparationIdentity,
		"脚本准备 Agent 生产身份",
	); err != nil {
		return err
	}
	if workflowInput.AttemptGeneration != input.AttemptGeneration {
		return generationMismatch("脚本准备 Agent attempt 与 Workflow 不一致", nil)
	}
	return nil
}

func (r *CommerceGenerationRuntime) LoadScriptUnitPreparation(
	ctx context.Context,
	input CommerceScriptUnitPreparationInput,
) (CommerceScriptUnitPreparationSnapshot, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockPreparationState(ctx, tx, input.Identity)
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhasePreparation, input); err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, err
	}
	snapshot := state.Snapshot
	if err := ValidateCommercePreparationSnapshot(input.Identity, snapshot); err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, generationMismatch("脚本准备快照无效", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, err
	}
	return snapshot, nil
}

func (r *CommerceGenerationRuntime) PersistLanguageResolution(
	ctx context.Context,
	input PersistCommerceLanguageResolutionInput,
) (CommerceLanguageResolutionState, error) {
	input.Contract.NeedsUserConfirmation = false
	if err := ValidateCommercePreparationSnapshot(input.WorkflowInput.Identity, input.Snapshot); err != nil {
		return CommerceLanguageResolutionState{}, generationMismatch("语言解析使用了无效快照", err)
	}
	if err := ValidateCommerceLanguageResolution(input.Contract, input.Snapshot); err != nil {
		return CommerceLanguageResolutionState{}, commerce.Error{Code: CommerceCodeLanguageContractInvalid, Message: "语言解析结果不符合冻结契约", Cause: err}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockPreparationState(ctx, tx, input.WorkflowInput.Identity)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhasePreparation, input.WorkflowInput); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	fresh := state.Snapshot
	if err := assertCommerceSnapshotEqual(input.Snapshot, fresh, "语言解析输入"); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if err := r.assertAgentProvenance(ctx, tx, CommerceAgentCallInput{
		PreparationIdentity: &input.WorkflowInput.Identity,
		WorkflowRunID:       input.WorkflowInput.WorkflowRunID,
		AttemptGeneration:   input.WorkflowInput.AttemptGeneration,
		Phase:               CommercePhasePreparation,
		Round:               input.Provenance.Round,
		Binding:             input.Snapshot.Bindings.LanguageResolver,
	}, input.Provenance); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	resolution, created, err := r.persistFrozenLanguageResolution(ctx, tx, state, input)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if !languageResolutionMatchesContract(resolution, input.Contract) {
		return CommerceLanguageResolutionState{}, generationMismatch("同一脚本输入已存在不同的语言解析结果", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_language_resolutions
		SET prompt_version_id = COALESCE(prompt_version_id, NULLIF($2, '')::uuid),
		    provider_call_id = COALESCE(provider_call_id, NULLIF($3, '')::uuid),
		    updated_at = updated_at
		WHERE id = $1
		  AND (prompt_version_id IS NULL OR prompt_version_id = NULLIF($2, '')::uuid)
		  AND (provider_call_id IS NULL OR provider_call_id = NULLIF($3, '')::uuid)
	`, resolution.ID, input.Provenance.PromptVersionID, input.Provenance.ProviderCallID); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	row, err := r.loadLanguageResolutionForUpdate(ctx, tx, input.WorkflowInput.Identity, resolution.ID)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	contract := input.Contract
	contract.SourceLanguage = row.SourceLanguage
	contract.TargetLanguage = row.TargetLanguage
	contract.Confidence = row.Confidence
	contract.Reasoning = row.Reasoning
	contract.NeedsUserConfirmation = row.NeedsConfirmation
	result := CommerceLanguageResolutionState{
		ResolutionID: row.ID,
		Revision:     row.Revision,
		InputHash:    fresh.InputHash,
		Status:       row.Status,
		Contract:     contract,
	}
	if input.WorkflowInput.Identity.RebuildID != "" && result.Status == "needs_confirmation" {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_rebuilds
			SET status = 'waiting_user_confirmation'
			WHERE id = $1 AND workflow_run_id = $2 AND status = 'running'
		`, input.WorkflowInput.Identity.RebuildID, input.WorkflowInput.WorkflowRunID); err != nil {
			return CommerceLanguageResolutionState{}, err
		}
	}
	if created {
		eventName := "commerce.language.resolved"
		if result.Status == "needs_confirmation" {
			eventName = "commerce.language.confirmation_required"
		}
		if err := appendCommerceWorkflowEvent(ctx, tx,
			input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
			eventName, "commerce_language_resolution", result.ResolutionID, map[string]any{
				"workflowRunId":         input.WorkflowInput.WorkflowRunID,
				"commerceScriptUnitId":  input.WorkflowInput.Identity.ScriptUnitID,
				"languageResolutionId":  result.ResolutionID,
				"sourceScriptVersionId": state.SourceVersion.ID,
				"sourceLanguage":        result.Contract.SourceLanguage,
				"targetLanguage":        result.Contract.TargetLanguage,
				"confidence":            result.Contract.Confidence,
				"status":                result.Status,
			}); err != nil {
			return CommerceLanguageResolutionState{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	return result, nil
}

func (r *CommerceGenerationRuntime) persistFrozenLanguageResolution(
	ctx context.Context,
	tx pgx.Tx,
	state commercePreparationFrozenState,
	input PersistCommerceLanguageResolutionInput,
) (commerce.LanguageResolution, bool, error) {
	existing, err := r.repository.LoadLatestLanguageResolution(
		ctx, tx, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ID,
	)
	if err == nil && existing.InputHash == state.Snapshot.InputHash &&
		existing.SourceScriptVersionID == state.SourceVersion.ID {
		if existing.Status != "confirmed" && existing.TargetLanguage != nil {
			confirmed, confirmErr := r.repository.ConfirmLanguageResolution(
				ctx, tx, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ID,
				existing.ID, *existing.TargetLanguage, input.WorkflowInput.CreatedBy,
			)
			return confirmed, false, confirmErr
		}
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return commerce.LanguageResolution{}, false, err
	}
	sourceLanguage := input.Contract.SourceLanguage
	targetLanguage := input.Contract.TargetLanguage
	confidence := input.Contract.Confidence
	confirmedBy := &input.WorkflowInput.CreatedBy
	item, err := r.repository.InsertLanguageResolution(
		ctx, tx, state.Unit, state.SourceVersion.ID, &sourceLanguage, &targetLanguage,
		&confidence, strings.TrimSpace(input.Contract.Reasoning),
		false, "confirmed", confirmedBy, state.Snapshot.InputHash,
	)
	return item, err == nil, err
}

func (r *CommerceGenerationRuntime) ConfirmLanguage(
	ctx context.Context,
	input ConfirmCommerceLanguageInput,
) (CommerceLanguageResolutionState, error) {
	if input.Signal.Identity != input.WorkflowInput.Identity ||
		input.Signal.ResolutionID != input.Current.ResolutionID ||
		input.Signal.ExpectedRevision != input.Current.Revision ||
		input.Signal.InputHash != input.Snapshot.InputHash ||
		input.Current.InputHash != input.Snapshot.InputHash {
		return CommerceLanguageResolutionState{}, commerce.Error{Code: commerce.CodeRevisionConflict, Message: "语言确认信号已过期"}
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockPreparationState(ctx, tx, input.WorkflowInput.Identity)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhasePreparation, input.WorkflowInput); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	fresh := state.Snapshot
	if err := assertCommerceSnapshotEqual(input.Snapshot, fresh, "语言确认输入"); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	row, err := r.loadLanguageResolutionForUpdate(ctx, tx, input.WorkflowInput.Identity, input.Current.ResolutionID)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if row.Status == "confirmed" && row.Revision == input.Current.Revision+1 &&
		row.SourceScriptVersionID == fresh.SourceScriptVersionID && row.TargetLanguage == input.Signal.TargetLanguage {
		contract := input.Current.Contract
		contract.SourceLanguage = row.SourceLanguage
		contract.TargetLanguage = row.TargetLanguage
		contract.Confidence = row.Confidence
		contract.Reasoning = row.Reasoning
		contract.NeedsUserConfirmation = false
		result := CommerceLanguageResolutionState{
			ResolutionID: row.ID, Revision: row.Revision, InputHash: fresh.InputHash,
			Status: row.Status, Contract: contract,
		}
		if err := tx.Commit(ctx); err != nil {
			return CommerceLanguageResolutionState{}, err
		}
		return result, nil
	}
	if row.Revision != input.Current.Revision || row.SourceScriptVersionID != fresh.SourceScriptVersionID ||
		row.SourceLanguage != input.Current.Contract.SourceLanguage || row.TargetLanguage != input.Current.Contract.TargetLanguage ||
		row.Status != input.Current.Status {
		return CommerceLanguageResolutionState{}, commerce.Error{Code: commerce.CodeRevisionConflict, Message: "语言解析结果已被其他操作修改"}
	}
	confirmed, err := r.repository.ConfirmLanguageResolution(
		ctx, tx, input.WorkflowInput.Identity.OrganizationID,
		input.WorkflowInput.Identity.ProjectID, input.WorkflowInput.Identity.ScriptUnitID,
		input.Current.ResolutionID, input.Signal.TargetLanguage, input.WorkflowInput.CreatedBy,
	)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	row, err = r.loadLanguageResolutionForUpdate(ctx, tx, input.WorkflowInput.Identity, confirmed.ID)
	if err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	contract := input.Current.Contract
	contract.SourceLanguage = row.SourceLanguage
	contract.TargetLanguage = row.TargetLanguage
	contract.Confidence = row.Confidence
	contract.Reasoning = row.Reasoning
	contract.NeedsUserConfirmation = false
	result := CommerceLanguageResolutionState{
		ResolutionID: row.ID,
		Revision:     row.Revision,
		InputHash:    fresh.InputHash,
		Status:       row.Status,
		Contract:     contract,
	}
	if err := appendCommerceWorkflowEvent(ctx, tx,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.language.resolved", "commerce_language_resolution", result.ResolutionID, map[string]any{
			"workflowRunId":         input.WorkflowInput.WorkflowRunID,
			"commerceScriptUnitId":  input.WorkflowInput.Identity.ScriptUnitID,
			"languageResolutionId":  result.ResolutionID,
			"sourceScriptVersionId": fresh.SourceScriptVersionID,
			"sourceLanguage":        result.Contract.SourceLanguage,
			"targetLanguage":        result.Contract.TargetLanguage,
			"confidence":            result.Contract.Confidence,
			"status":                result.Status,
		}); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	if input.WorkflowInput.Identity.RebuildID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_rebuilds
			SET status = 'running'
			WHERE id = $1 AND workflow_run_id = $2 AND status = 'waiting_user_confirmation'
		`, input.WorkflowInput.Identity.RebuildID, input.WorkflowInput.WorkflowRunID)
		if err != nil {
			return CommerceLanguageResolutionState{}, err
		}
		if tag.RowsAffected() != 1 {
			return CommerceLanguageResolutionState{}, generationMismatch("脚本换代已不再等待语言确认", nil)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceLanguageResolutionState{}, err
	}
	return result, nil
}

func (r *CommerceGenerationRuntime) CommitScriptUnitPreparation(
	ctx context.Context,
	input CommerceScriptUnitPreparationCommit,
) (CommerceScriptUnitPreparationCommitResult, error) {
	if err := validateCommercePreparationCommit(input); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	defer tx.Rollback(ctx)
	if replay, found, err := loadPreparationCommitReplay(ctx, tx, input); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	} else if found {
		if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, input.WorkflowInput.WorkflowRunID, replay); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
		return replay, nil
	}
	state, err := r.lockPreparationState(ctx, tx, input.WorkflowInput.Identity)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhasePreparation, input.WorkflowInput); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	fresh := state.Snapshot
	if err := assertCommerceSnapshotEqual(input.Snapshot, fresh, "脚本准备提交"); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	resolution, err := r.loadLanguageResolutionForUpdate(ctx, tx, input.WorkflowInput.Identity, input.LanguageResolution.ResolutionID)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if resolution.Revision != input.LanguageResolution.Revision || resolution.Status != "confirmed" ||
		resolution.SourceScriptVersionID != state.SourceVersion.ID ||
		resolution.SourceLanguage != input.Localization.SourceLanguage || resolution.TargetLanguage != input.Localization.TargetLanguage {
		return CommerceScriptUnitPreparationCommitResult{}, commerce.Error{Code: commerce.CodeRevisionConflict, Message: "脚本语言结果已变化，不能提交本地化"}
	}
	if err := r.assertPreparationAgentProvenance(ctx, tx, input); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	localizationID, err := insertPreparedCommerceLocalization(ctx, tx, state, input)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	identity, err := insertAndActivatePreparedUnitGeneration(ctx, tx, state, input, localizationID)
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	productionWorkflowRunID := ""
	if input.WorkflowInput.Identity.RebuildID == "" {
		productionWorkflowRunID, err = enqueueCommerceStoryboardPlanningTx(
			ctx, tx, identity, input.WorkflowInput.CreatedBy, input.WorkflowInput.WorkflowRunID,
		)
		if err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
	}
	localizationPayload := map[string]any{
		"workflowRunId":          input.WorkflowInput.WorkflowRunID,
		"commerceScriptUnitId":   identity.ScriptUnitID,
		"scriptUnitGenerationId": identity.UnitGenerationID,
		"localizationId":         localizationID,
		"sourceScriptVersionId":  state.SourceVersion.ID,
		"sourceLanguage":         input.Localization.SourceLanguage,
		"targetLanguage":         input.Localization.TargetLanguage,
		"status":                 "approved",
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
		"commerce.script.localization.created", "commerce_script_localization", localizationID, localizationPayload); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
		"commerce.script.localization.approved", "commerce_script_localization", localizationID, localizationPayload); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if input.WorkflowInput.Identity.RebuildID != "" {
		if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
			"commerce.script_unit.generation.archived", "commerce_script_unit_generation",
			input.WorkflowInput.Identity.SourceUnitGenerationID, map[string]any{
				"workflowRunId":          input.WorkflowInput.WorkflowRunID,
				"commerceScriptUnitId":   identity.ScriptUnitID,
				"scriptUnitGenerationId": input.WorkflowInput.Identity.SourceUnitGenerationID,
				"rebuildId":              input.WorkflowInput.Identity.RebuildID,
				"status":                 "archived",
			}); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
		if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
			"commerce.script.version.activated", "commerce_script_version", state.SourceVersion.ID, map[string]any{
				"workflowRunId":          input.WorkflowInput.WorkflowRunID,
				"commerceScriptUnitId":   identity.ScriptUnitID,
				"scriptUnitGenerationId": identity.UnitGenerationID,
				"scriptVersionId":        state.SourceVersion.ID,
				"version":                state.SourceVersion.Version,
				"rebuildId":              input.WorkflowInput.Identity.RebuildID,
			}); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
		"commerce.script_unit.generation.created", "commerce_script_unit_generation", identity.UnitGenerationID, map[string]any{
			"workflowRunId":          input.WorkflowInput.WorkflowRunID,
			"commerceScriptUnitId":   identity.ScriptUnitID,
			"scriptUnitGenerationId": identity.UnitGenerationID,
			"unitGenerationNo":       identity.UnitGenerationNo,
			"status":                 "active",
		}); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
		"commerce.storyboard.strategy.selected", "commerce_script_unit_generation", identity.UnitGenerationID, map[string]any{
			"commerceScriptUnitId":   identity.ScriptUnitID,
			"scriptUnitGenerationId": identity.UnitGenerationID,
			"strategy":               state.StoryboardStrategy,
		}); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
		"commerce.script_unit.updated", "commerce_script_unit", identity.ScriptUnitID, map[string]any{
			"workflowRunId":          input.WorkflowInput.WorkflowRunID,
			"commerceScriptUnitId":   identity.ScriptUnitID,
			"scriptUnitGenerationId": identity.UnitGenerationID,
			"revision":               identity.ScriptUnitRevision,
			"status":                 "ready",
		}); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if productionWorkflowRunID != "" {
		if err := appendCommerceWorkflowEvent(ctx, tx, identity.OrganizationID, identity.ProjectID,
			"commerce.storyboard.plan.started", "workflow_run", productionWorkflowRunID, map[string]any{
				"workflowRunId":          productionWorkflowRunID,
				"parentWorkflowRunId":    input.WorkflowInput.WorkflowRunID,
				"commerceScriptUnitId":   identity.ScriptUnitID,
				"scriptUnitGenerationId": identity.UnitGenerationID,
				"status":                 "queued",
			}); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, err
		}
	}
	result := CommerceScriptUnitPreparationCommitResult{
		Identity: identity, LocalizationID: localizationID,
		ProductionWorkflowRunID: productionWorkflowRunID,
		Status:                  "ready", InputHash: fresh.InputHash,
	}
	if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, input.WorkflowInput.WorkflowRunID, result); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, err
	}
	return result, nil
}

func loadPreparationCommitReplay(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceScriptUnitPreparationCommit,
) (CommerceScriptUnitPreparationCommitResult, bool, error) {
	var generationID, localizationID, configurationHash string
	var generationNo, scriptUnitRevision int64
	var configuration json.RawMessage
	err := tx.QueryRow(ctx, `
		SELECT generation.id::text, generation.unit_generation_no,
		       generation.localization_id::text, generation.unit_configuration_hash,
		       generation.unit_configuration_snapshot, generation.script_unit_revision
		FROM commerce_script_unit_generations generation
		JOIN commerce_script_units unit
		  ON unit.id = generation.script_unit_id
		 AND unit.organization_id = generation.organization_id
		 AND unit.project_id = generation.project_id
		WHERE generation.organization_id = $1
		  AND generation.project_id = $2
		  AND generation.script_unit_id = $3
		  AND generation.status = 'active'
		  AND unit.active_unit_generation_id = generation.id
		  AND generation.unit_configuration_snapshot->>'preparationWorkflowRunId' = $4
		  AND generation.unit_configuration_snapshot->>'preparationInputHash' = $5
		FOR UPDATE OF unit, generation
	`, input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		input.WorkflowInput.Identity.ScriptUnitID, input.WorkflowInput.WorkflowRunID,
		input.Snapshot.InputHash).Scan(
		&generationID, &generationNo, &localizationID, &configurationHash,
		&configuration, &scriptUnitRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceScriptUnitPreparationCommitResult{}, false, nil
	}
	if err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, false, err
	}
	if err := assertRawJSONHash(configuration, configurationHash, "脚本单元配置"); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, false, err
	}
	identity := commerce.UnitGenerationIdentity{
		ExecutionIdentity:     input.WorkflowInput.Identity.ExecutionIdentity,
		ProductID:             input.WorkflowInput.Identity.ProductID,
		ScriptUnitID:          input.WorkflowInput.Identity.ScriptUnitID,
		ScriptUnitRevision:    scriptUnitRevision,
		UnitGenerationID:      generationID,
		UnitGenerationNo:      generationNo,
		UnitConfigurationHash: configurationHash,
	}
	if err := ValidateCommercePreparationCommitIdentity(input.WorkflowInput.Identity, identity); err != nil {
		return CommerceScriptUnitPreparationCommitResult{}, false, generationMismatch("脚本准备重放结果身份不一致", err)
	}
	productionWorkflowRunID := ""
	if input.WorkflowInput.Identity.RebuildID == "" {
		if err := tx.QueryRow(ctx, `
			SELECT id::text FROM workflow_runs
			WHERE production_generation_id = $1
			  AND workflow_type = $2
			  AND input->'identity'->>'scriptUnitGenerationId' = $3
			ORDER BY created_at DESC LIMIT 1
		`, identity.ProjectGenerationID, commerceStoryboardWorkflowType, identity.UnitGenerationID).Scan(&productionWorkflowRunID); err != nil {
			return CommerceScriptUnitPreparationCommitResult{}, false, err
		}
	}
	return CommerceScriptUnitPreparationCommitResult{
		Identity: identity, LocalizationID: localizationID,
		ProductionWorkflowRunID: productionWorkflowRunID,
		Status:                  "ready", InputHash: input.Snapshot.InputHash,
	}, true, nil
}

func (r *CommerceGenerationRuntime) assertPreparationAgentProvenance(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceScriptUnitPreparationCommit,
) error {
	bindings := map[string]CommerceAgentBinding{
		input.Snapshot.Bindings.LanguageResolver.Role:     input.Snapshot.Bindings.LanguageResolver,
		input.Snapshot.Bindings.ScriptLocalizer.Role:      input.Snapshot.Bindings.ScriptLocalizer,
		input.Snapshot.Bindings.LocalizationReviewer.Role: input.Snapshot.Bindings.LocalizationReviewer,
	}
	required := map[string]bool{
		input.Snapshot.Bindings.LanguageResolver.Role: true,
	}
	if input.Localization.SourceLanguage != input.Localization.TargetLanguage {
		required[input.Snapshot.Bindings.ScriptLocalizer.Role] = true
	}
	seen := make(map[string]bool, len(bindings))
	for _, call := range input.AgentCalls {
		binding, ok := bindings[call.Role]
		if !ok {
			return generationMismatch("脚本准备提交包含未冻结的 Agent provenance", nil)
		}
		if err := r.assertAgentProvenance(ctx, tx, CommerceAgentCallInput{
			PreparationIdentity: &input.WorkflowInput.Identity,
			WorkflowRunID:       input.WorkflowInput.WorkflowRunID,
			AttemptGeneration:   input.WorkflowInput.AttemptGeneration,
			Phase:               CommercePhasePreparation,
			Round:               call.Round,
			Binding:             binding,
		}, call); err != nil {
			return err
		}
		seen[call.Role] = true
	}
	for role := range required {
		if !seen[role] {
			return generationMismatch("脚本准备提交缺少必需的 Agent provenance："+role, nil)
		}
	}
	return nil
}

func insertPreparedCommerceLocalization(
	ctx context.Context,
	tx pgx.Tx,
	state commercePreparationFrozenState,
	input CommerceScriptUnitPreparationCommit,
) (string, error) {
	localizedParts := make([]string, 0, len(input.Localization.Segments))
	for _, segment := range input.Localization.Segments {
		localizedParts = append(localizedParts, strings.TrimSpace(segment.LocalizedText))
	}
	localizedContent := strings.Join(localizedParts, "\n\n")
	localizationRaw, err := json.Marshal(input.Localization)
	if err != nil {
		return "", err
	}
	reviewRaw, err := json.Marshal(input.LocalizationReview)
	if err != nil {
		return "", err
	}
	timingRaw, err := json.Marshal(input.Timing)
	if err != nil {
		return "", err
	}
	var promptVersionID, providerCallID string
	for _, call := range input.AgentCalls {
		if call.Role == input.Snapshot.Bindings.ScriptLocalizer.Role {
			promptVersionID = call.PromptVersionID
			providerCallID = call.ProviderCallID
			break
		}
	}
	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(version), 0) + 1
		FROM commerce_ad_script_localizations
		WHERE script_unit_id = $1
	`, state.Unit.ID).Scan(&nextVersion); err != nil {
		return "", err
	}
	localizationID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_ad_script_localizations(
			id, organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_resolution_id, version,
			source_language, target_language, localized_content,
			localized_content_hash, structured_contract,
			estimated_voiceover_seconds, timing_analysis, timing_policy_version,
			review_status, reviewer_output, prompt_version_id, provider_call_id,
			status, revision, created_by, approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		        $14, $15, $16, 'approved', $17, NULLIF($18, '')::uuid,
		        NULLIF($19, '')::uuid, 'approved', 1, $20, now())
	`, localizationID, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ProductID,
		state.Unit.ID, state.SourceVersion.ID, input.LanguageResolution.ResolutionID, nextVersion,
		input.Localization.SourceLanguage, input.Localization.TargetLanguage, localizedContent,
		commerceStringHash(localizedContent), localizationRaw, input.Timing.EstimatedVoiceoverSeconds,
		timingRaw, input.Timing.PolicyVersion, reviewRaw, promptVersionID, providerCallID,
		input.WorkflowInput.CreatedBy); err != nil {
		return "", err
	}
	for _, segment := range input.Localization.Segments {
		claims, err := json.Marshal(segment.ProductClaims)
		if err != nil {
			return "", err
		}
		features, err := json.Marshal(segment.RequiredProductFeatures)
		if err != nil {
			return "", err
		}
		segmentHash, err := commerceContractHash(segment)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_localization_segments(
				organization_id, project_id, product_id, script_unit_id,
				source_script_version_id, localization_id, source_segment_id,
				segment_no, sales_beat, localized_text, voiceover_text,
				onscreen_text, product_claims, required_product_features, content_hash
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ProductID,
			state.Unit.ID, state.SourceVersion.ID, localizationID, segment.SourceSegmentID,
			segment.Ordinal, segment.SalesBeat, segment.LocalizedText, segment.VoiceoverText,
			segment.OnscreenText, claims, features, segmentHash); err != nil {
			return "", err
		}
	}
	return localizationID, nil
}

func insertAndActivatePreparedUnitGeneration(
	ctx context.Context,
	tx pgx.Tx,
	state commercePreparationFrozenState,
	input CommerceScriptUnitPreparationCommit,
	localizationID string,
) (commerce.UnitGenerationIdentity, error) {
	generationID := uuid.NewString()
	generationNo := state.Unit.UnitGenerationNo + 1
	configuration := map[string]any{
		"schemaVersion":                   3,
		"projectGenerationId":             state.Production.Generation.ID,
		"commerceWorkflowBindingId":       state.Production.CommerceBinding.ID,
		"commerceWorkflowBindingRevision": state.Production.CommerceBinding.Revision,
		"videoProductionBindingId":        state.Production.VideoBinding.ID,
		"videoProductionBindingRevision":  state.Production.VideoBinding.Revision,
		"workflowTemplateVersionId":       state.Template.ID,
		"productVersionId":                state.ProductVersion.ID,
		"sourceScriptVersionId":           state.SourceVersion.ID,
		"localizationId":                  localizationID,
		"referencePackId":                 state.ReferencePack.ID,
		"targetDurationSeconds":           state.Unit.TargetDurationSeconds,
		"targetPlatform":                  state.Unit.TargetPlatform,
		"storyboardStrategy":              state.StoryboardStrategy,
		"segmentationPolicyVersion":       commerce.CommerceSegmentationPolicyV2,
		"preparationWorkflowRunId":        input.WorkflowInput.WorkflowRunID,
		"preparationInputHash":            input.Snapshot.InputHash,
		"preparationAgentCalls":           input.AgentCalls,
	}
	if input.WorkflowInput.Identity.RebuildID != "" {
		configuration["rebuildId"] = input.WorkflowInput.Identity.RebuildID
		configuration["sourceUnitGenerationId"] = input.WorkflowInput.Identity.SourceUnitGenerationID
		configuration["targetConfigurationHash"] = input.WorkflowInput.Identity.TargetConfigurationHash
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	configurationHash, err := commerceContractHash(configuration)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_unit_generations(
			id, organization_id, project_id, product_id, script_unit_id,
			script_unit_revision, project_production_generation_id,
			unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			source_unit_generation_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'preparing', $9, $10,
		        $11, $12, $13, $14, $15, $16, NULLIF($17, '')::uuid, $18)
	`, generationID, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ProductID,
		state.Unit.ID, state.Unit.Revision+1, state.Production.Generation.ID, generationNo,
		state.Production.CommerceBinding.ID, state.Production.CommerceBinding.Revision,
		state.ProductVersion.ID, state.SourceVersion.ID, localizationID,
		state.ReferencePack.ID, raw, configurationHash,
		input.WorkflowInput.Identity.SourceUnitGenerationID, input.WorkflowInput.CreatedBy); err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if input.WorkflowInput.Identity.RebuildID != "" {
		return activateRebuiltUnitGeneration(ctx, tx, state, input, localizationID, generationID, generationNo, configurationHash)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET current_localization_id = $2, active_unit_generation_id = $3,
		    unit_generation_no = $4, status = 'ready', revision = revision + 1,
		    updated_at = now()
		WHERE id = $1 AND revision = $5
		  AND current_source_version_id = $6
		  AND active_unit_generation_id IS NULL
	`, state.Unit.ID, localizationID, generationID, generationNo,
		state.Unit.Revision, state.SourceVersion.ID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("广告脚本在准备期间已变化", nil)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND status = 'preparing'
	`, generationID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本单元生产代无法激活", nil)
	}
	identity := commerce.UnitGenerationIdentity{
		ExecutionIdentity:     state.Production.ExecutionIdentity(),
		ProductID:             state.Product.ID,
		ScriptUnitID:          state.Unit.ID,
		ScriptUnitRevision:    state.Unit.Revision + 1,
		UnitGenerationID:      generationID,
		UnitGenerationNo:      generationNo,
		UnitConfigurationHash: configurationHash,
	}
	if err := ValidateCommercePreparationCommitIdentity(input.WorkflowInput.Identity, identity); err != nil {
		return commerce.UnitGenerationIdentity{}, generationMismatch("新脚本单元生产代身份无效", err)
	}
	return identity, nil
}

func activateRebuiltUnitGeneration(
	ctx context.Context,
	tx pgx.Tx,
	state commercePreparationFrozenState,
	input CommerceScriptUnitPreparationCommit,
	localizationID string,
	generationID string,
	generationNo int64,
	configurationHash string,
) (commerce.UnitGenerationIdentity, error) {
	rebuild, err := statefulScriptUnitRebuild(ctx, tx, input)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if rebuild.TargetSourceScriptVersionID != state.SourceVersion.ID ||
		rebuild.TargetConfigurationHash != input.WorkflowInput.Identity.TargetConfigurationHash ||
		rebuild.SourceUnitGenerationID != input.WorkflowInput.Identity.SourceUnitGenerationID {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本换代提交身份已变化", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_storyboard_plans
		SET status = 'stale', active = false, stale_state = 'upstream_changed',
		    stale_at = COALESCE(stale_at, now())
		WHERE organization_id = $1 AND project_id = $2
		  AND script_unit_id = $3 AND script_unit_generation_id = $4
		  AND status <> 'archived'
	`, state.Unit.OrganizationID, state.Unit.ProjectID, state.Unit.ID, rebuild.SourceUnitGenerationID); err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations
		SET status = 'archived', archived_at = now()
		WHERE id = $1 AND script_unit_id = $2 AND status = 'active'
	`, rebuild.SourceUnitGenerationID, state.Unit.ID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本换代来源生产代已变化", nil)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND status = 'preparing'
	`, generationID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本换代目标生产代无法激活", nil)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET current_source_version_id = $2,
		    current_localization_id = $3,
		    active_unit_generation_id = $4,
		    unit_generation_no = $5,
		    language_mode = $6,
		    explicit_target_language = $7,
		    target_duration_seconds = $8,
		    target_platform = $9,
		    draft_content = $10,
		    draft_content_hash = $11,
		    draft_updated_at = now(),
		    status = 'ready', revision = revision + 1, updated_at = now()
		WHERE id = $1 AND revision = $12
		  AND active_unit_generation_id = $13
	`, state.Unit.ID, state.SourceVersion.ID, localizationID, generationID, generationNo,
		state.Unit.LanguageMode, state.Unit.ExplicitTargetLanguage, state.Unit.TargetDurationSeconds,
		state.Unit.TargetPlatform, state.SourceVersion.Content, state.SourceVersion.ContentHash,
		state.Unit.Revision, rebuild.SourceUnitGenerationID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("广告脚本在换代提交期间已变化", nil)
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_script_unit_rebuilds
		SET status = 'succeeded', target_localization_id = $2,
		    target_unit_generation_id = $3, completed_at = now(),
		    error_code = NULL, error_message = NULL
		WHERE id = $1 AND workflow_run_id = $4
		  AND status IN ('running', 'waiting_user_confirmation')
	`, rebuild.ID, localizationID, generationID, input.WorkflowInput.WorkflowRunID)
	if err != nil {
		return commerce.UnitGenerationIdentity{}, err
	}
	if tag.RowsAffected() != 1 {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本换代记录已不再可提交", nil)
	}
	identity := commerce.UnitGenerationIdentity{
		ExecutionIdentity:     state.Production.ExecutionIdentity(),
		ProductID:             state.Product.ID,
		ScriptUnitID:          state.Unit.ID,
		ScriptUnitRevision:    state.Unit.Revision + 1,
		UnitGenerationID:      generationID,
		UnitGenerationNo:      generationNo,
		UnitConfigurationHash: configurationHash,
	}
	if err := ValidateCommercePreparationCommitIdentity(input.WorkflowInput.Identity, identity); err != nil {
		return commerce.UnitGenerationIdentity{}, generationMismatch("脚本换代目标生产身份无效", err)
	}
	return identity, nil
}

func statefulScriptUnitRebuild(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceScriptUnitPreparationCommit,
) (commerceScriptUnitRebuildState, error) {
	var item commerceScriptUnitRebuildState
	err := tx.QueryRow(ctx, `
		SELECT id::text, source_unit_generation_id::text,
		       target_source_script_version_id::text, target_configuration_hash
		FROM commerce_script_unit_rebuilds
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND script_unit_id = $4 AND workflow_run_id = $5
		  AND status IN ('running', 'waiting_user_confirmation')
		FOR UPDATE
	`, input.WorkflowInput.Identity.RebuildID,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		input.WorkflowInput.Identity.ScriptUnitID, input.WorkflowInput.WorkflowRunID).Scan(
		&item.ID, &item.SourceUnitGenerationID, &item.TargetSourceScriptVersionID,
		&item.TargetConfigurationHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commerceScriptUnitRebuildState{}, generationMismatch("脚本换代记录不存在或已终结", err)
	}
	return item, err
}

type commerceScriptUnitRebuildState struct {
	ID                          string
	SourceUnitGenerationID      string
	TargetSourceScriptVersionID string
	TargetConfigurationHash     string
}

func commerceStringHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (r *CommerceGenerationRuntime) LoadStoryboardPlanning(
	ctx context.Context,
	input CommerceStoryboardPlanningInput,
) (CommerceStoryboardPlanningSnapshot, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockGenerationState(ctx, tx, input.Identity)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, CommercePhaseStoryboard, input); err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	snapshot, err := r.buildStoryboardSnapshot(ctx, tx, state)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	if err := ValidateCommerceStoryboardSnapshot(input.Identity, snapshot); err != nil {
		return CommerceStoryboardPlanningSnapshot{}, generationMismatch("分镜规划快照无效", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	return snapshot, nil
}

// BuildStoryboardPlanningPreviewTx builds the exact deterministic plan that the
// Storyboard Workflow will commit. The caller owns the transaction so identity
// checks, preview generation, and workflow enqueueing can share one lock.
func (r *CommerceGenerationRuntime) BuildStoryboardPlanningPreviewTx(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
) (CommerceStoryboardPlanningSnapshot, CommerceSalesScriptContractState, CommerceStoryboardDeterministicPlan, error) {
	state, err := r.lockGenerationState(ctx, tx, identity)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{}, err
	}
	snapshot, err := r.buildStoryboardSnapshot(ctx, tx, state)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{}, err
	}
	contractState, found, err := loadCommerceSalesScriptContractState(ctx, tx, identity.UnitGenerationID)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{}, err
	}
	if !found || contractState.InputHash != snapshot.InputHash {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{},
			commerce.Error{Code: commerce.CodeScriptOrganizationNeed, Message: "已冻结销售脚本契约不存在或已失效"}
	}
	if err := validatePersistedCommerceSalesScript(contractState, snapshot); err != nil {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{}, err
	}
	plan, err := BuildCommerceStoryboardDeterministicPlan(snapshot, contractState.Contract)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, CommerceSalesScriptContractState{}, CommerceStoryboardDeterministicPlan{}, err
	}
	return snapshot, contractState, plan, nil
}

func (r *CommerceGenerationRuntime) CommitStoryboardPlan(
	ctx context.Context,
	input CommerceStoryboardPlanCommit,
) (CommerceStoryboardPlanCommitResult, error) {
	if err := validateCommerceStoryboardCommit(input); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	defer tx.Rollback(ctx)
	if replay, found, err := loadStoryboardCommitReplay(ctx, tx, input); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	} else if found {
		if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, input.WorkflowInput.WorkflowRunID, replay); err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		return replay, nil
	}
	state, err := r.lockGenerationState(ctx, tx, input.WorkflowInput.Identity)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, input.WorkflowInput.WorkflowRunID, CommercePhaseStoryboard, input.WorkflowInput); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	fresh, err := r.buildStoryboardSnapshot(ctx, tx, state)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := assertCommerceSnapshotEqual(input.Snapshot, fresh, "分镜方案提交"); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	rebuilt, err := BuildCommerceStoryboardProjection(fresh, input.Plan)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "分镜方案不符合冻结输入", Cause: err}
	}
	if err := assertCommerceSnapshotEqual(input.Projection, rebuilt, "分镜投影"); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := r.assertSalesScriptContract(ctx, tx, input); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := r.assertStoryboardAgentProvenance(ctx, tx, input); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	result, err := r.persistCommerceStoryboardPlan(ctx, tx, state, input, rebuilt)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	baseEventPayload := map[string]any{
		"workflowRunId":            input.WorkflowInput.WorkflowRunID,
		"commerceScriptUnitId":     result.Identity.ScriptUnitID,
		"scriptUnitGenerationId":   result.Identity.UnitGenerationID,
		"commerceStoryboardPlanId": result.StoryboardPlanID,
		"status":                   result.Status,
	}
	segmentationPayload := cloneCommerceEventPayload(baseEventPayload)
	segmentationPayload["segmentationPlanHash"] = input.DeterministicPlan.SegmentationPlanHash
	if err := appendCommerceWorkflowEvent(ctx, tx,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.storyboard.segmentation.completed", "commerce_storyboard_plan", result.StoryboardPlanID, segmentationPayload); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.storyboard.creative.generated", "commerce_storyboard_plan", result.StoryboardPlanID, baseEventPayload); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	committedPayload := cloneCommerceEventPayload(baseEventPayload)
	committedPayload["previewHash"] = input.DeterministicPlan.PreviewHash
	if err := appendCommerceWorkflowEvent(ctx, tx,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.storyboard.plan.committed", "commerce_storyboard_plan", result.StoryboardPlanID, committedPayload); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx,
		input.WorkflowInput.Identity.OrganizationID, input.WorkflowInput.Identity.ProjectID,
		"commerce.storyboard.plan.completed", "commerce_storyboard_plan", result.StoryboardPlanID, map[string]any{
			"workflowRunId":            input.WorkflowInput.WorkflowRunID,
			"commerceScriptUnitId":     result.Identity.ScriptUnitID,
			"scriptUnitGenerationId":   result.Identity.UnitGenerationID,
			"commerceStoryboardPlanId": result.StoryboardPlanID,
			"planRevision":             result.PlanRevision,
			"shotCount":                result.ShotCount,
			"status":                   result.Status,
		}); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, input.WorkflowInput.WorkflowRunID, result); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	return result, nil
}

func cloneCommerceEventPayload(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source)+1)
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (r *CommerceGenerationRuntime) FailCommerceGenerationWorkflow(
	ctx context.Context,
	input CommerceGenerationWorkflowFailureInput,
) error {
	phase, workflowInput, runID, err := commerceFailureWorkflowInput(input)
	if err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status); err != nil {
		return err
	}
	if status == "succeeded" || status == "failed" || status == "cancelled" || status == "partial_succeeded" {
		return tx.Commit(ctx)
	}
	if status != "cancelling" {
		if _, err := r.lockWorkflowRun(ctx, tx, runID, phase, workflowInput); err != nil {
			return err
		}
	} else if err := assertCommerceWorkflowRunIdentity(ctx, tx, runID, phase, workflowInput); err != nil {
		return err
	}
	code := strings.TrimSpace(input.ErrorCode)
	if code == "" {
		code = codeActivityFailed
	}
	message := strings.TrimSpace(input.ErrorMessage)
	if message == "" {
		message = "带货视频工作流执行失败"
	}
	targetStatus := "failed"
	if input.Cancelled {
		targetStatus = "cancelled"
	}
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = $2, error_code = $3, error_message = $4,
		    completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
	`, runID, targetStatus, code, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return generationMismatch("带货视频 Workflow Run 已不再可失败终结", nil)
	}
	if input.PreparationInput != nil && input.PreparationInput.Identity.RebuildID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_rebuilds
			SET status = $2, completed_at = now(), error_code = $3, error_message = $4
			WHERE id = $1 AND workflow_run_id = $5
			  AND status IN ('running', 'waiting_user_confirmation')
		`, input.PreparationInput.Identity.RebuildID, targetStatus, code, message, runID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return generationMismatch("脚本换代已不再可失败终结", nil)
		}
	}
	if input.OrganizationInput != nil || input.StoryboardInput != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_sales_script_contracts
			SET status = $2, completed_at = now(), error_code = $3,
			    error_message = $4, updated_at = now()
			WHERE current_workflow_run_id = $1 AND status = 'running'
		`, runID, targetStatus, code, message); err != nil {
			return err
		}
	}
	if input.StoryboardInput != nil {
		eventName := "commerce.storyboard.plan.failed"
		if input.Cancelled {
			eventName = "commerce.storyboard.plan.cancelled"
		}
		if err := appendCommerceWorkflowEvent(ctx, tx,
			input.StoryboardInput.Identity.OrganizationID, input.StoryboardInput.Identity.ProjectID,
			eventName, "workflow_run", runID, map[string]any{
				"workflowRunId":          runID,
				"commerceScriptUnitId":   input.StoryboardInput.Identity.ScriptUnitID,
				"scriptUnitGenerationId": input.StoryboardInput.Identity.UnitGenerationID,
				"status":                 targetStatus,
				"errorCode":              code,
				"errorMessage":           message,
			}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func commerceFailureWorkflowInput(input CommerceGenerationWorkflowFailureInput) (CommerceWorkflowPhase, any, string, error) {
	if input.PreparationInput != nil && input.OrganizationInput == nil && input.StoryboardInput == nil {
		return CommercePhasePreparation, *input.PreparationInput, input.PreparationInput.WorkflowRunID, nil
	}
	if input.OrganizationInput != nil && input.PreparationInput == nil && input.StoryboardInput == nil {
		return CommercePhaseScriptOrganization, *input.OrganizationInput, input.OrganizationInput.WorkflowRunID, nil
	}
	if input.StoryboardInput != nil && input.PreparationInput == nil && input.OrganizationInput == nil {
		return CommercePhaseStoryboard, *input.StoryboardInput, input.StoryboardInput.WorkflowRunID, nil
	}
	return "", nil, "", generationMismatch("工作流失败终结身份无效", nil)
}

func finalizeCommerceWorkflowSuccessTx(ctx context.Context, tx pgx.Tx, runID string, result any) error {
	output, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var status string
	var existing json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT status, output FROM workflow_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status, &existing); err != nil {
		return err
	}
	if status == "succeeded" {
		if err := assertCommerceSnapshotEqual(json.RawMessage(existing), json.RawMessage(output), "Workflow 成功输出"); err != nil {
			return err
		}
		return nil
	}
	if status != "queued" && status != "running" && status != "waiting_review" {
		return generationMismatch("带货视频 Workflow Run 已不再可提交", nil)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, error_code = NULL, error_message = NULL,
		    completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_review')
	`, runID, output)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return generationMismatch("带货视频 Workflow Run 已不再可提交", nil)
	}
	return nil
}

func (r *CommerceGenerationRuntime) FindCommerceAgentReplay(
	ctx context.Context,
	input CommerceAgentCallInput,
) (CommerceAgentCallOutput, bool, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceAgentCallOutput{}, false, err
	}
	defer tx.Rollback(ctx)
	if (input.Phase == CommercePhaseScriptOrganization || input.Phase == CommercePhaseStoryboard) && input.GenerationIdentity != nil {
		if _, err := r.lockGenerationState(ctx, tx, *input.GenerationIdentity); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
		if err := r.lockCommerceSalesScriptAgentWorkflow(ctx, tx, input); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
	} else if (input.Phase == CommercePhaseImagePrompt ||
		input.Phase == CommercePhaseImageFidelity ||
		input.Phase == CommercePhaseVideoPrompt) && input.GenerationIdentity != nil {
		if _, err := r.lockGenerationState(ctx, tx, *input.GenerationIdentity); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
		if err := r.lockCommerceMediaAgentWorkflow(ctx, tx, input); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
	} else if input.Phase == CommercePhasePreparation && input.PreparationIdentity != nil {
		if _, err := r.lockPreparationState(ctx, tx, *input.PreparationIdentity); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
		if err := r.lockCommercePreparationAgentWorkflow(ctx, tx, input); err != nil {
			return CommerceAgentCallOutput{}, false, err
		}
	} else {
		return CommerceAgentCallOutput{}, false, commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "Agent 身份与阶段不匹配"}
	}
	var raw json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT output
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_key = $2
		  AND node_type = $3
		  AND attempt_generation = $4
		  AND status = 'succeeded'
		ORDER BY updated_at DESC
		LIMIT 1
	`, input.WorkflowRunID, commerceAgentNodeKey(input), "agent.commerce."+input.Binding.Role, input.AttemptGeneration).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceAgentCallOutput{}, false, nil
	}
	if err != nil {
		return CommerceAgentCallOutput{}, false, err
	}
	var output CommerceAgentCallOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return CommerceAgentCallOutput{}, false, generationMismatch("已完成 Agent 节点输出损坏", err)
	}
	if output.Provenance.Role != input.Binding.Role || output.Provenance.Round != input.Round ||
		output.Provenance.ProviderModelID != input.Binding.ProviderModelID ||
		output.Provenance.PromptVersionID != input.Binding.PromptVersionID {
		return CommerceAgentCallOutput{}, false, generationMismatch("已完成 Agent 节点 provenance 与冻结绑定不一致", nil)
	}
	return output, true, nil
}

func (r *CommerceGenerationRuntime) begin(ctx context.Context) (pgx.Tx, error) {
	if r == nil || r.db == nil || r.repository == nil || r.service == nil || r.catalog == nil {
		return nil, commerce.Error{Code: CommerceCodeActivityPortUnavailable, Message: "带货视频持久化运行时未配置"}
	}
	return r.db.Begin(ctx)
}

func (r *CommerceGenerationRuntime) lockPreparationState(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.ScriptUnitPreparationIdentity,
) (commercePreparationFrozenState, error) {
	if err := ValidateCommerceScriptUnitPreparationIdentity(identity); err != nil {
		return commercePreparationFrozenState{}, commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "脚本准备身份不完整", Cause: err}
	}
	production, err := r.service.AssertWritableExecution(ctx, tx, identity.ExecutionIdentity)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if err := assertRawJSONHash(production.VideoBinding.ProfileSnapshot, production.VideoBinding.ProfileSnapshotHash, "视频 Profile"); err != nil {
		return commercePreparationFrozenState{}, err
	}
	if err := assertRawJSONHash(production.CommerceBinding.ConfigurationSnapshot, production.CommerceBinding.ConfigurationHash, "Commerce Binding"); err != nil {
		return commercePreparationFrozenState{}, err
	}

	product, found, err := r.repository.LockProduct(ctx, tx, identity.OrganizationID, identity.ProjectID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if !found || product.ID != identity.ProductID || product.CurrentVersionID == nil || *product.CurrentVersionID != identity.ProductVersionID {
		return commercePreparationFrozenState{}, generationMismatch("脚本准备使用的商品版本已变化", nil)
	}
	productVersion, err := r.repository.LoadProductVersion(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ProductVersionID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if productVersion.FactsHash != identity.ProductFactsHash {
		return commercePreparationFrozenState{}, generationMismatch("脚本准备使用的商品事实 hash 已变化", nil)
	}
	unit, err := r.repository.LoadScriptUnit(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, true)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if unit.ProductID != identity.ProductID || unit.Status == "archived" || unit.Revision != identity.ScriptUnitRevision {
		return commercePreparationFrozenState{}, generationMismatch("广告脚本已变化", nil)
	}
	stateStoryboardStrategy := commerce.StoryboardStrategySmart
	if identity.RebuildID == "" {
		if unit.ActiveUnitGenerationID != nil || unit.CurrentSourceVersionID == nil || *unit.CurrentSourceVersionID != identity.SourceScriptVersionID {
			return commercePreparationFrozenState{}, generationMismatch("广告脚本已变化或已有活动生产代", nil)
		}
	} else {
		rebuild, rebuildErr := r.repository.LoadScriptUnitRebuildByID(ctx, tx, identity.RebuildID)
		if rebuildErr != nil {
			return commercePreparationFrozenState{}, rebuildErr
		}
		if rebuild.OrganizationID != identity.OrganizationID || rebuild.ProjectID != identity.ProjectID ||
			rebuild.ProductID != identity.ProductID || rebuild.ScriptUnitID != identity.ScriptUnitID ||
			rebuild.ProjectGenerationID != identity.ProjectGenerationID ||
			rebuild.SourceUnitGenerationID != identity.SourceUnitGenerationID ||
			rebuild.TargetSourceScriptVersionID != identity.SourceScriptVersionID ||
			rebuild.TargetConfigurationHash != identity.TargetConfigurationHash ||
			rebuild.ExpectedRevision != identity.ScriptUnitRevision ||
			(rebuild.Status != "running" && rebuild.Status != "waiting_user_confirmation") ||
			unit.ActiveUnitGenerationID == nil || *unit.ActiveUnitGenerationID != rebuild.SourceUnitGenerationID {
			return commercePreparationFrozenState{}, generationMismatch("脚本换代快照或活动生产代已变化", nil)
		}
		var sourceStatus, sourceConfigurationHash string
		if err := tx.QueryRow(ctx, `
			SELECT status, unit_configuration_hash
			FROM commerce_script_unit_generations
			WHERE id = $1 AND script_unit_id = $2 AND organization_id = $3 AND project_id = $4
			FOR UPDATE
		`, rebuild.SourceUnitGenerationID, unit.ID, identity.OrganizationID, identity.ProjectID).Scan(
			&sourceStatus, &sourceConfigurationHash,
		); err != nil {
			return commercePreparationFrozenState{}, err
		}
		if sourceStatus != "active" || sourceConfigurationHash != rebuild.SourceUnitConfigurationHash {
			return commercePreparationFrozenState{}, generationMismatch("脚本换代来源生产代已变化", nil)
		}
		var targetConfiguration struct {
			SchemaVersion            int                         `json:"schemaVersion"`
			TargetStoryboardStrategy commerce.StoryboardStrategy `json:"targetStoryboardStrategy"`
		}
		if err := json.Unmarshal(rebuild.TargetConfiguration, &targetConfiguration); err != nil {
			return commercePreparationFrozenState{}, generationMismatch("脚本换代目标配置无法解析", err)
		}
		if targetConfiguration.SchemaVersion != 2 {
			return commercePreparationFrozenState{}, generationMismatch("脚本换代目标配置版本无效", nil)
		}
		strategy, strategyErr := commerce.ParseStoryboardStrategy(string(targetConfiguration.TargetStoryboardStrategy))
		if strategyErr != nil || strategy == commerce.StoryboardStrategyManual {
			return commercePreparationFrozenState{}, commerce.Error{Code: commerce.CodeStoryboardStrategy, Message: "脚本换代切分策略无效", Cause: strategyErr}
		}
		stateStoryboardStrategy = strategy
		unit.LanguageMode = rebuild.TargetLanguageMode
		unit.ExplicitTargetLanguage = rebuild.TargetLanguage
		unit.TargetDurationSeconds = rebuild.TargetDurationSeconds
		unit.TargetPlatform = rebuild.TargetPlatform
	}
	source, err := r.repository.LoadScriptVersion(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, identity.SourceScriptVersionID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if source.ContentHash != identity.SourceScriptContentHash {
		return commercePreparationFrozenState{}, generationMismatch("脚本准备使用的正文 hash 已变化", nil)
	}
	segments, err := r.repository.ListScriptSegments(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, identity.SourceScriptVersionID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if len(segments) == 0 {
		return commercePreparationFrozenState{}, generationMismatch("冻结脚本没有规范化段落", nil)
	}
	pack, err := r.repository.LoadProductReferencePack(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ReferencePackID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	if pack.Status != "active" || pack.ProductID != identity.ProductID || pack.ProductVersionID != identity.ProductVersionID ||
		pack.ProductFactsHash != identity.ProductFactsHash || pack.PackHash != identity.ReferencePackHash || len(pack.Items) == 0 {
		return commercePreparationFrozenState{}, commerce.Error{Code: CommerceCodeProductReferencePackStale, Message: "商品引用包已失效或不完整"}
	}
	template, err := loadFrozenCommerceTemplate(ctx, tx, production.CommerceBinding.TemplateVersionID)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	var bindingConfig commerceGenerationBindingConfiguration
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &bindingConfig); err != nil {
		return commercePreparationFrozenState{}, generationMismatch("Commerce Binding 配置无法解析", err)
	}
	if err := validateFrozenBindingConfiguration(production, template, bindingConfig); err != nil {
		return commercePreparationFrozenState{}, err
	}
	preparation, promptBindings, modelContracts, err := buildCommerceSetupPreparation(template, productVersion, unit, source, segments)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	agentBindings, err := resolveFrozenCommerceAgentBindings(
		template,
		production.CommerceBinding.ModelRoutingSnapshot,
		production.CommerceBinding.CapabilitySnapshot,
		promptBindings,
		modelContracts,
	)
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	preparation.Identity = identity
	preparation.ProductVersionID = identity.ProductVersionID
	preparation.SourceScriptVersionID = identity.SourceScriptVersionID
	preparation.ReferencePackID = identity.ReferencePackID
	preparation.Bindings = CommercePreparationAgentBindings{
		LanguageResolver:     agentBindings["languageResolver"],
		ScriptLocalizer:      agentBindings["scriptLocalizer"],
		LocalizationReviewer: agentBindings["localizationReviewer"],
	}
	preparation.InputHash, err = commerceContractHash(map[string]any{
		"phase": "script_unit_preparation", "identity": identity,
		"templateVersionId": template.ID, "templateContentHash": template.ContentHash,
		"productVersionId": productVersion.ID, "productFactsHash": productVersion.FactsHash,
		"sourceScriptVersionId": source.ID, "sourceContentHash": source.ContentHash,
		"referencePackId": pack.ID, "referencePackHash": pack.PackHash,
	})
	if err != nil {
		return commercePreparationFrozenState{}, err
	}
	return commercePreparationFrozenState{
		Production: production, Product: product, ProductVersion: productVersion,
		Unit: unit, SourceVersion: source, SourceSegments: segments,
		ReferencePack: pack, Template: template, BindingConfig: bindingConfig,
		AgentBindings: agentBindings, Snapshot: preparation,
		StoryboardStrategy: stateStoryboardStrategy,
	}, nil
}

func (r *CommerceGenerationRuntime) lockGenerationState(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
) (commerceGenerationFrozenState, error) {
	if err := ValidateCommerceUnitGenerationIdentity(identity); err != nil {
		return commerceGenerationFrozenState{}, commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "脚本单元生产身份不完整", Cause: err}
	}
	production, err := r.service.AssertWritableExecution(ctx, tx, identity.ExecutionIdentity)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	generation, err := r.repository.LockUnitGenerationContext(ctx, tx, production, identity)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if generation.Identity != identity {
		return commerceGenerationFrozenState{}, generationMismatch("脚本单元生产身份已变化", nil)
	}
	if err := assertRawJSONHash(generation.ConfigurationSnapshot, generation.Identity.UnitConfigurationHash, "脚本单元配置"); err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if err := assertRawJSONHash(production.VideoBinding.ProfileSnapshot, production.VideoBinding.ProfileSnapshotHash, "视频 Profile"); err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if err := assertRawJSONHash(production.CommerceBinding.ConfigurationSnapshot, production.CommerceBinding.ConfigurationHash, "Commerce Binding"); err != nil {
		return commerceGenerationFrozenState{}, err
	}

	product, found, err := r.repository.LockProduct(ctx, tx, identity.OrganizationID, identity.ProjectID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if !found || product.ID != identity.ProductID || product.CurrentVersionID == nil || *product.CurrentVersionID != generation.ProductVersionID {
		return commerceGenerationFrozenState{}, generationMismatch("脚本单元冻结的商品版本已失效", nil)
	}
	productVersion, err := r.repository.LoadProductVersion(ctx, tx, identity.OrganizationID, identity.ProjectID, generation.ProductVersionID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	unit, err := r.repository.LoadScriptUnit(ctx, tx, identity.OrganizationID, identity.ProjectID, identity.ScriptUnitID, false)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if unit.Status == "archived" ||
		unit.ActiveUnitGenerationID == nil || *unit.ActiveUnitGenerationID != identity.UnitGenerationID ||
		unit.CurrentSourceVersionID == nil || *unit.CurrentSourceVersionID != generation.SourceScriptVersionID ||
		unit.CurrentLocalizationID == nil || *unit.CurrentLocalizationID != generation.LocalizationID {
		return commerceGenerationFrozenState{}, generationMismatch("广告脚本或活动生产代已变化", nil)
	}
	source, err := r.repository.LoadScriptVersion(ctx, tx, identity.OrganizationID, identity.ProjectID, unit.ID, generation.SourceScriptVersionID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	segments, err := r.repository.ListScriptSegments(ctx, tx, identity.OrganizationID, identity.ProjectID, unit.ID, source.ID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if len(segments) == 0 {
		return commerceGenerationFrozenState{}, generationMismatch("冻结脚本没有规范化段落", nil)
	}
	localization, err := r.repository.LoadLocalization(ctx, tx, identity.OrganizationID, identity.ProjectID, unit.ID, generation.LocalizationID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if localization.SourceScriptVersionID != source.ID || localization.Status != "approved" || localization.ReviewStatus != "approved" {
		return commerceGenerationFrozenState{}, generationMismatch("脚本单元冻结的本地化版本未通过审核", nil)
	}
	pack, err := r.repository.LoadProductReferencePack(ctx, tx, identity.OrganizationID, identity.ProjectID, generation.ReferencePackID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	if pack.Status != "active" || pack.ProductID != product.ID || pack.ProductVersionID != productVersion.ID || pack.ProductFactsHash != productVersion.FactsHash || len(pack.Items) == 0 {
		return commerceGenerationFrozenState{}, commerce.Error{Code: CommerceCodeProductReferencePackStale, Message: "商品引用包已失效或不完整"}
	}
	template, err := loadFrozenCommerceTemplate(ctx, tx, production.CommerceBinding.TemplateVersionID)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	var bindingConfig commerceGenerationBindingConfiguration
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &bindingConfig); err != nil {
		return commerceGenerationFrozenState{}, generationMismatch("Commerce Binding 配置无法解析", err)
	}
	if err := validateFrozenBindingConfiguration(production, template, bindingConfig); err != nil {
		return commerceGenerationFrozenState{}, err
	}
	var unitConfig commerceUnitConfigurationSnapshot
	if err := json.Unmarshal(generation.ConfigurationSnapshot, &unitConfig); err != nil {
		return commerceGenerationFrozenState{}, generationMismatch("脚本单元配置无法解析", err)
	}
	if err := validateFrozenUnitConfiguration(production, generation, unit, unitConfig); err != nil {
		return commerceGenerationFrozenState{}, err
	}
	_, promptBindings, modelContracts, err := buildCommerceSetupPreparation(template, productVersion, unit, source, segments)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	agentBindings, err := resolveFrozenCommerceAgentBindings(template, production.CommerceBinding.ModelRoutingSnapshot, production.CommerceBinding.CapabilitySnapshot, promptBindings, modelContracts)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	videoEnvelope, videoEnvelopeHash, err := buildCommerceVideoExecutionEnvelope(
		production,
		bindingConfig.ProductionConfiguration,
		production.CommerceBinding.CapabilitySnapshot,
	)
	if err != nil {
		return commerceGenerationFrozenState{}, err
	}
	strategy, err := commerce.ParseStoryboardStrategy(unitConfig.StoryboardStrategy)
	if err != nil {
		return commerceGenerationFrozenState{}, generationMismatch("脚本单元切分策略无效", err)
	}
	timingPolicy := commerceAdvisoryTimingPolicy(localization.TargetLanguage)
	return commerceGenerationFrozenState{
		Production: production, Generation: generation, Product: product, ProductVersion: productVersion,
		Unit: unit, SourceVersion: source, SourceSegments: segments, Localization: localization,
		ReferencePack: pack, Template: template, BindingConfig: bindingConfig,
		PromptBindings: promptBindings, ModelContracts: modelContracts, AgentBindings: agentBindings,
		StoryboardConfig: commerceStoryboardFrozenConfiguration{
			AspectRatio:                bindingConfig.ProductionConfiguration.AspectRatio,
			TimelineTimebase:           bindingConfig.ProductionConfiguration.TimelineTimebase,
			FPSNumerator:               bindingConfig.ProductionConfiguration.FPSNumerator,
			FPSDenominator:             bindingConfig.ProductionConfiguration.FPSDenominator,
			Strategy:                   strategy,
			SegmentationPolicyVersion:  unitConfig.SegmentationPolicyVersion,
			VideoExecutionEnvelope:     videoEnvelope,
			VideoExecutionEnvelopeHash: videoEnvelopeHash,
			AllowedDurations:           append([]int(nil), videoEnvelope.ExecutableDurationSeconds...),
			TimingPolicy:               timingPolicy,
		},
	}, nil
}

func (r *CommerceGenerationRuntime) buildStoryboardSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	state commerceGenerationFrozenState,
) (CommerceStoryboardPlanningSnapshot, error) {
	localizedSegments, err := loadFrozenLocalizationSegments(ctx, tx, state)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	references := make([]CommerceProductReferenceSnapshot, 0, len(state.ReferencePack.Items))
	for _, item := range state.ReferencePack.Items {
		references = append(references, CommerceProductReferenceSnapshot{
			PackItemID: item.ID, ReferenceID: item.ProductReferenceID,
			Role: normalizeCommerceReferenceRole(item.ReferenceRole), Ordinal: item.Ordinal,
			ContentHash: item.ContentHash, Required: item.ReferenceRole == "primary",
		})
	}
	sort.Slice(references, func(i, j int) bool { return references[i].Ordinal < references[j].Ordinal })
	localizedContractHash, err := commerceContractHash(state.Localization.StructuredContract)
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	inputHash, err := commerceContractHash(map[string]any{
		"phase": "storyboard_planning", "identity": state.Generation.Identity,
		"productVersionId": state.ProductVersion.ID, "productFactsHash": state.ProductVersion.FactsHash,
		"sourceScriptVersionId": state.SourceVersion.ID, "sourceContentHash": state.SourceVersion.ContentHash,
		"localizationId": state.Localization.ID, "localizedContentHash": state.Localization.LocalizedContentHash,
		"localizedContractHash": localizedContractHash,
		"referencePackId":       state.ReferencePack.ID, "referencePackHash": state.ReferencePack.PackHash,
		"aspectRatio":                state.StoryboardConfig.AspectRatio,
		"timelineTimebase":           state.StoryboardConfig.TimelineTimebase,
		"fpsNumerator":               state.StoryboardConfig.FPSNumerator,
		"fpsDenominator":             state.StoryboardConfig.FPSDenominator,
		"allowedDurations":           state.StoryboardConfig.AllowedDurations,
		"storyboardStrategy":         state.StoryboardConfig.Strategy,
		"segmentationPolicyVersion":  state.StoryboardConfig.SegmentationPolicyVersion,
		"videoExecutionEnvelopeHash": state.StoryboardConfig.VideoExecutionEnvelopeHash,
		"timingPolicy":               state.StoryboardConfig.TimingPolicy,
	})
	if err != nil {
		return CommerceStoryboardPlanningSnapshot{}, err
	}
	return CommerceStoryboardPlanningSnapshot{
		Identity: state.Generation.Identity, InputHash: inputHash,
		ProductVersionID: state.ProductVersion.ID, SourceScriptVersionID: state.SourceVersion.ID,
		LocalizationID: state.Localization.ID, ReferencePackID: state.ReferencePack.ID,
		TargetLocale: state.Localization.TargetLanguage, TargetDurationSeconds: state.Unit.TargetDurationSeconds,
		AspectRatio: state.StoryboardConfig.AspectRatio, TimelineTimebase: state.StoryboardConfig.TimelineTimebase,
		FPSNumerator: state.StoryboardConfig.FPSNumerator, FPSDenominator: state.StoryboardConfig.FPSDenominator,
		TimingPolicyVersion:  state.Localization.TimingPolicyVersion,
		LocalizedContentHash: state.Localization.LocalizedContentHash, LocalizedContractHash: localizedContractHash,
		AllowedShotDurations:       append([]int(nil), state.StoryboardConfig.AllowedDurations...),
		StoryboardStrategy:         state.StoryboardConfig.Strategy,
		SegmentationPolicyVersion:  state.StoryboardConfig.SegmentationPolicyVersion,
		VideoExecutionEnvelope:     state.StoryboardConfig.VideoExecutionEnvelope,
		VideoExecutionEnvelopeHash: state.StoryboardConfig.VideoExecutionEnvelopeHash,
		TimingPolicy:               state.StoryboardConfig.TimingPolicy,
		LocalizedSegments:          localizedSegments, ProductReferences: references,
		ProductFacts:         append(json.RawMessage(nil), state.ProductVersion.FactsSnapshot...),
		LocalizationContract: append(json.RawMessage(nil), state.Localization.StructuredContract...),
		Bindings: CommerceStoryboardAgentBindings{
			ScriptOrganizer:    state.AgentBindings["scriptOrganizer"],
			StoryboardPlanner:  state.AgentBindings["storyboardPlanner"],
			StoryboardReviewer: state.AgentBindings["storyboardReviewer"],
		},
	}, nil
}

func (r *CommerceGenerationRuntime) lockWorkflowRun(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID string,
	phase CommerceWorkflowPhase,
	workflowInput any,
) (commerceWorkflowRunRecord, error) {
	item, err := loadCommerceWorkflowRunRecord(ctx, tx, workflowRunID)
	if err != nil {
		return commerceWorkflowRunRecord{}, err
	}
	if err := validateCommerceWorkflowRunRecord(item, workflowRunID, phase, workflowInput); err != nil {
		return commerceWorkflowRunRecord{}, err
	}
	if item.Status != "queued" && item.Status != "running" && item.Status != "waiting_review" {
		return commerceWorkflowRunRecord{}, generationMismatch("Workflow Run 已不再可写", nil)
	}
	return item, nil
}

func loadCommerceWorkflowRunRecord(ctx context.Context, tx pgx.Tx, workflowRunID string) (commerceWorkflowRunRecord, error) {
	var item commerceWorkflowRunRecord
	err := tx.QueryRow(ctx, `
		SELECT run.organization_id::text, run.project_id::text,
		       run.production_generation_id::text,
		       run.video_production_binding_id::text,
		       run.video_production_binding_revision,
		       run.workflow_type, run.status, run.input,
		       run.attempt_generation, run.created_by::text,
		       outbox.input, outbox.input_hash
		FROM workflow_runs run
		JOIN workflow_start_outbox outbox ON outbox.workflow_run_id = run.id
		WHERE run.id = $1
		FOR UPDATE OF run
	`, workflowRunID).Scan(
		&item.OrganizationID, &item.ProjectID, &item.ProductionGenerationID,
		&item.VideoProductionBindingID, &item.VideoProductionBindingRevision,
		&item.WorkflowType, &item.Status, &item.Input, &item.AttemptGeneration,
		&item.CreatedBy, &item.OutboxInput, &item.OutboxInputHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commerceWorkflowRunRecord{}, generationMismatch("带货视频 Workflow Run 不存在或缺少可靠启动记录", err)
	}
	if err != nil {
		return commerceWorkflowRunRecord{}, err
	}
	return item, nil
}

func validateCommerceWorkflowRunRecord(
	item commerceWorkflowRunRecord,
	workflowRunID string,
	phase CommerceWorkflowPhase,
	workflowInput any,
) error {
	expectedType, identity, attemptGeneration, createdBy, err := commerceWorkflowInputIdentity(phase, workflowInput)
	if err != nil {
		return err
	}
	if item.WorkflowType != expectedType || item.OrganizationID != identity.OrganizationID || item.ProjectID != identity.ProjectID ||
		item.ProductionGenerationID != identity.ProjectGenerationID ||
		item.VideoProductionBindingID != identity.VideoProductionBindingID ||
		item.VideoProductionBindingRevision != identity.VideoProductionBindingRevision ||
		item.AttemptGeneration != attemptGeneration || (createdBy != "" && item.CreatedBy != createdBy) {
		return generationMismatch("Workflow Run 与脚本单元生产身份不一致", nil)
	}
	if err := assertWorkflowInputMatches(item.Input, workflowInput, workflowRunID); err != nil {
		return err
	}
	runHash, err := canonicalCommerceWorkflowInputHash(item.Input)
	if err != nil {
		return err
	}
	outboxHash, err := canonicalCommerceWorkflowInputHash(item.OutboxInput)
	if err != nil {
		return err
	}
	if runHash != item.OutboxInputHash || outboxHash != item.OutboxInputHash || runHash != outboxHash {
		return generationMismatch("Workflow Run 输入 hash 与启动快照不一致", nil)
	}
	return nil
}

func assertCommerceWorkflowRunIdentity(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID string,
	phase CommerceWorkflowPhase,
	workflowInput any,
) error {
	item, err := loadCommerceWorkflowRunRecord(ctx, tx, workflowRunID)
	if err != nil {
		return err
	}
	return validateCommerceWorkflowRunRecord(item, workflowRunID, phase, workflowInput)
}

func commerceWorkflowInputIdentity(
	phase CommerceWorkflowPhase,
	input any,
) (string, commerce.ExecutionIdentity, int, string, error) {
	switch phase {
	case CommercePhasePreparation:
		item, ok := input.(CommerceScriptUnitPreparationInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "脚本准备 Workflow 输入类型无效"}
		}
		return commercePreparationWorkflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	case CommercePhaseScriptOrganization:
		item, ok := input.(CommerceScriptOrganizationInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "销售脚本整理 Workflow 输入类型无效"}
		}
		return commerceScriptOrganizationWorkflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	case CommercePhaseStoryboard:
		item, ok := input.(CommerceStoryboardPlanningInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "分镜 Workflow 输入类型无效"}
		}
		return commerceStoryboardWorkflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	case CommercePhaseImagePrompt, CommercePhaseImageFidelity:
		item, ok := input.(CommerceReferenceImageBatchInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "参考图 Workflow 输入类型无效"}
		}
		expectedOperation := "generate_prompts"
		workflowType := commerceImagePromptWorkflowType
		if phase == CommercePhaseImageFidelity {
			expectedOperation = "generate_images"
			workflowType = commerceReferenceImageWorkflowType
		}
		if item.Operation != expectedOperation {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "参考图 Workflow 操作与阶段不一致"}
		}
		return workflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	case CommercePhaseVideoPrompt, CommercePhaseVideoRender:
		item, ok := input.(CommerceVideoBatchInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "视频生产 Workflow 输入类型无效"}
		}
		expectedOperation := "generate_prompts"
		workflowType := commerceVideoPromptWorkflowType
		if phase == CommercePhaseVideoRender {
			expectedOperation = "generate_videos"
			workflowType = commerceShotVideoWorkflowType
		}
		if item.Operation != expectedOperation {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "视频生产 Workflow 操作与阶段不一致"}
		}
		return workflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	case CommercePhaseFinalCompose:
		item, ok := input.(CommerceFinalComposeInput)
		if !ok {
			return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "成片合成 Workflow 输入类型无效"}
		}
		return commerceFinalComposeWorkflowType, item.Identity.ExecutionIdentity, item.AttemptGeneration, item.CreatedBy, nil
	default:
		return "", commerce.ExecutionIdentity{}, 0, "", commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "带货视频工作流阶段无效"}
	}
}

func assertWorkflowInputMatches(raw json.RawMessage, expected any, workflowRunID string) error {
	var actual any
	switch expected.(type) {
	case CommerceScriptUnitPreparationInput:
		var item CommerceScriptUnitPreparationInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("脚本准备 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceScriptOrganizationInput:
		var item CommerceScriptOrganizationInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("销售脚本整理 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceStoryboardPlanningInput:
		var item CommerceStoryboardPlanningInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("分镜 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceReferenceImageBatchInput:
		var item CommerceReferenceImageBatchInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("参考图 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceVideoBatchInput:
		var item CommerceVideoBatchInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("视频生产 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceFinalComposeInput:
		var item CommerceFinalComposeInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("成片合成 Workflow 输入无法解析", err)
		}
		actual = item
	case CommerceScriptUnitBatchCoordinatorInput:
		var item CommerceScriptUnitBatchCoordinatorInput
		if err := json.Unmarshal(raw, &item); err != nil {
			return generationMismatch("跨脚本批量 Workflow 输入无法解析", err)
		}
		actual = item
	default:
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "不支持的带货视频 Workflow 输入"}
	}
	actualHash, err := commerceContractHash(actual)
	if err != nil {
		return err
	}
	expectedHash, err := commerceContractHash(expected)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return generationMismatch("Workflow Run 输入与 Activity 输入不一致", nil)
	}
	if strings.TrimSpace(workflowRunID) == "" {
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "workflowRunId 不能为空"}
	}
	return nil
}

func canonicalCommerceWorkflowInputHash(raw json.RawMessage) (string, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func loadFrozenCommerceTemplate(ctx context.Context, tx pgx.Tx, versionID string) (commerce.WorkflowTemplateVersion, error) {
	var item commerce.WorkflowTemplateVersion
	err := tx.QueryRow(ctx, `
		SELECT version.id::text, template.id::text, template.template_key, version.version,
		       version.content_hash, version.configuration_snapshot, version.prompt_bindings,
		       version.agent_model_contracts, version.language_contract,
		       version.image_capability_contract, version.video_capability_contract,
		       profile.profile_key, profile_version.version
		FROM commerce_workflow_template_versions version
		JOIN commerce_workflow_templates template ON template.id = version.template_id
		JOIN video_production_profile_versions profile_version
		  ON profile_version.id = version.video_production_profile_version_id
		JOIN video_production_profiles profile ON profile.id = profile_version.profile_id
		WHERE version.id = $1
		  AND version.status IN ('published', 'retired')
		FOR SHARE OF version, template, profile_version, profile
	`, versionID).Scan(
		&item.ID, &item.TemplateID, &item.TemplateKey, &item.Version,
		&item.ContentHash, &item.ConfigurationSnapshot, &item.PromptBindings,
		&item.AgentModelContracts, &item.LanguageContract,
		&item.ImageCapabilityContract, &item.VideoCapabilityContract,
		&item.VideoProfileKey, &item.VideoProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commerce.WorkflowTemplateVersion{}, generationMismatch("冻结的 Commerce Workflow Template 不存在", err)
	}
	return item, err
}

func validateFrozenBindingConfiguration(
	production commerce.ProductionContext,
	template commerce.WorkflowTemplateVersion,
	config commerceGenerationBindingConfiguration,
) error {
	if config.SchemaVersion != 2 || config.WorkflowTemplateVersionID != template.ID ||
		config.WorkflowTemplateContentHash != template.ContentHash {
		return generationMismatch("Commerce Binding 冻结配置与模板版本不一致", nil)
	}
	if config.ProductionConfiguration.SchemaVersion != videoproduction.ProductionConfigurationSnapshotVersion ||
		strings.TrimSpace(config.ProductionConfiguration.AspectRatio) == "" ||
		config.ProductionConfiguration.TimelineTimebase <= 0 ||
		config.ProductionConfiguration.FPSNumerator <= 0 || config.ProductionConfiguration.FPSDenominator <= 0 {
		return generationMismatch("Commerce Binding 的生产配置不完整", nil)
	}
	if (config.ProductionConfiguration.TimelineTimebase*int64(config.ProductionConfiguration.FPSDenominator))%int64(config.ProductionConfiguration.FPSNumerator) != 0 {
		return generationMismatch("Commerce Binding 的时间基准无法对齐帧率", nil)
	}
	promptHash, err := commerceContractHash(config.PromptBindings)
	if err != nil {
		return err
	}
	templatePromptHash, err := commerceContractHash(template.PromptBindings)
	if err != nil {
		return err
	}
	languageHash, err := commerceContractHash(config.LanguageContract)
	if err != nil {
		return err
	}
	templateLanguageHash, err := commerceContractHash(template.LanguageContract)
	if err != nil {
		return err
	}
	if promptHash != templatePromptHash || languageHash != templateLanguageHash {
		return generationMismatch("Commerce Binding 内嵌模板快照与冻结模板版本不一致", nil)
	}
	return nil
}

func validateFrozenUnitConfiguration(
	production commerce.ProductionContext,
	generation commerce.UnitGenerationContext,
	unit commerce.ScriptUnit,
	config commerceUnitConfigurationSnapshot,
) error {
	strategy, strategyErr := commerce.ParseStoryboardStrategy(config.StoryboardStrategy)
	if config.SchemaVersion != 3 || strategyErr != nil || strategy == commerce.StoryboardStrategyManual ||
		config.SegmentationPolicyVersion != commerce.CommerceSegmentationPolicyV2 {
		return generationMismatch("脚本单元配置版本过旧或切分策略无效，请重建脚本单元生产代", strategyErr)
	}
	if config.ProjectGenerationID != production.Generation.ID ||
		config.CommerceWorkflowBindingID != production.CommerceBinding.ID ||
		config.CommerceWorkflowBindingRevision != production.CommerceBinding.Revision ||
		config.VideoProductionBindingID != production.VideoBinding.ID ||
		config.VideoProductionBindingRevision != production.VideoBinding.Revision ||
		config.WorkflowTemplateVersionID != production.CommerceBinding.TemplateVersionID ||
		config.ProductVersionID != generation.ProductVersionID ||
		config.SourceScriptVersionID != generation.SourceScriptVersionID ||
		config.LocalizationID != generation.LocalizationID || config.ReferencePackID != generation.ReferencePackID ||
		config.TargetDurationSeconds != unit.TargetDurationSeconds || config.TargetPlatform != unit.TargetPlatform {
		return generationMismatch("脚本单元配置快照与活动生产身份不一致", nil)
	}
	return nil
}

func resolveFrozenCommerceAgentBindings(
	template commerce.WorkflowTemplateVersion,
	routingRaw json.RawMessage,
	capabilityRaw json.RawMessage,
	prompts map[string]commerceSetupPromptBinding,
	models map[string]commerceSetupModelContract,
) (map[string]CommerceAgentBinding, error) {
	var routing map[string]struct {
		Candidates []struct {
			ProviderModelID string `json:"providerModelId"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return nil, generationMismatch("Commerce Binding 模型路由快照无法解析", err)
	}
	var capabilities map[string]struct {
		ProviderModelID string `json:"providerModelId"`
	}
	if err := json.Unmarshal(capabilityRaw, &capabilities); err != nil {
		return nil, generationMismatch("Commerce Binding 模型能力快照无法解析", err)
	}
	roles := []string{
		"languageResolver", "scriptLocalizer", "localizationReviewer",
		"scriptOrganizer", "storyboardPlanner", "storyboardReviewer",
		"imagePromptAgent", "imageFidelityReviewer",
		"videoPromptAgent", "videoPromptReviewer",
	}
	result := make(map[string]CommerceAgentBinding, len(roles))
	for _, role := range roles {
		prompt, promptOK := prompts[role]
		model, modelOK := models[role]
		route, routeOK := routing[role]
		capability, capabilityOK := capabilities[role]
		if !promptOK || !modelOK || !routeOK || len(route.Candidates) == 0 || strings.TrimSpace(route.Candidates[0].ProviderModelID) == "" {
			return nil, generationMismatch("Commerce Binding 缺少冻结 Agent 绑定："+role, nil)
		}
		providerModelID := strings.TrimSpace(route.Candidates[0].ProviderModelID)
		if capabilityOK && strings.TrimSpace(capability.ProviderModelID) != "" && capability.ProviderModelID != providerModelID {
			return nil, generationMismatch("Commerce Binding 路由与能力快照不一致："+role, nil)
		}
		binding := CommerceAgentBinding{
			Role: role, TemplateKey: prompt.TemplateKey, PromptVersionID: prompt.PromptVersionID,
			PromptContentHash: prompt.ContentHash, ModelProfileKey: model.ProfileKey,
			ProviderModelID: providerModelID, MaxReviewRounds: prompt.MaxReviewRounds,
		}
		if err := ValidateCommerceAgentBinding(binding); err != nil {
			return nil, generationMismatch("冻结 Agent 绑定无效："+role, err)
		}
		result[role] = binding
	}
	if template.ID == "" {
		return nil, generationMismatch("Commerce Workflow Template 身份为空", nil)
	}
	return result, nil
}

func commerceAllowedVideoDurations(raw json.RawMessage) ([]int, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, generationMismatch("视频能力快照无法解析", err)
	}
	video, ok := root["videoGenerator"].(map[string]any)
	if !ok {
		return nil, generationMismatch("视频能力快照缺少 videoGenerator", nil)
	}
	videoRaw, err := json.Marshal(video)
	if err != nil {
		return nil, generationMismatch("冻结视频能力无法序列化", err)
	}
	var snapshot struct {
		ProviderModelID string                `json:"providerModelId"`
		Capabilities    []provider.Capability `json:"capabilities"`
	}
	if err := json.Unmarshal(videoRaw, &snapshot); err != nil {
		return nil, generationMismatch("冻结视频能力无法解析", err)
	}
	if len(snapshot.Capabilities) == 0 {
		return nil, generationMismatch("视频能力快照没有冻结可执行能力", nil)
	}
	values, err := provider.ExecutableWholeSecondVideoDurations(
		snapshot.Capabilities,
		provider.Model{ID: snapshot.ProviderModelID},
	)
	if err != nil {
		return nil, generationMismatch("当前视频模型没有可执行的整数时长集合", err)
	}
	return values, nil
}

type commerceFrozenVideoCapabilityCandidate struct {
	ModelProfileID          string `json:"modelProfileId"`
	ModelProfileKey         string `json:"modelProfileKey"`
	ModelProfileBindingID   string `json:"modelProfileBindingId"`
	ProviderModelID         string `json:"providerModelId"`
	ProviderAccountID       string `json:"providerAccountId"`
	ModelKey                string `json:"modelKey"`
	Priority                int    `json:"priority"`
	Weight                  int    `json:"weight"`
	VideoGenerationVariants []struct {
		VariantKey                  string   `json:"variantKey"`
		CapabilitySnapshotHash      string   `json:"capabilitySnapshotHash"`
		ExecutableDurationSeconds   []int    `json:"executableDurationSeconds"`
		Resolutions                 []string `json:"resolutions"`
		AspectRatios                []string `json:"aspectRatios"`
		SupportsContinuousExtension bool     `json:"supportsContinuousExtension"`
	} `json:"videoGenerationVariants"`
}

func buildCommerceVideoExecutionEnvelope(
	production commerce.ProductionContext,
	configuration videoproduction.ProductionConfigurationSnapshot,
	raw json.RawMessage,
) (commerce.VideoExecutionEnvelope, string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("视频能力快照无法解析", err)
	}
	videoRaw, ok := root["videoGenerator"]
	if !ok {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("视频能力快照缺少 videoGenerator", nil)
	}
	var video struct {
		Candidates []commerceFrozenVideoCapabilityCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(videoRaw, &video); err != nil {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("视频候选路由无法解析", err)
	}
	if len(video.Candidates) == 0 {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("当前视频模型没有可执行路由，请检查业务模型配置", nil)
	}
	var profile struct {
		ProfileKey string `json:"profileKey"`
	}
	if err := json.Unmarshal(production.VideoBinding.ProfileSnapshot, &profile); err != nil {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("视频 Profile 快照无法解析", err)
	}
	targetResolution := configuredCommerceVideoResolution(configuration)
	if targetResolution == "" {
		targetResolution = selectCommerceVideoResolution(video.Candidates, configuration.AspectRatio, configuration.ImageQuality)
	}
	if targetResolution == "" {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("当前视频模型没有可执行的分辨率", nil)
	}
	envelope := commerce.VideoExecutionEnvelope{
		ContractVersion:                    commerce.CommerceVideoEnvelopeV1,
		ProjectProductionGenerationID:      production.Generation.ID,
		VideoProductionBindingID:           production.VideoBinding.ID,
		VideoProductionBindingRevision:     production.VideoBinding.Revision,
		VideoProductionProfileVersionID:    production.VideoBinding.ProfileVersionID,
		VideoProductionProfileSnapshotHash: production.VideoBinding.ProfileSnapshotHash,
		ModelProfileKey:                    video.Candidates[0].ModelProfileKey,
		TargetResolution:                   targetResolution,
		AspectRatio:                        configuration.AspectRatio,
	}
	durationSet := map[int]struct{}{}
	for _, candidate := range video.Candidates {
		for _, variant := range candidate.VideoGenerationVariants {
			if !containsCommerceNormalizedString(variant.Resolutions, targetResolution) {
				continue
			}
			route := commerce.VideoExecutionRoute{
				RouteKey:       candidate.ModelProfileBindingID + ":" + candidate.ProviderModelID + ":" + variant.VariantKey,
				ModelProfileID: candidate.ModelProfileID, ModelProfileKey: candidate.ModelProfileKey,
				ModelProfileBindingID: candidate.ModelProfileBindingID,
				ProviderModelID:       candidate.ProviderModelID, ProviderAccountID: candidate.ProviderAccountID,
				ModelKey: candidate.ModelKey, Priority: candidate.Priority, Weight: candidate.Weight,
				VariantKey: variant.VariantKey, CapabilitySnapshotHash: variant.CapabilitySnapshotHash,
				ExecutableDurationSeconds:   append([]int(nil), variant.ExecutableDurationSeconds...),
				Resolutions:                 append([]string(nil), variant.Resolutions...),
				AspectRatios:                append([]string(nil), variant.AspectRatios...),
				SupportsContinuousExtension: variant.SupportsContinuousExtension,
			}
			envelope.Routes = append(envelope.Routes, route)
			for _, duration := range variant.ExecutableDurationSeconds {
				if duration > 0 {
					durationSet[duration] = struct{}{}
				}
			}
		}
	}
	envelope.ExecutableDurationSeconds = make([]int, 0, len(durationSet))
	for duration := range durationSet {
		envelope.ExecutableDurationSeconds = append(envelope.ExecutableDurationSeconds, duration)
	}
	canonicalEnvelope, hash, err := commerce.CanonicalizeVideoExecutionEnvelope(envelope)
	if err != nil {
		return commerce.VideoExecutionEnvelope{}, "", generationMismatch("当前视频模型没有同时满足时长和分辨率的候选路由", err)
	}
	return canonicalEnvelope, hash, nil
}

func configuredCommerceVideoResolution(configuration videoproduction.ProductionConfigurationSnapshot) string {
	if len(configuration.Settings) == 0 || string(configuration.Settings) == "null" {
		return ""
	}
	var settings map[string]any
	if err := json.Unmarshal(configuration.Settings, &settings); err != nil {
		return ""
	}
	for _, key := range []string{"videoResolution", "resolution"} {
		if value, ok := settings[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}

func selectCommerceVideoResolution(
	candidates []commerceFrozenVideoCapabilityCandidate,
	aspectRatio string,
	imageQuality string,
) string {
	values := map[string]struct{}{}
	for _, candidate := range candidates {
		for _, variant := range candidate.VideoGenerationVariants {
			for _, value := range variant.Resolutions {
				value = strings.ToLower(strings.TrimSpace(value))
				if value != "" {
					values[value] = struct{}{}
				}
			}
		}
	}
	if len(values) == 0 {
		return ""
	}
	portrait := strings.TrimSpace(aspectRatio) == "9:16"
	preferred := []string{"720p", "1280x720", "720x1280", "1080p", "1920x1080", "1080x1920"}
	if strings.EqualFold(strings.TrimSpace(imageQuality), "high") {
		preferred = []string{"1080p", "1920x1080", "1080x1920", "720p", "1280x720", "720x1280"}
	}
	if portrait {
		if strings.EqualFold(strings.TrimSpace(imageQuality), "high") {
			preferred = []string{"1080x1920", "1080p", "720x1280", "720p", "1920x1080", "1280x720"}
		} else {
			preferred = []string{"720x1280", "720p", "1080x1920", "1080p", "1280x720", "1920x1080"}
		}
	}
	for _, value := range preferred {
		if _, ok := values[value]; ok {
			return value
		}
	}
	sorted := make([]string, 0, len(values))
	for value := range values {
		sorted = append(sorted, value)
	}
	sort.Strings(sorted)
	return sorted[0]
}

func containsCommerceNormalizedString(values []string, expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == expected {
			return true
		}
	}
	return false
}

func loadFrozenLocalizationSegments(
	ctx context.Context,
	tx pgx.Tx,
	state commerceGenerationFrozenState,
) ([]CommerceLocalizedSegmentSnapshot, error) {
	rows, err := tx.Query(ctx, `
		SELECT localized.id::text, localized.source_segment_id::text,
		       localized.segment_no, localized.sales_beat,
		       localized.localized_text, localized.voiceover_text,
		       localized.onscreen_text, localized.product_claims,
		       localized.required_product_features, source.required
		FROM commerce_localization_segments localized
		JOIN commerce_ad_script_segments source
		  ON source.id = localized.source_segment_id
		 AND source.script_version_id = localized.source_script_version_id
		 AND source.script_unit_id = localized.script_unit_id
		 AND source.organization_id = localized.organization_id
		 AND source.project_id = localized.project_id
		WHERE localized.organization_id = $1
		  AND localized.project_id = $2
		  AND localized.script_unit_id = $3
		  AND localized.localization_id = $4
		ORDER BY localized.segment_no
	`, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID, state.Unit.ID, state.Localization.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CommerceLocalizedSegmentSnapshot, 0, len(state.SourceSegments))
	for rows.Next() {
		var item CommerceLocalizedSegmentSnapshot
		var claimsRaw, featuresRaw json.RawMessage
		if err := rows.Scan(
			&item.ID, &item.SourceSegmentID, &item.Ordinal, &item.SalesBeat,
			&item.LocalizedText, &item.VoiceoverText, &item.OnscreenText,
			&claimsRaw, &featuresRaw, &item.Required,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(claimsRaw, &item.ProductClaims); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(featuresRaw, &item.RequiredProductFeatures); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) != len(state.SourceSegments) {
		return nil, generationMismatch("本地化段落与冻结原文段落数量不一致", nil)
	}
	for index := range items {
		if items[index].SourceSegmentID != state.SourceSegments[index].ID || items[index].Ordinal != state.SourceSegments[index].SegmentNo {
			return nil, generationMismatch("本地化段落与冻结原文段落身份不一致", nil)
		}
	}
	return items, nil
}

func (r *CommerceGenerationRuntime) loadLanguageResolutionForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.ScriptUnitPreparationIdentity,
	resolutionID string,
) (commerceLanguageResolutionRow, error) {
	var item commerceLanguageResolutionRow
	err := tx.QueryRow(ctx, `
		SELECT id::text, source_script_version_id::text, language_mode,
		       COALESCE(source_language, ''), COALESCE(target_language, ''),
		       COALESCE(confidence::float8, 0), reasoning,
		       needs_user_confirmation, status, xmin::text::bigint
		FROM commerce_language_resolutions
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND product_id = $4 AND script_unit_id = $5
		FOR UPDATE
	`, resolutionID, identity.OrganizationID, identity.ProjectID, identity.ProductID, identity.ScriptUnitID).Scan(
		&item.ID, &item.SourceScriptVersionID, &item.LanguageMode,
		&item.SourceLanguage, &item.TargetLanguage, &item.Confidence,
		&item.Reasoning, &item.NeedsConfirmation, &item.Status, &item.Revision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commerceLanguageResolutionRow{}, generationMismatch("语言解析结果不属于当前脚本单元", err)
	}
	return item, err
}

func (r *CommerceGenerationRuntime) assertAgentProvenance(
	ctx context.Context,
	tx pgx.Tx,
	callInput CommerceAgentCallInput,
	provenance CommerceAgentProvenance,
) error {
	binding := callInput.Binding
	if provenance.Role != binding.Role || provenance.ProviderModelID != binding.ProviderModelID ||
		provenance.PromptVersionID != binding.PromptVersionID || provenance.PromptTemplateKey != binding.TemplateKey ||
		provenance.PromptHash == "" || provenance.Round < 1 || provenance.Round > binding.MaxReviewRounds {
		return generationMismatch("Agent provenance 与冻结绑定不一致", nil)
	}
	if strings.TrimSpace(provenance.NodeRunID) == "" || strings.TrimSpace(provenance.ProviderCallID) == "" {
		return generationMismatch("Agent provenance 缺少节点或供应商调用身份", nil)
	}
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status
		FROM workflow_node_runs
		WHERE id = $1 AND workflow_run_id = $2
		  AND node_key = $3 AND node_type = $4
		  AND attempt_generation = $5
	`, provenance.NodeRunID, callInput.WorkflowRunID,
		commerceAgentNodeKey(callInput), "agent.commerce."+binding.Role, callInput.AttemptGeneration).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || status != "succeeded" {
		return generationMismatch("Agent provenance 对应的 Workflow 节点未成功", err)
	}
	return err
}

func (r *CommerceGenerationRuntime) assertStoryboardAgentProvenance(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceStoryboardPlanCommit,
) error {
	if len(input.AgentCalls) == 0 {
		return generationMismatch("分镜提交缺少 Agent provenance", nil)
	}
	bindings := map[string]CommerceAgentBinding{
		input.Snapshot.Bindings.StoryboardPlanner.Role:  input.Snapshot.Bindings.StoryboardPlanner,
		input.Snapshot.Bindings.StoryboardReviewer.Role: input.Snapshot.Bindings.StoryboardReviewer,
	}
	requiredRole := input.Snapshot.Bindings.StoryboardPlanner.Role
	seen := make(map[string]bool, len(bindings))
	for _, call := range input.AgentCalls {
		binding, ok := bindings[call.Role]
		if !ok {
			return generationMismatch("分镜提交包含未冻结的 Agent provenance", nil)
		}
		if err := r.assertAgentProvenance(ctx, tx, CommerceAgentCallInput{
			GenerationIdentity: &input.WorkflowInput.Identity,
			WorkflowRunID:      input.WorkflowInput.WorkflowRunID,
			AttemptGeneration:  input.WorkflowInput.AttemptGeneration,
			Phase:              CommercePhaseStoryboard,
			Round:              call.Round,
			Binding:            binding,
		}, call); err != nil {
			return err
		}
		seen[call.Role] = true
	}
	if !seen[requiredRole] {
		return generationMismatch("分镜提交缺少必需的 Agent provenance："+requiredRole, nil)
	}
	return nil
}

func validateCommercePreparationCommit(input CommerceScriptUnitPreparationCommit) error {
	if err := ValidateCommercePreparationSnapshot(input.WorkflowInput.Identity, input.Snapshot); err != nil {
		return generationMismatch("脚本准备提交快照无效", err)
	}
	if input.LanguageResolution.Status != "confirmed" || input.LanguageResolution.InputHash != input.Snapshot.InputHash ||
		input.LanguageResolution.Contract.NeedsUserConfirmation {
		return commerce.Error{Code: commerce.CodeLanguageConfirmation, Message: "脚本目标语言尚未确认"}
	}
	if err := ValidateCommerceLanguageResolution(input.LanguageResolution.Contract, input.Snapshot); err != nil {
		return commerce.Error{Code: CommerceCodeLanguageContractInvalid, Message: "语言解析契约无效", Cause: err}
	}
	if err := ValidateCommerceLocalization(input.Localization, input.Snapshot, input.LanguageResolution.Contract); err != nil {
		return commerce.Error{Code: CommerceCodeLocalizationContractInvalid, Message: "本地化契约无效", Cause: err}
	}
	if err := ValidateCommerceLocalizationReview(input.LocalizationReview, input.Localization); err != nil || input.LocalizationReview.Decision != "approve" {
		return commerce.Error{Code: CommerceCodeLocalizationReviewExhausted, Message: "本地化尚未通过审核", Cause: err}
	}
	policy := commerceAdvisoryTimingPolicy(input.Localization.TargetLanguage)
	expectedTiming, err := AnalyzeCommerceTiming(input.Localization, policy, input.Snapshot.TargetDurationSeconds)
	if err != nil {
		return err
	}
	if expectedTiming != input.Timing {
		return generationMismatch("脚本时长分析与冻结输入不一致", nil)
	}
	return nil
}

func assertFrozenLocalizationMatches(existing commerce.ScriptLocalization, input CommerceScriptUnitPreparationCommit) error {
	localized := make([]string, 0, len(input.Localization.Segments))
	for _, segment := range input.Localization.Segments {
		localized = append(localized, strings.TrimSpace(segment.LocalizedText))
	}
	localizedText := strings.Join(localized, "\n\n")
	contractHash, err := commerceContractHash(input.Localization)
	if err != nil {
		return err
	}
	existingContractHash, err := commerceContractHash(existing.StructuredContract)
	if err != nil {
		return err
	}
	reviewHash, err := commerceContractHash(input.LocalizationReview)
	if err != nil {
		return err
	}
	existingReviewHash, err := commerceContractHash(existing.ReviewerOutput)
	if err != nil {
		return err
	}
	if existing.ID == "" || existing.SourceScriptVersionID != input.Snapshot.SourceScriptVersionID ||
		existing.LanguageResolutionID != input.LanguageResolution.ResolutionID ||
		existing.SourceLanguage != input.Localization.SourceLanguage || existing.TargetLanguage != input.Localization.TargetLanguage ||
		existing.LocalizedContent != localizedText || existing.Status != "approved" || existing.ReviewStatus != "approved" ||
		existing.TimingPolicyVersion != input.Timing.PolicyVersion ||
		existing.EstimatedVoiceoverSeconds != input.Timing.EstimatedVoiceoverSeconds ||
		existingContractHash != contractHash || existingReviewHash != reviewHash {
		return generationMismatch("工作流输出与 UnitGeneration 已冻结的 Localization 不一致；必须创建新单元生产代", nil)
	}
	return nil
}

func validateCommerceStoryboardCommit(input CommerceStoryboardPlanCommit) error {
	if err := ValidateCommerceStoryboardSnapshot(input.WorkflowInput.Identity, input.Snapshot); err != nil {
		return generationMismatch("分镜提交快照无效", err)
	}
	if strings.TrimSpace(input.SalesScriptContractID) == "" || strings.TrimSpace(input.SalesScriptContractHash) == "" {
		return commerce.Error{Code: commerce.CodeScriptOrganizationNeed, Message: "分镜提交缺少已冻结销售脚本契约"}
	}
	if err := ValidateCommerceSalesScript(input.SalesScript, input.Snapshot); err != nil {
		return commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "销售脚本契约无效", Cause: err}
	}
	deterministicPlan, err := BuildCommerceStoryboardDeterministicPlan(input.Snapshot, input.SalesScript)
	if err != nil {
		return commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "确定性分镜切分无效", Cause: err}
	}
	if err := assertCommerceSnapshotEqual(input.DeterministicPlan, deterministicPlan, "确定性分镜切分"); err != nil {
		return err
	}
	mergedPlan, err := applyCommerceStoryboardCreativeDirection(deterministicPlan.Skeleton, input.Plan)
	if err != nil {
		return commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "分镜创意修改了冻结切分结果", Cause: err}
	}
	if err := assertCommerceSnapshotEqual(input.Plan, mergedPlan, "分镜创意契约"); err != nil {
		return err
	}
	projection, err := BuildCommerceStoryboardProjection(input.Snapshot, input.Plan)
	if err != nil {
		return commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "分镜方案契约无效", Cause: err}
	}
	if err := assertCommerceSnapshotEqual(input.Projection, projection, "分镜投影"); err != nil {
		return err
	}
	if err := ValidateCommerceStoryboardReview(input.Review, input.Plan); err != nil || input.Review.Decision != "approve" {
		return commerce.Error{Code: CommerceCodeStoryboardReplanRequired, Message: "分镜方案尚未通过审核", Cause: err}
	}
	return nil
}

func loadStoryboardCommitReplay(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceStoryboardPlanCommit,
) (CommerceStoryboardPlanCommitResult, bool, error) {
	var result CommerceStoryboardPlanCommitResult
	err := tx.QueryRow(ctx, `
		SELECT id::text, revision, actual_shot_count, status, plan_hash
		FROM commerce_storyboard_plans
		WHERE workflow_run_id = $1 AND script_unit_generation_id = $2
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE
	`, input.WorkflowInput.WorkflowRunID, input.WorkflowInput.Identity.UnitGenerationID).Scan(
		&result.StoryboardPlanID, &result.PlanRevision, &result.ShotCount, &result.Status, &result.PlanHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceStoryboardPlanCommitResult{}, false, nil
	}
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, false, err
	}
	if result.PlanHash != input.Projection.PlanHash || result.ShotCount != len(input.Projection.Shots) || result.Status != "ready" {
		return CommerceStoryboardPlanCommitResult{}, false, generationMismatch("同一 Workflow Run 已提交不同的分镜结果", nil)
	}
	result.Identity = input.WorkflowInput.Identity
	return result, true, nil
}

func (r *CommerceGenerationRuntime) persistCommerceStoryboardPlan(
	ctx context.Context,
	tx pgx.Tx,
	state commerceGenerationFrozenState,
	input CommerceStoryboardPlanCommit,
	projection CommerceStoryboardProjection,
) (CommerceStoryboardPlanCommitResult, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_storyboard_plans
		SET active = false
		WHERE script_unit_generation_id = $1 AND active AND status = 'ready'
	`, input.WorkflowInput.Identity.UnitGenerationID); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	var revision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(revision), 0) + 1
		FROM commerce_storyboard_plans
		WHERE script_unit_generation_id = $1
	`, input.WorkflowInput.Identity.UnitGenerationID).Scan(&revision); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	plannerOutput, err := json.Marshal(map[string]any{
		"salesScriptContractId":   input.SalesScriptContractID,
		"salesScriptContractHash": input.SalesScriptContractHash,
		"storyboardPlan":          input.Plan, "agentCalls": input.AgentCalls,
	})
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	reviewerOutput, err := json.Marshal(input.Review)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	segmentationPlan, err := json.Marshal(input.DeterministicPlan.Segmentation)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	videoExecutionEnvelope, err := json.Marshal(input.Snapshot.VideoExecutionEnvelope)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	timingAdvisory, err := json.Marshal(input.DeterministicPlan.TimingAdvisory)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	planner := findCommerceAgentCall(input.AgentCalls, input.Snapshot.Bindings.StoryboardPlanner.Role)
	reviewer := findCommerceAgentCall(input.AgentCalls, input.Snapshot.Bindings.StoryboardReviewer.Role)
	planID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO commerce_storyboard_plans(
			id, organization_id, project_id, product_id, product_version_id,
			script_unit_id, source_script_version_id, localization_id, reference_pack_id,
			project_production_generation_id, script_unit_generation_id,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			workflow_run_id, revision, status, active, stale_state,
			target_language, localized_content_hash, localized_contract_hash,
			timing_policy_version, target_duration_seconds, aspect_ratio,
			timeline_timebase, fps_numerator, fps_denominator,
			estimated_shot_count, actual_shot_count,
			planner_prompt_version_id, reviewer_prompt_version_id,
			planner_provider_call_id, reviewer_provider_call_id,
			planner_output, reviewer_output, review_status, plan_hash, projection_hash,
			allowed_shot_durations, sales_script_contract_id,
			sales_script_contract_hash,
			segmentation_policy_version, segmentation_plan, segmentation_plan_hash,
			video_execution_envelope, video_execution_envelope_hash,
			timing_advisory, preview_hash,
			created_by, activated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14, $15, 'ready', true, 'fresh',
			$16, $17, $18, $19, $20, $21, $22, $23, $24,
			$25, $25, NULLIF($26, '')::uuid, NULLIF($27, '')::uuid,
			NULLIF($28, '')::uuid, NULLIF($29, '')::uuid,
			$30, $31, 'approved', $32, $32, $34,
			NULLIF($35, '')::uuid, $36,
			$37, $38, $39, $40, $41, $42, $43,
			NULLIF($33, '')::uuid, now()
		)
	`, planID, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
		state.Generation.Identity.ProductID, state.Generation.ProductVersionID,
		state.Generation.Identity.ScriptUnitID, state.Generation.SourceScriptVersionID,
		state.Generation.LocalizationID, state.Generation.ReferencePackID,
		state.Generation.Identity.ProjectGenerationID, state.Generation.Identity.UnitGenerationID,
		state.Generation.Identity.CommerceWorkflowBindingID, state.Generation.Identity.CommerceWorkflowBindingRevision,
		input.WorkflowInput.WorkflowRunID, revision, input.Snapshot.TargetLocale,
		input.Snapshot.LocalizedContentHash, input.Snapshot.LocalizedContractHash,
		input.Snapshot.TimingPolicyVersion, input.Snapshot.TargetDurationSeconds,
		input.Snapshot.AspectRatio, input.Snapshot.TimelineTimebase,
		input.Snapshot.FPSNumerator, input.Snapshot.FPSDenominator,
		len(projection.Shots), planner.PromptVersionID, reviewer.PromptVersionID,
		planner.ProviderCallID, reviewer.ProviderCallID, plannerOutput, reviewerOutput,
		projection.PlanHash, input.WorkflowInput.CreatedBy, input.Snapshot.AllowedShotDurations,
		input.SalesScriptContractID, input.SalesScriptContractHash,
		input.Snapshot.SegmentationPolicyVersion, segmentationPlan,
		input.DeterministicPlan.SegmentationPlanHash, videoExecutionEnvelope,
		input.Snapshot.VideoExecutionEnvelopeHash, timingAdvisory,
		input.DeterministicPlan.PreviewHash,
	)
	if err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	for _, shot := range projection.Shots {
		segmentedShot, ok := commerceSegmentationShotByOrdinal(input.DeterministicPlan.Segmentation, shot.ShotOrdinal)
		if !ok {
			return CommerceStoryboardPlanCommitResult{}, generationMismatch("确定性切分缺少镜头规划摘要", nil)
		}
		shotID := uuid.NewString()
		startTick := int64(shot.StartSeconds) * input.Snapshot.TimelineTimebase
		durationTicks := int64(shot.DurationSeconds) * input.Snapshot.TimelineTimebase
		endTick := startTick + durationTicks
		cameraText := strings.TrimSpace(string(shot.Contract.Camera))
		if cameraText == "" {
			cameraText = "{}"
		}
		metadata, err := json.Marshal(map[string]any{
			"commerceCandidateKey":     shot.CandidateKey,
			"shotPurpose":              shot.Contract.ShotPurpose,
			"composition":              shot.Contract.Composition,
			"requestedDurationSeconds": segmentedShot.RequestedDurationSeconds,
			"trimDurationSeconds":      segmentedShot.TrimDurationSeconds,
			"eligibleRouteSetHash":     segmentedShot.EligibleRouteSetHash,
		})
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO storyboard_shots(
				id, organization_id, project_id, shot_index, shot_no, title,
				action, dialogue, asset_bindings, metadata, workflow_run_id,
				visual, camera, status, storyboard_source, review_status, stale_state,
				start_tick, end_tick, duration_min_ticks, duration_max_ticks,
				duration_source, timing_confidence, production_generation_id,
				commerce_storyboard_plan_id
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, '[]', $9, $10,
				$7, $11, 'pending', 'commerce_script', 'approved', 'fresh',
				$12, $13, $14, $14, 'agent_estimated', 1,
				$15, $16
			)
		`, shotID, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
			shot.ShotOrdinal-1, shot.ShotOrdinal, fmt.Sprintf("镜头 %02d", shot.ShotOrdinal),
			shot.Contract.VisualAction, shot.Contract.VoiceoverText, metadata,
			input.WorkflowInput.WorkflowRunID, cameraText, startTick, endTick,
			durationTicks, state.Generation.Identity.ProjectGenerationID, planID)
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		productPresentation, err := json.Marshal(map[string]any{
			"shotPurpose": shot.Contract.ShotPurpose, "composition": shot.Contract.Composition,
			"camera": shot.Contract.Camera, "productReferenceIds": shot.Contract.ProductReferenceIDs,
			"requiredProductFeatures": shot.Contract.RequiredProductFeatures,
		})
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		soundEffects, err := json.Marshal(shot.Contract.SoundEffects)
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		creativeDirection, err := json.Marshal(map[string]any{
			"shotPurpose":         shot.Contract.ShotPurpose,
			"visualAction":        shot.Contract.VisualAction,
			"camera":              shot.Contract.Camera,
			"composition":         shot.Contract.Composition,
			"productReferenceIds": shot.Contract.ProductReferenceIDs,
		})
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO commerce_shot_contracts(
				storyboard_shot_id, organization_id, project_id,
				commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
				sales_beat, visual_action, product_presentation,
				voiceover_text, onscreen_text, target_language,
				sound_effects, music_cue, compliance_flags, contract_hash,
				review_status, reviewer_output, creative_direction,
				estimated_voiceover_ticks, voiceover_overflow_ticks,
				timing_advisory_level, recommended_request_duration_seconds,
				eligible_route_set_hash
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9,
				$10, $11, $12, $13, $14, '[]', $15, 'approved', $16,
				$17, $18, $19, $20, $21, $22
			)
		`, shotID, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
			planID, state.Generation.Identity.ScriptUnitID, state.Generation.Identity.UnitGenerationID,
			shot.Contract.SalesBeat, shot.Contract.VisualAction, productPresentation,
			shot.Contract.VoiceoverText, shot.Contract.OnscreenText, input.Snapshot.TargetLocale,
			soundEffects, shot.Contract.MusicCue, shot.ContractHash, reviewerOutput,
			creativeDirection, segmentedShot.EstimatedVoiceoverTicks,
			segmentedShot.VoiceoverOverflowTicks, segmentedShot.TimingAdvisoryLevel,
			segmentedShot.RequestedDurationSeconds, segmentedShot.EligibleRouteSetHash)
		if err != nil {
			return CommerceStoryboardPlanCommitResult{}, err
		}
		for _, link := range shot.SegmentLinks {
			_, err = tx.Exec(ctx, `
				INSERT INTO commerce_shot_segment_links(
					organization_id, project_id, storyboard_shot_id,
					commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
					localization_id, localization_segment_id, usage, ordinal,
					verbatim_start, verbatim_end
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			`, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
				shotID, planID, state.Generation.Identity.ScriptUnitID,
				state.Generation.Identity.UnitGenerationID, state.Generation.LocalizationID,
				link.LocalizationSegmentID, link.Usage, link.Ordinal,
				link.VerbatimStart, link.VerbatimEnd)
			if err != nil {
				return CommerceStoryboardPlanCommitResult{}, err
			}
		}
		for _, reference := range shot.ProductReferences {
			_, err = tx.Exec(ctx, `
				INSERT INTO commerce_shot_product_references(
					organization_id, project_id, storyboard_shot_id,
					commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
					product_reference_id, source_pack_id, source_pack_item_id,
					role, ordinal, required
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			`, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
				shotID, planID, state.Generation.Identity.ScriptUnitID,
				state.Generation.Identity.UnitGenerationID, reference.ProductReferenceID,
				reference.SourcePackID, reference.SourcePackItemID,
				reference.Role, reference.Ordinal, reference.Required)
			if err != nil {
				return CommerceStoryboardPlanCommitResult{}, err
			}
		}
	}
	if err := r.repository.MarkStoryboardUnitDownstreamStale(
		ctx,
		tx,
		state.Generation.Identity.UnitGenerationID,
		"storyboard_plan_activated",
	); err != nil {
		return CommerceStoryboardPlanCommitResult{}, err
	}
	return CommerceStoryboardPlanCommitResult{
		Identity: input.WorkflowInput.Identity, StoryboardPlanID: planID,
		PlanRevision: revision, ShotCount: len(projection.Shots), Status: "ready",
		PlanHash: projection.PlanHash,
	}, nil
}

func findCommerceAgentCall(items []CommerceAgentProvenance, role string) CommerceAgentProvenance {
	for index := len(items) - 1; index >= 0; index-- {
		if items[index].Role == role {
			return items[index]
		}
	}
	return CommerceAgentProvenance{}
}

func languageResolutionMatchesContract(item commerce.LanguageResolution, contract CommerceLanguageResolutionContract) bool {
	return optionalCommerceString(item.SourceLanguage) == contract.SourceLanguage &&
		optionalCommerceString(item.TargetLanguage) == contract.TargetLanguage &&
		optionalCommerceFloat(item.Confidence) == contract.Confidence &&
		item.Reasoning == contract.Reasoning &&
		item.NeedsUserConfirmation == contract.NeedsUserConfirmation
}

func assertRawJSONHash(raw json.RawMessage, expected string, label string) error {
	actual, err := commerceContractHash(raw)
	if err != nil {
		return generationMismatch(label+"快照无法计算 hash", err)
	}
	if actual != expected {
		return generationMismatch(label+"快照 hash 不一致", nil)
	}
	return nil
}

func assertCommerceSnapshotEqual(expected any, actual any, label string) error {
	expectedHash, err := commerceContractHash(expected)
	if err != nil {
		return err
	}
	actualHash, err := commerceContractHash(actual)
	if err != nil {
		return err
	}
	if expectedHash != actualHash {
		return generationMismatch(label+"已被其他操作修改", nil)
	}
	return nil
}

func generationMismatch(message string, cause error) error {
	return commerce.Error{Code: commerce.CodeGenerationMismatch, Message: message, Cause: cause}
}

var _ CommerceWorkflowActivityPorts = (*CommerceGenerationRuntime)(nil)
var _ CommerceAgentReplayPort = (*CommerceGenerationRuntime)(nil)
