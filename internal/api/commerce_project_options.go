package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
)

func (s *Server) getCommerceProjectOptions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if workspaceID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workspaceId 不能为空", nil, false)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectWrite, authz.Resource{WorkspaceID: workspaceID}) {
		return
	}
	var organizationID string
	if err := s.db.QueryRow(r.Context(), `SELECT organization_id::text FROM workspaces WHERE id = $1`, workspaceID).Scan(&organizationID); err != nil {
		s.writeError(w, r, err)
		return
	}
	options, err := s.loadCommerceProjectOptions(r.Context(), organizationID, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, options, nil)
}

func (s *Server) loadCommerceProjectOptions(ctx context.Context, organizationID string, includeProviderReadiness bool) (commercepkg.ProjectOptions, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.ProjectOptions{}, err
	}
	defer tx.Rollback(ctx)
	options, err := s.commerceCatalog.ResolveProjectOptions(ctx, tx, organizationID)
	if err != nil {
		return commercepkg.ProjectOptions{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.ProjectOptions{}, err
	}
	if !includeProviderReadiness || options.WorkflowTemplateVersionID == "" {
		return options, nil
	}
	return s.attachCommerceProviderReadiness(ctx, organizationID, options), nil
}

func (s *Server) attachCommerceProviderReadiness(ctx context.Context, organizationID string, options commercepkg.ProjectOptions) commercepkg.ProjectOptions {
	if s.providers == nil {
		options.Available = false
		options.Blockers = appendUniqueCommerceMessages(options.Blockers, "供应商运行时尚未配置")
		return options
	}
	for index := range options.ModelRequirements {
		requirement := &options.ModelRequirements[index]
		candidates, err := s.providers.ResolveRoutingCandidates(ctx, provider.RoutingRequest{
			OrganizationID: organizationID, ModelProfileKey: requirement.ProfileKey,
			TaskType: requirement.TaskType, Modality: requirement.Modality,
		})
		if err != nil {
			requirement.Ready = false
			requirement.Blocker = commerceProviderBlocker(requirement.Label, err)
			options.Blockers = appendUniqueCommerceMessages(options.Blockers, requirement.Blocker)
			continue
		}
		requirement.Ready = true
		requirement.CandidateCount = len(candidates)
	}

	usableLanguage := false
	for languageIndex := range options.Languages {
		language := &options.Languages[languageIndex]
		textRequirements := 0
		readyTextRequirements := 0
		imageRequirements := 0
		readyImageRequirements := 0
		videoRequirements := 0
		readyVideoRequirements := 0
		nativeAudioRequirements := 0
		readyNativeAudioRequirements := 0
		for _, requirement := range options.ModelRequirements {
			category := "text"
			switch requirement.Modality {
			case "image":
				category = "image"
				imageRequirements++
			case "video":
				category = "video"
				videoRequirements++
			default:
				textRequirements++
			}
			if requirement.Ready {
				switch category {
				case "image":
					readyImageRequirements++
				case "video":
					readyVideoRequirements++
				default:
					readyTextRequirements++
				}
			}
			if requirement.UsesNativeAudio {
				nativeAudioRequirements++
				if requirement.Ready {
					request := provider.RoutingRequest{
						OrganizationID: organizationID, ModelProfileKey: requirement.ProfileKey,
						TaskType: requirement.TaskType, Modality: requirement.Modality,
						NativeAudioLanguage: language.Locale,
					}
					if _, err := s.providers.ResolveRoutingCandidates(ctx, request); err == nil {
						readyNativeAudioRequirements++
					}
				}
			}
		}
		language.TextAvailable = textRequirements > 0 && readyTextRequirements == textRequirements
		language.ImagePromptAvailable = imageRequirements > 0 && readyImageRequirements == imageRequirements
		language.VideoPromptAvailable = videoRequirements > 0 && readyVideoRequirements == videoRequirements
		language.NativeAudioAvailable = nativeAudioRequirements > 0 && readyNativeAudioRequirements == nativeAudioRequirements
		if language.TextAvailable && language.ImagePromptAvailable && language.VideoPromptAvailable {
			usableLanguage = true
		}
	}
	if !usableLanguage {
		options.Blockers = appendUniqueCommerceMessages(options.Blockers, "当前没有可执行的带货视频语言与模型组合")
	}
	options.Available = len(options.Blockers) == 0 && usableLanguage
	return options
}

func commerceProviderBlocker(label string, err error) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "业务模型"
	}
	if standard, ok := provider.StandardErrorFromError(err); ok {
		switch standard.Code {
		case provider.CodeUnsupportedCapability:
			return label + "不支持所需语言或模态"
		}
		if strings.TrimSpace(standard.Message) != "" {
			return label + "：" + strings.TrimSpace(standard.Message)
		}
	}
	if errors.Is(err, provider.ErrValidation) && strings.Contains(err.Error(), provider.CodeModelProfileNotConfigured) {
		return label + "尚未配置可用业务模型"
	}
	return label + "当前不可用"
}

func appendUniqueCommerceMessages(values []string, additional ...string) []string {
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
