package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestResolveShotAnchorRoleUsesProfilePrimaryAnchor(t *testing.T) {
	role, err := resolveShotAnchorRole(videoproduction.ProfileStoryboardSheet, "")
	if err != nil {
		t.Fatal(err)
	}
	if role != videoproduction.AnchorRoleStoryboardSheet {
		t.Fatalf("storyboard sheet primary anchor = %q", role)
	}
	role, err = resolveShotAnchorRole(videoproduction.ProfileFirstLastFrame, "")
	if err != nil || role != videoproduction.AnchorRolePlannedFirstFrame {
		t.Fatalf("first-last primary anchor = %q / %v", role, err)
	}
}

func TestShotImagePromptReviewAttemptLimitIsThree(t *testing.T) {
	if maxShotImagePromptReviewAttempts != 3 {
		t.Fatalf("maxShotImagePromptReviewAttempts = %d, want 3", maxShotImagePromptReviewAttempts)
	}
	options := shotImagePromptReviewActivityOptions()
	if options.RetryPolicy == nil || options.RetryPolicy.MaximumAttempts != 1 {
		t.Fatalf("activity retry policy = %+v; replaying the activity could exceed three review rounds", options.RetryPolicy)
	}
}

func TestParseReviewedImagePrompt(t *testing.T) {
	reviewed, err := parseReviewedImagePrompt("```json\n"+`{"approved":true,"finalPrompt":"A tense medium close-up.","negativePrompt":"no extra people"}`+"\n```", generatedImagePrompt{})
	if err != nil {
		t.Fatalf("parseReviewedImagePrompt: %v", err)
	}
	if !reviewed.Approved || reviewed.FinalPrompt != "A tense medium close-up." {
		t.Fatalf("reviewed = %+v", reviewed)
	}
}

func TestParseGeneratedImagePromptAcceptsStructuredAnchorRecords(t *testing.T) {
	generated, err := parseGeneratedImagePrompt(`{
		"prompt":"A stable wide shot.",
		"sourceAnchors":[{"type":"scene","message":"ancestral hall"}],
		"assetAnchors":["asset-1"],
		"conflictsResolved":[{"reason":"kept the locked age"}]
	}`)
	if err != nil {
		t.Fatalf("parseGeneratedImagePrompt: %v", err)
	}
	if messages := agentJSONListMessages(generated.SourceAnchors); len(messages) != 1 || !strings.Contains(messages[0], "ancestral hall") {
		t.Fatalf("source anchors = %#v", messages)
	}
}

