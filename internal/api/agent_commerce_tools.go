package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
)

type agentCommerceHandler func(http.ResponseWriter, *http.Request, auth.Principal)

type agentCommerceResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newAgentCommerceResponseRecorder() *agentCommerceResponseRecorder {
	return &agentCommerceResponseRecorder{header: make(http.Header)}
}

func (w *agentCommerceResponseRecorder) Header() http.Header {
	return w.header
}

func (w *agentCommerceResponseRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *agentCommerceResponseRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (s *Server) agentToolCommerce(
	r *http.Request,
	principal auth.Principal,
	project Project,
	task AgentTask,
	step AgentStep,
	args map[string]any,
) agentToolResult {
	if !project.ProjectKind.IsCommerce() {
		return agentToolError(step.ToolName, args, newAPIError(http.StatusConflict, "PROJECT_KIND_MISMATCH", "当前项目不是带货视频项目"))
	}
	if err := s.normalizeAgentCommerceScriptArgs(r.Context(), project, task, step.ToolName, args); err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	projectPath := "/api/projects/" + url.PathEscape(project.ID) + "/commerce"
	invoke := func(method, path string, pathValues map[string]string, query url.Values, body map[string]any, handler agentCommerceHandler) agentToolResult {
		if pathValues == nil {
			pathValues = map[string]string{}
		}
		pathValues["projectId"] = project.ID
		return s.invokeAgentCommerceHandler(r, principal, task, step, args, method, projectPath+path, pathValues, query, body, handler)
	}

	scriptUnitID := agentReferenceStringArg(args, "scriptUnitId")
	switch step.ToolName {
	case "commerce.project.read_summary":
		summary, err := s.commerceAgentPlannerContext(r.Context(), project)
		if err != nil {
			return agentToolError(step.ToolName, args, err)
		}
		return agentToolOK(step.ToolName, args, "已读取带货项目摘要", summary)
	case "commerce.product.get":
		return invoke(http.MethodGet, "/product", nil, nil, nil, s.getCommerceProduct)
	case "commerce.product.references.list":
		query := make(url.Values)
		query.Set("filter[status]", agentStringArg(args, "status"))
		return invoke(http.MethodGet, "/product/references", nil, query, nil, s.listCommerceProductReferences)
	case "commerce.attachment.assign":
		attachmentID := agentReferenceStringArg(args, "attachmentId")
		if !agentTaskHasImageAttachment(task, attachmentID) {
			return agentToolError(
				step.ToolName,
				args,
				newAPIError(http.StatusUnprocessableEntity, "AGENT_IMAGE_ATTACHMENTS_INVALID", "只能绑定当前助手任务附加的图片"),
			)
		}
		result := s.invokeAgentCommerceHandler(
			r, principal, task, step, args,
			http.MethodPost,
			"/api/projects/"+url.PathEscape(project.ID)+"/agent/image-attachments/"+url.PathEscape(attachmentID)+"/assign",
			map[string]string{"projectId": project.ID, "attachmentId": attachmentID},
			nil,
			agentCommerceSelectArgs(args, "scope", "scriptUnitId", "referenceRole", "setPrimary"),
			s.assignAgentImageAttachment,
		)
		if result.Status == "succeeded" {
			if err := s.recordAgentTaskImageAttachmentUsage(
				r.Context(), task.ID, attachmentID, agentStringArg(args, "scope"),
			); err != nil {
				return agentToolError(step.ToolName, args, err)
			}
		}
		return result
	case "commerce.script.list":
		query := make(url.Values)
		status := agentStringArg(args, "status")
		query.Set("filter[status]", status)
		query.Set("cursor", agentStringArg(args, "cursor"))
		query.Set("include", "productionSummary")
		if limit := agentIntArg(args, "limit", 50, 1, 200); limit > 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		result := invoke(http.MethodGet, "/script-units", nil, query, nil, s.listCommerceScriptUnits)
		if result.Status != "succeeded" || (status != "" && status != "active") {
			return result
		}
		scripts, err := s.listAgentCommerceScriptsForSelection(r.Context(), project)
		if err != nil {
			return agentToolError(step.ToolName, args, err)
		}
		annotateAgentCommerceScriptList(result.Data, scripts)
		return result
	case "commerce.script.get":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.getCommerceScriptUnit)
	case "commerce.script.create":
		return invoke(http.MethodPost, "/script-units", nil, nil, args, s.createCommerceScriptUnit)
	case "commerce.script.update":
		body := agentCommercePatchBody(args)
		return invoke(http.MethodPatch, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil, body, s.updateCommerceScriptUnit)
	case "commerce.script.archive":
		return invoke(http.MethodDelete, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision"), s.archiveCommerceScriptUnit)
	case "commerce.script.derive.preview":
		return s.agentToolCommerceScriptDerivationPreview(r, project, task, step, args)
	case "commerce.script.derive.batch":
		sourceScriptUnitID := agentReferenceStringArg(args, "sourceScriptUnitId")
		return invoke(
			http.MethodPost,
			"/script-units/"+url.PathEscape(sourceScriptUnitID)+"/derivations",
			map[string]string{"scriptUnitId": sourceScriptUnitID},
			nil,
			agentCommerceSelectArgs(args, "dimension", "instruction", "preserve", "variations"),
			s.createCommerceScriptDerivation,
		)
	case "commerce.script.derivation.get":
		batchID := agentReferenceStringArg(args, "batchId")
		query := make(url.Values)
		if include := agentStringArg(args, "include"); include != "" {
			query.Set("include", include)
		}
		return invoke(
			http.MethodGet,
			"/script-derivations/"+url.PathEscape(batchID),
			map[string]string{"batchId": batchID},
			query,
			nil,
			s.getCommerceScriptDerivation,
		)
	case "commerce.script.derive.retry_failed":
		batchID := agentReferenceStringArg(args, "batchId")
		return invoke(
			http.MethodPost,
			"/script-derivations/"+url.PathEscape(batchID)+"/retry-failed",
			map[string]string{"batchId": batchID},
			nil,
			map[string]any{},
			s.retryCommerceScriptDerivation,
		)
	case "commerce.script.derive.cancel":
		batchID := agentReferenceStringArg(args, "batchId")
		return invoke(
			http.MethodPost,
			"/script-derivations/"+url.PathEscape(batchID)+"/cancel",
			map[string]string{"batchId": batchID},
			nil,
			agentCommerceSelectArgs(args, "reason"),
			s.cancelCommerceScriptDerivation,
		)
	case "commerce.video.options":
		return invoke(http.MethodGet, "/video-options", nil, nil, nil, s.getCommerceDirectVideoOptions)
	case "commerce.video.list":
		query := make(url.Values)
		query.Set("filter[scriptUnitId]", scriptUnitID)
		return invoke(http.MethodGet, "/direct-videos", nil, query, nil, s.listCommerceDirectVideos)
	case "commerce.video.get":
		jobID := agentReferenceStringArg(args, "jobId")
		return invoke(http.MethodGet, "/direct-videos/"+url.PathEscape(jobID), map[string]string{"jobId": jobID}, nil, nil, s.getCommerceDirectVideo)
	case "commerce.video.generate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/direct-videos",
			map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "durationSeconds", "resolution", "aspectRatio", "generateAudio", "references"),
			s.createCommerceDirectVideo)
	case "commerce.video.cancel":
		jobID := agentReferenceStringArg(args, "jobId")
		return invoke(http.MethodPost, "/direct-videos/"+url.PathEscape(jobID)+"/cancel",
			map[string]string{"jobId": jobID}, nil,
			agentCommerceSelectArgs(args, "reason"), s.cancelCommerceDirectVideo)
	case "commerce.product.version.list":
		return invoke(http.MethodGet, "/product/versions", nil, nil, nil, s.listCommerceProductVersions)
	case "commerce.product.version.create":
		return invoke(http.MethodPost, "/product/versions", nil, nil, args, s.createCommerceProductVersion)
	case "commerce.product.rebuild_impact":
		return invoke(http.MethodPost, "/product/rebuild-impact", nil, nil, args, s.getCommerceProductRebuildImpact)
	case "commerce.product.rebuild":
		return invoke(http.MethodPost, "/product/rebuilds", nil, nil, args, s.createCommerceProductRebuild)
	case "commerce.product.reference.list":
		query := make(url.Values)
		query.Set("filter[status]", agentStringArg(args, "status"))
		return invoke(http.MethodGet, "/product/references", nil, query, nil, s.listCommerceProductReferences)
	case "commerce.product.reference.add":
		return invoke(http.MethodPost, "/product/references", nil, nil, args, s.completeCommerceProductReferenceUpload)
	case "commerce.product.reference.archive":
		referenceID := agentReferenceStringArg(args, "referenceId")
		return invoke(http.MethodDelete, "/product/references/"+url.PathEscape(referenceID), map[string]string{"referenceId": referenceID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision"), s.archiveCommerceProductReference)
	case "commerce.product.reference.set_primary":
		referenceID := agentReferenceStringArg(args, "referenceId")
		body := agentCommerceSelectArgs(args, "expectedRevision")
		body["setPrimary"] = true
		return invoke(http.MethodPatch, "/product/references/"+url.PathEscape(referenceID), map[string]string{"referenceId": referenceID}, nil,
			body, s.updateCommerceProductReference)

	case "commerce.script_unit.list":
		query := make(url.Values)
		query.Set("filter[status]", agentStringArg(args, "status"))
		query.Set("cursor", agentStringArg(args, "cursor"))
		query.Set("include", "productionSummary")
		if limit := agentIntArg(args, "limit", 50, 1, 200); limit > 0 {
			query.Set("limit", strconv.Itoa(limit))
		}
		return invoke(http.MethodGet, "/script-units", nil, query, nil, s.listCommerceScriptUnits)
	case "commerce.script_unit.get":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.getCommerceScriptUnit)
	case "commerce.script_unit.create":
		return invoke(http.MethodPost, "/script-units", nil, nil, args, s.createCommerceScriptUnit)
	case "commerce.script_unit.duplicate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/duplicate", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedScriptUnitsRevision"), s.duplicateCommerceScriptUnit)
	case "commerce.script_unit.create_language_variant":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/language-variants", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedScriptUnitsRevision", "targetLanguage"), s.createCommerceScriptLanguageVariant)
	case "commerce.script_unit.reorder":
		return invoke(http.MethodPost, "/script-units/reorder", nil, nil, args, s.reorderCommerceScriptUnits)
	case "commerce.script_unit.archive":
		return invoke(http.MethodDelete, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision"), s.archiveCommerceScriptUnit)
	case "commerce.script_unit.version.list":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/versions", map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.listCommerceScriptVersions)
	case "commerce.script_unit.version.create":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/versions", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision", "content", "sourceLanguageHint", "activate"), s.createCommerceScriptVersion)
	case "commerce.script_unit.version.activate":
		versionID := agentReferenceStringArg(args, "versionId")
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/versions/"+url.PathEscape(versionID)+"/activate",
			map[string]string{"scriptUnitId": scriptUnitID, "versionId": versionID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision"), s.activateCommerceScriptVersion)
	case "commerce.script_unit.language.get":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/language-resolution", map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.getCommerceScriptLanguageResolution)
	case "commerce.script_unit.language.set":
		return invoke(http.MethodPatch, "/script-units/"+url.PathEscape(scriptUnitID), map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision", "languageMode", "explicitTargetLanguage"), s.updateCommerceScriptUnit)
	case "commerce.script_unit.language.confirm":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/language-confirmation", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "languageResolutionId", "targetLanguage"), s.confirmCommerceScriptLanguage)
	case "commerce.script_unit.localization.list":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/localizations", map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.listCommerceScriptLocalizations)
	case "commerce.script_unit.localization.create":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/localizations", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "sourceScriptVersionId", "languageResolutionId", "sourceLanguage", "targetLanguage", "localizedContent", "structuredContract", "reviewerOutput", "approve"), s.createCommerceScriptLocalization)
	case "commerce.script_unit.localization.activate":
		localizationID := agentReferenceStringArg(args, "localizationId")
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/localizations/"+url.PathEscape(localizationID)+"/activate",
			map[string]string{"scriptUnitId": scriptUnitID, "localizationId": localizationID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision"), s.activateCommerceScriptLocalization)

	case "commerce.script_unit.storyboard.generate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/storyboard-plans", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "expectedUnitGenerationId"), s.createCommerceStoryboardPlan)
	case "commerce.script_unit.storyboard.list":
		planID := agentReferenceStringArg(args, "planId")
		if planID != "" {
			return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/storyboard-plans/"+url.PathEscape(planID),
				map[string]string{"scriptUnitId": scriptUnitID, "planId": planID}, nil, nil, s.getCommerceStoryboardPlan)
		}
		query := make(url.Values)
		query.Set("filter[status]", agentStringArg(args, "status"))
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/storyboard-plans", map[string]string{"scriptUnitId": scriptUnitID}, query, nil, s.listCommerceStoryboardPlans)
	case "commerce.script_unit.storyboard.update_shot":
		shotID := agentReferenceStringArg(args, "shotId")
		return invoke(http.MethodPatch, "/script-units/"+url.PathEscape(scriptUnitID)+"/shots/"+url.PathEscape(shotID),
			map[string]string{"scriptUnitId": scriptUnitID, "shotId": shotID}, nil,
			agentCommerceSelectArgs(args, "expectedPlanRevision", "expectedShotRevision", "visualAction", "shotPurpose", "composition", "camera", "voiceoverText", "onscreenText", "durationSeconds", "productReferenceIds"), s.updateCommerceStoryboardShot)
	case "commerce.script_unit.storyboard.reorder":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/shots/reorder", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "planId", "expectedPlanRevision", "items"), s.reorderCommerceStoryboardShots)

	case "commerce.script_unit.reference_images.generate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/reference-images/generate-batch", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "operation", "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds", "force", "concurrency"), s.generateCommerceReferenceImageBatch)
	case "commerce.script_unit.reference_images.retry_failed", "commerce.script_unit.video_prompts.retry_failed", "commerce.script_unit.shot_videos.retry_failed":
		runID := agentReferenceStringArg(args, "runId")
		return invoke(http.MethodPost, "/production-runs/"+url.PathEscape(runID)+"/retry-failed", map[string]string{"runId": runID}, nil,
			agentCommerceSelectArgs(args, "itemIds", "concurrency"), s.retryFailedCommerceProductionRun)
	case "commerce.script_unit.video_prompts.generate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/video-prompts/generate-batch", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds", "force", "concurrency", "resolution"), s.generateCommerceVideoPromptBatch)
	case "commerce.script_unit.shot_videos.generate":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/shot-videos/generate-batch", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds", "force", "concurrency", "resolution"), s.generateCommerceShotVideoBatch)
	case "commerce.script_unit.shot_videos.cancel":
		runID := agentReferenceStringArg(args, "runId")
		return invoke(http.MethodPost, "/production-runs/"+url.PathEscape(runID)+"/cancel", map[string]string{"runId": runID}, nil,
			agentCommerceSelectArgs(args, "reason"), s.cancelCommerceProductionRun)

	case "commerce.script_unit.timeline.get":
		timelineID := agentReferenceStringArg(args, "timelineId")
		if timelineID != "" {
			return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/timelines/"+url.PathEscape(timelineID),
				map[string]string{"scriptUnitId": scriptUnitID, "timelineId": timelineID}, nil, nil, s.getCommerceTimeline)
		}
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/timelines", map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.listCommerceTimelines)
	case "commerce.script_unit.timeline.update":
		timelineID := agentReferenceStringArg(args, "timelineId")
		return invoke(http.MethodPatch, "/script-units/"+url.PathEscape(scriptUnitID)+"/timelines/"+url.PathEscape(timelineID),
			map[string]string{"scriptUnitId": scriptUnitID, "timelineId": timelineID}, nil,
			agentCommerceSelectArgs(args, "expectedRevision", "title", "overlays"), s.updateCommerceTimeline)
	case "commerce.script_unit.final.list":
		return invoke(http.MethodGet, "/script-units/"+url.PathEscape(scriptUnitID)+"/final-videos", map[string]string{"scriptUnitId": scriptUnitID}, nil, nil, s.listCommerceFinalVideos)
	case "commerce.script_unit.final.compose":
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/final-videos/compose", map[string]string{"scriptUnitId": scriptUnitID}, nil,
			agentCommerceSelectArgs(args, "timelineId", "expectedTimelineRevision", "expectedUnitGenerationId", "title", "resolution"), s.composeCommerceFinalVideo)
	case "commerce.script_unit.final.activate":
		versionID := agentReferenceStringArg(args, "finalVideoVersionId")
		return invoke(http.MethodPost, "/script-units/"+url.PathEscape(scriptUnitID)+"/final-videos/"+url.PathEscape(versionID)+"/activate",
			map[string]string{"scriptUnitId": scriptUnitID, "finalVideoVersionId": versionID}, nil, nil, s.activateCommerceFinalVideo)
	case "commerce.script_unit.batch.advance":
		return s.agentToolCommerceBatchAdvance(r, principal, project, task, step, args)
	case "commerce.script_unit.batch.retry_failed":
		return s.agentToolCommerceBatchRetryFailed(r, principal, project, task, step, args)
	case "commerce.script_unit.batch.cancel":
		return s.agentToolCommerceBatchCancel(r, principal, project, task, step, args)
	default:
		return agentToolError(step.ToolName, args, newAPIError(http.StatusNotImplemented, "AGENT_TOOL_NOT_IMPLEMENTED", "该带货视频助手工具尚未接入执行器"))
	}
}

