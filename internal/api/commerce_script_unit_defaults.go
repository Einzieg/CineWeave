package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
)

const commerceScriptUnitDefaultsSettingsKey = "commerceScriptUnitDefaults"

func defaultCommerceScriptUnitDefaults() commercepkg.ScriptUnitDefaults {
	return commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: 30,
		TargetPlatform:        "douyin",
		LanguageMode:          "auto",
	}
}

func commerceScriptUnitDefaultsFromSettings(settings json.RawMessage) (commercepkg.ScriptUnitDefaults, error) {
	defaults := defaultCommerceScriptUnitDefaults()
	if len(settings) == 0 || string(settings) == "null" {
		return defaults, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(settings, &document); err != nil {
		return commercepkg.ScriptUnitDefaults{}, err
	}
	value, ok := document[commerceScriptUnitDefaultsSettingsKey]
	if !ok || len(value) == 0 || string(value) == "null" {
		return defaults, nil
	}
	if err := json.Unmarshal(value, &defaults); err != nil {
		return commercepkg.ScriptUnitDefaults{}, err
	}
	return defaults, nil
}

func mergeCommerceScriptUnitDefaults(settings json.RawMessage, defaults commercepkg.ScriptUnitDefaults) (json.RawMessage, error) {
	document := map[string]json.RawMessage{}
	if len(settings) > 0 && string(settings) != "null" {
		if err := json.Unmarshal(settings, &document); err != nil {
			return nil, err
		}
	}
	encodedDefaults, err := json.Marshal(defaults)
	if err != nil {
		return nil, err
	}
	document[commerceScriptUnitDefaultsSettingsKey] = encodedDefaults
	return json.Marshal(document)
}

func (s *Server) updateCommerceScriptUnitDefaults(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision      int64   `json:"expectedRevision"`
		TargetDurationSeconds int     `json:"targetDurationSeconds"`
		TargetPlatform        string  `json:"targetPlatform"`
		LanguageMode          string  `json:"languageMode"`
		TargetLanguage        *string `json:"targetLanguage"`
	}
	if !decode(w, r, &req) {
		return
	}
	raw, err := json.Marshal(req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "commerce.script.defaults.update", raw,
		strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := decodeAgentToolData[Project](result.Data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func normalizedCommerceScriptUnitDefaults(defaults commercepkg.ScriptUnitDefaults) (commercepkg.ScriptUnitDefaults, error) {
	defaults.TargetPlatform = strings.TrimSpace(defaults.TargetPlatform)
	defaults.LanguageMode = strings.ToLower(strings.TrimSpace(defaults.LanguageMode))
	if defaults.TargetDurationSeconds <= 0 || defaults.TargetPlatform == "" {
		return commercepkg.ScriptUnitDefaults{}, commercepkg.Error{Code: commercepkg.CodeSetupIncomplete, Message: "新脚本默认时长和目标平台不能为空"}
	}
	if defaults.LanguageMode != "auto" && defaults.LanguageMode != "explicit" {
		return commercepkg.ScriptUnitDefaults{}, commercepkg.Error{Code: commercepkg.CodeLanguageRequired, Message: "语言模式必须为自动判断或明确指定"}
	}
	if defaults.LanguageMode == "auto" {
		defaults.TargetLanguage = nil
		return defaults, nil
	}
	if defaults.TargetLanguage == nil || strings.TrimSpace(*defaults.TargetLanguage) == "" {
		return commercepkg.ScriptUnitDefaults{}, commercepkg.Error{Code: commercepkg.CodeLanguageRequired, Message: "明确指定语言时必须选择目标语言"}
	}
	locale, err := commercepkg.NormalizeLocale(*defaults.TargetLanguage)
	if err != nil {
		return commercepkg.ScriptUnitDefaults{}, err
	}
	defaults.TargetLanguage = &locale
	return defaults, nil
}

func validateCommerceScriptUnitDefaults(options commercepkg.ProjectOptions, project Project, defaults commercepkg.ScriptUnitDefaults) error {
	if err := options.ValidateDraftSelection(
		defaults.TargetDurationSeconds, project.VideoRatio, project.ImageQuality,
		defaults.LanguageMode, defaults.TargetLanguage,
	); err != nil {
		return err
	}
	if defaults.LanguageMode != "explicit" || defaults.TargetLanguage == nil {
		return nil
	}
	for _, language := range options.Languages {
		if language.Locale != *defaults.TargetLanguage {
			continue
		}
		if !language.TextAvailable || !language.ImagePromptAvailable || !language.VideoPromptAvailable {
			return commercepkg.Error{Code: commercepkg.CodeLanguageUnsupported, Message: "当前模型链路无法执行所选目标语言"}
		}
		return nil
	}
	return commercepkg.Error{Code: commercepkg.CodeLanguageUnsupported, Message: "目标语言不在当前带货视频模板支持范围内"}
}
