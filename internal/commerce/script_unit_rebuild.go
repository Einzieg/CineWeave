package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const scriptUnitRebuildImpactLifetime = 15 * time.Minute

func (s *CatalogService) PlanScriptUnitRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	target ScriptUnitRebuildTarget,
	requestedBy string,
) (ScriptUnitRebuildImpact, error) {
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	if unit.Status == "archived" || unit.ActiveUnitGenerationID == nil {
		return ScriptUnitRebuildImpact{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元没有可换代的活动生产代"}
	}
	if target.ExpectedRevision != unit.Revision {
		return ScriptUnitRebuildImpact{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	generation, err := s.repository.LockUnitGenerationContext(ctx, tx, production, UnitGenerationIdentity{
		ExecutionIdentity: production.ExecutionIdentity(), ProductID: unit.ProductID,
		ScriptUnitID: unit.ID, UnitGenerationID: *unit.ActiveUnitGenerationID,
	})
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	if target.TargetStoryboardStrategy == "" {
		var sourceConfig struct {
			StoryboardStrategy StoryboardStrategy `json:"storyboardStrategy"`
		}
		if err := json.Unmarshal(generation.ConfigurationSnapshot, &sourceConfig); err != nil {
			return ScriptUnitRebuildImpact{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元生产配置无法解析", Cause: err}
		}
		target.TargetStoryboardStrategy = sourceConfig.StoryboardStrategy
	}
	target, err = normalizeScriptUnitRebuildTarget(target, unit)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	version, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unitID, target.TargetSourceScriptVersionID)
	if err != nil {
		return ScriptUnitRebuildImpact{}, Error{Code: CodeScriptVersionStale, Message: "目标脚本版本不存在", Cause: err}
	}
	targetConfiguration := map[string]any{
		"schemaVersion":                 2,
		"projectGenerationId":           production.Generation.ID,
		"sourceUnitGenerationId":        generation.Identity.UnitGenerationID,
		"sourceUnitConfigurationHash":   generation.Identity.UnitConfigurationHash,
		"targetSourceScriptVersionId":   version.ID,
		"targetSourceScriptContentHash": version.ContentHash,
		"targetLanguageMode":            target.TargetLanguageMode,
		"targetLanguage":                target.TargetLanguage,
		"targetDurationSeconds":         target.TargetDurationSeconds,
		"targetPlatform":                target.TargetPlatform,
		"targetStoryboardStrategy":      target.TargetStoryboardStrategy,
	}
	targetRaw, err := json.Marshal(targetConfiguration)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	targetHash, err := hashJSON(targetRaw)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	counts, err := s.repository.LoadScriptUnitRebuildAffectedCounts(ctx, tx, organizationID, projectID, generation.Identity.UnitGenerationID)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	blockers, err := s.repository.LoadScriptUnitRebuildBlockers(ctx, tx, organizationID, projectID, generation.Identity.UnitGenerationID)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	expiresAt := time.Now().UTC().Add(scriptUnitRebuildImpactLifetime)
	token := hashText(uuid.NewString() + ":" + unit.ID + ":" + targetHash)
	impact := ScriptUnitRebuildImpact{
		ProjectID: projectID, ProjectGenerationID: production.Generation.ID,
		ScriptUnitID: unit.ID, SourceUnitGenerationID: generation.Identity.UnitGenerationID,
		SourceScriptVersionID:       generation.SourceScriptVersionID,
		TargetSourceScriptVersionID: version.ID, ExpectedRevision: unit.Revision,
		TargetLanguageMode: target.TargetLanguageMode, TargetLanguage: target.TargetLanguage,
		TargetDurationSeconds: target.TargetDurationSeconds, TargetPlatform: target.TargetPlatform,
		TargetStoryboardStrategy: target.TargetStoryboardStrategy,
		TargetConfigurationHash:  targetHash, ImpactToken: token, ExpiresAt: expiresAt,
		Affected: counts, EstimatedAgentCalls: 3, Blockers: blockers,
	}
	impactRaw, err := json.Marshal(impact)
	if err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	if err := s.repository.SupersedePlannedScriptUnitRebuilds(ctx, tx, organizationID, projectID, unitID); err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	if err := s.repository.InsertPlannedScriptUnitRebuild(
		ctx, tx, production, unit, generation, target, targetRaw, targetHash,
		impactRaw, token, requestedBy,
	); err != nil {
		return ScriptUnitRebuildImpact{}, err
	}
	return impact, nil
}

func (s *CatalogService) ApproveScriptUnitRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	impactToken string,
	expectedRevision int64,
	idempotencyKey string,
) (ScriptUnitRebuildExecution, error) {
	impactToken = strings.TrimSpace(impactToken)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if impactToken == "" || idempotencyKey == "" {
		return ScriptUnitRebuildExecution{}, errors.New("script unit rebuild impact token and idempotency key are required")
	}
	rebuild, err := s.repository.LockScriptUnitRebuildByToken(ctx, tx, organizationID, projectID, unitID, impactToken)
	if err != nil {
		return ScriptUnitRebuildExecution{}, Error{Code: CodeScriptRebuildStale, Message: "脚本换代确认已失效，请重新检查影响", Cause: err}
	}
	if rebuild.Status != "planned" {
		if rebuild.IdempotencyKey == idempotencyKey && rebuild.WorkflowRunID != "" {
			return scriptUnitRebuildExecution(rebuild, ScriptUnitPreparationIdentity{}, true), nil
		}
		return ScriptUnitRebuildExecution{}, Error{Code: CodeScriptRebuildStale, Message: "脚本换代状态已变化，请重新检查影响"}
	}
	var impact ScriptUnitRebuildImpact
	if err := json.Unmarshal(rebuild.ImpactSnapshot, &impact); err != nil {
		return ScriptUnitRebuildExecution{}, err
	}
	if time.Now().UTC().After(impact.ExpiresAt) || rebuild.ExpectedRevision != expectedRevision {
		return ScriptUnitRebuildExecution{}, Error{Code: CodeScriptRebuildStale, Message: "脚本换代确认已过期，请重新检查影响"}
	}
	identity, err := s.scriptUnitRebuildPreparationIdentity(ctx, tx, rebuild)
	if err != nil {
		return ScriptUnitRebuildExecution{}, err
	}
	blockers, err := s.repository.LoadScriptUnitRebuildBlockers(ctx, tx, organizationID, projectID, rebuild.SourceUnitGenerationID)
	if err != nil {
		return ScriptUnitRebuildExecution{}, err
	}
	if len(blockers) > 0 {
		return ScriptUnitRebuildExecution{}, Error{
			Code: CodeScriptRebuildBlocked, Message: "脚本仍有活动任务，暂不能换代",
			Details: map[string]any{"blockers": blockers},
		}
	}
	if err := s.repository.MarkScriptUnitRebuildRunning(ctx, tx, rebuild, idempotencyKey); err != nil {
		return ScriptUnitRebuildExecution{}, err
	}
	rebuild.Status = "running"
	rebuild.IdempotencyKey = idempotencyKey
	return scriptUnitRebuildExecution(rebuild, identity, false), nil
}

func (s *CatalogService) AttachScriptUnitRebuildWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	rebuildID string,
	workflowRunID string,
) error {
	return s.repository.AttachScriptUnitRebuildWorkflow(ctx, tx, rebuildID, workflowRunID)
}

func (s *CatalogService) scriptUnitRebuildPreparationIdentity(
	ctx context.Context,
	tx pgx.Tx,
	rebuild persistedScriptUnitRebuild,
) (ScriptUnitPreparationIdentity, error) {
	production, err := s.repository.LockActiveProductionContext(ctx, tx, rebuild.OrganizationID, rebuild.ProjectID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if production.Generation.ID != rebuild.ProjectGenerationID {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptRebuildStale, Message: "项目生产配置已变化，请重新检查影响"}
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, rebuild.OrganizationID, rebuild.ProjectID, rebuild.ScriptUnitID, true)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if unit.Revision != rebuild.ExpectedRevision || unit.ActiveUnitGenerationID == nil || *unit.ActiveUnitGenerationID != rebuild.SourceUnitGenerationID {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptRebuildStale, Message: "脚本单元已变化，请重新检查影响"}
	}
	generation, err := s.repository.LockUnitGenerationContext(ctx, tx, production, UnitGenerationIdentity{
		ExecutionIdentity: production.ExecutionIdentity(), ProductID: unit.ProductID,
		ScriptUnitID: unit.ID, UnitGenerationID: rebuild.SourceUnitGenerationID,
		UnitConfigurationHash: rebuild.SourceUnitConfigurationHash,
	})
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	targetVersion, err := s.repository.LoadScriptVersion(ctx, tx, rebuild.OrganizationID, rebuild.ProjectID, unit.ID, rebuild.TargetSourceScriptVersionID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptRebuildStale, Message: "目标脚本版本已失效", Cause: err}
	}
	productVersion, err := s.repository.LoadProductVersion(ctx, tx, rebuild.OrganizationID, rebuild.ProjectID, generation.ProductVersionID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	pack, err := s.repository.LoadProductReferencePack(ctx, tx, rebuild.OrganizationID, rebuild.ProjectID, generation.ReferencePackID)
	if err != nil {
		return ScriptUnitPreparationIdentity{}, err
	}
	if pack.PackHash == "" || pack.ProductVersionID != productVersion.ID {
		return ScriptUnitPreparationIdentity{}, Error{Code: CodeScriptRebuildStale, Message: "商品引用包已变化，请重新检查影响"}
	}
	return ScriptUnitPreparationIdentity{
		ExecutionIdentity: production.ExecutionIdentity(), ProductID: unit.ProductID,
		ProductVersionID: productVersion.ID, ProductFactsHash: productVersion.FactsHash,
		ScriptUnitID: unit.ID, ScriptUnitRevision: unit.Revision,
		SourceScriptVersionID: targetVersion.ID, SourceScriptContentHash: targetVersion.ContentHash,
		ReferencePackID: pack.ID, ReferencePackHash: pack.PackHash,
		RebuildID: rebuild.ID, SourceUnitGenerationID: rebuild.SourceUnitGenerationID,
		TargetConfigurationHash: rebuild.TargetConfigurationHash,
	}, nil
}

