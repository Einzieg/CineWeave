package workflows

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestParseReviewedVideoPrompt(t *testing.T) {
	reviewed, err := parseReviewedVideoPrompt("```json\n"+`{"approved":true,"finalPrompt":"Slow push toward the subject.","issues":["too long"],"changes":["condensed"]}`+"\n```", generatedVideoPrompt{})
	if err != nil {
		t.Fatalf("parseReviewedVideoPrompt: %v", err)
	}
	if !reviewed.Approved || reviewed.FinalPrompt != "Slow push toward the subject." {
		t.Fatalf("reviewed = %+v", reviewed)
	}
}

func TestValidateVideoPromptDialogueRequiresVerbatimChinese(t *testing.T) {
	contextValue := shotVideoPromptAgentContext{
		Script: shotVideoPromptScript{Content: "分集剧本文本可能使用不同的 Markdown 排版。"},
		Shot: shotVideoPromptShot{
			Title: "方源开口",
			ScriptDialogue: []StoryboardDialogueLine{{
				Speaker: "方源",
				Text:    "青山落日，秋月春风。",
				Kind:    "dialogue",
			}},
		},
	}
	if err := validateVideoPromptDialogue(`Fang Yuan speaks in Chinese: 方源：“青山落日，秋月春风。”`, contextValue); err != nil {
		t.Fatalf("verbatim Chinese dialogue rejected: %v", err)
	}
	if err := validateVideoPromptDialogue("Fang Yuan says: Green mountains and sunset.", contextValue); err == nil || !strings.Contains(err.Error(), "did not preserve") {
		t.Fatalf("expected omitted dialogue error, got %v", err)
	}
}

func TestValidateVideoPromptDialogueAllowsEmptyTimingAssignmentDespiteStaleSpeakingCues(t *testing.T) {
	contextValue := shotVideoPromptAgentContext{
		Script: shotVideoPromptScript{Content: "**方源**：\n这是五百年前？"},
		Shot: shotVideoPromptShot{
			Title:          "方源低声确认",
			Visual:         "方源张口说话",
			ExistingPrompt: "Fang Yuan speaks in Chinese: 方源：“这是五百年前？”",
		},
	}
	if err := validateVideoPromptDialogue("Fang Yuan silently looks out the window, lips closed.", contextValue); err != nil {
		t.Fatalf("empty authoritative dialogue should not be inferred from stale cues: %v", err)
	}
}

func TestValidateVideoPromptDialogueRejectsUnassignedScriptDialogue(t *testing.T) {
	contextValue := shotVideoPromptAgentContext{
		Script: shotVideoPromptScript{Content: "**方源**：\n这是五百年前？"},
		Shot:   shotVideoPromptShot{Title: "雨夜窗前"},
	}
	if err := validateVideoPromptDialogue("方源低声说：这是五百年前？", contextValue); err == nil || !strings.Contains(err.Error(), "outside the authoritative shot timing") {
		t.Fatalf("expected unassigned dialogue error, got %v", err)
	}
}

func TestValidateVideoPromptDialogueMatchesFullyBoldMultilineScript(t *testing.T) {
	contextValue := shotVideoPromptAgentContext{
		Script: shotVideoPromptScript{Content: `**方源（轻笑，望着远山）：**
青山落日，秋月春风。
当真是……朝如青丝暮成雪，是非成败转头空。`},
		Shot: shotVideoPromptShot{ScriptDialogue: []StoryboardDialogueLine{{
			Speaker:  "方源",
			Delivery: "轻笑，望着远山",
			Text:     "青山落日，秋月春风。\n当真是……朝如青丝暮成雪，是非成败转头空。",
			Kind:     "dialogue",
		}}},
	}
	prompt := "方源自然说出：青山落日，秋月春风。 当真是……朝如青丝暮成雪，是非成败转头空。"
	if err := validateVideoPromptDialogue(prompt, contextValue); err != nil {
		t.Fatalf("authoritative multiline dialogue rejected: %v", err)
	}
}

