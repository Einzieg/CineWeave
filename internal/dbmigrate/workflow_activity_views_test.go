package dbmigrate

import (
	"strings"
	"testing"
)

func TestWorkflowActivityViewsMigrationPreservesWorkflowHistory(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000070_workflow_activity_views.sql")
	for _, fragment := range []string{
		"CREATE TABLE workflow_activity_views",
		"PRIMARY KEY (project_id, user_id)",
		"cleared_terminal_through TIMESTAMPTZ NOT NULL",
		"REFERENCES projects(id) ON DELETE CASCADE",
		"REFERENCES users(id) ON DELETE CASCADE",
		"CREATE INDEX workflow_runs_project_created_idx",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000070 migration is missing %q", fragment)
		}
	}
	if strings.Contains(sql, "DELETE FROM workflow_runs") {
		t.Fatal("000070 must not delete workflow history")
	}
	if err := validateProtectedProviderMigration("000070_workflow_activity_views.sql", sql); err != nil {
		t.Fatalf("000070 writes protected Provider configuration: %v", err)
	}
}
