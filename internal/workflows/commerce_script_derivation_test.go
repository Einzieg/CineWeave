package workflows

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestCommerceScriptDerivationBatchUsesBoundedChildrenAndKeepsPartialSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(
		CommerceScriptDerivationItemWorkflow,
		workflow.RegisterOptions{Name: CommerceScriptDerivationItemWorkflowName},
	)
	input := CommerceScriptDerivationBatchInput{
		OrganizationID: "organization", ProjectID: "project", BatchID: "batch",
		WorkflowRunID: "workflow", MaxConcurrency: 3,
	}
	itemIDs := []string{"item-1", "item-2", "item-3", "item-4", "item-5", "item-6"}
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptDerivationBatchInput) (CommerceScriptDerivationBatchSnapshot, error) {
		return CommerceScriptDerivationBatchSnapshot{
			Batch:   commerce.ScriptDerivationBatch{ID: input.BatchID},
			ItemIDs: itemIDs,
		}, nil
	}, activity.RegisterOptions{Name: StartCommerceScriptDerivationBatchActivity})

	var mu sync.Mutex
	active := 0
	maxActive := 0
	generateCalls := make(map[string]int)
	release := make(chan struct{})
	var releaseOnce sync.Once
	env.RegisterActivityWithOptions(func(_ context.Context, item CommerceScriptDerivationItemInput) (CommerceScriptDerivationItemSnapshot, error) {
		return testCommerceScriptDerivationSnapshot(item.ItemID), nil
	}, activity.RegisterOptions{Name: LoadCommerceScriptDerivationItemActivity})
	env.RegisterActivityWithOptions(func(ctx context.Context, call CommerceScriptDerivationAgentInput) (CommerceScriptDerivationAgentOutput, error) {
		if call.Phase == "generate" {
			mu.Lock()
			active++
			generateCalls[call.WorkflowInput.ItemID]++
			if active > maxActive {
				maxActive = active
			}
			if active >= input.MaxConcurrency {
				releaseOnce.Do(func() { close(release) })
			}
			mu.Unlock()
			select {
			case <-release:
			case <-time.After(time.Second):
				return CommerceScriptDerivationAgentOutput{}, errors.New("derivation child workflows did not overlap")
			case <-ctx.Done():
				return CommerceScriptDerivationAgentOutput{}, ctx.Err()
			}
			time.Sleep(15 * time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			if call.WorkflowInput.ItemID == "item-2" {
				return CommerceScriptDerivationAgentOutput{}, errors.New("temporary provider failure")
			}
			return CommerceScriptDerivationAgentOutput{
				RawOutput: fmt.Sprintf(
					`{"contractVersion":"commerce-script-derivation/v1","title":"%s","content":"独立广告脚本"}`,
					call.WorkflowInput.ItemID,
				),
			}, nil
		}
		return CommerceScriptDerivationAgentOutput{
			RawOutput: `{"contractVersion":"commerce-script-derivation-review/v1","decision":"approve","issues":[],"feedback":""}`,
		}, nil
	}, activity.RegisterOptions{Name: CallCommerceScriptDerivationAgentActivity})
	env.RegisterActivityWithOptions(func(_ context.Context, commit CommerceScriptDerivationCommitInput) (CommerceScriptDerivationItemOutput, error) {
		return CommerceScriptDerivationItemOutput{
			ItemID: commit.WorkflowInput.ItemID, Status: "succeeded",
			OutputScriptUnitID:    "script-" + commit.WorkflowInput.ItemID,
			OutputScriptVersionID: "version-" + commit.WorkflowInput.ItemID,
		}, nil
	}, activity.RegisterOptions{Name: CommitCommerceScriptDerivationItemActivity})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptDerivationFailureInput) error {
		return nil
	}, activity.RegisterOptions{Name: FailCommerceScriptDerivationItemActivity})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptDerivationBatchInput) (CommerceScriptDerivationBatchOutput, error) {
		return CommerceScriptDerivationBatchOutput{
			BatchID: input.BatchID, Status: "partial_succeeded",
			RequestedCount: 6, SucceededCount: 5, FailedRetryableCount: 1,
		}, nil
	}, activity.RegisterOptions{Name: FinalizeCommerceScriptDerivationBatchActivity})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptDerivationBatchInput) error {
		return nil
	}, activity.RegisterOptions{Name: CancelCommerceScriptDerivationBatchActivity})

	env.ExecuteWorkflow(CommerceScriptDerivationBatchWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceScriptDerivationBatchOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "partial_succeeded", output.Status)
	require.Equal(t, 5, output.SucceededCount)
	require.Equal(t, 1, output.FailedRetryableCount)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, input.MaxConcurrency, maxActive)
	require.Equal(t, 3, generateCalls["item-2"], "provider activity must stop at its configured retry limit")
}