func TestComposeAuthoritativeVideoPromptInjectsStructuredAudio(t *testing.T) {
	dialogue := []StoryboardDialogueLine{
		{Speaker: "方源", Text: "但我知道。\n这绝不是梦。", Delivery: "低声", Kind: "dialogue"},
		{Text: "一声清越蝉鸣。", Kind: "system"},
	}
	prompt := composeAuthoritativeVideoPrompt("Slowly pull back from the window.", dialogue)
	for _, expected := range []string{
		"<cineweave_authoritative_speech_timeline>",
		"<cineweave_non_speech_sound_timeline>",
		`"speaker":"方源"`,
		`"text":"但我知道。\n这绝不是梦。"`,
		`"kind":"system"`,
		`"description":"一声清越蝉鸣。"`,
		"never spoken language",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("composed prompt misses %q: %s", expected, prompt)
		}
	}
	spoken, found, err := extractAuthoritativeVideoPromptAudio(prompt)
	if err != nil || !found || len(spoken) != 1 || spoken[0].Kind != "dialogue" {
		t.Fatalf("spoken timeline = %+v, found=%v, err=%v", spoken, found, err)
	}
	sounds, found, err := extractAuthoritativeVideoPromptSound(prompt)
	if err != nil || !found || len(sounds) != 1 || sounds[0].Description != "一声清越蝉鸣。" {
		t.Fatalf("sound timeline = %+v, found=%v, err=%v", sounds, found, err)
	}
}

func TestStoryboardAudioCuesForSegmentClipsAndKeepsContinuationFlags(t *testing.T) {
	cues := []StoryboardDialogueLine{
		{Kind: "system", Text: "【音效：山风呼啸】", SpanStartTick: 10, SpanEndTick: 30},
		{Kind: "system", Text: "【音效：兵刃碰撞】", SpanStartTick: 30, SpanEndTick: 50},
		{Kind: "dialogue", Speaker: "方源", Text: "退后。", SpanStartTick: 20, SpanEndTick: 25},
	}

	got := storyboardAudioCuesForSegment(cues, 20, 40)
	if len(got) != 2 {
		t.Fatalf("segment sound cues = %+v, want 2 non-speech cues", got)
	}
	if got[0].SpanStartTick != 0 || got[0].SpanEndTick != 10 || !got[0].ContinuesFromPrevious || got[0].ContinuesToNext {
		t.Fatalf("first clipped sound cue = %+v", got[0])
	}
	if got[1].SpanStartTick != 10 || got[1].SpanEndTick != 20 || got[1].ContinuesFromPrevious || !got[1].ContinuesToNext {
		t.Fatalf("second clipped sound cue = %+v", got[1])
	}
}

func TestRequestStructuredVideoPromptDoesNotRetryDeterministicValidation(t *testing.T) {
	calls := 0
	request := provider.GatewayTextRequest{Input: json.RawMessage(`{"prompt":"return JSON"}`)}
	_, _, err := requestStructuredVideoPrompt(
		context.Background(),
		NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1},
		request,
		func(_ context.Context, _ NodeExecution, _ provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
			calls++
			return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: `{}`}}, nil
		},
		func(string) (generatedVideoPrompt, error) {
			return generatedVideoPrompt{}, workflowError{Code: provider.CodeInvalidRequest, Message: "deterministic dialogue validation failed"}
		},
	)
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want one deterministic validation attempt", err, calls)
	}
}

