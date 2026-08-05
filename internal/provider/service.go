package provider

import (
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
	"sort"
	"strconv"
	"strings"
	"time"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/provider/outbound"
	"github.com/Einzieg/cineweave/internal/videoproduction"
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
	mediaFetcher        *outbound.MediaFetcher
	videoMediaProbe     VideoMediaProbeFunc
	videoMediaFileProbe VideoMediaFileProbeFunc
	guard               *ProviderGuard
	billingRouting      editionpkg.BillingRoutingAuthorizer
	billingIdentity     GatewayBillingIdentityResolver
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
		mediaFetcher:        outbound.NewMediaFetcher(outbound.Config{}),
		videoMediaProbe:     defaultVideoMediaProbe,
		videoMediaFileProbe: defaultVideoMediaFileProbe,
		guard:               NewProviderGuard(db),
		billingRouting:      editionpkg.MustCommunityRuntime().BillingRoutingAuthorizer,
		billingIdentity:     passthroughGatewayBillingIdentityResolver{},
	}
}

func (s *Service) SetBillingRoutingAuthorizer(
	authorizer editionpkg.BillingRoutingAuthorizer,
) {
	if authorizer == nil {
		s.billingRouting = editionpkg.MustCommunityRuntime().
			BillingRoutingAuthorizer
		return
	}
	s.billingRouting = authorizer
}

func (s *Service) SetGatewayBillingIdentityResolver(
	resolver GatewayBillingIdentityResolver,
) {
	if resolver == nil {
		s.billingIdentity = passthroughGatewayBillingIdentityResolver{}
		return
	}
	s.billingIdentity = resolver
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
	status, err := normalizeListStatusFilter(status)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, accountSelect(`
		WHERE a.organization_id = $1
		  AND ($2 = 'all' OR a.status = $2)
		  AND NOT EXISTS (
		      SELECT 1
		      FROM provider_managed_accounts managed
		      WHERE managed.provider_account_id = a.id
		        AND managed.management_scope = 'system_managed'
		  )
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
	if _, err := providerMediaRequestPolicy(config); err != nil {
		return Account{}, err
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

func (s *Service) ListCredentials(ctx context.Context, organizationID, accountID, status string) ([]Credential, error) {
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return nil, err
	}
	status, err := normalizeCredentialStatusFilter(status)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, credentialSelect(`
		WHERE pc.organization_id = $1
		  AND pc.provider_account_id = $2
		  AND ($3 = 'all' OR pc.status = $3)
		  AND NOT EXISTS (
		      SELECT 1
		      FROM provider_managed_credentials managed
		      WHERE managed.provider_credential_id = pc.id
		        AND managed.management_scope = 'system_managed'
		  )
		ORDER BY pc.is_active DESC, pc.credential_key, pc.created_at DESC
	`), organizationID, accountID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Credential, 0)
	for rows.Next() {
		item, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetCredential(ctx context.Context, organizationID, accountID, credentialID string) (Credential, error) {
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return Credential{}, err
	}
	row := s.db.QueryRow(ctx, credentialSelect(`
		WHERE pc.organization_id = $1
		  AND pc.provider_account_id = $2
		  AND pc.id = $3
		  AND NOT EXISTS (
		      SELECT 1
		      FROM provider_managed_credentials managed
		      WHERE managed.provider_credential_id = pc.id
		        AND managed.management_scope = 'system_managed'
		  )
	`), organizationID, accountID, credentialID)
	return scanCredential(row)
}

func (s *Service) CreateCredential(ctx context.Context, organizationID, accountID, userID string, req CreateCredentialRequest) (Credential, error) {
	credentialKey := strings.TrimSpace(req.CredentialKey)
	if credentialKey == "" || len(req.Credential) == 0 {
		return Credential{}, fmt.Errorf("%w: credentialKey and credential are required", ErrValidation)
	}
	if len([]rune(credentialKey)) > 120 {
		return Credential{}, fmt.Errorf("%w: credentialKey is too long", ErrValidation)
	}
	credentialType := strings.TrimSpace(req.CredentialType)
	if credentialType == "" {
		credentialType = "api_key"
	}
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return Credential{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Credential{}, err
	}
	defer tx.Rollback(ctx)
	credentialID, err := s.insertCredential(ctx, tx, organizationID, accountID, userID, credentialKey, credentialType, req.Credential)
	if err != nil {
		if isUniqueViolation(err) {
			return Credential{}, fmt.Errorf("%w: an active credential with this key already exists", ErrConflict)
		}
		return Credential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Credential{}, err
	}
	return s.GetCredential(ctx, organizationID, accountID, credentialID)
}

func (s *Service) UpdateAccount(ctx context.Context, organizationID, accountID string, req UpdateAccountRequest) (Account, error) {
	current, err := s.GetTenantAccount(ctx, organizationID, accountID)
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
	if _, err := providerMediaRequestPolicy(config); err != nil {
		return Account{}, err
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
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
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
	if _, err := tx.Exec(ctx, `
		UPDATE provider_models
		SET status = 'disabled'
		WHERE provider_account_id = $1
	`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_call_logs c
		SET model_profile_binding_id = NULL
		FROM model_profile_bindings b
		JOIN provider_models m ON m.id = b.provider_model_id
		WHERE c.model_profile_binding_id = b.id
		  AND m.provider_account_id = $1
	`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks t
		SET model_profile_binding_id = NULL
		FROM model_profile_bindings b
		JOIN provider_models m ON m.id = b.provider_model_id
		WHERE t.model_profile_binding_id = b.id
		  AND m.provider_account_id = $1
	`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM model_profile_bindings b
		USING provider_models m
		WHERE b.provider_model_id = m.id
		  AND m.provider_account_id = $1
	`, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) RotateCredential(ctx context.Context, organizationID, accountID, userID string, req RotateCredentialRequest) (Account, error) {
	if len(req.Credential) == 0 {
		return Account{}, fmt.Errorf("%w: credential is required", ErrValidation)
	}
	credentialKey := strings.TrimSpace(req.CredentialKey)
	if credentialKey == "" {
		credentialKey = "default"
	}
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return Account{}, err
	}
	var credentialID string
	if err := s.db.QueryRow(ctx, `
		SELECT id
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND credential_key = $3
		  AND is_active = true
	`, organizationID, accountID, credentialKey).Scan(&credentialID); err != nil {
		return Account{}, err
	}
	if _, err := s.RotateCredentialByID(ctx, organizationID, accountID, credentialID, userID, req); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, organizationID, accountID)
}

func (s *Service) RotateCredentialByID(ctx context.Context, organizationID, accountID, credentialID, userID string, req RotateCredentialRequest) (Credential, error) {
	if len(req.Credential) == 0 {
		return Credential{}, fmt.Errorf("%w: credential is required", ErrValidation)
	}
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return Credential{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Credential{}, err
	}
	defer tx.Rollback(ctx)

	var credentialKey, credentialType string
	var active bool
	if err := tx.QueryRow(ctx, `
		SELECT credential_key, credential_type, is_active
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND id = $3
		FOR UPDATE
	`, organizationID, accountID, credentialID).Scan(&credentialKey, &credentialType, &active); err != nil {
		return Credential{}, err
	}
	if !active {
		return Credential{}, fmt.Errorf("%w: credential is not active", ErrValidation)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_credentials
		SET is_active = false, status = 'rotated', rotated_at = now()
		WHERE id = $1
	`, credentialID); err != nil {
		return Credential{}, err
	}
	newCredentialID, err := s.insertCredential(ctx, tx, organizationID, accountID, userID, credentialKey, credentialType, req.Credential)
	if err != nil {
		return Credential{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provider_credential_models(
			provider_credential_id, provider_model_id, is_available,
			last_discovered_at, created_at, updated_at
		)
		SELECT $1, provider_model_id, is_available, last_discovered_at, now(), now()
		FROM provider_credential_models
		WHERE provider_credential_id = $2
		ON CONFLICT (provider_credential_id, provider_model_id) DO NOTHING
	`, newCredentialID, credentialID); err != nil {
		return Credential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Credential{}, err
	}
	return s.GetCredential(ctx, organizationID, accountID, newCredentialID)
}

