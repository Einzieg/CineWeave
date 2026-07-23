package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

const (
	commerceProjectCreateScope = "commerce_project_create"
	commerceSetupLifetime      = 30 * 24 * time.Hour
)

type createProjectRequest struct {
	WorkspaceID                   string          `json:"workspaceId"`
	Name                          string          `json:"name"`
	Description                   *string         `json:"description"`
	ProjectKind                   string          `json:"projectKind"`
	ProjectType                   *string         `json:"projectType"`
	ContentType                   *string         `json:"contentType"`
	AspectRatio                   *string         `json:"aspectRatio"`
	VideoRatio                    *string         `json:"videoRatio"`
	ArtStyle                      *string         `json:"artStyle"`
	DirectorManualPromptVersionID *string         `json:"directorManualPromptVersionId"`
	VisualManualPromptVersionID   *string         `json:"visualManualPromptVersionId"`
	ImageModelProfileKey          *string         `json:"imageModelProfileKey"`
	VideoModelProfileKey          *string         `json:"videoModelProfileKey"`
	ScriptModelProfileKey         *string         `json:"scriptModelProfileKey"`
	TTSModelProfileKey            *string         `json:"ttsModelProfileKey"`
	ASRModelProfileKey            *string         `json:"asrModelProfileKey"`
	AudioStrategy                 *string         `json:"audioStrategy"`
	AudioRequirement              *string         `json:"audioRequirement"`
	ImageQuality                  *string         `json:"imageQuality"`
	VideoProductionProfileKey     *string         `json:"videoProductionProfileKey"`
	VideoProductionProfileVersion *int            `json:"videoProductionProfileVersion"`
	CompatibilityPolicy           *string         `json:"compatibilityPolicy"`
	TimelineTimebase              *int64          `json:"timelineTimebase"`
	FPSNumerator                  *int            `json:"fpsNumerator"`
	FPSDenominator                *int            `json:"fpsDenominator"`
	DefaultTargetDurationSeconds  *int            `json:"defaultTargetDurationSeconds"`
	DefaultTargetPlatform         *string         `json:"defaultTargetPlatform"`
	DefaultLanguageMode           *string         `json:"defaultLanguageMode"`
	DefaultTargetLanguage         *string         `json:"defaultTargetLanguage"`
	Settings                      json.RawMessage `json:"settings"`
}

type projectCreateOptions struct {
	Settings         json.RawMessage
	VideoRatio       string
	AspectRatio      *string
	ImageQuality     string
	AudioStrategy    string
	AudioRequirement string
	TimelineTimebase int64
	FPSNumerator     int
	FPSDenominator   int
}

