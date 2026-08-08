package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type projectControlAsyncAction func(
	context.Context,
	auth.Principal,
	Project,
	projectcontrol.Command,
	json.RawMessage,
) (agentToolResult, error)

type projectControlSharedRuntimeHandler struct {
	executor   *projectControlExecutor
	descriptor projectcontrol.Descriptor
	action     projectControlAsyncAction
}

func projectControlAsyncActionSet(server *Server) map[string]projectControlAsyncAction {
	return map[string]projectControlAsyncAction{
		"adaptation.generate_script":              server.executeAdaptationGenerateScriptAsyncAction,
		"asset.revise_prompt":                     server.executeAssetRevisePromptAsyncAction,
		"asset.batch_generate_images":             server.executeAssetBatchImagesAsyncAction,
		"asset.batch_generate_prompts":            server.executeAssetBatchPromptsAsyncAction,
		"export.create":                           server.executeExportCreateAsyncAction,
		"provider.attest_video_capability":        server.executeProviderAttestVideoCapabilityAsyncAction,
		"provider.install_catalog_preset":         server.executeProviderInstallCatalogAsyncAction,
		"provider.test_model":                     server.executeProviderTestModelAsyncAction,
		"provider.update_account":                 server.executeProviderUpdateAccountAsyncAction,
		"provider.update_model":                   server.executeProviderUpdateModelAsyncAction,
		"provider.verify_video_capability":        server.executeProviderVerifyVideoCapabilityAsyncAction,
		"project.delete":                          server.executeProjectDeleteAsyncAction,
		"project.delete.retry":                    server.executeProjectDeleteRetryAsyncAction,
		"project.production_rebuild":              server.executeProjectProductionRebuildAsyncAction,
		"project.production_rebuild.retry_failed": server.executeProjectProductionRebuildRetryAsyncAction,
		"review.run":                              server.executeReviewRunAsyncAction,
		"review.generate_fix":                     server.executeReviewGenerateFixAsyncAction,
		"script.generate_from_source":             server.executeScriptGenerateFromSourceAsyncAction,
		"commerce.script.revise":                  server.executeCommerceScriptReviseAsyncAction,
		"commerce.script.derive.preview":          server.executeCommerceScriptDerivationPreviewAsyncAction,
		"commerce.script.derive.batch":            server.executeCommerceScriptDerivationBatchAsyncAction,
		"commerce.script.derive.retry_failed":     server.executeCommerceScriptDerivationRetryAsyncAction,
		"commerce.script.derive.cancel":           server.executeCommerceScriptDerivationCancelAsyncAction,
		"commerce.script.rebuild":                 server.executeCommerceScriptRebuildAsyncAction,
		"commerce.video.generate":                 server.executeCommerceDirectVideoGenerateAsyncAction,
		"commerce.video.cancel":                   server.executeCommerceDirectVideoCancelAsyncAction,
		"script.rewrite":                          server.executeScriptRewriteAsyncAction,
		"script.rewrite_preview":                  server.executeScriptRewritePreviewAsyncAction,
		"shot.render_plan.create":                 server.executeShotRenderPlanCreateAsyncAction,
		"shot.render_plan.review_audio":           server.executeShotRenderPlanReviewAudioAsyncAction,
		"shot.cancel_running_videos":              server.executeShotCancelRunningVideosAsyncAction,
		"shot.generate_image_prompts":             server.executeShotGenerateImagePromptsAsyncAction,
		"shot.generate_missing_images":            server.executeShotGenerateMissingImagesAsyncAction,
		"shot.generate_missing_videos":            server.executeShotGenerateMissingVideosAsyncAction,
		"shot.generate_video_prompts":             server.executeShotGenerateVideoPromptsAsyncAction,
		"shot_asset.generate_derived_image":       server.executeShotAssetDerivedImageAsyncAction,
		"storyboard.generate_anchor":              server.executeStoryboardGenerateAnchorAsyncAction,
		"storyboard.replan_shot_state":            server.executeStoryboardReplanShotStateAsyncAction,
		"storyboard.update_shot":                  server.executeStoryboardUpdateShotAsyncAction,
		"timeline.compose":                        server.executeTimelineComposeAsyncAction,
		"workflow.cancel":                         server.executeWorkflowCancelAsyncAction,
		"workflow.start":                          server.executeWorkflowStartAsyncAction,
	}
}

