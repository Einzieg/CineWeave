package commerce

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScriptDerivationMaterializationRetryAndLineageIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_COMMERCE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_COMMERCE_INTEGRATION_TEST=1 to run script derivation integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for script derivation integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	fixture := createScriptDerivationIntegrationFixture(t, ctx, tx)
	repository := NewRepository()
	service := NewScriptDerivationService(repository)
	input := CreateScriptDerivationInput{
		Dimension:   "scene",
		Instruction: "仅改变使用场景，保持商品事实、语言和行动号召",
		Preserve:    []string{"product_facts", "language", "cta"},
		Variations: []ScriptDerivationVariation{
			{Key: "night_market", Label: "夜市场景", Brief: "夜市摊位真实体验"},
			{Key: "shopping_mall", Label: "商场场景", Brief: "商场试戴与近景展示"},
			{Key: "office_commute", Label: "通勤场景", Brief: "工作日通勤使用"},
		},
	}
	prepared, err := service.PrepareBatch(ctx, tx, PrepareScriptDerivationParams{
		BatchID:        uuid.NewString(),
		OrganizationID: fixture.OrganizationID,
		ProjectID:      fixture.ProjectID,
		ScriptUnitID:   fixture.SourceUnit.ID,
		CreatedBy:      fixture.UserID,
		IdempotencyKey: "derive-root-" + fixture.Suffix,
		RequestHash:    strings.Repeat("a", 64),
		Input:          input,
	})
	if err != nil {
		t.Fatalf("prepare derivation batch: %v", err)
	}
	if len(prepared.Positions) != 3 {
		t.Fatalf("reserved positions = %+v", prepared.Positions)
	}
	for index, position := range prepared.Positions {
		if position.UnitNo != int64(index+2) || position.SortOrder != int64((index+2)*10) {
			t.Fatalf("reserved position %d = %+v", index, position)
		}
	}

	editedContent := "用户在裂变启动后编辑的新正文"
	editedSource, err := NewCatalogService(repository).UpdateScriptUnit(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, fixture.SourceUnit.ID,
		fixture.UserID, fixture.SourceUnit.Revision,
		UpdateScriptUnitInput{DraftContent: &editedContent},
	)
	if err != nil {
		t.Fatalf("edit source after batch freeze: %v", err)
	}
	if editedSource.CurrentContent != editedContent {
		t.Fatalf("edited source current content = %q", editedSource.CurrentContent)
	}

	batch, err := repository.StartScriptDerivationBatch(ctx, tx, prepared.Batch)
	if err != nil {
		t.Fatalf("start batch: %v", err)
	}
	var promptVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM prompt_versions
		ORDER BY created_at, id
		LIMIT 1
	`).Scan(&promptVersionID); err != nil {
		t.Fatalf("resolve prompt version: %v", err)
	}
	items, err := repository.ListScriptDerivationItems(ctx, tx, batch.ID)
	if err != nil {
		t.Fatalf("list batch items: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("batch items = %d", len(items))
	}
	if batch.SourceContentSnapshot != fixture.SourceContent {
		t.Fatalf("frozen source = %q, want %q", batch.SourceContentSnapshot, fixture.SourceContent)
	}

	for _, itemIndex := range []int{2, 0} {
		item := items[itemIndex]
		attempt, err := repository.StartScriptDerivationItem(ctx, tx, item)
		if err != nil {
			t.Fatalf("start item %d: %v", itemIndex, err)
		}
		if itemIndex == 2 {
			promptHash := "sha256:" + strings.Repeat("d", 64)
			outputHash := strings.Repeat("e", 64)
			call, err := repository.InsertScriptDerivationAttemptCall(
				ctx, tx, ScriptDerivationAttemptCall{
					BatchID: batch.ID, ItemID: item.ID, AttemptID: attempt.ID,
					OrganizationID: batch.OrganizationID, ProjectID: batch.ProjectID,
					ProductID: batch.ProductID, RoundNo: 1, Phase: "generate",
					ModelProfileKey:   batch.ScriptModelProfileKey,
					PromptTemplateKey: "commerce_script_derivation_generate",
					PromptVersionID:   promptVersionID, PromptHash: promptHash,
					StartedAt: time.Now().UTC(),
				},
			)
			if err != nil {
				t.Fatalf("insert attempt call with registry prompt hash: %v", err)
			}
			if err := repository.CompleteScriptDerivationAttemptCall(
				ctx, tx, call.ID, "", "", "", outputHash,
			); err != nil {
				t.Fatalf("complete attempt call: %v", err)
			}
			var storedPromptHash, storedOutputHash string
			if err := tx.QueryRow(ctx, `
				SELECT prompt_hash, output_content_hash
				FROM commerce_script_derivation_attempt_calls
				WHERE id = $1
			`, call.ID).Scan(&storedPromptHash, &storedOutputHash); err != nil {
				t.Fatalf("reload attempt call hashes: %v", err)
			}
			if storedPromptHash != promptHash || storedOutputHash != outputHash {
				t.Fatalf(
					"attempt call hashes = (%q, %q), want (%q, %q)",
					storedPromptHash, storedOutputHash, promptHash, outputHash,
				)
			}
		}
		item, err = repository.LoadScriptDerivationItem(
			ctx, tx, fixture.OrganizationID, fixture.ProjectID, item.ID, true,
		)
		if err != nil {
			t.Fatalf("reload item %d: %v", itemIndex, err)
		}
		if _, _, err := repository.MaterializeScriptDerivationItem(
			ctx, tx, batch, item, "", "独立裂变脚本 "+item.VariationLabel,
		); err != nil {
			t.Fatalf("materialize item %d: %v", itemIndex, err)
		}
	}
	failed := items[1]
	if _, err := repository.StartScriptDerivationItem(ctx, tx, failed); err != nil {
		t.Fatalf("start retryable item: %v", err)
	}
	failed, err = repository.LoadScriptDerivationItem(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, failed.ID, true,
	)
	if err != nil {
		t.Fatalf("reload retryable item: %v", err)
	}
	if err := repository.FailScriptDerivationItem(
		ctx, tx, failed, true, "UPSTREAM_TIMEOUT", "上游暂时超时",
	); err != nil {
		t.Fatalf("fail retryable item: %v", err)
	}
	batch, err = repository.ReconcileScriptDerivationBatch(ctx, tx, batch)
	if err != nil {
		t.Fatalf("reconcile root batch: %v", err)
	}
	if batch.Status != "partial_succeeded" || batch.SucceededCount != 2 ||
		batch.FailedRetryableCount != 1 {
		t.Fatalf("root batch after partial completion = %+v", batch)
	}

	units, err := repository.ListScriptUnits(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, "active", 0, "", 100,
	)
	if err != nil {
		t.Fatalf("list materialized scripts: %v", err)
	}
	assertScriptDerivationUnitOrder(t, units, []int64{1, 2, 4})
	for _, unit := range units {
		if unit.DerivedFromScriptUnitID == nil {
			continue
		}
		var metadata struct {
			ScriptDerivation struct {
				BatchID           string `json:"batchId"`
				SourceScriptTitle string `json:"sourceScriptTitle"`
				VariationLabel    string `json:"variationLabel"`
			} `json:"scriptDerivation"`
		}
		if err := json.Unmarshal(unit.Metadata, &metadata); err != nil {
			t.Fatalf("decode derivation metadata: %v", err)
		}
		if metadata.ScriptDerivation.BatchID != batch.ID ||
			metadata.ScriptDerivation.SourceScriptTitle == "" ||
			metadata.ScriptDerivation.VariationLabel == "" {
			t.Fatalf("derivation metadata = %+v", metadata.ScriptDerivation)
		}
	}

	retry, err := service.PrepareRetryBatch(
		ctx, tx, batch.ID, "", fixture.OrganizationID, fixture.ProjectID,
		fixture.UserID, "derive-retry-"+fixture.Suffix, strings.Repeat("b", 64),
	)
	if err != nil {
		t.Fatalf("prepare retry batch: %v", err)
	}
	if len(retry.Positions) != 1 ||
		retry.Positions[0].UnitNo != items[1].ReservedUnitNo ||
		retry.Positions[0].SortOrder != items[1].ReservedSortOrder {
		t.Fatalf("retry did not reuse the root reservation: %+v", retry.Positions)
	}
	retryBatch, err := repository.StartScriptDerivationBatch(ctx, tx, retry.Batch)
	if err != nil {
		t.Fatalf("start retry batch: %v", err)
	}
	retryItems, err := repository.ListScriptDerivationItems(ctx, tx, retryBatch.ID)
	if err != nil || len(retryItems) != 1 {
		t.Fatalf("retry items = %+v error=%v", retryItems, err)
	}
	if _, err := repository.StartScriptDerivationItem(ctx, tx, retryItems[0]); err != nil {
		t.Fatalf("start retry item: %v", err)
	}
	retryItem, err := repository.LoadScriptDerivationItem(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, retryItems[0].ID, true,
	)
	if err != nil {
		t.Fatalf("reload retry item: %v", err)
	}
	if _, _, err := repository.MaterializeScriptDerivationItem(
		ctx, tx, retryBatch, retryItem, "", "重试成功的商场裂变脚本",
	); err != nil {
		t.Fatalf("materialize retry item: %v", err)
	}
	retryBatch, err = repository.ReconcileScriptDerivationBatch(ctx, tx, retryBatch)
	if err != nil {
		t.Fatalf("reconcile retry batch: %v", err)
	}
	if retryBatch.Status != "succeeded" {
		t.Fatalf("retry batch status = %s", retryBatch.Status)
	}

	rootDetail, err := service.GetBatch(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, batch.ID, true,
	)
	if err != nil {
		t.Fatalf("load root lineage: %v", err)
	}
	if rootDetail.Status != "partial_succeeded" {
		t.Fatalf("retry mutated root batch status: %s", rootDetail.Status)
	}
	if len(rootDetail.Lineage) != 2 || len(rootDetail.LineageResults) != 3 {
		t.Fatalf("lineage summary=%d results=%d", len(rootDetail.Lineage), len(rootDetail.LineageResults))
	}
	var retriedResult *ScriptDerivationLineageResult
	for index := range rootDetail.LineageResults {
		if rootDetail.LineageResults[index].VariationKey == "shopping_mall" {
			retriedResult = &rootDetail.LineageResults[index]
			break
		}
	}
	if retriedResult == nil || retriedResult.LatestResult.Status != "succeeded" ||
		len(retriedResult.Items) != 2 {
		t.Fatalf("retried lineage result = %+v", retriedResult)
	}

	units, err = repository.ListScriptUnits(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, "active", 0, "", 100,
	)
	if err != nil {
		t.Fatalf("list scripts after retry: %v", err)
	}
	assertScriptDerivationUnitOrder(t, units, []int64{1, 2, 3, 4})
	source, err := repository.LoadScriptUnit(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, fixture.SourceUnit.ID, false,
	)
	if err != nil {
		t.Fatalf("reload source: %v", err)
	}
	if source.CurrentContent != editedContent {
		t.Fatalf("source was overwritten by derivation: %q", source.CurrentContent)
	}
}

func TestNormalizeLanguageScriptDerivationRequiresBCP47(t *testing.T) {
	input := CreateScriptDerivationInput{
		Dimension: "language", Instruction: "改为目标语言",
		Variations: []ScriptDerivationVariation{{
			Key: "malay", Label: "马来语", Brief: "面向马来西亚用户",
		}},
	}
	if err := NormalizeScriptDerivationInput(&input); err == nil {
		t.Fatal("non-BCP47 language derivation key was accepted")
	}
	input.Variations[0].Key = "ms-my"
	if err := NormalizeScriptDerivationInput(&input); err != nil {
		t.Fatalf("valid BCP47 language derivation key: %v", err)
	}
	if input.Variations[0].Key != "ms-MY" {
		t.Fatalf("normalized locale = %s", input.Variations[0].Key)
	}
}

type scriptDerivationIntegrationFixture struct {
	Suffix         string
	OrganizationID string
	UserID         string
	ProjectID      string
	SourceContent  string
	SourceUnit     ScriptUnit
}

func createScriptDerivationIntegrationFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
) scriptDerivationIntegrationFixture {
	t.Helper()
	fixture := scriptDerivationIntegrationFixture{
		Suffix:        strings.ReplaceAll(uuid.NewString(), "-", ""),
		SourceContent: "原始商品广告脚本，保持商品事实和行动号召。",
	}
	var workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ('Derivation Integration', $1)
		RETURNING id::text
	`, "derivation-"+fixture.Suffix).Scan(&fixture.OrganizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users(email, display_name)
		VALUES ($1, 'Derivation Integration')
		RETURNING id::text
	`, "derivation-"+fixture.Suffix+"@example.test").Scan(&fixture.UserID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, 'Derivation Integration')
		RETURNING id::text
	`, fixture.OrganizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, project_kind, project_type,
			video_ratio, video_production_state, settings, created_by
		)
		VALUES ($1, $2, 'Derivation Integration', 'commerce_video',
		        'commerce_video', '9:16', 'unconfigured', '{}', $3)
		RETURNING id::text
	`, fixture.OrganizationID, workspaceID, fixture.UserID).Scan(&fixture.ProjectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	var profileVersionID string
	if err := tx.QueryRow(ctx, `
		SELECT version.id::text
		FROM video_production_profile_versions version
		JOIN video_production_profiles profile ON profile.id = version.profile_id
		WHERE profile.profile_key = $1
		  AND version.lifecycle_state = 'published'
		  AND version.implementation_state = 'available'
		ORDER BY version.version DESC
		LIMIT 1
	`, videoproduction.ProfileSingleFrameI2V).Scan(&profileVersionID); err != nil {
		t.Fatalf("resolve profile version: %v", err)
	}
	var templateID, templateVersionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_workflow_templates(
			organization_id, template_key, name, status, created_by
		)
		VALUES ($1, $2, 'Derivation Integration', 'active', $3)
		RETURNING id::text
	`, fixture.OrganizationID, DefaultWorkflowTemplateKey, fixture.UserID).Scan(&templateID); err != nil {
		t.Fatalf("insert commerce template: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_workflow_template_versions(
			template_id, version, configuration_snapshot, prompt_bindings,
			agent_model_contracts, language_contract, image_capability_contract,
			video_capability_contract, video_production_profile_version_id,
			content_hash, status, created_by, published_at
		)
		VALUES ($1, 1, '{}', '{}', '{}', '{}', '{}', '{}', $2, $3, 'published', $4, now())
		RETURNING id::text
	`, templateID, profileVersionID, strings.Repeat("c", 64), fixture.UserID).Scan(&templateVersionID); err != nil {
		t.Fatalf("insert commerce template version: %v", err)
	}
	service := NewService(NewRepository())
	bindings, err := service.PrepareInitialBindings(ctx, tx, InitialBindingParams{
		OrganizationID: fixture.OrganizationID, ProjectID: fixture.ProjectID,
		WorkflowTemplateVersion: templateVersionID, CreatedBy: fixture.UserID,
		CompatibilityPolicy: videoproduction.CompatibilityStrict,
		VideoOverrides:      json.RawMessage(`{}`),
		ProductionConfiguration: videoproduction.ProductionConfigurationSnapshot{
			SchemaVersion:         videoproduction.ProductionConfigurationSnapshotVersion,
			ProjectType:           ProjectTypeCommerce,
			AspectRatio:           "9:16",
			VideoRatio:            "9:16",
			ImageModelProfileKey:  "image_generation_default",
			VideoModelProfileKey:  "video_generation_default",
			ScriptModelProfileKey: "script_agent_default",
			AudioStrategy:         "native_av",
			AudioRequirement:      "preferred",
			ImageQuality:          "standard",
			TimelineTimebase:      90000,
			FPSNumerator:          24,
			FPSDenominator:        1,
			Settings:              json.RawMessage(`{}`),
			ManualBindings:        map[string]videoproduction.ManualBindingSnapshot{},
		},
		ConfigurationSnapshot: json.RawMessage(`{"defaultLanguageMode":"auto"}`),
		ModelRoutingSnapshot:  json.RawMessage(`{"script":"script_agent_default"}`),
		CapabilitySnapshot:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("prepare bindings: %v", err)
	}
	if err := service.ActivateInitialBindings(ctx, tx, fixture.ProjectID, bindings); err != nil {
		t.Fatalf("activate bindings: %v", err)
	}

	var connectorID, accountID, modelID, profileID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM provider_connectors
		ORDER BY is_official DESC, created_at, id LIMIT 1
	`).Scan(&connectorID); err != nil {
		t.Fatalf("resolve provider connector: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_accounts(
			organization_id, connector_id, name, base_url, auth_type, status, created_by
		)
		VALUES ($1, $2, 'Derivation Text', 'https://example.invalid/v1', 'none', 'active', $3)
		RETURNING id::text
	`, fixture.OrganizationID, connectorID, fixture.UserID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'derivation-text', 'Derivation Text', 'text', 'active')
		RETURNING id::text
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO model_profiles(
			organization_id, profile_key, name, purpose, routing_strategy
		)
		VALUES ($1, 'script_agent_default', 'Script Agent', 'script derivation', 'priority_with_fallback')
		RETURNING id::text
	`, fixture.OrganizationID).Scan(&profileID); err != nil {
		t.Fatalf("insert model profile: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO model_profile_bindings(
			model_profile_id, provider_model_id, priority, weight, enabled
		)
		VALUES ($1, $2, 100, 100, true)
	`, profileID, modelID); err != nil {
		t.Fatalf("insert model profile binding: %v", err)
	}

	catalog := NewCatalogService(NewRepository())
	zeroRevision := int64(0)
	product, err := catalog.CreateProductVersion(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, fixture.UserID,
		&zeroRevision, ProductVersionInput{
			Name: "测试头盔", Brand: "CineWeave",
			SellingPoints:     json.RawMessage(`["轻量","耐用"]`),
			ImmutableFeatures: json.RawMessage(`{"color":"black"}`),
			ProhibitedClaims:  json.RawMessage(`["绝对安全"]`),
		},
	)
	if err != nil {
		t.Fatalf("create product version: %v", err)
	}
	source, err := catalog.CreateScriptUnit(
		ctx, tx, fixture.OrganizationID, fixture.ProjectID, fixture.UserID,
		product.Product.ScriptUnitsRevision, CreateScriptUnitInput{
			Title: "原始脚本", Content: fixture.SourceContent, LanguageMode: "explicit",
			ExplicitTargetLanguage: stringPointer("zh-CN"),
			TargetDurationSeconds:  15, TargetPlatform: "tiktok",
		},
	)
	if err != nil {
		t.Fatalf("create source script: %v", err)
	}
	fixture.SourceUnit = source.ScriptUnit
	return fixture
}

func assertScriptDerivationUnitOrder(t *testing.T, units []ScriptUnit, want []int64) {
	t.Helper()
	if len(units) != len(want) {
		t.Fatalf("script unit count = %d, want %d", len(units), len(want))
	}
	for index, unit := range units {
		if unit.UnitNo != want[index] {
			t.Fatalf("script unit %d unitNo = %d, want %d", index, unit.UnitNo, want[index])
		}
	}
}
