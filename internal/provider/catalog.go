package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) ListCatalogEntries(ctx context.Context, organizationID string) ([]CatalogEntry, error) {
	rows, err := s.db.Query(ctx, catalogEntrySelect(`
		WHERE e.enabled = true
		ORDER BY e.is_official DESC, e.category ASC, e.display_name ASC
	`), strings.TrimSpace(organizationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CatalogEntry, 0)
	for rows.Next() {
		item, err := scanCatalogEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetCatalogEntry(ctx context.Context, providerKey, organizationID string) (CatalogEntry, error) {
	row := s.db.QueryRow(ctx, catalogEntrySelect(`
		WHERE e.provider_key = $2
		  AND e.enabled = true
	`), strings.TrimSpace(organizationID), strings.TrimSpace(providerKey))
	item, err := scanCatalogEntry(row)
	if err == pgx.ErrNoRows {
		return CatalogEntry{}, CatalogError{Code: CodeProviderPresetNotFound, Message: "provider preset was not found"}
	}
	return item, err
}

func (s *Service) InstallCatalogEntry(ctx context.Context, organizationID, userID, providerKey string, req InstallCatalogRequest) (InstallCatalogResponse, error) {
	organizationID = strings.TrimSpace(organizationID)
	userID = strings.TrimSpace(userID)
	providerKey = strings.TrimSpace(providerKey)
	if organizationID == "" || userID == "" || providerKey == "" {
		return InstallCatalogResponse{}, fmt.Errorf("%w: organizationId, userId, and providerKey are required", ErrValidation)
	}
	if strings.TrimSpace(req.OrganizationID) != "" && strings.TrimSpace(req.OrganizationID) != organizationID {
		return InstallCatalogResponse{}, fmt.Errorf("%w: organizationId must match request context", ErrValidation)
	}
	entry, err := s.GetCatalogEntry(ctx, providerKey, organizationID)
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	setup := req.Setup
	if setup == nil {
		setup = map[string]any{}
	}
	if missing := missingRequiredSetupFields(entry.SetupSchema, setup); len(missing) > 0 {
		return InstallCatalogResponse{}, CatalogError{Code: CodeProviderSetupFieldMissing, Message: "missing required setup field: " + missing[0]}
	}
	models, err := catalogInstallModels(entry, req.Models)
	if err != nil {
		return InstallCatalogResponse{}, err
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" && entry.DefaultBaseURL != nil {
		baseURL = strings.TrimSpace(*entry.DefaultBaseURL)
	}
	authType := strings.TrimSpace(req.AuthType)
	if authType == "" {
		authType = entry.DefaultAuthType
	}
	if authType == "" {
		authType = "bearer"
	}
	if authType != "none" && strings.TrimSpace(req.APIKey) == "" {
		return InstallCatalogResponse{}, fmt.Errorf("%w: apiKey is required", ErrValidation)
	}
	if len(entry.ConnectorManifest) > 0 && entry.ProviderType == "declarative_manifest" {
		manifest, _, err := ParseManifest(entry.ConnectorManifest, "")
		if err != nil {
			return InstallCatalogResponse{}, CatalogError{Code: CodeProviderManifestInvalid, Message: err.Error()}
		}
		if validation := ValidateManifest(manifest); !validation.Valid {
			return InstallCatalogResponse{}, CatalogError{Code: CodeProviderManifestInvalid, Message: validation.Errors[0].Message}
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	defer tx.Rollback(ctx)

	connector, err := upsertCatalogConnector(ctx, tx, entry)
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	config, err := catalogAccountConfig(entry, req.Config, setup)
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	accountName, err := uniqueCatalogAccountName(ctx, tx, organizationID, firstNonEmpty(req.Name, entry.DisplayName))
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	var accountID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, $7)
		RETURNING id
	`, organizationID, connector.ID, accountName, nullString(baseURL), authType, config, userID).Scan(&accountID); err != nil {
		return InstallCatalogResponse{}, err
	}
	if strings.TrimSpace(req.APIKey) != "" {
		if _, err := s.insertCredential(ctx, tx, organizationID, accountID, userID, "default", "api_key", map[string]any{"apiKey": strings.TrimSpace(req.APIKey)}); err != nil {
			return InstallCatalogResponse{}, err
		}
	}

	modelIDsByKey := map[string]string{}
	for _, model := range models {
		modelID, err := insertCatalogModel(ctx, tx, accountID, model)
		if err != nil {
			return InstallCatalogResponse{}, err
		}
		modelIDsByKey[model.ModelKey] = modelID
	}

	bindings := make([]CatalogProfileBindingResult, 0, len(req.BindProfiles))
	for _, binding := range req.BindProfiles {
		modelID := modelIDsByKey[strings.TrimSpace(binding.ModelKey)]
		if modelID == "" && len(models) == 1 {
			modelID = modelIDsByKey[models[0].ModelKey]
		}
		if modelID == "" {
			return InstallCatalogResponse{}, fmt.Errorf("%w: bindProfiles.modelKey is invalid", ErrValidation)
		}
		profileID, err := ensureCatalogModelProfile(ctx, tx, organizationID, strings.TrimSpace(binding.ProfileKey))
		if err != nil {
			return InstallCatalogResponse{}, err
		}
		priority := 100
		if binding.Priority != nil {
			priority = *binding.Priority
		}
		weight := 100
		if binding.Weight != nil {
			weight = *binding.Weight
		}
		enabled := true
		if binding.Enabled != nil {
			enabled = *binding.Enabled
		}
		var bindingID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (model_profile_id, provider_model_id) DO UPDATE SET
				priority = EXCLUDED.priority,
				weight = EXCLUDED.weight,
				enabled = EXCLUDED.enabled
			RETURNING id
		`, profileID, modelID, priority, weight, enabled).Scan(&bindingID); err != nil {
			return InstallCatalogResponse{}, err
		}
		bindings = append(bindings, CatalogProfileBindingResult{ProfileID: profileID, ProfileKey: binding.ProfileKey, ModelID: modelID, BindingID: bindingID})
	}

	if err := tx.Commit(ctx); err != nil {
		return InstallCatalogResponse{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, accountID)
	if err != nil {
		return InstallCatalogResponse{}, err
	}
	installedModels := make([]Model, 0, len(modelIDsByKey))
	for _, model := range models {
		item, err := s.GetModel(ctx, organizationID, modelIDsByKey[model.ModelKey])
		if err != nil {
			return InstallCatalogResponse{}, err
		}
		installedModels = append(installedModels, item)
	}
	return InstallCatalogResponse{
		ProviderKey: providerKey,
		Connector:   connector,
		Account:     account,
		Models:      installedModels,
		Bindings:    bindings,
	}, nil
}

func catalogEntrySelect(suffix string) string {
	return `
		SELECT
			e.id, e.provider_key, e.name, e.display_name, e.description,
			e.provider_type, e.category, e.logo_key, e.docs_url,
			e.default_base_url, e.default_auth_type, e.connector_manifest,
			e.model_templates, e.supported_task_types, e.setup_schema,
			e.enabled, e.is_official, e.created_at, e.updated_at,
			CASE
				WHEN $1 = '' THEN 0
				ELSE (
					SELECT COUNT(*)
					FROM provider_accounts a
					JOIN provider_connectors c ON c.id = a.connector_id
					WHERE a.organization_id = NULLIF($1, '')::uuid
					  AND c.connector_key = e.provider_key
				)
			END AS installed_count
		FROM provider_catalog_entries e
	` + suffix
}

func scanCatalogEntry(row rowScanner) (CatalogEntry, error) {
	var item CatalogEntry
	var description, logoKey, docsURL, defaultBaseURL sql.NullString
	var connectorManifest, modelTemplates, supportedTaskTypes, setupSchema []byte
	err := row.Scan(
		&item.ID,
		&item.ProviderKey,
		&item.Name,
		&item.DisplayName,
		&description,
		&item.ProviderType,
		&item.Category,
		&logoKey,
		&docsURL,
		&defaultBaseURL,
		&item.DefaultAuthType,
		&connectorManifest,
		&modelTemplates,
		&supportedTaskTypes,
		&setupSchema,
		&item.Enabled,
		&item.IsOfficial,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.InstalledCount,
	)
	item.Description = stringPtr(description)
	item.LogoKey = stringPtr(logoKey)
	item.DocsURL = stringPtr(docsURL)
	item.DefaultBaseURL = stringPtr(defaultBaseURL)
	item.ConnectorManifest = rawOrDefault(connectorManifest, "{}")
	item.ModelTemplates = rawOrDefault(modelTemplates, "[]")
	item.SupportedTaskTypes = rawOrDefault(supportedTaskTypes, "[]")
	item.SetupSchema = rawOrDefault(setupSchema, "{}")
	return item, err
}

func upsertCatalogConnector(ctx context.Context, tx pgx.Tx, entry CatalogEntry) (Connector, error) {
	manifest, err := normalizeJSON(entry.ConnectorManifest, "{}")
	if err != nil {
		return Connector{}, CatalogError{Code: CodeProviderManifestInvalid, Message: "interface configuration is invalid"}
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, $2, 'http', $3, $4, 'v1')
		ON CONFLICT (connector_key) DO UPDATE SET
			name = EXCLUDED.name,
			type = EXCLUDED.type,
			is_official = EXCLUDED.is_official,
			manifest = EXCLUDED.manifest,
			version = EXCLUDED.version
		RETURNING id, connector_key, name, type, is_official, manifest, version, created_at
	`, entry.ProviderKey, entry.DisplayName, entry.IsOfficial, manifest)
	return scanConnector(row)
}

func catalogInstallModels(entry CatalogEntry, requested []CatalogInstallModel) ([]CatalogInstallModel, error) {
	if len(requested) > 0 {
		models := make([]CatalogInstallModel, 0, len(requested))
		for _, model := range requested {
			normalized, err := normalizeCatalogInstallModel(model)
			if err != nil {
				return nil, err
			}
			models = append(models, normalized)
		}
		return models, nil
	}
	var templates []CatalogModelTemplate
	if err := json.Unmarshal(entry.ModelTemplates, &templates); err != nil {
		return nil, CatalogError{Code: CodeProviderModelTemplateInvalid, Message: "model template is invalid"}
	}
	models := make([]CatalogInstallModel, 0, len(templates))
	for _, template := range templates {
		model, err := normalizeCatalogInstallModel(CatalogInstallModel{
			ModelKey:              template.ModelKey,
			DisplayName:           template.DisplayName,
			Modality:              template.Modality,
			TaskTypes:             template.TaskTypes,
			InputLimits:           template.InputLimits,
			OutputLimits:          template.OutputLimits,
			QualityTiers:          template.QualityTiers,
			ProviderOptionsSchema: template.ProviderOptionsSchema,
			PricingPolicy:         template.PricingPolicy,
		})
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, CatalogError{Code: CodeProviderModelTemplateInvalid, Message: "model template is empty"}
	}
	return models, nil
}

func normalizeCatalogInstallModel(model CatalogInstallModel) (CatalogInstallModel, error) {
	model.ModelKey = strings.TrimSpace(model.ModelKey)
	model.DisplayName = strings.TrimSpace(model.DisplayName)
	model.Modality = strings.TrimSpace(model.Modality)
	if model.ModelKey == "" || model.DisplayName == "" || model.Modality == "" {
		return CatalogInstallModel{}, CatalogError{Code: CodeProviderModelTemplateInvalid, Message: "model id, display name, and modality are required"}
	}
	if len(model.TaskTypes) == 0 {
		return CatalogInstallModel{}, CatalogError{Code: CodeProviderModelTemplateInvalid, Message: "model task types are required"}
	}
	if len(model.InputLimits) == 0 {
		model.InputLimits = json.RawMessage(`{}`)
	}
	if len(model.OutputLimits) == 0 {
		model.OutputLimits = json.RawMessage(`{}`)
	}
	if len(model.QualityTiers) == 0 {
		model.QualityTiers = json.RawMessage(`[]`)
	}
	return model, nil
}

func insertCatalogModel(ctx context.Context, tx pgx.Tx, accountID string, model CatalogInstallModel) (string, error) {
	var modelID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (provider_account_id, model_key) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			modality = EXCLUDED.modality,
			status = 'active'
		RETURNING id
	`, accountID, model.ModelKey, model.DisplayName, model.Modality).Scan(&modelID); err != nil {
		return "", err
	}
	taskTypes := mustJSON(model.TaskTypes)
	providerOptionsSchema := model.ProviderOptionsSchema
	if len(providerOptionsSchema) == 0 {
		providerOptionsSchema = json.RawMessage(`{}`)
	}
	pricingPolicy := model.PricingPolicy
	if len(pricingPolicy) == 0 {
		pricingPolicy = json.RawMessage(`{}`)
	}
	if _, err := insertCapability(ctx, tx, modelID, CapabilityInput{
		TaskTypes:             taskTypes,
		InputLimits:           model.InputLimits,
		OutputLimits:          model.OutputLimits,
		QualityTiers:          model.QualityTiers,
		ProviderOptionsSchema: providerOptionsSchema,
		PricingPolicy:         pricingPolicy,
	}); err != nil {
		return "", err
	}
	return modelID, nil
}

func catalogAccountConfig(entry CatalogEntry, raw json.RawMessage, setup map[string]any) (json.RawMessage, error) {
	config := map[string]any{}
	for key, value := range catalogDefaultConfig(entry.SetupSchema) {
		config[key] = value
	}
	if len(raw) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("%w: config must be a JSON object", ErrValidation)
		}
		for key, value := range decoded {
			config[key] = value
		}
	}
	for key, value := range setup {
		config[key] = value
	}
	if entry.ProviderType == "declarative_manifest" {
		config["runtime"] = "declarative_manifest"
	} else {
		config["runtime"] = "openai_compatible"
	}
	return mustJSON(config), nil
}

