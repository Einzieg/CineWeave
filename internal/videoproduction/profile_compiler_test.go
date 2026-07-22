package videoproduction

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltInProfileCompilerContracts(t *testing.T) {
	tests := []struct {
		profileKey      string
		implementation  string
		anchors         []string
		initialContract string
		videoRoles      []string
	}{
		{ProfileSingleFrameI2V, ImplementationAvailable, []string{AnchorRolePlannedFirstFrame}, InputContractFirstFrame, []string{ReferenceRoleFirstFrame}},
		{ProfileFirstLastFrame, ImplementationReserved, []string{AnchorRolePlannedFirstFrame, AnchorRolePlannedLastFrame}, InputContractFirstLastFrames, []string{ReferenceRoleFirstFrame, ReferenceRoleLastFrame}},
		{ProfileMultimodalReference, ImplementationReserved, []string{AnchorRolePlannedFirstFrame}, InputContractFirstFramePlusReferences, []string{ReferenceRoleFirstFrame, ReferenceRoleCharacterIdentity, ReferenceRoleSceneIdentity}},
		{ProfileStoryboardSheet, ImplementationReserved, []string{AnchorRoleStoryboardSheet, AnchorRoleStoryboardPanel}, InputContractStoryboardSheetReference, []string{ReferenceRoleStoryboardSheet}},
	}
	compiler := NewProfileCompiler()
	for _, test := range tests {
		t.Run(test.profileKey, func(t *testing.T) {
			version := profileCompilerFixtureVersion(test.profileKey, test.implementation, test.anchors, test.initialContract)
			compiled, err := compiler.Compile(version, false)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if compiled.InitialInputContract != test.initialContract {
				t.Fatalf("initial contract = %s, want %s", compiled.InitialInputContract, test.initialContract)
			}
			for _, role := range test.videoRoles {
				if !compiled.Strategy.References().Allows(ReferencePurposeVideo, role) {
					t.Fatalf("video role %s is not allowed", role)
				}
			}
			for _, role := range []string{PromptRoleAnchorPlan, PromptRoleAnchorGenerate, PromptRoleAnchorReview, PromptRoleVideoGenerate, PromptRoleVideoReview} {
				key, keyErr := compiled.Strategy.Prompts().TemplateKey(role)
				if keyErr != nil || key != "video_profile."+test.profileKey+"."+role {
					t.Fatalf("template key %s = %q / %v", role, key, keyErr)
				}
			}
			_, availableErr := compiler.Compile(version, true)
			if test.implementation == ImplementationAvailable && availableErr != nil {
				t.Fatalf("available profile rejected: %v", availableErr)
			}
			if test.implementation == ImplementationReserved && availableErr == nil {
				t.Fatal("reserved profile was accepted as executable")
			}
		})
	}
}

func TestProfileStrategiesPreservePromptSafetyRules(t *testing.T) {
	cues := []DialogueCue{{Speaker: "方源", Text: "今日我便破境。", Kind: "dialogue"}}
	for _, profileKey := range []string{ProfileSingleFrameI2V, ProfileFirstLastFrame, ProfileMultimodalReference, ProfileStoryboardSheet} {
		imageReview := ReviewImagePrompt(profileKey, "电影画面，画面字幕：今日我便破境。", cues)
		if imageReview.Approved {
			t.Fatalf("%s image reviewer accepted dialogue leakage", profileKey)
		}
		videoReview := ReviewVideoPrompt(profileKey, "方源转身离开。", cues, true, true)
		if videoReview.Approved {
			t.Fatalf("%s video reviewer accepted missing verbatim dialogue", profileKey)
		}
		approved := ReviewVideoPrompt(profileKey, "方源转身，并用中文逐字说：今日我便破境。", cues, true, true)
		if !approved.Approved {
			t.Fatalf("%s video reviewer rejected valid dialogue: %+v", profileKey, approved.Issues)
		}
	}
}

func TestProfilePlannerContractsDeclareScreenDirectionAtShotStateLevel(t *testing.T) {
	for _, profileKey := range []string{ProfileSingleFrameI2V, ProfileFirstLastFrame, ProfileMultimodalReference, ProfileStoryboardSheet} {
		strategy, err := ProfileStrategyFor(profileKey)
		if err != nil {
			t.Fatal(err)
		}
		contract := strategy.Anchors().PlannerContract()
		if !strings.Contains(contract, `"action":{"entry":"","exit":""},"screenDirection":""`) {
			t.Fatalf("%s planner contract does not declare screenDirection beside action", profileKey)
		}
		if !strings.Contains(contract, "禁止放入 action 内部") {
			t.Fatalf("%s planner contract does not reject nested screenDirection", profileKey)
		}
	}
}

