package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

// PrepareProjectRebuild materializes the complete target generation while the
// source generation remains active. The caller owns the transaction boundary.
func (s *Service) PrepareProjectRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	rebuildID string,
	params InitialBindingParams,
) (PreparedProjectRebuild, error) {
	rebuild, err := s.repository.LockProjectRebuild(ctx, tx, organizationID, projectID, rebuildID)
	if err != nil {
		return PreparedProjectRebuild{}, err
	}
	if rebuild.Status != "approved" && rebuild.Status != "running" {
		return PreparedProjectRebuild{}, Error{Code: CodeProjectRebuildBlocked, Message: "带货视频项目换代不在可准备状态"}
	}
	source, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return PreparedProjectRebuild{}, err
	}
	if source.Generation.ID != rebuild.SourceProjectGenerationID ||
		source.VideoBinding.ID != rebuild.SourceVideoBindingID ||
		source.CommerceBinding.ID != rebuild.SourceCommerceBindingID ||
		source.CommerceBinding.ConfigurationHash != rebuild.SourceCommerceConfigurationHash {
		return PreparedProjectRebuild{}, Error{Code: CodeBindingMismatch, Message: "带货视频项目换代来源配置已变化"}
	}
	if source.ProjectRevision != rebuild.ExpectedProjectRevision {
		return PreparedProjectRebuild{}, Error{Code: CodeRevisionConflict, Message: "项目已变化，请重新确认换代影响"}
	}
	if !source.ProjectLocked || source.ProjectState != "rebuilding" {
		return PreparedProjectRebuild{}, Error{Code: CodeProjectLocked, Message: "带货视频项目未进入换代准备状态"}
	}
	if rebuild.TargetPrepared != nil {
		return PreparedProjectRebuild{
			RebuildID:         rebuild.ID,
			Source:            source,
			Target:            *rebuild.TargetPrepared,
			PreparedUnitCount: rebuild.PreparedUnitCount,
		}, nil
	}

	params.OrganizationID = organizationID
	params.ProjectID = projectID
	params.SourceGenerationID = source.Generation.ID
	params.RebuildID = rebuild.ID
	target, err := s.PrepareInitialBindings(ctx, tx, params)
	if err != nil {
		return PreparedProjectRebuild{}, err
	}
	if target.VideoProfileVersionID != rebuild.TargetProfileVersionID {
		return PreparedProjectRebuild{}, Error{
			Code:    CodeBindingMismatch,
			Message: "带货视频业务模板与目标视频生产方案不一致",
			Details: map[string]any{
				"expectedProfileVersionId": rebuild.TargetProfileVersionID,
				"actualProfileVersionId":   target.VideoProfileVersionID,
			},
		}
	}

	seeds, err := s.repository.ListProjectRebuildUnitSeeds(ctx, tx, source)
	if err != nil {
		return PreparedProjectRebuild{}, err
	}
	if err := s.repository.AttachPreparedProjectRebuild(ctx, tx, rebuild, target, len(seeds)); err != nil {
		return PreparedProjectRebuild{}, err
	}
	for _, seed := range seeds {
		prepared, err := prepareProjectRebuildUnit(
			seed,
			target,
			params.WorkflowTemplateVersion,
			rebuild.ID,
		)
		if err != nil {
			return PreparedProjectRebuild{}, fmt.Errorf("prepare script unit %s: %w", seed.ScriptUnitID, err)
		}
		if err := s.repository.InsertPreparingProjectRebuildUnit(ctx, tx, rebuild.ID, target, prepared, params.CreatedBy); err != nil {
			return PreparedProjectRebuild{}, err
		}
	}
	return PreparedProjectRebuild{
		RebuildID:         rebuild.ID,
		Source:            source,
		Target:            target,
		PreparedUnitCount: len(seeds),
	}, nil
}

