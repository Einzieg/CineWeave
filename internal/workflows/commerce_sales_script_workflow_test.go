package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/commerce"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceScriptOrganizationWorkflowReusesReadyContractWithoutAgentCall(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input, snapshot, contract, contractHash := testCommerceSalesScriptWorkflowFixture(t)
	agentCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, CommerceSalesScriptContractClaimInput) (CommerceSalesScriptContractClaimResult, error) {
		return CommerceSalesScriptContractClaimResult{
			Snapshot: snapshot,
			State: CommerceSalesScriptContractState{
				ContractID: "00000000-0000-4000-8000-000000000020", Status: "ready", AttemptGeneration: 1,
				OwnerWorkflowRunID: "00000000-0000-4000-8000-000000000021", InputHash: snapshot.InputHash,
				Contract: contract, ContractHash: contractHash,
			},
		}, nil
	}, activity.RegisterOptions{Name: ClaimCommerceSalesScriptContractActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
		agentCalls++
		return CommerceAgentCallOutput{}, nil
	}, activity.RegisterOptions{Name: OrganizeCommerceScriptActivityName})

	env.ExecuteWorkflow(CommerceScriptOrganizationWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceScriptOrganizationOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "ready", output.Status)
	require.Equal(t, contractHash, output.ContractHash)
	require.Zero(t, agentCalls, "a ready immutable contract must prevent a duplicate paid organizer call")
}

func TestCommerceScriptOrganizationWorkflowStopsAfterThreeInvalidAgentRounds(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	agentCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, CommerceSalesScriptContractClaimInput) (CommerceSalesScriptContractClaimResult, error) {
		return CommerceSalesScriptContractClaimResult{
			Snapshot: snapshot,
			State: CommerceSalesScriptContractState{
				ContractID: "00000000-0000-4000-8000-000000000020", Status: "running", AttemptGeneration: 1,
				OwnerWorkflowRunID: input.WorkflowRunID, Owner: true, InputHash: snapshot.InputHash,
			},
		}, nil
	}, activity.RegisterOptions{Name: ClaimCommerceSalesScriptContractActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
		agentCalls++
		return CommerceAgentCallOutput{RawOutput: `{"unexpected":true}`}, nil
	}, activity.RegisterOptions{Name: OrganizeCommerceScriptActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceGenerationWorkflowFailureInput) error {
		return nil
	}, activity.RegisterOptions{Name: FailCommerceGenerationWorkflowActivityName})

	env.ExecuteWorkflow(CommerceScriptOrganizationWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, CommerceMaxAgentReviewRounds, agentCalls)
}

func TestCommerceScriptOrganizationWorkflowFillsMissingVisualIntentWithoutRetry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	contract.Segments[0].VisualIntent = ""
	agentCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, CommerceSalesScriptContractClaimInput) (CommerceSalesScriptContractClaimResult, error) {
		return CommerceSalesScriptContractClaimResult{
			Snapshot: snapshot,
			State: CommerceSalesScriptContractState{
				ContractID: "00000000-0000-4000-8000-000000000020", Status: "running", AttemptGeneration: 1,
				OwnerWorkflowRunID: input.WorkflowRunID, Owner: true, InputHash: snapshot.InputHash,
			},
		}, nil
	}, activity.RegisterOptions{Name: ClaimCommerceSalesScriptContractActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
		agentCalls++
		raw, err := json.Marshal(contract)
		require.NoError(t, err)
		return CommerceAgentCallOutput{
			RawOutput: string(raw),
			Provenance: CommerceAgentProvenance{
				Role: "script_organizer", Round: 1,
				NodeRunID:         "00000000-0000-4000-8000-000000000022",
				ProviderRequestID: "00000000-0000-4000-8000-000000000023",
				ProviderCallID:    "00000000-0000-4000-8000-000000000024",
			},
		}, nil
	}, activity.RegisterOptions{Name: OrganizeCommerceScriptActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, commit CommerceSalesScriptContractCommitInput) (CommerceSalesScriptContractState, error) {
		require.Equal(t, fallbackCommerceVisualIntent(snapshot), commit.Contract.Segments[0].VisualIntent)
		hash, err := commerceContractHash(commit.Contract)
		require.NoError(t, err)
		return CommerceSalesScriptContractState{
			ContractID: "00000000-0000-4000-8000-000000000020", Status: "ready",
			AttemptGeneration: 1, InputHash: snapshot.InputHash, Contract: commit.Contract, ContractHash: hash,
		}, nil
	}, activity.RegisterOptions{Name: CommitCommerceSalesScriptContractActivityName})

	env.ExecuteWorkflow(CommerceScriptOrganizationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, agentCalls)
	var output CommerceScriptOrganizationOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, fallbackCommerceVisualIntent(snapshot), output.Contract.Segments[0].VisualIntent)
}

