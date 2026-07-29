package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	ManagementScopeSystemManaged = "system_managed"

	ManagedCredentialStateImportedInactive = "imported_inactive"
	ManagedCredentialStateActive           = "active"
	ManagedCredentialStateRevoked          = "revoked"
	ManagedCredentialStateQuarantined      = "quarantined"
)

type EnsureManagedProviderAccountRequest struct {
	OrganizationID      string          `json:"organizationId"`
	CreatedByUserID     string          `json:"createdByUserId"`
	ManagementReference string          `json:"managementReference"`
	Name                string          `json:"name"`
	ConnectorKey        string          `json:"connectorKey"`
	BaseURL             string          `json:"baseUrl"`
	AuthType            string          `json:"authType"`
	Config              json.RawMessage `json:"config,omitempty"`
}

type ManagedProviderAccountResult struct {
	ID                  string `json:"id"`
	OrganizationID      string `json:"organizationId"`
	ManagementReference string `json:"managementReference"`
}

type ImportManagedCredentialRequest struct {
	AttemptID            string         `json:"attemptId"`
	OrganizationID       string         `json:"organizationId"`
	ProviderAccountID    string         `json:"providerAccountId"`
	CredentialKey        string         `json:"credentialKey"`
	CredentialType       string         `json:"credentialType"`
	ImportIdempotencyKey string         `json:"importIdempotencyKey"`
	RequestHash          string         `json:"requestHash"`
	ManagementReference  string         `json:"managementReference"`
	Credential           map[string]any `json:"credential"`
}

type ResolveManagedCredentialRequest struct {
	AttemptID            string `json:"attemptId"`
	ImportIdempotencyKey string `json:"importIdempotencyKey"`
}

type ActivateManagedCredentialRequest struct {
	AttemptID                string `json:"attemptId"`
	ActivationIdempotencyKey string `json:"activationIdempotencyKey"`
	ProviderCredentialID     string `json:"providerCredentialId"`
	BillingCredentialID      string `json:"billingCredentialId"`
	CredentialRevision       int64  `json:"credentialRevision"`
	BillingAccountID         string `json:"billingAccountId"`
	BillingAuthorityID       string `json:"billingAuthorityId"`
	MappingHash              string `json:"mappingHash"`
}

type RevokeManagedCredentialRequest struct {
	AttemptID                string `json:"attemptId"`
	RevocationIdempotencyKey string `json:"revocationIdempotencyKey"`
	ProviderCredentialID     string `json:"providerCredentialId"`
}

type ManagedCredentialResult struct {
	State                       string `json:"state"`
	ImportID                    string `json:"importId"`
	ProviderCredentialID        string `json:"providerCredentialId"`
	ProviderCredentialReference string `json:"providerCredentialReference"`
}

type managedCredentialImport struct {
	ID                          string
	OrganizationID              string
	AttemptID                   string
	ImportIdempotencyKey        string
	LocalRequestHash            string
	ProviderAccountID           string
	ProviderCredentialID        string
	ProviderCredentialReference string
	CredentialKey               string
	Status                      string
	ActivationRequestHash       string
	RevocationRequestHash       string
}

