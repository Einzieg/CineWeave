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
	"github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type commerceSetupPromptBinding struct {
	TemplateKey     string `json:"templateKey"`
	PromptVersionID string `json:"promptVersionId"`
	ContentHash     string `json:"contentHash"`
	MaxReviewRounds int    `json:"maxReviewRounds"`
}

type commerceSetupModelContract struct {
	ProfileKey         string `json:"profileKey"`
	TaskType           string `json:"taskType"`
	Modality           string `json:"modality"`
	UsesInputLanguage  bool   `json:"usesInputLanguage"`
	UsesOutputLanguage bool   `json:"usesOutputLanguage"`
	UsesPromptLanguage bool   `json:"usesPromptLanguage"`
	UsesNativeAudio    bool   `json:"usesNativeAudio"`
}

type commerceSetupLanguageContract struct {
	Resolver struct {
		AutoConfidenceThreshold float64 `json:"autoConfidenceThreshold"`
		ConfirmationMode        string  `json:"confirmationMode"`
	} `json:"resolver"`
	Locales []struct {
		Locale       string               `json:"locale"`
		TimingPolicy CommerceTimingPolicy `json:"timingPolicy"`
	} `json:"locales"`
}

type commerceSetupLanguageConfiguration struct {
	LocaleSuggestions   []string
	ConfidenceThreshold float64
	ConfirmationMode    string
	TimingPolicies      map[string]CommerceTimingPolicy
}

type commerceSetupSnapshot struct {
	Run         commerce.SetupRun
	Session     commerce.SetupSession
	Template    commerce.WorkflowTemplateVersion
	Product     commerce.Product
	ProductData commerce.ProductVersion
	References  []commerce.ProductReference
	Unit        commerce.ScriptUnit
	Source      commerce.ScriptVersion
	Segments    []commerce.ScriptSegment
	Preparation CommerceScriptUnitPreparationSnapshot
	Prompts     map[string]commerceSetupPromptBinding
	Models      map[string]commerceSetupModelContract
}

type CommerceSetupRuntime struct {
	db         *pgxpool.Pool
	repository *commerce.Repository
	catalog    *commerce.CatalogService
	providers  *provider.Service
	gateway    *provider.GatewayClient
	prompts    *prompts.Service
}

func NewCommerceSetupRuntime(db *pgxpool.Pool, gateway *provider.GatewayClient) *CommerceSetupRuntime {
	repository := commerce.NewRepository()
	return &CommerceSetupRuntime{
		db: db, repository: repository, catalog: commerce.NewCatalogService(repository),
		providers: provider.NewService(db, nil), gateway: gateway, prompts: prompts.NewService(db),
	}
}

