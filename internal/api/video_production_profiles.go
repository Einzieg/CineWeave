package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type videoProductionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type videoProductionProfileItem struct {
	videoproduction.ProfileVersion
	Available bool `json:"available"`
}

type videoModelCompatibilityCandidate struct {
	ModelProfileBindingID string                             `json:"modelProfileBindingId"`
	ModelProfileID        string                             `json:"modelProfileId"`
	ModelProfileKey       string                             `json:"modelProfileKey"`
	ModelProfileName      string                             `json:"modelProfileName"`
	ProviderAccountID     string                             `json:"providerAccountId"`
	ProviderAccountName   string                             `json:"providerAccountName"`
	ProviderModelID       string                             `json:"providerModelId"`
	ProviderModelKey      string                             `json:"providerModelKey"`
	ProviderModelName     string                             `json:"providerModelName"`
	Priority              int                                `json:"priority"`
	Weight                int                                `json:"weight"`
	Capability            videoproduction.ModelCapability    `json:"capability"`
	Compatibility         videoproduction.ModelCompatibility `json:"compatibility"`
}

type videoProductionCompatibilityResponse struct {
	Profile             videoProductionProfileItem           `json:"profile"`
	ModelProfileKey     string                               `json:"modelProfileKey"`
	NativeAudioRequired bool                                 `json:"nativeAudioRequired"`
	Compatible          bool                                 `json:"compatible"`
	Executable          bool                                 `json:"executable"`
	Issues              []videoproduction.CompatibilityIssue `json:"issues"`
	Candidates          []videoModelCompatibilityCandidate   `json:"candidates"`
}

func (s *Server) attachVideoProductionContext(ctx context.Context, db videoProductionQueryer, item *Project) error {
	productionContext, err := videoproduction.LoadActiveContext(ctx, db, item.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var activeGenerationID *string
		if stateErr := db.QueryRow(ctx, `
			SELECT active_video_production_generation_id::text,
			       video_production_state,
			       video_production_locked
			FROM projects
			WHERE id = $1
		`, item.ID).Scan(&activeGenerationID, &item.VideoProductionState, &item.VideoProductionLocked); stateErr != nil {
			return stateErr
		}
		if activeGenerationID != nil && strings.TrimSpace(*activeGenerationID) != "" {
			return err
		}
		item.VideoProductionBinding = nil
		item.ProductionGeneration = nil
		return nil
	}
	if err != nil {
		return err
	}
	item.VideoProductionBinding = &productionContext.Binding
	item.ProductionGeneration = &productionContext.Generation
	item.VideoProductionState = productionContext.State
	item.VideoProductionLocked = productionContext.Locked
	return nil
}

func (s *Server) listVideoProductionProfiles(w http.ResponseWriter, r *http.Request, _ auth.Principal) {
	items, err := videoproduction.ListProfiles(r.Context(), s.db)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	result := make([]videoProductionProfileItem, 0, len(items))
	for _, item := range items {
		result = append(result, videoProductionProfileItem{ProfileVersion: item, Available: item.Available()})
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": result}, nil)
}

func (s *Server) getProjectVideoProductionProfile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: project.ID}) {
		return
	}
	if project.VideoProductionBinding == nil || project.ProductionGeneration == nil {
		s.writeVideoProductionError(w, r, videoproduction.NewError(
			videoproduction.CodeGenerationMismatch,
			"项目没有活动的视频生产代",
			false,
		))
		return
	}
	version := project.VideoProductionBinding.ProfileVersion
	profile, err := videoproduction.ResolveProfileVersion(
		r.Context(),
		s.db,
		project.VideoProductionBinding.ProfileKey,
		&version,
		false,
	)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"profile":              videoProductionProfileItem{ProfileVersion: profile, Available: profile.Available()},
		"binding":              project.VideoProductionBinding,
		"productionGeneration": project.ProductionGeneration,
		"state":                project.VideoProductionState,
		"locked":               project.VideoProductionLocked,
	}, nil)
}

