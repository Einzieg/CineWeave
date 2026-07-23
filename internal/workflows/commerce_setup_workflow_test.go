package workflows

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceProjectSetupWorkflowIgnoresStaleConfirmationAndResumes(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := CommerceProjectSetupInput{
		OrganizationID: "00000000-0000-4000-8000-000000000001", ProjectID: "00000000-0000-4000-8000-000000000002", SetupSessionID: "session",
		ExpectedSessionRevision: 1, WorkflowTemplateVersionID: "00000000-0000-4000-8000-000000000003",
		ProductID: "00000000-0000-4000-8000-000000000004", ProductVersionID: "00000000-0000-4000-8000-000000000005",
		ScriptUnitID: "00000000-0000-4000-8000-000000000006", SourceScriptVersionID: "00000000-0000-4000-8000-000000000007", RequestedBy: "00000000-0000-4000-8000-000000000008",
	}
	identity := testCommerceSetupIdentity(input)
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, current CommerceProjectSetupInput) (CommerceProjectSetupOutput, error) {
		calls++
		if calls == 1 {
			return CommerceProjectSetupOutput{
				SetupSessionID: current.SetupSessionID, LanguageResolutionID: "resolution",
				SuggestedTargetLanguage: "zh-CN", NeedsUserConfirmation: true,
				Status: "waiting_user_confirmation",
			}, nil
		}
		require.Equal(t, "resolution", current.LanguageResolutionID)
		require.Equal(t, "zh-CN", current.ConfirmedTargetLanguage)
		return CommerceProjectSetupOutput{
			Identity: identity, SetupSessionID: current.SetupSessionID, ProjectGenerationID: identity.ProjectGenerationID,
			VideoProductionBindingID: identity.VideoProductionBindingID, VideoProductionBindingRevision: identity.VideoProductionBindingRevision,
			CommerceWorkflowBindingID: identity.CommerceWorkflowBindingID, CommerceWorkflowBindingRevision: identity.CommerceWorkflowBindingRevision,
			ScriptUnitGenerationID: identity.UnitGenerationID, ScriptUnitGenerationNo: identity.UnitGenerationNo,
			LocalizationID: "00000000-0000-4000-8000-000000000012", ReferencePackID: "00000000-0000-4000-8000-000000000013",
			ProductionWorkflowRunID: "00000000-0000-4000-8000-000000000014", Status: "completed",
		}, nil
	}, activity.RegisterOptions{Name: ExecuteCommerceProjectSetupActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceProjectSetupFailureInput) error { return nil }, activity.RegisterOptions{Name: FailCommerceProjectSetupActivityName})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(CommerceSetupLanguageConfirmationSignalName, CommerceSetupLanguageConfirmationSignal{
			SetupSessionID: "stale-session", LanguageResolutionID: "resolution", TargetLanguage: "zh-CN",
		})
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(CommerceSetupLanguageConfirmationSignalName, CommerceSetupLanguageConfirmationSignal{
			SetupSessionID: input.SetupSessionID, LanguageResolutionID: "resolution", TargetLanguage: "zh-CN",
		})
	}, 2*time.Second)

	env.ExecuteWorkflow(CommerceProjectSetupWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceProjectSetupOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "completed", output.Status)
	require.Equal(t, 2, calls)
}

func TestValidateCommerceAgentBindingAcceptsPromptRegistryContentHash(t *testing.T) {
	binding := CommerceAgentBinding{
		Role:              "languageResolver",
		TemplateKey:       "commerce_language_resolver",
		PromptVersionID:   "00000000-0000-4000-8000-000000000001",
		PromptContentHash: "sha256:" + strings.Repeat("a", 64),
		ModelProfileKey:   "script_agent_default",
		ProviderModelID:   "00000000-0000-4000-8000-000000000002",
		MaxReviewRounds:   CommerceMaxAgentReviewRounds,
	}
	require.NoError(t, ValidateCommerceAgentBinding(binding))

	binding.PromptContentHash = "sha256:not-a-hash"
	require.ErrorContains(t, ValidateCommerceAgentBinding(binding), "prompt content hash is invalid")
}