func (s *Service) EnsureManagedProviderAccount(
	ctx context.Context,
	request EnsureManagedProviderAccountRequest,
) (ManagedProviderAccountResult, error) {
	normalized, requestHash, err := normalizeManagedProviderAccountRequest(request)
	if err != nil {
		return ManagedProviderAccountResult{}, err
	}
	if s == nil || s.db == nil {
		return ManagedProviderAccountResult{}, errors.New("provider database is not initialized")
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ManagedProviderAccountResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		normalized.OrganizationID+":"+normalized.ManagementReference,
	); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	existing, existingHash, found, err := findManagedProviderAccount(
		ctx,
		tx,
		normalized.OrganizationID,
		normalized.ManagementReference,
	)
	if err != nil {
		return ManagedProviderAccountResult{}, err
	}
	if found {
		if existingHash != requestHash {
			return ManagedProviderAccountResult{}, fmt.Errorf(
				"%w: managed Provider account reference was reused with a different request",
				ErrConflict,
			)
		}
		if err := tx.Commit(ctx); err != nil {
			return ManagedProviderAccountResult{}, err
		}
		return existing, nil
	}

	var connectorID string
	if err := tx.QueryRow(
		ctx,
		`SELECT id::text FROM provider_connectors WHERE connector_key = $1`,
		normalized.ConnectorKey,
	).Scan(&connectorID); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	var actorExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE id = $1
			  AND status = 'active'
		)
	`, normalized.CreatedByUserID).Scan(&actorExists); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	if !actorExists {
		return ManagedProviderAccountResult{}, fmt.Errorf(
			"%w: createdByUserId must identify an active CineWeave user",
			ErrValidation,
		)
	}
	var accountID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_accounts(
			organization_id,
			connector_id,
			name,
			base_url,
			auth_type,
			status,
			config,
			created_by
		)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, $7)
		RETURNING id::text
	`,
		normalized.OrganizationID,
		connectorID,
		normalized.Name,
		nullIfBlank(normalized.BaseURL),
		normalized.AuthType,
		normalized.Config,
		normalized.CreatedByUserID,
	).Scan(&accountID); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provider_managed_accounts(
			provider_account_id,
			organization_id,
			management_scope,
			management_reference,
			ensure_request_hash
		)
		VALUES ($1, $2, 'system_managed', $3, $4)
	`, accountID, normalized.OrganizationID, normalized.ManagementReference, requestHash); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	return ManagedProviderAccountResult{
		ID:                  accountID,
		OrganizationID:      normalized.OrganizationID,
		ManagementReference: normalized.ManagementReference,
	}, nil
}

func (s *Service) ImportManagedCredential(
	ctx context.Context,
	request ImportManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	normalized, localRequestHash, secretFingerprint, err := normalizeManagedCredentialImportRequest(request)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	if s == nil || s.db == nil || s.vault == nil {
		return ManagedCredentialResult{}, errors.New("provider credential import dependencies are not initialized")
	}
	encrypted, err := s.vault.EncryptJSON(normalized.Credential)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		normalized.ImportIdempotencyKey,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	existing, found, err := loadManagedCredentialImportByKey(
		ctx,
		tx,
		normalized.ImportIdempotencyKey,
	)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	if found {
		if existing.LocalRequestHash != localRequestHash ||
			existing.AttemptID != normalized.AttemptID {
			return ManagedCredentialResult{}, fmt.Errorf(
				"%w: managed credential import idempotency key was reused with a different request",
				ErrConflict,
			)
		}
		if err := tx.Commit(ctx); err != nil {
			return ManagedCredentialResult{}, err
		}
		return existing.result(), nil
	}

	var accountReference string
	if err := tx.QueryRow(ctx, `
		SELECT managed.management_reference
		FROM provider_managed_accounts managed
		JOIN provider_accounts account
		  ON account.id = managed.provider_account_id
		WHERE managed.organization_id = $1
		  AND managed.provider_account_id = $2
		  AND managed.management_scope = 'system_managed'
		  AND account.status = 'active'
		FOR UPDATE OF managed, account
	`, normalized.OrganizationID, normalized.ProviderAccountID).Scan(&accountReference); err != nil {
		return ManagedCredentialResult{}, err
	}
	var credentialID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_credentials(
			organization_id,
			provider_account_id,
			credential_key,
			credential_type,
			secret_ref,
			encrypted_payload,
			masked_preview,
			status,
			is_active,
			created_by
		)
		VALUES (
			$1, $2, $3, $4, 'local:aes-gcm:v1', $5, $6,
			'active', false, NULL
		)
		RETURNING id::text
	`,
		normalized.OrganizationID,
		normalized.ProviderAccountID,
		normalized.CredentialKey,
		normalized.CredentialType,
		encrypted,
		MaskCredentialPayload(normalized.Credential),
	).Scan(&credentialID); err != nil {
		return ManagedCredentialResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provider_managed_credentials(
			provider_credential_id,
			organization_id,
			provider_account_id,
			management_scope,
			management_reference
		)
		VALUES ($1, $2, $3, 'system_managed', $4)
	`,
		credentialID,
		normalized.OrganizationID,
		normalized.ProviderAccountID,
		normalized.ManagementReference,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	var importID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_credential_imports(
			organization_id,
			attempt_id,
			import_idempotency_key,
			local_request_hash,
			upstream_request_hash,
			provider_account_id,
			provider_credential_id,
			provider_credential_reference,
			credential_key,
			credential_type,
			secret_fingerprint,
			status
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			'imported_inactive'
		)
		RETURNING id::text
	`,
		normalized.OrganizationID,
		normalized.AttemptID,
		normalized.ImportIdempotencyKey,
		localRequestHash,
		normalized.RequestHash,
		normalized.ProviderAccountID,
		credentialID,
		normalized.ManagementReference,
		normalized.CredentialKey,
		normalized.CredentialType,
		secretFingerprint,
	).Scan(&importID); err != nil {
		return ManagedCredentialResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedCredentialResult{}, err
	}
	return ManagedCredentialResult{
		State:                       ManagedCredentialStateImportedInactive,
		ImportID:                    importID,
		ProviderCredentialID:        credentialID,
		ProviderCredentialReference: normalized.ManagementReference,
	}, nil
}

