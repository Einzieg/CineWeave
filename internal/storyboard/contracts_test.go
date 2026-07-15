package storyboard

import (
	"errors"
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