func TestCommerceScriptOrganizationWorkflowRestoresApprovedTextWithoutRetry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	contract.Segments[0].VoiceoverText = "模型改写后的口播"
	contract.Segments[0].OnscreenText = "模型改写后的字幕"
	agentCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, CommerceSalesScriptContractClaimInput) (CommerceSalesScriptContractClaimResult, error) {
		return CommerceSalesScriptContractClaimResult{
			Snapshot: snapshot,
			State: CommerceSalesScriptContractState{
				ContractID: "00000000-0000-4000-8000-000000000020", Status: "running", AttemptGeneration: 1,
				OwnerWorkflowRunID: input.WorkflowRunID, Owner: true, InputHash: snapshot.InputHash,
			},
		}, nil
	}, activity.RegisterOptions{Name: ClaimCommerceSalesScriptContractActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceAgentCallInput) (CommerceAgentCallOutput, error) {
		agentCalls++
		raw, err := json.Marshal(contract)
		require.NoError(t, err)
		return CommerceAgentCallOutput{
			RawOutput: string(raw),
			Provenance: CommerceAgentProvenance{
				Role: "script_organizer", Round: 1,
				NodeRunID:         "00000000-0000-4000-8000-000000000022",
				ProviderRequestID: "00000000-0000-4000-8000-000000000023",
				ProviderCallID:    "00000000-0000-4000-8000-000000000024",
			},
		}, nil
	}, activity.RegisterOptions{Name: OrganizeCommerceScriptActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, commit CommerceSalesScriptContractCommitInput) (CommerceSalesScriptContractState, error) {
		require.Equal(t, snapshot.LocalizedSegments[0].VoiceoverText, commit.Contract.Segments[0].VoiceoverText)
		require.Equal(t, snapshot.LocalizedSegments[0].OnscreenText, commit.Contract.Segments[0].OnscreenText)
		hash, err := commerceContractHash(commit.Contract)
		require.NoError(t, err)
		return CommerceSalesScriptContractState{
			ContractID: "00000000-0000-4000-8000-000000000020", Status: "ready",
			AttemptGeneration: 1, InputHash: snapshot.InputHash, Contract: commit.Contract, ContractHash: hash,
		}, nil
	}, activity.RegisterOptions{Name: CommitCommerceSalesScriptContractActivityName})

	env.ExecuteWorkflow(CommerceScriptOrganizationWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, agentCalls)
}

func TestCommerceSalesScriptValidationFeedbackIdentifiesInvalidField(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	contract.Segments[0].SalesBeat = "script"

	err := ValidateCommerceSalesScript(contract, snapshot)
	issues := commerceValidationFeedback(CommerceCodeStoryboardContractInvalid, "salesScript", err)

	require.Len(t, issues, 1)
	require.Equal(t, "segments[0].salesBeat", issues[0].Field)
	require.Equal(t, snapshot.LocalizedSegments[0].SourceSegmentID, issues[0].SourceSegmentID)
	require.Contains(t, issues[0].Suggestion, "hook")
}

func TestCommerceStoryboardAgentSnapshotUsesSalesScriptAsSalesBeatAuthority(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	snapshot.LocalizedSegments[0].SalesBeat = "script"
	snapshot.LocalizationContract = json.RawMessage(`{
		"contractVersion":"commerce-script-localization/v1",
		"segments":[{"sourceSegmentId":"00000000-0000-4000-8000-00000000000e","salesBeat":"script"}]
	}`)

	agentSnapshot, err := buildCommerceStoryboardAgentSnapshot(snapshot, contract)

	require.NoError(t, err)
	require.Equal(t, "salesScript.segments", agentSnapshot.SalesBeatAuthority)
	require.Equal(t, CommerceDurationExecutionPolicy, agentSnapshot.DurationPolicy)
	require.Equal(t, CommerceVoiceoverTimingPolicy, agentSnapshot.VoiceoverTimingPolicy)
	require.Equal(t, snapshot.AllowedShotDurations, agentSnapshot.ProviderRequestDurationSuggestions)
	require.Equal(t, "hook", agentSnapshot.Segments[0].SalesBeat)
	serialized, err := json.Marshal(agentSnapshot)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "localizationContract")
	require.NotContains(t, string(serialized), `"allowedShotDurations"`)
}

func TestCommerceStoryboardPlannerInputUsesStableSourceAliases(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	agentSnapshot, err := buildCommerceStoryboardAgentSnapshot(snapshot, contract)
	require.NoError(t, err)
	actualID := snapshot.LocalizedSegments[0].SourceSegmentID

	plannerSnapshot, plannerSalesScript, aliases, err := aliasCommerceStoryboardPlannerInput(agentSnapshot, contract)

	require.NoError(t, err)
	require.Equal(t, "00000000-0000-4000-8000-000000000001", plannerSnapshot.Segments[0].SourceSegmentID)
	require.Equal(t, plannerSnapshot.Segments[0].SourceSegmentID, plannerSalesScript.Segments[0].SourceSegmentID)
	require.Equal(t, actualID, aliases.aliasToActual[plannerSnapshot.Segments[0].SourceSegmentID])
	serialized, err := json.Marshal(map[string]any{
		"snapshot":    plannerSnapshot,
		"salesScript": plannerSalesScript,
	})
	require.NoError(t, err)
	require.NotContains(t, string(serialized), actualID)
}

func TestCommerceStoryboardSourceAliasesResolveBeforeValidation(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	agentSnapshot, err := buildCommerceStoryboardAgentSnapshot(snapshot, contract)
	require.NoError(t, err)
	plannerSnapshot, _, aliases, err := aliasCommerceStoryboardPlannerInput(agentSnapshot, contract)
	require.NoError(t, err)
	actualID := snapshot.LocalizedSegments[0].SourceSegmentID
	alias := plannerSnapshot.Segments[0].SourceSegmentID

	aliased, err := resolveCommerceStoryboardSourceSegmentAliases(CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "aliased-shot", SourceSegmentIDs: []string{alias},
		}},
	}, aliases)
	require.NoError(t, err)
	require.Equal(t, []string{actualID}, aliased.Shots[0].SourceSegmentIDs)

	actual, err := resolveCommerceStoryboardSourceSegmentAliases(CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "actual-shot", SourceSegmentIDs: []string{actualID},
		}},
	}, aliases)
	require.NoError(t, err)
	require.Equal(t, []string{actualID}, actual.Shots[0].SourceSegmentIDs)

	_, err = resolveCommerceStoryboardSourceSegmentAliases(CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey:     "unknown-shot",
			SourceSegmentIDs: []string{"00000000-0000-4000-8000-000000000099"},
		}},
	}, aliases)
	require.ErrorContains(t, err, "unknown source segment alias")
}

