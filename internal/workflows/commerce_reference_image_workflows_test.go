package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceReferenceImageBatchWorkflowUsesBoundedConcurrencyAndKeepsPartialSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := testCommerceReferenceImageBatchInput(3, []string{"shot-1", "shot-2", "shot-3", "shot-4", "shot-5", "shot-6"})

	var mu sync.Mutex
	active := 0
	maxActive := 0
	calls := make(map[string]int)
	release := make(chan struct{})
	var releaseOnce sync.Once
	env.RegisterActivityWithOptions(func(ctx context.Context, _ CommerceReferenceImageBatchInput, shotID string) (CommerceReferenceImageItemOutput, error) {
		mu.Lock()
		active++
		calls[shotID]++
		if active > maxActive {
			maxActive = active
		}
		if active >= input.Concurrency {
			releaseOnce.Do(func() { close(release) })
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(time.Second):
			return CommerceReferenceImageItemOutput{}, errors.New("reference image activities did not overlap")
		case <-ctx.Done():
			return CommerceReferenceImageItemOutput{}, ctx.Err()
		}
		time.Sleep(15 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		if shotID == "shot-2" {
			return CommerceReferenceImageItemOutput{}, errors.New("upstream image rejected")
		}
		return CommerceReferenceImageItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded}, nil
	}, activity.RegisterOptions{Name: GenerateCommerceReferenceImageItemActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, _ CommerceReferenceImageBatchInput, output CommerceReferenceImageBatchOutput) (CommerceReferenceImageBatchOutput, error) {
		return output, nil
	}, activity.RegisterOptions{Name: FinalizeCommerceReferenceImageBatchActivityName})
	env.RegisterActivityWithOptions(func(context.Context, FinalizeCommerceReferenceImageFailureInput) error { return nil }, activity.RegisterOptions{Name: FinalizeCommerceReferenceImageFailureActivityName})

	env.ExecuteWorkflow(CommerceReferenceImageBatchWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceReferenceImageBatchOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, commerce.RunPartiallySucceeded, output.Status)
	require.Equal(t, 5, output.Succeeded)
	require.Equal(t, 1, output.Failed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 3, maxActive)
	require.Equal(t, 1, calls["shot-2"], "paid image activity must not be retried automatically")
}

func TestValidateCommerceImagePromptPlanRejectsAudioAndTextLeakage(t *testing.T) {
	identity := testCommerceReferenceImageIdentity()
	snapshot := CommerceReferenceImageShotSnapshot{
		Identity: identity, ProductVersionID: "product-version", TargetLocale: "zh-CN", AspectRatio: "9:16",
		VoiceoverText: "现在下单立即享受优惠", OnscreenText: "限时优惠", SoundEffects: []string{"清脆提示音"}, MusicCue: "轻快电子乐",
		MinimumReferences: 1, MaximumReferences: 2,
		References: []CommerceReferenceImageReference{{ReferenceID: "reference-1"}},
	}
	base := CommerceImagePromptPlanContract{
		ContractVersion:      CommerceImagePromptPlanContractVersion,
		CommerceScriptUnitID: identity.ScriptUnitID, ScriptUnitGenerationID: identity.UnitGenerationID,
		CommerceWorkflowBindingID: identity.CommerceWorkflowBindingID, ProductVersionID: snapshot.ProductVersionID,
		VisualPrompt: "商品置于干净桌面，柔和侧光，包装外观保持一致", NegativePrompt: "no text, no watermark",
		InstructionLanguage: "zh-CN", TargetLanguage: "zh-CN", ReferenceIDs: []string{"reference-1"},
		MustPreserve: []string{"包装颜色", "瓶身形状"}, MustNotRenderText: []string{"限时优惠"}, AspectRatio: "9:16",
	}
	require.NoError(t, ValidateCommerceImagePromptPlan(base, snapshot))
	for name, leaked := range map[string]string{
		"voiceover":     snapshot.VoiceoverText,
		"onscreen text": snapshot.OnscreenText,
		"sound effect":  snapshot.SoundEffects[0],
		"music cue":     snapshot.MusicCue,
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.VisualPrompt += "，" + leaked
			require.Error(t, ValidateCommerceImagePromptPlan(candidate, snapshot))
		})
	}
}