func TestParseRejectedImagePromptAcceptsStructuredFeedbackWithoutReplacementPrompt(t *testing.T) {
	reviewed, err := parseReviewedImagePrompt(`{
		"approved":false,
		"issues":[{"type":"locked_fact_conflict","severity":"error","message":"age does not match the locked asset"}],
		"changes":[{"message":"select an age-compatible asset"}]
	}`, generatedImagePrompt{Prompt: "candidate"})
	if err != nil {
		t.Fatalf("parseReviewedImagePrompt: %v", err)
	}
	if reviewed.Approved {
		t.Fatal("rejected review was parsed as approved")
	}
	if summary := reviewedImagePromptSummary(reviewed); !strings.Contains(summary, "age does not match") || !strings.Contains(summary, "age-compatible") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestApplyShotImagePromptReviewFeedbackIncludesRequiredCorrectionContract(t *testing.T) {
	rendered := promptsvc.RenderedPrompt{RenderedText: "base prompt", RenderedHash: "old", Source: "system_active"}
	feedback := &shotImagePromptReviewFeedback{
		Attempt:                 1,
		PreviousCandidate:       generatedImagePrompt{Prompt: "old candidate"},
		ReviewerSuggestedPrompt: "corrected candidate",
		Issues:                  agentJSONListFromStrings("missing the clan leader"),
		Changes:                 agentJSONListFromStrings("add the clan leader without changing the scene"),
		Summary:                 "missing the clan leader",
	}
	corrected := applyShotImagePromptReviewFeedback(rendered, feedback, 2)
	for _, required := range []string{"review_correction", "missing the clan leader", "corrected candidate", "严格返回 JSON"} {
		if !strings.Contains(corrected.RenderedText, required) {
			t.Fatalf("corrected prompt missing %q: %s", required, corrected.RenderedText)
		}
	}
	if corrected.RenderedHash == rendered.RenderedHash || !strings.Contains(corrected.Source, "review_feedback_v1") {
		t.Fatalf("correction provenance was not updated: %+v", corrected)
	}
}

func TestReviewerCorrectedImagePromptDraftBecomesNextReviewCandidate(t *testing.T) {
	feedback := &shotImagePromptReviewFeedback{
		ReviewProviderCallID:            "review-call",
		ReviewModelID:                   "review-model",
		PreviousCandidate:               generatedImagePrompt{Prompt: "old candidate", NegativePrompt: "old negative"},
		ReviewerSuggestedPrompt:         "corrected candidate",
		ReviewerSuggestedNegativePrompt: "corrected negative",
		Changes:                         agentJSONListFromStrings("keep the blade moving"),
	}
	draft, provenance, ok := reviewerCorrectedImagePromptDraft(feedback)
	if !ok || draft.Prompt != "corrected candidate" || draft.NegativePrompt != "corrected negative" {
		t.Fatalf("corrected draft = %+v ok=%v", draft, ok)
	}
	if provenance.ProviderCallID != "review-call" || provenance.ModelID != "review-model" {
		t.Fatalf("correction provenance = %+v", provenance)
	}
	if messages := agentJSONListMessages(draft.ConflictsResolved); len(messages) != 1 || messages[0] != "keep the blade moving" {
		t.Fatalf("conflicts resolved = %#v", messages)
	}
	if imagePromptCorrectionSource(feedback) != "reviewer_correction" {
		t.Fatalf("correction source = %q", imagePromptCorrectionSource(feedback))
	}
}

func TestShouldAcceptFinalReviewerCorrectionOnlyOnFinalResolvableAttempt(t *testing.T) {
	reviewed := reviewedImagePrompt{
		Prompt:  "完整修正版提示词",
		Issues:  agentJSONListFromStrings("候选构图与镜头事实不一致"),
		Changes: agentJSONListFromStrings("将主体移动到画面右侧"),
	}
	if shouldAcceptFinalReviewerCorrection(2, reviewed) {
		t.Fatal("accepted reviewer correction before the final review attempt")
	}
	if !shouldAcceptFinalReviewerCorrection(3, reviewed) {
		t.Fatal("did not accept a complete, resolvable correction from the final review attempt")
	}
	reviewed.Issues = agentJSONListFromStrings("两个锁定事实无法同时满足")
	if shouldAcceptFinalReviewerCorrection(3, reviewed) {
		t.Fatal("accepted an explicitly unresolvable final correction")
	}
	reviewed.Issues = nil
	reviewed.Prompt = ""
	if shouldAcceptFinalReviewerCorrection(3, reviewed) {
		t.Fatal("accepted an empty final correction")
	}
}

func TestRequestStructuredImagePromptRetriesMalformedJSON(t *testing.T) {
	calls := 0
	request := provider.GatewayTextRequest{Input: mustJSON(map[string]any{"prompt": "return json"})}
	parsed, _, err := requestStructuredImagePrompt(
		context.Background(),
		NodeExecution{NodeRunID: "node", ExecutionToken: "token", AttemptGeneration: 1},
		request,
		func(_ context.Context, _ NodeExecution, req provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
			calls++
			if calls == 2 {
				var input map[string]any
				if err := json.Unmarshal(req.Input, &input); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(input["prompt"].(string), "结构化输出纠错") {
					t.Fatalf("retry prompt = %q", input["prompt"])
				}
				return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: `{"prompt":"fixed"}`}}, nil
			}
			return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: "not-json"}}, nil
		},
		parseGeneratedImagePrompt,
		"图片提示词生成 Agent",
	)
	if err != nil {
		t.Fatalf("requestStructuredImagePrompt: %v", err)
	}
	if calls != 2 || parsed.Prompt != "fixed" {
		t.Fatalf("calls=%d parsed=%+v", calls, parsed)
	}
}