func TestCommerceStoryboardCreativeSkeletonAliasesDoNotMutateFrozenPlan(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	agentSnapshot, err := buildCommerceStoryboardAgentSnapshot(snapshot, contract)
	require.NoError(t, err)
	plannerSnapshot, _, aliases, err := aliasCommerceStoryboardPlannerInput(agentSnapshot, contract)
	require.NoError(t, err)
	actualID := snapshot.LocalizedSegments[0].SourceSegmentID
	skeleton := CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "shot-001", ShotOrdinal: 1, SourceSegmentIDs: []string{actualID},
		}},
	}

	aliased, err := aliasCommerceStoryboardCreativeSkeleton(skeleton, aliases)

	require.NoError(t, err)
	require.Equal(t, actualID, skeleton.Shots[0].SourceSegmentIDs[0])
	require.Equal(t, plannerSnapshot.Segments[0].SourceSegmentID, aliased.Shots[0].SourceSegmentIDs[0])
	aliased.Shots[0].SourceSegmentIDs[0] = "mutated"
	require.Equal(t, actualID, skeleton.Shots[0].SourceSegmentIDs[0])
}

func TestCommerceStoryboardCreativeDirectionCannotChangeDeterministicPlan(t *testing.T) {
	skeleton := CommerceStoryboardPlanContract{
		ContractVersion:           CommerceStoryboardPlanContractVersion,
		CommerceScriptUnitID:      "script-unit",
		ScriptUnitGenerationID:    "unit-generation",
		CommerceWorkflowBindingID: "workflow-binding",
		ProductVersionID:          "product-version",
		TargetLocale:              "ms-MY",
		TargetDurationSeconds:     15,
		Shots: []CommerceStoryboardShotContract{
			{
				CandidateKey: "shot-001", ShotOrdinal: 1,
				SourceSegmentIDs: []string{"source-001"}, DurationSeconds: 6,
				SalesBeat: "hook", VoiceoverText: "Ayat pertama",
				OnscreenText: "Teks pertama", SoundEffects: []string{"whoosh"},
				MusicCue: "energetic", ProductReferenceIDs: []string{"reference-001"},
				RequiredProductFeatures: []string{"matte finish"},
			},
			{
				CandidateKey: "shot-002", ShotOrdinal: 2,
				SourceSegmentIDs: []string{"source-002"}, DurationSeconds: 9,
				SalesBeat: "cta", VoiceoverText: "Ayat kedua",
				OnscreenText: "Teks kedua", ProductReferenceIDs: []string{"reference-001"},
			},
		},
	}
	candidate := skeleton
	candidate.Shots = append([]CommerceStoryboardShotContract(nil), skeleton.Shots...)
	candidate.Shots[0].SourceSegmentIDs = []string{"tampered-source"}
	candidate.Shots[0].DurationSeconds = 15
	candidate.Shots[0].SalesBeat = "tampered"
	candidate.Shots[0].VoiceoverText = "tampered"
	candidate.Shots[0].OnscreenText = "tampered"
	candidate.Shots[0].SoundEffects = []string{"tampered"}
	candidate.Shots[0].MusicCue = "tampered"
	candidate.Shots[0].RequiredProductFeatures = []string{"tampered"}
	candidate.Shots[0].ShotPurpose = "展示卖点"
	candidate.Shots[0].VisualAction = "自然拿起商品"
	candidate.Shots[0].Camera = json.RawMessage(`{"shotSize":"close_up"}`)
	candidate.Shots[0].Composition = "商品居中"
	candidate.Shots[1].ShotPurpose = "完成行动号召"
	candidate.Shots[1].VisualAction = "商品保持清晰并结束演示"
	candidate.Shots[1].Camera = json.RawMessage(`{"shotSize":"medium"}`)
	candidate.Shots[1].Composition = "人物与商品同框"

	result, err := applyCommerceStoryboardCreativeDirection(skeleton, candidate)

	require.NoError(t, err)
	require.Equal(t, skeleton.Shots[0].SourceSegmentIDs, result.Shots[0].SourceSegmentIDs)
	require.Equal(t, skeleton.Shots[0].DurationSeconds, result.Shots[0].DurationSeconds)
	require.Equal(t, skeleton.Shots[0].SalesBeat, result.Shots[0].SalesBeat)
	require.Equal(t, skeleton.Shots[0].VoiceoverText, result.Shots[0].VoiceoverText)
	require.Equal(t, skeleton.Shots[0].OnscreenText, result.Shots[0].OnscreenText)
	require.Equal(t, skeleton.Shots[0].SoundEffects, result.Shots[0].SoundEffects)
	require.Equal(t, skeleton.Shots[0].MusicCue, result.Shots[0].MusicCue)
	require.Equal(t, skeleton.Shots[0].RequiredProductFeatures, result.Shots[0].RequiredProductFeatures)
	require.Equal(t, candidate.Shots[0].ShotPurpose, result.Shots[0].ShotPurpose)
	require.Equal(t, candidate.Shots[0].VisualAction, result.Shots[0].VisualAction)
	require.JSONEq(t, string(candidate.Shots[0].Camera), string(result.Shots[0].Camera))
	require.Equal(t, candidate.Shots[0].Composition, result.Shots[0].Composition)
}