func (s *Server) agentToolCommerceScriptDerivationPreview(
	r *http.Request,
	project Project,
	task AgentTask,
	step AgentStep,
	args map[string]any,
) agentToolResult {
	input := commercepkg.ScriptDerivationPreviewInput{
		SourceScriptUnitID: agentReferenceStringArg(args, "sourceScriptUnitId"),
		Count:              agentIntArg(args, "count", 0, 1, commercepkg.ScriptDerivationMaxVariations),
		Dimension:          agentStringArg(args, "dimension"),
		Instruction:        agentStringArg(args, "instruction"),
		CandidateValues:    agentStringSliceArg(args, "candidateValues"),
		Preserve:           agentStringSliceArg(args, "preserve"),
	}
	prepared, err := s.commerceDerivations.PreparePreview(
		r.Context(), s.db, project.OrganizationID, project.ID, input,
	)
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	variables, err := prepared.PromptVariables()
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	rendered, err := promptsvc.Render(prepared.Prompt, variables)
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	rendered = promptsvc.WithOutputContract(rendered)
	idempotencyKey := agentStepIdempotencyKey(task, step)
	response, err := provider.NewGatewayClientFromEnv().GenerateText(
		r.Context(),
		provider.GatewayTextRequest{
			OrganizationID:    project.OrganizationID,
			WorkspaceID:       project.WorkspaceID,
			ProjectID:         project.ID,
			ModelProfileKey:   prepared.Routing.ModelProfileKey,
			ProviderModelID:   prepared.Routing.ProviderModelID,
			PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID:   rendered.PromptVersionID,
			PromptHash:        rendered.RenderedHash,
			PromptSource:      rendered.Source,
			IdempotencyKey:    idempotencyKey,
			Input: mustRawJSON(map[string]any{
				"prompt": rendered.RenderedText, "responseFormat": "json",
				"maxOutputTokens": 8000,
			}),
			Options: provider.GatewayTextOptions{
				IdempotencyKey: idempotencyKey,
			},
		},
	)
	if err != nil {
		if standard, ok := provider.StandardErrorFromError(err); ok {
			return agentToolError(step.ToolName, args, apiError{
				Status: provider.HTTPStatusForStandardError(standard),
				Code:   standard.Code, Message: standard.Message,
				Retryable: standard.Retryable,
			})
		}
		return agentToolError(step.ToolName, args, err)
	}
	preview, err := commercepkg.DecodeScriptDerivationPreview(
		response.Output.Text, prepared.Input, prepared.Source,
	)
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	preview.ProviderRequestID = response.ProviderRequestID
	preview.ProviderCallID = response.ProviderCallID
	preview.ProviderModelID = response.ModelID
	preview.PromptTemplateKey = rendered.TemplateKey
	preview.PromptVersionID = rendered.PromptVersionID
	preview.PromptHash = rendered.RenderedHash
	data, err := agentCommerceValueData(preview)
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	data["confirmation"] = map[string]any{
		"sourceScriptUnitId": preview.SourceScriptUnitID,
		"sourceScriptTitle":  preview.SourceScriptTitle,
		"dimension":          preview.Dimension,
		"count":              len(preview.Variations),
		"preserve":           preview.Preserve,
		"variations":         preview.Variations,
		"maySpendProvider":   true,
	}
	return agentToolOK(
		step.ToolName,
		args,
		fmt.Sprintf("已生成 %d 个裂变候选，确认后可创建独立广告脚本", len(preview.Variations)),
		data,
	)
}