func TestBuildReviewedShotImagePromptExcludesDialogueText(t *testing.T) {
	contextValue := shotImagePromptAgentContext{Shot: shotImagePromptShot{
		Visual:      "方源在山巅下令。",
		Camera:      "中近景",
		AspectRatio: "16:9",
	}}
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "杀上青茅山。", Kind: "dialogue"}}
	prompt := buildReviewedShotImagePrompt(
		stripScriptDialogueFromImagePrompt("Cinematic medium close-up of Fang Yuan commanding the group. 方源说：“杀上青茅山。” 台词仅作为表演上下文。", dialogue),
		"no extra people",
		contextValue,
	)
	for _, required := range []string{"方源", "No on-screen text", "16:9"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
	if strings.Contains(prompt, "杀上青茅山。") || strings.Contains(prompt, "台词") || strings.Contains(prompt, "Performance context only") {
		t.Fatalf("prompt retained dialogue text: %s", prompt)
	}
	if err := validateShotImagePromptContainsNoDialogue(prompt, dialogue); err != nil {
		t.Fatalf("dialogue-free prompt rejected: %v", err)
	}
}

func TestValidateShotImagePromptContainsNoDialogueRejectsLiteralLine(t *testing.T) {
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "杀上青茅山。", Kind: "dialogue"}}
	if err := validateShotImagePromptContainsNoDialogue("方源正在说：杀上青茅山。", dialogue); err == nil {
		t.Fatal("expected literal dialogue to be rejected")
	}
}

func TestValidateShotImagePromptContainsNoDialogueAllowsNegativeConstraints(t *testing.T) {
	prompt := "A silent confrontation. 无台词、无对白、no dialogue, without spoken words."
	if err := validateShotImagePromptContainsNoDialogue(prompt, nil); err != nil {
		t.Fatalf("negative dialogue constraint was rejected: %v", err)
	}
	if err := validateShotImagePromptContainsNoDialogue("中景。台词：你终于来了。", nil); err == nil {
		t.Fatal("explicit dialogue metadata should be rejected")
	}
}

func TestValidateShotImagePromptProviderSafetyRejectsGraphicDetails(t *testing.T) {
	for _, prompt := range []string{
		"角色站在黏稠暗红血泊边缘，衣摆持续滴血。",
		"山风卷动角色的破损血袍。",
		"群雄被自爆的冲击光吞没，画面在巨响中毁灭。",
		"背景不得出现尸体、残肢、血腥人体细节。",
		"负面约束：开放伤口、流血、血液飞溅、大面积血污。",
		"A graphic gore scene with an exposed wound.",
	} {
		if err := validateShotImagePromptProviderSafety(prompt); err == nil {
			t.Fatalf("expected graphic prompt to be rejected: %s", prompt)
		}
	}
	if err := validateShotImagePromptProviderSafety("角色带有克制的战损污痕，战后山巅气氛肃杀。无令人不适的伤害细节。"); err != nil {
		t.Fatalf("non-graphic aftermath prompt was rejected: %v", err)
	}
}

func TestProviderSafeShotImageContextRewritesDestructiveTransitionLanguage(t *testing.T) {
	input := "方源自爆，光芒向外爆发并吞没群雄，画面在巨响中走向毁灭，负面约束不得出现人体残留。"
	got := providerSafeShotImageContext(input)
	if err := validateShotImagePromptProviderSafety(got); err != nil {
		t.Fatalf("rewritten transition remained unsafe: %v\n%s", err, got)
	}
	for _, forbidden := range []string{"自爆", "爆发", "吞没", "巨响", "毁灭", "人体残留"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("rewritten transition retained %q: %s", forbidden, got)
		}
	}
	for _, required := range []string{"光场转场", "绽放", "逐渐覆盖", "无声过渡"} {
		if !strings.Contains(got, required) {
			t.Fatalf("rewritten transition lost %q: %s", required, got)
		}
	}
}

