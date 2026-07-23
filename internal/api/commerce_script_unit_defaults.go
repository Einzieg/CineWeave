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
	if req.ExpectedRevision <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedRevision 必须为正整数", nil, false)
		return
	}
	defaults, err := normalizedCommerceScriptUnitDefaults(commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: req.TargetDurationSeconds,
		TargetPlatform:        req.TargetPlatform,
		LanguageMode:          req.LanguageMode,
		TargetLanguage:        req.TargetLanguage,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	options, err := s.loadCommerceProjectOptions(r.Context(), project.OrganizationID, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := validateCommerceScriptUnitDefaults(options, project, defaults); err != nil {
		s.writeError(w, r, err)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	locked, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if locked.Revision != req.ExpectedRevision {
		s.writeError(w, r, projectRevisionConflict(locked, req.ExpectedRevision))
		return
	}
	settings, err := mergeCommerceScriptUnitDefaults(locked.Settings, defaults)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, err := tx.Exec(r.Context(), `
		UPDATE projects
		SET settings = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND revision = $3
	`, locked.ID, settings, locked.Revision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if command.RowsAffected() != 1 {
		s.writeError(w, r, projectRevisionConflict(locked, locked.Revision))
		return
	}
	updated, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1`), locked.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, locked.OrganizationID, locked.ID,
		"commerce.project.defaults.updated", "project", locked.ID, mustRawJSON(map[string]any{
			"revision":              updated.Revision,
			"targetDurationSeconds": defaults.TargetDurationSeconds,
			"targetPlatform":        defaults.TargetPlatform,
			"languageMode":          defaults.LanguageMode,
			"targetLanguage":        defaults.TargetLanguage,
		})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.attachVideoProductionContext(r.Context(), tx, &updated); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
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