func (h *projectControlSharedRuntimeHandler) Execute(ctx context.Context, request projectcontrol.DispatchRequest) (projectcontrol.DispatchOutcome, error) {
	if h == nil || h.executor == nil || h.executor.server == nil || h.action == nil {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			"PROJECT_CONTROL_ACTION_UNAVAILABLE", "共享动作执行器不可用", true, nil,
		)
	}
	project, principal, err := h.executor.authorizedNativeProjectAction(
		ctx,
		auth.Principal{UserID: request.Command.ActorUserID, OrganizationID: request.Command.OrganizationID},
		h.descriptor.Name,
		request.Command.ProjectID,
		descriptorPermissions(h.descriptor)...,
	)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, projectControlActionRuntimeFailure(err)
	}
	if tool, exists := h.executor.agentTools[h.descriptor.Name]; exists {
		if err := agent.ValidateToolInput(tool, request.Command.Input); err != nil {
			return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
				"VALIDATION_FAILED", err.Error(), false, err,
			)
		}
	}
	ctx = withAPIProviderIdentity(ctx, principal, "project-control-command:"+request.Command.ID)
	result, err := h.action(ctx, principal, project, request.Command, request.Command.Input)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, projectControlActionRuntimeFailure(err)
	}
	if result.Status != "succeeded" {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			firstNonEmpty(result.ErrorCode, "ACTION_FAILED"),
			firstNonEmpty(result.ErrorMessage, result.Summary, "操作失败"),
			result.Retryable,
			nil,
		)
	}
	if h.descriptor.StartsWorkflow {
		workflowRunIDs, workflowErr := agentWorkflowRunIDsFromValue(result.Data)
		if workflowErr != nil || len(workflowRunIDs) == 0 {
			return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
				"CHILD_WORKFLOW_CONTRACT_INVALID", "启动型动作没有返回有效的子工作流标识", false, workflowErr,
			)
		}
		result.ChildWorkflowRunIDs = workflowRunIDs
	}
	output, err := json.Marshal(result)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, projectControlActionRuntimeFailure(err)
	}
	links, err := h.executor.workflowLinks(ctx, request.Command, project.ID, result.ChildWorkflowRunIDs)
	if err != nil {
		return projectcontrol.DispatchOutcome{}, projectcontrol.NewRuntimeFailure(
			"PROJECT_CONTROL_WORKFLOW_LINK_INVALID", "读取子工作流关联失败", true, err,
		)
	}
	outcome := projectcontrol.DispatchOutcome{Output: output, WorkflowLinks: links}
	if len(links) > 0 {
		outcome.NextReconcileAfter = 3 * time.Second
	}
	return outcome, nil
}

func projectControlActionRuntimeFailure(err error) *projectcontrol.RuntimeFailure {
	if err == nil {
		return projectcontrol.NewRuntimeFailure("PROJECT_CONTROL_ACTION_FAILED", "操作执行失败", true, nil)
	}
	var runtimeFailure *projectcontrol.RuntimeFailure
	if errors.As(err, &runtimeFailure) && runtimeFailure != nil {
		return runtimeFailure
	}
	var appErr apiError
	if errors.As(err, &appErr) {
		return projectcontrol.NewRuntimeFailure(
			firstNonEmpty(strings.TrimSpace(appErr.Code), "ACTION_FAILED"),
			firstNonEmpty(strings.TrimSpace(appErr.Message), "操作失败"),
			appErr.Retryable,
			err,
		)
	}
	var accessErr authz.AccessError
	if errors.As(err, &accessErr) || errors.Is(err, authz.ErrAccessDenied) || errors.Is(err, auth.ErrForbidden) {
		return projectcontrol.NewRuntimeFailure("PERMISSION_DENIED", "当前用户已无权执行该操作", false, err)
	}
	if standard, ok := provider.StandardErrorFromError(err); ok {
		return projectcontrol.NewRuntimeFailure(
			firstNonEmpty(strings.TrimSpace(standard.Code), "PROVIDER_REQUEST_FAILED"),
			firstNonEmpty(strings.TrimSpace(standard.Message), "供应商请求失败"),
			standard.Retryable,
			err,
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return projectcontrol.NewRuntimeFailure("RESOURCE_NOT_FOUND", "目标数据不存在", false, err)
	}
	if errors.Is(err, context.Canceled) {
		return projectcontrol.NewRuntimeFailure("ACTION_CANCELLED", "操作已取消", false, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return projectcontrol.NewRuntimeFailure("ACTION_TIMEOUT", "操作超时", true, err)
	}
	return projectcontrol.NewRuntimeFailure("PROJECT_CONTROL_ACTION_FAILED", "操作执行失败", true, err)
}