func normalizeScriptUnitRebuildTarget(target ScriptUnitRebuildTarget, current ScriptUnit) (ScriptUnitRebuildTarget, error) {
	target.TargetSourceScriptVersionID = strings.TrimSpace(target.TargetSourceScriptVersionID)
	target.TargetLanguageMode = strings.ToLower(strings.TrimSpace(target.TargetLanguageMode))
	target.TargetPlatform = strings.TrimSpace(target.TargetPlatform)
	if target.TargetSourceScriptVersionID == "" || target.TargetPlatform == "" {
		return ScriptUnitRebuildTarget{}, Error{Code: CodeScriptRebuildRequired, Message: "脚本换代目标配置不完整"}
	}
	if target.TargetLanguageMode != "auto" && target.TargetLanguageMode != "explicit" {
		return ScriptUnitRebuildTarget{}, Error{Code: CodeLanguageUnsupported, Message: "目标语言模式无效"}
	}
	if target.TargetLanguageMode == "auto" {
		target.TargetLanguage = nil
	} else {
		if target.TargetLanguage == nil {
			return ScriptUnitRebuildTarget{}, Error{Code: CodeLanguageRequired, Message: "请选择目标语言"}
		}
		locale, err := normalizeLocale(*target.TargetLanguage)
		if err != nil {
			return ScriptUnitRebuildTarget{}, err
		}
		target.TargetLanguage = &locale
	}
	if target.TargetDurationSeconds <= 0 {
		return ScriptUnitRebuildTarget{}, Error{Code: CodeDurationExceeded, Message: "目标时长必须为正整数秒"}
	}
	strategy, err := ParseStoryboardStrategy(string(target.TargetStoryboardStrategy))
	if err != nil || strategy == StoryboardStrategyManual {
		return ScriptUnitRebuildTarget{}, Error{Code: CodeStoryboardStrategy, Message: "请选择智能切分或单段生成"}
	}
	target.TargetStoryboardStrategy = strategy
	if current.Status == "archived" {
		return ScriptUnitRebuildTarget{}, Error{Code: CodeScriptUnitArchived, Message: "已归档脚本不能换代"}
	}
	return target, nil
}

func scriptUnitRebuildExecution(
	rebuild persistedScriptUnitRebuild,
	identity ScriptUnitPreparationIdentity,
	replay bool,
) ScriptUnitRebuildExecution {
	return ScriptUnitRebuildExecution{
		RebuildID: rebuild.ID, Status: rebuild.Status, WorkflowRunID: rebuild.WorkflowRunID,
		ScriptUnitID: rebuild.ScriptUnitID, SourceUnitGenerationID: rebuild.SourceUnitGenerationID,
		TargetSourceScriptVersionID: rebuild.TargetSourceScriptVersionID,
		TargetConfigurationHash:     rebuild.TargetConfigurationHash,
		PreparationIdentity:         identity, IdempotentReplay: replay,
	}
}
