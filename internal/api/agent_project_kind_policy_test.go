package api

import (
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
)

func TestUnavailableAgentToolRejectsCrossKindBeforeExecution(t *testing.T) {
	commerceProject := Project{ProjectKind: commercepkg.ProjectKindCommerceVideo}
	result := unavailableAgentToolResult(commerceProject, "storyboard.update_shot")
	if result.ErrorCode != "PROJECT_KIND_MISMATCH" || result.Status != "failed" {
		t.Fatalf("cross-kind result = %+v", result)
	}
	if result.Data != nil || len(result.ChildWorkflowRunIDs) != 0 {
		t.Fatalf("cross-kind rejection must not contain execution output: %+v", result)
	}

	narrativeProject := Project{ProjectKind: commercepkg.ProjectKindNarrative}
	result = unavailableAgentToolResult(narrativeProject, "commerce.video.generate")
	if result.ErrorCode != "PROJECT_KIND_MISMATCH" {
		t.Fatalf("narrative cross-kind result = %+v", result)
	}

	result = unavailableAgentToolResult(narrativeProject, "unknown.tool")
	if result.ErrorCode != "UNKNOWN_TOOL" {
		t.Fatalf("unknown tool result = %+v", result)
	}
}
