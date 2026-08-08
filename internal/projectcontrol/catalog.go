package projectcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	ToolCatalogSchemaVersionV1 = "cineweave.mcp-tool-catalog.v1"
	MaxMCPToolCatalogSize      = 1000
)

var mcpToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type ToolCatalog struct {
	SchemaVersion string             `json:"schemaVersion"`
	CatalogHash   string             `json:"catalogHash"`
	Tools         []ToolCatalogEntry `json:"tools"`
}

type ToolCatalogEntry struct {
	Name               string             `json:"name"`
	ActionName         string             `json:"actionName"`
	Version            int                `json:"version"`
	Label              string             `json:"label"`
	Summary            string             `json:"summary"`
	InputSchemaHash    string             `json:"inputSchemaHash"`
	OutputSchemaHash   string             `json:"outputSchemaHash"`
	Permissions        []string           `json:"permissions"`
	ProjectKinds       []string           `json:"projectKinds"`
	ReadOnly           bool               `json:"readOnly"`
	Destructive        bool               `json:"destructive"`
	Idempotent         bool               `json:"idempotent"`
	Costed             bool               `json:"costed"`
	StartsWorkflow     bool               `json:"startsWorkflow"`
	SupportsDryRun     bool               `json:"supportsDryRun"`
	ExecutionMode      ExecutionMode      `json:"executionMode"`
	ActivityVisibility ActivityVisibility `json:"activityVisibility"`
}

func BuildToolCatalog(descriptors []Descriptor) (ToolCatalog, error) {
	entries := make([]ToolCatalogEntry, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if !descriptor.ExportToMCP {
			continue
		}
		if err := descriptor.Validate(); err != nil {
			return ToolCatalog{}, err
		}
		toolName, err := MCPToolName(descriptor.Name)
		if err != nil {
			return ToolCatalog{}, err
		}
		if _, exists := seen[toolName]; exists {
			return ToolCatalog{}, fmt.Errorf("duplicate MCP wire tool %s", toolName)
		}
		seen[toolName] = struct{}{}
		inputHash, err := canonicalJSONHash(descriptor.InputSchema)
		if err != nil {
			return ToolCatalog{}, fmt.Errorf("hash input schema for %s: %w", descriptor.Name, err)
		}
		outputHash, err := canonicalJSONHash(descriptor.OutputSchema)
		if err != nil {
			return ToolCatalog{}, fmt.Errorf("hash output schema for %s: %w", descriptor.Name, err)
		}
		permissions := sortedUniqueStrings(descriptor.Permissions)
		projectKinds := sortedUniqueStrings(descriptor.ProjectKinds)
		entries = append(entries, ToolCatalogEntry{
			Name: toolName, ActionName: descriptor.Name, Version: descriptor.Version,
			Label: descriptor.Label, Summary: descriptor.Summary,
			InputSchemaHash: inputHash, OutputSchemaHash: outputHash,
			Permissions: permissions, ProjectKinds: projectKinds,
			ReadOnly: descriptor.ReadOnly, Destructive: descriptor.Destructive,
			Idempotent: descriptor.Idempotent, Costed: descriptor.Costed,
			StartsWorkflow: descriptor.StartsWorkflow, SupportsDryRun: descriptor.SupportsDryRun,
			ExecutionMode: descriptor.ExecutionMode, ActivityVisibility: descriptor.ActivityVisibility,
		})
	}
	if len(entries) > MaxMCPToolCatalogSize {
		return ToolCatalog{}, fmt.Errorf("MCP tool catalog has %d exported tools, maximum is %d", len(entries), MaxMCPToolCatalogSize)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].Version < entries[j].Version
		}
		return entries[i].Name < entries[j].Name
	})
	payload := struct {
		SchemaVersion string             `json:"schemaVersion"`
		Tools         []ToolCatalogEntry `json:"tools"`
	}{SchemaVersion: ToolCatalogSchemaVersionV1, Tools: entries}
	hash, err := canonicalJSONHashValue(payload)
	if err != nil {
		return ToolCatalog{}, fmt.Errorf("hash MCP tool catalog: %w", err)
	}
	return ToolCatalog{
		SchemaVersion: ToolCatalogSchemaVersionV1,
		CatalogHash:   hash,
		Tools:         entries,
	}, nil
}

// MCPToolName keeps the domain action name stable while exposing a name that
// Codex can register as a Responses API function tool. Codex reserves double
// underscores for its qualified mcp__server__tool namespace, so wire aliases
// use a single underscore and catalog generation rejects any collision.
func MCPToolName(actionName string) (string, error) {
	actionName = strings.TrimSpace(actionName)
	if actionName == "" {
		return "", fmt.Errorf("MCP action name is required")
	}
	toolName := strings.ReplaceAll(actionName, ".", "_")
	if !mcpToolNamePattern.MatchString(toolName) {
		return "", fmt.Errorf("MCP wire tool %q derived from action %q must match %s", toolName, actionName, mcpToolNamePattern.String())
	}
	return toolName, nil
}

func canonicalJSONHash(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return canonicalJSONHashValue(value)
}

func canonicalJSONHashValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
