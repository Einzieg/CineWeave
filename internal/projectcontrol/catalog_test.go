package projectcontrol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildToolCatalogIsDeterministicAndFiltersPrivateActions(t *testing.T) {
	descriptors := []Descriptor{
		testCatalogDescriptor("zeta.read", true),
		testCatalogDescriptor("alpha.write", true),
		testCatalogDescriptor("internal.only", false),
	}
	first, err := BuildToolCatalog(descriptors)
	if err != nil {
		t.Fatalf("BuildToolCatalog() error = %v", err)
	}
	second, err := BuildToolCatalog([]Descriptor{descriptors[2], descriptors[0], descriptors[1]})
	if err != nil {
		t.Fatalf("BuildToolCatalog(reordered) error = %v", err)
	}
	if first.CatalogHash != second.CatalogHash {
		t.Fatalf("catalog hash changed with descriptor order: %s != %s", first.CatalogHash, second.CatalogHash)
	}
	if len(first.Tools) != 2 || first.Tools[0].Name != "alpha_write" || first.Tools[1].Name != "zeta_read" {
		t.Fatalf("unexpected catalog tools: %#v", first.Tools)
	}
	if first.Tools[0].ActionName != "alpha.write" || first.Tools[1].ActionName != "zeta.read" {
		t.Fatalf("unexpected catalog action mapping: %#v", first.Tools)
	}
	if first.Tools[0].InputSchemaHash == "" || first.Tools[0].OutputSchemaHash == "" {
		t.Fatalf("schema hashes are missing: %#v", first.Tools[0])
	}
}

func TestBuildToolCatalogRejectsDuplicateToolNames(t *testing.T) {
	_, err := BuildToolCatalog([]Descriptor{
		testCatalogDescriptor("project.get", true),
		testCatalogDescriptor("project.get", true),
	})
	if err == nil {
		t.Fatal("BuildToolCatalog() error = nil, want duplicate error")
	}
}

func TestBuildToolCatalogRejectsWireNameCollisions(t *testing.T) {
	_, err := BuildToolCatalog([]Descriptor{
		testCatalogDescriptor("project.get", true),
		testCatalogDescriptor("project_get", true),
	})
	if err == nil {
		t.Fatal("BuildToolCatalog() error = nil, want wire name collision")
	}
}

func TestBuildToolCatalogRejectsCatalogLargerThanCodexDiscoveryPage(t *testing.T) {
	descriptors := make([]Descriptor, 0, MaxMCPToolCatalogSize+1)
	for index := 0; index <= MaxMCPToolCatalogSize; index++ {
		descriptor := testCatalogDescriptor(fmt.Sprintf("catalog.tool_%04d", index), true)
		descriptors = append(descriptors, descriptor)
	}
	if _, err := BuildToolCatalog(descriptors); err == nil || !strings.Contains(err.Error(), "maximum is") {
		t.Fatalf("BuildToolCatalog() error = %v, want catalog size rejection", err)
	}
}

func TestMCPToolNameUsesCodexCompatibleStableAlias(t *testing.T) {
	name, err := MCPToolName("commerce.script.create_language_variant")
	if err != nil {
		t.Fatal(err)
	}
	if name != "commerce_script_create_language_variant" {
		t.Fatalf("MCPToolName() = %q", name)
	}
	if !mcpToolNamePattern.MatchString(name) {
		t.Fatalf("wire name %q is not Codex compatible", name)
	}
	if _, err := MCPToolName("invalid/tool"); err == nil {
		t.Fatal("MCPToolName(invalid) error = nil")
	}
}

func testCatalogDescriptor(name string, export bool) Descriptor {
	return Descriptor{
		Name: name, Version: 1, Label: name, Summary: name, Description: name,
		Risk: RiskRead, Scope: ScopeProject,
		Permissions: []string{"project.read"}, ProjectKinds: []string{"narrative"},
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":true}`),
		Effects:      Effects{}, ReadOnly: true, Idempotent: true,
		ExecutionMode: ExecutionModeSync, ActivityVisibility: ActivityVisibilityAuditOnly,
		ExportToMCP: export,
	}
}
