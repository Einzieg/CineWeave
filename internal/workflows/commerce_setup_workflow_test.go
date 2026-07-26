package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceProjectSetupWorkflowCompletesWithoutLanguageConfirmation(t *testing.T) {
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

	env.ExecuteWorkflow(CommerceProjectSetupWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceProjectSetupOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "completed", output.Status)
	require.Equal(t, 1, calls)
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

func TestBuildCommerceIdentityLocalizationParsesSectionedMalayScript(t *testing.T) {
	source := []string{
		"Create a realistic 15-second TikTok UGC-style motorcycle helmet showcase video.",
		"SETTING:",
		"A quiet roadside in Malaysia during golden hour.",
		"VOICEOVER (Malay):",
		`"Helmet yang tengah viral dekat TikTok ni memang berbaloi."`,
		`"Finishing matte dia memang cantik dan berkualiti."`,
		"SCENE 1 (0-3s)",
		"The presenter catches the helmet and looks at the camera.",
		`"Helmet yang tengah viral dekat TikTok ni memang berbaloi."`,
		"AUDIO:",
		"Add energetic yet clean TikTok lifestyle BGM.",
	}
	segments := make([]CommerceSourceSegmentSnapshot, 0, len(source))
	for index, text := range source {
		segments = append(segments, CommerceSourceSegmentSnapshot{
			ID:         fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1),
			Ordinal:    index + 1,
			Kind:       "script",
			SourceText: text,
		})
	}
	snapshot := CommerceScriptUnitPreparationSnapshot{SourceSegments: segments}
	resolution := CommerceLanguageResolutionContract{
		ContractVersion:     CommerceLanguageResolutionContractVersion,
		SourceLanguage:      "ms-MY",
		TargetLanguage:      "ms-MY",
		LanguageComposition: "mixed",
	}

	candidate := BuildCommerceIdentityLocalization(snapshot, resolution)
	for _, index := range []int{0, 1, 2, 3, 6, 7, 8, 9, 10} {
		require.Empty(t, candidate.Segments[index].VoiceoverText, "segment %d must not become speech", index+1)
	}
	require.Equal(t, "Helmet yang tengah viral dekat TikTok ni memang berbaloi.", candidate.Segments[4].VoiceoverText)
	require.Equal(t, "Finishing matte dia memang cantik dan berkualiti.", candidate.Segments[5].VoiceoverText)
	require.NoError(t, ValidateCommerceLocalization(candidate, snapshot, resolution))

	review := BuildCommerceIdentityLocalizationReview(candidate)
	require.Equal(t, "approve", review.Decision)
	require.Len(t, review.CheckedSegmentIDs, len(source))
	require.NoError(t, ValidateCommerceLocalizationReview(review, candidate))
}

func TestCanonicalizeApprovedCommerceLocalizationReviewUsesCandidateCoverage(t *testing.T) {
	candidate := CommerceLocalizationContract{
		Segments: []CommerceLocalizationSegmentContract{
			{SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b"},
			{SourceSegmentID: "5c8f1f08-b273-4daf-902e-f54569251aa6"},
		},
	}
	review := CommerceLocalizationReviewContract{
		ContractVersion: CommerceReviewDecisionContractVersion,
		Decision:        "approve",
		Issues:          []CommerceReviewIssue{},
		CheckedSegmentIDs: []string{
			"4bd6107a-5634-412d-86ff-02a093bb2b",
			"5c8f1f08-b273-4daf-902e-f54569251aa6",
		},
	}

	require.ErrorContains(t, ValidateCommerceLocalizationReview(review, candidate), "unknown id")
	review = CanonicalizeApprovedCommerceLocalizationReview(review, candidate)
	require.Equal(t, []string{
		"4bd6107a-5634-412d-86ff-ff02a093bb2b",
		"5c8f1f08-b273-4daf-902e-f54569251aa6",
	}, review.CheckedSegmentIDs)
	require.NoError(t, ValidateCommerceLocalizationReview(review, candidate))
}

func TestCanonicalizeApprovedCommerceLocalizationReviewDoesNotRewriteRevision(t *testing.T) {
	candidate := CommerceLocalizationContract{
		Segments: []CommerceLocalizationSegmentContract{
			{SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b"},
		},
	}
	review := CommerceLocalizationReviewContract{
		ContractVersion: CommerceReviewDecisionContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			SourceSegmentID: "4bd6107a-5634-412d-86ff-02a093bb2b",
			Code:            "semantic_fidelity",
			Message:         "revise this segment",
		}},
		CheckedSegmentIDs: []string{"4bd6107a-5634-412d-86ff-02a093bb2b"},
	}

	normalized := CanonicalizeApprovedCommerceLocalizationReview(review, candidate)
	require.Equal(t, review, normalized)
	require.Error(t, ValidateCommerceLocalizationReview(normalized, candidate))
}

