package projectdeletion

import (
	"strings"
	"testing"
)

func TestStorageCatalogUsesCurrentSchemaAndStablePlaceholders(t *testing.T) {
	candidates := StorageCandidateUnion("$2")
	shared := SharedStorageReferenceQuery("$1", "$2")

	for _, query := range []string{candidates, shared} {
		if strings.Contains(query, "storyboard_shot_continuity_frames") {
			t.Fatal("storage catalog references removed storyboard_shot_continuity_frames table")
		}
		for _, required := range []string{"shot_visual_anchors", "shot_reference_pack_items"} {
			if !strings.Contains(query, required) {
				t.Fatalf("storage catalog query is missing %s", required)
			}
		}
	}
	if strings.Contains(candidates, "$PROJECT_ID") ||
		strings.Contains(shared, "$PROJECT_ID") ||
		strings.Contains(shared, "$STORAGE_KEY") {
		t.Fatal("storage catalog contains an unresolved SQL placeholder")
	}
}

func TestStorageCatalogRejectsUnexpectedPlaceholder(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("StorageCandidateUnion() did not reject an unexpected placeholder")
		}
	}()
	_ = StorageCandidateUnion("$3")
}