func (s *Service) RevokeCredential(ctx context.Context, organizationID, accountID, credentialID string) error {
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE provider_credentials
		SET is_active = false, status = 'revoked'
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND id = $3
		  AND is_active = true
	`, organizationID, accountID, credentialID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) ListModels(ctx context.Context, organizationID, accountID, status string) ([]Model, error) {
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
		return nil, err
	}
	status, err := normalizeListStatusFilter(status)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, provider_account_id, model_key, display_name, modality, status, created_at, updated_at
		FROM provider_models
		WHERE provider_account_id = $1
		  AND ($2 = 'all' OR status = $2)
		ORDER BY created_at DESC
	`, accountID, status)
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
		if item.Status == "active" {
			item, err = s.reconcileModelCapabilityPreset(ctx, item)
			if err != nil {
				return nil, err
			}
			if err := s.ensureDefaultCapabilityForModel(ctx, s.db, item.ID, item.Modality); err != nil {
				return nil, err
			}
		}
		item.Capabilities, err = s.listCapabilities(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListAvailableModels(ctx context.Context, organizationID string) ([]AvailableModel, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			m.id,
			m.model_key,
			m.display_name,
			m.modality,
			m.status,
			CASE
				WHEN managed.provider_account_id IS NULL THEN 'tenant_managed'
				ELSE managed.management_scope
			END AS management_scope,
			m.created_at,
			m.updated_at
		FROM provider_models m
		JOIN provider_accounts a ON a.id = m.provider_account_id
		LEFT JOIN provider_managed_accounts managed
		  ON managed.provider_account_id = a.id
		WHERE a.organization_id = $1
		  AND a.status = 'active'
		  AND m.status = 'active'
		  AND (
			EXISTS (
				SELECT 1
				FROM provider_credential_models mapping
				JOIN provider_credentials credential
				  ON credential.id = mapping.provider_credential_id
				WHERE mapping.provider_model_id = m.id
				  AND mapping.is_available = true
				  AND credential.organization_id = a.organization_id
				  AND credential.provider_account_id = a.id
				  AND credential.status = 'active'
				  AND credential.is_active = true
			)
			OR (
				NOT EXISTS (
					SELECT 1
					FROM provider_credential_models mapping
					WHERE mapping.provider_model_id = m.id
				)
				AND EXISTS (
					SELECT 1
					FROM provider_credentials credential
					WHERE credential.organization_id = a.organization_id
					  AND credential.provider_account_id = a.id
					  AND credential.status = 'active'
					  AND credential.is_active = true
				)
			)
		  )
		ORDER BY m.modality, m.display_name, m.model_key, m.id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelsByKey := make(map[string]AvailableModel)
	for rows.Next() {
		var item AvailableModel
		if err := rows.Scan(
			&item.ID,
			&item.ModelKey,
			&item.DisplayName,
			&item.Modality,
			&item.Status,
			&item.ManagementScope,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		reconciled, err := s.reconcileModelCapabilityPreset(ctx, Model{
			ID:          item.ID,
			ModelKey:    item.ModelKey,
			DisplayName: item.DisplayName,
			Modality:    item.Modality,
			Status:      item.Status,
		})
		if err != nil {
			return nil, err
		}
		item.DisplayName = reconciled.DisplayName
		item.Modality = reconciled.Modality
		if err := s.ensureDefaultCapabilityForModel(ctx, s.db, item.ID, item.Modality); err != nil {
			return nil, err
		}
		item.Capabilities, err = s.listCapabilities(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		key := normalizeAvailableModelKey(item.ModelKey)
		if current, exists := modelsByKey[key]; !exists || availableModelPreferred(item, current) {
			modelsByKey[key] = item
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]AvailableModel, 0, len(modelsByKey))
	for _, item := range modelsByKey {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Modality != items[j].Modality {
			return items[i].Modality < items[j].Modality
		}
		if items[i].DisplayName != items[j].DisplayName {
			return items[i].DisplayName < items[j].DisplayName
		}
		return normalizeAvailableModelKey(items[i].ModelKey) < normalizeAvailableModelKey(items[j].ModelKey)
	})
	return items, nil
}

func normalizeAvailableModelKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func availableModelPreferred(candidate, current AvailableModel) bool {
	if candidate.ManagementScope != current.ManagementScope {
		return candidate.ManagementScope == "system_managed"
	}
	candidateRank := availableModelCapabilityRank(candidate.Capabilities)
	currentRank := availableModelCapabilityRank(current.Capabilities)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	return candidate.ID < current.ID
}

func availableModelCapabilityRank(capabilities []Capability) int {
	rank := 0
	for _, capability := range capabilities {
		source := capabilityLanguageMetadataFromSchema(capability.ProviderOptionsSchema).Source
		value := 1
		switch source {
		case CapabilitySourceOfficial:
			value = 6
		case CapabilitySourceProvider:
			value = 5
		case CapabilitySourceManual:
			value = 4
		case CapabilitySourcePreset:
			value = 3
		case CapabilitySourceDiscovered:
			value = 2
		}
		if value > rank {
			rank = value
		}
	}
	return rank
}

func (s *Service) CreateModel(ctx context.Context, organizationID, accountID string, req CreateModelRequest) (Model, error) {
	if _, err := s.GetTenantAccount(ctx, organizationID, accountID); err != nil {
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
	var err error
	status, err = normalizeModelStatus(status)
	if err != nil {
		return Model{}, err
	}
	capability := req.Capabilities
	if capability != nil {
		capability = capabilityWithManualProvenance(capability)
	}
	if capability == nil {
		if preset, ok, err := s.lookupModelCapabilityPreset(ctx, s.db, modelKey); err != nil {
			return Model{}, err
		} else if ok {
			if displayName == "" || strings.EqualFold(displayName, modelKey) {
				displayName = preset.DisplayName
			}
			modality = preset.Modality
			input := preset.capabilityInput()
			capability = &input
		} else {
			input := defaultCapabilityInput(modality)
			capability = &input
		}
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
		ON CONFLICT (provider_account_id, model_key) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    modality = EXCLUDED.modality,
		    status = EXCLUDED.status,
		    updated_at = now()
		RETURNING id
	`, accountID, modelKey, displayName, modality, status).Scan(&modelID); err != nil {
		return Model{}, err
	}
	if capability != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM provider_model_capabilities WHERE provider_model_id = $1`, modelID); err != nil {
			return Model{}, err
		}
		if _, err := insertCapability(ctx, tx, modelID, *capability); err != nil {
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
	if item.Status == "active" {
		item, err = s.reconcileModelCapabilityPreset(ctx, item)
		if err != nil {
			return Model{}, err
		}
		if err := s.ensureDefaultCapabilityForModel(ctx, s.db, item.ID, item.Modality); err != nil {
			return Model{}, err
		}
	}
	item.Capabilities, err = s.listCapabilities(ctx, item.ID)
	if err != nil {
		return Model{}, err
	}
	return item, nil
}

func (s *Service) UpdateModel(ctx context.Context, organizationID, modelID string, req UpdateModelRequest) (Model, error) {
	if err := s.EnsureTenantProviderModel(ctx, organizationID, modelID); err != nil {
		return Model{}, err
	}
	return s.updateModel(ctx, organizationID, modelID, req)
}

func (s *Service) UpdateAvailableModel(
	ctx context.Context,
	organizationID string,
	modelID string,
	req UpdateAvailableModelRequest,
) (Model, error) {
	if req.DisplayName == nil && req.Modality == nil && req.Capabilities == nil {
		return Model{}, fmt.Errorf("%w: at least one model configuration field is required", ErrValidation)
	}
	if err := s.EnsureAvailableProviderModel(ctx, organizationID, modelID); err != nil {
		return Model{}, err
	}
	return s.updateModel(ctx, organizationID, modelID, UpdateModelRequest{
		DisplayName:  req.DisplayName,
		Modality:     req.Modality,
		Capabilities: req.Capabilities,
	})
}

func (s *Service) updateModel(ctx context.Context, organizationID, modelID string, req UpdateModelRequest) (Model, error) {
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
	status, err = normalizeModelStatus(status)
	if err != nil {
		return Model{}, err
	}
	if modelKey == "" || displayName == "" || modality == "" {
		return Model{}, fmt.Errorf("%w: modelKey, displayName, and modality are required", ErrValidation)
	}
	capability := req.Capabilities
	if capability != nil {
		capability = capabilityWithManualProvenance(capability)
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
		if isUniqueViolation(err) {
			return Model{}, ErrModelAlreadyExists
		}
		return Model{}, err
	}
	if capability != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM provider_model_capabilities WHERE provider_model_id = $1`, modelID); err != nil {
			return Model{}, err
		}
		if _, err := insertCapability(ctx, tx, modelID, *capability); err != nil {
			return Model{}, err
		}
	} else if status == "active" {
		if err := s.ensureDefaultCapabilityForModel(ctx, tx, modelID, modality); err != nil {
			return Model{}, err
		}
	}
	if capability != nil || modality != current.Modality {
		if err := validateExistingModelBindingRuntimeOptions(ctx, tx, modelID, modality); err != nil {
			return Model{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	return s.GetModel(ctx, organizationID, modelID)
}

func (s *Service) DeleteModel(ctx context.Context, organizationID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("%w: modelId is required", ErrValidation)
	}
	if err := s.EnsureTenantProviderModel(ctx, organizationID, modelID); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `
		SELECT m.id
		FROM provider_models m
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE a.organization_id = $1
		  AND m.id = $2
		FOR UPDATE
	`, organizationID, modelID).Scan(&modelID); err != nil {
		return err
	}
	var activeTaskCount, activeLeaseCount int
	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*)
		   FROM provider_async_tasks
		   WHERE provider_model_id = $1
		     AND status IN ('queued', 'running', 'cancelling')),
		  (SELECT count(*)
		   FROM provider_leases
		   WHERE provider_model_id = $1
		     AND status = 'active'
		     AND expires_at > now())
	`, modelID).Scan(&activeTaskCount, &activeLeaseCount); err != nil {
		return err
	}
	if activeTaskCount > 0 || activeLeaseCount > 0 {
		return fmt.Errorf(
			"%w: tasks=%d leases=%d",
			ErrModelInUse,
			activeTaskCount,
			activeLeaseCount,
		)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_call_logs c
		SET model_profile_binding_id = NULL
		FROM model_profile_bindings b
		WHERE c.model_profile_binding_id = b.id
		  AND b.provider_model_id = $1
	`, modelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks t
		SET model_profile_binding_id = NULL
		FROM model_profile_bindings b
		WHERE t.model_profile_binding_id = b.id
		  AND b.provider_model_id = $1
	`, modelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM model_profile_bindings
		WHERE provider_model_id = $1
	`, modelID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM provider_models WHERE id = $1`, modelID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	routingStrategy, err := validateRoutingStrategy(routingStrategy)
	if err != nil {
		return ModelProfile{}, err
	}
	fallbackStrategy, err := validateFallbackStrategy(req.FallbackStrategy)
	if err != nil {
		return ModelProfile{}, err
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
	routingStrategy, err = validateRoutingStrategy(routingStrategy)
	if err != nil {
		return ModelProfile{}, err
	}
	if len(req.FallbackStrategy) > 0 {
		fallbackStrategy, err = validateFallbackStrategy(req.FallbackStrategy)
		if err != nil {
			return ModelProfile{}, err
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
	model, err := s.GetModel(ctx, organizationID, req.ProviderModelID)
	if err != nil {
		return ModelProfile{}, err
	}
	if model.Status != "active" {
		return ModelProfile{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
	}
	if err := s.ensureDefaultCapabilityForModel(ctx, s.db, model.ID, model.Modality); err != nil {
		return ModelProfile{}, err
	}
	priority := 100
	if req.Priority != nil {
		if *req.Priority < 0 {
			return ModelProfile{}, fmt.Errorf("%w: priority must be non-negative", ErrValidation)
		}
		priority = *req.Priority
	}
	weight := 100
	if req.Weight != nil {
		if *req.Weight < 0 {
			return ModelProfile{}, fmt.Errorf("%w: weight must be non-negative", ErrValidation)
		}
		weight = *req.Weight
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	runtimeOptions, err := normalizeModelProfileBindingRuntimeOptions(model, req.RuntimeOptions)
	if err != nil {
		return ModelProfile{}, err
	}
	runtimeOptionsJSON, err := encodeModelProfileBindingRuntimeOptions(runtimeOptions)
	if err != nil {
		return ModelProfile{}, err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled, runtime_options)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (model_profile_id, provider_model_id) DO UPDATE SET
			priority = EXCLUDED.priority,
			weight = EXCLUDED.weight,
			enabled = EXCLUDED.enabled,
			runtime_options = EXCLUDED.runtime_options
	`, profileID, req.ProviderModelID, priority, weight, enabled, runtimeOptionsJSON); err != nil {
		return ModelProfile{}, err
	}
	return s.GetModelProfile(ctx, organizationID, profileID)
}

func (s *Service) UpdateModelProfileBinding(ctx context.Context, organizationID, profileID, bindingID string, req UpdateModelProfileBindingRequest) (ModelProfile, error) {
	if req.Priority == nil && req.Weight == nil && req.Enabled == nil && req.RuntimeOptions == nil {
		return ModelProfile{}, fmt.Errorf("%w: at least one binding field is required", ErrValidation)
	}
	if req.Priority != nil && *req.Priority < 0 {
		return ModelProfile{}, fmt.Errorf("%w: priority must be non-negative", ErrValidation)
	}
	if req.Weight != nil && *req.Weight < 0 {
		return ModelProfile{}, fmt.Errorf("%w: weight must be non-negative", ErrValidation)
	}
	var runtimeOptionsJSON json.RawMessage
	if req.RuntimeOptions != nil {
		var providerModelID string
		if err := s.db.QueryRow(ctx, `
			SELECT b.provider_model_id
			FROM model_profile_bindings b
			JOIN model_profiles p ON p.id = b.model_profile_id
			WHERE p.organization_id = $1 AND p.id = $2 AND b.id = $3
		`, organizationID, profileID, bindingID).Scan(&providerModelID); err != nil {
			return ModelProfile{}, err
		}
		model, err := s.GetModel(ctx, organizationID, providerModelID)
		if err != nil {
			return ModelProfile{}, err
		}
		runtimeOptions, err := normalizeModelProfileBindingRuntimeOptions(model, req.RuntimeOptions)
		if err != nil {
			return ModelProfile{}, err
		}
		runtimeOptionsJSON, err = encodeModelProfileBindingRuntimeOptions(runtimeOptions)
		if err != nil {
			return ModelProfile{}, err
		}
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE model_profile_bindings b
		SET priority = COALESCE($4::integer, b.priority),
		    weight = COALESCE($5::integer, b.weight),
		    enabled = COALESCE($6::boolean, b.enabled),
		    runtime_options = COALESCE($7::jsonb, b.runtime_options)
		FROM model_profiles p
		WHERE b.model_profile_id = p.id
		  AND p.organization_id = $1
		  AND p.id = $2
		  AND b.id = $3
	`, organizationID, profileID, bindingID, req.Priority, req.Weight, req.Enabled, runtimeOptionsJSON)
	if err != nil {
		return ModelProfile{}, err
	}
	if tag.RowsAffected() == 0 {
		return ModelProfile{}, pgx.ErrNoRows
	}
	return s.GetModelProfile(ctx, organizationID, profileID)
}

func (s *Service) DeleteModelProfileBinding(ctx context.Context, organizationID, profileID, bindingID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE provider_call_logs
		SET model_profile_binding_id = NULL
		WHERE model_profile_binding_id = $1
	`, bindingID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks
		SET model_profile_binding_id = NULL
		WHERE model_profile_binding_id = $1
	`, bindingID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
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
	return tx.Commit(ctx)
}

func (s *Service) DiscoverModels(ctx context.Context, organizationID, accountID string) (ModelDiscoveryResult, error) {
	if !s.gatewayConfigured() {
		if err := s.requireGatewayOrDirectFallback(); err != nil {
			return ModelDiscoveryResult{}, err
		}
	}
	credentials, err := s.ListCredentials(ctx, organizationID, accountID, "active")
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	if len(credentials) == 0 {
		return ModelDiscoveryResult{}, fmt.Errorf("%w: provider account has no active credentials", ErrValidation)
	}

	result := ModelDiscoveryResult{Models: []DiscoveredModel{}, Unsupported: []any{}}
	seenModels := make(map[string]struct{})
	for _, credential := range credentials {
		discovered, err := s.DiscoverModelsForCredential(ctx, organizationID, accountID, credential.ID)
		if err != nil {
			return ModelDiscoveryResult{}, err
		}
		for _, model := range discovered.Models {
			modelKey := strings.TrimSpace(model.ModelKey)
			if _, seen := seenModels[modelKey]; seen {
				continue
			}
			seenModels[modelKey] = struct{}{}
			result.Models = append(result.Models, model)
		}
		result.Unsupported = append(result.Unsupported, discovered.Unsupported...)
		result.Sync.DiscoveredCount += discovered.Sync.DiscoveredCount
		result.Sync.CreatedCount += discovered.Sync.CreatedCount
		result.Sync.ExistingCount += discovered.Sync.ExistingCount
		result.Sync.SkippedDisabledCount += discovered.Sync.SkippedDisabledCount
		result.Sync.IgnoredCount += discovered.Sync.IgnoredCount
	}
	return result, nil
}

func (s *Service) DiscoverModelsForCredential(ctx context.Context, organizationID, accountID, credentialID string) (ModelDiscoveryResult, error) {
	if !s.gatewayConfigured() {
		if err := s.requireGatewayOrDirectFallback(); err != nil {
			return ModelDiscoveryResult{}, err
		}
	}
	credentialSummary, err := s.GetCredential(ctx, organizationID, accountID, credentialID)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	if !credentialSummary.IsActive || credentialSummary.Status != "active" {
		return ModelDiscoveryResult{}, fmt.Errorf("%w: credential is not active", ErrValidation)
	}
	if s.gatewayConfigured() {
		var response GatewayDiscoverModelsResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/models/discover", GatewayDiscoverModelsRequest{
			OrganizationID: organizationID,
			AccountID:      accountID,
			CredentialID:   credentialID,
		}, &response); err != nil {
			return ModelDiscoveryResult{}, err
		}
		if isProviderFailureStatus(response.Status) {
			return ModelDiscoveryResult{}, errorFromGatewayStandard(response.Error)
		}
		result := ModelDiscoveryResult{
			CredentialID:  credentialID,
			CredentialKey: credentialSummary.CredentialKey,
			Models:        response.Models,
			Unsupported:   response.Unsupported,
		}
		syncResult, err := s.syncDiscoveredModelsForCredentialWithSummary(ctx, organizationID, accountID, credentialID, result.Models)
		if err != nil {
			return ModelDiscoveryResult{}, err
		}
		result.Sync = syncResult
		return result, nil
	}
	if err := s.requireGatewayOrDirectFallback(); err != nil {
		return ModelDiscoveryResult{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, accountID)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	credential, _, err := s.activeCredentialPayloadByID(ctx, organizationID, accountID, credentialID)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	cfg := parseOpenAICompatibleConfig(account.Config)
	client := newOpenAICompatibleClient(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	result, err := client.discoverModels(ctx, account, apiKey, cfg)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	result.CredentialID = credentialID
	result.CredentialKey = credentialSummary.CredentialKey
	syncResult, err := s.syncDiscoveredModelsForCredentialWithSummary(ctx, organizationID, accountID, credentialID, result.Models)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	result.Sync = syncResult
	return result, nil
}

func (s *Service) syncDiscoveredModels(ctx context.Context, organizationID, accountID string, models []DiscoveredModel) error {
	_, err := s.syncDiscoveredModelsWithSummary(ctx, organizationID, accountID, models)
	return err
}

func (s *Service) syncDiscoveredModelsWithSummary(ctx context.Context, organizationID, accountID string, models []DiscoveredModel) (ModelDiscoverySync, error) {
	return s.syncDiscoveredModelsForCredentialWithSummary(ctx, organizationID, accountID, "", models)
}

func (s *Service) syncDiscoveredModelsForCredentialWithSummary(ctx context.Context, organizationID, accountID, credentialID string, models []DiscoveredModel) (ModelDiscoverySync, error) {
	summary := ModelDiscoverySync{DiscoveredCount: len(models)}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ModelDiscoverySync{}, err
	}
	defer tx.Rollback(ctx)

	var accountExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM provider_accounts
			WHERE organization_id = $1
			  AND id = $2
		)
	`, organizationID, accountID).Scan(&accountExists); err != nil {
		return ModelDiscoverySync{}, err
	}
	if !accountExists {
		return ModelDiscoverySync{}, pgx.ErrNoRows
	}
	if credentialID != "" {
		var credentialActive bool
		if err := tx.QueryRow(ctx, `
			SELECT is_active AND status = 'active'
			FROM provider_credentials
			WHERE organization_id = $1
			  AND provider_account_id = $2
			  AND id = $3
		`, organizationID, accountID, credentialID).Scan(&credentialActive); err != nil {
			return ModelDiscoverySync{}, err
		}
		if !credentialActive {
			return ModelDiscoverySync{}, fmt.Errorf("%w: credential is not active", ErrValidation)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE provider_credential_models
			SET is_available = false,
			    last_discovered_at = now(),
			    updated_at = now()
			WHERE provider_credential_id = $1
		`, credentialID); err != nil {
			return ModelDiscoverySync{}, err
		}
	}

	existingStatuses := map[string]string{}
	existingRows, err := tx.Query(ctx, `
		SELECT model_key, status
		FROM provider_models
		WHERE provider_account_id = $1
	`, accountID)
	if err != nil {
		return ModelDiscoverySync{}, err
	}
	for existingRows.Next() {
		var modelKey, status string
		if err := existingRows.Scan(&modelKey, &status); err != nil {
			existingRows.Close()
			return ModelDiscoverySync{}, err
		}
		existingStatuses[modelKey] = status
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		return ModelDiscoverySync{}, err
	}
	existingRows.Close()

	seen := map[string]struct{}{}
	for _, discovered := range models {
		modelKey := strings.TrimSpace(discovered.ModelKey)
		if modelKey == "" {
			summary.IgnoredCount++
			continue
		}
		if _, ok := seen[modelKey]; ok {
			summary.IgnoredCount++
			continue
		}
		seen[modelKey] = struct{}{}
		if status, exists := existingStatuses[modelKey]; !exists {
			summary.CreatedCount++
		} else if status == "disabled" {
			summary.SkippedDisabledCount++
		} else {
			summary.ExistingCount++
		}

		displayName := strings.TrimSpace(discovered.DisplayName)
		if displayName == "" {
			displayName = modelKey
		}
		modality := normalizeDiscoveredModality(discovered.Modality)
		capability := defaultCapabilityInput(modality)
		presetMatched := false
		if preset, ok, err := s.lookupModelCapabilityPreset(ctx, tx, modelKey); err != nil {
			return ModelDiscoverySync{}, err
		} else if ok {
			presetMatched = true
			if displayName == "" || strings.EqualFold(displayName, modelKey) {
				displayName = preset.DisplayName
			}
			modality = preset.Modality
			capability = preset.capabilityInput()
		}
		capability, err = normalizeCapabilityInput(capability)
		if err != nil {
			return ModelDiscoverySync{}, err
		}
		status := "active"

		var modelID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (provider_account_id, model_key) DO UPDATE SET
				display_name = CASE
					WHEN provider_models.status <> 'disabled'
					  AND NOT EXISTS (
						SELECT 1
						FROM provider_model_capabilities capability
						WHERE capability.provider_model_id = provider_models.id
						  AND COALESCE(capability.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') = 'manual'
					  ) THEN EXCLUDED.display_name
					ELSE provider_models.display_name
				END,
				modality = CASE
					WHEN provider_models.status <> 'disabled'
					  AND NOT EXISTS (
						SELECT 1
						FROM provider_model_capabilities capability
						WHERE capability.provider_model_id = provider_models.id
						  AND COALESCE(capability.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') = 'manual'
					  ) THEN EXCLUDED.modality
					ELSE provider_models.modality
				END,
				status = CASE
					WHEN provider_models.status = 'disabled' THEN provider_models.status
					WHEN provider_models.status IN ('deprecated', 'error') THEN 'active'
					ELSE provider_models.status
				END,
				updated_at = CASE
					WHEN provider_models.status <> 'disabled'
					  AND NOT EXISTS (
						SELECT 1
						FROM provider_model_capabilities capability
						WHERE capability.provider_model_id = provider_models.id
						  AND COALESCE(capability.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') = 'manual'
					  ) THEN now()
					ELSE provider_models.updated_at
				END
			RETURNING id
		`, accountID, modelKey, displayName, modality, status).Scan(&modelID); err != nil {
			return ModelDiscoverySync{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_model_capabilities(
				provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
			)
			SELECT $1, $2, $3, $4, $5, $6, $7
			WHERE NOT EXISTS (
				SELECT 1 FROM provider_model_capabilities WHERE provider_model_id = $1
			)
		`, modelID, capability.TaskTypes, capability.InputLimits, capability.OutputLimits, capability.QualityTiers, capability.ProviderOptionsSchema, capability.PricingPolicy); err != nil {
			return ModelDiscoverySync{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE provider_model_capabilities c
			SET task_types = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.task_types IS NULL OR c.task_types IN ('[]'::jsonb, '{}'::jsonb) THEN $2 ELSE c.task_types END,
			    input_limits = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.input_limits IS NULL OR c.input_limits = '{}'::jsonb THEN $3 ELSE c.input_limits END,
			    output_limits = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.output_limits IS NULL OR c.output_limits = '{}'::jsonb THEN $4 ELSE c.output_limits END,
			    quality_tiers = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.quality_tiers IS NULL OR c.quality_tiers IN ('[]'::jsonb, '{}'::jsonb) THEN $5 ELSE c.quality_tiers END,
			    provider_options_schema = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.provider_options_schema IS NULL OR c.provider_options_schema = '{}'::jsonb THEN $6 ELSE c.provider_options_schema END,
			    pricing_policy = CASE WHEN (
			        $8::boolean
			        AND COALESCE(c.provider_options_schema #>> '{xCapabilities,capabilitySource}', 'unknown') NOT IN ('official', 'provider', 'manual')
			    ) OR c.pricing_policy IS NULL OR c.pricing_policy = '{}'::jsonb THEN $7 ELSE c.pricing_policy END
			FROM provider_models m
			WHERE c.provider_model_id = m.id
			  AND c.provider_model_id = $1
			  AND m.status <> 'disabled'
		`, modelID, capability.TaskTypes, capability.InputLimits, capability.OutputLimits, capability.QualityTiers, capability.ProviderOptionsSchema, capability.PricingPolicy, presetMatched); err != nil {
			return ModelDiscoverySync{}, err
		}
		if credentialID != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO provider_credential_models(
					provider_credential_id, provider_model_id, is_available,
					last_discovered_at, created_at, updated_at
				)
				VALUES ($1, $2, true, now(), now(), now())
				ON CONFLICT (provider_credential_id, provider_model_id) DO UPDATE SET
					is_available = true,
					last_discovered_at = now(),
					updated_at = now()
			`, credentialID, modelID); err != nil {
				return ModelDiscoverySync{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ModelDiscoverySync{}, err
	}
	return summary, nil
}

func normalizeListStatusFilter(value string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return "active", nil
	}
	switch status {
	case "active", "disabled", "all":
		return status, nil
	default:
		return "", fmt.Errorf("%w: invalid status filter", ErrValidation)
	}
}

func normalizeCredentialStatusFilter(value string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return "active", nil
	}
	switch status {
	case "active", "rotated", "revoked", "expired", "all":
		return status, nil
	default:
		return "", fmt.Errorf("%w: invalid credential status filter", ErrValidation)
	}
}

func normalizeModelStatus(value string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return "active", nil
	}
	switch status {
	case "active", "disabled", "deprecated", "error":
		return status, nil
	default:
		return "", fmt.Errorf("%w: model status is invalid", ErrValidation)
	}
}

func normalizeDiscoveredModality(value string) string {
	switch strings.TrimSpace(value) {
	case "image", "video", "audio", "embedding", "multimodal":
		return strings.TrimSpace(value)
	default:
		return "text"
	}
}

func discoveredTaskTypes(modality string) []string {
	switch modality {
	case "image":
		return []string{TaskTypeImageGenerate}
	case "video":
		return []string{"video.text_to_video", "video.image_to_video", TaskTypeVideoCreateTask, TaskTypeVideoPollTask, TaskTypeVideoCancelTask}
	case "audio":
		return []string{TaskTypeAudioTTS, TaskTypeAudioTranscribe}
	case "embedding":
		return []string{"embedding.create"}
	case "multimodal":
		return []string{TaskTypeTextGenerate, TaskTypeTextStream, TaskTypeImageGenerate, TaskTypeAudioTTS, TaskTypeAudioTranscribe, TaskTypeVideoCreateTask, TaskTypeVideoPollTask}
	default:
		return []string{TaskTypeTextGenerate, TaskTypeTextStream}
	}
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
	if model.Status != "active" {
		return ProviderTestResult{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
	}
	account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return ProviderTestResult{}, err
	}
	credential, credentialID, err := s.credentialPayloadForModel(ctx, organizationID, model.ProviderAccountID, model.ID)
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
	case "audio_tts_test", "audio_transcription_test":
		return ProviderTestResult{}, fmt.Errorf("%w: configure PROVIDER_GATEWAY_URL for %s", ErrProviderGatewayRequired, testType)
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
	if model.Status != "active" {
		return ProviderTestResult{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
	}

	status := "succeeded"
	latencyMS := 0
	var providerCallID string
	var errorCode, errorMessage string
	var normalizedOutput json.RawMessage
	var requestSnapshot json.RawMessage
	var responseSnapshot json.RawMessage
	var attempts []GatewayAttempt

	switch testType {
	case "connection_test", "auth_test", "model_discovery_test":
		credentialID, err := s.credentialIDForModel(ctx, organizationID, model.ProviderAccountID, model.ID)
		if err != nil {
			return ProviderTestResult{}, err
		}
		gatewayReq := GatewayDiscoverModelsRequest{
			OrganizationID: organizationID,
			AccountID:      model.ProviderAccountID,
			CredentialID:   credentialID,
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
		if isProviderFailureStatus(status) {
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
		attempts = gatewayResp.Attempts
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if isProviderFailureStatus(status) {
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
		attempts = gatewayResp.Attempts
		requestSnapshot = mustJSON(gatewayReq)
		responseSnapshot = mustJSON(gatewayResp)
		if isProviderFailureStatus(status) {
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
		if isProviderFailureStatus(status) {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "audio_tts_test":
		gatewayReq := GatewayTTSRequest{
			OrganizationID: organizationID, ProjectID: stringFieldFromJSON(input, "projectId"),
			WorkflowRunID: stringFieldFromJSON(input, "workflowRunId"), NodeRunID: stringFieldFromJSON(input, "nodeRunId"),
			ProviderModelID: modelID, IdempotencyKey: req.IdempotencyKey, Input: input,
		}
		var gatewayResp GatewayTTSResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/audio/tts", gatewayReq, &gatewayResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID, status, latencyMS = gatewayResp.ProviderCallID, gatewayResp.Status, gatewayResp.LatencyMS
		attempts, requestSnapshot, responseSnapshot = gatewayResp.Attempts, mustJSON(gatewayReq), mustJSON(gatewayResp)
		if isProviderFailureStatus(status) {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "audio_transcription_test":
		gatewayReq := GatewayASRRequest{
			OrganizationID: organizationID, ProjectID: stringFieldFromJSON(input, "projectId"),
			WorkflowRunID: stringFieldFromJSON(input, "workflowRunId"), NodeRunID: stringFieldFromJSON(input, "nodeRunId"),
			ProviderModelID: modelID, IdempotencyKey: req.IdempotencyKey, Input: input,
			Source: GatewayAudioSource{
				ArtifactID: stringFieldFromJSON(input, "artifactId"), MediaFileID: stringFieldFromJSON(input, "mediaFileId"),
				StorageKey: stringFieldFromJSON(input, "storageKey"), FileName: stringFieldFromJSON(input, "fileName"),
			},
		}
		var gatewayResp GatewayASRResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/audio/transcribe", gatewayReq, &gatewayResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID, status, latencyMS = gatewayResp.ProviderCallID, gatewayResp.Status, gatewayResp.LatencyMS
		attempts, requestSnapshot, responseSnapshot = gatewayResp.Attempts, mustJSON(gatewayReq), mustJSON(gatewayResp)
		if isProviderFailureStatus(status) {
			errorCode, errorMessage = gatewayErrorFields(gatewayResp.Error)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			normalizedOutput = mustJSON(gatewayResp.Output)
		}
	case "video_generation_test":
		projectID := stringFieldFromJSON(input, "projectId")
		if projectID == "" {
			return ProviderTestResult{}, fmt.Errorf("%w: projectId is required for video_generation_test", ErrValidation)
		}
		production, err := videoproduction.LoadActiveContextForOrganization(ctx, s.db, organizationID, projectID)
		if err != nil {
			return ProviderTestResult{}, err
		}
		createReq := GatewayVideoCreateTaskRequest{
			OrganizationID:                 organizationID,
			ProjectID:                      projectID,
			ProductionGenerationID:         production.Generation.ID,
			VideoProductionBindingID:       production.Binding.ID,
			VideoProductionBindingRevision: production.Binding.Revision,
			WorkflowRunID:                  stringFieldFromJSON(input, "workflowRunId"),
			NodeRunID:                      stringFieldFromJSON(input, "nodeRunId"),
			ProviderModelID:                modelID,
			IdempotencyKey:                 req.IdempotencyKey,
			Input:                          input,
		}
		var createResp GatewayVideoCreateTaskResponse
		if err := s.postGatewayJSON(ctx, "/internal/provider/video/create-task", createReq, &createResp); err != nil {
			return ProviderTestResult{}, err
		}
		providerCallID = createResp.ProviderCallID
		status = createResp.Status
		latencyMS = createResp.LatencyMS
		attempts = createResp.Attempts
		requestSnapshot = mustJSON(createReq)
		responseSnapshot = mustJSON(createResp)
		normalizedOutput = mustJSON(map[string]any{
			"providerAsyncTaskId": createResp.ProviderAsyncTaskID,
			"externalTaskId":      createResp.ExternalTaskID,
			"status":              createResp.Status,
		})
		if isProviderFailureStatus(status) {
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
				OrganizationID:                 organizationID,
				ProviderAsyncTaskID:            createResp.ProviderAsyncTaskID,
				ProjectID:                      createReq.ProjectID,
				ProductionGenerationID:         createReq.ProductionGenerationID,
				VideoProductionBindingID:       createReq.VideoProductionBindingID,
				VideoProductionBindingRevision: createReq.VideoProductionBindingRevision,
				WorkflowRunID:                  createReq.WorkflowRunID,
				NodeRunID:                      createReq.NodeRunID,
			}
			var pollResp GatewayVideoPollTaskResponse
			if err := s.postGatewayJSON(ctx, "/internal/provider/video/poll-task", pollReq, &pollResp); err != nil {
				return ProviderTestResult{}, err
			}
			providerCallID = pollResp.ProviderCallID
			status = pollResp.Status
			latencyMS += pollResp.LatencyMS
			responseSnapshot = mustJSON(pollResp)
			if isProviderFailureStatus(status) {
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
	testRunStatus := status
	if testRunStatus == "blocked" {
		testRunStatus = "failed"
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
		testRunStatus,
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
		Attempts:         attempts,
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
		var credentialID string
		var err error
		if req.ProviderModelID != "" {
			credentialID, err = s.credentialIDForModel(ctx, req.OrganizationID, req.ProviderAccountID, req.ProviderModelID)
		} else {
			credentialID, err = s.activeCredentialID(ctx, req.OrganizationID, req.ProviderAccountID)
		}
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
			id, provider_request_id, attempt_generation, attempt_sequence,
			organization_id, project_id, production_generation_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost::text, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			requested_duration_seconds::float8, actual_duration_seconds::float8, media_probe,
			created_at, started_at, completed_at,
			billing_context_id, provider_external_log_id
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
	var estimatedCost sql.NullString
	if err := s.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status IN ('failed', 'blocked')),
			COALESCE(sum(estimated_cost), 0)::text
		FROM provider_call_logs
		WHERE organization_id = $1
	`, organizationID).Scan(
		&summary.TotalCalls,
		&summary.FailedCalls,
		&estimatedCost,
	); err != nil {
		return UsageSummary{}, err
	}
	summary.EstimatedCost = "0"
	if estimatedCost.Valid {
		summary.EstimatedCost = estimatedCost.String
	}
	summary.EstimateCurrency = "USD"
	summary.Authoritative = false
	summary.SourceSemantics = "technical_estimate"
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
	client := &GatewayClient{
		BaseURL: s.gatewayURL,
		Token:   s.gatewayToken,
		Client:  s.httpClient,
	}
	return client.StreamText(ctx, payload, nil)
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
	return &StandardErrorError{Standard: *standard}
}

func gatewayErrorFields(standard *StandardError) (string, string) {
	if standard == nil {
		return CodeUnknownError, "provider gateway request failed"
	}
	return standard.Code, standard.Message
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
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
		  AND status = 'active'
		ORDER BY credential_key, created_at DESC
		LIMIT 1
	`, organizationID, accountID).Scan(&credentialID)
	return credentialID, err
}

func (s *Service) activeCredentialPayload(ctx context.Context, organizationID, accountID string) (map[string]any, string, error) {
	credentialID, err := s.activeCredentialID(ctx, organizationID, accountID)
	if err != nil {
		return nil, "", err
	}
	return s.activeCredentialPayloadByID(ctx, organizationID, accountID, credentialID)
}

func (s *Service) credentialIDForModel(ctx context.Context, organizationID, accountID, modelID string) (string, error) {
	var credentialID string
	err := s.db.QueryRow(ctx, `
		SELECT pc.id
		FROM provider_credential_models pcm
		JOIN provider_credentials pc ON pc.id = pcm.provider_credential_id
		JOIN provider_models pm ON pm.id = pcm.provider_model_id
		WHERE pc.organization_id = $1
		  AND pc.provider_account_id = $2
		  AND pm.provider_account_id = $2
		  AND pm.id = $3
		  AND pc.is_active = true
		  AND pc.status = 'active'
		  AND pcm.is_available = true
		ORDER BY pc.credential_key, pc.created_at DESC
		LIMIT 1
	`, organizationID, accountID, modelID).Scan(&credentialID)
	if err == nil {
		return credentialID, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}

	var hasDiscoveryMapping bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM provider_credential_models pcm
			JOIN provider_credentials pc ON pc.id = pcm.provider_credential_id
			WHERE pc.organization_id = $1
			  AND pc.provider_account_id = $2
			  AND pcm.provider_model_id = $3
		)
	`, organizationID, accountID, modelID).Scan(&hasDiscoveryMapping); err != nil {
		return "", err
	}
	if hasDiscoveryMapping {
		return "", fmt.Errorf("%w: provider model is not available for any active credential", ErrValidation)
	}
	return s.activeCredentialID(ctx, organizationID, accountID)
}

func (s *Service) credentialPayloadForModel(ctx context.Context, organizationID, accountID, modelID string) (map[string]any, string, error) {
	credentialID, err := s.credentialIDForModel(ctx, organizationID, accountID, modelID)
	if err != nil {
		return nil, "", err
	}
	return s.activeCredentialPayloadByID(ctx, organizationID, accountID, credentialID)
}

func (s *Service) activeCredentialPayloadByID(ctx context.Context, organizationID, accountID, credentialID string) (map[string]any, string, error) {
	var encrypted []byte
	err := s.db.QueryRow(ctx, `
		SELECT encrypted_payload
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND id = $3
		  AND is_active = true
		  AND status = 'active'
	`, organizationID, accountID, credentialID).Scan(&encrypted)
	if err != nil {
		return nil, "", err
	}
	return s.decryptCredentialPayload(credentialID, encrypted)
}

// credentialPayloadByID resolves the exact historical credential. It is used
// by long-running provider tasks that must keep the credential identity chosen
// at task creation even after the logical credential is rotated.
func (s *Service) credentialPayloadByID(ctx context.Context, organizationID, accountID, credentialID string) (map[string]any, string, error) {
	var encrypted []byte
	err := s.db.QueryRow(ctx, `
		SELECT encrypted_payload
		FROM provider_credentials
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND id = $3
	`, organizationID, accountID, credentialID).Scan(&encrypted)
	if err != nil {
		return nil, "", err
	}
	return s.decryptCredentialPayload(credentialID, encrypted)
}

func (s *Service) decryptCredentialPayload(credentialID string, encrypted []byte) (map[string]any, string, error) {
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
			(
				SELECT count(*)
				FROM provider_credentials pc
				WHERE pc.provider_account_id = a.id
				  AND pc.is_active = true
			) AS credential_count,
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
		&item.CredentialCount,
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

func credentialSelect(suffix string) string {
	return `
		SELECT
			pc.id,
			pc.organization_id,
			pc.provider_account_id,
			pc.credential_key,
			pc.credential_type,
			pc.masked_preview,
			pc.status,
			pc.is_active,
			(
				SELECT count(*)
				FROM provider_credential_models pcm
				WHERE pcm.provider_credential_id = pc.id
				  AND pcm.is_available = true
			) AS available_model_count,
			(
				SELECT max(pcm.last_discovered_at)
				FROM provider_credential_models pcm
				WHERE pcm.provider_credential_id = pc.id
			) AS last_discovered_at,
			pc.created_by,
			pc.created_at,
			pc.expires_at,
			pc.rotated_at
		FROM provider_credentials pc
	` + suffix
}

func scanCredential(row rowScanner) (Credential, error) {
	var item Credential
	var preview, createdBy sql.NullString
	var lastDiscoveredAt, expiresAt, rotatedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProviderAccountID,
		&item.CredentialKey,
		&item.CredentialType,
		&preview,
		&item.Status,
		&item.IsActive,
		&item.AvailableModelCount,
		&lastDiscoveredAt,
		&createdBy,
		&item.CreatedAt,
		&expiresAt,
		&rotatedAt,
	)
	item.MaskedPreview = preview.String
	item.CreatedBy = stringPtr(createdBy)
	item.LastDiscoveredAt = timePtr(lastDiscoveredAt)
	item.ExpiresAt = timePtr(expiresAt)
	item.RotatedAt = timePtr(rotatedAt)
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
	metadata := capabilityLanguageMetadataFromSchema(item.ProviderOptionsSchema)
	item.SupportedInputLanguages = metadata.SupportedInputLanguages
	item.SupportedOutputLanguages = metadata.SupportedOutputLanguages
	item.SupportedPromptLanguages = metadata.SupportedPromptLanguages
	item.SupportedNativeAudioLanguages = metadata.SupportedNativeAudioLanguages
	item.Source = metadata.Source
	item.ApprovalStatus = metadata.ApprovalStatus
	return item, err
}

type capabilityWriter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertCapability(ctx context.Context, tx capabilityWriter, modelID string, input CapabilityInput) (string, error) {
	capability, err := normalizeCapabilityInput(input)
	if err != nil {
		return "", err
	}
	var capabilityID string
	err = tx.QueryRow(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, modelID, capability.TaskTypes, capability.InputLimits, capability.OutputLimits, capability.QualityTiers, capability.ProviderOptionsSchema, capability.PricingPolicy).Scan(&capabilityID)
	return capabilityID, err
}

type capabilityEnsurer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *Service) ensureDefaultCapabilityForModel(ctx context.Context, exec capabilityEnsurer, modelID, modality string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	capability, err := normalizeCapabilityInput(defaultCapabilityInput(modality))
	if err != nil {
		return err
	}
	if _, err := exec.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		SELECT $1, $2, $3, $4, $5, $6, $7
		WHERE NOT EXISTS (
			SELECT 1 FROM provider_model_capabilities WHERE provider_model_id = $1
		)
	`, modelID, capability.TaskTypes, capability.InputLimits, capability.OutputLimits, capability.QualityTiers, capability.ProviderOptionsSchema, capability.PricingPolicy); err != nil {
		return err
	}
	_, err = exec.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET task_types = $2,
		    provider_options_schema = CASE
		      WHEN provider_options_schema IS NULL OR provider_options_schema = '{}'::jsonb THEN $3
		      ELSE provider_options_schema
		    END
		WHERE provider_model_id = $1
		  AND (
		    task_types IS NULL
		    OR task_types = '[]'::jsonb
		    OR task_types = '{}'::jsonb
		  )
	`, modelID, capability.TaskTypes, capability.ProviderOptionsSchema)
	return err
}

func normalizeCapabilityInput(input CapabilityInput) (CapabilityInput, error) {
	taskTypes, err := normalizeJSON(input.TaskTypes, "[]")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: taskTypes must be valid JSON", ErrValidation)
	}
	inputLimits, err := normalizeJSON(input.InputLimits, "{}")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: inputLimits must be valid JSON", ErrValidation)
	}
	outputLimits, err := normalizeJSON(input.OutputLimits, "{}")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: outputLimits must be valid JSON", ErrValidation)
	}
	qualityTiers, err := normalizeJSON(input.QualityTiers, "[]")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: qualityTiers must be valid JSON", ErrValidation)
	}
	providerOptionsSchema, err := normalizeJSON(input.ProviderOptionsSchema, "{}")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: providerOptionsSchema must be valid JSON", ErrValidation)
	}
	pricingPolicy, err := normalizeJSON(input.PricingPolicy, "{}")
	if err != nil {
		return CapabilityInput{}, fmt.Errorf("%w: pricingPolicy must be valid JSON", ErrValidation)
	}
	inputLimits, outputLimits, qualityTiers = normalizeCapabilityLimitsForTaskTypes(taskTypes, inputLimits, outputLimits, qualityTiers)
	providerOptionsSchema = normalizeProviderOptionsSchema(providerOptionsSchema, taskTypes, inputLimits, outputLimits, qualityTiers)
	providerOptionsSchema, err = normalizeReasoningCapabilityDefaults(providerOptionsSchema)
	if err != nil {
		return CapabilityInput{}, err
	}
	normalized := CapabilityInput{
		TaskTypes:                     taskTypes,
		InputLimits:                   inputLimits,
		OutputLimits:                  outputLimits,
		QualityTiers:                  qualityTiers,
		ProviderOptionsSchema:         providerOptionsSchema,
		PricingPolicy:                 pricingPolicy,
		SupportedInputLanguages:       input.SupportedInputLanguages,
		SupportedOutputLanguages:      input.SupportedOutputLanguages,
		SupportedPromptLanguages:      input.SupportedPromptLanguages,
		SupportedNativeAudioLanguages: input.SupportedNativeAudioLanguages,
		Source:                        input.Source,
		ApprovalStatus:                input.ApprovalStatus,
	}
	normalized, providerOptionsSchema, err = normalizeCapabilityLanguageMetadata(normalized, providerOptionsSchema)
	if err != nil {
		return CapabilityInput{}, err
	}
	normalized.ProviderOptionsSchema = providerOptionsSchema
	return normalized, nil
}

func capabilityWithManualProvenance(input *CapabilityInput) *CapabilityInput {
	if input == nil {
		return nil
	}
	copy := *input
	if strings.TrimSpace(copy.Source) == "" {
		copy.Source = CapabilitySourceManual
	}
	if strings.TrimSpace(copy.ApprovalStatus) == "" {
		copy.ApprovalStatus = CapabilityApprovalApproved
	}
	return &copy
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
		SELECT id, model_profile_id, provider_model_id, priority, weight, enabled, runtime_options, created_at
		FROM model_profile_bindings
		WHERE model_profile_id = $1
		ORDER BY priority ASC, weight DESC, created_at ASC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ModelProfileBinding, 0)
	for rows.Next() {
		var item ModelProfileBinding
		var runtimeOptions []byte
		if err := rows.Scan(&item.ID, &item.ModelProfileID, &item.ProviderModelID, &item.Priority, &item.Weight, &item.Enabled, &runtimeOptions, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.RuntimeOptions, err = decodeModelProfileBindingRuntimeOptions(runtimeOptions)
		if err != nil {
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
	if req.AttemptGeneration <= 0 {
		req.AttemptGeneration = 1
	}
	if req.AttemptSequence <= 0 {
		req.AttemptSequence = 1
	}
	if (strings.TrimSpace(req.OperationItemID) == "") != (req.OperationItemAttempt <= 0) {
		return CallLog{}, fmt.Errorf("%w: operationItemId and operationItemAttempt must be provided together", ErrValidation)
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
	mediaProbe, err := normalizeJSON(req.MediaProbe, "{}")
	if err != nil {
		return CallLog{}, fmt.Errorf("%w: mediaProbe must be valid JSON", ErrValidation)
	}

	row := db.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			id,
			provider_request_id, attempt_generation, attempt_sequence,
			organization_id, project_id, production_generation_id,
			operation_id, operation_item_id, operation_item_attempt, video_render_plan_id, video_render_segment_id,
			workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			prompt_version_id, prompt_hash,
			lease_id, idempotency_key, task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			requested_duration_seconds, actual_duration_seconds, media_probe,
			started_at, completed_at,
			billing_context_id, provider_external_log_id
		)
		VALUES (
			COALESCE(NULLIF(@id, '')::uuid, gen_random_uuid()),
			NULLIF(@provider_request_id, '')::uuid, @attempt_generation, @attempt_sequence,
			@organization_id, NULLIF(@project_id, '')::uuid, NULLIF(@production_generation_id, '')::uuid,
			NULLIF(@operation_id, '')::uuid, NULLIF(@operation_item_id, '')::uuid, NULLIF(@operation_item_attempt, 0), NULLIF(@video_render_plan_id, '')::uuid, NULLIF(@video_render_segment_id, '')::uuid,
			NULLIF(@workflow_run_id, '')::uuid, NULLIF(@node_run_id, '')::uuid,
			@provider_account_id, NULLIF(@provider_model_id, '')::uuid, NULLIF(@credential_id, '')::uuid,
			NULLIF(@model_profile_id, '')::uuid, NULLIF(@model_profile_binding_id, '')::uuid, NULLIF(@model_profile_key, ''),
			NULLIF(@prompt_version_id, '')::uuid, NULLIF(@prompt_hash, ''),
			NULLIF(@lease_id, '')::uuid, NULLIF(@idempotency_key, ''), @task_type, @execution_mode, @status,
			@latency_ms, @input_tokens, @output_tokens, @estimated_cost, @currency,
			NULLIF(@error_code, ''), NULLIF(@error_message, ''), @upstream_status, NULLIF(@upstream_error_code, ''),
			@request_snapshot, @response_snapshot, @normalized_output, @artifact_ids, @media_file_ids,
			@requested_duration_seconds, @actual_duration_seconds, @media_probe,
			CASE WHEN @status IN ('running', 'succeeded', 'failed', 'skipped', 'blocked', 'unknown_outcome') THEN now() ELSE NULL END,
			CASE WHEN @status IN ('succeeded', 'failed', 'cancelled', 'skipped', 'blocked', 'unknown_outcome') THEN now() ELSE NULL END,
			COALESCE(
			    NULLIF(@billing_context_id, '')::uuid,
			    (
			        SELECT billing_context_id
			        FROM provider_requests
			        WHERE id = NULLIF(@provider_request_id, '')::uuid
			    )
			),
			NULLIF(@provider_external_log_id, '')
		)
		ON CONFLICT (id) DO UPDATE SET
			provider_request_id = COALESCE(EXCLUDED.provider_request_id, provider_call_logs.provider_request_id),
			attempt_generation = EXCLUDED.attempt_generation,
			attempt_sequence = EXCLUDED.attempt_sequence,
			organization_id = EXCLUDED.organization_id,
			project_id = EXCLUDED.project_id,
			production_generation_id = COALESCE(EXCLUDED.production_generation_id, provider_call_logs.production_generation_id),
			operation_id = COALESCE(EXCLUDED.operation_id, provider_call_logs.operation_id),
			operation_item_id = COALESCE(EXCLUDED.operation_item_id, provider_call_logs.operation_item_id),
			operation_item_attempt = COALESCE(EXCLUDED.operation_item_attempt, provider_call_logs.operation_item_attempt),
			video_render_plan_id = COALESCE(EXCLUDED.video_render_plan_id, provider_call_logs.video_render_plan_id),
			video_render_segment_id = COALESCE(EXCLUDED.video_render_segment_id, provider_call_logs.video_render_segment_id),
			workflow_run_id = EXCLUDED.workflow_run_id,
			node_run_id = EXCLUDED.node_run_id,
			provider_account_id = EXCLUDED.provider_account_id,
			provider_model_id = EXCLUDED.provider_model_id,
			credential_id = EXCLUDED.credential_id,
			model_profile_id = EXCLUDED.model_profile_id,
			model_profile_binding_id = EXCLUDED.model_profile_binding_id,
			model_profile_key = EXCLUDED.model_profile_key,
			prompt_version_id = EXCLUDED.prompt_version_id,
			prompt_hash = EXCLUDED.prompt_hash,
			lease_id = EXCLUDED.lease_id,
			idempotency_key = EXCLUDED.idempotency_key,
			task_type = EXCLUDED.task_type,
			execution_mode = EXCLUDED.execution_mode,
			status = EXCLUDED.status,
			latency_ms = EXCLUDED.latency_ms,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			estimated_cost = EXCLUDED.estimated_cost,
			currency = EXCLUDED.currency,
			error_code = EXCLUDED.error_code,
			error_message = EXCLUDED.error_message,
			upstream_status = EXCLUDED.upstream_status,
			upstream_error_code = EXCLUDED.upstream_error_code,
			request_snapshot = EXCLUDED.request_snapshot,
			response_snapshot = EXCLUDED.response_snapshot,
			normalized_output = EXCLUDED.normalized_output,
			artifact_ids = EXCLUDED.artifact_ids,
			media_file_ids = EXCLUDED.media_file_ids,
			requested_duration_seconds = EXCLUDED.requested_duration_seconds,
			actual_duration_seconds = EXCLUDED.actual_duration_seconds,
			media_probe = EXCLUDED.media_probe,
			billing_context_id = COALESCE(
			    provider_call_logs.billing_context_id,
			    EXCLUDED.billing_context_id
			),
			provider_external_log_id = COALESCE(
			    EXCLUDED.provider_external_log_id,
			    provider_call_logs.provider_external_log_id
			),
			started_at = COALESCE(provider_call_logs.started_at, EXCLUDED.started_at),
			completed_at = EXCLUDED.completed_at
		RETURNING
			id, provider_request_id, attempt_generation, attempt_sequence,
			organization_id, project_id, production_generation_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id,
			model_profile_id, model_profile_binding_id, model_profile_key,
			task_type, execution_mode, status,
			latency_ms, input_tokens, output_tokens, estimated_cost::text, currency,
			error_code, error_message, upstream_status, upstream_error_code,
			request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			requested_duration_seconds::float8, actual_duration_seconds::float8, media_probe,
			created_at, started_at, completed_at,
			billing_context_id, provider_external_log_id
	`, pgx.NamedArgs{
		"id":                         strings.TrimSpace(req.ID),
		"provider_request_id":        strings.TrimSpace(req.ProviderRequestID),
		"attempt_generation":         req.AttemptGeneration,
		"attempt_sequence":           req.AttemptSequence,
		"organization_id":            req.OrganizationID,
		"project_id":                 strings.TrimSpace(req.ProjectID),
		"production_generation_id":   strings.TrimSpace(req.ProductionGenerationID),
		"operation_id":               strings.TrimSpace(req.OperationID),
		"operation_item_id":          strings.TrimSpace(req.OperationItemID),
		"operation_item_attempt":     req.OperationItemAttempt,
		"video_render_plan_id":       strings.TrimSpace(req.ExecutionPlanID),
		"video_render_segment_id":    strings.TrimSpace(req.RenderSegmentID),
		"workflow_run_id":            strings.TrimSpace(req.WorkflowRunID),
		"node_run_id":                strings.TrimSpace(req.NodeRunID),
		"provider_account_id":        req.ProviderAccountID,
		"provider_model_id":          strings.TrimSpace(req.ProviderModelID),
		"credential_id":              strings.TrimSpace(req.CredentialID),
		"model_profile_id":           strings.TrimSpace(req.ModelProfileID),
		"model_profile_binding_id":   strings.TrimSpace(req.ModelProfileBindingID),
		"model_profile_key":          strings.TrimSpace(req.ModelProfileKey),
		"prompt_version_id":          strings.TrimSpace(req.PromptVersionID),
		"prompt_hash":                strings.TrimSpace(req.PromptHash),
		"lease_id":                   strings.TrimSpace(req.LeaseID),
		"idempotency_key":            strings.TrimSpace(req.IdempotencyKey),
		"task_type":                  taskType,
		"execution_mode":             executionMode,
		"status":                     status,
		"latency_ms":                 req.LatencyMS,
		"input_tokens":               nullInt(req.InputTokens),
		"output_tokens":              nullInt(req.OutputTokens),
		"estimated_cost":             nullString(req.EstimatedCost),
		"currency":                   currencyOrDefault(req.Currency),
		"error_code":                 strings.TrimSpace(req.ErrorCode),
		"error_message":              strings.TrimSpace(req.ErrorMessage),
		"upstream_status":            req.UpstreamStatus,
		"upstream_error_code":        strings.TrimSpace(req.UpstreamErrorCode),
		"request_snapshot":           requestSnapshot,
		"response_snapshot":          nullIfJSONNull(responseSnapshot),
		"normalized_output":          nullIfJSONNull(normalizedOutput),
		"artifact_ids":               artifactIDs,
		"media_file_ids":             mediaFileIDs,
		"requested_duration_seconds": nullFloat(req.RequestedDurationSeconds),
		"actual_duration_seconds":    nullFloat(req.ActualDurationSeconds),
		"media_probe":                mediaProbe,
		"billing_context_id":         strings.TrimSpace(req.BillingContextID),
		"provider_external_log_id":   strings.TrimSpace(req.ProviderExternalLogID),
	})
	return scanCallLog(row)
}

func scanCallLog(row rowScanner) (CallLog, error) {
	var item CallLog
	var providerRequestID sql.NullString
	var billingContextID, providerExternalLogID sql.NullString
	var projectID, productionGenerationID, workflowRunID, nodeRunID, providerModelID, credentialID sql.NullString
	var modelProfileID, modelProfileBindingID, modelProfileKey sql.NullString
	var errorCode, errorMessage, upstreamErrorCode sql.NullString
	var estimatedCost, currency sql.NullString
	var latencyMS, inputTokens, outputTokens, upstreamStatus sql.NullInt64
	var requestedDurationSeconds, actualDurationSeconds sql.NullFloat64
	var requestSnapshot, responseSnapshot, normalizedOutput, artifactIDs, mediaFileIDs, mediaProbe []byte
	var startedAt, completedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&providerRequestID,
		&item.AttemptGeneration,
		&item.AttemptSequence,
		&item.OrganizationID,
		&projectID,
		&productionGenerationID,
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
		&requestedDurationSeconds,
		&actualDurationSeconds,
		&mediaProbe,
		&item.CreatedAt,
		&startedAt,
		&completedAt,
		&billingContextID,
		&providerExternalLogID,
	)
	item.ProviderRequestID = stringPtr(providerRequestID)
	item.ProjectID = stringPtr(projectID)
	item.ProductionGenerationID = stringPtr(productionGenerationID)
	item.WorkflowRunID = stringPtr(workflowRunID)
	item.NodeRunID = stringPtr(nodeRunID)
	item.ProviderModelID = stringPtr(providerModelID)
	item.CredentialID = stringPtr(credentialID)
	item.BillingContextID = stringPtr(billingContextID)
	item.ProviderExternalLogID = stringPtr(providerExternalLogID)
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
	if requestedDurationSeconds.Valid {
		item.RequestedDurationSeconds = &requestedDurationSeconds.Float64
	}
	if actualDurationSeconds.Valid {
		item.ActualDurationSeconds = &actualDurationSeconds.Float64
	}
	item.MediaProbe = rawOrDefault(mediaProbe, "{}")
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
	if standard, ok := StandardErrorFromError(err); ok {
		if standard.UpstreamStatus > 0 {
			value := standard.UpstreamStatus
			upstreamStatus = &value
		}
		return status, standard.Code, standard.Message, upstreamStatus, standard.UpstreamCode
	}
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		standard := NormalizeUpstreamError(upstreamErr)
		statusValue := upstreamErr.Status
		return status, standard.Code, standard.Message, &statusValue, upstreamErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status, CodeUpstreamTimeout, "provider request timed out", nil, ""
	}
	if isProviderTransportTimeout(err) {
		return status, CodeUpstreamTimeout, "provider request timed out", nil, ""
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return status, CodeUpstreamStreamTruncated, "provider stream ended before a completion marker", nil, ""
	}
	if containsUpstreamCompletionStreamDisconnect(err.Error()) {
		return status, CodeUpstreamStreamTruncated, "provider stream ended before a completion marker", nil, ""
	}
	if errors.Is(err, ErrValidation) {
		return status, CodeInvalidRequest, err.Error(), nil, ""
	}
	if isTransientProviderTransportError(err) {
		return status, CodeUpstreamInternalError, "provider connection was interrupted", nil, ""
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

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
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
