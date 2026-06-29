package provider

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db                  *pgxpool.Pool
	vault               *Vault
	gatewayURL          string
	gatewayToken        string
	gatewayRuntime      bool
	allowDirectFallback bool
	httpClient          *http.Client
	objectStorage       ObjectStorage
}

type rowScanner interface {
	Scan(dest ...any) error
}

func NewService(db *pgxpool.Pool, vault *Vault) *Service {
	env := strings.TrimSpace(os.Getenv("CINEWEAVE_ENV"))
	return &Service{
		db:                  db,
		vault:               vault,
		allowDirectFallback: providerDirectFallbackAllowed(os.Getenv("CINEWEAVE_ALLOW_PROVIDER_DIRECT_FALLBACK"), env),
		httpClient:          &http.Client{Timeout: 2 * time.Minute},
	}
}

func (s *Service) SetGateway(baseURL, token string) {
	s.gatewayURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	s.gatewayToken = strings.TrimSpace(token)
}

func (s *Service) EnableGatewayRuntime() {
	s.gatewayRuntime = true
}

func (s *Service) ListConnectors(ctx context.Context) ([]Connector, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, connector_key, name, type, is_official, manifest, version, created_at
		FROM provider_connectors
		ORDER BY is_official DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Connector, 0)
	for rows.Next() {
		item, err := scanConnector(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ImportConnector(ctx context.Context, req ImportConnectorRequest) (Connector, error) {
	connectorKey := strings.TrimSpace(req.ConnectorKey)
	name := strings.TrimSpace(req.Name)
	connectorType := strings.TrimSpace(req.Type)
	manifest := req.Manifest
	if len(req.Manifest) > 0 || strings.TrimSpace(req.ManifestText) != "" {
		parsed, manifestJSON, err := ParseManifest(req.Manifest, req.ManifestText)
		if err != nil {
			return Connector{}, err
		}
		validation := ValidateManifest(parsed)
		if !validation.Valid {
			return Connector{}, fmt.Errorf("%w: manifest validation failed: %s", ErrValidation, validation.Errors[0].Message)
		}
		connectorKey = parsed.ID
		name = parsed.Name
		connectorType = parsed.Transport
		manifest = manifestJSON
		if req.Version == "" {
			req.Version = parsed.Version
		}
	}
	if connectorKey == "" || name == "" || connectorType == "" {
		return Connector{}, fmt.Errorf("%w: connectorKey, name, and type are required", ErrValidation)
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "v1"
	}
	manifest, err := normalizeJSON(manifest, "{}")
	if err != nil {
		return Connector{}, fmt.Errorf("%w: manifest must be valid JSON", ErrValidation)
	}
	row := s.db.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (connector_key) DO UPDATE SET
			name = EXCLUDED.name,
			type = EXCLUDED.type,
			is_official = EXCLUDED.is_official,
			manifest = EXCLUDED.manifest,
			version = EXCLUDED.version
		RETURNING id, connector_key, name, type, is_official, manifest, version, created_at
	`, connectorKey, name, connectorType, req.IsOfficial, manifest, version)
	return scanConnector(row)
}

func (s *Service) ValidateManifest(req ValidateManifestRequest) (ManifestValidationResult, error) {
	manifest, _, err := ParseManifest(req.Manifest, req.ManifestText)
	if err != nil {
		return ManifestValidationResult{
			Valid: false,
			Errors: []ManifestValidationIssue{{
				Path:    "$",
				Message: err.Error(),
			}},
		}, nil
	}
	return ValidateManifest(manifest), nil
}

func (s *Service) ListAccounts(ctx context.Context, organizationID, status string, limit int) ([]Account, error) {
	limit = normalizeLimit(limit, 20, 100)
	status = strings.TrimSpace(status)
	rows, err := s.db.Query(ctx, accountSelect(`
		WHERE a.organization_id = $1
		  AND ($2 = '' OR a.status = $2)
		ORDER BY a.created_at DESC
		LIMIT $3
	`), organizationID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Account, 0)
	for rows.Next() {
		item, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateAccount(ctx context.Context, organizationID, userID string, req CreateAccountRequest) (Account, error) {
	connectorKey := strings.TrimSpace(req.ConnectorKey)
	name := strings.TrimSpace(req.Name)
	if organizationID == "" || connectorKey == "" || name == "" {
		return Account{}, fmt.Errorf("%w: organizationId, connectorKey, and name are required", ErrValidation)
	}
	authType := strings.TrimSpace(req.AuthType)
	if authType == "" {
		authType = "bearer"
	}
	baseURL, err := normalizeBaseURL(req.BaseURL)
	if err != nil {
		return Account{}, err
	}
	config, err := normalizeJSON(req.Config, "{}")
	if err != nil {
		return Account{}, fmt.Errorf("%w: config must be valid JSON", ErrValidation)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx)

	var connectorID string
	if err := tx.QueryRow(ctx, `SELECT id FROM provider_connectors WHERE connector_key = $1`, connectorKey).Scan(&connectorID); err != nil {
		return Account{}, err
	}

	var accountID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, $7)
		RETURNING id
	`, organizationID, connectorID, name, nullStringValue(baseURL), authType, config, userID).Scan(&accountID); err != nil {
		return Account{}, err
	}

	if len(req.Credential) > 0 {
		if _, err := s.insertCredential(ctx, tx, organizationID, accountID, userID, "default", "api_key", req.Credential); err != nil {
			return Account{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, organizationID, accountID)
}

func (s *Service) GetAccount(ctx context.Context, organizationID, accountID string) (Account, error) {
	row := s.db.QueryRow(ctx, accountSelect(`WHERE a.organization_id = $1 AND a.id = $2`), organizationID, accountID)
	return scanAccount(row)
}

func (s *Service) UpdateAccount(ctx context.Context, organizationID, accountID string, req UpdateAccountRequest) (Account, error) {
	current, err := s.GetAccount(ctx, organizationID, accountID)
	if err != nil {
		return Account{}, err
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" {
		return Account{}, fmt.Errorf("%w: name is required", ErrValidation)
	}
	authType := current.AuthType
	if req.AuthType != nil {
		authType = strings.TrimSpace(*req.AuthType)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	baseURL := sql.NullString{}
	if current.BaseURL != nil {
		baseURL = sql.NullString{String: *current.BaseURL, Valid: true}
	}
	if req.BaseURL != nil {
		baseURL, err = normalizeBaseURL(*req.BaseURL)
		if err != nil {
			return Account{}, err
		}
	}
	config := current.Config
	if len(req.Config) > 0 {
		config, err = normalizeJSON(req.Config, "{}")
		if err != nil {
			return Account{}, fmt.Errorf("%w: config must be valid JSON", ErrValidation)
		}
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE provider_accounts
		SET name = $3, base_url = $4, auth_type = $5, status = $6, config = $7
		WHERE organization_id = $1 AND id = $2
	`, organizationID, accountID, name, nullStringValue(baseURL), authType, status, config); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, organizationID, accountID)
}

func (s *Service) DeleteAccount(ctx context.Context, organizationID, accountID string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE provider_accounts
		SET status = 'disabled'
		WHERE organization_id = $1 AND id = $2
	`, organizationID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) RotateCredential(ctx context.Context, organizationID, accountID, userID string, req RotateCredentialRequest) (Account, error) {
	if len(req.Credential) == 0 {
		return Account{}, fmt.Errorf("%w: credential is required", ErrValidation)
	}
	credentialKey := strings.TrimSpace(req.CredentialKey)
	if credentialKey == "" {
		credentialKey = "default"
	}
	if _, err := s.GetAccount(ctx, organizationID, accountID); err != nil {
		return Account{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE provider_credentials
		SET is_active = false, status = 'rotated', rotated_at = now()
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND credential_key = $3
		  AND is_active = true
	`, organizationID, accountID, credentialKey); err != nil {
		return Account{}, err
	}
	if _, err := s.insertCredential(ctx, tx, organizationID, accountID, userID, credentialKey, "api_key", req.Credential); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, organizationID, accountID)
}

func (s *Service) ListModels(ctx context.Context, organizationID, accountID string) ([]Model, error) {
	if _, err := s.GetAccount(ctx, organizationID, accountID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, provider_account_id, model_key, display_name, modality, status, created_at, updated_at
		FROM provider_models
		WHERE provider_account_id = $1
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Model, 0)
	for rows.Next() {
		item, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		item.Capabilities, err = s.listCapabilities(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateModel(ctx context.Context, organizationID, accountID string, req CreateModelRequest) (Model, error) {
	if _, err := s.GetAccount(ctx, organizationID, accountID); err != nil {
		return Model{}, err
	}
	modelKey := strings.TrimSpace(req.ModelKey)
	displayName := strings.TrimSpace(req.DisplayName)
	modality := strings.TrimSpace(req.Modality)
	if modelKey == "" || displayName == "" || modality == "" {
		return Model{}, fmt.Errorf("%w: modelKey, displayName, and modality are required", ErrValidation)
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback(ctx)

	var modelID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, accountID, modelKey, displayName, modality, status).Scan(&modelID); err != nil {
		return Model{}, err
	}
	if req.Capabilities != nil {
		if _, err := insertCapability(ctx, tx, modelID, *req.Capabilities); err != nil {
			return Model{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	return s.GetModel(ctx, organizationID, modelID)
}

func (s *Service) GetModel(ctx context.Context, organizationID, modelID string) (Model, error) {
	row := s.db.QueryRow(ctx, `
		SELECT m.id, m.provider_account_id, m.model_key, m.display_name, m.modality, m.status, m.created_at, m.updated_at
		FROM provider_models m
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE a.organization_id = $1 AND m.id = $2
	`, organizationID, modelID)
	item, err := scanModel(row)
	if err != nil {
		return Model{}, err
	}
	item.Capabilities, err = s.listCapabilities(ctx, item.ID)
	if err != nil {
		return Model{}, err
	}
	return item, nil
}

func (s *Service) UpdateModel(ctx context.Context, organizationID, modelID string, req UpdateModelRequest) (Model, error) {
	current, err := s.GetModel(ctx, organizationID, modelID)
	if err != nil {
		return Model{}, err
	}
	modelKey := current.ModelKey
	displayName := current.DisplayName
	modality := current.Modality
	status := current.Status
	if req.ModelKey != nil {
		modelKey = strings.TrimSpace(*req.ModelKey)
	}
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Modality != nil {
		modality = strings.TrimSpace(*req.Modality)
	}
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if modelKey == "" || displayName == "" || modality == "" {
		return Model{}, fmt.Errorf("%w: modelKey, displayName, and modality are required", ErrValidation)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE provider_models
		SET model_key = $2, display_name = $3, modality = $4, status = $5
		WHERE id = $1
	`, modelID, modelKey, displayName, modality, status); err != nil {
		return Model{}, err
	}
	if req.Capabilities != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM provider_model_capabilities WHERE provider_model_id = $1`, modelID); err != nil {
			return Model{}, err
		}
		if _, err := insertCapability(ctx, tx, modelID, *req.Capabilities); err != nil {
			return Model{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	return s.GetModel(ctx, organizationID, modelID)
}

func (s *Service) ListModelProfiles(ctx context.Context, organizationID string) ([]ModelProfile, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy, created_at, updated_at
		FROM model_profiles
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ModelProfile, 0)
	for rows.Next() {
		item, err := scanModelProfile(rows)
		if err != nil {
			return nil, err
		}
		item.Bindings, err = s.listModelProfileBindings(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateModelProfile(ctx context.Context, organizationID string, req CreateModelProfileRequest) (ModelProfile, error) {
	profileKey := strings.TrimSpace(req.ProfileKey)
	name := strings.TrimSpace(req.Name)
	purpose := strings.TrimSpace(req.Purpose)
	if profileKey == "" || name == "" || purpose == "" {
		return ModelProfile{}, fmt.Errorf("%w: profileKey, name, and purpose are required", ErrValidation)
	}
	routingStrategy := strings.TrimSpace(req.RoutingStrategy)
	if routingStrategy == "" {
		routingStrategy = "priority"
	}
	fallbackStrategy, err := normalizeJSON(req.FallbackStrategy, "{}")
	if err != nil {
		return ModelProfile{}, fmt.Errorf("%w: fallbackStrategy must be valid JSON", ErrValidation)
	}

	var profileID string
	if err := s.db.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, organizationID, profileKey, name, purpose, routingStrategy, fallbackStrategy).Scan(&profileID); err != nil {
		return ModelProfile{}, err
	}
	return s.GetModelProfile(ctx, organizationID, profileID)
}

func (s *Service) GetModelProfile(ctx context.Context, organizationID, profileID string) (ModelProfile, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy, created_at, updated_at
		FROM model_profiles
		WHERE organization_id = $1 AND id = $2
	`, organizationID, profileID)
	item, err := scanModelProfile(row)
	if err != nil {
		return ModelProfile{}, err
	}
	item.Bindings, err = s.listModelProfileBindings(ctx, item.ID)
	if err != nil {
		return ModelProfile{}, err
	}
	return item, nil
}

func (s *Service) UpdateModelProfile(ctx context.Context, organizationID, profileID string, req UpdateModelProfileRequest) (ModelProfile, error) {
	current, err := s.GetModelProfile(ctx, organizationID, profileID)
	if err != nil {
		return ModelProfile{}, err
	}
	profileKey := current.ProfileKey
	name := current.Name
	purpose := current.Purpose
	routingStrategy := current.RoutingStrategy
	fallbackStrategy := current.FallbackStrategy
	if req.ProfileKey != nil {
		profileKey = strings.TrimSpace(*req.ProfileKey)
	}
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Purpose != nil {
		purpose = strings.TrimSpace(*req.Purpose)
	}
	if req.RoutingStrategy != nil {
		routingStrategy = strings.TrimSpace(*req.RoutingStrategy)
	}
	if len(req.FallbackStrategy) > 0 {
		fallbackStrategy, err = normalizeJSON(req.FallbackStrategy, "{}")
		if err != nil {
			return ModelProfile{}, fmt.Errorf("%w: fallbackStrategy must be valid JSON", ErrValidation)
		}
	}
	if profileKey == "" || name == "" || purpose == "" {
		return ModelProfile{}, fmt.Errorf("%w: profileKey, name, and purpose are required", ErrValidation)
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE model_profiles
		SET profile_key = $3, name = $4, purpose = $5, routing_strategy = $6, fallback_strategy = $7
		WHERE organization_id = $1 AND id = $2
	`, organizationID, profileID, profileKey, name, purpose, routingStrategy, fallbackStrategy); err != nil {
		return ModelProfile{}, err
	}
	return s.GetModelProfile(ctx, organizationID, profileID)
}

func (s *Service) CreateModelProfileBinding(ctx context.Context, organizationID, profileID string, req CreateModelProfileBindingRequest) (ModelProfile, error) {
	if _, err := s.GetModelProfile(ctx, organizationID, profileID); err != nil {
		return ModelProfile{}, err
	}
	if strings.TrimSpace(req.ProviderModelID) == "" {
		return ModelProfile{}, fmt.Errorf("%w: providerModelId is required", ErrValidation)
	}
	if _, err := s.GetModel(ctx, organizationID, req.ProviderModelID); err != nil {
		return ModelProfile{}, err
	}
	priority := req.Priority
	if priority == 0 {
		priority = 100
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (model_profile_id, provider_model_id) DO UPDATE SET
			priority = EXCLUDED.priority,
			weight = EXCLUDED.weight,
			enabled = EXCLUDED.enabled
	`, profileID, req.ProviderModelID, priority, weight, enabled); err != nil {
		return ModelProfile{}, err
	}
	return s.GetModelProfile(ctx, organizationID, profileID)
}

func (s *Service) DeleteModelProfileBinding(ctx context.Context, organizationID, profileID, bindingID string) error {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM model_profile_bindings b
		USING model_profiles p
		WHERE b.model_profile_id = p.id
		  AND p.organization_id = $1
		  AND p.id = $2
		  AND b.id = $3
	`, organizationID, profileID, bindingID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) DiscoverModels(ctx context.Context, organizationID, accountID string) (ModelDiscoveryResult, error) {
	if s.gatewayConfigured() {
		var response GatewayDiscoverModelsResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/models/discover", GatewayDiscoverModelsRequest{
			OrganizationID: organizationID,
			AccountID:      accountID,
		}, &response); err != nil {
			return ModelDiscoveryResult{}, err
		}
		if response.Status == "failed" {
			return ModelDiscoveryResult{}, errorFromGatewayStandard(response.Error)
		}
		return ModelDiscoveryResult{
			Models:      response.Models,
			Unsupported: response.Unsupported,
		}, nil
	}
	if err := s.requireGatewayOrDirectFallback(); err != nil {
		return ModelDiscoveryResult{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, accountID)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	credential, _, err := s.activeCredentialPayload(ctx, organizationID, accountID)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	cfg := parseOpenAICompatibleConfig(account.Config)
	client := newOpenAICompatibleClient(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	return client.discoverModels(ctx, account, apiKey, cfg)
}

func (s *Service) RecordProviderModelTest(ctx context.Context, organizationID, userID, modelID string, req TestProviderModelRequest) (ProviderTestResult, error) {
	testType := strings.TrimSpace(req.TestType)
	if testType == "" {
		testType = "connection_test"
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return ProviderTestResult{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}

	if s.gatewayConfigured() {
		return s.recordProviderModelTestViaGateway(ctx, organizationID, userID, modelID, testType, input, req)
	}
	if err := s.requireGatewayOrDirectFallback(); err != nil {
		return ProviderTestResult{}, err
	}

	model, err := s.GetModel(ctx, organizationID, modelID)
	if err != nil {
		return ProviderTestResult{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return ProviderTestResult{}, err
	}
	credential, credentialID, err := s.activeCredentialPayload(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return ProviderTestResult{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return ProviderTestResult{}, err
	}

	cfg := parseOpenAICompatibleConfig(account.Config)
	client := newOpenAICompatibleClient(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	normalizedOutput := json.RawMessage(`null`)
	responseSnapshot := json.RawMessage(`null`)
	requestSnapshot := input
	status := "succeeded"
	var latencyMS int
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string

	switch testType {
	case "connection_test", "auth_test", "model_discovery_test":
		started := time.Now()
		discovery, err := client.discoverModels(ctx, account, apiKey, cfg)
		latencyMS = int(time.Since(started).Milliseconds())
		if err != nil {
			status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(err)
			responseSnapshot = upstreamBody(err)
		} else {
			normalizedOutput = mustJSON(map[string]any{"models": discovery.Models, "unsupported": discovery.Unsupported})
			responseSnapshot = normalizedOutput
			requestSnapshot = mustJSON(map[string]any{"method": "GET", "endpoint": cfg.ModelsEndpoint})
		}
	case "text_generation_test":
		result, err := client.chatCompletion(ctx, account, model, apiKey, cfg, input)
		latencyMS = result.LatencyMS
		requestSnapshot = result.RequestSnapshot
		responseSnapshot = result.ResponseSnapshot
		if err != nil {
			status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(err)
			if len(responseSnapshot) == 0 {
				responseSnapshot = upstreamBody(err)
			}
		} else {
			normalizedOutput = result.NormalizedOutput
		}
	case "streaming_test":
		status = "failed"
		errorCode = CodeUnsupportedCapability
		errorMessage = "streaming test is not implemented in this phase"
		normalizedOutput = mustJSON(map[string]any{"status": "failed", "code": errorCode})
	case "image_generation_test":
		return ProviderTestResult{}, fmt.Errorf("%w: configure PROVIDER_GATEWAY_URL for image_generation_test", ErrProviderGatewayRequired)
	case "video_generation_test":
		return ProviderTestResult{}, fmt.Errorf("%w: configure PROVIDER_GATEWAY_URL for video_generation_test", ErrProviderGatewayRequired)
	default:
		return ProviderTestResult{}, fmt.Errorf("%w: unsupported testType", ErrValidation)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ProviderTestResult{}, err
	}
	defer tx.Rollback(ctx)

	var testRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_test_runs(
			organization_id, provider_account_id, provider_model_id, test_type, status,
			request_snapshot, response_snapshot, normalized_output, error_code, error_message, latency_ms, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`,
		organizationID,
		model.ProviderAccountID,
		model.ID,
		testType,
		status,
		mustSanitize(requestSnapshot, "{}"),
		nullIfJSONNull(mustSanitize(responseSnapshot, "null")),
		nullIfJSONNull(normalizedOutput),
		nullString(errorCode),
		nullString(errorMessage),
		latencyMS,
		userID,
	).Scan(&testRunID); err != nil {
		return ProviderTestResult{}, err
	}

	call, err := recordCall(ctx, tx, RecordCallRequest{
		OrganizationID:    organizationID,
		ProviderAccountID: model.ProviderAccountID,
		ProviderModelID:   model.ID,
		CredentialID:      credentialID,
		IdempotencyKey:    req.IdempotencyKey,
		TaskType:          testType,
		ExecutionMode:     "sync",
		Status:            status,
		LatencyMS:         &latencyMS,
		ErrorCode:         errorCode,
		ErrorMessage:      errorMessage,
		UpstreamStatus:    upstreamStatus,
		UpstreamErrorCode: upstreamErrorCode,
		RequestSnapshot:   requestSnapshot,
		ResponseSnapshot:  responseSnapshot,
		NormalizedOutput:  normalizedOutput,
	})
	if err != nil {
		return ProviderTestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProviderTestResult{}, err
	}
	return ProviderTestResult{
		TestRunID:        testRunID,
		ProviderCallID:   call.ID,
		Status:           status,
		LatencyMS:        latencyMS,
		ErrorCode:        stringPtr(sql.NullString{String: errorCode, Valid: errorCode != ""}),
		ErrorMessage:     stringPtr(sql.NullString{String: errorMessage, Valid: errorMessage != ""}),
		NormalizedOutput: normalizedOutput,
	}, nil
}

func (s *Service) recordProviderModelTestViaGateway(ctx context.Context, organizationID, userID, modelID, testType string, input json.RawMessage, req TestProviderModelRequest) (ProviderTestResult, error) {
	model, err := s.GetModel(ctx, organizationID, modelID)
	if err != nil {
		return ProviderTestResult{}, err
	}

	status := "succeeded"
	latencyMS := 0
	var providerCallID string
	var errorCode, errorMessage string
	var normalizedOutput json.RawMessage
	var requestSnapshot json.RawMessage
	var responseSnapshot json.RawMessage

	switch testType {
	case "connection_test", "auth_test", "model_discovery_test":
		gatewayReq := GatewayDiscoverModelsRequest{
			OrganizationID: organizationID,
			AccountID:      model.ProviderAccountID,
			TestType:       testType,
			IdempotencyKey: req.IdempotencyKey,
		}
		var gatewayResp GatewayDiscoverModelsResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/models/discover", gatewayReq, &gatewayResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = gatewayResp.ProviderCallID
		status = gatewayResp.Status
		latencyMS = gatewayResp.LatencyMS
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if status == "failed" {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(map[string]any{"models": gatewayResp.Models, "unsupported": gatewayResp.Unsupported})
		}
	case "text_generation_test":
		gatewayReq := GatewayTextRequest{
			OrganizationID:  organizationID,
			ProviderModelID: modelID,
			IdempotencyKey:  req.IdempotencyKey,
			Input:           input,
		}
		var gatewayResp GatewayTextResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/text/generate", gatewayReq, &gatewayResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = gatewayResp.ProviderCallID
		status = gatewayResp.Status
		latencyMS = gatewayResp.LatencyMS
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if status == "failed" {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "streaming_test":
		gatewayReq := GatewayTextRequest{
			OrganizationID:  organizationID,
			ProviderModelID: modelID,
			IdempotencyKey:  req.IdempotencyKey,
			Input:           input,
		}
		gatewayResp, err := s.postGatewayStream(ctx, gatewayReq)
		if err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = gatewayResp.ProviderCallID
		status = gatewayResp.Status
		latencyMS = gatewayResp.LatencyMS
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if status == "failed" {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "image_generation_test":
		gatewayReq := GatewayImageRequest{
			OrganizationID:  organizationID,
			ProjectID:       stringFieldFromJSON(input, "projectId"),
			WorkflowRunID:   stringFieldFromJSON(input, "workflowRunId"),
			NodeRunID:       stringFieldFromJSON(input, "nodeRunId"),
			ProviderModelID: modelID,
			IdempotencyKey:  req.IdempotencyKey,
			Input:           input,
		}
		var gatewayResp GatewayImageResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/image/generate", gatewayReq, &gatewayResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = gatewayResp.ProviderCallID
		status = gatewayResp.Status
		latencyMS = gatewayResp.LatencyMS
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if status == "failed" {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "video_generation_test":
		createReq := GatewayVideoCreateTaskRequest{
			OrganizationID:  organizationID,
			ProjectID:       stringFieldFromJSON(input, "projectId"),
			WorkflowRunID:   stringFieldFromJSON(input, "workflowRunId"),
			NodeRunID:       stringFieldFromJSON(input, "nodeRunId"),
			ProviderModelID: modelID,
			IdempotencyKey:  req.IdempotencyKey,
			Input:           input,
		}
		var createResp GatewayVideoCreateTaskResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/video/create-task", createReq, &createResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = createResp.ProviderCallID
		status = createResp.Status
		latencyMS = createResp.LatencyMS
		requestSnapshot = mustJSON(createReq)
		responseSnapshot = mustJSON(createResp)
		normalizedOutput = mustJSON(map[string]any{
			"providerAsyncTaskId": createResp.ProviderAsyncTaskID,
			"externalTaskId":      createResp.ExternalTaskID,
			"status":              createResp.Status,
		})
		if status == "failed" {
			errorCode, errorMessage = gatewayErrorFields(createResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode, "providerAsyncTaskId": createResp.ProviderAsyncTaskID})
			break
		}
		maxPolls := intFieldFromJSON(input, "maxPolls")
		if maxPolls <= 0 {
			maxPolls = createReq.Options.MaxPolls
		}
		if maxPolls <= 0 {
			maxPolls = 5
		}
		for attempt := 0; attempt < maxPolls; attempt++ {
			pollReq := GatewayVideoPollTaskRequest{
				OrganizationID:      organizationID,
				ProviderAsyncTaskID: createResp.ProviderAsyncTaskID,
				ProjectID:           createReq.ProjectID,
				WorkflowRunID:       createReq.WorkflowRunID,
				NodeRunID:           createReq.NodeRunID,
			}
			var pollResp GatewayVideoPollTaskResponse
			if err := s.postGatewayJSON(ctx, "/internal/provider/video/poll-task", pollReq, &pollResp); err != nil {
				return ProviderTestResult{}, err
			}
			providerCallID = pollResp.ProviderCallID
			status = pollResp.Status
			latencyMS += pollResp.LatencyMS
			responseSnapshot = mustJSON(pollResp)
			if status == "failed" {
				errorCode, errorMessage = gatewayErrorFields(pollResp.Error)
				normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode, "providerAsyncTaskId": pollResp.ProviderAsyncTaskID})
				break
			}
			normalizedOutput = mustJSON(map[string]any{
				"providerAsyncTaskId": pollResp.ProviderAsyncTaskID,
				"externalTaskId":      pollResp.ExternalTaskID,
				"status":              pollResp.Status,
				"artifactId":          pollResp.Output.ArtifactID,
				"mediaFileId":         pollResp.Output.MediaFileID,
				"storageKey":          pollResp.Output.StorageKey,
				"mimeType":            pollResp.Output.MimeType,
			})
			if status == "succeeded" || status == "cancelled" {
				break
			}
		}
	default:
		return ProviderTestResult{}, fmt.Errorf("%w: unsupported testType", ErrValidation)
	}
	if normalizedOutput == nil {
		normalizedOutput = json.RawMessage(`null`)
	}
	if responseSnapshot == nil {
		responseSnapshot = json.RawMessage(`null`)
	}

	var testRunID string
	if err := s.db.QueryRow(ctx, `
		INSERT INTO provider_test_runs(
			organization_id, provider_account_id, provider_model_id, test_type, status,
			request_snapshot, response_snapshot, normalized_output, error_code, error_message, latency_ms, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`,
		organizationID,
		model.ProviderAccountID,
		model.ID,
		testType,
		status,
		mustSanitize(requestSnapshot, "{}"),
		nullIfJSONNull(mustSanitize(responseSnapshot, "null")),
		nullIfJSONNull(normalizedOutput),
		nullString(errorCode),
		nullString(errorMessage),
		latencyMS,
		userID,
	).Scan(&testRunID); err != nil {
		return ProviderTestResult{}, err
	}

	return ProviderTestResult{
		TestRunID:        testRunID,
		ProviderCallID:   providerCallID,
		Status:           status,
		LatencyMS:        latencyMS,
		ErrorCode:        stringPtr(sql.NullString{String: errorCode, Valid: errorCode != ""}),
		ErrorMessage:     stringPtr(sql.NullString{String: errorMessage, Valid: errorMessage != ""}),
		NormalizedOutput: normalizedOutput,
	}, nil
}

func (s *Service) RunManifestTest(ctx context.Context, organizationID, userID string, req ManifestTestRunRequest) (ManifestTestRunResult, error) {
	if s.gatewayConfigured() {
		var response ManifestTestRunResult
		if err := s.postGatewayJSON(ctx, "/internal/provider/manifests/test-run", GatewayManifestTestRunRequest{
			OrganizationID: organizationID,
			UserID:         userID,
			Request:        req,
		}, &response); err != nil {
			return ManifestTestRunResult{}, err
		}
		return response, nil
	}
	if err := s.requireGatewayOrDirectFallback(); err != nil {
		return ManifestTestRunResult{}, err
	}
	if strings.TrimSpace(req.AccountID) == "" {
		return ManifestTestRunResult{}, fmt.Errorf("%w: accountId is required", ErrValidation)
	}
	account, err := s.GetAccount(ctx, organizationID, req.AccountID)
	if err != nil {
		return ManifestTestRunResult{}, err
	}
	credential, credentialID, err := s.activeCredentialPayload(ctx, organizationID, account.ID)
	if err != nil {
		return ManifestTestRunResult{}, err
	}

	manifest, err := s.manifestForTestRun(ctx, account, req)
	if err != nil {
		return ManifestTestRunResult{}, err
	}
	validation := ValidateManifest(manifest)
	if !validation.Valid {
		return ManifestTestRunResult{}, fmt.Errorf("%w: manifest validation failed: %s", ErrValidation, validation.Errors[0].Message)
	}

	result, runErr := runDeclarativeManifest(ctx, manifest, account, credential, req)
	status := result.Status
	if status == "" {
		status = "succeeded"
	}
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		if len(result.ResponseSnapshot) == 0 {
			result.ResponseSnapshot = upstreamBody(runErr)
		}
		if len(result.NormalizedOutput) == 0 {
			result.NormalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	}
	if len(result.NormalizedOutput) == 0 {
		result.NormalizedOutput = json.RawMessage(`{}`)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ManifestTestRunResult{}, err
	}
	defer tx.Rollback(ctx)

	var testRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_test_runs(
			organization_id, provider_account_id, test_type, status,
			request_snapshot, response_snapshot, normalized_output,
			error_code, error_message, latency_ms, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`,
		organizationID,
		account.ID,
		"manifest:"+req.EndpointKey,
		status,
		mustSanitize(result.RequestSnapshot, "{}"),
		nullIfJSONNull(mustSanitize(result.ResponseSnapshot, "null")),
		nullIfJSONNull(result.NormalizedOutput),
		nullString(errorCode),
		nullString(errorMessage),
		result.LatencyMS,
		userID,
	).Scan(&testRunID); err != nil {
		return ManifestTestRunResult{}, err
	}

	call, err := recordCall(ctx, tx, RecordCallRequest{
		OrganizationID:    organizationID,
		ProviderAccountID: account.ID,
		CredentialID:      credentialID,
		IdempotencyKey:    req.IdempotencyKey,
		TaskType:          "manifest:" + req.EndpointKey,
		ExecutionMode:     manifestExecutionMode(manifest, req.EndpointKey),
		Status:            status,
		LatencyMS:         &result.LatencyMS,
		ErrorCode:         errorCode,
		ErrorMessage:      errorMessage,
		UpstreamStatus:    upstreamStatus,
		UpstreamErrorCode: upstreamErrorCode,
		RequestSnapshot:   result.RequestSnapshot,
		ResponseSnapshot:  result.ResponseSnapshot,
		NormalizedOutput:  result.NormalizedOutput,
	})
	if err != nil {
		return ManifestTestRunResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManifestTestRunResult{}, err
	}
	return ManifestTestRunResult{
		TestRunID:        testRunID,
		ProviderCallID:   call.ID,
		EndpointKey:      req.EndpointKey,
		Status:           status,
		LatencyMS:        result.LatencyMS,
		ErrorCode:        stringPtr(sql.NullString{String: errorCode, Valid: errorCode != ""}),
		ErrorMessage:     stringPtr(sql.NullString{String: errorMessage, Valid: errorMessage != ""}),
		NormalizedOutput: result.NormalizedOutput,
	}, nil
}

func (s *Service) RecordCall(ctx context.Context, req RecordCallRequest) (CallLog, error) {
	if _, err := s.GetAccount(ctx, req.OrganizationID, req.ProviderAccountID); err != nil {
		return CallLog{}, err
	}
	if req.ProviderModelID != "" {
		if _, err := s.GetModel(ctx, req.OrganizationID, req.ProviderModelID); err != nil {
			return CallLog{}, err
		}
	}
	if req.CredentialID == "" {
		credentialID, err := s.activeCredentialID(ctx, req.OrganizationID, req.ProviderAccountID)
		if err != nil && err != pgx.ErrNoRows {
			return CallLog{}, err
		}
		req.CredentialID = credentialID
	}
	return recordCall(ctx, s.db, req)
}

func (s *Service) ListCallLogs(ctx context.Context, organizationID string, filters CallLogFilters) ([]CallLog, error) {
	limit := normalizeLimit(filters.Limit, 20, 100)
	rows, err := s.db.Query(ctx, `
		SELECT
			id, organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost::text, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			created_at, started_at, completed_at
		FROM provider_call_logs
		WHERE organization_id = $1
		  AND ($2 = '' OR project_id = $2::uuid)
		  AND ($3 = '' OR status = $3)
		ORDER BY created_at DESC
		LIMIT $4
	`, organizationID, strings.TrimSpace(filters.ProjectID), strings.TrimSpace(filters.Status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CallLog, 0)
	for rows.Next() {
		item, err := scanCallLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) UsageSummary(ctx context.Context, organizationID string) (UsageSummary, error) {
	var summary UsageSummary
	var totalCost sql.NullString
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'failed'),
			COALESCE(sum(estimated_cost), 0)::text
		FROM provider_call_logs
		WHERE organization_id = $1
	`, organizationID).Scan(&summary.TotalCalls, &summary.FailedCalls, &totalCost); err != nil {
		return UsageSummary{}, err
	}
	summary.TotalCost = "0"
	if totalCost.Valid {
		summary.TotalCost = totalCost.String
	}
	summary.Currency = "USD"
	return summary, nil
}

func (s *Service) gatewayConfigured() bool {
	return strings.TrimSpace(s.gatewayURL) != ""
}

func (s *Service) requireGatewayOrDirectFallback() error {
	if s.gatewayRuntime || s.gatewayConfigured() || s.allowDirectFallback {
		return nil
	}
	return fmt.Errorf("%w: configure PROVIDER_GATEWAY_URL or explicitly set CINEWEAVE_ALLOW_PROVIDER_DIRECT_FALLBACK=true for development/test", ErrProviderGatewayRequired)
}

func providerDirectFallbackAllowed(raw, env string) bool {
	if !strings.EqualFold(strings.TrimSpace(raw), "true") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "development", "test":
		return true
	default:
		return false
	}
}

func (s *Service) postGatewayJSON(ctx context.Context, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.gatewayURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.gatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.gatewayToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return gatewayHTTPError(resp.StatusCode, responseBody)
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *StandardError  `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("%w: provider gateway response is invalid", ErrValidation)
	}
	if envelope.Error != nil {
		return errorFromGatewayStandard(envelope.Error)
	}
	if len(envelope.Data) == 0 || target == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("%w: provider gateway data is invalid", ErrValidation)
	}
	return nil
}

func (s *Service) postGatewayStream(ctx context.Context, payload GatewayTextRequest) (GatewayTextResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.gatewayURL+"/internal/provider/text/stream", bytes.NewReader(body))
	if err != nil {
		return GatewayTextResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if s.gatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.gatewayToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if readErr != nil {
			return GatewayTextResponse{}, readErr
		}
		return GatewayTextResponse{}, gatewayHTTPError(resp.StatusCode, responseBody)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	event := ""
	var completed GatewayTextResponse
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			event = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch event {
		case "provider.completed":
			if err := json.Unmarshal([]byte(data), &completed); err != nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream completion is invalid", ErrValidation)
			}
		case "provider.error":
			var standard StandardError
			if err := json.Unmarshal([]byte(data), &standard); err != nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream error is invalid", ErrValidation)
			}
			return GatewayTextResponse{}, errorFromGatewayStandard(&standard)
		}
	}
	if err := scanner.Err(); err != nil {
		return GatewayTextResponse{}, err
	}
	if completed.Status == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream did not complete", ErrValidation)
	}
	return completed, nil
}

func gatewayHTTPError(status int, body []byte) error {
	var envelope struct {
		Error *StandardError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error != nil {
		return errorFromGatewayStandard(envelope.Error)
	}
	return &UpstreamError{Status: status, Body: string(body)}
}

func errorFromGatewayStandard(standard *StandardError) error {
	if standard == nil {
		return fmt.Errorf("%w: provider gateway request failed", ErrValidation)
	}
	if standard.UpstreamStatus > 0 {
		return &UpstreamError{
			Status: standard.UpstreamStatus,
			Code:   standard.UpstreamCode,
			Body:   string(mustJSON(standard)),
		}
	}
	return fmt.Errorf("%w: %s", ErrValidation, standard.Message)
}

func gatewayErrorFields(standard *StandardError) (string, string) {
	if standard == nil {
		return CodeUnknownError, "provider gateway request failed"
	}
	return standard.Code, standard.Message
}

func (s *Service) insertCredential(ctx context.Context, tx pgx.Tx, organizationID, accountID, userID, credentialKey, credentialType string, payload map[string]any) (string, error) {
	encrypted, err := s.vault.EncryptJSON(payload)
	if err != nil {
		return "", err
	}
	maskedPreview := MaskCredentialPayload(payload)
	var credentialID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_credentials(
			organization_id, provider_account_id, credential_key, credential_type,
			secret_ref, encrypted_payload, masked_preview, status, is_active, created_by
		)
		VALUES ($1, $2, $3, $4, 'local:aes-gcm:v1', $5, $6, 'active', true, $7)
		RETURNING id
	`, organizationID, accountID, credentialKey, credentialType, encrypted, maskedPreview, nullString(userID)).Scan(&credentialID); err != nil {
		return "", err
	}
	return credentialID, nil
}

func (s *Service) activeCredentialID(ctx context.Context, organizationID, accountID string) (string, error) {
	var credentialID string
	err := s.db.QueryRow(ctx, `
		SELECT id
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND is_active = true
		ORDER BY created_at DESC
		LIMIT 1
	`, organizationID, accountID).Scan(&credentialID)
	return credentialID, err
}

func (s *Service) activeCredentialPayload(ctx context.Context, organizationID, accountID string) (map[string]any, string, error) {
	var credentialID string
	var encrypted []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, encrypted_payload
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND is_active = true
		ORDER BY created_at DESC
		LIMIT 1
	`, organizationID, accountID).Scan(&credentialID, &encrypted)
	if err != nil {
		return nil, "", err
	}
	decrypted, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return nil, "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(decrypted, &payload); err != nil {
		return nil, "", err
	}
	return payload, credentialID, nil
}

func (s *Service) manifestForTestRun(ctx context.Context, account Account, req ManifestTestRunRequest) (ProviderManifest, error) {
	if len(req.Manifest) > 0 || strings.TrimSpace(req.ManifestText) != "" {
		manifest, _, err := ParseManifest(req.Manifest, req.ManifestText)
		return manifest, err
	}
	var raw []byte
	err := s.db.QueryRow(ctx, `
		SELECT c.manifest
		FROM provider_accounts a
		JOIN provider_connectors c ON c.id = a.connector_id
		WHERE a.id = $1
	`, account.ID).Scan(&raw)
	if err != nil {
		return ProviderManifest{}, err
	}
	manifest, _, err := ParseManifest(raw, "")
	return manifest, err
}

func manifestExecutionMode(manifest ProviderManifest, endpointKey string) string {
	endpoint, ok := manifest.Endpoints[endpointKey]
	if !ok {
		return "sync"
	}
	if endpointType(endpoint.EndpointType) == "async_create" {
		return "async"
	}
	return "sync"
}

func scanConnector(row rowScanner) (Connector, error) {
	var item Connector
	var manifest []byte
	err := row.Scan(&item.ID, &item.ConnectorKey, &item.Name, &item.Type, &item.IsOfficial, &manifest, &item.Version, &item.CreatedAt)
	item.Manifest = rawOrDefault(manifest, "{}")
	return item, err
}

func accountSelect(suffix string) string {
	return `
		SELECT
			a.id,
			a.organization_id,
			a.connector_id,
			c.connector_key,
			a.name,
			a.base_url,
			a.auth_type,
			a.status,
			a.config,
			(
				SELECT pc.masked_preview
				FROM provider_credentials pc
				WHERE pc.provider_account_id = a.id
				  AND pc.is_active = true
				ORDER BY pc.created_at DESC
				LIMIT 1
			) AS credential_preview,
			a.created_by,
			a.created_at,
			a.updated_at
		FROM provider_accounts a
		JOIN provider_connectors c ON c.id = a.connector_id
	` + suffix
}

func scanAccount(row rowScanner) (Account, error) {
	var item Account
	var baseURL sql.NullString
	var config []byte
	var preview sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ConnectorID,
		&item.ConnectorKey,
		&item.Name,
		&baseURL,
		&item.AuthType,
		&item.Status,
		&config,
		&preview,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if baseURL.Valid {
		item.BaseURL = &baseURL.String
	}
	if preview.Valid {
		item.CredentialPreview = &preview.String
	}
	item.Config = rawOrDefault(config, "{}")
	return item, err
}

func scanModel(row rowScanner) (Model, error) {
	var item Model
	err := row.Scan(&item.ID, &item.ProviderAccountID, &item.ModelKey, &item.DisplayName, &item.Modality, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	item.Capabilities = []Capability{}
	return item, err
}

func (s *Service) listCapabilities(ctx context.Context, modelID string) ([]Capability, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy, created_at
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
		ORDER BY created_at
	`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Capability, 0)
	for rows.Next() {
		item, err := scanCapability(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCapability(row rowScanner) (Capability, error) {
	var item Capability
	var taskTypes, inputLimits, outputLimits, qualityTiers, providerOptionsSchema, pricingPolicy []byte
	err := row.Scan(
		&item.ID,
		&item.ProviderModelID,
		&taskTypes,
		&inputLimits,
		&outputLimits,
		&qualityTiers,
		&providerOptionsSchema,
		&pricingPolicy,
		&item.CreatedAt,
	)
	item.TaskTypes = rawOrDefault(taskTypes, "[]")
	item.InputLimits = rawOrDefault(inputLimits, "{}")
	item.OutputLimits = rawOrDefault(outputLimits, "{}")
	item.QualityTiers = rawOrDefault(qualityTiers, "[]")
	item.ProviderOptionsSchema = rawOrDefault(providerOptionsSchema, "{}")
	item.PricingPolicy = rawOrDefault(pricingPolicy, "{}")
	return item, err
}

type capabilityWriter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertCapability(ctx context.Context, tx capabilityWriter, modelID string, input CapabilityInput) (string, error) {
	taskTypes, err := normalizeJSON(input.TaskTypes, "[]")
	if err != nil {
		return "", fmt.Errorf("%w: taskTypes must be valid JSON", ErrValidation)
	}
	inputLimits, err := normalizeJSON(input.InputLimits, "{}")
	if err != nil {
		return "", fmt.Errorf("%w: inputLimits must be valid JSON", ErrValidation)
	}
	outputLimits, err := normalizeJSON(input.OutputLimits, "{}")
	if err != nil {
		return "", fmt.Errorf("%w: outputLimits must be valid JSON", ErrValidation)
	}
	qualityTiers, err := normalizeJSON(input.QualityTiers, "[]")
	if err != nil {
		return "", fmt.Errorf("%w: qualityTiers must be valid JSON", ErrValidation)
	}
	providerOptionsSchema, err := normalizeJSON(input.ProviderOptionsSchema, "{}")
	if err != nil {
		return "", fmt.Errorf("%w: providerOptionsSchema must be valid JSON", ErrValidation)
	}
	pricingPolicy, err := normalizeJSON(input.PricingPolicy, "{}")
	if err != nil {
		return "", fmt.Errorf("%w: pricingPolicy must be valid JSON", ErrValidation)
	}
	var capabilityID string
	err = tx.QueryRow(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, modelID, taskTypes, inputLimits, outputLimits, qualityTiers, providerOptionsSchema, pricingPolicy).Scan(&capabilityID)
	return capabilityID, err
}

func scanModelProfile(row rowScanner) (ModelProfile, error) {
	var item ModelProfile
	var fallback []byte
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProfileKey, &item.Name, &item.Purpose, &item.RoutingStrategy, &fallback, &item.CreatedAt, &item.UpdatedAt)
	item.FallbackStrategy = rawOrDefault(fallback, "{}")
	item.Bindings = []ModelProfileBinding{}
	return item, err
}

func (s *Service) listModelProfileBindings(ctx context.Context, profileID string) ([]ModelProfileBinding, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, model_profile_id, provider_model_id, priority, weight, enabled, created_at
		FROM model_profile_bindings
		WHERE model_profile_id = $1
		ORDER BY priority ASC, created_at ASC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ModelProfileBinding, 0)
	for rows.Next() {
		var item ModelProfileBinding
		if err := rows.Scan(&item.ID, &item.ModelProfileID, &item.ProviderModelID, &item.Priority, &item.Weight, &item.Enabled, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type callWriter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func recordCall(ctx context.Context, db callWriter, req RecordCallRequest) (CallLog, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.ProviderAccountID) == "" {
		return CallLog{}, fmt.Errorf("%w: organizationId and providerAccountId are required", ErrValidation)
	}
	taskType := strings.TrimSpace(req.TaskType)
	if taskType == "" {
		return CallLog{}, fmt.Errorf("%w: taskType is required", ErrValidation)
	}
	executionMode := strings.TrimSpace(req.ExecutionMode)
	if executionMode == "" {
		executionMode = "sync"
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "running"
	}
	requestSnapshot, err := SanitizeRawJSON(req.RequestSnapshot, "{}")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: requestSnapshot must be valid JSON", ErrValidation)
	}
	responseSnapshot, err := SanitizeRawJSON(req.ResponseSnapshot, "null")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: responseSnapshot must be valid JSON", ErrValidation)
	}
	normalizedOutput, err := normalizeJSON(req.NormalizedOutput, "null")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: normalizedOutput must be valid JSON", ErrValidation)
	}
	artifactIDs, err := normalizeJSON(req.ArtifactIDs, "[]")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: artifactIds must be valid JSON", ErrValidation)
	}
	mediaFileIDs, err := normalizeJSON(req.MediaFileIDs, "[]")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: mediaFileIds must be valid JSON", ErrValidation)
	}

	row := db.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			id,
			organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			prompt_version_id, prompt_hash,
			idempotency_key, task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			started_at, completed_at
		)
		VALUES (
			COALESCE(NULLIF($31, '')::uuid, gen_random_uuid()),
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10,
			$11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20, $21,
			$22, $23, $24, $25,
			$26, $27, $28, $29, $30,
			CASE WHEN $16 IN ('running', 'succeeded', 'failed', 'skipped') THEN now() ELSE NULL END,
			CASE WHEN $16 IN ('succeeded', 'failed', 'cancelled', 'skipped') THEN now() ELSE NULL END
		)
		RETURNING
			id, organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost::text, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			created_at, started_at, completed_at
	`,
		req.OrganizationID,
		nullString(req.ProjectID),
		nullString(req.WorkflowRunID),
		nullString(req.NodeRunID),
		req.ProviderAccountID,
		nullString(req.ProviderModelID),
		nullString(req.CredentialID),
		nullString(req.ModelProfileID),
		nullString(req.ModelProfileBindingID),
		nullString(req.ModelProfileKey),
		nullString(req.PromptVersionID),
		nullString(req.PromptHash),
		nullString(strings.TrimSpace(req.IdempotencyKey)),
		taskType,
		executionMode,
		status,
		req.LatencyMS,
		nullInt(req.InputTokens),
		nullInt(req.OutputTokens),
		nullString(req.EstimatedCost),
		currencyOrDefault(req.Currency),
		nullString(req.ErrorCode),
		nullString(req.ErrorMessage),
		req.UpstreamStatus,
		nullString(req.UpstreamErrorCode),
		requestSnapshot,
		nullIfJSONNull(responseSnapshot),
		nullIfJSONNull(normalizedOutput),
		artifactIDs,
		mediaFileIDs,
		strings.TrimSpace(req.ID),
	)
	return scanCallLog(row)
}

func scanCallLog(row rowScanner) (CallLog, error) {
	var item CallLog
	var projectID, workflowRunID, nodeRunID, providerModelID, credentialID sql.NullString
	var modelProfileID, modelProfileBindingID, modelProfileKey sql.NullString
	var errorCode, errorMessage, upstreamErrorCode sql.NullString
	var estimatedCost, currency sql.NullString
	var latencyMS, inputTokens, outputTokens, upstreamStatus sql.NullInt64
	var requestSnapshot, responseSnapshot, normalizedOutput, artifactIDs, mediaFileIDs []byte
	var startedAt, completedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&projectID,
		&workflowRunID,
		&nodeRunID,
		&item.ProviderAccountID,
		&providerModelID,
		&credentialID,
		&modelProfileID,
		&modelProfileBindingID,
		&modelProfileKey,
		&item.TaskType,
		&item.ExecutionMode,
		&item.Status,
		&latencyMS,
		&inputTokens,
		&outputTokens,
		&estimatedCost,
		&currency,
		&errorCode,
		&errorMessage,
		&upstreamStatus,
		&upstreamErrorCode,
		&requestSnapshot,
		&responseSnapshot,
		&normalizedOutput,
		&artifactIDs,
		&mediaFileIDs,
		&item.CreatedAt,
		&startedAt,
		&completedAt,
	)
	item.ProjectID = stringPtr(projectID)
	item.WorkflowRunID = stringPtr(workflowRunID)
	item.NodeRunID = stringPtr(nodeRunID)
	item.ProviderModelID = stringPtr(providerModelID)
	item.CredentialID = stringPtr(credentialID)
	item.ModelProfileID = stringPtr(modelProfileID)
	item.ModelProfileBindingID = stringPtr(modelProfileBindingID)
	item.ModelProfileKey = stringPtr(modelProfileKey)
	item.ErrorCode = stringPtr(errorCode)
	item.ErrorMessage = stringPtr(errorMessage)
	item.UpstreamErrorCode = stringPtr(upstreamErrorCode)
	if latencyMS.Valid {
		value := int(latencyMS.Int64)
		item.LatencyMS = &value
	}
	if inputTokens.Valid {
		value := int(inputTokens.Int64)
		item.InputTokens = &value
	}
	if outputTokens.Valid {
		value := int(outputTokens.Int64)
		item.OutputTokens = &value
	}
	item.EstimatedCost = stringPtr(estimatedCost)
	item.Currency = stringPtr(currency)
	if upstreamStatus.Valid {
		value := int(upstreamStatus.Int64)
		item.UpstreamStatus = &value
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	item.RequestSnapshot = rawOrDefault(requestSnapshot, "{}")
	item.ResponseSnapshot = rawOrNil(responseSnapshot)
	item.NormalizedOutput = rawOrNil(normalizedOutput)
	item.ArtifactIDs = rawOrDefault(artifactIDs, "[]")
	item.MediaFileIDs = rawOrDefault(mediaFileIDs, "[]")
	return item, err
}

func normalizedProviderFailure(err error) (status string, code string, message string, upstreamStatus *int, upstreamCode string) {
	status = "failed"
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		standard := NormalizeHTTPError(upstreamErr.Status, upstreamErr.Code)
		statusValue := upstreamErr.Status
		return status, standard.Code, standard.Message, &statusValue, upstreamErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status, CodeUpstreamTimeout, "provider request timed out", nil, ""
	}
	return status, CodeUnknownError, err.Error(), nil, ""
}

func upstreamBody(err error) json.RawMessage {
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) && strings.TrimSpace(upstreamErr.Body) != "" && json.Valid([]byte(upstreamErr.Body)) {
		return json.RawMessage(upstreamErr.Body)
	}
	return json.RawMessage(`null`)
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return raw
}

func mustSanitize(raw json.RawMessage, fallback string) json.RawMessage {
	sanitized, err := SanitizeRawJSON(raw, fallback)
	if err != nil {
		return json.RawMessage(fallback)
	}
	return sanitized
}

func normalizeBaseURL(value string) (sql.NullString, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return sql.NullString{}, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return sql.NullString{}, fmt.Errorf("%w: baseUrl must be an absolute URL", ErrValidation)
	}
	return sql.NullString{String: value, Valid: true}, nil
}

func normalizeJSON(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return json.RawMessage(fallback), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return raw, nil
}

func rawOrDefault(raw []byte, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(raw)
}

func rawOrNil(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(raw)
}

func stringFieldFromJSON(raw json.RawMessage, key string) string {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	value, _ := decoded[key].(string)
	return strings.TrimSpace(value)
}

func intFieldFromJSON(raw json.RawMessage, key string) int {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0
	}
	switch typed := decoded[key].(type) {
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func nullIfJSONNull(raw json.RawMessage) any {
	if string(raw) == "null" {
		return nil
	}
	return raw
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func currencyOrDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "USD"
	}
	return strings.ToUpper(value)
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func normalizeLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

type commandExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
