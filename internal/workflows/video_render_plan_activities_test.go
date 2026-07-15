package workflows

import (
	"reflect"
	"strings"
	"testing"
)

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
	}}
	if !sameStoryboardDialogueContent(reviewed, current) {
		t.Fatal("timing provenance should not change dialogue ownership")
	}
	current[0].Text = "另一句台词"
	if sameStoryboardDialogueContent(reviewed, current) {
		t.Fatal("changed dialogue text must invalidate the reviewed prompt")
	}
}

func TestShouldReusePreparedShotVideoPlan(t *testing.T) {
	if !shouldReusePreparedShotVideoPlan(false) {
		t.Fatal("normal generation should reuse an active prepared plan")
	}
	if shouldReusePreparedShotVideoPlan(true) {
		t.Fatal("forced generation must create a new execution plan")
	}
}

func TestShouldTransitionWorkflowOnActivityFailure(t *testing.T) {
	tests := []struct {
		name  string
		input TextToStoryboardInput
		want  bool
	}{
		{name: "explicit workflow", input: TextToStoryboardInput{Prompt: "batch_legacy", FailureScope: workflowFailureScopeWorkflow}, want: true},
		{name: "explicit batch item", input: TextToStoryboardInput{Prompt: "video polling", FailureScope: workflowFailureScopeBatchItem}, want: false},
		{name: "legacy batch", input: TextToStoryboardInput{Prompt: "batch_generate_shot_videos"}, want: false},
		{name: "legacy workflow", input: TextToStoryboardInput{Prompt: "regenerate_shot_video"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldTransitionWorkflowOnActivityFailure(test.input); got != test.want {
				t.Fatalf("shouldTransitionWorkflowOnActivityFailure() = %v, want %v", got, test.want)
			}
		})
	}
}
