package api

import "testing"

func TestProjectRevisionConflictIncludesCurrentBasicSnapshot(t *testing.T) {
	description := "current description"
	conflict := projectRevisionConflict(Project{
		Name:        "current name",
		Description: &description,
		Revision:    7,
	}, 5)

	if conflict.Code != "PROJECT_REVISION_CONFLICT" || conflict.Status != 409 {
		t.Fatalf("conflict = %+v", conflict)
	}
	details, ok := conflict.Details.(map[string]any)
	if !ok {
		t.Fatalf("details type = %T", conflict.Details)
	}
	snapshot, ok := details["currentSnapshot"].(map[string]any)
	if !ok {
		t.Fatalf("currentSnapshot = %#v", details["currentSnapshot"])
	}
	if snapshot["name"] != "current name" || snapshot["description"] != description || snapshot["revision"] != int64(7) {
		t.Fatalf("currentSnapshot = %#v", snapshot)
	}
}
