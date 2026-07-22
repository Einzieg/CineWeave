package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	seedfiles "github.com/Einzieg/cineweave/db/seeds"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
)

func TestProjectManualTemplatesAndBindings(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var templates struct {
		Items []PromptTemplate `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/project-manual-templates", seed.ownerToken, seed.organizationID, nil, &templates)
	directorVersionID := activeManualVersionID(t, templates.Items, "default_director_manual")
	_ = activeManualVersionID(t, templates.Items, "default_visual_manual")
	toonflowDirectorVersionID := activeManualVersionID(t, templates.Items, "toonflow_director_manual_xianxia_fantasy")
	toonflowVisualVersionID := activeManualVersionID(t, templates.Items, "toonflow_visual_manual_2d_90s_japanese_anime")

	var directorOnly struct {
		Items []PromptTemplate `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/project-manual-templates?filter%5Bkind%5D=director", seed.ownerToken, seed.organizationID, nil, &directorOnly)
	if !hasPromptTemplate(directorOnly.Items, "default_director_manual") || !hasPromptTemplate(directorOnly.Items, "toonflow_director_manual_xianxia_fantasy") {
		t.Fatalf("director manual templates = %+v", directorOnly.Items)
	}

	var created Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", seed.ownerToken, seed.organizationID, map[string]any{
		"workspaceId": seed.workspaceID,
		"name":        "Manual Project",
	}, &created)
	if !strings.Contains(created.DirectorManual, "README") || !strings.Contains(created.DirectorManual, "分镜表设计") {
		t.Fatalf("created director manual = %q", created.DirectorManual)
	}
	if !strings.Contains(created.VisualManual, "角色模板") || !strings.Contains(created.VisualManual, "分镜视频模板") {
		t.Fatalf("created visual manual = %q", created.VisualManual)
	}
	if created.VideoProductionBinding == nil {
		t.Fatal("created project has no video production binding")
	}
	configuration, err := videoproduction.DecodeProductionConfiguration(created.VideoProductionBinding.ProfileSnapshot)
	if err != nil {
		t.Fatalf("decode initial production configuration: %v", err)
	}
	if configuration.SchemaVersion != videoproduction.ProductionConfigurationSnapshotVersion ||
		configuration.ManualBindings["director"].PromptVersionID == "" ||
		configuration.ManualBindings["visual"].PromptVersionID == "" {
		t.Fatalf("initial production configuration is incomplete: %+v", configuration)
	}

	var bindings struct {
		Items []ProjectManualBinding `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	assertManualBinding(t, bindings.Items, "director", "default_director_manual")
	assertManualBinding(t, bindings.Items, "visual", "default_visual_manual")

	foreignOrganizationID := uuid.NewString()
	foreignTemplateID := uuid.NewString()
	foreignVersionID := uuid.NewString()
	foreignSuffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO organizations(id, name, slug) VALUES ($1, $2, $3)
	`, foreignOrganizationID, "Foreign Organization "+foreignSuffix, "foreign-"+foreignSuffix); err != nil {
		t.Fatalf("seed foreign organization: %v", err)
	}
	defer seed.pool.Exec(seed.ctx, `DELETE FROM organizations WHERE id = $1`, foreignOrganizationID)
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO prompt_templates(
			id, organization_id, template_key, name, purpose, description,
			modality, task_type, scope, status, is_system
		) VALUES ($1, $2, $3, 'Foreign Director Manual', 'director_manual', '',
		          'text', 'text.generate', 'organization', 'active', false)
	`, foreignTemplateID, foreignOrganizationID, "foreign_director_manual_"+foreignSuffix); err != nil {
		t.Fatalf("seed foreign organization prompt template: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO prompt_versions(
			id, prompt_template_id, template_id, version_no, version, content,
			variables_schema, content_hash, status, content_format, activated_at
		) VALUES ($1, $2, $2, 1, 1, 'FOREIGN_ORGANIZATION_SECRET',
		          '{}'::jsonb, $3, 'active', 'markdown', now())
	`, foreignVersionID, foreignTemplateID, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed foreign organization prompt version: %v", err)
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects", seed.ownerToken, seed.organizationID, map[string]any{
		"workspaceId":                   seed.workspaceID,
		"name":                          "Cross Organization Manual Project",
		"directorManualPromptVersionId": foreignVersionID,
	}, http.StatusUnprocessableEntity, "MANUAL_VERSION_NOT_AVAILABLE")
	assertAPIErrorCode(t, server, http.MethodPost,
		"/api/projects/"+created.ID+"/video-production/rebuild-impact",
		seed.ownerToken, seed.organizationID, map[string]any{
			"targetProfileKey": "single_frame_i2v",
			"targetConfiguration": map[string]any{
				"directorManualPromptVersionId": foreignVersionID,
			},
		}, http.StatusConflict, videoproduction.CodeRebuildConflict)

	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE projects
		SET director_manual = 'BROKEN_DIRECTOR_MANUAL',
		    visual_manual = 'BROKEN_VISUAL_MANUAL'
		WHERE id = $1
	`, created.ID); err != nil {
		t.Fatalf("corrupt project manual columns: %v", err)
	}
	var loaded Project
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID, seed.ownerToken, seed.organizationID, nil, &loaded)
	if strings.Contains(loaded.DirectorManual, "BROKEN") || !strings.Contains(loaded.DirectorManual, "默认导演手册") {
		t.Fatalf("project director manual should come from active binding, got %q", loaded.DirectorManual)
	}
	if strings.Contains(loaded.VisualManual, "BROKEN") || !strings.Contains(loaded.VisualManual, "默认视觉手册") {
		t.Fatalf("project visual manual should come from active binding, got %q", loaded.VisualManual)
	}

	assertAPIErrorCode(t, server, http.MethodPut, "/api/projects/"+created.ID+"/manual-bindings/director", seed.ownerToken, seed.organizationID, map[string]any{
		"promptVersionId": directorVersionID,
	}, http.StatusConflict, videoproduction.CodeConfigurationRebuildRequired)

	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+created.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"directorManual": "自定义导演手册",
	}, http.StatusConflict, videoproduction.CodeConfigurationRebuildRequired)

	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+created.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"artStyle":                      "2d_90s_japanese_anime",
		"directorManualPromptVersionId": toonflowDirectorVersionID,
		"visualManualPromptVersionId":   toonflowVisualVersionID,
		"settings": map[string]any{
			"toonflowVisualStyle": "2d_90s_japanese_anime",
			"toonflowStoryStyle":  "xianxia_fantasy",
		},
	}, http.StatusConflict, videoproduction.CodeConfigurationRebuildRequired)
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	assertManualBinding(t, bindings.Items, "director", "default_director_manual")
	assertManualBinding(t, bindings.Items, "visual", "default_visual_manual")

	var toonflowProject Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", seed.ownerToken, seed.organizationID, map[string]any{
		"workspaceId":                   seed.workspaceID,
		"name":                          "Toonflow Manual Project",
		"artStyle":                      "2d_90s_japanese_anime",
		"directorManualPromptVersionId": toonflowDirectorVersionID,
		"visualManualPromptVersionId":   toonflowVisualVersionID,
		"settings": map[string]any{
			"toonflowVisualStyle": "2d_90s_japanese_anime",
			"toonflowStoryStyle":  "xianxia_fantasy",
		},
	}, &toonflowProject)
	if !strings.Contains(toonflowProject.DirectorManual, "古风仙侠") || !strings.Contains(toonflowProject.DirectorManual, "分镜表") {
		t.Fatalf("toonflow director manual = %q", toonflowProject.DirectorManual)
	}
	if !strings.Contains(toonflowProject.VisualManual, "90") || !strings.Contains(toonflowProject.VisualManual, "角色") {
		t.Fatalf("toonflow visual manual = %q", toonflowProject.VisualManual)
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+toonflowProject.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	assertManualBinding(t, bindings.Items, "director", "toonflow_director_manual_xianxia_fantasy")
	assertManualBinding(t, bindings.Items, "visual", "toonflow_visual_manual_2d_90s_japanese_anime")
}

func TestToonflowSeedResourcesContainRequiredKeys(t *testing.T) {
	raw, err := seedfiles.FS.ReadFile("prompt-registry/000001_prompt_registry.json")
	if err != nil {
		t.Fatalf("read Toonflow prompt seed: %v", err)
	}
	assertSeedPromptTemplateKeys(t, raw, "prompt-registry", []string{
		"toonflow_visual_2d_90s_japanese_anime_prefix",
		"toonflow_visual_2d_90s_japanese_anime_art_character",
		"toonflow_visual_2d_90s_japanese_anime_art_character_derivative",
		"toonflow_visual_2d_90s_japanese_anime_art_storyboard_video",
		"toonflow_director_xianxia_fantasy_director_planning_narrative",
		"toonflow_production_agent_decision",
		"toonflow_script_execution_script",
	})

	manualRaw, err := seedfiles.FS.ReadFile("project-manuals/000001_project_manuals.json")
	if err != nil {
		t.Fatalf("read Toonflow manual seed: %v", err)
	}
	assertSeedPromptTemplateKeys(t, manualRaw, "project-manuals", []string{
		"toonflow_visual_manual_2d_90s_japanese_anime",
		"toonflow_director_manual_xianxia_fantasy",
	})
}

func assertSeedPromptTemplateKeys(t *testing.T, raw []byte, resourceKey string, required []string) {
	t.Helper()
	var manifest struct {
		ResourceKey string `json:"resourceKey"`
		Tables      []struct {
			Name string          `json:"name"`
			Rows json.RawMessage `json:"rows"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse %s seed: %v", resourceKey, err)
	}
	if manifest.ResourceKey != resourceKey {
		t.Fatalf("seed resource key = %q, want %q", manifest.ResourceKey, resourceKey)
	}
	templateKeys := map[string]bool{}
	for _, table := range manifest.Tables {
		if table.Name != "prompt_templates" {
			continue
		}
		var rows []struct {
			TemplateKey string `json:"template_key"`
		}
		if err := json.Unmarshal(table.Rows, &rows); err != nil {
			t.Fatalf("parse %s prompt templates: %v", resourceKey, err)
		}
		for _, row := range rows {
			templateKeys[row.TemplateKey] = true
		}
	}
	for _, want := range required {
		if !templateKeys[want] {
			t.Fatalf("%s seed missing prompt template %s", resourceKey, want)
		}
	}
}

func hasPromptTemplate(items []PromptTemplate, templateKey string) bool {
	for _, item := range items {
		if item.TemplateKey == templateKey {
			return true
		}
	}
	return false
}

func activeManualVersionID(t *testing.T, items []PromptTemplate, templateKey string) string {
	t.Helper()
	for _, item := range items {
		if item.TemplateKey != templateKey {
			continue
		}
		if item.ActiveVersion == nil || item.ActiveVersion.ID == "" {
			t.Fatalf("template %s has no active version: %+v", templateKey, item)
		}
		return item.ActiveVersion.ID
	}
	t.Fatalf("template %s not found in %+v", templateKey, items)
	return ""
}

func assertManualBinding(t *testing.T, items []ProjectManualBinding, manualKind, templateKey string) {
	t.Helper()
	for _, item := range items {
		if item.ManualKind == manualKind && item.TemplateKey == templateKey && item.Status == "active" {
			return
		}
	}
	t.Fatalf("manual binding %s/%s not found in %+v", manualKind, templateKey, items)
}
