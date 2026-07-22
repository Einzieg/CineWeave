package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMultiCredentialDiscoveryRoutesModelsByCredential(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)

	var betaModelsEmpty atomic.Bool
	var requestMu sync.Mutex
	modelCredentials := map[string]string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/v1/models":
			models := []map[string]any{}
			switch credential {
			case "sk-alpha-secret":
				models = append(models,
					map[string]any{"id": "alpha-model", "object": "model"},
					map[string]any{"id": "shared-model", "object": "model"},
				)
			case "sk-beta-secret", "sk-beta-rotated":
				if !betaModelsEmpty.Load() {
					models = append(models, map[string]any{"id": "beta-model", "object": "model"})
				}
				models = append(models, map[string]any{"id": "shared-model", "object": "model"})
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "invalid key"}})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
		case "/v1/chat/completions":
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requestMu.Lock()
			modelCredentials[body.Model] = credential
			requestMu.Unlock()
			allowed := (body.Model == "alpha-model" && credential == "sk-alpha-secret") ||
				(body.Model == "beta-model" && (credential == "sk-beta-secret" || credential == "sk-beta-rotated")) ||
				(body.Model == "shared-model" && (credential == "sk-alpha-secret" || credential == "sk-beta-secret" || credential == "sk-beta-rotated"))
			if !allowed {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "model_not_found", "message": "model is not available for this key"}})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
				"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	orgID, userID, seededModelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, seededModelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account: %v", err)
	}

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	if _, err := service.RotateCredential(ctx, orgID, accountID, userID, RotateCredentialRequest{
		CredentialKey: "default",
		Credential:    map[string]any{"apiKey": "sk-alpha-secret"},
	}); err != nil {
		t.Fatalf("rotate default credential: %v", err)
	}
	alpha := mustActiveCredentialByKey(t, ctx, service, orgID, accountID, "default")
	beta, err := service.CreateCredential(ctx, orgID, accountID, userID, CreateCredentialRequest{
		CredentialKey: "beta-group",
		Credential:    map[string]any{"apiKey": "sk-beta-secret"},
	})
	if err != nil {
		t.Fatalf("create beta credential: %v", err)
	}
	if _, err := service.CreateCredential(ctx, orgID, accountID, userID, CreateCredentialRequest{
		CredentialKey: "beta-group",
		Credential:    map[string]any{"apiKey": "must-not-leak"},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate credential error = %v, want ErrConflict", err)
	}
	gatewayDiscovery, err := service.DiscoverModelsViaGateway(ctx, GatewayDiscoverModelsRequest{
		OrganizationID: orgID,
		AccountID:      accountID,
		CredentialID:   beta.ID,
	})
	if err != nil {
		t.Fatalf("gateway discover beta models: %v", err)
	}
	if gatewayDiscovery.Status != "succeeded" || len(gatewayDiscovery.Models) != 2 {
		t.Fatalf("gateway beta discovery = %#v", gatewayDiscovery)
	}
	var discoveryCredentialID string
	if err := pool.QueryRow(ctx, `SELECT credential_id::text FROM provider_call_logs WHERE id = $1`, gatewayDiscovery.ProviderCallID).Scan(&discoveryCredentialID); err != nil {
		t.Fatalf("select discovery call credential: %v", err)
	}
	if discoveryCredentialID != beta.ID {
		t.Fatalf("discovery call credential = %s, want %s", discoveryCredentialID, beta.ID)
	}

	alphaDiscovery, err := service.DiscoverModelsForCredential(ctx, orgID, accountID, alpha.ID)
	if err != nil {
		t.Fatalf("discover alpha models: %v", err)
	}
	if alphaDiscovery.CredentialID != alpha.ID || len(alphaDiscovery.Models) != 2 {
		t.Fatalf("alpha discovery = %#v, want credential %s and 2 models", alphaDiscovery, alpha.ID)
	}
	betaDiscovery, err := service.DiscoverModelsForCredential(ctx, orgID, accountID, beta.ID)
	if err != nil {
		t.Fatalf("discover beta models: %v", err)
	}
	if betaDiscovery.CredentialID != beta.ID || len(betaDiscovery.Models) != 2 {
		t.Fatalf("beta discovery = %#v, want credential %s and 2 models", betaDiscovery, beta.ID)
	}

	credentials, err := service.ListCredentials(ctx, orgID, accountID, "active")
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(credentials) != 2 {
		t.Fatalf("active credentials = %#v, want 2", credentials)
	}
	encodedCredentials, err := json.Marshal(credentials)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	for _, secret := range []string{"sk-alpha-secret", "sk-beta-secret", "must-not-leak"} {
		if strings.Contains(string(encodedCredentials), secret) {
			t.Fatalf("credential response leaked secret %q: %s", secret, encodedCredentials)
		}
	}
	for _, credential := range credentials {
		if credential.AvailableModelCount != 2 || credential.LastDiscoveredAt == nil {
			t.Fatalf("credential discovery summary = %#v, want 2 available models and timestamp", credential)
		}
	}

	models, err := service.ListModels(ctx, orgID, accountID, "active")
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	betaModelID := providerModelIDByKey(t, models, "beta-model")
	response, err := service.GenerateText(ctx, GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: betaModelID,
		Input:           json.RawMessage(`{"prompt":"route beta"}`),
	})
	if err != nil {
		t.Fatalf("generate with beta model: %v", err)
	}
	if response.Status != "succeeded" || response.Output.Text != "ok" {
		t.Fatalf("beta response = %#v", response)
	}
	requestMu.Lock()
	usedCredential := modelCredentials["beta-model"]
	requestMu.Unlock()
	if usedCredential != "sk-beta-secret" {
		t.Fatalf("beta model used credential %q, want beta key", usedCredential)
	}
	var loggedCredentialID string
	if err := pool.QueryRow(ctx, `SELECT credential_id::text FROM provider_call_logs WHERE id = $1`, response.ProviderCallID).Scan(&loggedCredentialID); err != nil {
		t.Fatalf("select provider call credential: %v", err)
	}
	if loggedCredentialID != beta.ID {
		t.Fatalf("provider call credential = %s, want %s", loggedCredentialID, beta.ID)
	}

	rotatedBeta, err := service.RotateCredentialByID(ctx, orgID, accountID, beta.ID, userID, RotateCredentialRequest{
		Credential: map[string]any{"apiKey": "sk-beta-rotated"},
	})
	if err != nil {
		t.Fatalf("rotate beta credential by id: %v", err)
	}
	if rotatedBeta.ID == beta.ID || rotatedBeta.CredentialKey != beta.CredentialKey || rotatedBeta.AvailableModelCount != 2 {
		t.Fatalf("rotated beta credential = %#v", rotatedBeta)
	}
	response, err = service.GenerateText(ctx, GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: betaModelID,
		Input:           json.RawMessage(`{"prompt":"route rotated beta"}`),
	})
	if err != nil || response.Status != "succeeded" {
		t.Fatalf("generate with rotated beta credential response=%#v err=%v", response, err)
	}
	requestMu.Lock()
	usedCredential = modelCredentials["beta-model"]
	requestMu.Unlock()
	if usedCredential != "sk-beta-rotated" {
		t.Fatalf("beta model used credential %q after rotation, want rotated key", usedCredential)
	}

	betaModelsEmpty.Store(true)
	if _, err := service.DiscoverModelsForCredential(ctx, orgID, accountID, rotatedBeta.ID); err != nil {
		t.Fatalf("rediscover empty beta group: %v", err)
	}
	response, err = service.GenerateText(ctx, GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: betaModelID,
		Input:           json.RawMessage(`{"prompt":"must be blocked"}`),
	})
	if err != nil {
		t.Fatalf("blocked beta generation returned infrastructure error: %v", err)
	}
	if response.Status != "failed" || response.Error == nil {
		t.Fatalf("blocked beta response = %#v, want normalized failure", response)
	}
	model, err := service.GetModel(ctx, orgID, betaModelID)
	if err != nil {
		t.Fatalf("get unavailable beta model: %v", err)
	}
	if model.Status != "active" {
		t.Fatalf("beta model status = %q, want globally active", model.Status)
	}
}

func mustActiveCredentialByKey(t *testing.T, ctx context.Context, service *Service, organizationID, accountID, credentialKey string) Credential {
	t.Helper()
	credentials, err := service.ListCredentials(ctx, organizationID, accountID, "active")
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	for _, credential := range credentials {
		if credential.CredentialKey == credentialKey {
			return credential
		}
	}
	t.Fatalf("active credential %q not found in %#v", credentialKey, credentials)
	return Credential{}
}

func providerModelIDByKey(t *testing.T, models []Model, modelKey string) string {
	t.Helper()
	for _, model := range models {
		if model.ModelKey == modelKey {
			return model.ID
		}
	}
	t.Fatalf("provider model %q not found in %#v", modelKey, models)
	return ""
}