func TestRequestStructuredVideoPromptRetriesMalformedOutput(t *testing.T) {
	calls := 0
	request := provider.GatewayTextRequest{Input: json.RawMessage(`{"prompt":"return JSON","maxOutputTokens":10}`)}
	parsed, response, err := requestStructuredVideoPrompt(
		context.Background(),
		NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1},
		request,
		func(_ context.Context, _ NodeExecution, req provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
			calls++
			if calls == 1 {
				return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: `{"prompt":"truncated`}}, nil
			}
			var input map[string]any
			if err := json.Unmarshal(req.Input, &input); err != nil {
				t.Fatalf("retry input: %v", err)
			}
			if !strings.Contains(input["prompt"].(string), "结构化输出纠错") || int(input["maxOutputTokens"].(float64)) != structuredVideoPromptOutputTokens {
				t.Fatalf("retry input = %+v", input)
			}
			return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: `{"prompt":"complete","negativePrompt":""}`}}, nil
		},
		parseGeneratedVideoPrompt,
	)
	if err != nil {
		t.Fatalf("requestStructuredVideoPrompt: %v", err)
	}
	if calls != 2 || parsed.Prompt != "complete" || response.Output.Text == "" {
		t.Fatalf("calls=%d parsed=%+v response=%+v", calls, parsed, response)
	}
}

func TestRequestStructuredVideoPromptRetriesTransientProviderFailure(t *testing.T) {
	calls := 0
	parsed, _, err := requestStructuredVideoPrompt(
		context.Background(),
		NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1},
		provider.GatewayTextRequest{Input: json.RawMessage(`{"prompt":"return JSON"}`)},
		func(_ context.Context, _ NodeExecution, _ provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
			calls++
			if calls < structuredVideoPromptAttempts {
				return provider.GatewayTextResponse{}, &provider.StandardErrorError{Standard: provider.StandardError{
					Code: provider.CodeUpstreamStreamTruncated, Message: "stream truncated", Retryable: true,
				}}
			}
			return provider.GatewayTextResponse{Output: provider.GatewayTextOutput{Text: `{"prompt":"complete"}`}}, nil
		},
		parseGeneratedVideoPrompt,
	)
	if err != nil || calls != structuredVideoPromptAttempts || parsed.Prompt != "complete" {
		t.Fatalf("err=%v calls=%d parsed=%+v", err, calls, parsed)
	}
}

func TestRequestStructuredVideoPromptDoesNotRetryPermanentProviderFailure(t *testing.T) {
	calls := 0
	_, _, err := requestStructuredVideoPrompt(
		context.Background(),
		NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1},
		provider.GatewayTextRequest{Input: json.RawMessage(`{"prompt":"return JSON"}`)},
		func(_ context.Context, _ NodeExecution, _ provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
			calls++
			return provider.GatewayTextResponse{}, &provider.StandardErrorError{Standard: provider.StandardError{
				Code: provider.CodeInvalidRequest, Message: "invalid", Retryable: false,
			}}
		},
		parseGeneratedVideoPrompt,
	)
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, want one permanent failure", err, calls)
	}
}

func TestValidateVideoPromptForCandidatesUsesUTF8Bytes(t *testing.T) {
	candidates := []provider.GatewayModelConstraintCandidate{{
		ModelKey: "grok-imagine-video-1.5-preview",
		Prompt: provider.PromptLengthConstraint{
			MaxLength: 8,
			Unit:      provider.PromptLengthUnitUTF8Bytes,
		},
	}}
	if _, err := validateVideoPromptForCandidates("蛊真人", candidates); err == nil {
		t.Fatal("expected nine-byte prompt to exceed eight-byte limit")
	}
	measurements, err := validateVideoPromptForCandidates("Grok", candidates)
	if err != nil {
		t.Fatalf("validateVideoPromptForCandidates: %v", err)
	}
	if measurements["grok-imagine-video-1.5-preview:utf8_bytes"] != 4 {
		t.Fatalf("measurements = %+v", measurements)
	}
}

func TestVideoPromptModelContextsReserveSafetyMargin(t *testing.T) {
	contexts := videoPromptModelContexts([]provider.GatewayModelConstraintCandidate{{
		ModelKey: "grok-imagine-video-1.5-preview",
		Prompt:   provider.PromptLengthConstraint{MaxLength: 4096, Unit: provider.PromptLengthUnitUTF8Bytes},
	}})
	if len(contexts) != 1 || contexts[0].TargetLength != 3481 {
		t.Fatalf("contexts = %+v", contexts)
	}
}

