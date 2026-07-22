package videoproduction

import (
	"errors"
	"strings"
	"testing"
)

const (
	testSceneID     = "11111111-1111-4111-8111-111111111111"
	testCharacterID = "22222222-2222-4222-8222-222222222222"
	testPropID      = "33333333-3333-4333-8333-333333333333"
)

func validTestShotState() ShotState {
	return ShotState{
		Scene: SceneState{AssetID: testSceneID, TimeOfDay: "dusk", Weather: "light_rain", Lighting: "cool_backlight"},
		Characters: []CharacterState{{
			AssetID:    testCharacterID,
			Pose:       "standing",
			Expression: "guarded",
			Blocking:   BlockingState{Horizontal: "left", Depth: "foreground", Facing: "screen_right"},
		}},
		Props:           []PropState{{AssetID: testPropID, State: "held", HolderAssetID: testCharacterID}},
		Camera:          CameraState{ShotSize: "medium", Angle: "eye_level", AxisSide: "A", LensIntent: "normal", Movement: "dolly_in"},
		Action:          ActionState{Entry: "角色刚转向对手", Exit: "角色停在对峙姿态"},
		ScreenDirection: "left_to_right",
	}
}

func TestShotStateValidationAndHashAreDeterministic(t *testing.T) {
	state := validTestShotState()
	if err := ValidateShotState(state); err != nil {
		t.Fatalf("validate state: %v", err)
	}
	first, err := HashShotState(state)
	if err != nil {
		t.Fatalf("hash state: %v", err)
	}
	state.Characters = append([]CharacterState{{
		AssetID:  "44444444-4444-4444-8444-444444444444",
		Blocking: BlockingState{Horizontal: "right", Depth: "background", Facing: "screen_left"},
	}}, state.Characters...)
	second, err := HashShotState(state)
	if err != nil {
		t.Fatalf("hash expanded state: %v", err)
	}
	state.Characters[0], state.Characters[1] = state.Characters[1], state.Characters[0]
	third, err := HashShotState(state)
	if err != nil {
		t.Fatalf("hash reordered state: %v", err)
	}
	if second != third || first == second || len(first) != 64 {
		t.Fatalf("state hashes are not canonical: %s %s %s", first, second, third)
	}
}

func TestTransitionClassifierUsesConservativeResetPrecedence(t *testing.T) {
	previous := validTestShotState()
	current := validTestShotState()
	current.Scene.AssetID = "55555555-5555-4555-8555-555555555555"
	transition, err := ClassifyTransition(&previous, current, TransitionSuggestion{TransitionType: TransitionMatchAction, TailPolicy: "hard", Confidence: 0.9})
	if err != nil {
		t.Fatalf("classify scene transition: %v", err)
	}
	if transition.TransitionType != TransitionSceneCut || transition.TailPolicy != TailPolicyNone || transition.AnchorPolicy != AnchorPolicyIndependent {
		t.Fatalf("scene transition = %+v", transition)
	}
	current = validTestShotState()
	current.Camera.ShotSize = "close_up"
	transition, err = ClassifyTransition(&previous, current, TransitionSuggestion{TransitionType: TransitionMatchAction})
	if err != nil {
		t.Fatalf("classify camera transition: %v", err)
	}
	if transition.TransitionType != TransitionCameraCut || transition.TailPolicy == "hard" {
		t.Fatalf("camera transition = %+v", transition)
	}
}

func TestShotContractReviewRejectsMissingRequiredAssetAndIdentityMorph(t *testing.T) {
	entry := validTestShotState()
	exit := validTestShotState()
	exit.Characters = nil
	transition, err := ClassifyTransition(nil, entry, TransitionSuggestion{})
	if err != nil {
		t.Fatalf("classify first shot: %v", err)
	}
	review := ReviewShotContract(entry, exit, transition, []string{testCharacterID, "66666666-6666-4666-8666-666666666666"})
	if review.Approved || len(review.Issues) < 2 {
		t.Fatalf("review = %+v", review)
	}
}

