package workflows

import (
	"strings"
	"testing"
)

func TestSelectSourceToScriptTargetsKeepsOnlyExplicitChapterAfterMatch(t *testing.T) {
	snapshot := sourceToScriptSourceSnapshot{
		Source: ProjectSourceRecord{ID: "source-1", SourceType: "novel"},
		Items: []sourceToScriptSnapshotItem{
			{SourceToScriptManifestItem: SourceToScriptManifestItem{ItemKey: "chapter-1", SourceChapterID: "chapter-1", ManifestOrdinal: 1}},
			{SourceToScriptManifestItem: SourceToScriptManifestItem{ItemKey: "chapter-2", SourceChapterID: "chapter-2", ManifestOrdinal: 2}},
			{SourceToScriptManifestItem: SourceToScriptManifestItem{ItemKey: "chapter-3", SourceChapterID: "chapter-3", ManifestOrdinal: 3}},
		},
	}

	refs, err := selectSourceToScriptTargets(&snapshot, []string{"chapter-1"})
	if err != nil {
		t.Fatalf("selectSourceToScriptTargets: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "chapter-1" {
		t.Fatalf("refs = %+v, want only chapter-1", refs)
	}
	if !snapshot.Items[0].IsTarget || snapshot.Items[1].IsTarget || snapshot.Items[2].IsTarget {
		t.Fatalf("target flags = [%v %v %v], want [true false false]", snapshot.Items[0].IsTarget, snapshot.Items[1].IsTarget, snapshot.Items[2].IsTarget)
	}
}

func TestSelectSourceToScriptTargetsSelectsAllOnlyWhenRequestIsEmpty(t *testing.T) {
	snapshot := sourceToScriptSourceSnapshot{
		Source: ProjectSourceRecord{ID: "source-1", SourceType: "novel"},
		Items: []sourceToScriptSnapshotItem{
			{SourceToScriptManifestItem: SourceToScriptManifestItem{ItemKey: "chapter-1", SourceChapterID: "chapter-1", ManifestOrdinal: 1}},
			{SourceToScriptManifestItem: SourceToScriptManifestItem{ItemKey: "chapter-2", SourceChapterID: "chapter-2", ManifestOrdinal: 2}},
		},
	}

	refs, err := selectSourceToScriptTargets(&snapshot, nil)
	if err != nil {
		t.Fatalf("selectSourceToScriptTargets: %v", err)
	}
	if len(refs) != 2 || !snapshot.Items[0].IsTarget || !snapshot.Items[1].IsTarget {
		t.Fatalf("all-target selection = refs %+v flags [%v %v]", refs, snapshot.Items[0].IsTarget, snapshot.Items[1].IsTarget)
	}
}

func TestSourceToScriptManifestTargetsRejectsDrift(t *testing.T) {
	manifest := SourceToScriptGenerationManifest{
		SchemaVersion:      2,
		SourceType:         "novel",
		TargetItemKeys:     []string{"chapter-1"},
		TargetChapterIDs:   []string{"chapter-1"},
		SeriesEpisodeTotal: 2,
		Items: []SourceToScriptManifestItem{
			{ItemKey: "chapter-1", SourceChapterID: "chapter-1", ManifestOrdinal: 1, IsTarget: true},
			{ItemKey: "chapter-2", SourceChapterID: "chapter-2", ManifestOrdinal: 2, IsTarget: true},
		},
	}

	_, err := sourceToScriptManifestTargets(manifest)
	if err == nil || !strings.Contains(err.Error(), "flags do not match") {
		t.Fatalf("error = %v, want target flag mismatch", err)
	}
}
