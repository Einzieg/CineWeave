package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
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
		"<cineweave_authoritative_audio_timeline>",
		`"speaker":"方源"`,
		`"text":"但我知道。\n这绝不是梦。"`,
		`"kind":"system"`,
		"Earlier audio or dialogue instructions are non-authoritative",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("composed prompt misses %q: %s", expected, prompt)
		}
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