func TestCommerceStoryboardCreativeDirectionRejectsChangedShotGrouping(t *testing.T) {
	skeleton := CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{
			{CandidateKey: "shot-001", ShotOrdinal: 1},
			{CandidateKey: "shot-002", ShotOrdinal: 2},
		},
	}
	candidate := CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{
			{CandidateKey: "shot-001", ShotOrdinal: 1},
		},
	}

	_, err := applyCommerceStoryboardCreativeDirection(skeleton, candidate)

	require.ErrorContains(t, err, "changed deterministic shot count")
}

func TestCommerceStoryboardReviewRoundsAreCappedAtThree(t *testing.T) {
	planner := CommerceAgentBinding{MaxReviewRounds: 99}
	reviewer := CommerceAgentBinding{MaxReviewRounds: 99}

	require.Equal(t, CommerceMaxAgentReviewRounds, commerceReviewRounds(planner, reviewer))
}

func TestCommerceStoryboardRequiredContextCoverageIsBackendOwned(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	contextBeforeID := "00000000-0000-4000-8000-000000000090"
	contextAfterID := "00000000-0000-4000-8000-000000000091"
	missingVoiceoverID := "00000000-0000-4000-8000-000000000092"
	snapshot.LocalizedSegments = append(
		[]CommerceLocalizedSegmentSnapshot{{
			ID:              "00000000-0000-4000-8000-000000000093",
			SourceSegmentID: contextBeforeID, Ordinal: 1, LocalizedText: "VOICEOVER:", Required: true,
		}},
		snapshot.LocalizedSegments...,
	)
	snapshot.LocalizedSegments[1].Ordinal = 2
	snapshot.LocalizedSegments = append(snapshot.LocalizedSegments,
		CommerceLocalizedSegmentSnapshot{
			ID:              "00000000-0000-4000-8000-000000000094",
			SourceSegmentID: contextAfterID, Ordinal: 3, LocalizedText: "SCENE 1", Required: true,
		},
		CommerceLocalizedSegmentSnapshot{
			ID:              "00000000-0000-4000-8000-000000000095",
			SourceSegmentID: missingVoiceoverID, Ordinal: 4, LocalizedText: "Spoken line",
			VoiceoverText: "Spoken line", Required: true,
		},
	)
	plan := CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "shot-001",
			SourceSegmentIDs: []string{
				snapshot.LocalizedSegments[1].SourceSegmentID,
			},
		}},
	}

	reconciled, err := reconcileCommerceStoryboardRequiredContextCoverage(snapshot, plan)

	require.NoError(t, err)
	require.Equal(t, []string{
		contextBeforeID,
		snapshot.LocalizedSegments[1].SourceSegmentID,
		contextAfterID,
	}, reconciled.Shots[0].SourceSegmentIDs)
	require.NotContains(t, reconciled.Shots[0].SourceSegmentIDs, missingVoiceoverID,
		"missing spoken content must remain a hard validation failure")
}