func (r *CommerceSetupRuntime) ExecuteCommerceProjectSetup(ctx context.Context, input CommerceProjectSetupInput) (CommerceProjectSetupOutput, error) {
	if r == nil || r.db == nil || r.gateway == nil {
		return CommerceProjectSetupOutput{}, commerce.Error{Code: provider.CodeProviderGatewayRequired, Message: "Provider Gateway 或工作流数据库未配置"}
	}
	if replay, found, err := r.loadSetupReplay(ctx, input); err != nil {
		return CommerceProjectSetupOutput{}, err
	} else if found {
		return replay, nil
	}
	snapshot, err := r.loadSetupSnapshot(ctx, input)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}

	resolution, waiting, err := r.resolveSetupLanguage(ctx, input, snapshot)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if waiting {
		return CommerceProjectSetupOutput{
			SetupSessionID: input.SetupSessionID, LanguageResolutionID: resolution.ID,
			SuggestedTargetLanguage: optionalCommerceString(resolution.TargetLanguage),
			NeedsUserConfirmation:   true, Status: "waiting_user_confirmation",
		}, nil
	}
	if resolution.SourceLanguage == nil || resolution.TargetLanguage == nil {
		return CommerceProjectSetupOutput{}, commerce.Error{Code: commerce.CodeLanguageConfirmation, Message: "脚本语言尚未确认"}
	}
	configuration, err := videoproduction.LoadProductionConfiguration(ctx, r.db, input.ProjectID)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}

	routing, capabilities, agentBindings, err := r.resolveSetupRouting(
		ctx, snapshot, configuration, *resolution.SourceLanguage, *resolution.TargetLanguage,
	)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	localization, review, calls, err := r.prepareSetupLocalization(
		ctx, snapshot, resolution, agentBindings,
	)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	localizationRaw, err := json.Marshal(localization)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	reviewRaw, err := json.Marshal(review)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	localizedContent := make([]string, 0, len(localization.Segments))
	for _, segment := range localization.Segments {
		localizedContent = append(localizedContent, strings.TrimSpace(segment.LocalizedText))
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	defer tx.Rollback(ctx)
	localizedText := strings.Join(localizedContent, "\n\n")
	var persisted commerce.ScriptLocalization
	existingLocalizations, err := r.catalog.ListLocalizations(ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	for _, candidate := range existingLocalizations {
		if candidate.SourceScriptVersionID == input.SourceScriptVersionID && candidate.LanguageResolutionID == resolution.ID &&
			candidate.SourceLanguage == *resolution.SourceLanguage && candidate.TargetLanguage == *resolution.TargetLanguage &&
			candidate.LocalizedContent == localizedText &&
			candidate.Status == "approved" && candidate.ReviewStatus == "approved" {
			persisted = candidate
			break
		}
	}
	if persisted.ID == "" {
		persisted, _, err = r.catalog.CreateLocalization(ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID, input.RequestedBy, commerce.LocalizationInput{
			SourceScriptVersionID: input.SourceScriptVersionID, LanguageResolutionID: resolution.ID,
			SourceLanguage: *resolution.SourceLanguage, TargetLanguage: *resolution.TargetLanguage,
			LocalizedContent: localizedText, StructuredContract: localizationRaw,
			ReviewerOutput: reviewRaw, Approve: true,
		})
		if err != nil {
			return CommerceProjectSetupOutput{}, err
		}
	}
	currentConfiguration, err := videoproduction.LoadProductionConfiguration(ctx, tx, input.ProjectID)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	frozenConfigurationHash, err := commerceSetupHash(configuration)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	currentConfigurationHash, err := commerceSetupHash(currentConfiguration)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if frozenConfigurationHash != currentConfigurationHash {
		return CommerceProjectSetupOutput{}, commerce.Error{Code: commerce.CodeSetupRevisionConflict, Message: "项目生产配置在创建期间已变化，请重新启动"}
	}
	commerceConfiguration, err := setupCommerceConfigurationSnapshot(snapshot, configuration)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	routingRaw, err := json.Marshal(routing)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	capabilityRaw, err := json.Marshal(capabilities)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	agentCallsRaw, err := json.Marshal(calls)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	referenceIDs := make([]string, 0, len(snapshot.References))
	for _, reference := range snapshot.References {
		referenceIDs = append(referenceIDs, reference.ID)
	}
	result, err := r.catalog.CommitInitialSetup(ctx, tx, commerce.InitialSetupCommitParams{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		SetupSessionID: input.SetupSessionID, SetupRunID: snapshot.Run.ID,
		WorkflowTemplateVersionID: input.WorkflowTemplateVersionID,
		ProductID:                 input.ProductID, ProductVersionID: input.ProductVersionID,
		ProductReferenceIDs: referenceIDs, ScriptUnitID: input.ScriptUnitID,
		SourceScriptVersionID: input.SourceScriptVersionID, LocalizationID: persisted.ID,
		CreatedBy: input.RequestedBy, ProductionConfiguration: configuration,
		CommerceConfiguration: commerceConfiguration, ModelRoutingSnapshot: routingRaw,
		CapabilitySnapshot: capabilityRaw, PreparationInputHash: snapshot.Preparation.InputHash,
		PreparationAgentCalls: agentCallsRaw,
	})
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	productionWorkflowRunID, err := enqueueCommerceStoryboardPlanningTx(ctx, tx, result.Identity, input.RequestedBy, "")
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	output := CommerceProjectSetupOutput{
		Identity:                        result.Identity,
		SetupSessionID:                  input.SetupSessionID,
		ProjectGenerationID:             result.Bindings.ProjectGenerationID,
		VideoProductionBindingID:        result.Bindings.VideoBindingID,
		VideoProductionBindingRevision:  result.Bindings.VideoBindingRevision,
		CommerceWorkflowBindingID:       result.Bindings.CommerceBindingID,
		CommerceWorkflowBindingRevision: result.Bindings.CommerceBindingRevision,
		ScriptUnitGenerationID:          result.UnitGenerationID,
		ScriptUnitGenerationNo:          result.UnitGenerationNo,
		LocalizationID:                  result.LocalizationID,
		ReferencePackID:                 result.ReferencePackID,
		ProductionWorkflowRunID:         productionWorkflowRunID,
		LanguageResolutionID:            resolution.ID,
		Status:                          "completed",
	}
	outputRaw, err := json.Marshal(output)
	if err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET output = $2, updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status = 'succeeded'
	`, snapshot.Run.ID, outputRaw); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET production_workflow_run_id = $2, updated_at = now(), revision = revision + 1
		WHERE id = $1 AND state = 'completed'
	`, input.SetupSessionID, productionWorkflowRunID); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.project_generation.activated", "production_generation", result.Bindings.ProjectGenerationID, map[string]any{
			"projectProductionGenerationId":   result.Bindings.ProjectGenerationID,
			"projectProductionGenerationNo":   result.Bindings.ProjectGenerationNo,
			"videoProductionBindingId":        result.Bindings.VideoBindingID,
			"videoProductionBindingRevision":  result.Bindings.VideoBindingRevision,
			"commerceWorkflowBindingId":       result.Bindings.CommerceBindingID,
			"commerceWorkflowBindingRevision": result.Bindings.CommerceBindingRevision,
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.workflow_binding.created", "commerce_workflow_binding", result.Bindings.CommerceBindingID, map[string]any{
			"projectProductionGenerationId":   result.Bindings.ProjectGenerationID,
			"videoProductionBindingId":        result.Bindings.VideoBindingID,
			"videoProductionBindingRevision":  result.Bindings.VideoBindingRevision,
			"commerceWorkflowBindingId":       result.Bindings.CommerceBindingID,
			"commerceWorkflowBindingRevision": result.Bindings.CommerceBindingRevision,
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.reference_pack.created", "commerce_product_reference_pack", result.ReferencePackID, map[string]any{
			"productVersionId": input.ProductVersionID,
			"referencePackId":  result.ReferencePackID,
			"status":           "active",
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	localizationPayload := map[string]any{
		"workflowRunId":          snapshot.Run.ID,
		"commerceScriptUnitId":   input.ScriptUnitID,
		"scriptUnitGenerationId": result.UnitGenerationID,
		"localizationId":         result.LocalizationID,
		"sourceScriptVersionId":  input.SourceScriptVersionID,
		"sourceLanguage":         *resolution.SourceLanguage,
		"targetLanguage":         *resolution.TargetLanguage,
		"status":                 "approved",
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.script.localization.created", "commerce_script_localization", result.LocalizationID, localizationPayload); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.script.localization.approved", "commerce_script_localization", result.LocalizationID, localizationPayload); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.script_unit.generation.created", "commerce_script_unit_generation", result.UnitGenerationID, map[string]any{
			"workflowRunId":          snapshot.Run.ID,
			"commerceScriptUnitId":   input.ScriptUnitID,
			"scriptUnitGenerationId": result.UnitGenerationID,
			"unitGenerationNo":       result.UnitGenerationNo,
			"status":                 "active",
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.setup.completed", "commerce_setup_run", snapshot.Run.ID, map[string]any{
			"setupSessionId":         input.SetupSessionID,
			"setupRunId":             snapshot.Run.ID,
			"workflowRunId":          snapshot.Run.ID,
			"commerceScriptUnitId":   input.ScriptUnitID,
			"scriptUnitGenerationId": result.UnitGenerationID,
			"status":                 "completed",
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"commerce.storyboard.plan.started", "workflow_run", productionWorkflowRunID, map[string]any{
			"workflowRunId":          productionWorkflowRunID,
			"parentWorkflowRunId":    snapshot.Run.ID,
			"commerceScriptUnitId":   input.ScriptUnitID,
			"scriptUnitGenerationId": result.UnitGenerationID,
			"status":                 "queued",
		}); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceProjectSetupOutput{}, err
	}
	return output, nil
}