func TestBindCommerceImagePromptPlanIdentityUsesFrozenSnapshot(t *testing.T) {
	identity := testCommerceReferenceImageIdentity()
	snapshot := CommerceReferenceImageShotSnapshot{
		Identity: identity, ProductVersionID: "product-version", TargetLocale: "zh-CN", AspectRatio: "9:16",
		MinimumReferences: 1, MaximumReferences: 1,
		References: []CommerceReferenceImageReference{{ReferenceID: "reference-1"}},
	}
	agentOutput := CommerceImagePromptPlanContract{
		ContractVersion:      CommerceImagePromptPlanContractVersion,
		CommerceScriptUnitID: "hallucinated-script", ScriptUnitGenerationID: "hallucinated-generation",
		CommerceWorkflowBindingID: "hallucinated-binding", ProductVersionID: "hallucinated-product-version",
		VisualPrompt: "商品正面特写，保持包装颜色和形状一致", NegativePrompt: "no text, no watermark",
		InstructionLanguage: "zh-CN", TargetLanguage: "zh-CN", ReferenceIDs: []string{"reference-1"},
		AspectRatio: "9:16",
	}

	bound := BindCommerceImagePromptPlanIdentity(agentOutput, snapshot)

	require.Equal(t, identity.ScriptUnitID, bound.CommerceScriptUnitID)
	require.Equal(t, identity.UnitGenerationID, bound.ScriptUnitGenerationID)
	require.Equal(t, identity.CommerceWorkflowBindingID, bound.CommerceWorkflowBindingID)
	require.Equal(t, snapshot.ProductVersionID, bound.ProductVersionID)
	require.NoError(t, ValidateCommerceImagePromptPlan(bound, snapshot))
}

func TestCommerceImagePromptAgentContextCarriesPreviousFidelityIssues(t *testing.T) {
	snapshot := CommerceReferenceImageShotSnapshot{
		StoryboardShotID: "shot-4",
		PreviousFidelityIssues: []CommerceReviewIssue{{
			Code: "SHOT_COMPOSITION_MISMATCH", Field: "generatedImage",
			Message: "生成结果是多面板拼贴", Suggestion: "只生成单一连续镜头的起始帧",
		}},
	}

	var contextValue struct {
		Snapshot       CommerceReferenceImageShotSnapshot `json:"snapshot"`
		ReviewerIssues []CommerceReviewIssue              `json:"reviewerIssues"`
		RenderPolicy   struct {
			ImageCount             int    `json:"imageCount"`
			FrameMoment            string `json:"frameMoment"`
			ForbidTemporalSequence bool   `json:"forbidTemporalSequence"`
			ForbidStoryboardPanels bool   `json:"forbidStoryboardPanels"`
		} `json:"renderPolicy"`
	}
	require.NoError(t, json.Unmarshal(commerceImagePromptAgentContext(snapshot), &contextValue))
	require.Equal(t, snapshot.StoryboardShotID, contextValue.Snapshot.StoryboardShotID)
	require.Equal(t, snapshot.PreviousFidelityIssues, contextValue.ReviewerIssues)
	require.Equal(t, 1, contextValue.RenderPolicy.ImageCount)
	require.Equal(t, "start_state_only", contextValue.RenderPolicy.FrameMoment)
	require.True(t, contextValue.RenderPolicy.ForbidTemporalSequence)
	require.True(t, contextValue.RenderPolicy.ForbidStoryboardPanels)
}

func TestMergeCommerceReviewIssuesKeepsNewestIssuePerCodeAndField(t *testing.T) {
	newest := []CommerceReviewIssue{
		{Code: "PRODUCT_MATERIAL_CHANGED", Field: "generatedImage", Message: "new material issue", Suggestion: "new fix"},
	}
	older := []CommerceReviewIssue{
		{Code: "PRODUCT_MATERIAL_CHANGED", Field: "generatedImage", Message: "old material issue", Suggestion: "old fix"},
		{Code: "SHOT_COMPOSITION_CHANGED", Field: "generatedImage", Message: "panel issue", Suggestion: "single frame"},
	}

	merged := mergeCommerceReviewIssues(newest, older)

	require.Len(t, merged, 2)
	require.Equal(t, "new material issue", merged[0].Message)
	require.Equal(t, "SHOT_COMPOSITION_CHANGED", merged[1].Code)
}