func (s *Server) invokeAgentCommerceHandler(
	parent *http.Request,
	principal auth.Principal,
	task AgentTask,
	step AgentStep,
	args map[string]any,
	method string,
	path string,
	pathValues map[string]string,
	query url.Values,
	body map[string]any,
	handler agentCommerceHandler,
) agentToolResult {
	if body == nil {
		body = map[string]any{}
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	requestURL := path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(parent.Context(), method, requestURL, bytes.NewReader(rawBody))
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", agentStepIdempotencyKey(task, step))
	req.Header.Set("X-Request-Id", "agent_"+step.ID)
	for key, value := range pathValues {
		req.SetPathValue(key, value)
	}
	recorder := newAgentCommerceResponseRecorder()
	handler(recorder, req, principal)
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.body.Bytes(), &envelope); err != nil {
		return agentToolError(step.ToolName, args, newAPIError(http.StatusInternalServerError, "AGENT_COMMERCE_HANDLER_INVALID_RESPONSE", "带货视频操作返回了无效响应"))
	}
	if recorder.status < 200 || recorder.status >= 300 || envelope.Error != nil {
		if envelope.Error == nil {
			return agentToolError(step.ToolName, args, newAPIError(recorder.status, "AGENT_COMMERCE_HANDLER_FAILED", "带货视频操作失败"))
		}
		result := agentToolResult{
			Name:         step.ToolName,
			Status:       "failed",
			Summary:      envelope.Error.Message,
			Arguments:    args,
			Retryable:    envelope.Error.Retryable,
			ErrorCode:    envelope.Error.Code,
			ErrorMessage: envelope.Error.Message,
			NextActions:  agentToolErrorNextActions(step.ToolName, envelope.Error.Code),
			Data:         map[string]any{"details": envelope.Error.Details, "httpStatus": recorder.status},
		}
		return result
	}
	data := agentCommerceResultData(envelope.Data, envelope.Meta)
	return agentToolOK(step.ToolName, args, "带货视频操作已完成", data)
}

