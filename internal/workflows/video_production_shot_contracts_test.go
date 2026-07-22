package workflows

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestCanonicalizeShotContractStatesAddsRequiredAssetsAndKeepsIdentityReachable(t *testing.T) {
	const (
		sceneID     = "10000000-0000-0000-0000-000000000001"
		characterID = "10000000-0000-0000-0000-000000000002"
		propID      = "10000000-0000-0000-0000-000000000003"
		extraID     = "10000000-0000-0000-0000-000000000004"
	)
	entry := validShotContractState(sceneID)
	exit := validShotContractState(sceneID)
	exit.Characters = []videoproduction.CharacterState{{
		AssetID: extraID,
		Blocking: videoproduction.BlockingState{
			Horizontal: "right", Depth: "midground", Facing: "screen_left",
		},
	}}

	entry, exit = canonicalizeShotContractStates(
		entry,
		exit,
		[]string{sceneID, characterID, propID},
		map[string]string{sceneID: "scene", characterID: "character", propID: "prop"},
	)

	review := videoproduction.ReviewShotContract(
		entry,
		exit,
		videoproduction.ShotTransition{
			TransitionType: videoproduction.TransitionUnclassified,
			TailPolicy:     videoproduction.TailPolicyNone,
			AnchorPolicy:   videoproduction.AnchorPolicyIndependent,
			Reset:          []string{"camera"},
			Confidence:     1,
		},
		[]string{sceneID, characterID, propID},
	)
	if !review.Approved {
		t.Fatalf("review = %+v, want approved", review)
	}
	if !shotStateHasCharacter(entry, characterID) || shotStateHasCharacter(exit, extraID) {
		t.Fatalf("entry/exit character identities were not canonicalized: entry=%+v exit=%+v", entry.Characters, exit.Characters)
	}
	if !shotStateHasProp(entry, propID) || !shotStateHasProp(exit, propID) {
		t.Fatalf("required prop was not carried through the shot: entry=%+v exit=%+v", entry.Props, exit.Props)
	}
}

func validShotContractState(sceneID string) videoproduction.ShotState {
	return videoproduction.ShotState{
		Scene:      videoproduction.SceneState{AssetID: sceneID, TimeOfDay: "night", Weather: "indoor"},
		Characters: []videoproduction.CharacterState{},
		Props:      []videoproduction.PropState{},
		Camera: videoproduction.CameraState{
			ShotSize: "medium", Angle: "eye_level", AxisSide: "A", LensIntent: "normal", Movement: "static",
		},
		Action:          videoproduction.ActionState{Entry: "动作开始", Exit: "动作结束"},
		ScreenDirection: "static",
	}
}
