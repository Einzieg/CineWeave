package storyboard

import (
	"errors"
	"strings"
	"testing"
)

func TestTimingAnalyzerContractRequiresContiguousStableOrdinals(t *testing.T) {
	output := TimingAnalyzerOutput{Scenes: []TimingAnalyzerScene{{
		SceneKey:     "scene-1",
		SceneOrdinal: 0,
		Units: []TimingAnalyzerUnit{{
			UnitKey:     "unit-1",
			UnitOrdinal: 0,
			Type:        UnitDialogue,
			Track:       TrackAudio,
			Text:        "你来了。",
		}},
	}}}
	if err := ValidateTimingAnalyzerOutput(output); err != nil {
		t.Fatalf("validate timing analyzer output: %v", err)
	}
	output.Scenes[0].Units[0].UnitOrdinal = 2
	if err := ValidateTimingAnalyzerOutput(output); !errors.Is(err, ErrInvalidTimingAnalyzerOutput) {
		t.Fatalf("error = %v, want analyzer contract error", err)
	}
}

func TestParseTimingAnalyzerOutputNormalizesKnownAliases(t *testing.T) {
	output, err := ParseTimingAnalyzerOutput(`{"scenes":[{"sceneKey":"scene-1","sceneOrdinal":0,"units":[{"unitKey":"unit-1","unitOrdinal":0,"type":"montage","track":"video","text":"记忆闪回。"}]}]}`)
	if err != nil {
		t.Fatalf("parse timing analyzer output: %v", err)
	}
	unit := output.Scenes[0].Units[0]
	if unit.Type != UnitTransition || unit.Track != TrackVisual {
		t.Fatalf("normalized unit = %s/%s, want transition/visual", unit.Type, unit.Track)
	}
}

func TestParseTimingAnalyzerOutputNormalizesCombatType(t *testing.T) {
	output, err := ParseTimingAnalyzerOutput(`{"scenes":[{"sceneKey":"scene-1","sceneOrdinal":0,"units":[{"unitKey":"unit-1","unitOrdinal":0,"type":"combat","track":"visual","text":"群雄同时逼近。"}]}]}`)
	if err != nil {
		t.Fatalf("parse combat timing analyzer output: %v", err)
	}
	unit := output.Scenes[0].Units[0]
	if unit.Type != UnitAction || unit.Track != TrackVisual || unit.ActionKind != ActionCombat {
		t.Fatalf("normalized unit = %s/%s/%s, want action/visual/combat", unit.Type, unit.Track, unit.ActionKind)
	}
}

func TestParseTimingAnalyzerOutputRejectsUnknownAlias(t *testing.T) {
	_, err := ParseTimingAnalyzerOutput(`{"scenes":[{"sceneKey":"scene-1","sceneOrdinal":0,"units":[{"unitKey":"unit-1","unitOrdinal":0,"type":"unknown_beat","track":"visual","text":"未知。"}]}]}`)
	if !errors.Is(err, ErrInvalidTimingAnalyzerOutput) {
		t.Fatalf("error = %v, want analyzer contract error", err)
	}
}

func TestContinuityBlueprintRejectsDependencyCycle(t *testing.T) {
	output := ContinuityBlueprintOutput{
		Scenes: []ContinuityBlueprintScene{
			{SceneKey: "scene-1", SceneOrdinal: 0, PacingProfile: "standard", SuggestedShotMinimum: 1, SuggestedShotMaximum: 3},
			{SceneKey: "scene-2", SceneOrdinal: 1, PacingProfile: "standard", SuggestedShotMinimum: 1, SuggestedShotMaximum: 3},
		},
		Dependencies: []ContinuityBlueprintDependency{
			{FromSceneKey: "scene-1", ToSceneKey: "scene-2", Strong: true},
			{FromSceneKey: "scene-2", ToSceneKey: "scene-1", Strong: true},
		},
	}
	if err := ValidateContinuityBlueprint(output, []string{"scene-1", "scene-2"}); !errors.Is(err, ErrInvalidContinuityBlueprint) {
		t.Fatalf("error = %v, want blueprint contract error", err)
	}
}