// ActivatePreparedProjectRebuild performs the all-or-nothing generation switch.
// No provider work is started here; every target unit leaves the transaction in
// storyboard_required state through the project aggregate.
func (s *Service) ActivatePreparedProjectRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	rebuildID string,
) (ProjectRebuildActivationResult, error) {
	rebuild, err := s.repository.LockProjectRebuild(ctx, tx, organizationID, projectID, rebuildID)
	if err != nil {
		return ProjectRebuildActivationResult{}, err
	}
	if rebuild.Status == "succeeded" && rebuild.TargetPrepared != nil {
		return s.repository.ActivatePreparedProjectRebuild(
			ctx,
			tx,
			rebuild,
			*rebuild.TargetPrepared,
			videoproduction.ProductionConfigurationSnapshot{},
			rebuild.PreparedUnitCount,
		)
	}
	if rebuild.Status != "running" || rebuild.TargetPrepared == nil {
		return ProjectRebuildActivationResult{}, Error{Code: CodeProjectRebuildBlocked, Message: "带货视频项目换代尚未完成全量预检"}
	}
	var configuration videoproduction.ProductionConfigurationSnapshot
	if err := json.Unmarshal(rebuild.TargetConfiguration, &configuration); err != nil {
		return ProjectRebuildActivationResult{}, fmt.Errorf("decode target production configuration: %w", err)
	}
	if configuration.ProjectType != ProjectTypeCommerce || configuration.ContentType != "" {
		return ProjectRebuildActivationResult{}, Error{Code: CodeProjectKindMismatch, Message: "目标生产配置不是带货视频配置"}
	}
	return s.repository.ActivatePreparedProjectRebuild(
		ctx,
		tx,
		rebuild,
		*rebuild.TargetPrepared,
		configuration,
		rebuild.PreparedUnitCount,
	)
}

func prepareProjectRebuildUnit(
	seed ProjectRebuildUnitSeed,
	target InitialBindingResult,
	workflowTemplateVersionID string,
	rebuildID string,
) (ProjectRebuildUnitTarget, error) {
	var snapshot map[string]any
	if err := json.Unmarshal(seed.ConfigurationSnapshot, &snapshot); err != nil {
		return ProjectRebuildUnitTarget{}, err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["productionIdentity"] = map[string]any{
		"projectGenerationId":             target.ProjectGenerationID,
		"videoProductionBindingId":        target.VideoBindingID,
		"videoProductionBindingRevision":  target.VideoBindingRevision,
		"videoProfileSnapshotHash":        target.VideoProfileSnapshotHash,
		"commerceWorkflowBindingId":       target.CommerceBindingID,
		"commerceWorkflowBindingRevision": target.CommerceBindingRevision,
		"commerceConfigurationHash":       target.CommerceConfigurationHash,
	}
	snapshot["projectGenerationId"] = target.ProjectGenerationID
	snapshot["videoProductionBindingId"] = target.VideoBindingID
	snapshot["videoProductionBindingRevision"] = target.VideoBindingRevision
	snapshot["commerceWorkflowBindingId"] = target.CommerceBindingID
	snapshot["commerceWorkflowBindingRevision"] = target.CommerceBindingRevision
	snapshot["workflowTemplateVersionId"] = workflowTemplateVersionID
	snapshot["rebuildId"] = rebuildID
	snapshot["sourceUnitGenerationId"] = seed.SourceUnitGenerationID
	snapshot["targetConfigurationHash"] = target.CommerceConfigurationHash
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ProjectRebuildUnitTarget{}, err
	}
	hash, err := hashJSON(raw)
	if err != nil {
		return ProjectRebuildUnitTarget{}, err
	}
	return ProjectRebuildUnitTarget{
		ProjectRebuildUnitSeed:  seed,
		TargetUnitGenerationID:  newID(),
		TargetUnitGenerationNo:  seed.SourceUnitGenerationNo + 1,
		TargetConfiguration:     raw,
		TargetConfigurationHash: hash,
	}, nil
}

func validatePreparedTarget(rebuild ProjectRebuildContext, target InitialBindingResult) error {
	if rebuild.TargetVideoBindingID != target.VideoBindingID ||
		rebuild.TargetProjectGenerationID != target.ProjectGenerationID ||
		rebuild.TargetCommerceBindingID != target.CommerceBindingID ||
		rebuild.TargetCommerceConfigurationHash != target.CommerceConfigurationHash {
		return Error{Code: CodeBindingMismatch, Message: "带货视频目标换代身份不一致"}
	}
	return nil
}

func projectRebuildNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return Error{Code: CodeProjectRebuildBlocked, Message: "带货视频项目换代记录不存在", Cause: err}
	}
	return err
}
