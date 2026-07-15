package provider

import (
	"testing"
	"time"
)

func TestVideoRenderPlanKeyForcedGenerationUsesWorkflowIdentity(t *testing.T) {
	selected := matchedVideoPlanCandidate{
		Candidate:    resolvedVideoPlanCandidate{Model: Model{ID: "video-model-1"}},
		Variant:      VideoGenerationVariant{VariantKey: "default"},
		SnapshotHash: "sha256:capabilities",
	}
	base := GatewayVideoPlanRequest{
		StoryboardShotID:    "shot-1",
		WorkflowRunID:       "workflow-1",
		Force:               true,
		TargetDurationTicks: 900000,
		TaskType:            "video.image_to_video",
		ReferenceMode:       "first_frame",
		AspectRatio:         "16:9",
		Resolution:          "720p",
		AudioStrategy:       "native_av",
		AudioRequirement:    "preferred",
		DialogueLanguage:    "zh-CN",
		HasDialogue:         true,
	}

	first, err := videoRenderPlanKey(base, 1, selected)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := videoRenderPlanKey(base, 1, selected)
	if err != nil {
		t.Fatal(err)
	}
	if retry != first {
		t.Fatalf("same forced workflow must remain idempotent: %s != %s", retry, first)
	}

	secondWorkflow := base
	secondWorkflow.WorkflowRunID = "workflow-2"
	second, err := videoRenderPlanKey(secondWorkflow, 1, selected)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("different forced workflows must not reuse the same render plan key")
	}

	nonForcedFirst := base
	nonForcedFirst.Force = false
	nonForcedSecond := secondWorkflow
	nonForcedSecond.Force = false
	firstDefault, err := videoRenderPlanKey(nonForcedFirst, 1, selected)
	if err != nil {
		t.Fatal(err)
	}
	secondDefault, err := videoRenderPlanKey(nonForcedSecond, 1, selected)
	if err != nil {
		t.Fatal(err)
	}
	if firstDefault != secondDefault {
		t.Fatal("workflow identity must not affect normal render plan reuse")
	}
}

func TestVideoRenderPlanTTLPolicySupportsLongRunningProduction(t *testing.T) {
	if defaultVideoRenderPlanTTL < 70*time.Minute {
		t.Fatalf("default render plan TTL %s does not cover a 70-minute production run", defaultVideoRenderPlanTTL)
	}
	if minimumReusableVideoPlanTTL <= 0 || minimumReusableVideoPlanTTL >= defaultVideoRenderPlanTTL {
		t.Fatalf("invalid reusable plan guard %s for default TTL %s", minimumReusableVideoPlanTTL, defaultVideoRenderPlanTTL)
	}
}