func TestCanonicalizeCommerceLocalizationReviewCoverageUsesCandidateIdentityForRevision(t *testing.T) {
	candidate := CommerceLocalizationContract{
		Segments: []CommerceLocalizationSegmentContract{
			{SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b"},
		},
	}
	review := CommerceLocalizationReviewContract{
		ContractVersion: CommerceReviewDecisionContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code:            "PRICE_CHANGED",
			Field:           "segments[0].voiceoverText",
			SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b",
			Message:         "价格发生变化",
			Suggestion:      "恢复原价格",
		}},
		CheckedSegmentIDs: []string{"4bd6107a-5634-412d-86ff-02a093bb2b"},
	}

	normalized := CanonicalizeCommerceLocalizationReviewCoverage(review, candidate)
	require.Equal(t, []string{"4bd6107a-5634-412d-86ff-ff02a093bb2b"}, normalized.CheckedSegmentIDs)
	require.NoError(t, ValidateCommerceLocalizationReview(normalized, candidate))
}

func TestApplyCommerceLocalizationReviewPolicyTreatsStyleFeedbackAsAdvisory(t *testing.T) {
	candidate := CommerceLocalizationContract{
		Segments: []CommerceLocalizationSegmentContract{
			{SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b"},
		},
		Warnings: []json.RawMessage{},
	}
	for _, code := range []string{"LANGUAGE_NATURALNESS", "NATURALNESS_ISSUE", "PRODUCT_CLAIM_STRENGTHENED"} {
		t.Run(code, func(t *testing.T) {
			review := CommerceLocalizationReviewContract{
				ContractVersion: CommerceReviewDecisionContractVersion,
				Decision:        "revise",
				Issues: []CommerceReviewIssue{{
					Code:            code,
					Field:           "segments[0].voiceoverText",
					SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b",
					Message:         "仅为不改变事实的表达建议",
					Suggestion:      "可选润色",
				}},
			}

			updated, approvedReview, approved := ApplyCommerceLocalizationReviewPolicy(candidate, review)
			require.True(t, approved)
			require.Equal(t, "approve", approvedReview.Decision)
			require.Empty(t, approvedReview.Issues)
			require.Equal(t, []string{"4bd6107a-5634-412d-86ff-ff02a093bb2b"}, approvedReview.CheckedSegmentIDs)
			require.Len(t, updated.Warnings, 1)
			require.Contains(t, string(updated.Warnings[0]), code)
		})
	}
}

func TestApplyCommerceLocalizationReviewPolicyKeepsMaterialFactsAsWarnings(t *testing.T) {
	candidate := CommerceLocalizationContract{
		Segments: []CommerceLocalizationSegmentContract{
			{SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b"},
		},
	}
	review := CommerceLocalizationReviewContract{
		ContractVersion: CommerceReviewDecisionContractVersion,
		Decision:        "revise",
		Issues: []CommerceReviewIssue{{
			Code:            "PRICE_CHANGED",
			Field:           "segments[0].voiceoverText",
			SourceSegmentID: "4bd6107a-5634-412d-86ff-ff02a093bb2b",
			Message:         "价格发生变化",
			Suggestion:      "恢复原价格",
		}},
	}

	updated, approvedReview, approved := ApplyCommerceLocalizationReviewPolicy(candidate, review)
	require.True(t, approved)
	require.Len(t, updated.Warnings, 1)
	require.Contains(t, string(updated.Warnings[0]), "PRICE_CHANGED")
	require.Equal(t, "approve", approvedReview.Decision)
	require.Empty(t, approvedReview.Issues)
}

func TestCommerceAutomaticLanguageAcceptsMixedMalayWithoutConfirmation(t *testing.T) {
	snapshot := CommerceScriptUnitPreparationSnapshot{
		LanguageMode:                "auto",
		AllowedLocales:              []string{"en-US", "ms-MY"},
		LanguageConfidenceThreshold: 0.8,
		LanguageConfirmationMode:    CommerceLanguageConfirmationDisabled,
	}
	resolution := CommerceLanguageResolutionContract{
		ContractVersion:       CommerceLanguageResolutionContractVersion,
		SourceLanguage:        "ms-MY",
		TargetLanguage:        "ms-MY",
		Confidence:            0.98,
		LanguageComposition:   "mixed",
		NeedsUserConfirmation: false,
		Reasoning:             "VOICEOVER (Malay) declares Malay speech; English text only describes scenes.",
		Issues:                []CommerceLanguageIssue{},
	}
	require.NoError(t, ValidateCommerceLanguageResolution(resolution, snapshot))

	resolution.NeedsUserConfirmation = true
	require.ErrorContains(t, ValidateCommerceLanguageResolution(resolution, snapshot), "must not require user confirmation")
}

func TestParseCommerceSetupLanguageConfigurationPropagatesTemplateLocales(t *testing.T) {
	configuration, err := parseCommerceSetupLanguageConfiguration(json.RawMessage(`{
		"resolver": {
			"autoConfidenceThreshold": 0.8,
			"confirmationMode": "disabled"
		},
		"locales": [
			{
				"locale": "en-us",
				"timingPolicy": {
					"version": "en-us-voiceover/v2",
					"unit": "word",
					"normalUnitsPerSecond": 2.5
				}
			},
			{
				"locale": "ms-my",
				"timingPolicy": {
					"version": "ms-my-voiceover/v1",
					"unit": "word",
					"normalUnitsPerSecond": 2.5
				}
			}
		]
	}`))

	require.NoError(t, err)
	require.Equal(t, []string{"en-US", "ms-MY"}, configuration.LocaleSuggestions)
	require.Equal(t, 0.8, configuration.ConfidenceThreshold)
	require.Equal(t, CommerceLanguageConfirmationDisabled, configuration.ConfirmationMode)
	require.Equal(t, "ms-my-voiceover/v1", configuration.TimingPolicies["ms-MY"].Version)
	require.Equal(t, 2.5, configuration.TimingPolicies["ms-MY"].NormalUnitsPerSecond)
}

func TestParseCommerceSetupLanguageConfigurationRejectsDuplicateCanonicalLocale(t *testing.T) {
	_, err := parseCommerceSetupLanguageConfiguration(json.RawMessage(`{
		"resolver": {
			"autoConfidenceThreshold": 0.8,
			"confirmationMode": "disabled"
		},
		"locales": [
			{"locale": "ms-my"},
			{"locale": "ms-MY"}
		]
	}`))

	require.ErrorContains(t, err, "locale ms-MY is duplicated")
}

func TestParseCommerceSetupLanguageConfigurationRejectsMissingFrozenSettings(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		message string
	}{
		{
			name:    "missing locales",
			raw:     `{"resolver":{"autoConfidenceThreshold":0.8,"confirmationMode":"disabled"},"locales":[]}`,
			message: "no locale suggestions",
		},
		{
			name:    "missing confidence threshold",
			raw:     `{"resolver":{"confirmationMode":"disabled"},"locales":[{"locale":"ms-MY"}]}`,
			message: "confidence threshold",
		},
		{
			name:    "confirmation enabled",
			raw:     `{"resolver":{"autoConfidenceThreshold":0.8,"confirmationMode":"required"},"locales":[{"locale":"ms-MY"}]}`,
			message: "confirmation mode must be disabled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseCommerceSetupLanguageConfiguration(json.RawMessage(test.raw))
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestAnalyzeCommerceTimingSupportsMalayWordPolicy(t *testing.T) {
	localization := CommerceLocalizationContract{
		ContractVersion: CommerceLocalizationContractVersion,
		SourceLanguage:  "ms-MY",
		TargetLanguage:  "ms-MY",
		Segments: []CommerceLocalizationSegmentContract{{
			Ordinal:         1,
			SourceSegmentID: "00000000-0000-4000-8000-000000000001",
			LocalizedText:   "Helmet ini memang selesa.",
			VoiceoverText:   "Helmet ini memang selesa.",
		}},
	}
	timing, err := AnalyzeCommerceTiming(localization, CommerceTimingPolicy{
		Version: "ms-my-voiceover/v1", Unit: "word", NormalUnitsPerSecond: 2.5,
		CommaPauseSeconds: 0.15, SentencePauseSeconds: 0.35,
	}, 15)
	require.NoError(t, err)
	require.Equal(t, 4, timing.Units)
	require.Equal(t, "ms-MY", timing.Locale)
	require.False(t, timing.Exceeded)
}

func TestAnalyzeCommerceTimingReturnsOverrunAsAdvisory(t *testing.T) {
	localization := CommerceLocalizationContract{
		ContractVersion: CommerceLocalizationContractVersion,
		SourceLanguage:  "ms-MY",
		TargetLanguage:  "ms-MY",
		Segments: []CommerceLocalizationSegmentContract{
			{
				Ordinal:         1,
				SourceSegmentID: "00000000-0000-4000-8000-000000000001",
				LocalizedText:   "SETTING:",
			},
			{
				Ordinal:         2,
				SourceSegmentID: "00000000-0000-4000-8000-000000000002",
				LocalizedText:   "Helmet ini sangat selesa...",
				VoiceoverText:   "Helmet ini sangat selesa...",
			},
			{
				Ordinal:         3,
				SourceSegmentID: "00000000-0000-4000-8000-000000000003",
				LocalizedText:   "AUDIO:",
			},
			{
				Ordinal:         4,
				SourceSegmentID: "00000000-0000-4000-8000-000000000004",
				LocalizedText:   "Memang berbaloi?!",
				VoiceoverText:   "Memang berbaloi?!",
			},
		},
	}
	policy := CommerceTimingPolicy{
		Version: "ms-my-voiceover/v1", Unit: "word", NormalUnitsPerSecond: 2.5,
		SentencePauseSeconds: 0.35, SegmentGapSeconds: 0.1,
	}
	timing, err := AnalyzeCommerceTiming(localization, policy, 2)
	require.NoError(t, err)
	require.Equal(t, 6, timing.Units)
	require.InDelta(t, 3.2, timing.EstimatedVoiceoverSeconds, 0.001)
	require.True(t, timing.Exceeded)

	// An overrun is persisted as an advisory. Recomputing the frozen timing remains valid.
	recomputed, err := AnalyzeCommerceTiming(localization, policy, 2)
	require.NoError(t, err)
	require.Equal(t, timing, recomputed)
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
