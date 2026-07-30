package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestManagedProviderCredentialLifecycleAndTenantIsolation(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/models" {
				http.NotFound(writer, request)
				return
			}
			if request.Header.Get("Authorization") !=
				"Bearer managed-credential-test-secret-one" {
				http.Error(writer, "unauthorized", http.StatusUnauthorized)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"data": []map[string]string{{
					"id": "managed-text-model",
				}},
			}); err != nil {
				t.Errorf("encode model response: %v", err)
			}
		},
	))
	defer modelServer.Close()

	ctx, pool, vault := openProviderAdminTestDB(t)
	organizationID, userID, modelID := seedGatewayIntegrationData(
		t,
		ctx,
		pool,
		vault,
		modelServer.URL,
	)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	var tenantAccountID, connectorKey string
	if err := pool.QueryRow(ctx, `
		SELECT model.provider_account_id::text, connector.connector_key
		FROM provider_models model
		JOIN provider_accounts account ON account.id = model.provider_account_id
		JOIN provider_connectors connector ON connector.id = account.connector_id
		WHERE model.id = $1
	`, modelID).Scan(&tenantAccountID, &connectorKey); err != nil {
		t.Fatalf("load tenant Provider fixture: %v", err)
	}
	service := NewService(pool, vault)
	accountRequest := EnsureManagedProviderAccountRequest{
		OrganizationID:      organizationID,
		CreatedByUserID:     userID,
		ManagementReference: "billing-authority:test:billing-account:" + uuid.NewString(),
		Name:                "系统计费账户",
		ConnectorKey:        connectorKey,
		BaseURL:             modelServer.URL + "/v1",
		AuthType:            "bearer",
	}
	managedAccount, err := service.EnsureManagedProviderAccount(ctx, accountRequest)
	if err != nil {
		t.Fatalf("ensure managed Provider account: %v", err)
	}
	replayedAccount, err := service.EnsureManagedProviderAccount(ctx, accountRequest)
	if err != nil {
		t.Fatalf("replay managed Provider account: %v", err)
	}
	if replayedAccount != managedAccount {
		t.Fatalf("replayed account = %#v, want %#v", replayedAccount, managedAccount)
	}
	conflictingAccountRequest := accountRequest
	conflictingAccountRequest.Name = "冲突名称"
	if _, err := service.EnsureManagedProviderAccount(
		ctx,
		conflictingAccountRequest,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting account ensure error = %v, want ErrConflict", err)
	}

	accounts, err := service.ListAccounts(ctx, organizationID, "all", 100)
	if err != nil {
		t.Fatalf("list tenant Provider accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != tenantAccountID {
		t.Fatalf("tenant account list = %#v, want only %s", accounts, tenantAccountID)
	}
	if _, err := service.GetTenantAccount(
		ctx,
		organizationID,
		managedAccount.ID,
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("get managed account through tenant API error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := service.CreateCredential(
		ctx,
		organizationID,
		managedAccount.ID,
		userID,
		CreateCredentialRequest{
			CredentialKey: "forbidden",
			Credential:    map[string]any{"apiKey": "must-not-be-stored"},
		},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tenant mutation of managed account error = %v, want pgx.ErrNoRows", err)
	}

	firstSecret := "managed-credential-test-secret-one"
	firstImportRequest := ImportManagedCredentialRequest{
		AttemptID:            uuid.NewString(),
		OrganizationID:       organizationID,
		ProviderAccountID:    managedAccount.ID,
		CredentialKey:        "text-default",
		CredentialType:       "api_key",
		ImportIdempotencyKey: "gateway-import:" + uuid.NewString(),
		RequestHash:          strings.Repeat("a", 64),
		ManagementReference:  "billing-credential:" + uuid.NewString(),
		Credential:           map[string]any{"apiKey": firstSecret},
	}
	firstImport, err := service.ImportManagedCredential(ctx, firstImportRequest)
	if err != nil {
		t.Fatalf("import first managed credential: %v", err)
	}
	if firstImport.State != ManagedCredentialStateImportedInactive {
		t.Fatalf("first import state = %q", firstImport.State)
	}
	replayedImport, err := service.ImportManagedCredential(ctx, firstImportRequest)
	if err != nil {
		t.Fatalf("replay first managed credential import: %v", err)
	}
	if replayedImport != firstImport {
		t.Fatalf("replayed import = %#v, want %#v", replayedImport, firstImport)
	}
	resolvedImport, err := service.ResolveManagedCredential(
		ctx,
		ResolveManagedCredentialRequest{
			AttemptID:            firstImportRequest.AttemptID,
			ImportIdempotencyKey: firstImportRequest.ImportIdempotencyKey,
		},
	)
	if err != nil || resolvedImport != firstImport {
		t.Fatalf("resolved import = %#v, err=%v, want %#v", resolvedImport, err, firstImport)
	}
	var encrypted []byte
	var firstActive bool
	if err := pool.QueryRow(ctx, `
		SELECT encrypted_payload, is_active
		FROM provider_credentials
		WHERE id = $1
	`, firstImport.ProviderCredentialID).Scan(&encrypted, &firstActive); err != nil {
		t.Fatalf("load first managed credential: %v", err)
	}
	if firstActive {
		t.Fatal("sealed managed credential became routable before activation")
	}
	if bytes.Contains(encrypted, []byte(firstSecret)) {
		t.Fatal("managed credential ciphertext contains plaintext secret")
	}
	plaintext, _, err := service.credentialPayloadByID(
		ctx,
		organizationID,
		managedAccount.ID,
		firstImport.ProviderCredentialID,
	)
	if err != nil || plaintext["apiKey"] != firstSecret {
		t.Fatalf("decrypt imported credential = %#v, err=%v", plaintext, err)
	}

	firstActivation := ActivateManagedCredentialRequest{
		AttemptID:                firstImportRequest.AttemptID,
		ActivationIdempotencyKey: "gateway-activate:" + firstImportRequest.AttemptID,
		ProviderCredentialID:     firstImport.ProviderCredentialID,
		BillingCredentialID:      uuid.NewString(),
		CredentialRevision:       1,
		BillingAccountID:         uuid.NewString(),
		BillingAuthorityID:       uuid.NewString(),
		MappingHash:              strings.Repeat("b", 64),
	}
	firstActiveResult, err := service.ActivateManagedCredential(ctx, firstActivation)
	if err != nil {
		t.Fatalf("activate first managed credential: %v", err)
	}
	if firstActiveResult.State != ManagedCredentialStateActive {
		t.Fatalf("first activation state = %q", firstActiveResult.State)
	}
	if replayed, err := service.ActivateManagedCredential(
		ctx,
		firstActivation,
	); err != nil || replayed != firstActiveResult {
		t.Fatalf("replay first activation = %#v, err=%v", replayed, err)
	}
	discoveryRequest := DiscoverManagedCredentialModelsRequest{
		AttemptID:               firstImportRequest.AttemptID,
		OrganizationID:          organizationID,
		ProviderAccountID:       managedAccount.ID,
		ProviderCredentialID:    firstImport.ProviderCredentialID,
		DiscoveryIdempotencyKey: "gateway-discover:" + firstImportRequest.AttemptID,
	}
	discovery, err := service.DiscoverManagedCredentialModels(
		ctx,
		discoveryRequest,
	)
	if err != nil {
		t.Fatalf("discover managed credential models: %v", err)
	}
	if discovery.Status != "succeeded" ||
		discovery.CredentialID != firstImport.ProviderCredentialID ||
		len(discovery.Models) != 1 ||
		discovery.Models[0].ModelKey != "managed-text-model" ||
		discovery.Sync.CreatedCount != 1 {
		t.Fatalf("managed credential discovery = %#v", discovery)
	}
	var mapped bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM provider_credential_models credential_model
			JOIN provider_models model
			  ON model.id = credential_model.provider_model_id
			WHERE credential_model.provider_credential_id = $1
			  AND credential_model.is_available
			  AND model.provider_account_id = $2
			  AND model.model_key = 'managed-text-model'
			  AND model.status = 'active'
		)
	`, firstImport.ProviderCredentialID, managedAccount.ID).Scan(&mapped); err != nil {
		t.Fatalf("load managed credential model mapping: %v", err)
	}
	if !mapped {
		t.Fatal("managed credential discovery did not persist a routable model mapping")
	}
	availableModels, err := service.ListAvailableModels(ctx, organizationID)
	if err != nil {
		t.Fatalf("list available Provider models: %v", err)
	}
	if len(availableModels) != 2 {
		t.Fatalf("available models = %#v, want tenant and managed models", availableModels)
	}
	availableByKey := make(map[string]AvailableModel, len(availableModels))
	for _, model := range availableModels {
		availableByKey[model.ModelKey] = model
	}
	if availableByKey["managed-text-model"].ManagementScope != "system_managed" {
		t.Fatalf(
			"managed model scope = %q",
			availableByKey["managed-text-model"].ManagementScope,
		)
	}
	if availableByKey["gpt-integration"].ManagementScope != "tenant_managed" {
		t.Fatalf(
			"tenant model scope = %q",
			availableByKey["gpt-integration"].ManagementScope,
		)
	}

	secondImportRequest := firstImportRequest
	secondImportRequest.AttemptID = uuid.NewString()
	secondImportRequest.ImportIdempotencyKey = "gateway-import:" + uuid.NewString()
	secondImportRequest.RequestHash = strings.Repeat("c", 64)
	secondImportRequest.ManagementReference = "billing-credential:" + uuid.NewString()
	secondImportRequest.Credential = map[string]any{"apiKey": "managed-credential-test-secret-two"}
	secondImport, err := service.ImportManagedCredential(ctx, secondImportRequest)
	if err != nil {
		t.Fatalf("import replacement managed credential: %v", err)
	}
	secondActivation := firstActivation
	secondActivation.AttemptID = secondImportRequest.AttemptID
	secondActivation.ActivationIdempotencyKey = "gateway-activate:" + secondImportRequest.AttemptID
	secondActivation.ProviderCredentialID = secondImport.ProviderCredentialID
	secondActivation.BillingCredentialID = uuid.NewString()
	secondActivation.CredentialRevision = 2
	secondActivation.MappingHash = strings.Repeat("d", 64)
	if _, err := service.ActivateManagedCredential(ctx, secondActivation); err != nil {
		t.Fatalf("activate replacement managed credential: %v", err)
	}
	var oldActive, replacementActive bool
	var oldStatus, replacementStatus string
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT is_active FROM provider_credentials WHERE id = $1),
			(SELECT status FROM provider_credentials WHERE id = $1),
			(SELECT is_active FROM provider_credentials WHERE id = $2),
			(SELECT status FROM provider_credentials WHERE id = $2)
	`, firstImport.ProviderCredentialID, secondImport.ProviderCredentialID).Scan(
		&oldActive,
		&oldStatus,
		&replacementActive,
		&replacementStatus,
	); err != nil {
		t.Fatalf("load rotation state: %v", err)
	}
	if oldActive || oldStatus != "rotated" || !replacementActive || replacementStatus != "active" {
		t.Fatalf(
			"rotation state old=(%t,%s) replacement=(%t,%s)",
			oldActive,
			oldStatus,
			replacementActive,
			replacementStatus,
		)
	}

	revocation := RevokeManagedCredentialRequest{
		AttemptID:                secondImportRequest.AttemptID,
		RevocationIdempotencyKey: "gateway-revoke:" + secondImportRequest.AttemptID,
		ProviderCredentialID:     secondImport.ProviderCredentialID,
	}
	if err := service.RevokeManagedCredential(ctx, revocation); err != nil {
		t.Fatalf("revoke replacement managed credential: %v", err)
	}
	if err := service.RevokeManagedCredential(ctx, revocation); err != nil {
		t.Fatalf("replay replacement revocation: %v", err)
	}
	var revokedActive bool
	var revokedStatus string
	if err := pool.QueryRow(ctx, `
		SELECT is_active, status
		FROM provider_credentials
		WHERE id = $1
	`, secondImport.ProviderCredentialID).Scan(&revokedActive, &revokedStatus); err != nil {
		t.Fatalf("load revoked managed credential: %v", err)
	}
	if revokedActive || revokedStatus != "revoked" {
		t.Fatalf("revoked credential state = (%t,%s)", revokedActive, revokedStatus)
	}
	availableModels, err = service.ListAvailableModels(ctx, organizationID)
	if err != nil {
		t.Fatalf("list models after managed credential revocation: %v", err)
	}
	if len(availableModels) != 1 ||
		availableModels[0].ModelKey != "gpt-integration" ||
		availableModels[0].ManagementScope != "tenant_managed" {
		t.Fatalf(
			"available models after managed revocation = %#v",
			availableModels,
		)
	}
}
