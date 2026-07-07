package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type agentProjectGapSummary struct {
	Summary          string                       `json:"summary"`
	Overall          ProductionOverall            `json:"overall"`
	Source           agentProjectSourceGap        `json:"source"`
	Storyboard       agentProjectStoryboardGap    `json:"storyboard"`
	Assets           agentProjectAssetsGap        `json:"assets"`
	ShotImages       agentProjectShotMediaGap     `json:"shotImages"`
	ShotVideos       agentProjectShotMediaGap     `json:"shotVideos"`
	FinalVideo       agentProjectFinalVideoGap    `json:"finalVideo"`
	Workflows        agentProjectWorkflowGap      `json:"workflows"`
	Reviews          agentProjectReviewGap        `json:"reviews"`
	ProviderProfiles []agentProviderProfileStatus `json:"providerProfiles"`
	Gaps             []string                     `json:"gaps"`
	NextActions      []agentProjectNextAction     `json:"nextActions"`
}

type agentProjectSourceGap struct {
	HasSource             bool    `json:"hasSource"`
	NovelSourceCount      int     `json:"novelSourceCount"`
	ScriptSourceCount     int     `json:"scriptSourceCount"`
	ChapterCount          int     `json:"chapterCount"`
	EventCount            int     `json:"eventCount"`
	PendingEventReviews   int     `json:"pendingEventReviews"`
	HasAdaptationPlan     bool    `json:"hasAdaptationPlan"`
	HasActiveScript       bool    `json:"hasActiveScript"`
	ActiveScriptID        *string `json:"activeScriptId,omitempty"`
	ScriptSceneCount      int     `json:"scriptSceneCount"`
	PendingScriptReviews  int     `json:"pendingScriptReviews"`
	StaleScriptSceneCount int     `json:"staleScriptSceneCount"`
	Status                string  `json:"status"`
}

type agentProjectAssetsGap struct {
	TotalAssets                  int    `json:"totalAssets"`
	MissingAssetCardCount        int    `json:"missingAssetCardCount"`
	MissingReferenceImageCount   int    `json:"missingReferenceImageCount"`
	MissingPrimaryReferenceCount int    `json:"missingPrimaryReferenceCount"`
	PendingReviewCount           int    `json:"pendingReviewCount"`
	StaleCount                   int    `json:"staleCount"`
	DownstreamStaleCount         int    `json:"downstreamStaleCount"`
	Status                       string `json:"status"`
}

type agentProjectStoryboardGap struct {
	ShotCount          int    `json:"shotCount"`
	ConfirmedShotCount int    `json:"confirmedShotCount"`
	PendingReviewCount int    `json:"pendingReviewCount"`
	StaleShotCount     int    `json:"staleShotCount"`
	Status             string `json:"status"`
}

type agentProjectShotMediaGap struct {
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	Running   int    `json:"running"`
	Pending   int    `json:"pending"`
	Stale     int    `json:"stale"`
	Missing   int    `json:"missing"`
	Status    string `json:"status"`
}

type agentProjectFinalVideoGap struct {
	Ready            bool    `json:"ready"`
	Status           string  `json:"status"`
	FinalVideoID     *string `json:"finalVideoId,omitempty"`
	TimelineID       *string `json:"timelineId,omitempty"`
	EnabledClipCount int     `json:"enabledClipCount"`
	Stale            bool    `json:"stale"`
}