func TestCommerceImageFidelityContextLabelsGeneratedCandidate(t *testing.T) {
	snapshot := CommerceReferenceImageShotSnapshot{
		References: []CommerceReferenceImageReference{
			{ReferenceID: "reference-1", PackItemID: "pack-1", ArtifactID: "artifact-1", StorageKey: "reference-1.png"},
			{ReferenceID: "reference-2", PackItemID: "pack-2", ArtifactID: "artifact-2", StorageKey: "reference-2.png"},
		},
	}
	generated := provider.GatewayImageOutput{
		ArtifactID: "generated-artifact", MediaFileID: "generated-media", StorageKey: "generated.png",
	}

	references := commerceImageFidelityReviewReferences(snapshot.References, generated, "image-version")
	require.Len(t, references, 3)
	for index, reference := range references {
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(reference.Metadata, &metadata))
		require.Equal(t, float64(index+1), metadata["attachmentIndex"])
		if index < 2 {
			require.Equal(t, "product_reference", metadata["reviewRole"])
		} else {
			require.Equal(t, "generated_candidate", metadata["reviewRole"])
		}
	}

	var contextValue struct {
		GeneratedImage struct {
			AttachmentIndex int `json:"attachmentIndex"`
		} `json:"generatedImage"`
		AttachmentGuide struct {
			ProductReferenceAttachmentIndexes []int  `json:"productReferenceAttachmentIndexes"`
			GeneratedCandidateAttachmentIndex int    `json:"generatedCandidateAttachmentIndex"`
			Instructions                      string `json:"instructions"`
		} `json:"attachmentGuide"`
	}
	require.NoError(t, json.Unmarshal(
		commerceImageFidelityAgentContext(snapshot, CommerceImagePromptPlanState{}, generated, "image-version"),
		&contextValue,
	))
	require.Equal(t, 3, contextValue.GeneratedImage.AttachmentIndex)
	require.Equal(t, []int{1, 2}, contextValue.AttachmentGuide.ProductReferenceAttachmentIndexes)
	require.Equal(t, 3, contextValue.AttachmentGuide.GeneratedCandidateAttachmentIndex)
	require.Contains(t, contextValue.AttachmentGuide.Instructions, "must never be treated as panels")
}

func TestReconcileCommerceImageFidelityReviewTreatsCreativeAlignmentAsAdvisory(t *testing.T) {
	issue := CommerceReviewIssue{
		Code: "PRODUCT_MATERIAL_CHANGED", Field: "generatedImage",
		Message: "自然光产生了轻微高光", Suggestion: "可降低高光后重试",
	}
	review := CommerceImageFidelityReviewContract{
		ContractVersion: CommerceImageFidelityReviewContractVersion,
		Decision:        "reject", Issues: []CommerceReviewIssue{issue}, RegenerationRecommended: true,
		Checks: CommerceImageFidelityChecks{
			ProductIdentity: true, Packaging: true, Color: true, Shape: true,
			ReferenceOwnership: true, NoForbiddenText: true, ShotAlignment: true,
		},
	}
	require.NoError(t, ValidateCommerceImageFidelityReview(review))

	reconciled := ReconcileCommerceImageFidelityReview(review)

	require.Equal(t, "approve", reconciled.Decision)
	require.Empty(t, reconciled.Issues)
	require.Equal(t, []CommerceReviewIssue{issue}, reconciled.AdvisoryIssues)
	require.Equal(t, "approved_with_advisory_issues", reconciled.Resolution)
	require.False(t, reconciled.RegenerationRecommended)
	require.NoError(t, ValidateCommerceImageFidelityReview(reconciled))

	review.Checks.ShotAlignment = false
	approvedWithAlignmentWarning := ReconcileCommerceImageFidelityReview(review)
	require.Equal(t, "approve", approvedWithAlignmentWarning.Decision)
	require.Equal(t, []CommerceReviewIssue{issue}, approvedWithAlignmentWarning.AdvisoryIssues)
	require.NoError(t, ValidateCommerceImageFidelityReview(approvedWithAlignmentWarning))

	review.Checks.NoForbiddenText = false
	rejected := ReconcileCommerceImageFidelityReview(review)
	require.Equal(t, "reject", rejected.Decision)
	require.Equal(t, []CommerceReviewIssue{issue}, rejected.Issues)
	require.Empty(t, rejected.AdvisoryIssues)
}