func agentCommerceSelectArgs(args map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := args[key]; exists {
			result[key] = value
		}
	}
	return result
}

func agentCommercePatchBody(args map[string]any) map[string]any {
	result := agentCommerceSelectArgs(args, "expectedRevision")
	patch, ok := mapFromAny(args["patch"])
	if !ok {
		return result
	}
	for key, value := range patch {
		if key == "content" {
			key = "draftContent"
		}
		result[key] = value
	}
	return result
}

func agentCommerceResultData(data, meta any) map[string]any {
	result := make(map[string]any)
	if object, ok := data.(map[string]any); ok {
		for key, value := range object {
			result[key] = value
		}
	} else if data != nil {
		result["result"] = data
	}
	if meta != nil {
		result["meta"] = meta
	}
	if strings.TrimSpace(stringValueFromAny(result["workflowRunId"])) == "" &&
		strings.TrimSpace(stringValueFromAny(result["workflowType"])) != "" {
		if workflowRunID := strings.TrimSpace(stringValueFromAny(result["id"])); workflowRunID != "" {
			result["workflowRunId"] = workflowRunID
		}
	}
	return result
}

func annotateAgentCommerceScriptList(data map[string]any, scripts commercepkg.ScriptUnitList) {
	if data == nil {
		return
	}
	ordinals := make(map[string]int, len(scripts.Items))
	for index, item := range scripts.Items {
		ordinals[strings.TrimSpace(item.ID)] = index + 1
	}
	items, ok := data["items"].([]any)
	if !ok {
		return
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ordinal := ordinals[strings.TrimSpace(stringValueFromAny(item["id"]))]
		if ordinal > 0 {
			item["stableOrdinal"] = ordinal
		}
	}
	if scripts.ScriptUnitsRevision > 0 {
		data["scriptUnitsRevision"] = scripts.ScriptUnitsRevision
	}
}

