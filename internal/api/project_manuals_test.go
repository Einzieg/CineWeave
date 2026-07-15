package api

import (
	"net/http"
	"strings"
	"testing"

	seedfiles "github.com/Einzieg/cineweave/db/seeds"
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

	var bindings struct {
		Items []ProjectManualBinding `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	assertManualBinding(t, bindings.Items, "director", "default_director_manual")
	assertManualBinding(t, bindings.Items, "visual", "default_visual_manual")

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

	var rebound ProjectManualBinding
	doAPISuccess(t, server, http.MethodPut, "/api/projects/"+created.ID+"/manual-bindings/director", seed.ownerToken, seed.organizationID, map[string]any{
		"promptVersionId": directorVersionID,
	}, &rebound)
	if rebound.ManualKind != "director" || rebound.TemplateKey != "default_director_manual" {
		t.Fatalf("rebound manual = %+v", rebound)
	}

	var updated Project
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+created.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"directorManual": "自定义导演手册",
	}, &updated)
	if updated.DirectorManual != "自定义导演手册" {
		t.Fatalf("updated director manual = %q", updated.DirectorManual)
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	for _, item := range bindings.Items {
		if item.ManualKind == "director" {
			t.Fatalf("director binding should be disabled after direct edit: %+v", bindings.Items)
		}
	}
	assertManualBinding(t, bindings.Items, "visual", "default_visual_manual")

	var patchedManuals Project
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+created.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"artStyle":                      "2d_90s_japanese_anime",
		"directorManualPromptVersionId": toonflowDirectorVersionID,
		"visualManualPromptVersionId":   toonflowVisualVersionID,
		"settings": map[string]any{
			"toonflowVisualStyle": "2d_90s_japanese_anime",
			"toonflowStoryStyle":  "xianxia_fantasy",
		},
	}, &patchedManuals)
	if !strings.Contains(patchedManuals.DirectorManual, "仙侠") || !strings.Contains(patchedManuals.VisualManual, "90") {
		t.Fatalf("patched manuals director=%q visual=%q", patchedManuals.DirectorManual, patchedManuals.VisualManual)
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+created.ID+"/manual-bindings", seed.ownerToken, seed.organizationID, nil, &bindings)
	assertManualBinding(t, bindings.Items, "director", "toonflow_director_manual_xianxia_fantasy")
	assertManualBinding(t, bindings.Items, "visual", "toonflow_visual_manual_2d_90s_japanese_anime")

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
	if !strings.Contains(toonflowProject.DirectorManual, "仙侠") || !strings.Contains(toonflowProject.DirectorManual, "导演规划") {
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
	content := string(raw)
	required := []string{
		"toonflow_visual_2d_90s_japanese_anime_prefix",
		"toonflow_visual_2d_90s_japanese_anime_art_character",
		"toonflow_visual_2d_90s_japanese_anime_art_character_derivative",
		"toonflow_visual_2d_90s_japanese_anime_art_storyboard_video",
		"toonflow_director_xianxia_fantasy_director_planning_narrative",
		"toonflow_production_agent_decision",
		"toonflow_script_execution_script",
		`"resourceKey": "prompt-registry"`,
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Fatalf("Toonflow prompt seed missing %s", want)
		}
	}

	manualRaw, err := seedfiles.FS.ReadFile("project-manuals/000001_project_manuals.json")
	if err != nil {
		t.Fatalf("read Toonflow manual seed: %v", err)
	}
	manualContent := string(manualRaw)
	for _, want := range []string{
		"toonflow_visual_manual_2d_90s_japanese_anime",
		"toonflow_director_manual_xianxia_fantasy",
		`"resourceKey": "project-manuals"`,
	} {
		if !strings.Contains(manualContent, want) {
			t.Fatalf("Toonflow manual seed missing %s", want)
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