type agentProjectWorkflowGap struct {
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

type agentProjectReviewGap struct {
	OpenCritical int `json:"openCritical"`
	OpenHigh     int `json:"openHigh"`
}

type agentProviderProfileStatus struct {
	Purpose                      string `json:"purpose"`
	ProfileKey                   string `json:"profileKey"`
	RequiredModality             string `json:"requiredModality"`
	ProfileExists                bool   `json:"profileExists"`
	EnabledBindingCount          int    `json:"enabledBindingCount"`
	ActiveBindingCount           int    `json:"activeBindingCount"`
	ActiveCompatibleBindingCount int    `json:"activeCompatibleBindingCount"`
	Ready                        bool   `json:"ready"`
	Reason                       string `json:"reason,omitempty"`
}

type agentProjectNextAction struct {
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	Reason       string         `json:"reason"`
	Tool         string         `json:"tool,omitempty"`
	WorkflowType string         `json:"workflowType,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
}

func (s *Server) agentProjectGapSummary(ctx context.Context, project Project, status ProductionStatus) (agentProjectGapSummary, error) {
	workflows, err := s.agentProjectWorkflowGap(ctx, project.ID)
	if err != nil {
		return agentProjectGapSummary{}, err
	}
	reviews, err := s.agentProjectReviewGap(ctx, project.ID)
	if err != nil {
		return agentProjectGapSummary{}, err
	}
	profiles, err := s.agentProviderProfileStatuses(ctx, project)
	if err != nil {
		return agentProjectGapSummary{}, err
	}
	return buildAgentProjectGapSummary(status, workflows, reviews, profiles), nil
}

func (s *Server) agentProjectWorkflowGap(ctx context.Context, projectID string) (agentProjectWorkflowGap, error) {
	var out agentProjectWorkflowGap
	err := s.db.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('queued', 'running', 'cancelling')),
		  COUNT(*) FILTER (WHERE status = 'failed')
		FROM workflow_runs
		WHERE project_id = $1
	`, projectID).Scan(&out.Running, &out.Failed)
	return out, err
}

