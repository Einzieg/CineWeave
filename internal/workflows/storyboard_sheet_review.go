package workflows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

const storyboardSheetOutputReviewTemplateKey = "video_profile.storyboard_sheet.output.review"

type ReviewStoryboardSheetOutputInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	CreatedBy        string `json:"createdBy,omitempty"`
	ShotID           string `json:"shotId"`
	SheetAnchorID    string `json:"sheetAnchorId"`
	SheetArtifactID  string `json:"sheetArtifactId"`
	SheetMediaFileID string `json:"sheetMediaFileId"`
	SheetStorageKey  string `json:"sheetStorageKey"`
	PanelManifestID  string `json:"panelManifestId"`
}

type StoryboardSheetOutputReview struct {
	Approved            bool     `json:"approved"`
	PanelCountObserved  int      `json:"panelCountObserved"`
	Ordered             bool     `json:"ordered"`
	NoVisibleText       bool     `json:"noVisibleText"`
	IdentityConsistent  bool     `json:"identityConsistent"`
	SceneConsistent     bool     `json:"sceneConsistent"`
	ActionSequenceValid bool     `json:"actionSequenceValid"`
	Issues              []string `json:"issues"`
}

type ReviewStoryboardSheetOutput struct {
	PanelManifestID         string                      `json:"panelManifestId"`
	PanelManifestHash       string                      `json:"panelManifestHash"`
	Approved                bool                        `json:"approved"`
	ReviewerProviderCallID  string                      `json:"reviewerProviderCallId"`
	ReviewerModelID         string                      `json:"reviewerModelId"`
	ReviewerPromptVersionID string                      `json:"reviewerPromptVersionId"`
	Review                  StoryboardSheetOutputReview `json:"review"`
}

