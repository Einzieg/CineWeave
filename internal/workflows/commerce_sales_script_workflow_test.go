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