func TestVideoCandidatesSupportNativeAudioUsesStoryboardSheetContract(t *testing.T) {
	candidates := []provider.GatewayModelConstraintCandidate{{
		References: provider.ReferenceConstraint{
			Supported: true, SupportsStoryboardSheetReference: true,
			InputContracts: []string{provider.VideoInputContractStoryboardSheetReference},
		},
		NativeAudio: provider.NativeAudioConstraint{
			Support: provider.VideoSupportTrue, SupportsDialogue: true, SupportsLipSync: true,
		},
	}}
	if !videoCandidatesSupportNativeAudio(candidates, videoproduction.InputContractStoryboardSheetReference, true) {
		t.Fatal("storyboard sheet native-audio candidate was rejected because it has no first-frame slot")
	}
	if videoCandidatesSupportNativeAudio(candidates, videoproduction.InputContractFirstFrame, true) {
		t.Fatal("storyboard sheet candidate was accepted for a first-frame contract")
	}
}

func TestApplyVideoPromptAudioRuntimeContractPreservesEpisodeContext(t *testing.T) {
	rendered := applyVideoPromptAudioRuntimeContract(promptsvc.RenderedPrompt{
		RenderedText: "完整分集剧本：WHOLE_EPISODE_CONTEXT",
		RenderedHash: "sha256:old",
		Source:       "system_active",
	})
	if !strings.Contains(rendered.RenderedText, "WHOLE_EPISODE_CONTEXT") ||
		!strings.Contains(rendered.RenderedText, "complete episode script remains required") ||
		!strings.Contains(rendered.RenderedText, "Only shot.scriptDialogue defines this shot's audio") {
		t.Fatalf("runtime contract = %s", rendered.RenderedText)
	}
	if rendered.RenderedHash == "sha256:old" || rendered.Source != "system_active+deterministic_audio_v2" {
		t.Fatalf("rendered provenance = %+v", rendered)
	}
}

func TestVideoPromptContextDurationUsesIntegerShotAndProviderSegmentDuration(t *testing.T) {
	shot := StoryboardShotRecord{Duration: 5.67}
	if got := videoPromptContextDuration(PrepareShotVideoPromptInput{}, shot); got != 6 {
		t.Fatalf("shot prompt duration = %v, want 6", got)
	}
	segment := PrepareShotVideoPromptInput{RenderSegmentID: "segment-1", RequestedDuration: 8, Duration: 8}
	if got := videoPromptContextDuration(segment, shot); got != 8 {
		t.Fatalf("segment prompt duration = %v, want 8", got)
	}
}

func TestComposeVideoExecutionContractUsesPreparedSegmentDuration(t *testing.T) {
	prompt, err := composeVideoExecutionContract("镜头从首帧开始缓慢推进。", shotVideoPromptShot{
		RenderSegmentID: "segment-1", RequestedDuration: 8,
		SegmentIndex: 0, SegmentCount: 2, SegmentStartTick: 0, SegmentEndTick: 7 * 90000,
	})
	if err != nil {
		t.Fatalf("composeVideoExecutionContract: %v", err)
	}
	for _, want := range []string{"requestedDurationSeconds: 8", "segmentIndex: 0", "segmentCount: 2", "plannedEndTick: 630000"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution contract misses %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "8.0") {
		t.Fatalf("execution contract contains a decimal provider duration: %s", prompt)
	}
}