func TestCommerceStoryboardPairsPrefaceVoiceoverWithExplicitScenes(t *testing.T) {
	makeID := func(value int) string {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", value)
	}
	localized := []CommerceLocalizedSegmentSnapshot{{
		ID: makeID(100), SourceSegmentID: makeID(200), Ordinal: 1,
		LocalizedText: "Create a realistic product showcase.", Required: true,
	}}
	for index := 0; index < 5; index++ {
		localized = append(localized, CommerceLocalizedSegmentSnapshot{
			ID: makeID(101 + index), SourceSegmentID: makeID(201 + index), Ordinal: len(localized) + 1,
			LocalizedText: fmt.Sprintf("Baris suara %d", index+1),
			VoiceoverText: fmt.Sprintf("Baris suara %d", index+1), Required: true,
		})
	}
	for index := 0; index < 5; index++ {
		localized = append(localized,
			CommerceLocalizedSegmentSnapshot{
				ID: makeID(110 + index*2), SourceSegmentID: makeID(210 + index*2), Ordinal: len(localized) + 1,
				LocalizedText: fmt.Sprintf("SCENE %d (%d-%ds)", index+1, index*3, (index+1)*3), Required: true,
			},
			CommerceLocalizedSegmentSnapshot{
				ID: makeID(111 + index*2), SourceSegmentID: makeID(211 + index*2), Ordinal: len(localized) + 2,
				LocalizedText: fmt.Sprintf("Visual action %d", index+1), Required: true,
			},
		)
	}
	salesScript := CommerceSalesScriptContract{
		Segments: make([]CommerceSalesScriptSegmentContract, 0, len(localized)),
	}
	for _, segment := range localized {
		salesScript.Segments = append(salesScript.Segments, CommerceSalesScriptSegmentContract{
			SourceSegmentID: segment.SourceSegmentID,
			SalesBeat:       "demonstration",
			VoiceoverText:   segment.VoiceoverText,
			VisualIntent:    segment.LocalizedText,
		})
	}
	snapshot := CommerceStoryboardPlanningSnapshot{
		TargetLocale: "ms-MY", TargetDurationSeconds: 15, TimelineTimebase: 1000,
		TimingPolicy: CommerceTimingPolicy{
			Version: "ms-my-voiceover/v1", Unit: "word", NormalUnitsPerSecond: 2.5,
		},
		LocalizedSegments: localized,
		ProductReferences: []CommerceProductReferenceSnapshot{{
			ReferenceID: makeID(400), Required: true,
		}},
	}

	beats, err := buildCommerceStoryboardBeatInputs(snapshot, salesScript)
	require.NoError(t, err)
	require.Len(t, beats, 10)
	for index := 0; index < 5; index++ {
		scene := beats[index*2]
		voiceover := beats[index*2+1]
		require.True(t, isCommerceStoryboardSceneHeader(scene))
		require.Equal(t, fmt.Sprintf("Baris suara %d", index+1), voiceover.VoiceoverText)
		if index > 0 {
			require.True(t, scene.ForceBoundaryBefore)
		}
		if index < 4 {
			require.True(t, voiceover.ForceBoundaryAfter)
		}
	}

	envelope, envelopeHash, err := commerce.CanonicalizeVideoExecutionEnvelope(commerce.VideoExecutionEnvelope{
		ContractVersion:                    commerce.CommerceVideoEnvelopeV1,
		ProjectProductionGenerationID:      makeID(500),
		VideoProductionBindingID:           makeID(501),
		VideoProductionBindingRevision:     1,
		VideoProductionProfileVersionID:    makeID(502),
		VideoProductionProfileSnapshotHash: strings.Repeat("a", 64),
		ModelProfileKey:                    "commerce_video",
		TargetResolution:                   "720p",
		AspectRatio:                        "9:16",
		Routes: []commerce.VideoExecutionRoute{{
			RouteKey: "route-1", ModelProfileID: makeID(503), ModelProfileKey: "commerce_video",
			ModelProfileBindingID: makeID(504), ProviderModelID: makeID(505), ProviderAccountID: makeID(506),
			ModelKey: "video-model", VariantKey: "default", CapabilitySnapshotHash: strings.Repeat("b", 64),
			ExecutableDurationSeconds: []int{6, 10, 12, 16}, Resolutions: []string{"720p"},
		}},
		ExecutableDurationSeconds: []int{6, 10, 12, 16},
	})
	require.NoError(t, err)
	segmentation, err := commerce.PlanStoryboardSegmentation(commerce.StoryboardSegmentationInput{
		Strategy: commerce.StoryboardStrategySmart, SegmentationPolicyVersion: commerce.CommerceSegmentationPolicyV2,
		TargetDurationSeconds: 15, TimelineTimebase: 1000,
		VideoExecutionEnvelope: envelope, VideoExecutionEnvelopeHash: envelopeHash, Beats: beats,
	})
	require.NoError(t, err)
	require.Len(t, segmentation.Shots, 5)

	plan, err := buildCommerceStoryboardCreativeSkeleton(snapshot, salesScript, beats, segmentation)
	require.NoError(t, err)
	require.Len(t, plan.Shots, 5)
	covered := map[string]bool{}
	for index, shot := range plan.Shots {
		require.Equal(t, fmt.Sprintf("Baris suara %d", index+1), shot.VoiceoverText)
		for _, sourceSegmentID := range shot.SourceSegmentIDs {
			covered[sourceSegmentID] = true
		}
	}
	require.Len(t, covered, len(localized))
	require.Contains(t, plan.Shots[0].SourceSegmentIDs, localized[0].SourceSegmentID)
	require.Contains(t, plan.Shots[0].SourceSegmentIDs, localized[7].SourceSegmentID)
	require.NotContains(t, plan.Shots[1].SourceSegmentIDs, localized[7].SourceSegmentID)
}

func TestCommerceStoryboardPlanUsesEditorialDurationIndependentOfProviderRequestOptions(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	snapshot.AllowedShotDurations = []int{6, 10, 12, 16}
	sourceSegmentID := snapshot.LocalizedSegments[0].SourceSegmentID
	productReferenceID := snapshot.ProductReferences[0].ReferenceID
	plan := CommerceStoryboardPlanContract{
		ContractVersion:           CommerceStoryboardPlanContractVersion,
		CommerceScriptUnitID:      snapshot.Identity.ScriptUnitID,
		ScriptUnitGenerationID:    snapshot.Identity.UnitGenerationID,
		CommerceWorkflowBindingID: snapshot.Identity.CommerceWorkflowBindingID,
		ProductVersionID:          snapshot.ProductVersionID,
		TargetLocale:              snapshot.TargetLocale,
		TargetDurationSeconds:     snapshot.TargetDurationSeconds,
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "editorial-shot-01", ShotOrdinal: 1,
			SourceSegmentIDs: []string{sourceSegmentID}, DurationSeconds: snapshot.TargetDurationSeconds,
			SalesBeat: "hook", ShotPurpose: "完整展示商品卖点", VisualAction: "商品主体进入画面并完成展示",
			Camera: json.RawMessage(`{"shotSize":"medium"}`), Composition: "商品居中",
			VoiceoverText:       snapshot.LocalizedSegments[0].VoiceoverText,
			OnscreenText:        snapshot.LocalizedSegments[0].OnscreenText,
			ProductReferenceIDs: []string{productReferenceID},
		}},
	}

	require.NoError(t, validateCommerceStoryboardPlanShape(snapshot, plan))
}