func TestStoryboardSheetRuntimeStrategyMatchesPublishedManifestContract(t *testing.T) {
	strategy, err := ProfileStrategyFor(ProfileStoryboardSheet)
	if err != nil {
		t.Fatal(err)
	}
	requirements := strategy.Anchors().Requirements()
	var panels AnchorRequirement
	for _, requirement := range requirements {
		if requirement.Role == AnchorRoleStoryboardPanel {
			panels = requirement
		}
	}
	if panels.Minimum != 3 || panels.Maximum != 6 {
		t.Fatalf("storyboard panel requirement = %+v", panels)
	}
	continuation := strategy.InputAdapter().ContinuationContracts()
	if len(continuation) != 1 || continuation[0] != InputContractVideoExtension {
		t.Fatalf("storyboard sheet continuation contracts = %#v", continuation)
	}
}

func TestProfileReferenceStrategiesRejectWrongCanonicalRoles(t *testing.T) {
	stateHash := strings.Repeat("a", 64)
	base := ReferenceResolveInput{
		Purpose: ReferencePurposeVideo, ShotStateRevision: 1,
		ProfileSnapshotHash: strings.Repeat("b", 64), ShotStateHash: stateHash,
		CapabilitySnapshotHash: strings.Repeat("c", 64), MaxReferences: 8,
	}
	first := ReferenceCandidate{ReferenceKey: "first", Role: ReferenceRoleFirstFrame, Required: true, Active: true, Fresh: true, ContentHash: strings.Repeat("d", 64)}
	last := ReferenceCandidate{ReferenceKey: "last", Role: ReferenceRoleLastFrame, Required: true, Active: true, Fresh: true, ContentHash: strings.Repeat("e", 64)}
	sheet := ReferenceCandidate{ReferenceKey: "sheet", Role: ReferenceRoleStoryboardSheet, Required: true, Active: true, Fresh: true, ContentHash: strings.Repeat("f", 64)}

	base.ProfileKey = ProfileSingleFrameI2V
	base.Candidates = []ReferenceCandidate{first, last}
	pack, err := ResolveReferencePack(base)
	if err != nil || len(pack.Manifest.Items) != 1 || pack.Manifest.Items[0].Role != ReferenceRoleFirstFrame {
		t.Fatalf("single frame pack = %+v / %v", pack.Manifest.Items, err)
	}
	base.ProfileKey = ProfileFirstLastFrame
	base.Candidates = []ReferenceCandidate{first}
	if _, err := ResolveReferencePack(base); err == nil {
		t.Fatal("first/last profile accepted a missing last frame")
	}
	base.Candidates = []ReferenceCandidate{first, last}
	if _, err := ResolveReferencePack(base); err != nil {
		t.Fatalf("first/last pack rejected: %v", err)
	}
	base.ProfileKey = ProfileStoryboardSheet
	base.Candidates = []ReferenceCandidate{sheet, first}
	pack, err = ResolveReferencePack(base)
	if err != nil || len(pack.Manifest.Items) != 1 || pack.Manifest.Items[0].Role != ReferenceRoleStoryboardSheet {
		t.Fatalf("storyboard sheet pack = %+v / %v", pack.Manifest.Items, err)
	}
}