func TestAlignShotStateVisibilityKeepsContinuityOnlyAssetsOffscreen(t *testing.T) {
	state := validTestShotState()
	extraCharacterID := "44444444-4444-4444-8444-444444444444"
	state.Characters = append(state.Characters, CharacterState{
		AssetID:  extraCharacterID,
		Blocking: BlockingState{Horizontal: "right", Depth: "background", Facing: "camera"},
	})
	aligned := AlignShotStateVisibility(state, []string{testSceneID, testCharacterID})
	characters := characterStateByID(aligned.Characters)
	if characters[extraCharacterID].Blocking.Horizontal != "offscreen" {
		t.Fatalf("continuity-only character remained visible: %+v", characters[extraCharacterID])
	}
	if aligned.Props[0].State != "hidden" {
		t.Fatalf("continuity-only prop remained visible: %+v", aligned.Props[0])
	}
	references := RequiredReferenceAssetIDs(aligned)
	if !sameStringSet(references, []string{testSceneID, testCharacterID}) {
		t.Fatalf("visible references = %v", references)
	}
	transition, err := ClassifyTransition(nil, aligned, TransitionSuggestion{})
	if err != nil {
		t.Fatal(err)
	}
	review := ReviewShotContract(aligned, aligned, transition, []string{testSceneID, testCharacterID, extraCharacterID})
	if review.Approved || !contractReviewHasIssue(review, "MISSING_REQUIRED_ASSET") {
		t.Fatalf("offscreen required character was accepted: %+v", review)
	}
}

func TestAlignShotStateVisibilityMakesRequiredAssetsVisible(t *testing.T) {
	state := validTestShotState()
	state.Characters[0].Blocking.Horizontal = "offscreen"
	state.Characters[0].Blocking.Facing = "away"
	state.Props[0].State = "hidden"
	state.Props[0].HolderAssetID = ""

	aligned := AlignShotStateVisibility(state, []string{testSceneID, testCharacterID, testPropID})
	if aligned.Characters[0].Blocking.Horizontal == "offscreen" {
		t.Fatal("required character remained offscreen")
	}
	if aligned.Props[0].State == "hidden" {
		t.Fatal("required prop remained hidden")
	}
	transition, err := ClassifyTransition(nil, aligned, TransitionSuggestion{})
	if err != nil {
		t.Fatal(err)
	}
	review := ReviewShotContract(aligned, aligned, transition, []string{testSceneID, testCharacterID, testPropID})
	if !review.Approved {
		t.Fatalf("review = %+v, want required visible assets approved", review)
	}
}

func TestNormalizeShotStateMakesOffscreenFacingDeterministic(t *testing.T) {
	state := validTestShotState()
	state.Characters[0].Blocking.Horizontal = "offscreen"
	state.Characters[0].Blocking.Facing = "unknown"
	state = NormalizeShotState(state)
	if state.Characters[0].Blocking.Facing != "away" {
		t.Fatalf("offscreen facing = %q", state.Characters[0].Blocking.Facing)
	}
	if err := ValidateShotState(state); err != nil {
		t.Fatalf("normalized offscreen state rejected: %v", err)
	}
}