func TestShotPlannerRejectsUnknownTimingUnit(t *testing.T) {
	output := ShotPlannerOutput{SceneKey: "scene-1", Shots: []ShotPlannerSuggestion{{
		SuggestionKey: "shot-1",
		TimingUnitIDs: []string{"unknown"},
		Visual:        "角色望向窗外",
	}}}
	if err := ValidateShotPlannerOutput(output, "scene-1", []string{"unit-1"}); !errors.Is(err, ErrInvalidShotPlannerOutput) {
		t.Fatalf("error = %v, want planner contract error", err)
	}
}

func TestDecodeShotPlannerOutputDefersUnknownTimingUnitValidation(t *testing.T) {
	output, err := DecodeShotPlannerOutput(`{"sceneKey":"scene-1","shots":[{"suggestionKey":"slot_001","timingUnitIds":["unit-typo"],"visual":"角色转身"}]}`, []string{"unit-1"})
	if err != nil {
		t.Fatalf("decode planner output before compiler alignment: %v", err)
	}
	if len(output.Shots) != 1 || output.Shots[0].TimingUnitIDs[0] != "unit-typo" {
		t.Fatalf("decoded planner output = %+v", output)
	}
	if err := ValidateShotPlannerOutput(output, "scene-1", []string{"unit-1"}); !errors.Is(err, ErrInvalidShotPlannerOutput) {
		t.Fatalf("unaligned output validation error = %v, want planner contract error", err)
	}
}

func TestParseShotPlannerOutputDropsStrayUnitAndMergesDuplicateRequirements(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1","unit-typo","unit-1"],"cutAfterTimingUnitId":"unit-typo","cutReason":"动作完成","visual":"人物转身","assetRequirements":[{"assetId":"asset-1","requirementType":"character_appearance","pose":"站立"},{"assetId":"asset-1","requirementType":"character_appearance","expression":"警觉"}]}]}`
	output, err := ParseShotPlannerOutput(raw, "scene-1", []string{"unit-1"})
	if err != nil {
		t.Fatalf("parse normalized shot planner output: %v", err)
	}
	shot := output.Shots[0]
	if len(shot.TimingUnitIDs) != 1 || shot.TimingUnitIDs[0] != "unit-1" || shot.CutAfterTimingUnitID != "" {
		t.Fatalf("normalized timing references = %+v / %q", shot.TimingUnitIDs, shot.CutAfterTimingUnitID)
	}
	if len(shot.AssetRequirements) != 1 || shot.AssetRequirements[0].Pose != "站立" || shot.AssetRequirements[0].Expression != "警觉" {
		t.Fatalf("normalized requirements = %+v", shot.AssetRequirements)
	}
}

func TestParseShotPlannerOutputDeduplicatesShotStateIdentitySets(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1"],"visual":"众人从两侧合围","plannedEntryState":{"characters":[{"assetId":"11111111-1111-4111-8111-111111111111","blocking":{"horizontal":"left","depth":"foreground","facing":"screen_right"}},{"assetId":"11111111-1111-4111-8111-111111111111","blocking":{"horizontal":"right","depth":"foreground","facing":"screen_left"}}],"props":[{"assetId":"22222222-2222-4222-8222-222222222222","state":"held"},{"assetId":"22222222-2222-4222-8222-222222222222","state":"held"}]},"plannedExitState":{"characters":[{"assetId":"11111111-1111-4111-8111-111111111111"},{"assetId":"11111111-1111-4111-8111-111111111111"}]}}]}`
	output, err := ParseShotPlannerOutput(raw, "scene-1", []string{"unit-1"})
	if err != nil {
		t.Fatalf("parse duplicated shot state identities: %v", err)
	}
	shot := output.Shots[0]
	if len(shot.PlannedEntryState.Characters) != 1 || len(shot.PlannedEntryState.Props) != 1 || len(shot.PlannedExitState.Characters) != 1 {
		t.Fatalf("deduplicated states = entry characters %d, entry props %d, exit characters %d", len(shot.PlannedEntryState.Characters), len(shot.PlannedEntryState.Props), len(shot.PlannedExitState.Characters))
	}
	if shot.PlannedEntryState.Characters[0].Blocking.Horizontal != "left" {
		t.Fatalf("first prominent placement was not preserved: %+v", shot.PlannedEntryState.Characters[0].Blocking)
	}
}