func (r *CommerceSetupRuntime) FailCommerceProjectSetup(ctx context.Context, input CommerceProjectSetupFailureInput) error {
	message := strings.TrimSpace(input.ErrorMessage)
	if message == "" {
		message = "项目准备流程执行失败"
	}
	code := strings.TrimSpace(input.ErrorCode)
	if code == "" {
		code = codeActivityFailed
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var runID string
	if err := tx.QueryRow(ctx, `
		SELECT setup_workflow_run_id::text
		FROM commerce_setup_sessions
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		FOR UPDATE
	`, input.WorkflowInput.SetupSessionID, input.WorkflowInput.OrganizationID, input.WorkflowInput.ProjectID).Scan(&runID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'failed', error_code = $2, error_message = $3,
		    completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_user_confirmation', 'needs_user_review')
	`, runID, code, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'failed', step = 'workflow_failed', last_error_code = $2,
		    last_error_message = $3, updated_at = now(), revision = revision + 1
		WHERE id = $1 AND setup_workflow_run_id = $4
		  AND state NOT IN ('completed', 'abandoned')
	`, input.WorkflowInput.SetupSessionID, code, message, runID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *CommerceSetupRuntime) loadSetupReplay(ctx context.Context, input CommerceProjectSetupInput) (CommerceProjectSetupOutput, bool, error) {
	session, err := r.catalog.GetSetupSession(ctx, r.db, input.OrganizationID, input.ProjectID, input.SetupSessionID)
	if err != nil {
		return CommerceProjectSetupOutput{}, false, err
	}
	if session.SetupWorkflowRunID == nil {
		return CommerceProjectSetupOutput{}, false, nil
	}
	run, err := r.catalog.GetSetupRun(ctx, r.db, input.OrganizationID, input.ProjectID, *session.SetupWorkflowRunID)
	if err != nil {
		return CommerceProjectSetupOutput{}, false, err
	}
	if run.Status != "succeeded" {
		return CommerceProjectSetupOutput{}, false, nil
	}
	var output CommerceProjectSetupOutput
	if err := json.Unmarshal(run.Output, &output); err != nil {
		return CommerceProjectSetupOutput{}, false, err
	}
	if output.SetupSessionID != input.SetupSessionID || output.Status != "completed" || output.ProjectGenerationID == "" ||
		output.VideoProductionBindingID == "" || output.CommerceWorkflowBindingID == "" || output.ScriptUnitGenerationID == "" ||
		output.ProductionWorkflowRunID == "" || ValidateCommerceUnitGenerationIdentity(output.Identity) != nil {
		return CommerceProjectSetupOutput{}, false, commerce.Error{Code: commerce.CodeBindingMismatch, Message: "已完成创建任务的输出身份不完整"}
	}
	return output, true, nil
}

func (r *CommerceSetupRuntime) loadSetupSnapshot(ctx context.Context, input CommerceProjectSetupInput) (commerceSetupSnapshot, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	session, err := r.repository.LoadSetupSession(ctx, tx, input.OrganizationID, input.ProjectID, input.SetupSessionID, true)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if session.SetupWorkflowRunID == nil || session.WorkflowTemplateVersionID != input.WorkflowTemplateVersionID ||
		session.ProductID == nil || *session.ProductID != input.ProductID ||
		session.ScriptUnitID == nil || *session.ScriptUnitID != input.ScriptUnitID ||
		session.SourceScriptVersionID == nil || *session.SourceScriptVersionID != input.SourceScriptVersionID ||
		session.State == "abandoned" || session.State == "failed" {
		return commerceSetupSnapshot{}, commerce.Error{Code: commerce.CodeSetupRevisionConflict, Message: "创建任务身份或状态已变化"}
	}
	run, err := r.repository.LoadSetupRun(ctx, tx, input.OrganizationID, input.ProjectID, *session.SetupWorkflowRunID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if run.SetupSessionID != session.ID || (run.Status != "running" && run.Status != "waiting_user_confirmation") {
		return commerceSetupSnapshot{}, commerce.Error{Code: commerce.CodeSetupRevisionConflict, Message: "创建任务已不再可写"}
	}
	template, err := r.repository.ResolvePublishedWorkflowTemplateVersion(ctx, tx, input.OrganizationID, input.WorkflowTemplateVersionID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	product, found, err := r.repository.LockProduct(ctx, tx, input.OrganizationID, input.ProjectID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if !found || product.ID != input.ProductID || product.CurrentVersionID == nil || *product.CurrentVersionID != input.ProductVersionID {
		return commerceSetupSnapshot{}, commerce.Error{Code: commerce.CodeProductVersionStale, Message: "商品版本在创建期间已变化"}
	}
	productData, err := r.repository.LoadProductVersion(ctx, tx, input.OrganizationID, input.ProjectID, input.ProductVersionID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	references, err := r.repository.ListProductReferences(ctx, tx, input.OrganizationID, input.ProjectID, "active")
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	unit, err := r.repository.LoadScriptUnit(ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID, true)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if unit.CurrentSourceVersionID == nil || *unit.CurrentSourceVersionID != input.SourceScriptVersionID || unit.ActiveUnitGenerationID != nil {
		return commerceSetupSnapshot{}, commerce.Error{Code: commerce.CodeScriptVersionStale, Message: "广告脚本在创建期间已变化"}
	}
	source, err := r.repository.LoadScriptVersion(ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID, input.SourceScriptVersionID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	segments, err := r.repository.ListScriptSegments(ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID, input.SourceScriptVersionID)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if len(segments) == 0 || len(references) == 0 {
		return commerceSetupSnapshot{}, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "广告脚本段落或商品图片不完整"}
	}
	preparation, promptBindings, modelContracts, err := buildCommerceSetupPreparation(template, productData, unit, source, segments)
	if err != nil {
		return commerceSetupSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commerceSetupSnapshot{}, err
	}
	return commerceSetupSnapshot{
		Run: run, Session: session, Template: template, Product: product, ProductData: productData,
		References: references, Unit: unit, Source: source, Segments: segments,
		Preparation: preparation, Prompts: promptBindings, Models: modelContracts,
	}, nil
}

func buildCommerceSetupPreparation(
	template commerce.WorkflowTemplateVersion,
	product commerce.ProductVersion,
	unit commerce.ScriptUnit,
	source commerce.ScriptVersion,
	segments []commerce.ScriptSegment,
) (CommerceScriptUnitPreparationSnapshot, map[string]commerceSetupPromptBinding, map[string]commerceSetupModelContract, error) {
	promptBindings, modelContracts, err := commerceSetupTemplateContracts(template)
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, nil, nil, err
	}
	languageConfiguration, err := parseCommerceSetupLanguageConfiguration(template.LanguageContract)
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, nil, nil, commerce.Error{
			Code: commerce.CodeSetupIncomplete, Message: "带货视频语言契约无效", Cause: err,
		}
	}
	sourceSegments := make([]CommerceSourceSegmentSnapshot, 0, len(segments))
	for _, segment := range segments {
		sourceSegments = append(sourceSegments, CommerceSourceSegmentSnapshot{
			ID: segment.ID, Ordinal: segment.SegmentNo, Kind: segment.SegmentKind,
			SourceText: segment.SourceText, ContentHash: segment.ContentHash, Required: segment.Required,
		})
	}
	inputHash, err := commerceSetupHash(map[string]any{
		"templateVersionId": template.ID, "templateHash": template.ContentHash,
		"productVersionId": product.ID, "productFactsHash": product.FactsHash,
		"scriptUnitId": unit.ID, "sourceScriptVersionId": source.ID, "sourceHash": source.ContentHash,
		"languageMode": unit.LanguageMode, "targetLanguage": unit.ExplicitTargetLanguage,
	})
	if err != nil {
		return CommerceScriptUnitPreparationSnapshot{}, nil, nil, err
	}
	return CommerceScriptUnitPreparationSnapshot{
		InputHash: inputHash, WorkflowTemplateVersionID: template.ID,
		WorkflowTemplateContentHash: template.ContentHash, ProductVersionID: product.ID,
		SourceScriptVersionID: source.ID, LanguageMode: unit.LanguageMode,
		ExplicitTargetLanguage:      optionalCommerceString(unit.ExplicitTargetLanguage),
		SourceLanguageHint:          optionalCommerceString(source.SourceLanguageHint),
		AllowedLocales:              languageConfiguration.LocaleSuggestions,
		LanguageConfidenceThreshold: languageConfiguration.ConfidenceThreshold,
		LanguageConfirmationMode:    languageConfiguration.ConfirmationMode,
		TargetDurationSeconds:       unit.TargetDurationSeconds, TargetPlatform: unit.TargetPlatform,
		SourceSegments: sourceSegments, ProductFacts: product.FactsSnapshot,
		TimingPolicies: languageConfiguration.TimingPolicies,
	}, promptBindings, modelContracts, nil
}

func parseCommerceSetupLanguageConfiguration(raw json.RawMessage) (commerceSetupLanguageConfiguration, error) {
	configuration := commerceSetupLanguageConfiguration{
		ConfirmationMode: CommerceLanguageConfirmationDisabled,
		TimingPolicies:   map[string]CommerceTimingPolicy{},
	}
	var contract commerceSetupLanguageContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		return commerceSetupLanguageConfiguration{}, err
	}
	configuration.ConfidenceThreshold = contract.Resolver.AutoConfidenceThreshold
	if mode := strings.TrimSpace(contract.Resolver.ConfirmationMode); mode != "" {
		configuration.ConfirmationMode = mode
	}
	seen := make(map[string]struct{}, len(contract.Locales))
	for _, configured := range contract.Locales {
		locale, err := canonicalCommerceLocale(configured.Locale)
		if err != nil {
			return commerceSetupLanguageConfiguration{}, err
		}
		if _, exists := seen[locale]; exists {
			return commerceSetupLanguageConfiguration{}, fmt.Errorf("locale %s is duplicated", locale)
		}
		seen[locale] = struct{}{}
		configuration.LocaleSuggestions = append(configuration.LocaleSuggestions, locale)
		policy := configured.TimingPolicy
		if strings.TrimSpace(policy.Version) == "" || strings.TrimSpace(policy.Unit) == "" || policy.NormalUnitsPerSecond <= 0 {
			policy = commerceAdvisoryTimingPolicy(locale)
		}
		configuration.TimingPolicies[locale] = policy
	}
	if len(configuration.LocaleSuggestions) == 0 {
		return commerceSetupLanguageConfiguration{}, errors.New("language contract has no locale suggestions")
	}
	if configuration.ConfidenceThreshold <= 0 || configuration.ConfidenceThreshold > 1 {
		return commerceSetupLanguageConfiguration{}, errors.New("language confidence threshold must be greater than 0 and no greater than 1")
	}
	if configuration.ConfirmationMode != CommerceLanguageConfirmationDisabled {
		return commerceSetupLanguageConfiguration{}, fmt.Errorf(
			"language confirmation mode must be %s",
			CommerceLanguageConfirmationDisabled,
		)
	}
	return configuration, nil
}

func commerceSetupTemplateContracts(
	template commerce.WorkflowTemplateVersion,
) (map[string]commerceSetupPromptBinding, map[string]commerceSetupModelContract, error) {
	promptBindings := map[string]commerceSetupPromptBinding{}
	if err := json.Unmarshal(template.PromptBindings, &promptBindings); err != nil {
		return nil, nil, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "带货视频 Prompt 绑定无效", Cause: err}
	}
	modelContracts := map[string]commerceSetupModelContract{}
	if err := json.Unmarshal(template.AgentModelContracts, &modelContracts); err != nil {
		return nil, nil, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "带货视频模型契约无效", Cause: err}
	}
	var imageContract, videoContract commerceSetupModelContract
	if err := json.Unmarshal(template.ImageCapabilityContract, &imageContract); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(template.VideoCapabilityContract, &videoContract); err != nil {
		return nil, nil, err
	}
	modelContracts["imageGenerator"] = imageContract
	modelContracts["videoGenerator"] = videoContract
	return promptBindings, modelContracts, nil
}

func (r *CommerceSetupRuntime) resolveSetupLanguage(ctx context.Context, input CommerceProjectSetupInput, snapshot commerceSetupSnapshot) (commerce.LanguageResolution, bool, error) {
	confirmExisting := func(existing commerce.LanguageResolution) (commerce.LanguageResolution, error) {
		if existing.Status == "confirmed" {
			return existing, nil
		}
		if existing.TargetLanguage == nil {
			return commerce.LanguageResolution{}, commerce.Error{Code: commerce.CodeLanguageRequired, Message: "语言解析没有给出目标语言"}
		}
		tx, err := r.db.Begin(ctx)
		if err != nil {
			return commerce.LanguageResolution{}, err
		}
		defer tx.Rollback(ctx)
		confirmed, err := r.catalog.ConfirmLanguage(
			ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID,
			existing.ID, *existing.TargetLanguage, input.RequestedBy,
		)
		if err != nil {
			return commerce.LanguageResolution{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return commerce.LanguageResolution{}, err
		}
		return confirmed, nil
	}
	if input.LanguageResolutionID != "" {
		existing, err := r.catalog.GetLanguageResolution(ctx, r.db, input.OrganizationID, input.ProjectID, input.ScriptUnitID)
		if err != nil {
			return commerce.LanguageResolution{}, false, err
		}
		if existing.ID != input.LanguageResolutionID || existing.TargetLanguage == nil || *existing.TargetLanguage != input.ConfirmedTargetLanguage {
			return commerce.LanguageResolution{}, false, commerce.Error{Code: commerce.CodeLanguageConfirmation, Message: "语言确认与创建任务不一致"}
		}
		existing, err = confirmExisting(existing)
		if err != nil {
			return commerce.LanguageResolution{}, false, err
		}
		return existing, false, nil
	}
	if existing, err := r.catalog.GetLanguageResolution(ctx, r.db, input.OrganizationID, input.ProjectID, input.ScriptUnitID); err == nil &&
		existing.SourceScriptVersionID == input.SourceScriptVersionID && existing.InputHash == snapshot.Preparation.InputHash {
		existing, err = confirmExisting(existing)
		return existing, false, err
	}
	if err := r.updateSetupProgress(ctx, snapshot.Run.ID, snapshot.Session.ID, "resolving_language", "language_resolution"); err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	binding, err := r.resolveAgentBinding(ctx, snapshot, "languageResolver", snapshot.Preparation.SourceLanguageHint, snapshot.Preparation.ExplicitTargetLanguage)
	if err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	var contract CommerceLanguageResolutionContract
	var lastErr error
	for round := 1; round <= commerceReviewRounds(binding); round++ {
		contextValue := map[string]any{"snapshot": snapshot.Preparation}
		if lastErr != nil {
			contextValue["validationError"] = lastErr.Error()
		}
		output, _, callErr := r.runSetupAgent(ctx, snapshot, binding, round, snapshot.Preparation.SourceLanguageHint, snapshot.Preparation.ExplicitTargetLanguage, contextValue)
		if callErr != nil {
			return commerce.LanguageResolution{}, false, callErr
		}
		contract, lastErr = ParseCommerceLanguageResolution(output)
		if lastErr == nil {
			contract.NeedsUserConfirmation = false
			lastErr = ValidateCommerceLanguageResolution(contract, snapshot.Preparation)
		}
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return commerce.LanguageResolution{}, false, commerce.Error{Code: CommerceCodeLanguageContractInvalid, Message: "语言解析结果连续三次不符合契约", Cause: lastErr}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	defer tx.Rollback(ctx)
	resolution, err := r.catalog.RecordLanguageResolution(
		ctx, tx, input.OrganizationID, input.ProjectID, input.ScriptUnitID, input.RequestedBy,
		contract.SourceLanguage, contract.TargetLanguage, contract.Confidence, contract.Reasoning,
		false, snapshot.Preparation.InputHash,
	)
	if err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	eventName := "commerce.language.resolved"
	if err := appendCommerceWorkflowEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		eventName, "commerce_language_resolution", resolution.ID, map[string]any{
			"workflowRunId":         snapshot.Run.ID,
			"commerceScriptUnitId":  input.ScriptUnitID,
			"languageResolutionId":  resolution.ID,
			"sourceScriptVersionId": input.SourceScriptVersionID,
			"sourceLanguage":        resolution.SourceLanguage,
			"targetLanguage":        resolution.TargetLanguage,
			"confidence":            resolution.Confidence,
			"status":                resolution.Status,
		}); err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commerce.LanguageResolution{}, false, err
	}
	return resolution, false, nil
}

func (r *CommerceSetupRuntime) resolveSetupRouting(
	ctx context.Context,
	snapshot commerceSetupSnapshot,
	configuration videoproduction.ProductionConfigurationSnapshot,
	sourceLanguage string,
	targetLanguage string,
) (map[string]any, map[string]any, map[string]CommerceAgentBinding, error) {
	roles := make([]string, 0, len(snapshot.Models))
	for role := range snapshot.Models {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	routingSnapshot := make(map[string]any, len(roles))
	capabilitySnapshot := make(map[string]any, len(roles))
	agentBindings := make(map[string]CommerceAgentBinding, len(snapshot.Prompts))
	for _, role := range roles {
		contract := snapshot.Models[role]
		request := provider.RoutingRequest{
			OrganizationID: snapshot.Session.OrganizationID, ModelProfileKey: contract.ProfileKey,
			TaskType: contract.TaskType, Modality: contract.Modality,
		}
		candidates, err := r.providers.ResolveRoutingCandidates(ctx, request)
		if err != nil {
			return nil, nil, nil, commerce.Error{Code: provider.CodeUnsupportedCapability, Message: fmt.Sprintf("业务模型 %s 当前不可用", role), Cause: err}
		}
		serialized := make([]map[string]any, 0, len(candidates))
		capabilityCandidates := make([]map[string]any, 0, len(candidates))
		primaryProviderModelID := ""
		primaryCapabilities := []provider.Capability{}
		for _, candidate := range candidates {
			capabilityCandidate := map[string]any{
				"modelProfileId": candidate.ModelProfileID, "modelProfileKey": candidate.ModelProfileKey,
				"modelProfileBindingId": candidate.ModelProfileBindingID,
				"providerModelId":       candidate.ProviderModelID, "providerAccountId": candidate.ProviderAccountID,
				"modelKey": candidate.ModelKey, "modality": candidate.Modality,
				"priority": candidate.Priority, "weight": candidate.Weight,
				"capabilities": candidate.Capabilities,
			}
			if role == "videoGenerator" {
				variants, err := provider.ExecutableVideoGenerationVariants(
					candidate.Capabilities,
					provider.Model{
						ID: candidate.ProviderModelID, ProviderAccountID: candidate.ProviderAccountID,
						ModelKey: candidate.ModelKey, Modality: candidate.Modality,
						Capabilities: candidate.Capabilities,
					},
				)
				if err != nil {
					continue
				}
				variantSnapshots := make([]map[string]any, 0, len(variants))
				for _, variant := range variants {
					durations, durationErr := provider.ExecutableWholeSecondDurationsForVideoVariant(variant)
					if durationErr != nil || len(variant.Resolutions) == 0 {
						continue
					}
					hash, hashErr := provider.VideoGenerationVariantSnapshotHash(variant)
					if hashErr != nil {
						return nil, nil, nil, hashErr
					}
					variantSnapshots = append(variantSnapshots, map[string]any{
						"variantKey": variant.VariantKey, "capabilitySnapshotHash": hash,
						"executableDurationSeconds":   durations,
						"resolutions":                 variant.Resolutions,
						"aspectRatios":                variant.AspectRatios,
						"supportsContinuousExtension": variant.Continuation.SupportsExtension,
						"capability":                  variant,
					})
				}
				if len(variantSnapshots) == 0 {
					continue
				}
				capabilityCandidate["videoGenerationVariants"] = variantSnapshots
			}
			serialized = append(serialized, map[string]any{
				"modelProfileId": candidate.ModelProfileID, "modelProfileKey": candidate.ModelProfileKey,
				"modelProfileBindingId": candidate.ModelProfileBindingID,
				"providerModelId":       candidate.ProviderModelID, "providerAccountId": candidate.ProviderAccountID,
				"modelKey": candidate.ModelKey, "modality": candidate.Modality,
				"priority": candidate.Priority, "weight": candidate.Weight,
			})
			capabilityCandidates = append(capabilityCandidates, capabilityCandidate)
			if primaryProviderModelID == "" {
				primaryProviderModelID = candidate.ProviderModelID
				primaryCapabilities = candidate.Capabilities
			}
		}
		if len(serialized) == 0 {
			if role == "videoGenerator" {
				return nil, nil, nil, commerce.Error{
					Code:    provider.CodeUnsupportedCapability,
					Message: "当前视频业务模型均缺少可执行的整数时长或分辨率能力",
				}
			}
			return nil, nil, nil, commerce.Error{
				Code: provider.CodeUnsupportedCapability, Message: fmt.Sprintf("业务模型 %s 当前没有可执行路由", role),
			}
		}
		routingSnapshot[role] = map[string]any{"request": request, "candidates": serialized}
		capabilitySnapshot[role] = map[string]any{
			"providerModelId": primaryProviderModelID, "capabilities": primaryCapabilities,
			"candidates": capabilityCandidates,
		}
		if promptBinding, ok := snapshot.Prompts[role]; ok {
			binding := CommerceAgentBinding{
				Role: role, TemplateKey: promptBinding.TemplateKey, PromptVersionID: promptBinding.PromptVersionID,
				PromptContentHash: promptBinding.ContentHash, ModelProfileKey: contract.ProfileKey,
				ProviderModelID: primaryProviderModelID, MaxReviewRounds: promptBinding.MaxReviewRounds,
			}
			if err := ValidateCommerceAgentBinding(binding); err != nil {
				return nil, nil, nil, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "带货视频 Agent 绑定无效", Cause: err}
			}
			agentBindings[role] = binding
		}
	}
	return routingSnapshot, capabilitySnapshot, agentBindings, nil
}

func (r *CommerceSetupRuntime) resolveAgentBinding(ctx context.Context, snapshot commerceSetupSnapshot, role, inputLanguage, outputLanguage string) (CommerceAgentBinding, error) {
	promptBinding, ok := snapshot.Prompts[role]
	if !ok {
		return CommerceAgentBinding{}, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "工作流模板缺少 Prompt 绑定：" + role}
	}
	modelContract, ok := snapshot.Models[role]
	if !ok {
		return CommerceAgentBinding{}, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "工作流模板缺少模型契约：" + role}
	}
	candidates, err := r.providers.ResolveRoutingCandidates(ctx, provider.RoutingRequest{
		OrganizationID: snapshot.Session.OrganizationID, ModelProfileKey: modelContract.ProfileKey,
		TaskType: modelContract.TaskType, Modality: modelContract.Modality,
	})
	if err != nil {
		return CommerceAgentBinding{}, err
	}
	binding := CommerceAgentBinding{
		Role: role, TemplateKey: promptBinding.TemplateKey, PromptVersionID: promptBinding.PromptVersionID,
		PromptContentHash: promptBinding.ContentHash, ModelProfileKey: modelContract.ProfileKey,
		ProviderModelID: candidates[0].ProviderModelID, MaxReviewRounds: promptBinding.MaxReviewRounds,
	}
	if err := ValidateCommerceAgentBinding(binding); err != nil {
		return CommerceAgentBinding{}, err
	}
	return binding, nil
}