func TestFirstLastFrameContractValidatesIdentityReachabilityAndDuration(t *testing.T) {
	entry := validTestShotState()
	entry.Characters[0].AppearanceVersionID = "77777777-7777-4777-8777-777777777777"
	entry.Characters[0].CostumeVariantID = "88888888-8888-4888-8888-888888888888"
	exit := entry
	exit.Characters = append([]CharacterState(nil), entry.Characters...)
	exit.Props = append([]PropState(nil), entry.Props...)
	exit.Characters[0].Blocking.Horizontal = "right"
	exit.Characters[0].Pose = "guard stance"
	exit.Action.Exit = "角色移动到画面右侧并停下"
	transition, err := ClassifyTransition(nil, entry, TransitionSuggestion{})
	if err != nil {
		t.Fatalf("classify transition: %v", err)
	}
	review := ReviewFirstLastFrameContract(entry, exit, transition, []string{testSceneID, testCharacterID, testPropID}, 4*90_000, 90_000)
	if !review.Approved {
		t.Fatalf("reachable first/last contract rejected: %+v", review)
	}

	tooShort := ReviewFirstLastFrameContract(entry, exit, transition, []string{testCharacterID}, 90_000, 90_000)
	if tooShort.Approved || !contractReviewHasIssue(tooShort, "FIRST_LAST_DURATION_TOO_SHORT") {
		t.Fatalf("short transition review = %+v", tooShort)
	}

	drifted := exit
	drifted.Characters = append([]CharacterState(nil), exit.Characters...)
	drifted.Characters[0].CostumeVariantID = "99999999-9999-4999-8999-999999999999"
	driftReview := ReviewFirstLastFrameContract(entry, drifted, transition, []string{testCharacterID}, 4*90_000, 90_000)
	if driftReview.Approved || !contractReviewHasIssue(driftReview, "FIRST_LAST_CHARACTER_IDENTITY_DRIFT") {
		t.Fatalf("identity drift review = %+v", driftReview)
	}
}

