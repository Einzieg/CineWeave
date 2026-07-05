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
	var bindingEnabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM model_profile_bindings WHERE id = $1`, bindingID).Scan(&bindingEnabled); err != nil {
		t.Fatalf("select binding enabled: %v", err)
	}
	if bindingEnabled {
		t.Fatal("model profile binding stayed enabled after account delete")
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