func (r *CommerceSetupRuntime) prepareSetupLocalization(
	ctx context.Context,
	snapshot commerceSetupSnapshot,
	resolution commerce.LanguageResolution,
	bindings map[string]CommerceAgentBinding,
) (CommerceLocalizationContract, CommerceLocalizationReviewContract, []CommerceAgentProvenance, error) {
	if resolution.SourceLanguage == nil || resolution.TargetLanguage == nil {
		return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil, commerce.Error{Code: commerce.CodeLanguageConfirmation, Message: "脚本语言尚未确认"}
	}
	contract := CommerceLanguageResolutionContract{
		ContractVersion: CommerceLanguageResolutionContractVersion,
		SourceLanguage:  *resolution.SourceLanguage, TargetLanguage: *resolution.TargetLanguage,
		Confidence: optionalCommerceFloat(resolution.Confidence), LanguageComposition: "single",
		NeedsUserConfirmation: false, Reasoning: resolution.Reasoning, Issues: []CommerceLanguageIssue{},
	}
	if err := r.updateSetupProgress(ctx, snapshot.Run.ID, snapshot.Session.ID, "localizing", "script_localization"); err != nil {
		return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil, err
	}
	if contract.SourceLanguage == contract.TargetLanguage {
		candidate := BuildCommerceIdentityLocalization(snapshot.Preparation, contract)
		if err := ValidateCommerceLocalization(candidate, snapshot.Preparation, contract); err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil,
				commerce.Error{Code: CommerceCodeLocalizationContractInvalid, Message: "同语种脚本通道解析失败", Cause: err}
		}
		review := BuildCommerceIdentityLocalizationReview(candidate)
		if err := ValidateCommerceLocalizationReview(review, candidate); err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil,
				commerce.Error{Code: CommerceCodeLocalizationContractInvalid, Message: "同语种脚本本地审核失败", Cause: err}
		}
		return candidate, review, []CommerceAgentProvenance{}, nil
	}

	localizer, localizerOK := bindings["scriptLocalizer"]
	if !localizerOK {
		return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, nil, commerce.Error{Code: commerce.CodeSetupIncomplete, Message: "脚本本地化 Agent 绑定不完整"}
	}
	reviewer, reviewerOK := bindings["localizationReviewer"]
	limit := commerceReviewRounds(localizer)
	feedback := []CommerceReviewIssue{}
	calls := make([]CommerceAgentProvenance, 0, limit*2)
	var lastErr error
	for round := 1; round <= limit; round++ {
		output, call, err := r.runSetupAgent(ctx, snapshot, localizer, round, contract.SourceLanguage, contract.TargetLanguage, map[string]any{
			"snapshot": snapshot.Preparation, "languageResolution": contract, "reviewerIssues": feedback,
		})
		if err != nil {
			return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, calls, err
		}
		calls = append(calls, call)
		candidate, parseErr := ParseCommerceLocalization(output)
		lastErr = parseErr
		if lastErr == nil {
			lastErr = ValidateCommerceLocalization(candidate, snapshot.Preparation, contract)
		}
		if lastErr != nil {
			feedback = commerceValidationFeedback(CommerceCodeLocalizationContractInvalid, "localization", lastErr)
			continue
		}
		if !reviewerOK {
			return candidate, BuildCommerceIdentityLocalizationReview(candidate), calls, nil
		}
		reviewOutput, reviewCall, err := r.runSetupAgent(ctx, snapshot, reviewer, round, contract.SourceLanguage, contract.TargetLanguage, map[string]any{
			"snapshot": snapshot.Preparation, "candidate": candidate,
		})
		if err != nil {
			return candidate, BuildCommerceIdentityLocalizationReview(candidate), calls, nil
		}
		calls = append(calls, reviewCall)
		review, reviewErr := ParseCommerceLocalizationReview(reviewOutput)
		if reviewErr == nil {
			review = CanonicalizeCommerceLocalizationReviewCoverage(review, candidate)
			reviewErr = ValidateCommerceLocalizationReview(review, candidate)
		}
		if reviewErr == nil {
			var approved bool
			candidate, review, approved = ApplyCommerceLocalizationReviewPolicy(candidate, review)
			if approved {
				return candidate, review, calls, nil
			}
		}
		if reviewErr != nil {
			return candidate, BuildCommerceIdentityLocalizationReview(candidate), calls, nil
		}
	}
	return CommerceLocalizationContract{}, CommerceLocalizationReviewContract{}, calls, commerce.Error{
		Code: CommerceCodeLocalizationContractInvalid, Message: fmt.Sprintf("脚本本地化结果在 %d 次尝试后仍不符合结构要求", limit), Cause: lastErr,
	}
}