func TestCommerceStoryboardSalesBeatValidationAndReviewReconciliation(t *testing.T) {
	_, snapshot, contract, _ := testCommerceSalesScriptWorkflowFixture(t)
	plan := CommerceStoryboardPlanContract{
		ContractVersion:           CommerceStoryboardPlanContractVersion,
		CommerceScriptUnitID:      snapshot.Identity.ScriptUnitID,
		ScriptUnitGenerationID:    snapshot.Identity.UnitGenerationID,
		CommerceWorkflowBindingID: snapshot.Identity.CommerceWorkflowBindingID,
		ProductVersionID:          snapshot.ProductVersionID,
		TargetLocale:              snapshot.TargetLocale,
		TargetDurationSeconds:     snapshot.TargetDurationSeconds,
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey: "hook-product-01", ShotOrdinal: 1,
			SourceSegmentIDs: []string{snapshot.LocalizedSegments[0].SourceSegmentID},
			DurationSeconds:  snapshot.TargetDurationSeconds, SalesBeat: "hook",
		}},
	}
	plan, err := reconcileCommerceStoryboardSalesBeats(contract, plan)
	require.NoError(t, err)
	require.Equal(t, "hook", plan.Shots[0].SalesBeat)
	contract.Segments = append(contract.Segments, CommerceSalesScriptSegmentContract{
		Ordinal: 2, SourceSegmentID: "00000000-0000-4000-8000-000000000099",
		SalesBeat: "demonstration", VisualIntent: "补充商品操作画面",
	})
	plan.Shots[0].SourceSegmentIDs = append(plan.Shots[0].SourceSegmentIDs, contract.Segments[1].SourceSegmentID)
	plan.Shots[0].SalesBeat = "proof"
	plan, err = reconcileCommerceStoryboardSalesBeats(contract, plan)
	require.NoError(t, err, "visual-only segments may cross sales beats inside one shot")
	require.Equal(t, "hook", plan.Shots[0].SalesBeat, "the voiced segment owns the shot sales beat")

	review := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code: "SALES_BEAT_MISMATCH", CandidateKey: plan.Shots[0].CandidateKey,
			Field: "salesBeat", Message: "Localization 使用了准备阶段分类",
			Suggestion: "改为 Localization 的 script",
		}},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)

	require.Equal(t, "approve", review.Decision)
	require.Empty(t, review.Issues)
	require.NoError(t, ValidateCommerceStoryboardReview(review, plan))

	durationReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{
			{
				Code: "SHOT_DURATION_NOT_ALLOWED", CandidateKey: plan.Shots[0].CandidateKey,
				Field: "durationSeconds", Message: "镜头时长不在供应商请求时长集合内",
				Suggestion: "使用供应商请求时长",
			},
			{
				Code: "VOICEOVER_TIMING_OVERFLOW", CandidateKey: plan.Shots[0].CandidateKey,
				Field: "voiceoverText", Message: "预计口播时长超过用户选择的目标时长",
				Suggestion: "延长视频",
			},
		},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)
	require.Equal(t, "approve", durationReview.Decision)
	require.Empty(t, durationReview.Issues)
	require.NoError(t, ValidateCommerceStoryboardReview(durationReview, plan))

	productReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code: "PRODUCT_REFERENCE_MISSING", CandidateKey: plan.Shots[0].CandidateKey,
			Field: "productReferenceIds", Message: "商品引用缺失", Suggestion: "补充当前引用包中的商品图",
		}},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)
	require.Equal(t, "approve", productReview.Decision)
	require.Empty(t, productReview.Issues)

	segmentBindingReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code: "SOURCE_SEGMENT_BINDING_MISMATCH", CandidateKey: plan.Shots[0].CandidateKey,
			Field: "sourceSegmentIds", Message: "审阅声称片段重复绑定", Suggestion: "重新分配片段",
		}},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)
	require.Equal(t, "approve", segmentBindingReview.Decision)
	require.Empty(t, segmentBindingReview.Issues)

	contextOrderReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{
			{
				Code: "SOURCE_SEGMENT_ORDER_INVALID", CandidateKey: plan.Shots[0].CandidateKey,
				Field: "sourceSegmentIds", Message: "视觉说明未按原文件块顺序绑定", Suggestion: "按行号重排",
			},
			{
				Code: "SEGMENT_LINK_ORDER_INVALID", CandidateKey: plan.Shots[0].CandidateKey,
				Field: "segmentLinks", Message: "上下文链接未按原文件块顺序绑定", Suggestion: "按行号重排",
			},
		},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)
	require.Equal(t, "approve", contextOrderReview.Decision)
	require.Empty(t, contextOrderReview.Issues)

	aspectRatioReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code: "ASPECT_RATIO_MISMATCH", CandidateKey: plan.Shots[0].CandidateKey,
			Field: "aspectRatio", Message: "平台通常使用其他画幅", Suggestion: "改变项目画幅",
		}},
		CheckedCandidateKeys:    []string{plan.Shots[0].CandidateKey},
		SegmentCoverageComplete: true,
		DurationTotalSeconds:    plan.TargetDurationSeconds,
	}, plan)
	require.Equal(t, "approve", aspectRatioReview.Decision)
	require.Empty(t, aspectRatioReview.Issues)

	duplicatePlan := plan
	duplicatePlan.Shots = append(append([]CommerceStoryboardShotContract(nil), plan.Shots...), CommerceStoryboardShotContract{
		CandidateKey:     "hook-product-02",
		ShotOrdinal:      2,
		SourceSegmentIDs: []string{plan.Shots[0].SourceSegmentIDs[0]},
	})
	duplicateBindingReview := reconcileCommerceStoryboardReview(CommerceStoryboardReviewContract{
		ContractVersion: CommerceStoryboardReviewContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code: "SOURCE_SEGMENT_BINDING_MISMATCH", CandidateKey: plan.Shots[0].CandidateKey,
			Field: "sourceSegmentIds", Message: "片段确实重复绑定", Suggestion: "重新分配片段",
		}},
	}, duplicatePlan)
	require.Equal(t, "approve", duplicateBindingReview.Decision)
	require.Empty(t, duplicateBindingReview.Issues)

	plan.Shots[0].SourceSegmentIDs = append(plan.Shots[0].SourceSegmentIDs, "00000000-0000-4000-8000-000000000098")
	_, err = reconcileCommerceStoryboardSalesBeats(contract, plan)
	require.ErrorContains(t, err, "outside the sales script")
}