func TestParseShotPlannerOutputRejectsRemovedContinuityGroupKey(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1"],"visual":"人物转身","continuityGroupKey":"legacy-group"}]}`
	_, err := ParseShotPlannerOutput(raw, "scene-1", []string{"unit-1"})
	if !errors.Is(err, ErrInvalidShotPlannerOutput) || !strings.Contains(err.Error(), "continuityGroupKey") {
		t.Fatalf("error = %v, want removed continuityGroupKey contract error", err)
	}
}

func TestParseShotPlannerOutputCanonicalizesNestedScreenDirection(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1"],"visual":"人物转身","plannedEntryState":{"action":{"entry":"抬脚","exit":"落脚","screenDirection":"left_to_right"}},"plannedExitState":{"action":{"entry":"抬脚","exit":"落脚","screenDirection":"left_to_right"}}}]}`
	output, err := ParseShotPlannerOutput(raw, "scene-1", []string{"unit-1"})
	if err != nil {
		t.Fatalf("parse nested screenDirection: %v", err)
	}
	shot := output.Shots[0]
	if shot.PlannedEntryState.ScreenDirection != "left_to_right" || shot.PlannedExitState.ScreenDirection != "left_to_right" {
		t.Fatalf("canonical screen directions = %q / %q", shot.PlannedEntryState.ScreenDirection, shot.PlannedExitState.ScreenDirection)
	}
}

func TestDecodeShotPlannerOutputDropsDescriptiveSceneAndCharacterState(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1"],"visual":"人物站在山巅","plannedEntryState":{"scene":{"assetId":"11111111-1111-4111-8111-111111111111","state":"黄昏山巅说明"},"characters":[{"assetId":"22222222-2222-4222-8222-222222222222","state":"人物说明","blocking":{"horizontal":"left","depth":"foreground","facing":"camera"}}],"props":[{"assetId":"33333333-3333-4333-8333-333333333333","state":"held","holderAssetId":"22222222-2222-4222-8222-222222222222"}]}}]}`
	output, err := DecodeShotPlannerOutput(raw, []string{"unit-1"})
	if err != nil {
		t.Fatalf("decode descriptive state noise: %v", err)
	}
	state := output.Shots[0].PlannedEntryState
	if state.Scene.AssetID == "" || len(state.Characters) != 1 {
		t.Fatalf("canonical state lost identities: %+v", state)
	}
	if len(state.Props) != 1 || state.Props[0].State != "held" {
		t.Fatalf("contractual prop state was removed: %+v", state.Props)
	}
}

func TestParseShotPlannerOutputRejectsConflictingScreenDirection(t *testing.T) {
	raw := `{"sceneKey":"scene-1","shots":[{"suggestionKey":"shot-1","timingUnitIds":["unit-1"],"visual":"人物转身","plannedEntryState":{"action":{"entry":"抬脚","exit":"落脚","screenDirection":"left_to_right"},"screenDirection":"right_to_left"}}]}`
	_, err := ParseShotPlannerOutput(raw, "scene-1", []string{"unit-1"})
	if !errors.Is(err, ErrInvalidShotPlannerOutput) || !strings.Contains(err.Error(), "conflicting screenDirection") {
		t.Fatalf("error = %v, want conflicting screenDirection contract error", err)
	}
}

func TestReviewerCannotApproveBlockingIssue(t *testing.T) {
	shot := 0
	output := StoryboardReviewerOutput{
		Approved: true,
		Issues: []StoryboardReviewerIssue{{
			Code:        "DIALOGUE_MISSING",
			Severity:    "error",
			Message:     "缺少对白",
			SceneKey:    "scene-1",
			ShotOrdinal: &shot,
		}},
	}
	if err := ValidateStoryboardReviewerOutput(output, []string{"scene-1"}, []string{"unit-1"}, 1); !errors.Is(err, ErrInvalidStoryboardReview) {
		t.Fatalf("error = %v, want reviewer contract error", err)
	}
}