func (s *Server) createCommerceProjectDraft(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	organizationID string,
	req createProjectRequest,
	options projectCreateOptions,
) {
	if invalidCommerceProjectHiddenFields(req) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROJECT_KIND_CONFIGURATION_INVALID", "带货视频不能提交导演手册、视觉手册或底层生产方案字段", nil, false)
		return
	}
	idempotency := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotency == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "创建带货视频项目需要 Idempotency-Key", nil, false)
		return
	}
	duration := 30
	if req.DefaultTargetDurationSeconds != nil {
		duration = *req.DefaultTargetDurationSeconds
	}
	if duration != 15 && duration != 30 && duration != 60 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "默认目标时长只支持 15、30 或 60 秒", nil, false)
		return
	}
	languageMode := normalizedProjectString(req.DefaultLanguageMode, "auto")
	if languageMode != "auto" && languageMode != "explicit" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCE_LANGUAGE_REQUIRED", "语言模式必须为自动判断或明确指定", nil, false)
		return
	}
	var targetLanguage *string
	if req.DefaultTargetLanguage != nil && strings.TrimSpace(*req.DefaultTargetLanguage) != "" {
		value := strings.TrimSpace(*req.DefaultTargetLanguage)
		targetLanguage = &value
	}
	if languageMode == "explicit" && targetLanguage == nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCE_LANGUAGE_REQUIRED", "明确指定语言时必须选择目标语言", nil, false)
		return
	}
	if languageMode == "auto" && targetLanguage != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROJECT_KIND_CONFIGURATION_INVALID", "自动判断语言时不能预设目标语言", nil, false)
		return
	}
	targetPlatform := normalizedProjectString(req.DefaultTargetPlatform, "douyin")
	if strings.TrimSpace(targetPlatform) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "默认目标平台不能为空", nil, false)
		return
	}
	projectOptions, err := s.loadCommerceProjectOptions(r.Context(), organizationID, false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if projectOptions.WorkflowTemplateVersionID == "" {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_WORKFLOW_TEMPLATE_UNAVAILABLE", "当前没有可用的带货视频工作流模板", nil, false)
		return
	}
	if err := projectOptions.ValidateDraftSelection(duration, options.VideoRatio, options.ImageQuality, languageMode, targetLanguage); err != nil {
		s.writeError(w, r, err)
		return
	}
	options.Settings, err = mergeCommerceScriptUnitDefaults(options.Settings, commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: duration,
		TargetPlatform:        targetPlatform,
		LanguageMode:          languageMode,
		TargetLanguage:        targetLanguage,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "项目设置必须为 JSON 对象", nil, false)
		return
	}

	requestHash := idempotencyRequestHash(req)
	inputSnapshot, err := json.Marshal(req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	claim, err := claimIdempotencyTx(r.Context(), tx, organizationID, commerceProjectCreateScope, idempotency, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		var replay Project
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		status := claim.replayStatus
		if status < 200 || status > 299 {
			status = http.StatusOK
		}
		httpx.WriteJSON(w, r, status, replay, map[string]any{"idempotentReplay": true})
		return
	}

	created, err := s.commerce.CreateDraftProject(r.Context(), tx, commercepkg.DraftProjectParams{
		OrganizationID:      organizationID,
		WorkspaceID:         req.WorkspaceID,
		Name:                req.Name,
		Description:         req.Description,
		AspectRatio:         options.AspectRatio,
		VideoRatio:          options.VideoRatio,
		AudioStrategy:       options.AudioStrategy,
		AudioRequirement:    options.AudioRequirement,
		ImageQuality:        options.ImageQuality,
		TimelineTimebase:    options.TimelineTimebase,
		FPSNumerator:        options.FPSNumerator,
		FPSDenominator:      options.FPSDenominator,
		Settings:            options.Settings,
		CreatedBy:           principal.UserID,
		IdempotencyScope:    commerceProjectCreateScope,
		ClientRequestID:     idempotency,
		RequestHash:         requestHash,
		InputSnapshot:       inputSnapshot,
		SetupExpiresAt:      time.Now().Add(commerceSetupLifetime),
		WorkflowTemplateKey: commercepkg.DefaultWorkflowTemplateKey,
	})
	if err != nil {
		if errors.Is(err, commercepkg.ErrWorkflowTemplateUnavailable) {
			httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_WORKFLOW_TEMPLATE_UNAVAILABLE", "当前没有可用的带货视频工作流模板", nil, false)
			return
		}
		s.writeError(w, r, err)
		return
	}

	item, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1`), created.ProjectID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	item.SetupSessionID = &created.SetupSessionID
	item.SetupState = &created.SetupState
	item.WorkflowTemplateVersionID = &created.WorkflowTemplateVersionID
	item.SetupConfigurationHash = &created.SetupConfigurationHash
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusCreated, item); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func invalidCommerceProjectHiddenFields(req createProjectRequest) bool {
	return nonEmptyProjectString(req.ArtStyle) || nonEmptyProjectString(req.DirectorManualPromptVersionID) ||
		nonEmptyProjectString(req.VisualManualPromptVersionID) || nonEmptyProjectString(req.ImageModelProfileKey) ||
		nonEmptyProjectString(req.VideoModelProfileKey) || nonEmptyProjectString(req.ScriptModelProfileKey) ||
		nonEmptyProjectString(req.TTSModelProfileKey) || nonEmptyProjectString(req.ASRModelProfileKey) ||
		nonEmptyProjectString(req.VideoProductionProfileKey) || req.VideoProductionProfileVersion != nil ||
		nonEmptyProjectString(req.CompatibilityPolicy)
}

func nonEmptyProjectString(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func createProjectAccessTx(ctx context.Context, tx pgx.Tx, organizationID, projectID, userID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members(project_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, projectID, userID); err != nil {
		return err
	}
	var roleID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'project_owner' AND scope = 'project'
	`).Scan(&roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_project_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'project', $4, $3)
		ON CONFLICT DO NOTHING
	`, organizationID, roleID, userID, projectID)
	return err
}
