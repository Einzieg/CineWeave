package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type allowOneManagedCredentialAuthorizer struct {
	credentialID string
}

func (authorizer allowOneManagedCredentialAuthorizer) Authorize(
	_ context.Context,
	request editionpkg.BillingRoutingRequest,
) (editionpkg.BillingRoutingDecision, error) {
	for _, candidate := range request.Candidates {
		if candidate.CredentialID == authorizer.credentialID {
			return editionpkg.BillingRoutingDecision{
				AllowedCredentialIDs: []string{authorizer.credentialID},
			}, nil
		}
	}
	return editionpkg.BillingRoutingDecision{}, editionpkg.AuthorizationError{
		Code:    editionpkg.DenialBillingRoutingCandidateMissing,
		Message: "credential belongs to a different billing account",
	}
}

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
	managedModel := availableByKey["managed-text-model"]
	updatedDisplayName := "Managed Text Model (configured)"
	updatedModality := "text"
	updatedManagedModel, err := service.UpdateAvailableModel(
		ctx,
		organizationID,
		managedModel.ID,
		UpdateAvailableModelRequest{
			DisplayName: &updatedDisplayName,
			Modality:    &updatedModality,
			Capabilities: &CapabilityInput{
				TaskTypes:             json.RawMessage(`["text.generate","text.stream"]`),
				InputLimits:           json.RawMessage(`{"maxInputTokens":8192}`),
				OutputLimits:          json.RawMessage(`{"maxOutputTokens":2048}`),
				QualityTiers:          json.RawMessage(`[]`),
				ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"supportsStreaming":true}}`),
				PricingPolicy:         json.RawMessage(`{}`),
			},
		},
	)
	if err != nil {
		t.Fatalf("update available managed model: %v", err)
	}
	if updatedManagedModel.DisplayName != updatedDisplayName ||
		updatedManagedModel.ModelKey != managedModel.ModelKey ||
		updatedManagedModel.Status != "active" {
		t.Fatalf("updated managed model = %#v", updatedManagedModel)
	}
	if len(updatedManagedModel.Capabilities) != 1 ||
		updatedManagedModel.Capabilities[0].Source != "manual" ||
		updatedManagedModel.Capabilities[0].ApprovalStatus != "approved" {
		t.Fatalf("updated managed model capabilities = %#v", updatedManagedModel.Capabilities)
	}
	if _, err := service.UpdateModel(ctx, organizationID, managedModel.ID, UpdateModelRequest{
		DisplayName: &updatedDisplayName,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tenant model update for managed model error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := service.syncDiscoveredModelsForCredentialWithSummary(
		ctx,
		organizationID,
		managedAccount.ID,
		firstImport.ProviderCredentialID,
		[]DiscoveredModel{{
			ModelKey:    managedModel.ModelKey,
			DisplayName: "Remote Managed Model",
			Modality:    "image",
		}},
	); err != nil {
		t.Fatalf("rediscover manually configured managed model: %v", err)
	}
	rediscoveredManagedModel, err := service.GetModel(ctx, organizationID, managedModel.ID)
	if err != nil {
		t.Fatalf("load rediscovered managed model: %v", err)
	}
	if rediscoveredManagedModel.DisplayName != updatedDisplayName || rediscoveredManagedModel.Modality != updatedModality {
		t.Fatalf(
			"rediscovered managed model metadata = (%q,%q), want manual (%q,%q)",
			rediscoveredManagedModel.DisplayName,
			rediscoveredManagedModel.Modality,
			updatedDisplayName,
			updatedModality,
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
	if _, err := service.UpdateAvailableModel(
		ctx,
		organizationID,
		managedModel.ID,
		UpdateAvailableModelRequest{DisplayName: &updatedDisplayName},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("update unavailable managed model error = %v, want pgx.ErrNoRows", err)
	}
}

func TestAvailableManagedModelsAreLogicalAndResolvePerBillingAccount(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	organizationID, userID, tenantModelID := seedGatewayIntegrationData(
		t,
		ctx,
		pool,
		vault,
		"http://example.test",
	)
	var connectorID string
	if err := pool.QueryRow(ctx, `
		SELECT account.connector_id::text
		FROM provider_models model
		JOIN provider_accounts account ON account.id = model.provider_account_id
		WHERE model.id = $1
	`, tenantModelID).Scan(&connectorID); err != nil {
		t.Fatalf("load connector: %v", err)
	}

	fixtures := make([]managedModelFixture, 0, 3)
	for index, hashCharacter := range []string{"a", "b", "c"} {
		fixtures = append(fixtures, insertManagedAvailableModelFixture(
			t,
			ctx,
			pool,
			vault,
			organizationID,
			userID,
			connectorID,
			index,
			hashCharacter,
		))
	}

	service := NewService(pool, vault)
	available, err := service.ListAvailableModels(ctx, organizationID)
	if err != nil {
		t.Fatalf("list available models: %v", err)
	}
	omniCount := 0
	var logicalOmni AvailableModel
	for _, model := range available {
		if model.ModelKey != "omni-fast-no-water" {
			continue
		}
		omniCount++
		logicalOmni = model
		if model.Modality != "video" {
			t.Fatalf("logical omni modality = %q, want video", model.Modality)
		}
		if !availableModelSupportsTaskType(model, TaskTypeVideoCreateTask) {
			t.Fatalf("logical omni capabilities = %#v, want video.create_task", model.Capabilities)
		}
	}
	if omniCount != 1 {
		t.Fatalf("logical omni model count = %d, want 1; models=%#v", omniCount, available)
	}
	variants, err := ExecutableVideoGenerationVariants(logicalOmni.Capabilities, Model{
		ID:           logicalOmni.ID,
		ModelKey:     logicalOmni.ModelKey,
		Modality:     logicalOmni.Modality,
		Status:       logicalOmni.Status,
		Capabilities: logicalOmni.Capabilities,
	})
	if err != nil {
		t.Fatalf("parse logical omni video variants: %v", err)
	}
	if len(variants) != 1 {
		t.Fatalf("logical omni variants = %#v, want one executable variant", variants)
	}
	durations, err := ExecutableWholeSecondDurationsForVideoVariant(variants[0])
	if err != nil {
		t.Fatalf("load logical omni durations: %v", err)
	}
	if len(durations) != 1 || durations[0] != 10 {
		t.Fatalf("logical omni durations = %#v, want [10]", durations)
	}

	var correctedRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM provider_models model
		JOIN provider_accounts account
		  ON account.id = model.provider_account_id
		JOIN provider_model_capabilities capability
		  ON capability.provider_model_id = model.id
		WHERE account.organization_id = $1
		  AND model.model_key = 'omni-fast-no-water'
		  AND model.modality = 'video'
		  AND capability.task_types ? 'video.create_task'
		  AND capability.provider_options_schema #>> '{xCapabilities,capabilitySource}' = 'preset'
	`, organizationID).Scan(&correctedRows); err != nil {
		t.Fatalf("load corrected managed models: %v", err)
	}
	if correctedRows != 3 {
		t.Fatalf("corrected managed model rows = %d, want 3", correctedRows)
	}

	service.SetBillingRoutingAuthorizer(allowOneManagedCredentialAuthorizer{
		credentialID: fixtures[1].credentialID,
	})
	selection, err := service.completeGatewaySelectionFromCandidateWithBilling(
		ctx,
		organizationID,
		uuid.NewString(),
		GatewayBillingIdentity{BillingContextID: uuid.NewString()},
		RoutingCandidate{
			ModelProfileID:        uuid.NewString(),
			ModelProfileBindingID: uuid.NewString(),
			ModelProfileKey:       "video_generation_default",
			ProviderModelID:       fixtures[0].modelID,
			ProviderAccountID:     fixtures[0].accountID,
			ModelKey:              "omni-fast-no-water",
			Modality:              "video",
		},
	)
	if err != nil {
		t.Fatalf("resolve logical model for billing account: %v", err)
	}
	if selection.Model.ID != fixtures[1].modelID ||
		selection.Account.ID != fixtures[1].accountID ||
		selection.CredentialID != fixtures[1].credentialID {
		t.Fatalf(
			"billing selection = model %s account %s credential %s, want %s/%s/%s",
			selection.Model.ID,
			selection.Account.ID,
			selection.CredentialID,
			fixtures[1].modelID,
			fixtures[1].accountID,
			fixtures[1].credentialID,
		)
	}
	firstModel, err := service.GetModel(ctx, organizationID, fixtures[0].modelID)
	if err != nil {
		t.Fatalf("load first logical video model: %v", err)
	}
	firstAccount, err := service.GetAccount(ctx, organizationID, fixtures[0].accountID)
	if err != nil {
		t.Fatalf("load first logical video account: %v", err)
	}
	videoCandidates, err := service.filterVideoPlanBillingCandidates(
		ctx,
		GatewayVideoPlanRequest{
			OrganizationID: organizationID,
			ProjectID:      uuid.NewString(),
			GatewayBillingIdentity: GatewayBillingIdentity{
				BillingContextID: uuid.NewString(),
			},
		},
		[]resolvedVideoPlanCandidate{{
			ProviderAccountID: fixtures[0].accountID,
			Account:           firstAccount,
			Model:             firstModel,
		}},
	)
	if err != nil {
		t.Fatalf("resolve logical video plan model for billing account: %v", err)
	}
	if len(videoCandidates) != 1 || videoCandidates[0].Model.ID != fixtures[1].modelID {
		t.Fatalf("video plan candidates = %#v, want model %s", videoCandidates, fixtures[1].modelID)
	}
}