func (s *Server) getProjectVideoProductionCompatibility(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: project.ID}) {
		return
	}
	profileKey := strings.TrimSpace(r.URL.Query().Get("targetProfileKey"))
	if profileKey == "" && project.VideoProductionBinding != nil {
		profileKey = project.VideoProductionBinding.ProfileKey
	}
	var version *int
	if raw := strings.TrimSpace(r.URL.Query().Get("targetProfileVersion")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "目标视频生产方案版本必须是正整数", nil, false)
			return
		}
		version = &parsed
	}
	profile, err := videoproduction.ResolveProfileVersion(r.Context(), s.db, profileKey, version, false)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	response, err := s.loadVideoProductionCompatibility(r.Context(), s.db, project, profile)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (s *Server) loadVideoProductionCompatibility(ctx context.Context, db videoProductionQueryer, project Project, profile videoproduction.ProfileVersion) (videoProductionCompatibilityResponse, error) {
	response := videoProductionCompatibilityResponse{
		Profile:             videoProductionProfileItem{ProfileVersion: profile, Available: profile.Available()},
		ModelProfileKey:     project.VideoModelProfileKey,
		NativeAudioRequired: project.AudioStrategy == "native_av" && project.AudioRequirement == "required",
		Issues:              make([]videoproduction.CompatibilityIssue, 0),
		Candidates:          make([]videoModelCompatibilityCandidate, 0),
	}
	if !profile.Available() {
		response.Issues = append(response.Issues, videoproduction.CompatibilityIssue{
			Code:    videoproduction.CodeProfileUnavailable,
			Message: "该视频生产方案暂不可用",
		})
		return response, nil
	}

	rows, err := db.Query(ctx, `
		SELECT binding.id::text, model_profile.id::text, model_profile.profile_key, model_profile.name,
		       account.id::text, account.name, model.id::text, model.model_key, model.display_name,
		       binding.priority, binding.weight,
		       COALESCE(capability.task_types, '[]'::jsonb),
		       COALESCE(capability.provider_options_schema, '{}'::jsonb)
		FROM model_profiles model_profile
		JOIN model_profile_bindings binding ON binding.model_profile_id = model_profile.id
		JOIN provider_models model ON model.id = binding.provider_model_id
		JOIN provider_accounts account ON account.id = model.provider_account_id
		LEFT JOIN LATERAL (
			SELECT item.task_types, item.provider_options_schema
			FROM provider_model_capabilities item
			WHERE item.provider_model_id = model.id
			ORDER BY item.created_at DESC, item.id DESC
			LIMIT 1
		) capability ON true
		WHERE model_profile.organization_id = $1
		  AND model_profile.profile_key = $2
		  AND binding.enabled = true
		  AND model.status = 'active'
		  AND account.status = 'active'
		ORDER BY binding.priority ASC, binding.weight DESC, binding.id
	`, project.OrganizationID, project.VideoModelProfileKey)
	if err != nil {
		return response, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate videoModelCompatibilityCandidate
		if err := rows.Scan(
			&candidate.ModelProfileBindingID,
			&candidate.ModelProfileID,
			&candidate.ModelProfileKey,
			&candidate.ModelProfileName,
			&candidate.ProviderAccountID,
			&candidate.ProviderAccountName,
			&candidate.ProviderModelID,
			&candidate.ProviderModelKey,
			&candidate.ProviderModelName,
			&candidate.Priority,
			&candidate.Weight,
			&candidate.Capability.TaskTypes,
			&candidate.Capability.ProviderOptionsSchema,
		); err != nil {
			return response, err
		}
		candidate.Compatibility = videoproduction.EvaluateModelCompatibility(profile, candidate.Capability, response.NativeAudioRequired)
		if candidate.Compatibility.Compatible {
			response.Compatible = true
		}
		response.Candidates = append(response.Candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return response, err
	}
	if len(response.Candidates) == 0 {
		response.Issues = append(response.Issues, videoproduction.CompatibilityIssue{
			Code:    "VIDEO_MODEL_BINDING_NOT_FOUND",
			Message: "当前业务视频模型没有启用的供应商模型绑定",
		})
	} else if !response.Compatible {
		response.Issues = append(response.Issues, videoproduction.CompatibilityIssue{
			Code:    videoproduction.CodeProfileIncompatible,
			Message: "当前业务视频模型没有满足该生产方案能力合同的模型",
		})
	}
	response.Executable = response.Profile.Available && response.Compatible && !project.VideoProductionLocked
	if project.VideoProductionLocked {
		response.Issues = append(response.Issues, videoproduction.CompatibilityIssue{
			Code:    videoproduction.CodeProjectLocked,
			Message: "项目视频生产配置正在重建",
		})
	}
	return response, nil
}

func (s *Server) writeVideoProductionError(w http.ResponseWriter, r *http.Request, err error) {
	var typed videoproduction.Error
	if !errors.As(err, &typed) {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteError(w, r, videoProductionErrorStatus(typed.Code), typed.Code, typed.Message, nil, typed.Retryable)
}

func videoProductionErrorStatus(code string) int {
	status := http.StatusConflict
	switch code {
	case videoproduction.CodeProfileNotFound:
		status = http.StatusNotFound
	case videoproduction.CodeProfileUnavailable, videoproduction.CodeProfileIncompatible:
		status = http.StatusUnprocessableEntity
	case videoproduction.CodePromptContractIncomplete:
		status = http.StatusInternalServerError
	}
	return status
}
