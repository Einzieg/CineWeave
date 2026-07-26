package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

const DefaultWorkflowTemplateKey = "commerce_video_v1"

type Service struct {
	repository Store
}

type Store interface {
	ResolvePublishedWorkflowTemplate(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error)
	ResolvePublishedWorkflowTemplateVersion(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error)
	ResolveWorkflowTemplateVersionForRebuild(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error)
	InsertDraftProject(context.Context, pgx.Tx, string, DraftProjectParams) error
	InsertProjectOwner(context.Context, pgx.Tx, string, string, string) error
	InsertSetupSession(context.Context, pgx.Tx, string, string, WorkflowTemplateVersion, DraftProjectParams) error
	CompleteDirectProjectSetup(context.Context, pgx.Tx, string, string, InitialBindingResult) error
	InsertPreparingVideoBinding(context.Context, pgx.Tx, string, InitialBindingParams, string, []byte, string, int64) error
	InsertPreparingCommerceBinding(context.Context, pgx.Tx, string, string, WorkflowTemplateVersion, InitialBindingParams, string, string, int64) error
	InsertPreparingProjectGeneration(context.Context, pgx.Tx, string, string, string, InitialBindingParams, int64) error
	NextBindingRevision(context.Context, pgx.Tx, string) (int64, error)
	NextProjectGenerationNo(context.Context, pgx.Tx, string) (int64, error)
	ActivateInitialBindings(context.Context, pgx.Tx, string, InitialBindingResult) error
	LockBindingPreparationProject(context.Context, pgx.Tx, string, string) (BindingPreparationState, error)
	LockActiveProductionContext(context.Context, pgx.Tx, string, string) (ProductionContext, error)
	LockUnitGenerationContext(context.Context, pgx.Tx, ProductionContext, UnitGenerationIdentity) (UnitGenerationContext, error)
	LockProjectRebuild(context.Context, pgx.Tx, string, string, string) (ProjectRebuildContext, error)
	ListProjectRebuildUnitSeeds(context.Context, pgx.Tx, ProductionContext) ([]ProjectRebuildUnitSeed, error)
	InsertPreparingProjectRebuildUnit(context.Context, pgx.Tx, string, InitialBindingResult, ProjectRebuildUnitTarget, string) error
	AttachPreparedProjectRebuild(context.Context, pgx.Tx, ProjectRebuildContext, InitialBindingResult, int) error
	ActivatePreparedProjectRebuild(context.Context, pgx.Tx, ProjectRebuildContext, InitialBindingResult, videoproduction.ProductionConfigurationSnapshot, int) (ProjectRebuildActivationResult, error)
}

func NewService(repository Store) *Service {
	if repository == nil {
		repository = NewRepository()
	}
	return &Service{repository: repository}
}

