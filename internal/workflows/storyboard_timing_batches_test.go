package workflows

import (
	"strings"
	"testing"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
)

func TestSplitEpisodeTimingBatchesUsesLevelTwoSceneHeadings(t *testing.T) {
	content := "# 第1集\n\n## 场景一：山巅\n\n### 1.1 包围\n\n画面一。\n\n## 场景二：雨夜\n\n### 2.1 山路\n\n画面二。"
	batches := splitEpisodeTimingBatches(content, nil)
	if len(batches) != 2 {
		t.Fatalf("batch count = %d, want 2: %+v", len(batches), batches)
	}
	if batches[0].Title != "场景一：山巅" || batches[1].Title != "场景二：雨夜" {
		t.Fatalf("batch titles = %q/%q", batches[0].Title, batches[1].Title)
	}
	if batches[0].SourceEndOffset != batches[1].SourceStartOffset {
		t.Fatalf("scene batches are not contiguous: %+v", batches)
	}
}

func TestSplitEpisodeTimingBatchesBoundsUnstructuredContent(t *testing.T) {
	content := strings.Repeat("剧情动作。", 900)
	batches := splitEpisodeTimingBatches(content, nil)
	if len(batches) < 2 {
		t.Fatalf("batch count = %d, want multiple batches", len(batches))
	}
	for _, batch := range batches {
		if length := len([]rune(batch.Content)); length > timingBatchMaximumRunes+timingBatchMaximumRunes/3 {
			t.Fatalf("batch %s is too large: %d", batch.Key, length)
		}
	}
}

func TestCanonicalizeAndMergeTimingBatchOutputsUsesGlobalStableOrdinals(t *testing.T) {
	firstRaw := `{"scenes":[{"sceneKey":"model-a","sceneOrdinal":7,"units":[{"unitKey":"model-u","unitOrdinal":9,"type":"montage","track":"video","text":"记忆闪回。"}]}]}`
	first, err := storyboardpkg.DecodeTimingAnalyzerOutput(firstRaw)
	if err != nil {
		t.Fatalf("decode first batch: %v", err)
	}
	first = canonicalizeTimingBatchOutput(first, 1, timingTextBatch{Ordinal: 0, ScriptSceneIDs: []string{"11111111-1111-1111-1111-111111111111"}})
	second := storyboardpkg.TimingAnalyzerOutput{Scenes: []storyboardpkg.TimingAnalyzerScene{{
		SceneKey:     "model-b",
		SceneOrdinal: 0,
		Units: []storyboardpkg.TimingAnalyzerUnit{{
			UnitKey: "model-u-2", UnitOrdinal: 0, Type: storyboardpkg.UnitDialogue, Track: storyboardpkg.TrackAudio, Text: "你好。",
		}},
	}}}
	merged, provenance, err := mergeTimingBatchOutputs([]timingBatchActivityOutput{
		{BatchKey: "batch-1", BatchOrdinal: 1, Semantic: second, ProviderCallID: "call-2", ModelID: "model-2"},
		{BatchKey: "batch-0", BatchOrdinal: 0, Semantic: first, ProviderCallID: "call-1", ModelID: "model-1"},
	}, 1)
	if err != nil {
		t.Fatalf("merge timing batches: %v", err)
	}
	if len(merged.Scenes) != 2 || merged.Scenes[0].SceneOrdinal != 0 || merged.Scenes[1].SceneOrdinal != 1 {
		t.Fatalf("merged scenes = %+v", merged.Scenes)
	}
	if merged.Scenes[0].Units[0].UnitOrdinal != 0 || merged.Scenes[1].Units[0].UnitOrdinal != 1 {
		t.Fatalf("merged unit ordinals = %d/%d", merged.Scenes[0].Units[0].UnitOrdinal, merged.Scenes[1].Units[0].UnitOrdinal)
	}
	if merged.Scenes[0].Units[0].Type != storyboardpkg.UnitTransition || merged.Scenes[0].Units[0].Track != storyboardpkg.TrackVisual {
		t.Fatalf("canonical alias = %s/%s", merged.Scenes[0].Units[0].Type, merged.Scenes[0].Units[0].Track)
	}
	if strings.Join(provenance.ProviderCallIDs, ",") != "call-1,call-2" {
		t.Fatalf("provider calls = %v", provenance.ProviderCallIDs)
	}
}