func (r *CommerceSetupRuntime) runSetupAgent(
	ctx context.Context,
	snapshot commerceSetupSnapshot,
	binding CommerceAgentBinding,
	round int,
	inputLanguage string,
	outputLanguage string,
	contextValue any,
) (string, CommerceAgentProvenance, error) {
	resolved, err := r.prompts.ResolveVersion(ctx, prompts.ResolveRequest{
		OrganizationID: snapshot.Session.OrganizationID, ProjectID: snapshot.Session.ProjectID,
		TemplateKey: binding.TemplateKey,
	}, binding.PromptVersionID)
	if err != nil {
		return "", CommerceAgentProvenance{}, err
	}
	if resolved.ContentHash != binding.PromptContentHash {
		return "", CommerceAgentProvenance{}, commerce.Error{Code: commerce.CodeBindingMismatch, Message: "Prompt 版本内容与工作流模板快照不一致"}
	}
	contextRaw, err := json.Marshal(contextValue)
	if err != nil {
		return "", CommerceAgentProvenance{}, err
	}
	rendered, err := prompts.Render(resolved, map[string]any{"input": map[string]any{"context": string(contextRaw)}})
	if err != nil {
		return "", CommerceAgentProvenance{}, err
	}
	idempotencyKey := fmt.Sprintf("commerce:setup:%s:%s:r%d", snapshot.Run.ID, binding.Role, round)
	response, err := r.gateway.GenerateText(ctx, provider.GatewayTextRequest{
		OrganizationID: snapshot.Session.OrganizationID, ProjectID: snapshot.Session.ProjectID,
		ModelProfileKey: binding.ModelProfileKey, ProviderModelID: binding.ProviderModelID,
		PromptTemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
		IdempotencyKey: idempotencyKey,
		Input:          mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 16000}),
		Options: providerTextGatewayOptionsForAttempt(
			provider.GatewayTextOptions{TimeoutMS: providerTextGatewayTimeoutMS, IdempotencyKey: idempotencyKey},
			currentActivityAttempt(ctx),
		),
	})
	if err != nil {
		return "", CommerceAgentProvenance{}, err
	}
	return response.Output.Text, CommerceAgentProvenance{
		Role: binding.Role, Round: round, ProviderRequestID: response.ProviderRequestID,
		ProviderCallID: response.ProviderCallID, ProviderModelID: response.ModelID,
		PromptTemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		PromptHash: rendered.RenderedHash,
	}, nil
}

