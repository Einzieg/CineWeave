package videoproduction

import (
	"strings"
	"testing"
)

func TestCompilePanelManifestUsesDurationBandsAndOrderedStates(t *testing.T) {
	entry := panelManifestTestState("准备拔剑")
	exit := panelManifestTestState("完成拔剑")
	exit.Characters[0].Pose = "drawn_sword"
	exit.Action.Entry = "准备拔剑"
	exit.Action.Exit = "完成拔剑"
	for _, test := range []struct {
		name          string
		durationTicks int64
		aspectRatio   string
		panelCount    int
		rows          int
		columns       int
		sheetRatio    string
	}{
		{name: "short landscape", durationTicks: 5 * 24, aspectRatio: "16:9", panelCount: 3, rows: 3, columns: 1, sheetRatio: "2:3"},
		{name: "medium", durationTicks: 8 * 24, aspectRatio: "16:9", panelCount: 4, rows: 2, columns: 2, sheetRatio: "16:9"},
		{name: "long portrait", durationTicks: 15 * 24, aspectRatio: "9:16", panelCount: 6, rows: 2, columns: 3, sheetRatio: "1:1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := CompilePanelManifest(PanelManifestCompileInput{
				StoryboardShotID: "shot-1", PlannedDurationTicks: test.durationTicks,
				TimelineTimebase: 24, VideoAspectRatio: test.aspectRatio,
				EntryState: entry, ExitState: exit,
			})
			if err != nil {
				t.Fatalf("CompilePanelManifest: %v", err)
			}
			if manifest.PanelCount != test.panelCount || manifest.Rows != test.rows || manifest.Columns != test.columns || manifest.SheetAspectRatio != test.sheetRatio {
				t.Fatalf("manifest layout = %+v", manifest)
			}
			if manifest.Panels[0].Stage != "entry" || manifest.Panels[0].TimeTick != 0 ||
				manifest.Panels[len(manifest.Panels)-1].Stage != "exit" || manifest.Panels[len(manifest.Panels)-1].TimeTick != test.durationTicks {
				t.Fatalf("manifest endpoints = %+v", manifest.Panels)
			}
			if manifest.Panels[len(manifest.Panels)-1].ExpectedState.Characters[0].Pose != "drawn_sword" || !strings.Contains(manifest.Panels[1].ActionStage, "拔剑") {
				t.Fatalf("manifest action sequence = %+v", manifest.Panels)
			}
		})
	}
}

func TestValidatePanelManifestRejectsReorderedPanelsAndHashDrift(t *testing.T) {
	state := panelManifestTestState("站定")
	manifest, err := CompilePanelManifest(PanelManifestCompileInput{
		StoryboardShotID: "shot-1", PlannedDurationTicks: 120, TimelineTimebase: 24,
		VideoAspectRatio: "16:9", EntryState: state, ExitState: state,
	})
	if err != nil {
		t.Fatalf("CompilePanelManifest: %v", err)
	}
	manifest.Panels[1].Ordinal = 3
	if err := ValidatePanelManifest(manifest); err == nil {
		t.Fatal("reordered panel manifest was accepted")
	}
	manifest, _ = CompilePanelManifest(PanelManifestCompileInput{
		StoryboardShotID: "shot-1", PlannedDurationTicks: 120, TimelineTimebase: 24,
		VideoAspectRatio: "16:9", EntryState: state, ExitState: state,
	})
	manifest.Panels[1].ActionStage = "被篡改"
	if err := ValidatePanelManifest(manifest); err == nil {
		t.Fatal("manifest hash drift was accepted")
	}
}

func panelManifestTestState(action string) ShotState {
	return ShotState{
		Scene: SceneState{AssetID: "11111111-1111-4111-8111-111111111111", TimeOfDay: "night", Weather: "clear"},
		Characters: []CharacterState{{
			AssetID: "22222222-2222-4222-8222-222222222222", Pose: "standing", Expression: "focused",
			Blocking: BlockingState{Horizontal: "center", Depth: "midground", Facing: "camera"},
		}},
		Props: []PropState{}, Camera: CameraState{ShotSize: "medium", Angle: "eye_level", AxisSide: "A", LensIntent: "normal", Movement: "static"},
		Action: ActionState{Entry: action, Exit: action}, ScreenDirection: "static",
	}
}
