package provider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderAdminStatusFilteringAndCascadeDelete(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}

	var profileID, bindingID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, 'admin-status-test', 'Admin Status Test', 'admin_status_test', 'priority', '{}')
		RETURNING id
	`, orgID).Scan(&profileID); err != nil {
		t.Fatalf("insert model profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 10, 100, true)
		RETURNING id
	`, profileID, modelID).Scan(&bindingID); err != nil {
		t.Fatalf("insert model profile binding: %v", err)
	}

	service := NewService(pool, vault)
	accounts, err := service.ListAccounts(ctx, orgID, "", 20)
	if err != nil {
		t.Fatalf("list active accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != accountID {
		t.Fatalf("active accounts = %#v, want account %s", accounts, accountID)
	}
	models, err := service.ListModels(ctx, orgID, accountID, "")
	if err != nil {
		t.Fatalf("list active models: %v", err)
	}
	if len(models) != 1 || models[0].ID != modelID {
		t.Fatalf("active models = %#v, want model %s", models, modelID)
	}

	if err := service.DeleteAccount(ctx, orgID, accountID); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	accounts, err = service.ListAccounts(ctx, orgID, "", 20)
	if err != nil {
		t.Fatalf("list active accounts after delete: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("active accounts after delete = %#v, want empty", accounts)
	}
	accounts, err = service.ListAccounts(ctx, orgID, "all", 20)
	if err != nil {
		t.Fatalf("list all accounts after delete: %v", err)
	}
	if len(accounts) != 1 || accounts[0].Status != "disabled" {
		t.Fatalf("all accounts after delete = %#v, want one disabled account", accounts)
	}
	models, err = service.ListModels(ctx, orgID, accountID, "")
	if err != nil {
		t.Fatalf("list active models after account delete: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("active models after account delete = %#v, want empty", models)
	}
	models, err = service.ListModels(ctx, orgID, accountID, "all")
	if err != nil {
		t.Fatalf("list all models after account delete: %v", err)
	}
	if len(models) != 1 || models[0].Status != "disabled" {
		t.Fatalf("all models after account delete = %#v, want one disabled model", models)
	}
	var bindingCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM model_profile_bindings WHERE id = $1`, bindingID).Scan(&bindingCount); err != nil {
		t.Fatalf("select binding count: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("model profile binding count after account delete = %d, want 0", bindingCount)
	}
}

func TestDeleteModelRemovesBindingsAndAllowsRebinding(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}
	var profileID, bindingID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, 'delete-model-binding-test', 'Delete Model Binding Test', 'delete_model_binding_test', 'priority', '{}')
		RETURNING id
	`, orgID).Scan(&profileID); err != nil {
		t.Fatalf("insert model profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		VALUES ($1, $2, 10, 100, true)
		RETURNING id
	`, profileID, modelID).Scan(&bindingID); err != nil {
		t.Fatalf("insert model profile binding: %v", err)
	}

	service := NewService(pool, vault)
	if err := service.DeleteModel(ctx, orgID, modelID); err != nil {
		t.Fatalf("delete model: %v", err)
	}
	var bindingCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM model_profile_bindings WHERE id = $1`, bindingID).Scan(&bindingCount); err != nil {
		t.Fatalf("select deleted binding count: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("binding count after model delete = %d, want 0", bindingCount)
	}

	restored, err := service.CreateModel(ctx, orgID, accountID, CreateModelRequest{
		ModelKey:    "gpt-integration",
		DisplayName: "GPT Integration Restored",
		Modality:    "text",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("restore provider model: %v", err)
	}
	if restored.ID != modelID {
		t.Fatalf("restored model ID = %s, want %s", restored.ID, modelID)
	}
	profile, err := service.CreateModelProfileBinding(ctx, orgID, profileID, CreateModelProfileBindingRequest{
		ProviderModelID: modelID,
	})
	if err != nil {
		t.Fatalf("rebind restored provider model: %v", err)
	}
	if len(profile.Bindings) != 1 || profile.Bindings[0].ProviderModelID != modelID {
		t.Fatalf("profile bindings after rebind = %#v, want one binding for model %s", profile.Bindings, modelID)
	}
}

func TestUpdateModelProfileBinding(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var accountID, profileID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, 'update-model-binding-test', 'Update Model Binding Test', 'update_model_binding_test', 'priority_with_fallback', '{}')
		RETURNING id
	`, orgID).Scan(&profileID); err != nil {
		t.Fatalf("insert model profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET provider_options_schema = '{"xCapabilities":{"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"]}}'
		WHERE provider_model_id = $1
	`, modelID); err != nil {
		t.Fatalf("configure reasoning levels: %v", err)
	}

	service := NewService(pool, vault)
	priority, weight, enabled := 200, 100, true
	profile, err := service.CreateModelProfileBinding(ctx, orgID, profileID, CreateModelProfileBindingRequest{
		ProviderModelID: modelID,
		Priority:        &priority,
		Weight:          &weight,
		Enabled:         &enabled,
		RuntimeOptions:  &ModelProfileBindingRuntimeOptions{ReasoningLevel: "low"},
	})
	if err != nil {
		t.Fatalf("create model profile binding: %v", err)
	}
	if len(profile.Bindings) != 1 {
		t.Fatalf("bindings = %#v, want one", profile.Bindings)
	}
	bindingID := profile.Bindings[0].ID

	if profile.Bindings[0].RuntimeOptions.ReasoningLevel != "low" {
		t.Fatalf("created runtime options = %#v, want low", profile.Bindings[0].RuntimeOptions)
	}

	priority, weight, enabled = 50, 250, false
	profile, err = service.UpdateModelProfileBinding(ctx, orgID, profileID, bindingID, UpdateModelProfileBindingRequest{
		Priority:       &priority,
		Weight:         &weight,
		Enabled:        &enabled,
		RuntimeOptions: &ModelProfileBindingRuntimeOptions{ReasoningLevel: "high"},
	})
	if err != nil {
		t.Fatalf("update model profile binding: %v", err)
	}
	if len(profile.Bindings) != 1 || profile.Bindings[0].Priority != 50 || profile.Bindings[0].Weight != 250 || profile.Bindings[0].Enabled || profile.Bindings[0].RuntimeOptions.ReasoningLevel != "high" {
		t.Fatalf("updated binding = %#v", profile.Bindings)
	}

	enabled = true
	profile, err = service.UpdateModelProfileBinding(ctx, orgID, profileID, bindingID, UpdateModelProfileBindingRequest{Enabled: &enabled})
	if err != nil {
		t.Fatalf("enable model profile binding: %v", err)
	}
	if profile.Bindings[0].Priority != 50 || profile.Bindings[0].Weight != 250 || !profile.Bindings[0].Enabled || profile.Bindings[0].RuntimeOptions.ReasoningLevel != "high" {
		t.Fatalf("partially updated binding = %#v", profile.Bindings[0])
	}
	if _, err := service.UpdateModel(ctx, orgID, modelID, UpdateModelRequest{
		Capabilities: &CapabilityInput{
			TaskTypes:             json.RawMessage(`["text.generate","text.stream"]`),
			InputLimits:           json.RawMessage(`{}`),
			OutputLimits:          json.RawMessage(`{}`),
			QualityTiers:          json.RawMessage(`[]`),
			ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium"]}}`),
			PricingPolicy:         json.RawMessage(`{}`),
		},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("capability update that removes bound reasoning level error = %v, want validation", err)
	}
	candidates, err := service.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  orgID,
		ModelProfileKey: "update-model-binding-test",
		TaskType:        TaskTypeTextGenerate,
		Modality:        "text",
	})
	if err != nil {
		t.Fatalf("resolve routing candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].RuntimeOptions.ReasoningLevel != "high" {
		t.Fatalf("routing runtime options = %#v, want high", candidates)
	}

	secondary, err := service.CreateModel(ctx, orgID, accountID, CreateModelRequest{
		ModelKey:    "binding-update-secondary",
		DisplayName: "Binding Update Secondary",
		Modality:    "text",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create secondary model: %v", err)
	}
	priority, weight = 50, 300
	profile, err = service.CreateModelProfileBinding(ctx, orgID, profileID, CreateModelProfileBindingRequest{
		ProviderModelID: secondary.ID,
		Priority:        &priority,
		Weight:          &weight,
	})
	if err != nil {
		t.Fatalf("bind secondary model: %v", err)
	}
	if len(profile.Bindings) != 2 || profile.Bindings[0].ProviderModelID != secondary.ID {
		t.Fatalf("binding order = %#v, want higher equal-priority weight first", profile.Bindings)
	}

	negative := -1
	if _, err := service.UpdateModelProfileBinding(ctx, orgID, profileID, bindingID, UpdateModelProfileBindingRequest{Priority: &negative}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative priority error = %v, want validation", err)
	}
	if _, err := service.UpdateModelProfileBinding(ctx, orgID, profileID, bindingID, UpdateModelProfileBindingRequest{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty update error = %v, want validation", err)
	}
	if _, err := service.UpdateModelProfileBinding(ctx, orgID, profileID, bindingID, UpdateModelProfileBindingRequest{
		RuntimeOptions: &ModelProfileBindingRuntimeOptions{ReasoningLevel: "max"},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unsupported reasoning level error = %v, want validation", err)
	}
}

func TestProviderModelDiscoveryDoesNotReviveDisabledModels(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var accountID, disabledModelID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE provider_model_capabilities SET task_types = '[]' WHERE provider_model_id = $1`, modelID); err != nil {
		t.Fatalf("clear active model task types: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'disabled-remote', 'Disabled Old', 'text', 'disabled')
		RETURNING id
	`, accountID).Scan(&disabledModelID); err != nil {
		t.Fatalf("insert disabled model: %v", err)
	}

	service := NewService(pool, vault)
	if err := service.syncDiscoveredModels(ctx, orgID, accountID, []DiscoveredModel{
		{ModelKey: "gpt-integration", DisplayName: "GPT Updated", Modality: "image"},
		{ModelKey: "disabled-remote", DisplayName: "Disabled New", Modality: "image"},
		{ModelKey: "new-remote", DisplayName: "New Remote", Modality: "video", Status: "disabled"},
	}); err != nil {
		t.Fatalf("sync discovered models: %v", err)
	}

	var displayName, modality, status string
	if err := pool.QueryRow(ctx, `
		SELECT display_name, modality, status
		FROM provider_models
		WHERE id = $1
	`, modelID).Scan(&displayName, &modality, &status); err != nil {
		t.Fatalf("select updated active model: %v", err)
	}
	if displayName != "GPT Updated" || modality != "image" || status != "active" {
		t.Fatalf("active model after discovery = (%q, %q, %q), want updated active image", displayName, modality, status)
	}

	if err := pool.QueryRow(ctx, `
		SELECT display_name, modality, status
		FROM provider_models
		WHERE id = $1
	`, disabledModelID).Scan(&displayName, &modality, &status); err != nil {
		t.Fatalf("select disabled model: %v", err)
	}
	if displayName != "Disabled Old" || modality != "text" || status != "disabled" {
		t.Fatalf("disabled model after discovery = (%q, %q, %q), want unchanged disabled text", displayName, modality, status)
	}

	if err := pool.QueryRow(ctx, `
		SELECT status
		FROM provider_models
		WHERE provider_account_id = $1 AND model_key = 'new-remote'
	`, accountID).Scan(&status); err != nil {
		t.Fatalf("select new discovered model: %v", err)
	}
	if status != "active" {
		t.Fatalf("new discovered model status = %q, want active", status)
	}

	var taskTypesRaw string
	if err := pool.QueryRow(ctx, `
		SELECT task_types::text
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
	`, modelID).Scan(&taskTypesRaw); err != nil {
		t.Fatalf("select updated task types: %v", err)
	}
	var taskTypes []string
	if err := json.Unmarshal([]byte(taskTypesRaw), &taskTypes); err != nil {
		t.Fatalf("decode task types %s: %v", taskTypesRaw, err)
	}
	if !containsString(taskTypes, TaskTypeImageGenerate) {
		t.Fatalf("task types after modality update = %#v, want %q", taskTypes, TaskTypeImageGenerate)
	}

	models, err := service.ListModels(ctx, orgID, accountID, "")
	if err != nil {
		t.Fatalf("list active models after discovery: %v", err)
	}
	for _, model := range models {
		if model.ID == disabledModelID {
			t.Fatalf("disabled model %s was returned by default ListModels", disabledModelID)
		}
	}
}

func TestCreateModelUpsertsAndReactivatesExistingModel(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE provider_models SET status = 'disabled' WHERE id = $1`, modelID); err != nil {
		t.Fatalf("disable provider model: %v", err)
	}

	service := NewService(pool, vault)
	model, err := service.CreateModel(ctx, orgID, accountID, CreateModelRequest{
		ModelKey:    "gpt-integration",
		DisplayName: "GPT Manual Restore",
		Modality:    "text",
		Status:      "active",
		Capabilities: &CapabilityInput{
			TaskTypes:   json.RawMessage(`["text.generate"]`),
			InputLimits: json.RawMessage(`{"maxTokens":123}`),
		},
	})
	if err != nil {
		t.Fatalf("create existing provider model: %v", err)
	}
	if model.ID != modelID {
		t.Fatalf("model ID = %s, want existing %s", model.ID, modelID)
	}
	if model.DisplayName != "GPT Manual Restore" || model.Status != "active" {
		t.Fatalf("model = (%q, %q), want restored active manual model", model.DisplayName, model.Status)
	}

	var capabilityCount int
	var taskTypesRaw, inputLimitsRaw string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), max(task_types::text), max(input_limits::text)
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
	`, modelID).Scan(&capabilityCount, &taskTypesRaw, &inputLimitsRaw); err != nil {
		t.Fatalf("select provider model capabilities: %v", err)
	}
	if capabilityCount != 1 {
		t.Fatalf("capability count = %d, want 1", capabilityCount)
	}
	if !strings.Contains(taskTypesRaw, TaskTypeTextGenerate) || strings.Contains(taskTypesRaw, TaskTypeTextStream) {
		t.Fatalf("task types = %s, want only text generate", taskTypesRaw)
	}
	if !strings.Contains(inputLimitsRaw, "123") {
		t.Fatalf("input limits = %s, want restored input limits", inputLimitsRaw)
	}
}

func TestSelectGatewayTextModelRejectsNonTextProviderModel(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	if _, err := pool.Exec(ctx, `UPDATE provider_models SET modality = 'image' WHERE id = $1`, modelID); err != nil {
		t.Fatalf("update provider model modality: %v", err)
	}

	service := NewService(pool, vault)
	_, err := service.selectGatewayTextModel(ctx, GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: modelID,
	}, TaskTypeTextGenerate)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "text generation") {
		t.Fatalf("selectGatewayTextModel error = %v, want text generation validation error", err)
	}
}

func TestSelectGatewayTextModelRejectsUnsupportedStreamTask(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	if _, err := pool.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET task_types = '["text.generate"]'
		WHERE provider_model_id = $1
	`, modelID); err != nil {
		t.Fatalf("update provider model task types: %v", err)
	}

	service := NewService(pool, vault)
	_, err := service.selectGatewayTextModel(ctx, GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: modelID,
	}, TaskTypeTextStream)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), TaskTypeTextStream) {
		t.Fatalf("selectGatewayTextModel stream error = %v, want unsupported stream validation error", err)
	}
}

func TestRecordProviderModelTestRejectsDisabledModel(t *testing.T) {
	ctx, pool, vault := openProviderAdminTestDB(t)
	orgID, userID, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://example.test")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	if _, err := pool.Exec(ctx, `UPDATE provider_models SET status = 'disabled' WHERE id = $1`, modelID); err != nil {
		t.Fatalf("disable provider model: %v", err)
	}

	service := NewService(pool, vault)
	service.SetGateway("http://127.0.0.1:1", "test-token")
	_, err := service.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "connection_test",
	})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("RecordProviderModelTest error = %v, want inactive model validation error", err)
	}
}

func openProviderAdminTestDB(t *testing.T) (context.Context, *pgxpool.Pool, *Vault) {
	t.Helper()
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider admin integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider admin integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return ctx, pool, vault
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