func (a Activities) ReviewStoryboardSheetOutput(ctx context.Context, input ReviewStoryboardSheetOutputInput) (_ ReviewStoryboardSheetOutput, err error) {
	var execution NodeExecution
	defer func() { err = finalizeWorkflowActivityError(ctx, a.db, execution, err) }()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" ||
		strings.TrimSpace(input.SheetAnchorID) == "" || strings.TrimSpace(input.SheetArtifactID) == "" ||
		strings.TrimSpace(input.SheetMediaFileID) == "" || strings.TrimSpace(input.SheetStorageKey) == "" ||
		strings.TrimSpace(input.PanelManifestID) == "" {
		return ReviewStoryboardSheetOutput{}, fmt.Errorf("storyboard sheet review input is incomplete")
	}
	if a.gateway == nil {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "Provider Gateway 未配置"}
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	if project.VideoProductionProfileKey != videoproduction.ProfileStoryboardSheet {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: videoproduction.CodeProfileIncompatible, Message: "当前项目不是分镜板生产方案"}
	}
	if existing, ok, err := a.findStoryboardSheetReview(ctx, input.PanelManifestID); err != nil {
		return ReviewStoryboardSheetOutput{}, err
	} else if ok {
		if !existing.Approved {
			return existing, storyboardSheetReviewRejectedError(existing.Review)
		}
		return existing, nil
	}
	manifest, ok, err := a.findStoryboardSheetManifest(ctx, input.ShotID, "", false)
	if err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	if !ok || manifest.ID != input.PanelManifestID || manifest.Status != "processing" {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: videoproduction.CodePanelManifestInvalid, Message: "分镜板 PanelManifest 不是待审核状态"}
	}
	var croppedCount int
	if err := a.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM storyboard_sheet_panels
		WHERE manifest_id = $1 AND status = 'cropped'
		  AND artifact_id IS NOT NULL AND media_file_id IS NOT NULL AND COALESCE(storage_key, '') <> ''
	`, input.PanelManifestID).Scan(&croppedCount); err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	if croppedCount != manifest.Manifest.PanelCount {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: videoproduction.CodePanelManifestInvalid, Message: "分镜板裁板结果不完整，不能进入审核"}
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ReviewStoryboardSheetOutput{}, fmt.Errorf("object storage does not support storyboard sheet review")
	}
	body, mimeType, err := objectStore.GetObject(ctx, input.SheetStorageKey, mediapkg.DefaultMaxStoryboardSheetBytes)
	if err != nil {
		return ReviewStoryboardSheetOutput{}, fmt.Errorf("download storyboard sheet for review: %w", err)
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(body)
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: provider.CodeInvalidRequest, Message: "分镜板审核输入不是图片"}
	}
	constraints, err := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
		OrganizationID: input.OrganizationID, ModelProfileKey: project.ScriptModelProfileKey,
		TaskType: provider.TaskTypeTextGenerate, Modality: "multimodal",
	})
	if err != nil || len(constraints.Candidates) == 0 {
		if err != nil {
			return ReviewStoryboardSheetOutput{}, workflowErrorFromProvider(err, codeActivityFailed)
		}
		return ReviewStoryboardSheetOutput{}, workflowError{Code: provider.CodeModelCapabilityUnavailable, Message: "分镜板实际成图审核需要多模态文本业务模型"}
	}
	reviewModel := constraints.Candidates[0]
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, storyboardSheetOutputReviewTemplateKey, map[string]any{
		"input": map[string]any{"context": map[string]any{
			"panelManifest": manifest.Manifest, "sheetArtifactId": input.SheetArtifactID,
			"sheetMediaFileId": input.SheetMediaFileID, "sheetStorageKey": input.SheetStorageKey,
		}},
	})
	if err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey:  nodeKeyForID("storyboard_sheet_output_review", input.PanelManifestID),
		NodeType: "agent.storyboard_sheet.output_review",
		Input: mustJSON(map[string]any{
			"storyboardShotId": input.ShotID, "panelManifestId": input.PanelManifestID,
			"panelManifestHash": manifest.Manifest.ManifestHash, "providerModelId": reviewModel.ProviderModelID,
			"promptTemplateKey": rendered.TemplateKey, "promptVersionId": rendered.PromptVersionID,
		}),
	})
	if err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(body)
	response, err := a.generateProviderText(ctx, execution, provider.GatewayTextRequest{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeRunID: execution.NodeRunID, ModelProfileKey: project.ScriptModelProfileKey,
		ProviderModelID:   reviewModel.ProviderModelID,
		PromptTemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
		Input: mustJSON(map[string]any{
			"messages": []map[string]any{
				{"role": "system", "content": rendered.RenderedText},
				{"role": "user", "content": []map[string]any{
					{"type": "text", "text": "请严格审核这张分镜板实际成图，并只返回约定 JSON。"},
					{"type": "image_url", "image_url": map[string]any{"url": dataURL, "detail": "high"}},
				}},
			},
			"responseFormat": "json", "maxOutputTokens": 1200, "temperature": 0,
		}),
		Options: providerTextGatewayOptions(),
	})
	if err != nil {
		return ReviewStoryboardSheetOutput{}, workflowErrorFromProvider(err, codeActivityFailed)
	}
	var review StoryboardSheetOutputReview
	if err := decodeAgentJSONObject(response.Output.Text, &review); err != nil {
		return ReviewStoryboardSheetOutput{}, workflowError{Code: provider.CodeInvalidRequest, Message: "分镜板实际成图审核输出无效：" + err.Error()}
	}
	if review.Issues == nil {
		review.Issues = []string{}
	}
	review = normalizeStoryboardSheetOutputReview(review, manifest.Manifest.PanelCount)
	output := ReviewStoryboardSheetOutput{
		PanelManifestID: input.PanelManifestID, PanelManifestHash: manifest.Manifest.ManifestHash,
		Approved: review.Approved, ReviewerProviderCallID: response.ProviderCallID,
		ReviewerModelID: response.ModelID, ReviewerPromptVersionID: rendered.PromptVersionID, Review: review,
	}
	if err := a.persistStoryboardSheetReview(ctx, input, project, execution, output); err != nil {
		return ReviewStoryboardSheetOutput{}, err
	}
	if !review.Approved {
		return output, storyboardSheetReviewRejectedError(review)
	}
	return output, nil
}

func normalizeStoryboardSheetOutputReview(review StoryboardSheetOutputReview, expectedPanelCount int) StoryboardSheetOutputReview {
	if review.Issues == nil {
		review.Issues = []string{}
	}
	if review.PanelCountObserved != expectedPanelCount {
		review.Issues = append(review.Issues, fmt.Sprintf("实际画格数为 %d，要求为 %d", review.PanelCountObserved, expectedPanelCount))
	}
	if !review.Ordered {
		review.Issues = append(review.Issues, "画格动作顺序不符合 PanelManifest")
	}
	if !review.NoVisibleText {
		review.Issues = append(review.Issues, "分镜板包含可见文字、数字、编号、字幕或水印")
	}
	if !review.IdentityConsistent {
		review.Issues = append(review.Issues, "人物身份或服装不一致")
	}
	if !review.SceneConsistent {
		review.Issues = append(review.Issues, "场景、道具或空间轴不一致")
	}
	if !review.ActionSequenceValid {
		review.Issues = append(review.Issues, "动作阶段序列无效")
	}
	review.Issues = uniqueWorkflowStrings(review.Issues)
	review.Approved = review.Approved && len(review.Issues) == 0
	return review
}

func storyboardSheetReviewRejectedError(review StoryboardSheetOutputReview) error {
	message := "分镜板实际成图审核未通过"
	if len(review.Issues) > 0 {
		message += "：" + strings.Join(review.Issues, "；")
	}
	return newWorkflowApplicationError(
		workflowError{Code: "STORYBOARD_SHEET_REVIEW_REJECTED", Message: message},
		"STORYBOARD_SHEET_REVIEW_REJECTED",
		message,
	)
}

func (a Activities) persistStoryboardSheetReview(ctx context.Context, input ReviewStoryboardSheetOutputInput, project ProjectProductionSettings, execution NodeExecution, output ReviewStoryboardSheetOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	status, reviewStatus := "failed", "rejected"
	if output.Approved {
		status, reviewStatus = "ready", "approved"
	}
	result, err := tx.Exec(ctx, `
		UPDATE storyboard_sheet_manifests
		SET status = $2, review_status = $3, reviewer_prompt_version_id = NULLIF($4, '')::uuid,
		    reviewer_provider_call_id = NULLIF($5, '')::uuid, reviewer_model_id = NULLIF($6, '')::uuid,
		    reviewer_output = $7, reviewed_at = now(), updated_at = now()
		WHERE id = $1 AND storyboard_shot_id = $8 AND production_generation_id = $9 AND status = 'processing'
	`, input.PanelManifestID, status, reviewStatus, output.ReviewerPromptVersionID,
		output.ReviewerProviderCallID, output.ReviewerModelID, mustJSON(output.Review), input.ShotID, project.ProductionGenerationID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_sheet_panels
		SET review_status = $2, updated_at = now(),
		    metadata = metadata || jsonb_build_object('reviewedAt', now(), 'reviewerProviderCallId', $3::text)
		WHERE manifest_id = $1 AND status = 'cropped'
	`, input.PanelManifestID, reviewStatus, output.ReviewerProviderCallID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors anchor
		SET review_status = $2, updated_at = now(),
		    metadata = metadata || jsonb_build_object(
		      'storyboardSheetOutputReview', $3::jsonb, 'reviewerProviderCallId', $4::text,
		      'reviewerModelId', $5::text, 'reviewedAt', now()
		    )
		WHERE anchor.storyboard_shot_id = $1
		  AND anchor.anchor_role IN ('storyboard_sheet', 'storyboard_panel')
		  AND anchor.status = 'ready' AND anchor.review_status = 'pending'
	`, input.ShotID, reviewStatus, mustJSON(output.Review), output.ReviewerProviderCallID, output.ReviewerModelID); err != nil {
		return err
	}
	shotStatus, imageStatus := "image_failed", "failed"
	if output.Approved {
		ready, err := profileRequiredAnchorsReadyTx(ctx, tx, input.ShotID, videoproduction.ProfileStoryboardSheet)
		if err != nil {
			return err
		}
		if !ready {
			return workflowError{Code: videoproduction.CodeReferencePackIncomplete, Message: "分镜板或裁板画格尚未全部审核通过"}
		}
		shotStatus, imageStatus = "image_succeeded", "succeeded"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = $2, image_status = $3,
		    image_error_code = CASE WHEN $4 THEN NULL ELSE 'STORYBOARD_SHEET_REVIEW_REJECTED' END,
		    image_error_message = CASE WHEN $4 THEN NULL ELSE '分镜板实际成图审核未通过' END,
		    image_completed_at = now(), updated_at = now()
		WHERE id = $1 AND project_id = $5 AND production_generation_id = $6
	`, input.ShotID, shotStatus, imageStatus, output.Approved, input.ProjectID, project.ProductionGenerationID); err != nil {
		return err
	}
	var episodeID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(script_episode_id::text, '')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND production_generation_id = $3
	`, input.ShotID, input.ProjectID, project.ProductionGenerationID).Scan(&episodeID); err != nil {
		return err
	}
	payload := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": input.ShotID,
		"panelManifestId": input.PanelManifestID, "panelManifestHash": output.PanelManifestHash,
		"approved": output.Approved, "productionGenerationId": project.ProductionGenerationID,
		"bindingId": project.VideoProductionBindingID, "bindingRevision": project.VideoProductionBindingRevision,
		"episodeId": episodeID,
	})
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.storyboard_sheet.reviewed", "storyboard_sheet_manifest", input.PanelManifestID, payload); err != nil {
		return err
	}
	if output.Approved {
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
			return err
		} else if !applied {
			return ErrWorkflowWriteFenced
		}
	} else {
		if _, err := failNodeRunTx(ctx, tx, execution, "STORYBOARD_SHEET_REVIEW_REJECTED", "分镜板实际成图审核未通过", mustJSON(output)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a Activities) findStoryboardSheetReview(ctx context.Context, manifestID string) (ReviewStoryboardSheetOutput, bool, error) {
	var output ReviewStoryboardSheetOutput
	var reviewRaw []byte
	err := a.db.QueryRow(ctx, `
		SELECT id::text, manifest_hash, review_status = 'approved',
		       COALESCE(reviewer_provider_call_id::text, ''), COALESCE(reviewer_model_id::text, ''),
		       COALESCE(reviewer_prompt_version_id::text, ''), reviewer_output
		FROM storyboard_sheet_manifests
		WHERE id = $1 AND status IN ('ready', 'failed') AND review_status IN ('approved', 'rejected')
	`, manifestID).Scan(
		&output.PanelManifestID, &output.PanelManifestHash, &output.Approved,
		&output.ReviewerProviderCallID, &output.ReviewerModelID, &output.ReviewerPromptVersionID, &reviewRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewStoryboardSheetOutput{}, false, nil
	}
	if err != nil {
		return ReviewStoryboardSheetOutput{}, false, err
	}
	if err := json.Unmarshal(reviewRaw, &output.Review); err != nil {
		return ReviewStoryboardSheetOutput{}, false, err
	}
	return output, true, nil
}