func TestBindCommerceStoryboardPlanIdentityFillsMissingFrozenFields(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	plan := CommerceStoryboardPlanContract{
		CommerceScriptUnitID:   snapshot.Identity.ScriptUnitID,
		ScriptUnitGenerationID: snapshot.Identity.UnitGenerationID,
		ProductVersionID:       snapshot.ProductVersionID,
	}

	bound, err := bindCommerceStoryboardPlanIdentity(snapshot, plan)

	require.NoError(t, err)
	require.Equal(t, snapshot.Identity.CommerceWorkflowBindingID, bound.CommerceWorkflowBindingID)
}

func TestBindCommerceStoryboardPlanIdentityOverwritesAgentOwnedFrozenFields(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	plan := CommerceStoryboardPlanContract{
		CommerceScriptUnitID:      "00000000-0000-4000-8000-000000000097",
		ScriptUnitGenerationID:    "00000000-0000-4000-8000-000000000098",
		CommerceWorkflowBindingID: "00000000-0000-4000-8000-000000000099",
		ProductVersionID:          "00000000-0000-4000-8000-000000000096",
		TargetLocale:              "invalid-agent-locale",
		TargetDurationSeconds:     999,
	}

	bound, err := bindCommerceStoryboardPlanIdentity(snapshot, plan)

	require.NoError(t, err)
	require.Equal(t, snapshot.Identity.ScriptUnitID, bound.CommerceScriptUnitID)
	require.Equal(t, snapshot.Identity.UnitGenerationID, bound.ScriptUnitGenerationID)
	require.Equal(t, snapshot.Identity.CommerceWorkflowBindingID, bound.CommerceWorkflowBindingID)
	require.Equal(t, snapshot.ProductVersionID, bound.ProductVersionID)
	require.Equal(t, snapshot.TargetLocale, bound.TargetLocale)
	require.Equal(t, snapshot.TargetDurationSeconds, bound.TargetDurationSeconds)
}

func TestReconcileCommerceStoryboardVoiceoverUsesFrozenVerbatimText(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	snapshot.LocalizedSegments[0].VoiceoverText = "Helmet yang viral... betul ke?"
	plan := CommerceStoryboardPlanContract{
		Shots: []CommerceStoryboardShotContract{{
			CandidateKey:     "shot-001",
			SourceSegmentIDs: []string{snapshot.LocalizedSegments[0].SourceSegmentID},
			VoiceoverText:    "Helmet yang viral… betul ke?",
		}},
	}

	reconciled, err := reconcileCommerceStoryboardVoiceover(snapshot, plan)

	require.NoError(t, err)
	require.Equal(t, snapshot.LocalizedSegments[0].VoiceoverText, reconciled.Shots[0].VoiceoverText)
}

func TestWithCommerceOutputContractAppendsStrictSchema(t *testing.T) {
	rendered := promptsvc.RenderedPrompt{
		RenderedText: "Return JSON.",
		RenderedHash: promptsvc.HashText("Return JSON."),
		Metadata: json.RawMessage(`{
			"outputContract":{
				"type":"object",
				"additionalProperties":false,
				"required":["contractVersion"]
			}
		}`),
	}

	withContract := withCommerceOutputContract(rendered)

	require.Contains(t, withContract.RenderedText, `"additionalProperties":false`)
	require.Contains(t, withContract.RenderedText, "不得增加未声明字段")
	require.NotEqual(t, rendered.RenderedHash, withContract.RenderedHash)
}

