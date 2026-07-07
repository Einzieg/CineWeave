package api

import (
	"net/http"
	"strings"
	"testing"
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

	var directorOnly struct {
		Items []PromptTemplate `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/project-manual-templates?filter%5Bkind%5D=director", seed.ownerToken, seed.organizationID, nil, &directorOnly)
	if len(directorOnly.Items) != 1 || directorOnly.Items[0].TemplateKey != "default_director_manual" {
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