func agentCommerceValueData(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) agentCommerceStepDryRunOutput(r *http.Request, project Project, toolName string, args map[string]any) map[string]any {
	if !project.ProjectKind.IsCommerce() {
		return map[string]any{"status": "blocked", "errorCode": "PROJECT_KIND_MISMATCH", "errorMessage": "当前项目不是带货视频项目"}
	}
	preview := map[string]any{
		"status":           "ready",
		"summary":          "将按当前版本和生产代执行带货视频操作。",
		"idempotencyScope": "agent_task_step",
	}
	if agentToolReadOnly(toolName) {
		preview["summary"] = "将读取带货视频项目数据。"
		return preview
	}
	if strings.HasPrefix(toolName, "commerce.script_unit.batch.") {
		if err := s.hydrateAgentCommerceBatchArgs(r.Context(), project, toolName, args); err != nil {
			return map[string]any{"status": "blocked", "errorCode": "COMMERCE_BATCH_STATE_UNAVAILABLE", "errorMessage": err.Error()}
		}
		items := agentCommerceBatchItems(args)
		if toolName == "commerce.script_unit.batch.advance" && len(items) == 0 {
			return map[string]any{"status": "blocked", "errorCode": "VALIDATION_FAILED", "errorMessage": "必须明确选择至少一个脚本单元"}
		}
		preview["targetStage"] = agentStringArg(args, "targetStage")
		preview["scriptUnitCount"] = len(items)
	} else if commerceAgentToolRequiresScriptUnit(toolName) {
		scriptUnitID := agentReferenceStringArg(args, "scriptUnitId")
		if scriptUnitID == "" {
			scriptUnitID = agentReferenceStringArg(args, "sourceScriptUnitId")
		}
		if scriptUnitID == "" {
			return map[string]any{
				"status":       "blocked",
				"errorCode":    "COMMERCE_SCRIPT_UNIT_SELECTION_REQUIRED",
				"errorMessage": "当前对话无法唯一确定带货脚本，请先列出候选项并询问用户",
			}
		}
		item, err := s.commerceCatalog.GetScriptUnit(r.Context(), s.db, project.OrganizationID, project.ID, scriptUnitID)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		preview["scriptUnitId"] = item.ID
		preview["scriptUnitTitle"] = item.Title
		preview["scriptUnitRevision"] = item.Revision
	}
	switch toolName {
	case "commerce.script.derive.preview":
		preview["summary"] = fmt.Sprintf(
			"将基于“%s”的当前正文生成 %d 个裂变候选，不创建新脚本。",
			stringValueFromAny(preview["scriptUnitTitle"]),
			agentIntArg(args, "count", 1, 1, commercepkg.ScriptDerivationMaxVariations),
		)
		preview["dimension"] = agentStringArg(args, "dimension")
		preview["instruction"] = agentStringArg(args, "instruction")
		preview["preserve"] = args["preserve"]
	case "commerce.script.derive.batch":
		variationCount := 0
		if variations, ok := args["variations"].([]any); ok {
			variationCount = len(variations)
		}
		preview["summary"] = fmt.Sprintf(
			"将基于“%s”创建 %d 个独立广告脚本。",
			stringValueFromAny(preview["scriptUnitTitle"]),
			variationCount,
		)
		preview["dimension"] = agentStringArg(args, "dimension")
		preview["instruction"] = agentStringArg(args, "instruction")
		preview["preserve"] = args["preserve"]
		preview["variations"] = args["variations"]
		preview["variationCount"] = variationCount
	}
	if shotIDs := agentReferenceStringSliceArg(args, "shotIds"); len(shotIDs) > 0 {
		preview["targetShotCount"] = len(shotIDs)
		preview["targetShotIds"] = shotIDs
	}
	if agentToolMaySpendProvider(toolName, args) {
		preview["estimatedCostCents"] = agentEstimatedProviderCostCents(toolName, args, 0)
	}
	return preview
}

