package workflows

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
)

func TestGatewayVideoDialogueSpansExcludesNonSpeechSystemAudio(t *testing.T) {
	shot := StoryboardShotRecord{
		StartTick: 90000,
		EndTick:   4 * 90000,
		Dialogue: []StoryboardDialogueLine{
			{Kind: "system", Text: "【音效：山风呼啸】", SpanStartTick: 90000, SpanEndTick: 2 * 90000},
			{Kind: "dialogue", Speaker: "方源", Text: "退后。", SpanStartTick: 2 * 90000, SpanEndTick: 3 * 90000},
		},
	}

	spans, err := gatewayVideoDialogueSpans(shot)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].Kind != "dialogue" || spans[0].Text != "退后。" {
		t.Fatalf("gateway dialogue spans = %+v", spans)
	}
}

func TestGatewayVideoDialogueSpansRejectsSpeakerlessDialogue(t *testing.T) {
	_, err := gatewayVideoDialogueSpans(StoryboardShotRecord{
		StartTick: 0,
		EndTick:   2 * 90000,
		Dialogue:  []StoryboardDialogueLine{{Kind: "dialogue", Text: "退后。", SpanStartTick: 0, SpanEndTick: 90000}},
	})
	var workflowErr workflowError
	if !errors.As(err, &workflowErr) || workflowErr.Code != provider.CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, provider.CodeStoryboardReplanRequired)
	}
}

func TestSplitReviewedShotVideoPromptKeepsSingleSegmentPrompt(t *testing.T) {
	want := "镜头缓慢推进，角色保持动作连续。"
	got, err := splitReviewedShotVideoPrompt("  "+want+"  ", 1)
	if err != nil {
		t.Fatalf("split prompt: %v", err)
	}
	if !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("prompts = %#v, want %#v", got, []string{want})
	}
}

func TestReviewedVideoSegmentPromptReadyRequiresExecutionHashOfExactPrompt(t *testing.T) {
	prompt := "镜头缓慢推进，角色保持动作连续。"
	if !reviewedVideoSegmentPromptReady(prompt, "approved", promptsvc.HashText(prompt)) {
		t.Fatal("approved prompt with an exact execution hash should be ready")
	}
	if reviewedVideoSegmentPromptReady(prompt, "approved", promptsvc.HashText("另一版提示词")) {
		t.Fatal("source or stale prompt hashes must not make a segment executable")
	}
	if reviewedVideoSegmentPromptReady(prompt, "reviewing", promptsvc.HashText(prompt)) {
		t.Fatal("unapproved prompt must not be executable")
	}
}

func TestSplitReviewedShotVideoPromptMapsReviewedSegments(t *testing.T) {
	prompt := "[片段 1/2]\n第一段保持角色从画面左侧进入。\n\n[片段 2/2]\n第二段承接上一段动作并完成对白。"
	want := []string{
		"第一段保持角色从画面左侧进入。",
		"第二段承接上一段动作并完成对白。",
	}
	got, err := splitReviewedShotVideoPrompt(prompt, 2)
	if err != nil {
		t.Fatalf("split prompt: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prompts = %#v, want %#v", got, want)
	}
}

func TestSplitReviewedShotVideoPromptRejectsUnsegmentedLongShot(t *testing.T) {
	_, err := splitReviewedShotVideoPrompt("只有一个整镜头提示词", 2)
	if err == nil || !strings.Contains(err.Error(), "segment headers") {
		t.Fatalf("error = %v, want missing segment headers", err)
	}
}

func TestSplitReviewedShotVideoPromptRejectsInconsistentSegmentOrder(t *testing.T) {
	prompt := "[片段 2/2]\n第二段\n\n[片段 1/2]\n第一段"
	_, err := splitReviewedShotVideoPrompt(prompt, 2)
	if err == nil || !strings.Contains(err.Error(), "ordinal") {
		t.Fatalf("error = %v, want ordinal error", err)
	}
}

func TestSameStoryboardDialogueContentIgnoresTimingButNotOwnership(t *testing.T) {
	reviewed := []StoryboardDialogueLine{{Speaker: "角色甲", Text: "保持原台词", Kind: "dialogue"}}
	current := []StoryboardDialogueLine{{
		Speaker: "角色甲", Text: "保持原台词", Kind: "dialogue",
		TimingUnitID: "unit-1", SpanStartTick: 90000, SpanEndTick: 180000,
	}, {Kind: "system", Text: "【音效：山风呼啸】", SpanStartTick: 0, SpanEndTick: 90000}}
	if !sameStoryboardDialogueContent(reviewed, current) {
		t.Fatal("timing provenance and non-speech sound cues should not change dialogue ownership")
	}
	current[0].Text = "另一句台词"
	if sameStoryboardDialogueContent(reviewed, current) {
		t.Fatal("changed dialogue text must invalidate the reviewed prompt")
	}
	current[0].Text = "保持原台词"
	reviewed = append(reviewed, StoryboardDialogueLine{Kind: "system", Text: "【音效：旧契约污染】"})
	if sameStoryboardDialogueContent(reviewed, current) {
		t.Fatal("legacy prompts that persisted sound cues as dialogue must be invalidated")
	}
}

func TestReconciledStoryboardDialogueTargetIDsExpandsAndDeduplicates(t *testing.T) {
	got := reconciledStoryboardDialogueTargetIDs(
		[]string{"shot-1", "shot-1"},
		[]string{"shot-2", "", "shot-1", "shot-3"},
	)
	want := []string{"shot-1", "shot-2", "shot-3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
}

func TestShouldTransitionWorkflowOnActivityFailure(t *testing.T) {
	tests := []struct {
		name  string
		input TextToStoryboardInput
		want  bool
	}{
		{name: "explicit workflow", input: TextToStoryboardInput{FailureScope: workflowFailureScopeWorkflow}, want: true},
		{name: "explicit batch item", input: TextToStoryboardInput{Prompt: "video polling", FailureScope: workflowFailureScopeBatchItem}, want: false},
		{name: "missing scope fails workflow", input: TextToStoryboardInput{}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldTransitionWorkflowOnActivityFailure(test.input); got != test.want {
				t.Fatalf("shouldTransitionWorkflowOnActivityFailure() = %v, want %v", got, test.want)
			}
		})
	}
}