func TestBuildReviewedShotImagePromptSanitizesLockedGraphicFacts(t *testing.T) {
	contextValue := shotImagePromptAgentContext{Shot: shotImagePromptShot{
		Visual:      "染血的衣摆掠过暗红血泊，山风卷动破损血袍。",
		Motion:      "血珠持续滴落。",
		AspectRatio: "16:9",
	}}
	prompt := buildReviewedShotImagePrompt(
		"角色带有克制的战损污痕，站在战后山巅。",
		"无尸体、残肢、血腥人体细节",
		contextValue,
	)
	if err := validateShotImagePromptProviderSafety(prompt); err != nil {
		t.Fatalf("sanitized prompt remained unsafe: %v\n%s", err, prompt)
	}
	for _, forbidden := range []string{"血泊", "滴血", "血珠", "血袍", "尸体", "骸骨", "残肢", "血腥人体细节"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt retained %q: %s", forbidden, prompt)
		}
	}
}

func TestReviewFeedbackDetectsExplicitUnresolvableConflict(t *testing.T) {
	feedback := shotImagePromptReviewFeedback{
		Issues:  agentJSONList{json.RawMessage(`{"severity":"error","resolvable":false,"message":"locked age conflicts with the selected reference"}`)},
		Summary: "locked facts conflict",
	}
	if !reviewFeedbackHasUnresolvableConflict(feedback) {
		t.Fatal("expected explicit resolvable=false conflict")
	}
}

func TestValidateShotImagePromptForCandidatesUsesUTF8BytesAndRejectsRawProfiles(t *testing.T) {
	candidates := []provider.GatewayModelConstraintCandidate{{
		ModelKey: "image-model",
		Prompt: provider.PromptLengthConstraint{
			MaxLength: 8,
			Unit:      provider.PromptLengthUnitUTF8Bytes,
		},
	}}
	if _, err := validateShotImagePromptForCandidates("蛊真人", candidates); err == nil {
		t.Fatal("expected nine-byte prompt to exceed eight-byte limit")
	}
	if _, err := validateShotImagePromptForCandidates(`{"baseClothing":"black robe"}`, nil); err == nil {
		t.Fatal("expected copied raw asset profile to be rejected")
	}
}

func TestCompactShotImagePromptAssetsRemovesRawProfileAndBoundsFields(t *testing.T) {
	values := compactShotImagePromptAssets([]ShotVideoPromptAsset{{
		AssetID:           "asset-1",
		AssetType:         "character",
		Name:              "方源",
		Description:       strings.Repeat("人", 700),
		Profile:           json.RawMessage(`{"forbiddenChanges":["everything"]}`),
		ConsistencyPrompt: strings.Repeat("衣", 1200),
		NegativePrompt:    strings.Repeat("禁", 500),
		Requirement: map[string]any{
			"action": strings.Repeat("动", 700),
			"prompt": strings.Repeat("提", 700),
		},
	}})
	if len(values) != 1 {
		t.Fatalf("values = %+v", values)
	}
	asset := values[0]
	if len(asset.Profile) != 0 {
		t.Fatalf("raw profile was retained: %s", asset.Profile)
	}
	if len([]rune(asset.Description)) != 500 || len([]rune(asset.ConsistencyPrompt)) != 1000 || len([]rune(asset.NegativePrompt)) != 300 {
		t.Fatalf("unexpected compact lengths: description=%d consistency=%d negative=%d", len([]rune(asset.Description)), len([]rune(asset.ConsistencyPrompt)), len([]rune(asset.NegativePrompt)))
	}
	if len([]rune(asset.Requirement["action"].(string))) != 500 || len([]rune(asset.Requirement["prompt"].(string))) != 500 {
		t.Fatalf("requirement was not bounded: %+v", asset.Requirement)
	}
}