func commerceAgentToolRequiresScriptUnit(toolName string) bool {
	switch toolName {
	case "commerce.script.get", "commerce.script.update", "commerce.script.archive",
		"commerce.script.derive.preview", "commerce.script.derive.batch",
		"commerce.video.options", "commerce.video.generate":
		return true
	}
	return strings.HasPrefix(toolName, "commerce.script_unit.") &&
		toolName != "commerce.script_unit.list" &&
		!strings.HasPrefix(toolName, "commerce.script_unit.batch.")
}

func agentCommerceBatchItems(args map[string]any) []map[string]any {
	raw, ok := args["items"].([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		if item, ok := mapFromAny(value); ok {
			items = append(items, item)
		}
	}
	return items
}

func agentCommerceBatchTargetCount(args map[string]any) int {
	items := agentCommerceBatchItems(args)
	if len(items) == 0 {
		return 0
	}
	switch agentStringArg(args, "targetStage") {
	case "reference_images", "video_prompts", "shot_videos":
		total := 0
		for _, item := range items {
			count := len(agentStringSliceArg(item, "shotIds"))
			if count == 0 {
				count = 1
			}
			total += count
		}
		return total
	default:
		return len(items)
	}
}

func (s *Server) hydrateAgentCommerceBatchArgs(ctx context.Context, project Project, toolName string, args map[string]any) error {
	if toolName != "commerce.script_unit.batch.retry_failed" && toolName != "commerce.script_unit.batch.cancel" {
		return nil
	}
	coordinatorID := agentReferenceStringArg(args, "coordinatorId")
	if _, err := uuid.Parse(coordinatorID); err != nil {
		return fmt.Errorf("跨脚本协调批次标识无效")
	}
	coordinator, err := getCommerceScriptUnitBatchCoordinator(ctx, s.db, project.OrganizationID, project.ID, coordinatorID)
	if err != nil {
		return err
	}
	args["targetStage"] = coordinator.TargetStage
	selected := make(map[string]struct{})
	for _, scriptUnitID := range agentReferenceStringSliceArg(args, "scriptUnitIds") {
		selected[scriptUnitID] = struct{}{}
	}
	items := make([]any, 0, len(coordinator.Items))
	for _, item := range coordinator.Items {
		if toolName == "commerce.script_unit.batch.retry_failed" && item.Status != "failed" {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[item.ScriptUnitID]; !ok {
				continue
			}
		}
		var snapshot map[string]any
		if len(item.InputSnapshot) > 0 {
			if err := json.Unmarshal(item.InputSnapshot, &snapshot); err != nil {
				return fmt.Errorf("跨脚本批次冻结参数无法读取")
			}
		}
		if snapshot == nil {
			snapshot = map[string]any{"scriptUnitId": item.ScriptUnitID}
		}
		items = append(items, snapshot)
	}
	args["items"] = items
	return nil
}

func (s *Server) agentToolCommerceBatchAdvance(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	return s.invokeAgentCommerceHandler(r, principal, task, step, args,
		http.MethodPost, "/api/projects/"+project.ID+"/commerce/script-unit-batches",
		map[string]string{"projectId": project.ID}, nil,
		agentCommerceSelectArgs(args, "targetStage", "items", "unitConcurrency", "maxConcurrency"),
		s.createCommerceScriptUnitBatch)
}

func (s *Server) agentToolCommerceBatchRetryFailed(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	coordinatorID := agentReferenceStringArg(args, "coordinatorId")
	return s.invokeAgentCommerceHandler(r, principal, task, step, args,
		http.MethodPost, "/api/projects/"+project.ID+"/commerce/script-unit-batches/"+url.PathEscape(coordinatorID)+"/retry-failed",
		map[string]string{"projectId": project.ID, "coordinatorId": coordinatorID}, nil,
		agentCommerceSelectArgs(args, "scriptUnitIds", "maxConcurrency"),
		s.retryCommerceScriptUnitBatch)
}

func (s *Server) agentToolCommerceBatchCancel(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	coordinatorID := agentReferenceStringArg(args, "coordinatorId")
	return s.invokeAgentCommerceHandler(r, principal, task, step, args,
		http.MethodPost, "/api/projects/"+project.ID+"/commerce/script-unit-batches/"+url.PathEscape(coordinatorID)+"/cancel",
		map[string]string{"projectId": project.ID, "coordinatorId": coordinatorID}, nil,
		agentCommerceSelectArgs(args, "reason"), s.cancelCommerceScriptUnitBatch)
}