func (s *Service) ResolveManagedCredential(
	ctx context.Context,
	request ResolveManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	attemptID := strings.TrimSpace(request.AttemptID)
	idempotencyKey := strings.TrimSpace(request.ImportIdempotencyKey)
	if attemptID == "" || idempotencyKey == "" {
		return ManagedCredentialResult{}, fmt.Errorf(
			"%w: attemptId and importIdempotencyKey are required",
			ErrValidation,
		)
	}
	row := s.db.QueryRow(ctx, managedCredentialImportSelect(`
		WHERE attempt_id = $1
		  AND import_idempotency_key = $2
	`), attemptID, idempotencyKey)
	item, err := scanManagedCredentialImport(row)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	return item.result(), nil
}

func (s *Service) ActivateManagedCredential(
	ctx context.Context,
	request ActivateManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	normalized, activationHash, err := normalizeManagedCredentialActivationRequest(request)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		normalized.AttemptID,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	item, err := loadManagedCredentialImportForUpdate(ctx, tx, normalized.AttemptID)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	if item.ProviderCredentialID != normalized.ProviderCredentialID {
		return ManagedCredentialResult{}, fmt.Errorf(
			"%w: activation Provider credential does not match the import",
			ErrConflict,
		)
	}
	if item.Status == ManagedCredentialStateActive {
		if item.ActivationRequestHash != activationHash {
			return ManagedCredentialResult{}, fmt.Errorf(
				"%w: managed credential activation was replayed with a different mapping",
				ErrConflict,
			)
		}
		if err := tx.Commit(ctx); err != nil {
			return ManagedCredentialResult{}, err
		}
		return item.result(), nil
	}
	if item.Status != ManagedCredentialStateImportedInactive {
		return ManagedCredentialResult{}, fmt.Errorf(
			"%w: managed credential cannot be activated from state %s",
			ErrConflict,
			item.Status,
		)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE provider_credentials existing
		SET
			is_active = false,
			status = 'rotated',
			rotated_at = COALESCE(rotated_at, now())
		FROM provider_managed_credentials managed
		WHERE managed.provider_credential_id = existing.id
		  AND managed.provider_account_id = $1
		  AND existing.credential_key = $2
		  AND existing.id <> $3
		  AND existing.is_active = true
		  AND existing.status = 'active'
	`, item.ProviderAccountID, item.CredentialKey, item.ProviderCredentialID); err != nil {
		return ManagedCredentialResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE provider_credentials credential
		SET
			is_active = true,
			status = 'active'
		FROM provider_managed_credentials managed
		WHERE managed.provider_credential_id = credential.id
		  AND credential.id = $1
		  AND managed.organization_id = $2
		  AND managed.provider_account_id = $3
	`, item.ProviderCredentialID, item.OrganizationID, item.ProviderAccountID)
	if err != nil {
		return ManagedCredentialResult{}, err
	}
	if tag.RowsAffected() != 1 {
		return ManagedCredentialResult{}, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_credential_imports
		SET
			status = 'active',
			activation_request_hash = $2,
			activated_at = COALESCE(activated_at, now()),
			updated_at = now()
		WHERE id = $1
	`, item.ID, activationHash); err != nil {
		return ManagedCredentialResult{}, err
	}
	item.Status = ManagedCredentialStateActive
	item.ActivationRequestHash = activationHash
	if err := tx.Commit(ctx); err != nil {
		return ManagedCredentialResult{}, err
	}
	return item.result(), nil
}

func (s *Service) RevokeManagedCredential(
	ctx context.Context,
	request RevokeManagedCredentialRequest,
) error {
	normalized, revocationHash, err := normalizeManagedCredentialRevocationRequest(request)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		normalized.AttemptID,
	); err != nil {
		return err
	}
	item, err := loadManagedCredentialImportForUpdate(ctx, tx, normalized.AttemptID)
	if err != nil {
		return err
	}
	if item.ProviderCredentialID != normalized.ProviderCredentialID {
		return fmt.Errorf(
			"%w: revocation Provider credential does not match the import",
			ErrConflict,
		)
	}
	if item.Status == ManagedCredentialStateRevoked {
		if item.RevocationRequestHash != revocationHash {
			return fmt.Errorf(
				"%w: managed credential revocation was replayed with a different request",
				ErrConflict,
			)
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_credentials credential
		SET
			is_active = false,
			status = 'revoked'
		FROM provider_managed_credentials managed
		WHERE managed.provider_credential_id = credential.id
		  AND credential.id = $1
		  AND managed.organization_id = $2
	`, item.ProviderCredentialID, item.OrganizationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_credential_imports
		SET
			status = 'revoked',
			revocation_request_hash = $2,
			revoked_at = COALESCE(revoked_at, now()),
			updated_at = now()
		WHERE id = $1
	`, item.ID, revocationHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) GetTenantAccount(
	ctx context.Context,
	organizationID string,
	accountID string,
) (Account, error) {
	row := s.db.QueryRow(ctx, accountSelect(`
		WHERE a.organization_id = $1
		  AND a.id = $2
		  AND NOT EXISTS (
		      SELECT 1
		      FROM provider_managed_accounts managed
		      WHERE managed.provider_account_id = a.id
		        AND managed.management_scope = 'system_managed'
		  )
	`), organizationID, accountID)
	return scanAccount(row)
}

func (s *Service) EnsureTenantProviderModel(
	ctx context.Context,
	organizationID string,
	modelID string,
) error {
	var exists bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM provider_models model
			JOIN provider_accounts account
			  ON account.id = model.provider_account_id
			WHERE account.organization_id = $1
			  AND model.id = $2
			  AND NOT EXISTS (
			      SELECT 1
			      FROM provider_managed_accounts managed
			      WHERE managed.provider_account_id = account.id
			        AND managed.management_scope = 'system_managed'
			  )
		)
	`, organizationID, modelID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

func normalizeManagedProviderAccountRequest(
	request EnsureManagedProviderAccountRequest,
) (EnsureManagedProviderAccountRequest, string, error) {
	request.OrganizationID = strings.TrimSpace(request.OrganizationID)
	request.CreatedByUserID = strings.TrimSpace(request.CreatedByUserID)
	request.ManagementReference = strings.TrimSpace(request.ManagementReference)
	request.Name = strings.TrimSpace(request.Name)
	request.ConnectorKey = strings.TrimSpace(request.ConnectorKey)
	request.AuthType = strings.TrimSpace(request.AuthType)
	if request.AuthType == "" {
		request.AuthType = "bearer"
	}
	baseURL, err := normalizeBaseURL(request.BaseURL)
	if err != nil {
		return EnsureManagedProviderAccountRequest{}, "", err
	}
	if baseURL.Valid {
		request.BaseURL = baseURL.String
	} else {
		request.BaseURL = ""
	}
	request.Config, err = normalizeJSON(request.Config, "{}")
	if err != nil {
		return EnsureManagedProviderAccountRequest{}, "", fmt.Errorf(
			"%w: config must be valid JSON",
			ErrValidation,
		)
	}
	request.Config, err = canonicalizeRawJSON(request.Config)
	if err != nil {
		return EnsureManagedProviderAccountRequest{}, "", fmt.Errorf(
			"%w: config must be valid JSON",
			ErrValidation,
		)
	}
	if request.OrganizationID == "" ||
		request.CreatedByUserID == "" ||
		request.ManagementReference == "" ||
		request.Name == "" ||
		request.ConnectorKey == "" {
		return EnsureManagedProviderAccountRequest{}, "", fmt.Errorf(
			"%w: organizationId, createdByUserId, managementReference, name, and connectorKey are required",
			ErrValidation,
		)
	}
	if len([]rune(request.ManagementReference)) > 240 {
		return EnsureManagedProviderAccountRequest{}, "", fmt.Errorf(
			"%w: managementReference is too long",
			ErrValidation,
		)
	}
	if _, err := providerMediaRequestPolicy(request.Config); err != nil {
		return EnsureManagedProviderAccountRequest{}, "", err
	}
	hash, err := hashCanonicalJSON(struct {
		SchemaVersion       string          `json:"schemaVersion"`
		OrganizationID      string          `json:"organizationId"`
		CreatedByUserID     string          `json:"createdByUserId"`
		ManagementReference string          `json:"managementReference"`
		Name                string          `json:"name"`
		ConnectorKey        string          `json:"connectorKey"`
		BaseURL             string          `json:"baseUrl"`
		AuthType            string          `json:"authType"`
		Config              json.RawMessage `json:"config"`
	}{
		SchemaVersion:       "cineweave.managed-provider-account.v1",
		OrganizationID:      request.OrganizationID,
		CreatedByUserID:     request.CreatedByUserID,
		ManagementReference: request.ManagementReference,
		Name:                request.Name,
		ConnectorKey:        request.ConnectorKey,
		BaseURL:             request.BaseURL,
		AuthType:            request.AuthType,
		Config:              request.Config,
	})
	return request, hash, err
}

func normalizeManagedCredentialImportRequest(
	request ImportManagedCredentialRequest,
) (ImportManagedCredentialRequest, string, string, error) {
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.OrganizationID = strings.TrimSpace(request.OrganizationID)
	request.ProviderAccountID = strings.TrimSpace(request.ProviderAccountID)
	request.CredentialKey = strings.TrimSpace(request.CredentialKey)
	request.CredentialType = strings.TrimSpace(request.CredentialType)
	request.ImportIdempotencyKey = strings.TrimSpace(request.ImportIdempotencyKey)
	request.RequestHash = strings.ToLower(strings.TrimSpace(request.RequestHash))
	request.ManagementReference = strings.TrimSpace(request.ManagementReference)
	if request.CredentialType == "" {
		request.CredentialType = "api_key"
	}
	if request.AttemptID == "" ||
		request.OrganizationID == "" ||
		request.ProviderAccountID == "" ||
		request.CredentialKey == "" ||
		request.ImportIdempotencyKey == "" ||
		request.ManagementReference == "" ||
		len(request.Credential) == 0 {
		return ImportManagedCredentialRequest{}, "", "", fmt.Errorf(
			"%w: managed credential import fields are incomplete",
			ErrValidation,
		)
	}
	if !isSHA256Hex(request.RequestHash) {
		return ImportManagedCredentialRequest{}, "", "", fmt.Errorf(
			"%w: requestHash must be a lowercase SHA-256 digest",
			ErrValidation,
		)
	}
	if len([]rune(request.CredentialKey)) > 120 ||
		len([]rune(request.ManagementReference)) > 240 {
		return ImportManagedCredentialRequest{}, "", "", fmt.Errorf(
			"%w: managed credential identity is too long",
			ErrValidation,
		)
	}
	credentialJSON, err := json.Marshal(request.Credential)
	if err != nil {
		return ImportManagedCredentialRequest{}, "", "", fmt.Errorf(
			"%w: credential must be valid JSON",
			ErrValidation,
		)
	}
	secretDigest := sha256.Sum256(credentialJSON)
	secretFingerprint := hex.EncodeToString(secretDigest[:])
	localHash, err := hashCanonicalJSON(struct {
		SchemaVersion        string `json:"schemaVersion"`
		AttemptID            string `json:"attemptId"`
		OrganizationID       string `json:"organizationId"`
		ProviderAccountID    string `json:"providerAccountId"`
		CredentialKey        string `json:"credentialKey"`
		CredentialType       string `json:"credentialType"`
		ImportIdempotencyKey string `json:"importIdempotencyKey"`
		UpstreamRequestHash  string `json:"upstreamRequestHash"`
		ManagementReference  string `json:"managementReference"`
		SecretFingerprint    string `json:"secretFingerprint"`
	}{
		SchemaVersion:        "cineweave.managed-provider-credential-import.v1",
		AttemptID:            request.AttemptID,
		OrganizationID:       request.OrganizationID,
		ProviderAccountID:    request.ProviderAccountID,
		CredentialKey:        request.CredentialKey,
		CredentialType:       request.CredentialType,
		ImportIdempotencyKey: request.ImportIdempotencyKey,
		UpstreamRequestHash:  request.RequestHash,
		ManagementReference:  request.ManagementReference,
		SecretFingerprint:    secretFingerprint,
	})
	return request, localHash, secretFingerprint, err
}

func normalizeManagedCredentialActivationRequest(
	request ActivateManagedCredentialRequest,
) (ActivateManagedCredentialRequest, string, error) {
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.ActivationIdempotencyKey = strings.TrimSpace(request.ActivationIdempotencyKey)
	request.ProviderCredentialID = strings.TrimSpace(request.ProviderCredentialID)
	request.BillingCredentialID = strings.TrimSpace(request.BillingCredentialID)
	request.BillingAccountID = strings.TrimSpace(request.BillingAccountID)
	request.BillingAuthorityID = strings.TrimSpace(request.BillingAuthorityID)
	request.MappingHash = strings.ToLower(strings.TrimSpace(request.MappingHash))
	if request.AttemptID == "" ||
		request.ActivationIdempotencyKey == "" ||
		request.ProviderCredentialID == "" ||
		request.BillingCredentialID == "" ||
		request.BillingAccountID == "" ||
		request.BillingAuthorityID == "" ||
		request.CredentialRevision <= 0 ||
		!isSHA256Hex(request.MappingHash) {
		return ActivateManagedCredentialRequest{}, "", fmt.Errorf(
			"%w: managed credential activation fields are incomplete",
			ErrValidation,
		)
	}
	hash, err := hashCanonicalJSON(struct {
		SchemaVersion            string `json:"schemaVersion"`
		AttemptID                string `json:"attemptId"`
		ActivationIdempotencyKey string `json:"activationIdempotencyKey"`
		ProviderCredentialID     string `json:"providerCredentialId"`
		BillingCredentialID      string `json:"billingCredentialId"`
		CredentialRevision       int64  `json:"credentialRevision"`
		BillingAccountID         string `json:"billingAccountId"`
		BillingAuthorityID       string `json:"billingAuthorityId"`
		MappingHash              string `json:"mappingHash"`
	}{
		SchemaVersion:            "cineweave.managed-provider-credential-activation.v1",
		AttemptID:                request.AttemptID,
		ActivationIdempotencyKey: request.ActivationIdempotencyKey,
		ProviderCredentialID:     request.ProviderCredentialID,
		BillingCredentialID:      request.BillingCredentialID,
		CredentialRevision:       request.CredentialRevision,
		BillingAccountID:         request.BillingAccountID,
		BillingAuthorityID:       request.BillingAuthorityID,
		MappingHash:              request.MappingHash,
	})
	return request, hash, err
}

func normalizeManagedCredentialRevocationRequest(
	request RevokeManagedCredentialRequest,
) (RevokeManagedCredentialRequest, string, error) {
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.RevocationIdempotencyKey = strings.TrimSpace(request.RevocationIdempotencyKey)
	request.ProviderCredentialID = strings.TrimSpace(request.ProviderCredentialID)
	if request.AttemptID == "" ||
		request.RevocationIdempotencyKey == "" ||
		request.ProviderCredentialID == "" {
		return RevokeManagedCredentialRequest{}, "", fmt.Errorf(
			"%w: managed credential revocation fields are incomplete",
			ErrValidation,
		)
	}
	hash, err := hashCanonicalJSON(struct {
		SchemaVersion            string `json:"schemaVersion"`
		AttemptID                string `json:"attemptId"`
		RevocationIdempotencyKey string `json:"revocationIdempotencyKey"`
		ProviderCredentialID     string `json:"providerCredentialId"`
	}{
		SchemaVersion:            "cineweave.managed-provider-credential-revocation.v1",
		AttemptID:                request.AttemptID,
		RevocationIdempotencyKey: request.RevocationIdempotencyKey,
		ProviderCredentialID:     request.ProviderCredentialID,
	})
	return request, hash, err
}

func findManagedProviderAccount(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	managementReference string,
) (ManagedProviderAccountResult, string, bool, error) {
	var result ManagedProviderAccountResult
	var requestHash string
	err := tx.QueryRow(ctx, `
		SELECT
			managed.provider_account_id::text,
			managed.organization_id::text,
			managed.management_reference,
			managed.ensure_request_hash
		FROM provider_managed_accounts managed
		JOIN provider_accounts account
		  ON account.id = managed.provider_account_id
		WHERE managed.organization_id = $1
		  AND managed.management_reference = $2
		  AND managed.management_scope = 'system_managed'
		  AND account.status = 'active'
		FOR UPDATE OF managed, account
	`, organizationID, managementReference).Scan(
		&result.ID,
		&result.OrganizationID,
		&result.ManagementReference,
		&requestHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedProviderAccountResult{}, "", false, nil
	}
	return result, requestHash, err == nil, err
}

func loadManagedCredentialImportByKey(
	ctx context.Context,
	tx pgx.Tx,
	idempotencyKey string,
) (managedCredentialImport, bool, error) {
	item, err := scanManagedCredentialImport(tx.QueryRow(
		ctx,
		managedCredentialImportSelect(`
			WHERE import_idempotency_key = $1
			FOR UPDATE
		`),
		idempotencyKey,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return managedCredentialImport{}, false, nil
	}
	return item, err == nil, err
}

func loadManagedCredentialImportForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	attemptID string,
) (managedCredentialImport, error) {
	return scanManagedCredentialImport(tx.QueryRow(
		ctx,
		managedCredentialImportSelect(`
			WHERE attempt_id = $1
			FOR UPDATE
		`),
		attemptID,
	))
}

func managedCredentialImportSelect(suffix string) string {
	return `
		SELECT
			id::text,
			organization_id::text,
			attempt_id,
			import_idempotency_key,
			local_request_hash,
			COALESCE(provider_account_id::text, ''),
			COALESCE(provider_credential_id::text, ''),
			provider_credential_reference,
			credential_key,
			status,
			COALESCE(activation_request_hash, ''),
			COALESCE(revocation_request_hash, '')
		FROM provider_credential_imports
	` + suffix
}

func scanManagedCredentialImport(row rowScanner) (managedCredentialImport, error) {
	var item managedCredentialImport
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.AttemptID,
		&item.ImportIdempotencyKey,
		&item.LocalRequestHash,
		&item.ProviderAccountID,
		&item.ProviderCredentialID,
		&item.ProviderCredentialReference,
		&item.CredentialKey,
		&item.Status,
		&item.ActivationRequestHash,
		&item.RevocationRequestHash,
	)
	return item, err
}

func (item managedCredentialImport) result() ManagedCredentialResult {
	return ManagedCredentialResult{
		State:                       item.Status,
		ImportID:                    item.ID,
		ProviderCredentialID:        item.ProviderCredentialID,
		ProviderCredentialReference: item.ProviderCredentialReference,
	}
}

func hashCanonicalJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalizeRawJSON(value json.RawMessage) (json.RawMessage, error) {
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(normalized), nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func nullIfBlank(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
