package workflows

import (
	"strings"
	"testing"
)

func TestStoryboardShotRecordSelectSQLUsesPersistedPerShotDialogue(t *testing.T) {
	query := storyboardShotRecordSelectSQL("s.id = $1")
	if !strings.Contains(query, "COALESCE(s.script_dialogue, '[]'::jsonb)") {
		t.Fatal("storyboard shot query does not load the persisted per-shot dialogue assignment")
	}
	if strings.Contains(query, "timing_unit.source_text") {
		t.Fatal("storyboard shot query must not replace per-shot dialogue with the full timing-unit text")
	}
}

func TestStoryboardDialogueToGatewaySpansExcludesSystemAudio(t *testing.T) {
	spans := storyboardDialogueToGatewaySpans([]StoryboardDialogueLine{
		{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "这里是角色台词。", Kind: "dialogue", SpanStartTick: 0, SpanEndTick: 90000},
		{TimingUnitID: "sound-1", Text: "【音效：山风呼啸】", Kind: "system", SpanStartTick: 0, SpanEndTick: 90000},
	})
	if len(spans) != 1 || spans[0].TimingUnitID != "dialogue-1" || spans[0].Kind != "dialogue" {
		t.Fatalf("provider dialogue spans = %+v", spans)
	}
}