func TestMultimodalReferenceResolverPreservesTypedSemanticsAndLimits(t *testing.T) {
	hash := func(character string) string { return strings.Repeat(character, 64) }
	first := ReferenceCandidate{ReferenceKey: "first", Role: ReferenceRoleFirstFrame, Required: true, Active: true, Fresh: true, ContentHash: hash("1")}
	character := ReferenceCandidate{ReferenceKey: "character", Role: ReferenceRoleCharacterIdentity, Required: true, AssetID: "character", Active: true, Fresh: true, ContentHash: hash("2")}
	scene := ReferenceCandidate{ReferenceKey: "scene", Role: ReferenceRoleSceneIdentity, Required: true, AssetID: "scene", Active: true, Fresh: true, ContentHash: hash("3")}
	prop := ReferenceCandidate{ReferenceKey: "prop", Role: ReferenceRolePropIdentity, Required: true, AssetID: "prop", Active: true, Fresh: true, ContentHash: hash("4")}
	video := ReferenceCandidate{ReferenceKey: "motion", Role: ReferenceRoleMotion, MediaType: "video", Priority: 300, Active: true, Fresh: true, ContentHash: hash("5")}
	audio := ReferenceCandidate{ReferenceKey: "voice", Role: ReferenceRoleAudio, MediaType: "audio", Priority: 200, Active: true, Fresh: true, ContentHash: hash("6")}
	continuity := ReferenceCandidate{ReferenceKey: "tail", Role: ReferenceRoleContinuityHint, Priority: 100, Active: true, Fresh: true, ContentHash: hash("7")}
	style := ReferenceCandidate{ReferenceKey: "style", Role: ReferenceRoleStyle, Priority: 50, Active: true, Fresh: true, ContentHash: hash("8")}

	pack, err := ResolveReferencePack(ReferenceResolveInput{
		ProfileKey: ProfileMultimodalReference, Purpose: ReferencePurposeVideo, ShotStateRevision: 1,
		ProfileSnapshotHash: hash("a"), ShotStateHash: hash("b"), CapabilitySnapshotHash: hash("c"),
		RequiredAssetIDs: []string{"character", "scene", "prop"},
		MaxReferences:    6, MaxImageReferences: 4, MaxVideoReferences: 1, MaxAudioReferences: 1,
		Candidates: []ReferenceCandidate{style, continuity, audio, video, prop, scene, character, first},
	})
	if err != nil {
		t.Fatalf("ResolveReferencePack: %v", err)
	}
	if len(pack.Manifest.Items) != 6 {
		t.Fatalf("items = %+v", pack.Manifest.Items)
	}
	roles := map[string]ReferencePackItem{}
	for _, item := range pack.Manifest.Items {
		roles[item.Role] = item
	}
	for _, role := range []string{ReferenceRoleFirstFrame, ReferenceRoleCharacterIdentity, ReferenceRoleSceneIdentity, ReferenceRolePropIdentity, ReferenceRoleMotion, ReferenceRoleAudio} {
		if roles[role].Role == "" || roles[role].MediaType == "" || roles[role].Semantics == "" {
			t.Fatalf("typed role %s missing from %+v", role, pack.Manifest.Items)
		}
	}
	if _, exists := roles[ReferenceRoleContinuityHint]; exists {
		t.Fatal("lower-priority continuity hint was not deterministically trimmed")
	}
	if roles[ReferenceRoleMotion].MediaType != "video" || roles[ReferenceRoleAudio].MediaType != "audio" {
		t.Fatalf("media roles = motion:%s audio:%s", roles[ReferenceRoleMotion].MediaType, roles[ReferenceRoleAudio].MediaType)
	}

	_, err = ResolveReferencePack(ReferenceResolveInput{
		ProfileKey: ProfileMultimodalReference, Purpose: ReferencePurposeVideo, ShotStateRevision: 1,
		ProfileSnapshotHash: hash("a"), ShotStateHash: hash("b"), CapabilitySnapshotHash: hash("c"),
		RequiredAssetIDs: []string{"character", "scene", "prop"},
		MaxReferences:    8, MaxImageReferences: 3,
		Candidates: []ReferenceCandidate{first, character, scene, prop},
	})
	if err == nil {
		t.Fatal("resolver accepted an image limit that drops a required asset")
	}
}

func profileCompilerFixtureVersion(profileKey, implementation string, anchors []string, initialContract string) ProfileVersion {
	prompts := map[string]string{}
	for _, role := range []string{PromptRoleAnchorPlan, PromptRoleAnchorGenerate, PromptRoleAnchorReview, PromptRoleVideoGenerate, PromptRoleVideoReview} {
		prompts[promptContractField(role)] = "video_profile." + profileKey + "." + role
	}
	configuration, _ := json.Marshal(map[string]any{"anchorRoles": anchors})
	capabilities, _ := json.Marshal(map[string]any{"initialInputContract": initialContract})
	promptContract, _ := json.Marshal(prompts)
	return ProfileVersion{
		ID: profileKey + "-version", ProfileKey: profileKey, Version: 1,
		LifecycleState: LifecyclePublished, ImplementationState: implementation,
		Configuration: configuration, CapabilityRequirements: capabilities,
		PromptContract: promptContract, InputContractVersion: "video-input-contract/v1",
	}
}