func (r *CommerceSetupRuntime) updateSetupProgress(ctx context.Context, runID, sessionID, state, step string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'running', updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('running', 'waiting_user_confirmation')
	`, runID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = $2, step = $3, updated_at = now(), revision = revision + 1
		WHERE id = $1 AND setup_workflow_run_id = $4
		  AND state NOT IN ('completed', 'failed', 'abandoned')
	`, sessionID, state, step, runID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return commerce.Error{Code: commerce.CodeSetupRevisionConflict, Message: "创建任务已不再可写"}
	}
	return tx.Commit(ctx)
}

func setupCommerceConfigurationSnapshot(
	snapshot commerceSetupSnapshot,
	production videoproduction.ProductionConfigurationSnapshot,
) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schemaVersion":               2,
		"workflowTemplateVersionId":   snapshot.Template.ID,
		"workflowTemplateContentHash": snapshot.Template.ContentHash,
		"configuration":               json.RawMessage(snapshot.Template.ConfigurationSnapshot),
		"productionConfiguration":     production,
		"promptBindings":              json.RawMessage(snapshot.Template.PromptBindings),
		"languageContract":            json.RawMessage(snapshot.Template.LanguageContract),
	})
}

func commerceSetupHash(value any) (string, error) {
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

func optionalCommerceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalCommerceFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

var _ CommerceProjectSetupPort = (*CommerceSetupRuntime)(nil)