func TestBuildCommerceIdentityLocalizationSeparatesContentChannels(t *testing.T) {
	snapshot := CommerceScriptUnitPreparationSnapshot{
		SourceSegments: []CommerceSourceSegmentSnapshot{
			{
				ID:         "00000000-0000-4000-8000-000000000001",
				Ordinal:    1,
				Kind:       "script",
				SourceText: "镜头一：白色随行杯立在桌面。旁白：每天出门，热饮也应该从容随行。音效：杯盖轻响",
			},
			{
				ID:         "00000000-0000-4000-8000-000000000002",
				Ordinal:    2,
				Kind:       "script",
				SourceText: "字幕：通勤携带更省心。音效：拉链声",
			},
			{
				ID:         "00000000-0000-4000-8000-000000000003",
				Ordinal:    3,
				Kind:       "script",
				SourceText: "让每一程都有合适温度。",
			},
		},
	}
	resolution := CommerceLanguageResolutionContract{
		ContractVersion: CommerceLanguageResolutionContractVersion,
		SourceLanguage:  "zh-CN",
		TargetLanguage:  "zh-CN",
	}

	candidate := BuildCommerceIdentityLocalization(snapshot, resolution)
	require.Equal(t, "每天出门，热饮也应该从容随行。", candidate.Segments[0].VoiceoverText)
	require.Empty(t, candidate.Segments[0].OnscreenText)
	require.Empty(t, candidate.Segments[1].VoiceoverText)
	require.Equal(t, "通勤携带更省心。", candidate.Segments[1].OnscreenText)
	require.Equal(t, snapshot.SourceSegments[2].SourceText, candidate.Segments[2].VoiceoverText)
	require.NoError(t, ValidateCommerceLocalization(candidate, snapshot, resolution))
}

func testCommerceSetupIdentity(input CommerceProjectSetupInput) commerce.UnitGenerationIdentity {
	return commerce.UnitGenerationIdentity{
		ExecutionIdentity: commerce.ExecutionIdentity{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
			ProjectGenerationID:      "00000000-0000-4000-8000-000000000009",
			VideoProductionBindingID: "00000000-0000-4000-8000-00000000000a", VideoProductionBindingRevision: 1,
			VideoProfileSnapshotHash:  strings.Repeat("a", 64),
			CommerceWorkflowBindingID: "00000000-0000-4000-8000-00000000000b", CommerceWorkflowBindingRevision: 1,
			CommerceConfigurationHash: strings.Repeat("b", 64),
		},
		ProductID: input.ProductID, ScriptUnitID: input.ScriptUnitID, ScriptUnitRevision: 2,
		UnitGenerationID: "00000000-0000-4000-8000-00000000000c", UnitGenerationNo: 1,
		UnitConfigurationHash: strings.Repeat("c", 64),
	}
}

func TestCommerceProjectSetupWorkflowFinalizesFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := CommerceProjectSetupInput{
		OrganizationID: "organization", ProjectID: "project", SetupSessionID: "session",
		ExpectedSessionRevision: 1, WorkflowTemplateVersionID: "template",
		ProductID: "product", ProductVersionID: "product-version",
		ScriptUnitID: "unit", SourceScriptVersionID: "source-version", RequestedBy: "user",
	}
	env.RegisterActivityWithOptions(func(context.Context, CommerceProjectSetupInput) (CommerceProjectSetupOutput, error) {
		return CommerceProjectSetupOutput{}, temporal.NewNonRetryableApplicationError("模型能力不匹配", provider.CodeUnsupportedCapability, nil)
	}, activity.RegisterOptions{Name: ExecuteCommerceProjectSetupActivityName})
	var failure CommerceProjectSetupFailureInput
	env.RegisterActivityWithOptions(func(_ context.Context, input CommerceProjectSetupFailureInput) error {
		failure = input
		return nil
	}, activity.RegisterOptions{Name: FailCommerceProjectSetupActivityName})

	env.ExecuteWorkflow(CommerceProjectSetupWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, input.SetupSessionID, failure.WorkflowInput.SetupSessionID)
	require.Equal(t, provider.CodeUnsupportedCapability, failure.ErrorCode)
	require.Contains(t, failure.ErrorMessage, "模型能力不匹配")
}