func TestCommerceProviderImageRequestMatchesGatewayHTTPContract(t *testing.T) {
	input := testCommerceReferenceImageBatchInput(5, []string{"shot-contract"})
	snapshot := CommerceReferenceImageShotSnapshot{
		Identity:         input.Identity,
		StoryboardShotID: "shot-contract",
		TargetLocale:     "zh-CN",
		AspectRatio:      "9:16",
		ImageQuality:     "high",
		Bindings: CommerceReferenceImageAgentBindings{ImagePromptAgent: CommerceAgentBinding{
			TemplateKey: "commerce.image_prompt", PromptVersionID: "prompt-version-1",
		}},
		ImageModel: CommerceMediaModelBinding{ModelProfileKey: "commerce_image", ProviderModelID: "provider-model-1"},
	}
	plan := CommerceImagePromptPlanState{
		Prompt:         "商品正面特写，保持包装颜色和标志一致",
		NegativePrompt: "no text, no watermark",
		PromptHash:     strings.Repeat("d", 64),
		References: []CommerceReferenceImageReference{{
			PackItemID: "pack-item-1", ReferenceID: "reference-1", Role: "primary", Ordinal: 1,
			ArtifactID: "artifact-1", StorageKey: "commerce/products/reference-1.png", ContentHash: strings.Repeat("e", 64), Required: true,
		}},
	}
	request := commerceProviderImageRequest(input, snapshot, plan, NodeExecution{NodeRunID: "node-run-1"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/provider/image/generate", r.URL.Path)
		require.Equal(t, "Bearer commerce-contract-token", r.Header.Get("Authorization"))
		var received provider.GatewayImageRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		require.Equal(t, request.OrganizationID, received.OrganizationID)
		require.Equal(t, request.ProjectID, received.ProjectID)
		require.Equal(t, "commerce_image", received.ModelProfileKey)
		require.Equal(t, "provider-model-1", received.ProviderModelID)
		require.Empty(t, received.PromptLanguage)
		require.False(t, received.RequireApprovedLanguageCapabilities)
		require.Equal(t, "commerce-reference-image:"+input.WorkflowRunID+":shot-contract", received.IdempotencyKey)
		require.Equal(t, received.IdempotencyKey, received.Options.IdempotencyKey)
		require.Len(t, received.References, 1)
		require.Equal(t, "commerce_product_reference", received.References[0].Type)
		require.Equal(t, "artifact-1", received.References[0].ArtifactID)
		require.NotContains(t, string(received.References[0].Metadata), "http")
		var imageInput map[string]any
		require.NoError(t, json.Unmarshal(received.Input, &imageInput))
		require.Equal(t, "9:16", imageInput["aspectRatio"])
		require.Equal(t, "high", imageInput["quality"])
		require.Equal(t, plan.Prompt, imageInput["prompt"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"succeeded","providerRequestId":"request-1","providerCallId":"call-1","modelId":"provider-model-1","output":{"artifactId":"generated-artifact","mediaFileId":"generated-media","storageKey":"commerce/generated.png"}}}`))
	}))
	t.Cleanup(server.Close)

	client := provider.GatewayClient{BaseURL: server.URL, Token: "commerce-contract-token", Client: server.Client()}
	response, err := client.GenerateImage(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "generated-artifact", response.Output.ArtifactID)
}

func TestCommerceReferenceImageReviewRetryReusesGeneratedMedia(t *testing.T) {
	version := CommerceShotImageVersionState{
		ID: "image-version-retry", ArtifactID: "artifact-1", MediaFileID: "media-1",
		StorageKey: "commerce/generated.png", ProviderRequestID: "request-1",
		ProviderCallID: "call-1", ProviderModelID: "model-1",
	}
	response, reusable := commerceGatewayImageResponseFromVersion(version)
	require.True(t, reusable)
	require.Equal(t, "succeeded", response.Status)
	require.Equal(t, version.ProviderRequestID, response.ProviderRequestID)
	require.Equal(t, version.ProviderCallID, response.ProviderCallID)
	require.Equal(t, version.ProviderModelID, response.ModelID)
	require.Equal(t, version.ArtifactID, response.Output.ArtifactID)
	require.Equal(t, version.MediaFileID, response.Output.MediaFileID)
	require.Equal(t, version.StorageKey, response.Output.StorageKey)

	_, reusable = commerceGatewayImageResponseFromVersion(CommerceShotImageVersionState{ArtifactID: "artifact-only"})
	require.False(t, reusable)

	reviewErr := commerceImageFidelityReviewFailure(errors.New("review provider unavailable"))
	code, message := workflowErrorFields(reviewErr, codeActivityFailed)
	require.Equal(t, CommerceCodeImageFidelityReviewFailed, code)
	require.Contains(t, message, "重试时将复用")
	require.True(t, commerceReferenceImageErrorRetryable(reviewErr))
}

func TestCommerceReferenceImageMediaReusePolicy(t *testing.T) {
	require.True(t, commerceReferenceImageMayReuseGeneratedMedia(CommerceReferenceImageBatchInput{}, "shot-1"))
	require.False(t, commerceReferenceImageMayReuseGeneratedMedia(CommerceReferenceImageBatchInput{Force: true}, "shot-1"))
	require.True(t, commerceReferenceImageMayReuseGeneratedMedia(CommerceReferenceImageBatchInput{
		Force: true, ReuseGeneratedMedia: true,
	}, "shot-1"))
	require.True(t, commerceReferenceImageMayReuseGeneratedMedia(CommerceReferenceImageBatchInput{
		Force: true, ReuseGeneratedMediaShotIDs: []string{"shot-1"},
	}, "shot-1"))
	require.False(t, commerceReferenceImageMayReuseGeneratedMedia(CommerceReferenceImageBatchInput{
		Force: true, ReuseGeneratedMediaShotIDs: []string{"shot-2"},
	}, "shot-1"))

	base := testCommerceReferenceImageBatchInput(1, []string{"shot-reuse-hash"})
	base.Force = true
	forcedHash, err := CommerceReferenceImageSubjectHash(base, "shot-reuse-hash")
	require.NoError(t, err)
	base.ReuseGeneratedMedia = true
	retryHash, err := CommerceReferenceImageSubjectHash(base, "shot-reuse-hash")
	require.NoError(t, err)
	require.NotEqual(t, forcedHash, retryHash)
}

func TestCommerceReferenceImageSnapshotHashIgnoresReviewFeedback(t *testing.T) {
	snapshot := CommerceReferenceImageShotSnapshot{
		StoryboardShotID: "shot-review-hash",
		References:       []CommerceReferenceImageReference{{ReferenceID: "reference-1"}},
	}
	before, err := commerceReferenceImageSnapshotInputHash(snapshot)
	require.NoError(t, err)

	snapshot.PreviousFidelityIssues = []CommerceReviewIssue{{
		Code: "SHOT_COMPOSITION_MISMATCH", Field: "generatedImage",
		Message: "构图偏离", Suggestion: "保持开场构图",
	}}
	after, err := commerceReferenceImageSnapshotInputHash(snapshot)
	require.NoError(t, err)

	require.Equal(t, before, after)
}

func TestWorkflowErrorFieldsPreservesCommerceErrorCode(t *testing.T) {
	code, message := workflowErrorFields(commerce.Error{
		Code: CommerceCodeImagePromptContractInvalid, Message: "缺少已审核图片提示词",
	}, codeActivityFailed)

	require.Equal(t, CommerceCodeImagePromptContractInvalid, code)
	require.Equal(t, "缺少已审核图片提示词", message)
}

func testCommerceReferenceImageBatchInput(concurrency int, shots []string) CommerceReferenceImageBatchInput {
	return CommerceReferenceImageBatchInput{
		Identity: testCommerceReferenceImageIdentity(), WorkflowRunID: "00000000-0000-4000-8000-00000000000d",
		ProductionRunID: "00000000-0000-4000-8000-00000000000e", StoryboardPlanID: "00000000-0000-4000-8000-00000000000f", PlanEditRevision: 1,
		Operation: "generate_images", ShotIDs: shots, Concurrency: concurrency,
		CreatedBy: "00000000-0000-4000-8000-000000000010", AttemptGeneration: 1,
	}
}

func testCommerceReferenceImageIdentity() commerce.UnitGenerationIdentity {
	return commerce.UnitGenerationIdentity{
		ExecutionIdentity: commerce.ExecutionIdentity{
			OrganizationID: "00000000-0000-4000-8000-000000000001", ProjectID: "00000000-0000-4000-8000-000000000002", ProjectGenerationID: "00000000-0000-4000-8000-000000000003",
			VideoProductionBindingID: "00000000-0000-4000-8000-000000000004", VideoProductionBindingRevision: 1,
			VideoProfileSnapshotHash:  strings.Repeat("a", 64),
			CommerceWorkflowBindingID: "00000000-0000-4000-8000-000000000005", CommerceWorkflowBindingRevision: 1,
			CommerceConfigurationHash: strings.Repeat("b", 64),
		},
		ProductID: "00000000-0000-4000-8000-000000000006", ScriptUnitID: "00000000-0000-4000-8000-000000000007", ScriptUnitRevision: 1,
		UnitGenerationID: "00000000-0000-4000-8000-000000000008", UnitGenerationNo: 1,
		UnitConfigurationHash: strings.Repeat("c", 64),
	}
}