func TestCommerceScriptDerivationItemStopsAfterThreeReviewRounds(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := CommerceScriptDerivationItemInput{
		OrganizationID: "organization", ProjectID: "project", BatchID: "batch",
		ItemID: "item", WorkflowRunID: "workflow",
	}
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptDerivationItemInput) (CommerceScriptDerivationItemSnapshot, error) {
		return testCommerceScriptDerivationSnapshot(input.ItemID), nil
	}, activity.RegisterOptions{Name: LoadCommerceScriptDerivationItemActivity})
	phaseCalls := make(map[string]int)
	env.RegisterActivityWithOptions(func(_ context.Context, call CommerceScriptDerivationAgentInput) (CommerceScriptDerivationAgentOutput, error) {
		phaseCalls[call.Phase]++
		switch call.Phase {
		case "review":
			return CommerceScriptDerivationAgentOutput{
				RawOutput: `{"contractVersion":"commerce-script-derivation-review/v1","decision":"revise","issues":[{"code":"PRODUCT_FACT_CHANGED","message":"商品事实被修改","suggestion":"恢复商品事实"}],"feedback":"恢复商品事实"}`,
			}, nil
		default:
			return CommerceScriptDerivationAgentOutput{
				RawOutput: `{"contractVersion":"commerce-script-derivation/v1","title":"场景变体","content":"一条完整广告脚本"}`,
			}, nil
		}
	}, activity.RegisterOptions{Name: CallCommerceScriptDerivationAgentActivity})
	var failure CommerceScriptDerivationFailureInput
	env.RegisterActivityWithOptions(func(_ context.Context, input CommerceScriptDerivationFailureInput) error {
		failure = input
		return nil
	}, activity.RegisterOptions{Name: FailCommerceScriptDerivationItemActivity})

	env.ExecuteWorkflow(CommerceScriptDerivationItemWorkflow, input)

	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 1, phaseCalls["generate"])
	require.Equal(t, 3, phaseCalls["review"])
	require.Equal(t, 2, phaseCalls["revise"])
	require.False(t, failure.Retryable)
	require.Equal(t, commerce.CodeScriptDerivationInvalid, failure.ErrorCode)
}

func TestCommerceScriptDerivationContractsRejectArraysAndUnknownReviewFields(t *testing.T) {
	var candidate CommerceScriptCandidate
	require.Error(t, decodeCommerceScriptCandidate(
		`[{"contractVersion":"commerce-script-derivation/v1","title":"A","content":"B"}]`,
		&candidate,
	))
	var review CommerceScriptReview
	require.Error(t, decodeCommerceScriptReview(
		`{"contractVersion":"commerce-script-derivation-review/v1","decision":"approve","issues":[],"feedback":"","score":100}`,
		&review,
	))
}

func TestScriptDerivationNodeRunUsesWorkflowExecutionGeneration(t *testing.T) {
	snapshot := testCommerceScriptDerivationSnapshot("retry-item")
	snapshot.Attempt.AttemptNo = 2
	input := scriptDerivationNodeRunInput(
		CommerceScriptDerivationAgentInput{
			WorkflowInput: CommerceScriptDerivationItemInput{
				OrganizationID: snapshot.Batch.OrganizationID,
				ProjectID:      snapshot.Batch.ProjectID,
				BatchID:        snapshot.Batch.ID,
				ItemID:         snapshot.Item.ID,
				WorkflowRunID:  "retry-workflow",
			},
			Snapshot: snapshot,
			Phase:    "generate",
			Round:    1,
		},
		promptsvc.RenderedPrompt{
			PromptVersionID: "prompt-version",
			RenderedHash:    "sha256:" + stringsOf("f", 64),
		},
	)

	require.Zero(
		t, input.AttemptGeneration,
		"business attempt_no must not be used as workflow execution generation",
	)
	require.Equal(t, "retry-workflow", input.WorkflowRunID)
	require.Equal(
		t, "commerce-script-derivation-retry-item-01-generate", input.NodeKey,
	)
}

func testCommerceScriptDerivationSnapshot(itemID string) CommerceScriptDerivationItemSnapshot {
	modelBindingID := "model-binding"
	providerModelID := "provider-model"
	workflowRunID := "workflow"
	return CommerceScriptDerivationItemSnapshot{
		Batch: commerce.ScriptDerivationBatch{
			ID: "batch", OrganizationID: "organization", ProjectID: "project",
			ProductID: "product", SourceContentSnapshot: "源广告脚本",
			SourceContentHash: stringsOf("a", 64), ProductVersionID: "product-version",
			ProductSnapshotHash:    stringsOf("b", 64),
			ProductionGenerationID: "generation", VideoProductionBindingID: "binding",
			VideoProductionBindingRevision: 1, ProductionConfigurationHash: stringsOf("c", 64),
			ScriptModelProfileKey: "script_agent_default",
			ModelProfileBindingID: &modelBindingID, ModelProfileBindingRevision: 1,
			ProviderModelID: &providerModelID, RoutingSnapshotHash: stringsOf("d", 64),
			Dimension: "scene", Instruction: "只替换场景",
			Preserve: []string{"product_facts"}, WorkflowRunID: &workflowRunID,
		},
		Item: commerce.ScriptDerivationItem{
			ID: itemID, BatchID: "batch", VariationKey: itemID,
			VariationLabel: itemID, VariationBrief: "不同场景",
			Status: "running", InputOrdinal: 1,
		},
		Attempt: commerce.ScriptDerivationAttempt{
			ID: "attempt-" + itemID, ItemID: itemID, BatchID: "batch",
			AttemptNo: 1, Status: "generating",
		},
		ProductVersion: commerce.ProductVersion{
			ID: "product-version", ProductID: "product", Name: "商品",
			FactsHash: stringsOf("e", 64),
		},
	}
}

func stringsOf(value string, count int) string {
	result := ""
	for len(result) < count {
		result += value
	}
	return result[:count]
}
