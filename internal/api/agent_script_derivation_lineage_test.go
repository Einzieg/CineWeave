package api

import (
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
)

func TestAgentScriptDerivationLineageProjectionUsesLatestResults(t *testing.T) {
	batch := commercepkg.ScriptDerivationBatch{
		Status:               "failed",
		RequestedCount:       5,
		FailedRetryableCount: 5,
		LineageResults: []commercepkg.ScriptDerivationLineageResult{
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
		},
	}

	projection := agentScriptDerivationLineageProjectionFromBatch(
		"root-batch", "retry-workflow", batch,
	)

	if projection.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", projection.Status)
	}
	if projection.TotalItems != 5 || projection.CompletedItems != 5 || projection.FailedItems != 0 {
		t.Fatalf("projection counts = %+v", projection)
	}
	if projection.LatestWorkflowRunID != "retry-workflow" {
		t.Fatalf("latest workflow run = %q", projection.LatestWorkflowRunID)
	}
}

func TestAgentScriptDerivationLineageProjectionPreservesPartialFailure(t *testing.T) {
	batch := commercepkg.ScriptDerivationBatch{
		LineageResults: []commercepkg.ScriptDerivationLineageResult{
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("failed_retryable"),
		},
	}

	projection := agentScriptDerivationLineageProjectionFromBatch("root-batch", "retry-workflow", batch)

	if projection.Status != "partial_succeeded" {
		t.Fatalf("status = %q, want partial_succeeded", projection.Status)
	}
	if projection.TotalItems != 5 || projection.CompletedItems != 4 || projection.FailedItems != 1 {
		t.Fatalf("projection counts = %+v", projection)
	}
}

func TestAgentScriptDerivationLineageProjectionKeepsActiveRetryRunning(t *testing.T) {
	batch := commercepkg.ScriptDerivationBatch{
		LineageResults: []commercepkg.ScriptDerivationLineageResult{
			scriptDerivationLineageResult("succeeded"),
			scriptDerivationLineageResult("reviewing"),
		},
	}

	projection := agentScriptDerivationLineageProjectionFromBatch("root-batch", "retry-workflow", batch)

	if projection.Status != "running" {
		t.Fatalf("status = %q, want running", projection.Status)
	}
	if projection.TotalItems != 2 || projection.CompletedItems != 1 || projection.FailedItems != 0 {
		t.Fatalf("projection counts = %+v", projection)
	}
}

func TestAgentScriptDerivationLineageProjectionFallsBackToBatchCounts(t *testing.T) {
	batch := commercepkg.ScriptDerivationBatch{
		Status:               "partial_succeeded",
		RequestedCount:       3,
		SucceededCount:       2,
		FailedRetryableCount: 1,
	}

	projection := agentScriptDerivationLineageProjectionFromBatch("root-batch", "root-workflow", batch)

	if projection.Status != "partial_succeeded" {
		t.Fatalf("status = %q, want partial_succeeded", projection.Status)
	}
	if projection.TotalItems != 3 || projection.CompletedItems != 2 || projection.FailedItems != 1 {
		t.Fatalf("projection counts = %+v", projection)
	}
}

func TestAgentGoalIsCommerceScriptDerivationOnly(t *testing.T) {
	tests := []struct {
		name string
		goal string
		want bool
	}{
		{
			name: "five scene variants",
			goal: "读取第1条广告脚本，把场景替换成5个不同版本，并创建5条独立的新广告脚本。",
			want: true,
		},
		{
			name: "derivation then videos",
			goal: "先创建五个场景变体，再为每条脚本批量生成视频。",
			want: false,
		},
		{
			name: "derivation then product update",
			goal: "创建五个场景变体，然后更新商品卖点。",
			want: false,
		},
		{
			name: "ordinary script update",
			goal: "修改第1条广告脚本的结尾。",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentGoalIsCommerceScriptDerivationOnly(tt.goal); got != tt.want {
				t.Fatalf("agentGoalIsCommerceScriptDerivationOnly(%q) = %v, want %v", tt.goal, got, tt.want)
			}
		})
	}
}

func scriptDerivationLineageResult(status string) commercepkg.ScriptDerivationLineageResult {
	return commercepkg.ScriptDerivationLineageResult{
		LatestResult: commercepkg.ScriptDerivationItem{Status: status},
	}
}