type managedModelFixture struct {
	accountID    string
	credentialID string
	modelID      string
}

func insertManagedAvailableModelFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	vault *Vault,
	organizationID string,
	userID string,
	connectorID string,
	index int,
	hashCharacter string,
) managedModelFixture {
	t.Helper()
	fixture := managedModelFixture{}
	managementReference := "billing-authority:test:billing-account:" + uuid.NewString()
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(
			organization_id, connector_id, name, base_url, auth_type, status, config, created_by
		)
		VALUES ($1, $2, $3, 'http://example.test/v1', 'bearer', 'active', '{}', $4)
		RETURNING id::text
	`, organizationID, connectorID, fmt.Sprintf("Managed account %d", index), userID).Scan(&fixture.accountID); err != nil {
		t.Fatalf("insert managed account %d: %v", index, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_managed_accounts(
			provider_account_id, organization_id, management_scope,
			management_reference, ensure_request_hash
		)
		VALUES ($1, $2, 'system_managed', $3, $4)
	`, fixture.accountID, organizationID, managementReference, strings.Repeat(hashCharacter, 64)); err != nil {
		t.Fatalf("insert managed account metadata %d: %v", index, err)
	}
	encrypted, err := vault.EncryptJSON(map[string]any{"apiKey": fmt.Sprintf("managed-secret-%d", index)})
	if err != nil {
		t.Fatalf("encrypt managed credential %d: %v", index, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_credentials(
			organization_id, provider_account_id, credential_key, credential_type,
			secret_ref, encrypted_payload, masked_preview, status, is_active, created_by
		)
		VALUES ($1, $2, $3, 'api_key', 'local:aes-gcm:v1', $4, '***test', 'active', true, $5)
		RETURNING id::text
	`, organizationID, fixture.accountID, fmt.Sprintf("managed-%d", index), encrypted, userID).Scan(&fixture.credentialID); err != nil {
		t.Fatalf("insert managed credential %d: %v", index, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_managed_credentials(
			provider_credential_id, organization_id, provider_account_id,
			management_scope, management_reference
		)
		VALUES ($1, $2, $3, 'system_managed', $4)
	`, fixture.credentialID, organizationID, fixture.accountID, "billing-credential:"+uuid.NewString()); err != nil {
		t.Fatalf("insert managed credential metadata %d: %v", index, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'omni-fast-no-water', 'omni-fast-no-water', 'text', 'active')
		RETURNING id::text
	`, fixture.accountID).Scan(&fixture.modelID); err != nil {
		t.Fatalf("insert managed model %d: %v", index, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits,
			quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES (
			$1, '["text.generate","text.stream"]', '{}', '{}', '[]',
			'{"xCapabilities":{"capabilitySource":"inferred","capabilityApprovalStatus":"inferred"}}',
			'{}'
		)
	`, fixture.modelID); err != nil {
		t.Fatalf("insert managed model capability %d: %v", index, err)
	}
	return fixture
}

func availableModelSupportsTaskType(model AvailableModel, taskType string) bool {
	for _, capability := range model.Capabilities {
		if containsNormalizedString(stringsFromRawJSON(capability.TaskTypes), taskType) {
			return true
		}
	}
	return false
}
