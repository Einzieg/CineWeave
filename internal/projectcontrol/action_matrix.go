package projectcontrol

import (
	"fmt"
	"sort"
	"strings"
)

const ActionMatrixSchemaVersionV1 = "cineweave.project-control-action-matrix.v1"

type MigrationStatus string
type ImplementationKind string

const (
	MigrationStatusMigrated      MigrationStatus = "migrated"
	MigrationStatusAdapterBacked MigrationStatus = "adapter_backed"
	MigrationStatusExcluded      MigrationStatus = "excluded"
)

const (
	ImplementationNativeProjectControl ImplementationKind = "native_project_control"
	ImplementationSharedDomain         ImplementationKind = "shared_domain"
	ImplementationAgentAdapter         ImplementationKind = "agent_adapter"
	ImplementationEditionHTTPAdapter   ImplementationKind = "edition_http_adapter"
)

type ActionContract struct {
	Descriptor            Descriptor
	AgentToolNames        []string
	RESTOperationIDs      []string
	CommercialActionNames []string
	ImplementationEntry   string
	ImplementationKind    ImplementationKind
	ExportToAgent         bool
	ExportToManual        bool
	MigrationStatus       MigrationStatus
	ExclusionReason       string
}

type ActionMatrix struct {
	SchemaVersion       string               `json:"schemaVersion"`
	MatrixHash          string               `json:"matrixHash"`
	Actions             []ActionMatrixEntry  `json:"actions"`
	OperationExclusions []OperationExclusion `json:"operationExclusions"`
}

type OperationExclusion struct {
	Registry    string `json:"registry"`
	OperationID string `json:"operationId"`
	Reason      string `json:"reason"`
}

type ActionMatrixEntry struct {
	ActionName            string             `json:"actionName"`
	ActionVersion         int                `json:"actionVersion"`
	Label                 string             `json:"label"`
	Summary               string             `json:"summary"`
	AgentToolNames        []string           `json:"agentToolNames"`
	RESTOperationIDs      []string           `json:"restOperationIds"`
	CommercialActionNames []string           `json:"commercialActionNames"`
	ImplementationEntry   string             `json:"implementationEntry"`
	ImplementationKind    ImplementationKind `json:"implementationKind"`
	Scope                 ScopeKind          `json:"scope"`
	Permissions           []string           `json:"permissions"`
	ProjectKinds          []string           `json:"projectKinds"`
	ReadOnly              bool               `json:"readOnly"`
	Destructive           bool               `json:"destructive"`
	Idempotent            bool               `json:"idempotent"`
	Costed                bool               `json:"costed"`
	StartsWorkflow        bool               `json:"startsWorkflow"`
	SupportsDryRun        bool               `json:"supportsDryRun"`
	ExecutionMode         ExecutionMode      `json:"executionMode"`
	ActivityVisibility    ActivityVisibility `json:"activityVisibility"`
	ExportToMCP           bool               `json:"exportToMcp"`
	ExportToAgent         bool               `json:"exportToAgent"`
	ExportToManual        bool               `json:"exportToManual"`
	MigrationStatus       MigrationStatus    `json:"migrationStatus"`
	ExclusionReason       string             `json:"exclusionReason,omitempty"`
}