func TestSanitizeVideoPromptVisualSafetyPreservesDialogueInAuthoritativeTrack(t *testing.T) {
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "纵是血泊，我也向前。", Kind: "dialogue"}}
	visual := sanitizeVideoPromptVisualSafety("方源站在血泊中说：纵是血泊，我也向前。", dialogue)
	if strings.Contains(visual, "血泊") || strings.Contains(visual, dialogue[0].Text) {
		t.Fatalf("visual safety normalization did not remove graphic or duplicated dialogue text: %s", visual)
	}
	finalPrompt := composeAuthoritativeVideoPrompt(visual, dialogue)
	if !strings.Contains(finalPrompt, `"text":"纵是血泊，我也向前。"`) {
		t.Fatalf("authoritative dialogue was not preserved verbatim: %s", finalPrompt)
	}
}

func TestComposeVideoPromptReviewCorrectionCarriesReviewerFeedback(t *testing.T) {
	prompt := composeVideoPromptReviewCorrection(
		"BASE_GENERATION_CONTRACT",
		generatedVideoPrompt{Prompt: "旧候选提示词", SourceAnchors: []string{"planned_first_frame"}},
		reviewedVideoPrompt{Issues: agentJSONListFromStrings("镜头运动与机位冲突"), Changes: agentJSONListFromStrings("保持固定机位")},
		2,
	)
	for _, want := range []string{
		"BASE_GENERATION_CONTRACT",
		"旧候选提示词",
		"镜头运动与机位冲突",
		"保持固定机位",
		`"round":2`,
		"不得原样返回上一版",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("correction prompt misses %q: %s", want, prompt)
		}
	}
}

func TestParseReviewedVideoPromptAcceptsSingletonReviewMessages(t *testing.T) {
	reviewed, err := parseReviewedVideoPrompt(`{
		"approved": false,
		"finalPrompt": "修订候选",
		"issues": "镜头运动与机位冲突",
		"changes": {"field":"camera","message":"保持固定机位"}
	}`, generatedVideoPrompt{})
	if err != nil {
		t.Fatalf("parseReviewedVideoPrompt: %v", err)
	}
	if got := strings.Join(agentJSONListMessages(reviewed.Issues), "；"); got != "镜头运动与机位冲突" {
		t.Fatalf("issues = %q", got)
	}
	if got := strings.Join(agentJSONListMessages(reviewed.Changes), "；"); !strings.Contains(got, "保持固定机位") {
		t.Fatalf("changes = %q", got)
	}
}

func TestVideoPromptReviewRoundNodeKeyKeepsFirstRoundStable(t *testing.T) {
	if got := videoPromptReviewRoundNodeKey("review_shot_10", 1); got != "review_shot_10" {
		t.Fatalf("first round node key = %q", got)
	}
	if got := videoPromptReviewRoundNodeKey("review_shot_10", 3); got != "review_shot_10_review_round_3" {
		t.Fatalf("third round node key = %q", got)
	}
}

func TestScopeVideoPromptContextPlanToSegmentRemovesOtherDialogue(t *testing.T) {
	plan := videoproduction.PromptContextPlan{
		EpisodeContinuityDigest: "本集连续性摘要",
		CurrentSceneScript:      "整场剧本文本",
		CurrentShotState: videoproduction.ShotState{
			Scene:           videoproduction.SceneState{AssetID: "11111111-1111-4111-8111-111111111111"},
			Camera:          videoproduction.CameraState{ShotSize: "medium", Angle: "eye_level", AxisSide: "A", LensIntent: "normal", Movement: "static"},
			Action:          videoproduction.ActionState{Entry: "人物站定", Exit: "人物保持站姿"},
			ScreenDirection: "static",
		},
		ModelContextLimit: 12000,
		ModelPromptLimit:  4096,
		VerbatimDialogueCues: []videoproduction.DialogueCue{
			{TimingUnitID: "cue-1", Speaker: "甲", Text: "第一段。", StartTick: 0, EndTick: 90000},
			{TimingUnitID: "cue-2", Speaker: "乙", Text: "第二段。", StartTick: 90000, EndTick: 180000},
		},
	}
	scoped, err := scopeVideoPromptContextPlanToSegment(plan, []StoryboardDialogueLine{{
		TimingUnitID: "cue-2", Speaker: "乙", Text: "第二段。", Kind: "dialogue", SpanStartTick: 0, SpanEndTick: 90000,
	}})
	if err != nil {
		t.Fatalf("scopeVideoPromptContextPlanToSegment: %v", err)
	}
	if len(scoped.VerbatimDialogueCues) != 1 || scoped.VerbatimDialogueCues[0].Text != "第二段。" || scoped.VerbatimDialogueCues[0].StartTick != 0 {
		t.Fatalf("scoped cues = %+v", scoped.VerbatimDialogueCues)
	}
}

