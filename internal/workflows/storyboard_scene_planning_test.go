package workflows

import (
	"strings"
	"testing"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
)

func TestAlignPlannerOutputToShotSlotsCreatesOneCreativePerRealShot(t *testing.T) {
	shots := []storyboardpkg.ShotDraft{
		{Ordinal: 0, Spans: []storyboardpkg.TimingSpan{{TimingUnitID: "unit-a"}}},
		{Ordinal: 1, Spans: []storyboardpkg.TimingSpan{{TimingUnitID: "unit-b"}}},
	}
	output := storyboardpkg.ShotPlannerOutput{SceneKey: "scene-1", Shots: []storyboardpkg.ShotPlannerSuggestion{
		{SuggestionKey: "model-key-a", TimingUnitIDs: []string{"unit-b"}, Visual: "人物走入长廊"},
		{SuggestionKey: "model-key-b", TimingUnitIDs: []string{"unit-a"}, Visual: "反打守候者抬眼"},
	}}
	got, err := alignPlannerOutputToShotSlots(output, shots)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shots[0].SuggestionKey != "slot_001" || got.Shots[1].SuggestionKey != "slot_002" {
		t.Fatalf("slot keys = %+v", got.Shots)
	}
	if len(got.Shots[0].TimingUnitIDs) != 1 || got.Shots[0].TimingUnitIDs[0] != "unit-a" || got.Shots[1].TimingUnitIDs[0] != "unit-b" {
		t.Fatalf("slot timing units = %+v", got.Shots)
	}
}

func TestAlignPlannerOutputToShotSlotsRejectsRepeatedVisual(t *testing.T) {
	shots := []storyboardpkg.ShotDraft{{Ordinal: 0}, {Ordinal: 1}}
	output := storyboardpkg.ShotPlannerOutput{Shots: []storyboardpkg.ShotPlannerSuggestion{
		{SuggestionKey: "slot_001", Visual: "相同画面"},
		{SuggestionKey: "slot_002", Visual: " 相同画面 "},
	}}
	if _, err := alignPlannerOutputToShotSlots(output, shots); err == nil || !strings.Contains(err.Error(), "same visual") {
		t.Fatalf("duplicate visual error = %v", err)
	}
}

func TestValidateShotPlannerAssetReferencesRejectsUnknownAsset(t *testing.T) {
	output := storyboardpkg.ShotPlannerOutput{Shots: []storyboardpkg.ShotPlannerSuggestion{{
		SuggestionKey:     "shot-1",
		AssetRequirements: []storyboardpkg.ShotPlannerAssetRequirement{{AssetID: "asset-missing", RequirementType: "character_appearance"}},
	}}}
	if err := validateShotPlannerAssetReferences(output, []CanonicalAssetRecord{{ID: "asset-known"}}); err == nil {
		t.Fatal("expected unknown asset reference to fail")
	}
}

func TestFilterUnknownPlannerAssetReferencesKeepsKnownRequirements(t *testing.T) {
	output := storyboardpkg.ShotPlannerOutput{Shots: []storyboardpkg.ShotPlannerSuggestion{{
		SuggestionKey: "shot-1",
		AssetRequirements: []storyboardpkg.ShotPlannerAssetRequirement{
			{AssetID: "asset-known", RequirementType: "character_appearance"},
			{AssetID: "asset-hallucinated", RequirementType: "prop_state"},
		},
	}}}
	filtered := filterUnknownPlannerAssetReferences(output, []CanonicalAssetRecord{{ID: "asset-known"}})
	if len(filtered.Shots[0].AssetRequirements) != 1 || filtered.Shots[0].AssetRequirements[0].AssetID != "asset-known" {
		t.Fatalf("filtered requirements = %+v", filtered.Shots[0].AssetRequirements)
	}
}

func TestSceneShotSlotsKeepDeterministicTimingOwnership(t *testing.T) {
	shots := []storyboardpkg.ShotDraft{{Ordinal: 0, StartTick: 0, EndTick: 90_000, DurationTicks: 90_000, Spans: []storyboardpkg.TimingSpan{{TimingUnitID: "unit-1", StartTick: 0, EndTick: 90_000}}}}
	units := []sceneTimingUnitRecord{{Unit: storyboardpkg.TimingUnit{ID: "unit-1", Type: storyboardpkg.UnitDialogue, Track: storyboardpkg.TrackAudio, Speaker: "方源", SourceText: "原文台词", StartTick: 0, EndTick: 90_000}}}
	slots := sceneShotSlotsForPrompt(shots, units)
	if len(slots) != 1 || slots[0]["slotKey"] != "slot_001" || slots[0]["durationTicks"] != int64(90_000) {
		t.Fatalf("shot slots = %+v", slots)
	}
}

func TestValidateShotPlannerImageDialogueIsolation(t *testing.T) {
	units := []sceneTimingUnitRecord{{Unit: storyboardpkg.TimingUnit{Type: storyboardpkg.UnitDialogue, SourceText: "你终于来了"}}}
	output := storyboardpkg.ShotPlannerOutput{Shots: []storyboardpkg.ShotPlannerSuggestion{{
		SuggestionKey: "shot-1", Visual: "人物站在门口", ImagePromptDirection: "画面字幕：你终于来了",
	}}}
	err := validateShotPlannerImageDialogueIsolation(output, units)
	if err == nil || !strings.Contains(err.Error(), "leaks script dialogue") {
		t.Fatalf("dialogue leak error = %v", err)
	}
	output.Shots[0].ImagePromptDirection = "逆光门廊，人物回头"
	output.Shots[0].VideoPromptDirection = "人物说：你终于来了"
	if err := validateShotPlannerImageDialogueIsolation(output, units); err != nil {
		t.Fatalf("video dialogue should be allowed: %v", err)
	}
}

func TestApplyScenePlannerRetryGuidanceRequiresCompleteJSONAndShotStates(t *testing.T) {
	context := map[string]any{}
	applyScenePlannerRetryGuidance(context, scenePlannerRetryFeedback{
		ErrorCode:    "INVALID_REQUEST",
		ErrorMessage: "plannedEntryState: action.entry and action.exit are required",
	})

	requirements, ok := context["outputRequirements"].(map[string]any)
	if !ok {
		t.Fatalf("output requirements = %#v", context["outputRequirements"])
	}
	if !strings.Contains(requirements["json"].(string), "UUID 字符串必须保留结束引号") {
		t.Fatalf("json requirement = %q", requirements["json"])
	}
	if !strings.Contains(requirements["shotStateActions"].(string), "plannedEntryState.action") ||
		!strings.Contains(requirements["shotStateActions"].(string), "plannedExitState.action") {
		t.Fatalf("shot state requirement = %q", requirements["shotStateActions"])
	}

	feedback, ok := context["retryFeedback"].(map[string]any)
	if !ok {
		t.Fatalf("retry feedback = %#v", context["retryFeedback"])
	}
	if feedback["errorCode"] != "INVALID_REQUEST" ||
		!strings.Contains(feedback["errorMessage"].(string), "action.entry") ||
		!strings.Contains(feedback["instruction"].(string), "不得原样重复失败结构") {
		t.Fatalf("retry feedback = %#v", feedback)
	}
}

func TestApplyScenePlannerRetryGuidanceOmitsEmptyFeedback(t *testing.T) {
	context := map[string]any{}
	applyScenePlannerRetryGuidance(context, scenePlannerRetryFeedback{})
	if _, exists := context["retryFeedback"]; exists {
		t.Fatalf("empty retry feedback should be omitted: %#v", context["retryFeedback"])
	}
}