func testCommerceSalesScriptWorkflowFixture(t *testing.T) (
	CommerceScriptOrganizationInput,
	CommerceStoryboardPlanningSnapshot,
	CommerceSalesScriptContract,
	string,
) {
	t.Helper()
	identity := testCommerceReferenceImageIdentity()
	binding := func(role string, ordinal int) CommerceAgentBinding {
		return CommerceAgentBinding{
			Role: role, TemplateKey: "commerce." + role,
			PromptVersionID:   fmt.Sprintf("00000000-0000-4000-8000-%012d", 30+ordinal),
			PromptContentHash: strings.Repeat(string(rune('a'+ordinal)), 64),
			ModelProfileKey:   "commerce_text", ProviderModelID: fmt.Sprintf("00000000-0000-4000-8000-%012d", 40+ordinal),
			MaxReviewRounds: CommerceMaxAgentReviewRounds,
		}
	}
	videoEnvelope := commerce.VideoExecutionEnvelope{
		ContractVersion:                    commerce.CommerceVideoEnvelopeV1,
		ProjectProductionGenerationID:      identity.ProjectGenerationID,
		VideoProductionBindingID:           identity.VideoProductionBindingID,
		VideoProductionBindingRevision:     identity.VideoProductionBindingRevision,
		VideoProductionProfileVersionID:    "00000000-0000-4000-8000-000000000060",
		VideoProductionProfileSnapshotHash: identity.VideoProfileSnapshotHash,
		ModelProfileKey:                    "commerce_video", TargetResolution: "720p", AspectRatio: "9:16",
		Routes: []commerce.VideoExecutionRoute{{
			RouteKey: "route-1", ModelProfileID: "00000000-0000-4000-8000-000000000061",
			ModelProfileKey: "commerce_video", ModelProfileBindingID: "00000000-0000-4000-8000-000000000062",
			ProviderModelID:   "00000000-0000-4000-8000-000000000063",
			ProviderAccountID: "00000000-0000-4000-8000-000000000064",
			ModelKey:          "video-test", Priority: 100, Weight: 100, VariantKey: "default",
			CapabilitySnapshotHash:    strings.Repeat("8", 64),
			ExecutableDurationSeconds: []int{5}, Resolutions: []string{"720p"},
			AspectRatios: []string{"9:16"},
		}},
		ExecutableDurationSeconds: []int{5},
	}
	videoEnvelope, videoEnvelopeHash, err := commerce.CanonicalizeVideoExecutionEnvelope(videoEnvelope)
	require.NoError(t, err)
	snapshot := CommerceStoryboardPlanningSnapshot{
		Identity: identity, InputHash: strings.Repeat("d", 64),
		ProductVersionID:      "00000000-0000-4000-8000-000000000009",
		SourceScriptVersionID: "00000000-0000-4000-8000-00000000000a",
		LocalizationID:        "00000000-0000-4000-8000-00000000000b",
		ReferencePackID:       "00000000-0000-4000-8000-00000000000c",
		TargetLocale:          "zh-CN", TargetDurationSeconds: 15, AspectRatio: "9:16",
		TimelineTimebase: 24000, FPSNumerator: 24, FPSDenominator: 1,
		TimingPolicyVersion: "commerce-zh/v1", LocalizedContentHash: strings.Repeat("e", 64),
		TimingPolicy: CommerceTimingPolicy{
			Version: "commerce-zh/v1", Unit: "han_character", NormalUnitsPerSecond: 3.5,
			CommaPauseSeconds: 0.15, SentencePauseSeconds: 0.3, SegmentGapSeconds: 0.1,
		},
		LocalizedContractHash: strings.Repeat("f", 64), AllowedShotDurations: []int{5},
		StoryboardStrategy:         commerce.StoryboardStrategySmart,
		SegmentationPolicyVersion:  commerce.CommerceSegmentationPolicyV2,
		VideoExecutionEnvelope:     videoEnvelope,
		VideoExecutionEnvelopeHash: videoEnvelopeHash,
		LocalizedSegments: []CommerceLocalizedSegmentSnapshot{{
			ID: "00000000-0000-4000-8000-00000000000d", SourceSegmentID: "00000000-0000-4000-8000-00000000000e",
			Ordinal: 1, SalesBeat: "hook", LocalizedText: "轻巧便携，随时使用", VoiceoverText: "轻巧便携，随时使用",
			OnscreenText: "轻巧便携", RequiredProductFeatures: []string{"包装外形"}, Required: true,
		}},
		ProductReferences: []CommerceProductReferenceSnapshot{{
			PackItemID: "00000000-0000-4000-8000-00000000000f", ReferenceID: "00000000-0000-4000-8000-000000000010",
			Role: "primary", Ordinal: 1, ContentHash: strings.Repeat("1", 64), Required: true,
		}},
		ProductFacts: json.RawMessage(`{"name":"示例商品"}`), LocalizationContract: json.RawMessage(`{"segments":[]}`),
		Bindings: CommerceStoryboardAgentBindings{
			ScriptOrganizer: binding("script_organizer", 1), StoryboardPlanner: binding("storyboard_planner", 2),
			StoryboardReviewer: binding("storyboard_reviewer", 3),
		},
	}
	contract := CommerceSalesScriptContract{
		ContractVersion: CommerceSalesScriptContractVersion, CommerceScriptUnitID: identity.ScriptUnitID,
		ScriptUnitGenerationID: identity.UnitGenerationID, ProductVersionID: snapshot.ProductVersionID,
		TargetLocale: snapshot.TargetLocale, TargetDurationSeconds: snapshot.TargetDurationSeconds,
		Segments: []CommerceSalesScriptSegmentContract{{
			Ordinal: 1, SourceSegmentID: snapshot.LocalizedSegments[0].SourceSegmentID, SalesBeat: "hook",
			VoiceoverText: snapshot.LocalizedSegments[0].VoiceoverText, OnscreenText: snapshot.LocalizedSegments[0].OnscreenText,
			VisualIntent: "商品正面展示", RequiredProductFeatures: []string{"包装外形"},
		}},
	}
	require.NoError(t, ValidateCommerceStoryboardSnapshot(identity, snapshot))
	require.NoError(t, ValidateCommerceSalesScript(contract, snapshot))
	contractHash, err := commerceContractHash(contract)
	require.NoError(t, err)
	input := CommerceScriptOrganizationInput{
		Identity: identity, WorkflowRunID: "00000000-0000-4000-8000-000000000011",
		CreatedBy: "00000000-0000-4000-8000-000000000012", AttemptGeneration: 1,
	}
	return input, snapshot, contract, contractHash
}