func contractReviewHasIssue(review ShotContractReview, code string) bool {
	for _, issue := range review.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestReferenceResolverFiltersHistoryAndEnforcesSingleFrame(t *testing.T) {
	hash := strings.Repeat("a", 64)
	pack, err := ResolveReferencePack(ReferenceResolveInput{
		ProfileKey: ProfileSingleFrameI2V, Purpose: ReferencePurposeVideo,
		ShotStateRevision: 1, ProfileSnapshotHash: hash, ShotStateHash: strings.Repeat("b", 64), CapabilitySnapshotHash: strings.Repeat("c", 64), MaxReferences: 1,
		Candidates: []ReferenceCandidate{
			{ReferenceKey: "old", Role: ReferenceRoleFirstFrame, Required: true, Priority: 2000, ContentHash: hash, Active: false, Fresh: false},
			{ReferenceKey: "current", Role: ReferenceRoleFirstFrame, Required: true, Priority: 1000, SourceType: "visual_anchor", ArtifactID: testSceneID, ContentHash: hash, Active: true, Fresh: true},
			{ReferenceKey: "character", Role: ReferenceRoleCharacterIdentity, Required: true, Priority: 900, AssetID: testCharacterID, ContentHash: hash, Active: true, Fresh: true},
		},
	})
	if err != nil {
		t.Fatalf("resolve first-frame pack: %v", err)
	}
	if len(pack.Manifest.Items) != 1 || pack.Manifest.Items[0].ReferenceKey != "current" || len(pack.ManifestHash) != 64 {
		t.Fatalf("pack = %+v", pack)
	}
	_, err = ResolveReferencePack(ReferenceResolveInput{
		ProfileKey: ProfileSingleFrameI2V, Purpose: ReferencePurposeVideo,
		ShotStateRevision: 1, ProfileSnapshotHash: hash, ShotStateHash: strings.Repeat("b", 64), CapabilitySnapshotHash: strings.Repeat("c", 64), MaxReferences: 1,
		Candidates: []ReferenceCandidate{{ReferenceKey: "character", Role: ReferenceRoleCharacterIdentity, Required: true, ContentHash: hash, Active: true, Fresh: true}},
	})
	var typed Error
	if !errors.As(err, &typed) || typed.Code != CodeReferencePackIncomplete {
		t.Fatalf("error = %v, want %s", err, CodeReferencePackIncomplete)
	}
}

func TestPromptContextPlanPreservesDialogueAndIsDeterministic(t *testing.T) {
	input := PromptContextCompileInput{
		EpisodeScript:          strings.Repeat("整集剧情事实与人物状态。", 300),
		CurrentSceneScript:     strings.Repeat("当前场景动作。", 100),
		AdjacentSceneSummaries: []AdjacentSceneSummary{{Ordinal: 1, Relation: "previous", Summary: strings.Repeat("前场摘要。", 50)}},
		CurrentShotState:       validTestShotState(),
		VerbatimDialogueCues:   []DialogueCue{{Speaker: "方源", Text: "这句话必须逐字保留。", StartTick: 0, EndTick: 24000}},
		ModelContextLimit:      4096,
		ModelPromptLimit:       3500,
	}
	first, err := CompilePromptContextPlan(input)
	if err != nil {
		t.Fatalf("compile context plan: %v", err)
	}
	second, err := CompilePromptContextPlan(input)
	if err != nil {
		t.Fatalf("compile second context plan: %v", err)
	}
	if first.PlanHash != second.PlanHash || first.VerbatimDialogueCues[0].Text != input.VerbatimDialogueCues[0].Text || first.BudgetAllocation.Limit != 3500 {
		t.Fatalf("context plan is not stable: %+v", first)
	}
	input.ModelPromptLimit = 200
	_, err = CompilePromptContextPlan(input)
	var typed Error
	if !errors.As(err, &typed) || typed.Code != CodePromptContextLimitExceeded {
		t.Fatalf("error = %v, want %s", err, CodePromptContextLimitExceeded)
	}
}

func TestSingleFramePromptReviewsEnforceImageIsolationAndVerbatimAudio(t *testing.T) {
	cues := []DialogueCue{{Speaker: "方源", Text: "杀上青茅山。"}}
	imageReview := ReviewSingleFrameImagePrompt("方源说：杀上青茅山。", cues)
	if imageReview.Approved {
		t.Fatalf("image review should reject dialogue leakage: %+v", imageReview)
	}
	videoReview := ReviewSingleFrameVideoPrompt("方源说：杀上青茅山。", cues, true, false)
	if videoReview.Approved {
		t.Fatalf("video review should reject missing native audio capability: %+v", videoReview)
	}
	videoReview = ReviewSingleFrameVideoPrompt("方源说：杀上青茅山。", cues, true, true)
	if !videoReview.Approved {
		t.Fatalf("video review should approve complete prompt: %+v", videoReview)
	}
}

func TestCompileSingleFramePromptContractCapturesAllProvenance(t *testing.T) {
	state := validTestShotState()
	stateHash, _ := HashShotState(state)
	contextPlan, err := CompilePromptContextPlan(PromptContextCompileInput{
		EpisodeScript: "整集剧本", CurrentSceneScript: "当前场景", CurrentShotState: state,
		ModelContextLimit: 4000, ModelPromptLimit: 3000,
	})
	if err != nil {
		t.Fatalf("compile context: %v", err)
	}
	hash := strings.Repeat("a", 64)
	pack := ReferencePack{
		ProfileSnapshotHash: hash, ShotStateHash: stateHash, CapabilitySnapshotHash: strings.Repeat("b", 64),
		Manifest: ReferencePackManifest{ProfileKey: ProfileSingleFrameI2V, Purpose: ReferencePurposeAnchor, ShotStateRevision: 1, Items: []ReferencePackItem{}},
	}
	pack.ManifestHash, _ = canonicalHash(pack.Manifest)
	compiled, err := CompilePromptContract(PromptContractInput{
		ProfileKey: ProfileSingleFrameI2V, ProfileVersionID: testSceneID, ProfileSnapshotHash: hash,
		Role: PromptRoleAnchorGenerate, InputContractVersion: "first_frame/v1",
		ContextPlan: contextPlan, ShotState: state, ReferencePack: pack, CapabilitySnapshotHash: strings.Repeat("b", 64),
		Layers: []PromptContractLayer{{LayerKey: "public_rules", ContentHash: strings.Repeat("c", 64), Source: "system"}},
	})
	if err != nil {
		t.Fatalf("compile prompt contract: %v", err)
	}
	if compiled.TemplateKey != "video_profile.single_frame_i2v.anchor.generate" || len(compiled.Provenance.ContractHash) != 64 || compiled.Provenance.PromptContextPlanHash != contextPlan.PlanHash {
		t.Fatalf("compiled contract = %+v", compiled)
	}
}