func (s *Server) agentProjectReviewGap(ctx context.Context, projectID string) (agentProjectReviewGap, error) {
	var out agentProjectReviewGap
	err := s.db.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status = 'open' AND severity = 'critical'),
		  COUNT(*) FILTER (WHERE status = 'open' AND severity = 'high')
		FROM review_items
		WHERE project_id = $1
	`, projectID).Scan(&out.OpenCritical, &out.OpenHigh)
	return out, err
}

func (s *Server) agentProviderProfileStatuses(ctx context.Context, project Project) ([]agentProviderProfileStatus, error) {
	defs := []struct {
		purpose  string
		key      string
		modality string
	}{
		{purpose: "文本/剧本业务模型", key: project.ScriptModelProfileKey, modality: "text"},
		{purpose: "图片业务模型", key: project.ImageModelProfileKey, modality: "image"},
		{purpose: "视频业务模型", key: project.VideoModelProfileKey, modality: "video"},
	}
	out := make([]agentProviderProfileStatus, 0, len(defs))
	for _, def := range defs {
		item := agentProviderProfileStatus{
			Purpose:          def.purpose,
			ProfileKey:       strings.TrimSpace(def.key),
			RequiredModality: def.modality,
		}
		if item.ProfileKey == "" {
			item.Reason = "profile_key_missing"
			out = append(out, item)
			continue
		}
		compatible := []string{def.modality, "multimodal"}
		err := s.db.QueryRow(ctx, `
			SELECT
			  COUNT(DISTINCT p.id) > 0,
			  COUNT(b.id) FILTER (WHERE b.enabled),
			  COUNT(b.id) FILTER (WHERE b.enabled AND m.status = 'active' AND a.status = 'active'),
			  COUNT(b.id) FILTER (
			    WHERE b.enabled
			      AND m.status = 'active'
			      AND a.status = 'active'
			      AND m.modality = ANY($3::text[])
			  )
			FROM model_profiles p
			LEFT JOIN model_profile_bindings b ON b.model_profile_id = p.id
			LEFT JOIN provider_models m ON m.id = b.provider_model_id
			LEFT JOIN provider_accounts a ON a.id = m.provider_account_id
			WHERE p.organization_id = $1 AND p.profile_key = $2
		`, project.OrganizationID, item.ProfileKey, compatible).Scan(
			&item.ProfileExists,
			&item.EnabledBindingCount,
			&item.ActiveBindingCount,
			&item.ActiveCompatibleBindingCount,
		)
		if err != nil {
			return nil, err
		}
		item.Ready = item.ProfileExists && item.ActiveCompatibleBindingCount > 0
		switch {
		case !item.ProfileExists:
			item.Reason = "profile_not_found"
		case item.EnabledBindingCount == 0:
			item.Reason = "binding_missing"
		case item.ActiveBindingCount == 0:
			item.Reason = "active_model_missing"
		case item.ActiveCompatibleBindingCount == 0:
			item.Reason = "modality_mismatch"
		}
		out = append(out, item)
	}
	return out, nil
}

func buildAgentProjectGapSummary(status ProductionStatus, workflows agentProjectWorkflowGap, reviews agentProjectReviewGap, profiles []agentProviderProfileStatus) agentProjectGapSummary {
	source := status.Stages.Source
	assets := status.Stages.Assets
	storyboard := status.Stages.Storyboard
	shotImages := status.Stages.ShotImages
	shotVideos := status.Stages.ShotVideos
	finalVideo := status.Stages.FinalVideo

	imageMissing := mediaMissingCount(shotImages)
	videoMissing := mediaMissingCount(shotVideos)
	totalAssets := assets.CharacterCount + assets.SceneCount + assets.PropCount
	hasSource := source.NovelSourceCount+source.ScriptSourceCount > 0
	hasActiveScript := source.ActiveScriptID != nil && strings.TrimSpace(*source.ActiveScriptID) != ""

	out := agentProjectGapSummary{
		Overall: status.Overall,
		Source: agentProjectSourceGap{
			HasSource:             hasSource,
			NovelSourceCount:      source.NovelSourceCount,
			ScriptSourceCount:     source.ScriptSourceCount,
			ChapterCount:          source.ChapterCount,
			EventCount:            source.EventCount,
			PendingEventReviews:   source.PendingEventReviewCount,
			HasAdaptationPlan:     source.ActiveAdaptationPlanID != nil && strings.TrimSpace(*source.ActiveAdaptationPlanID) != "",
			HasActiveScript:       hasActiveScript,
			ActiveScriptID:        source.ActiveScriptID,
			ScriptSceneCount:      source.ScriptSceneCount,
			PendingScriptReviews:  source.PendingScriptSceneCount,
			StaleScriptSceneCount: source.StaleScriptSceneCount,
			Status:                source.Status,
		},
		Assets: agentProjectAssetsGap{
			TotalAssets:                  totalAssets,
			MissingAssetCardCount:        assets.MissingAssetCardCount,
			MissingReferenceImageCount:   assets.MissingReferenceImageCount,
			MissingPrimaryReferenceCount: assets.MissingPrimaryReferenceCount,
			PendingReviewCount:           assets.PendingReviewCount,
			StaleCount:                   assets.StaleCount,
			DownstreamStaleCount:         assets.DownstreamStaleCount,
			Status:                       assets.Status,
		},
		Storyboard: agentProjectStoryboardGap{
			ShotCount:          storyboard.ShotCount,
			ConfirmedShotCount: storyboard.ConfirmedShotCount,
			PendingReviewCount: storyboard.PendingReviewCount,
			StaleShotCount:     storyboard.StaleShotCount,
			Status:             storyboard.Status,
		},
		ShotImages: agentProjectShotMediaGap{
			Total:     shotImages.Total,
			Succeeded: shotImages.Succeeded,
			Failed:    shotImages.Failed,
			Running:   shotImages.Running,
			Pending:   shotImages.Pending,
			Stale:     shotImages.Stale,
			Missing:   imageMissing,
			Status:    shotImages.Status,
		},
		ShotVideos: agentProjectShotMediaGap{
			Total:     shotVideos.Total,
			Succeeded: shotVideos.Succeeded,
			Failed:    shotVideos.Failed,
			Running:   shotVideos.Running,
			Pending:   shotVideos.Pending,
			Stale:     shotVideos.Stale,
			Missing:   videoMissing,
			Status:    shotVideos.Status,
		},
		FinalVideo: agentProjectFinalVideoGap{
			Ready:            finalVideo.Status == "ready",
			Status:           finalVideo.Status,
			FinalVideoID:     finalVideo.FinalVideoVersionID,
			TimelineID:       finalVideo.TimelineID,
			EnabledClipCount: finalVideo.EnabledClipCount,
			Stale:            finalVideo.Stale,
		},
		Workflows:        workflows,
		Reviews:          reviews,
		ProviderProfiles: profiles,
		Gaps:             []string{},
		NextActions:      []agentProjectNextAction{},
	}

	addGap := func(condition bool, text string) {
		if condition {
			out.Gaps = append(out.Gaps, text)
		}
	}
	addAction := func(condition bool, action agentProjectNextAction) {
		if condition {
			out.NextActions = append(out.NextActions, action)
		}
	}

	addGap(!hasSource, "缺少原文或剧本来源")
	addAction(!hasSource, manualNextAction("import_source", "导入原文或剧本", "项目没有可供生产的来源"))

	addGap(hasSource && source.NovelSourceCount > 0 && source.ChapterCount == 0, "小说原文尚未完成分卷分集")
	addAction(hasSource && source.NovelSourceCount > 0 && source.ChapterCount == 0, manualNextAction("split_source_chapters", "重新导入并自动分卷分集", "小说来源没有分集章节，无法按集提取事件"))

	addGap(hasSource && source.NovelSourceCount > 0 && source.ChapterCount > 0 && source.EventCount == 0 && !hasActiveScript, "小说事件尚未提取")
	addAction(hasSource && source.NovelSourceCount > 0 && source.ChapterCount > 0 && source.EventCount == 0 && !hasActiveScript, workflowNextAction("extract_events", "提取小说事件", "已有小说章节但没有事件", "extract_novel_events", map[string]any{}))

	addGap(hasSource && !hasActiveScript, "缺少已激活剧本")
	addAction(hasSource && !hasActiveScript && source.EventCount > 0 && source.AdaptationPlanCount == 0, workflowNextAction("generate_adaptation_plan", "生成改编方案", "已有事件但没有改编方案", "generate_adaptation_plan", map[string]any{}))
	addAction(hasSource && !hasActiveScript, workflowNextAction("source_to_script", "生成并激活剧本", "项目还没有可用于分镜的活动剧本", "source_to_script", map[string]any{}))

	addGap(hasActiveScript && source.ScriptSceneCount == 0, "活动剧本尚未拆分场景")
	addAction(hasActiveScript && source.ScriptSceneCount == 0, workflowNextAction("parse_script_scenes", "解析剧本场景", "活动剧本没有场景结构", "parse_script_scenes", map[string]any{}))

	addGap(source.PendingEventReviewCount > 0, fmt.Sprintf("%d 个事件待审核", source.PendingEventReviewCount))
	addGap(source.PendingScriptSceneCount > 0, fmt.Sprintf("%d 个剧本场景待审核", source.PendingScriptSceneCount))
	addGap(source.StaleScriptSceneCount > 0, fmt.Sprintf("%d 个剧本场景已过期", source.StaleScriptSceneCount))

	addGap(totalAssets > 0 && assets.MissingAssetCardCount > 0, fmt.Sprintf("%d 个资产缺少资产卡", assets.MissingAssetCardCount))
	addGap(totalAssets > 0 && assets.MissingReferenceImageCount > 0, fmt.Sprintf("%d 个资产缺少参考图", assets.MissingReferenceImageCount))
	addAction(hasActiveScript && totalAssets == 0, workflowNextAction("script_to_assets", "分析剧本资产", "活动剧本还没有角色、场景或道具资产", "script_to_assets", map[string]any{"input": map[string]any{"generateImages": false}}))
	addAction(totalAssets > 0 && assets.MissingReferenceImageCount > 0, workflowNextAction("generate_asset_images", "生成资产参考图", "部分核心资产缺少参考图", "script_to_assets", map[string]any{"input": map[string]any{"generateImages": true}}))

	addGap(hasActiveScript && storyboard.ShotCount == 0, "缺少分镜镜头")
	addGap(storyboard.PendingReviewCount > 0, fmt.Sprintf("%d 个分镜镜头待审核", storyboard.PendingReviewCount))
	addGap(storyboard.StaleShotCount > 0, fmt.Sprintf("%d 个分镜镜头已过期", storyboard.StaleShotCount))
	addAction(hasActiveScript && storyboard.ShotCount == 0, workflowNextAction("script_to_storyboard", "从剧本生成分镜", "活动剧本还没有分镜镜头", "script_to_storyboard", map[string]any{}))

	addGap(imageMissing > 0, fmt.Sprintf("%d 个镜头图片未完成", imageMissing))
	addAction(storyboard.ShotCount > 0 && imageMissing > 0, toolNextAction("generate_missing_images", "生成缺失镜头图片", "已有分镜但镜头图片未完成", "shot.generate_missing_images", map[string]any{}))

	addGap(videoMissing > 0, fmt.Sprintf("%d 个镜头视频未完成", videoMissing))
	addAction(storyboard.ShotCount > 0 && imageMissing == 0 && videoMissing > 0, toolNextAction("generate_missing_videos", "生成缺失镜头视频", "镜头图片已准备，视频未完成", "shot.generate_missing_videos", map[string]any{}))

	addGap(workflows.Running > 0, fmt.Sprintf("%d 个工作流正在运行", workflows.Running))
	addGap(workflows.Failed > 0, fmt.Sprintf("%d 个工作流失败待处理", workflows.Failed))

	addGap(reviews.OpenCritical > 0 || reviews.OpenHigh > 0, fmt.Sprintf("%d 个 critical、%d 个 high 审阅问题未解决", reviews.OpenCritical, reviews.OpenHigh))
	addAction(reviews.OpenCritical > 0 || reviews.OpenHigh > 0, toolNextAction("review_blockers", "处理高危审阅问题", "存在 high/critical 审阅问题", "review.list_items", map[string]any{"limit": 50}))

	missingProfiles := missingAgentProviderProfiles(profiles)
	if len(missingProfiles) > 0 {
		addGap(true, "业务模型绑定不可用："+strings.Join(missingProfiles, "、"))
		addAction(true, toolNextAction("configure_model_profiles", "配置业务模型绑定", "当前项目的文本/图片/视频业务模型未全部可用", "provider.list_status", map[string]any{}))
	}

	videoReady := shotVideos.Total > 0 && videoMissing == 0 && shotVideos.Running == 0 && shotVideos.Failed == 0
	addGap(videoReady && finalVideo.Status != "ready", "缺少可用成片")
	addAction(videoReady && finalVideo.Status != "ready", workflowNextAction("compose_timeline", "合成最终预览", "镜头视频已完成但还没有可用成片", "compose_timeline", map[string]any{}))
	addGap(finalVideo.Stale, "成片版本已过期")
	addAction(finalVideo.Stale, workflowNextAction("recompose_timeline", "重新合成成片", "当前成片已过期", "compose_timeline", map[string]any{}))

	if len(out.Gaps) == 0 {
		out.Summary = "项目来源、剧本、分镜、镜头图片/视频、审阅和业务模型绑定均未发现阻塞项，可进入成片合成或最终检查。"
	} else {
		out.Summary = "项目离成片还差：" + strings.Join(out.Gaps, "；") + "。"
	}
	return out
}

func mediaMissingCount(stage ProductionShotMediaStage) int {
	return maxInt(stage.Pending+stage.Stale+stage.Failed, 0)
}

func missingAgentProviderProfiles(profiles []agentProviderProfileStatus) []string {
	out := make([]string, 0)
	for _, profile := range profiles {
		if profile.Ready {
			continue
		}
		name := strings.TrimSpace(profile.Purpose)
		if name == "" {
			name = strings.TrimSpace(profile.ProfileKey)
		}
		if name == "" {
			name = profile.RequiredModality
		}
		out = append(out, name)
	}
	return out
}

func manualNextAction(key, label, reason string) agentProjectNextAction {
	return agentProjectNextAction{Key: key, Label: label, Reason: reason}
}

func toolNextAction(key, label, reason, tool string, args map[string]any) agentProjectNextAction {
	return agentProjectNextAction{Key: key, Label: label, Reason: reason, Tool: tool, Arguments: args}
}

func workflowNextAction(key, label, reason, workflowType string, args map[string]any) agentProjectNextAction {
	if args == nil {
		args = map[string]any{}
	}
	if _, ok := args["workflowType"]; !ok {
		args["workflowType"] = workflowType
	}
	return agentProjectNextAction{Key: key, Label: label, Reason: reason, Tool: "workflow.start", WorkflowType: workflowType, Arguments: args}
}

func agentTaskSummaryPatchFromStepOutput(raw json.RawMessage) (map[string]any, bool) {
	var result agentToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, false
	}
	if result.Status != "succeeded" || result.Name != "project.read_summary" {
		return nil, false
	}
	gapSummary, ok := mapFromAny(result.Data["projectGapSummary"])
	if !ok {
		return nil, false
	}
	summary := firstNonEmpty(stringValueFromAny(gapSummary["summary"]), result.Summary)
	return map[string]any{
		"summary":           summary,
		"projectGapSummary": gapSummary,
	}, true
}

func mapFromAny(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		out := map[string]any{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
		return out, len(out) > 0
	}
}
