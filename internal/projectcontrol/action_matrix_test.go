package projectcontrol

import "testing"

func TestBuildActionMatrixIsDeterministic(t *testing.T) {
	firstDescriptor := testCatalogDescriptor("source.update", true)
	firstDescriptor.Risk = RiskWrite
	firstDescriptor.Effects = Effects{WritesProject: true}
	firstDescriptor.ReadOnly = false
	firstDescriptor.ExecutionMode = ExecutionModeAsyncCommand
	firstDescriptor.ActivityVisibility = ActivityVisibilityPrimary
	secondDescriptor := testCatalogDescriptor("source.list", true)

	contracts := []ActionContract{
		{
			Descriptor: firstDescriptor, AgentToolNames: []string{"source.update"},
			RESTOperationIDs:    []string{"updateProjectSource"},
			ImplementationEntry: "internal/source.Service.Update",
			ImplementationKind:  ImplementationSharedDomain,
			ExportToAgent:       true, ExportToManual: true, MigrationStatus: MigrationStatusMigrated,
		},
		{
			Descriptor: secondDescriptor, AgentToolNames: []string{"source.list"},
			RESTOperationIDs:    []string{"listProjectSources"},
			ImplementationEntry: "internal/source.Service.List",
			ImplementationKind:  ImplementationSharedDomain,
			ExportToAgent:       true, ExportToManual: true, MigrationStatus: MigrationStatusMigrated,
		},
	}
	first, err := BuildActionMatrix(contracts)
	if err != nil {
		t.Fatalf("BuildActionMatrix() error = %v", err)
	}
	second, err := BuildActionMatrix([]ActionContract{contracts[1], contracts[0]})
	if err != nil {
		t.Fatalf("BuildActionMatrix(reordered) error = %v", err)
	}
	if first.MatrixHash != second.MatrixHash {
		t.Fatalf("matrix hash changed with order: %s != %s", first.MatrixHash, second.MatrixHash)
	}
	if len(first.Actions) != 2 || first.Actions[0].ActionName != "source.list" || first.Actions[1].ActionName != "source.update" {
		t.Fatalf("unexpected action order: %#v", first.Actions)
	}
}

func TestBuildActionMatrixRejectsDuplicateMappings(t *testing.T) {
	first := testCatalogDescriptor("source.list", true)
	second := testCatalogDescriptor("script.list", true)
	_, err := BuildActionMatrix([]ActionContract{
		{Descriptor: first, AgentToolNames: []string{"read.content"}, ImplementationEntry: "source.List", ImplementationKind: ImplementationSharedDomain, MigrationStatus: MigrationStatusMigrated},
		{Descriptor: second, AgentToolNames: []string{"read.content"}, ImplementationEntry: "script.List", ImplementationKind: ImplementationSharedDomain, MigrationStatus: MigrationStatusMigrated},
	})
	if err == nil {
		t.Fatal("BuildActionMatrix() error = nil, want duplicate mapping error")
	}
}

func TestBuildActionMatrixRejectsFalseMigratedAdapter(t *testing.T) {
	descriptor := testCatalogDescriptor("source.update", true)
	_, err := BuildActionMatrix([]ActionContract{{
		Descriptor: descriptor, ImplementationEntry: "api.executeProjectAction",
		ImplementationKind: ImplementationAgentAdapter, MigrationStatus: MigrationStatusMigrated,
	}})
	if err == nil {
		t.Fatal("BuildActionMatrix() error = nil, want adapter migration claim error")
	}
}