func (s *Service) CreateDraftProject(ctx context.Context, tx pgx.Tx, params DraftProjectParams) (DraftProjectResult, error) {
	params.Name = strings.TrimSpace(params.Name)
	params.OrganizationID = strings.TrimSpace(params.OrganizationID)
	params.WorkspaceID = strings.TrimSpace(params.WorkspaceID)
	params.CreatedBy = strings.TrimSpace(params.CreatedBy)
	params.IdempotencyScope = strings.TrimSpace(params.IdempotencyScope)
	params.ClientRequestID = strings.TrimSpace(params.ClientRequestID)
	if params.Name == "" || params.OrganizationID == "" || params.WorkspaceID == "" || params.CreatedBy == "" {
		return DraftProjectResult{}, errors.New("commerce project identity is incomplete")
	}
	if params.IdempotencyScope == "" || params.ClientRequestID == "" {
		return DraftProjectResult{}, errors.New("commerce setup idempotency identity is incomplete")
	}
	if !validSHA256(params.RequestHash) {
		return DraftProjectResult{}, errors.New("commerce setup request hash is invalid")
	}
	if params.SetupExpiresAt.IsZero() {
		return DraftProjectResult{}, errors.New("commerce setup expiration is required")
	}
	if len(params.Settings) == 0 {
		params.Settings = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(params.Settings); err != nil {
		return DraftProjectResult{}, errors.New("commerce project settings must be a JSON object")
	}
	if err := validateJSONObject(params.InputSnapshot); err != nil {
		return DraftProjectResult{}, errors.New("commerce setup input snapshot must be a JSON object")
	}
	if params.WorkflowTemplateKey == "" {
		params.WorkflowTemplateKey = DefaultWorkflowTemplateKey
	}
	template, err := s.repository.ResolvePublishedWorkflowTemplate(ctx, tx, params.OrganizationID, params.WorkflowTemplateKey)
	if err != nil {
		return DraftProjectResult{}, err
	}
	projectID := newID()
	setupSessionID := newID()
	if err := s.repository.InsertDraftProject(ctx, tx, projectID, params); err != nil {
		return DraftProjectResult{}, err
	}
	if err := s.repository.InsertProjectOwner(ctx, tx, params.OrganizationID, projectID, params.CreatedBy); err != nil {
		return DraftProjectResult{}, err
	}
	if err := s.repository.InsertSetupSession(ctx, tx, setupSessionID, projectID, template, params); err != nil {
		return DraftProjectResult{}, err
	}
	return DraftProjectResult{
		ProjectID:                 projectID,
		SetupSessionID:            setupSessionID,
		SetupState:                "draft",
		WorkflowTemplateVersionID: template.ID,
		SetupConfigurationHash:    template.ContentHash,
	}, nil
}

func (s *Service) PrepareInitialBindings(ctx context.Context, tx pgx.Tx, params InitialBindingParams) (InitialBindingResult, error) {
	if params.OrganizationID == "" || params.ProjectID == "" || params.WorkflowTemplateVersion == "" {
		return InitialBindingResult{}, errors.New("commerce binding identity is incomplete")
	}
	if params.CompatibilityPolicy == "" {
		params.CompatibilityPolicy = videoproduction.CompatibilityStrict
	}
	preparation, err := s.repository.LockBindingPreparationProject(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return InitialBindingResult{}, err
	}
	if params.SourceGenerationID == "" {
		if preparation.ActiveGenerationID != nil {
			return InitialBindingResult{}, Error{Code: CodeBindingMismatch, Message: "带货视频项目已经存在活动生产绑定"}
		}
		if params.RebuildID != "" {
			return InitialBindingResult{}, errors.New("commerce rebuild requires a source generation")
		}
	} else {
		if preparation.ActiveGenerationID == nil || *preparation.ActiveGenerationID != params.SourceGenerationID {
			return InitialBindingResult{}, Error{Code: CodeBindingMismatch, Message: "带货视频重建来源生产代已变化"}
		}
		if params.RebuildID == "" || !preparation.ProductionLocked || preparation.ProductionState != "rebuilding" {
			return InitialBindingResult{}, Error{Code: CodeProjectLocked, Message: "带货视频项目未进入重建准备状态"}
		}
	}
	if len(params.VideoOverrides) == 0 {
		params.VideoOverrides = json.RawMessage(`{}`)
	}
	for _, snapshot := range []*json.RawMessage{
		&params.ConfigurationSnapshot,
		&params.ModelRoutingSnapshot,
		&params.CapabilitySnapshot,
	} {
		if len(*snapshot) == 0 {
			*snapshot = json.RawMessage(`{}`)
		}
		var object map[string]any
		if err := json.Unmarshal(*snapshot, &object); err != nil {
			return InitialBindingResult{}, err
		}
	}
	var template WorkflowTemplateVersion
	if params.RebuildID == "" {
		template, err = s.repository.ResolvePublishedWorkflowTemplateVersion(
			ctx,
			tx,
			params.OrganizationID,
			params.WorkflowTemplateVersion,
		)
	} else {
		template, err = s.repository.ResolveWorkflowTemplateVersionForRebuild(
			ctx,
			tx,
			params.OrganizationID,
			params.WorkflowTemplateVersion,
		)
	}
	if err != nil {
		return InitialBindingResult{}, err
	}
	profile, err := videoproduction.ResolveProfileVersion(ctx, tx, template.VideoProfileKey, &template.VideoProfileVersion, true)
	if err != nil {
		return InitialBindingResult{}, err
	}
	videoBindingID := newID()
	commerceBindingID := newID()
	projectGenerationID := newID()
	revision, err := s.repository.NextBindingRevision(ctx, tx, params.ProjectID)
	if err != nil {
		return InitialBindingResult{}, err
	}
	generationNo, err := s.repository.NextProjectGenerationNo(ctx, tx, params.ProjectID)
	if err != nil {
		return InitialBindingResult{}, err
	}
	profileSnapshot, profileHash, err := videoproduction.BuildProfileSnapshot(ctx, tx, videoproduction.InitialBindingParams{
		Identity:            videoproduction.Identity{ProjectID: params.ProjectID, BindingID: videoBindingID, GenerationID: projectGenerationID},
		OrganizationID:      params.OrganizationID,
		CreatedBy:           params.CreatedBy,
		ProfileVersion:      profile,
		CompatibilityPolicy: params.CompatibilityPolicy,
		Overrides:           params.VideoOverrides,
		Configuration:       params.ProductionConfiguration,
	})
	if err != nil {
		return InitialBindingResult{}, err
	}
	configurationHash, err := hashJSON(params.ConfigurationSnapshot)
	if err != nil {
		return InitialBindingResult{}, err
	}
	if err := s.repository.InsertPreparingVideoBinding(ctx, tx, videoBindingID, params, profile.ID, profileSnapshot, profileHash, revision); err != nil {
		return InitialBindingResult{}, err
	}
	if err := s.repository.InsertPreparingCommerceBinding(ctx, tx, commerceBindingID, videoBindingID, template, params, configurationHash, profileHash, revision); err != nil {
		return InitialBindingResult{}, err
	}
	if err := s.repository.InsertPreparingProjectGeneration(ctx, tx, projectGenerationID, videoBindingID, commerceBindingID, params, generationNo); err != nil {
		return InitialBindingResult{}, err
	}
	return InitialBindingResult{
		VideoBindingID:            videoBindingID,
		VideoBindingRevision:      revision,
		VideoProfileVersionID:     profile.ID,
		VideoProfileSnapshot:      profileSnapshot,
		VideoProfileSnapshotHash:  profileHash,
		CommerceBindingID:         commerceBindingID,
		CommerceBindingRevision:   revision,
		CommerceConfigurationHash: configurationHash,
		ProjectGenerationID:       projectGenerationID,
		ProjectGenerationNo:       generationNo,
	}, nil
}

func (s *Service) ActivateInitialBindings(ctx context.Context, tx pgx.Tx, projectID string, result InitialBindingResult) error {
	return s.repository.ActivateInitialBindings(ctx, tx, projectID, result)
}

func (s *Service) CompleteDirectProjectSetup(
	ctx context.Context,
	tx pgx.Tx,
	setupSessionID string,
	projectID string,
	result InitialBindingResult,
) error {
	return s.repository.CompleteDirectProjectSetup(ctx, tx, setupSessionID, projectID, result)
}

func (s *Service) AssertWritableExecution(
	ctx context.Context,
	tx pgx.Tx,
	identity ExecutionIdentity,
) (ProductionContext, error) {
	item, err := s.repository.LockActiveProductionContext(ctx, tx, identity.OrganizationID, identity.ProjectID)
	if err != nil {
		return ProductionContext{}, err
	}
	if item.LifecycleStatus == "deleting" {
		return ProductionContext{}, Error{Code: CodeProjectDeletionInProgress, Message: "项目正在删除，不能继续写入生产数据"}
	}
	if item.ProjectLocked {
		return ProductionContext{}, Error{Code: CodeProjectLocked, Message: "带货视频项目生产配置正在切换", Retryable: true}
	}
	if err := assertExecutionIdentity(item, identity); err != nil {
		return ProductionContext{}, err
	}
	return item, nil
}

func (s *Service) AssertWritableUnitGeneration(
	ctx context.Context,
	tx pgx.Tx,
	identity UnitGenerationIdentity,
) (UnitGenerationContext, error) {
	production, err := s.AssertWritableExecution(ctx, tx, identity.ExecutionIdentity)
	if err != nil {
		return UnitGenerationContext{}, err
	}
	return s.repository.LockUnitGenerationContext(ctx, tx, production, identity)
}

func hashJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:]), nil
}

func validateJSONObject(raw json.RawMessage) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("JSON object is null")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