func TestVideoPromptDialogueTimingUsesConfiguredTimebase(t *testing.T) {
	timing := videoPromptDialogueTiming([]StoryboardDialogueLine{{
		TimingUnitID: "cue-1", Speaker: "方源", Text: "测试对白", Kind: "dialogue", SpanStartTick: 4155000, SpanEndTick: 5403750,
	}}, 90000)
	if len(timing) != 1 || math.Abs(timing[0].DurationSeconds-13.875) > 0.0001 {
		t.Fatalf("timing = %+v", timing)
	}
}

func TestAuthoritativeVideoPromptDialogueLinesReplaceModelOutputForSegment(t *testing.T) {
	segmentLines := []StoryboardDialogueLine{{
		TimingUnitID: "cue-1:part:1", Speaker: "正道群雄", Text: "这里只属于第一片段，", Kind: "dialogue",
		SpanStartTick: 0, SpanEndTick: 14 * 90000, ContinuesToNext: true,
	}}
	got := authoritativeVideoPromptDialogueLines(segmentLines)
	segmentLines[0].Text = "调用方随后发生修改"
	if len(got) != 1 || got[0].Text != "这里只属于第一片段，" || !got[0].ContinuesToNext {
		t.Fatalf("authoritative segment dialogue = %+v", got)
	}
}

func TestStoredVideoAudioCueSpeakerDoesNotTurnSystemAudioIntoNarration(t *testing.T) {
	if got := storedVideoAudioCueSpeaker("", "system"); got != "系统音频" {
		t.Fatalf("system audio speaker = %q", got)
	}
	if got := normalizeVideoAudioCueKind("system"); got != "system" {
		t.Fatalf("system audio kind = %q", got)
	}
	if got := storedVideoAudioCueSpeaker("", "voiceover"); got != "旁白" {
		t.Fatalf("voiceover speaker = %q", got)
	}
}

func TestConstrainVideoVisualPromptReservesExecutionAndAudioBudget(t *testing.T) {
	shot := shotVideoPromptShot{
		RenderSegmentID: "segment-1", RequestedDuration: 8,
		SegmentIndex: 0, SegmentCount: 1, SegmentStartTick: 0, SegmentEndTick: 7 * 90000,
	}
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "青山落日。", Kind: "dialogue", SpanStartTick: 0, SpanEndTick: 3 * 90000}}
	visual, err := constrainVideoVisualPrompt(strings.Repeat("保持首帧身份与构图，镜头缓慢推进。", 120), []provider.GatewayModelConstraintCandidate{{
		ModelKey: "grok-imagine-video-1.5-preview",
		Prompt:   provider.PromptLengthConstraint{MaxLength: 1600, Unit: provider.PromptLengthUnitUTF8Bytes},
	}}, shot, dialogue)
	if err != nil {
		t.Fatalf("constrainVideoVisualPrompt: %v", err)
	}
	execution, err := composeVideoExecutionContract(visual, shot)
	if err != nil {
		t.Fatal(err)
	}
	finalPrompt := composeAuthoritativeVideoPrompt(execution, dialogue)
	if got := provider.MeasurePromptLength(finalPrompt, provider.PromptLengthUnitUTF8Bytes); got > 1600 {
		t.Fatalf("final prompt length = %d", got)
	}
	if !strings.HasPrefix(visual, "保持首帧身份与构图") {
		t.Fatalf("visual prefix was not preserved: %s", visual)
	}
}
