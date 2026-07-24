package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
	require.Equal(t, "hook", agentSnapshot.Segments[0].SalesBeat)
	serialized, err := json.Marshal(agentSnapshot)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "localizationContract")
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
	require.NoError(t, validateCommerceStoryboardSalesBeats(contract, plan))

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

	plan.Shots[0].SalesBeat = "script"
	require.ErrorContains(t, validateCommerceStoryboardSalesBeats(contract, plan), "authoritative sales script")
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

func TestBindCommerceStoryboardPlanIdentityRejectsConflictingFrozenField(t *testing.T) {
	_, snapshot, _, _ := testCommerceSalesScriptWorkflowFixture(t)
	plan := CommerceStoryboardPlanContract{
		CommerceScriptUnitID:      snapshot.Identity.ScriptUnitID,
		ScriptUnitGenerationID:    snapshot.Identity.UnitGenerationID,
		CommerceWorkflowBindingID: "00000000-0000-4000-8000-000000000099",
		ProductVersionID:          snapshot.ProductVersionID,
	}

	_, err := bindCommerceStoryboardPlanIdentity(snapshot, plan)

	require.ErrorContains(t, err, "commerceWorkflowBindingId conflicts")
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
	snapshot := CommerceStoryboardPlanningSnapshot{
		Identity: identity, InputHash: strings.Repeat("d", 64),
		ProductVersionID:      "00000000-0000-4000-8000-000000000009",
		SourceScriptVersionID: "00000000-0000-4000-8000-00000000000a",
		LocalizationID:        "00000000-0000-4000-8000-00000000000b",
		ReferencePackID:       "00000000-0000-4000-8000-00000000000c",
		TargetLocale:          "zh-CN", TargetDurationSeconds: 15, AspectRatio: "9:16",
		TimelineTimebase: 24000, FPSNumerator: 24, FPSDenominator: 1,
		TimingPolicyVersion: "commerce-zh/v1", LocalizedContentHash: strings.Repeat("e", 64),
		LocalizedContractHash: strings.Repeat("f", 64), AllowedShotDurations: []int{5},
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