func BuildActionMatrix(contracts []ActionContract, exclusions ...OperationExclusion) (ActionMatrix, error) {
	actions := make([]ActionMatrixEntry, 0, len(contracts))
	seenActions := make(map[string]struct{}, len(contracts))
	seenAgentTools := make(map[string]string)
	seenRESTOperations := make(map[string]string)
	seenCommercialActions := make(map[string]string)
	for _, contract := range contracts {
		descriptor := contract.Descriptor
		if err := descriptor.Validate(); err != nil {
			return ActionMatrix{}, err
		}
		key := fmt.Sprintf("%s@%d", descriptor.Name, descriptor.Version)
		if _, exists := seenActions[key]; exists {
			return ActionMatrix{}, fmt.Errorf("duplicate project control action %s", key)
		}
		seenActions[key] = struct{}{}
		if strings.TrimSpace(contract.ImplementationEntry) == "" {
			return ActionMatrix{}, fmt.Errorf("project control action %s has no implementation entry", key)
		}
		if !validImplementationKind(contract.ImplementationKind) {
			return ActionMatrix{}, fmt.Errorf("project control action %s has invalid implementation kind %q", key, contract.ImplementationKind)
		}
		if !validMigrationStatus(contract.MigrationStatus) {
			return ActionMatrix{}, fmt.Errorf("project control action %s has invalid migration status %q", key, contract.MigrationStatus)
		}
		if contract.MigrationStatus == MigrationStatusExcluded && strings.TrimSpace(contract.ExclusionReason) == "" {
			return ActionMatrix{}, fmt.Errorf("excluded project control action %s requires a reason", key)
		}
		if contract.MigrationStatus == MigrationStatusMigrated && isAdapterImplementation(contract.ImplementationKind) {
			return ActionMatrix{}, fmt.Errorf("migrated project control action %s still uses %s", key, contract.ImplementationKind)
		}
		if contract.MigrationStatus == MigrationStatusAdapterBacked && !isAdapterImplementation(contract.ImplementationKind) {
			return ActionMatrix{}, fmt.Errorf("adapter-backed project control action %s uses non-adapter implementation %s", key, contract.ImplementationKind)
		}

		agentTools := sortedUniqueStrings(contract.AgentToolNames)
		restOperations := sortedUniqueStrings(contract.RESTOperationIDs)
		commercialActions := sortedUniqueStrings(contract.CommercialActionNames)
		if err := claimMappedNames("Agent tool", descriptor.Name, agentTools, seenAgentTools); err != nil {
			return ActionMatrix{}, err
		}
		if err := claimMappedNames("REST operation", descriptor.Name, restOperations, seenRESTOperations); err != nil {
			return ActionMatrix{}, err
		}
		if err := claimMappedNames("Commercial action", descriptor.Name, commercialActions, seenCommercialActions); err != nil {
			return ActionMatrix{}, err
		}
		permissions := sortedUniqueStrings(descriptor.Permissions)
		projectKinds := sortedUniqueStrings(descriptor.ProjectKinds)
		actions = append(actions, ActionMatrixEntry{
			ActionName: descriptor.Name, ActionVersion: descriptor.Version,
			Label: descriptor.Label, Summary: descriptor.Summary,
			AgentToolNames: agentTools, RESTOperationIDs: restOperations,
			CommercialActionNames: commercialActions,
			ImplementationEntry:   strings.TrimSpace(contract.ImplementationEntry),
			ImplementationKind:    contract.ImplementationKind,
			Scope:                 descriptor.Scope, Permissions: permissions, ProjectKinds: projectKinds,
			ReadOnly: descriptor.ReadOnly, Destructive: descriptor.Destructive,
			Idempotent: descriptor.Idempotent, Costed: descriptor.Costed,
			StartsWorkflow: descriptor.StartsWorkflow, SupportsDryRun: descriptor.SupportsDryRun,
			ExecutionMode: descriptor.ExecutionMode, ActivityVisibility: descriptor.ActivityVisibility,
			ExportToMCP: descriptor.ExportToMCP, ExportToAgent: contract.ExportToAgent,
			ExportToManual: contract.ExportToManual, MigrationStatus: contract.MigrationStatus,
			ExclusionReason: strings.TrimSpace(contract.ExclusionReason),
		})
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].ActionName == actions[j].ActionName {
			return actions[i].ActionVersion < actions[j].ActionVersion
		}
		return actions[i].ActionName < actions[j].ActionName
	})
	normalizedExclusions := make([]OperationExclusion, 0, len(exclusions))
	seenExclusions := make(map[string]struct{}, len(exclusions))
	for _, exclusion := range exclusions {
		exclusion.Registry = strings.TrimSpace(exclusion.Registry)
		exclusion.OperationID = strings.TrimSpace(exclusion.OperationID)
		exclusion.Reason = strings.TrimSpace(exclusion.Reason)
		if exclusion.Registry == "" || exclusion.OperationID == "" || exclusion.Reason == "" {
			return ActionMatrix{}, fmt.Errorf("operation exclusion requires registry, operation ID, and reason")
		}
		key := exclusion.Registry + "\x00" + exclusion.OperationID
		if _, exists := seenExclusions[key]; exists {
			return ActionMatrix{}, fmt.Errorf("duplicate operation exclusion %s/%s", exclusion.Registry, exclusion.OperationID)
		}
		seenExclusions[key] = struct{}{}
		if exclusion.Registry == "core_openapi" {
			if mappedAction, exists := seenRESTOperations[exclusion.OperationID]; exists {
				return ActionMatrix{}, fmt.Errorf("REST operation %s is both mapped to %s and excluded", exclusion.OperationID, mappedAction)
			}
		}
		normalizedExclusions = append(normalizedExclusions, exclusion)
	}
	sort.Slice(normalizedExclusions, func(i, j int) bool {
		left := normalizedExclusions[i].Registry + "\x00" + normalizedExclusions[i].OperationID
		right := normalizedExclusions[j].Registry + "\x00" + normalizedExclusions[j].OperationID
		return left < right
	})
	payload := struct {
		SchemaVersion       string               `json:"schemaVersion"`
		Actions             []ActionMatrixEntry  `json:"actions"`
		OperationExclusions []OperationExclusion `json:"operationExclusions"`
	}{SchemaVersion: ActionMatrixSchemaVersionV1, Actions: actions, OperationExclusions: normalizedExclusions}
	hash, err := canonicalJSONHashValue(payload)
	if err != nil {
		return ActionMatrix{}, fmt.Errorf("hash project control action matrix: %w", err)
	}
	return ActionMatrix{
		SchemaVersion: ActionMatrixSchemaVersionV1, MatrixHash: hash,
		Actions: actions, OperationExclusions: normalizedExclusions,
	}, nil
}

func validImplementationKind(kind ImplementationKind) bool {
	switch kind {
	case ImplementationNativeProjectControl, ImplementationSharedDomain,
		ImplementationAgentAdapter, ImplementationEditionHTTPAdapter:
		return true
	default:
		return false
	}
}

func isAdapterImplementation(kind ImplementationKind) bool {
	return kind == ImplementationAgentAdapter || kind == ImplementationEditionHTTPAdapter
}

func validMigrationStatus(status MigrationStatus) bool {
	switch status {
	case MigrationStatusMigrated, MigrationStatusAdapterBacked, MigrationStatusExcluded:
		return true
	default:
		return false
	}
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func claimMappedNames(kind, actionName string, names []string, seen map[string]string) error {
	for _, name := range names {
		if existing, exists := seen[name]; exists && existing != actionName {
			return fmt.Errorf("%s %s is mapped to both %s and %s", kind, name, existing, actionName)
		}
		seen[name] = actionName
	}
	return nil
}