func catalogDefaultConfig(raw json.RawMessage) map[string]any {
	var decoded struct {
		DefaultConfig map[string]any `json:"defaultConfig"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.DefaultConfig == nil {
		return map[string]any{}
	}
	return decoded.DefaultConfig
}

func missingRequiredSetupFields(raw json.RawMessage, setup map[string]any) []string {
	var decoded struct {
		Fields []struct {
			Key      string `json:"key"`
			Required bool   `json:"required"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	missing := []string{}
	for _, field := range decoded.Fields {
		key := strings.TrimSpace(field.Key)
		if !field.Required || key == "" {
			continue
		}
		value, ok := setup[key]
		if !ok || strings.TrimSpace(fmt.Sprintf("%v", value)) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func uniqueCatalogAccountName(ctx context.Context, tx pgx.Tx, organizationID, baseName string) (string, error) {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "Provider"
	}
	candidate := baseName
	for i := 1; i < 1000; i++ {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM provider_accounts
				WHERE organization_id = $1
				  AND lower(name) = lower($2)
			)
		`, organizationID, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s (%d)", baseName, i+1)
	}
	return "", fmt.Errorf("%w: provider account name could not be made unique", ErrConflict)
}

func ensureCatalogModelProfile(ctx context.Context, tx pgx.Tx, organizationID, profileKey string) (string, error) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return "", fmt.Errorf("%w: profileKey is required", ErrValidation)
	}
	var profileID string
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM model_profiles
		WHERE organization_id = $1 AND profile_key = $2
	`, organizationID, profileKey).Scan(&profileID)
	if err == nil {
		return profileID, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	name, purpose := defaultProfileNameAndPurpose(profileKey)
	if err := tx.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, $3, $4, 'priority_with_fallback', '{"enabled":true,"maxAttempts":2}'::jsonb)
		ON CONFLICT (organization_id, purpose) DO UPDATE SET purpose = EXCLUDED.purpose
		RETURNING id
	`, organizationID, profileKey, name, purpose).Scan(&profileID); err != nil {
		return "", err
	}
	return profileID, nil
}

func defaultProfileNameAndPurpose(profileKey string) (string, string) {
	switch profileKey {
	case "script_agent_default":
		return "脚本生成默认模型", "script"
	case "image_generation_default":
		return "图片生成默认模型", "image"
	case "video_generation_default":
		return "视频生成默认模型", "video"
	default:
		return profileKey, profileKey
	}
}
