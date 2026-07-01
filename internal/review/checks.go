package review

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deterministicRunner struct {
	db        *pgxpool.Pool
	projectID string
	items     []ReviewItemDraft
}

func (r *deterministicRunner) run(ctx context.Context) ([]ReviewItemDraft, error) {
	if err := r.checkScripts(ctx); err != nil {
		return nil, err
	}
	if err := r.checkAssets(ctx); err != nil {
		return nil, err
	}
	if err := r.checkStoryboardAndMedia(ctx); err != nil {
		return nil, err
	}
	if err := r.checkShotAssets(ctx); err != nil {
		return nil, err
	}
	if err := r.checkTimelineAndFinalVideo(ctx); err != nil {
		return nil, err
	}
	return r.items, nil
}

func (r *deterministicRunner) checkScripts(ctx context.Context) error {
	var scriptID, versionID string
	err := r.db.QueryRow(ctx, `
		SELECT id::text, current_version_id::text
		FROM scripts
		WHERE project_id = $1 AND status = 'active' AND current_version_id IS NOT NULL
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 1
	`, r.projectID).Scan(&scriptID, &versionID)
	if err != nil {
		if err == pgx.ErrNoRows {
			r.add("issue", "script", "high", "缺少当前剧本", "项目没有 active script，后续资产、分镜和视频生产缺少稳定输入。", "先导入或生成剧本，并将一个剧本版本设为当前版本。", "project", "")
			return nil
		}
		return err
	}
	var sceneCount, approvedCount int
	if err := r.db.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE review_status = 'approved')
		FROM script_scenes
		WHERE project_id = $1 AND script_version_id = $2
	`, r.projectID, versionID).Scan(&sceneCount, &approvedCount); err != nil {
		return err
	}
	if sceneCount == 0 {
		r.add("issue", "script", "medium", "剧本未切分分场", "当前剧本没有结构化分场，资产分析和分镜生成会缺少场景粒度。", "运行分场解析并确认关键分场。", "project", "")
	} else if approvedCount == 0 {
		r.add("warning", "script", "medium", "分场尚未确认", "当前剧本已有分场，但没有任何分场处于 approved 状态。", "审阅关键分场并标记通过。", "project", "")
	}
	rows, err := r.db.Query(ctx, `
		SELECT id::text, COALESCE(title, ''), scene_no
		FROM script_scenes
		WHERE project_id = $1 AND script_version_id = $2 AND manual_override = true AND stale_state = 'needs_regeneration'
		ORDER BY scene_index, id
	`, r.projectID, versionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		var sceneNo int
		if err := rows.Scan(&id, &title, &sceneNo); err != nil {
			return err
		}
		r.add("issue", "script", "medium", fmt.Sprintf("分场 %d 需要重新生成", sceneNo), labelOr(title, "人工修改的分场")+" 标记为需要重新生成。", "重新生成该分场或手工确认其与上游剧本一致。", "script_scene", id)
	}
	return rows.Err()
}

func (r *deterministicRunner) checkAssets(ctx context.Context) error {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, asset_type, name, COALESCE(description, ''),
		       primary_reference_artifact_id::text, primary_reference_media_file_id::text, primary_reference_storage_key,
		       reference_artifact_id::text, reference_media_file_id::text, reference_storage_key,
		       status, review_status, stale_state
		FROM canonical_assets
		WHERE project_id = $1
		ORDER BY asset_type, name
	`, r.projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, assetType, name, description, status, reviewStatus, staleState string
		var primaryArtifactID, primaryMediaFileID, primaryStorageKey, referenceArtifactID, referenceMediaFileID, referenceStorageKey sql.NullString
		if err := rows.Scan(&id, &assetType, &name, &description, &primaryArtifactID, &primaryMediaFileID, &primaryStorageKey, &referenceArtifactID, &referenceMediaFileID, &referenceStorageKey, &status, &reviewStatus, &staleState); err != nil {
			return err
		}
		if strings.TrimSpace(description) == "" {
			r.add("issue", "asset", "medium", "资产缺少描述", fmt.Sprintf("%s“%s”没有描述，生成提示词会缺少稳定设定。", assetTypeLabel(assetType), name), "补充身份、外观、用途或场景描述。", "canonical_asset", id)
		}
		hasPrimary := validString(primaryArtifactID) || validString(primaryMediaFileID) || validString(primaryStorageKey) || validString(referenceArtifactID) || validString(referenceMediaFileID) || validString(referenceStorageKey)
		if !hasPrimary {
			severity := "medium"
			if assetType == "character" {
				severity = "high"
			} else if assetType == "prop" {
				severity = "low"
			}
			r.add("issue", "asset", severity, "资产缺少主参考图", fmt.Sprintf("%s“%s”没有主参考图，后续镜头可能出现一致性问题。", assetTypeLabel(assetType), name), "生成或上传参考图，并设为主参考。", "canonical_asset", id)
		}
		if staleState != "" && staleState != "fresh" {
			r.add("issue", "asset", "medium", "资产已过期", fmt.Sprintf("%s“%s”的 stale_state 为 %s。", assetTypeLabel(assetType), name, staleState), "重新生成资产卡或参考图，并确认下游镜头。", "canonical_asset", id)
		}
		if reviewStatus != "" && reviewStatus != "approved" {
			r.add("warning", "asset", "low", "资产尚未审阅通过", fmt.Sprintf("%s“%s”的审阅状态为 %s。", assetTypeLabel(assetType), name, reviewStatus), "审阅资产设定卡并标记通过。", "canonical_asset", id)
		}
		if status == "image_failed" {
			r.add("issue", "asset", "high", "资产参考图生成失败", fmt.Sprintf("%s“%s”的参考图生成失败。", assetTypeLabel(assetType), name), "检查提示词或供应商设置后重新生成参考图。", "canonical_asset", id)
		}
	}
	return rows.Err()
}

func (r *deterministicRunner) checkStoryboardAndMedia(ctx context.Context) error {
	var shotCount int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_shots WHERE project_id = $1 AND deleted_at IS NULL`, r.projectID).Scan(&shotCount); err != nil {
		return err
	}
	if shotCount == 0 {
		r.add("issue", "storyboard", "high", "缺少分镜镜头", "项目没有分镜镜头，无法进入镜头图片、镜头视频和成片生产。", "从当前剧本生成分镜。", "project", "")
		return nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id::text, COALESCE(shot_no, shot_index + 1), COALESCE(visual, ''), duration_seconds,
		       COALESCE(review_status, ''), COALESCE(stale_state, ''), COALESCE(image_status, ''), COALESCE(video_status, ''),
		       image_artifact_id::text, video_artifact_id::text
		FROM storyboard_shots
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY shot_index, id
	`, r.projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, visual, reviewStatus, staleState, imageStatus, videoStatus string
		var shotNo int
		var duration sql.NullFloat64
		var imageArtifactID, videoArtifactID sql.NullString
		if err := rows.Scan(&id, &shotNo, &visual, &duration, &reviewStatus, &staleState, &imageStatus, &videoStatus, &imageArtifactID, &videoArtifactID); err != nil {
			return err
		}
		shotLabel := fmt.Sprintf("镜头 %d", shotNo)
		if strings.TrimSpace(visual) == "" {
			r.add("issue", "storyboard", "high", shotLabel+" 缺少画面描述", "分镜镜头没有 visual，图片和视频提示词会缺少核心画面目标。", "补充镜头画面描述。", "storyboard_shot", id)
		}
		if !duration.Valid || duration.Float64 <= 0 {
			r.add("issue", "storyboard", "medium", shotLabel+" 缺少时长", "分镜镜头没有有效 duration_seconds，时间线合成难以估算节奏。", "补充镜头目标时长。", "storyboard_shot", id)
		}
		if reviewStatus != "" && reviewStatus != "approved" {
			r.add("warning", "storyboard", "low", shotLabel+" 尚未审阅通过", "分镜镜头审阅状态为 "+reviewStatus+"。", "审阅镜头并标记通过。", "storyboard_shot", id)
		}
		if staleState != "" && staleState != "fresh" {
			r.add("issue", "storyboard", "medium", shotLabel+" 已过期", "分镜镜头 stale_state 为 "+staleState+"。", "根据最新剧本或资产重新生成该镜头。", "storyboard_shot", id)
		}
		switch imageStatus {
		case "failed":
			r.add("issue", "shot_image", "high", shotLabel+" 图片生成失败", "镜头图片生成失败。", "修正提示词或供应商设置后重新生成镜头图片。", "storyboard_shot", id)
		case "stale":
			r.add("issue", "shot_image", "medium", shotLabel+" 图片已过期", "镜头图片状态为 stale。", "重新生成镜头图片。", "storyboard_shot", id)
		}
		switch videoStatus {
		case "failed":
			r.add("issue", "shot_video", "high", shotLabel+" 视频生成失败", "镜头视频生成失败。", "修正提示词或供应商设置后重新生成镜头视频。", "storyboard_shot", id)
		case "stale":
			r.add("issue", "shot_video", "medium", shotLabel+" 视频已过期", "镜头视频状态为 stale。", "重新生成镜头视频。", "storyboard_shot", id)
		}
		if validString(videoArtifactID) && !validString(imageArtifactID) {
			r.add("issue", "shot_image", "high", shotLabel+" 有视频但缺少图片", "镜头已有视频产物，但缺少对应镜头图片，后续重生成和追溯会不稳定。", "补齐或重新生成镜头图片。", "storyboard_shot", id)
		}
		if !validString(videoArtifactID) {
			r.add("issue", "shot_video", "medium", shotLabel+" 缺少视频产物", "镜头没有 video_artifact_id，最终成片会缺少该镜头。", "生成镜头视频。", "storyboard_shot", id)
		}
	}
	return rows.Err()
}

func (r *deterministicRunner) checkShotAssets(ctx context.Context) error {
	rows, err := r.db.Query(ctx, `
		SELECT s.id::text, COALESCE(s.shot_no, s.shot_index + 1), COUNT(req.id)
		FROM storyboard_shots s
		LEFT JOIN shot_asset_requirements req ON req.storyboard_shot_id = s.id
		WHERE s.project_id = $1 AND s.deleted_at IS NULL
		GROUP BY s.id, s.shot_no, s.shot_index
		HAVING COUNT(req.id) = 0
		ORDER BY COALESCE(s.shot_no, s.shot_index + 1)
	`, r.projectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var shotNo, count int
		if err := rows.Scan(&id, &shotNo, &count); err != nil {
			rows.Close()
			return err
		}
		_ = count
		r.add("warning", "shot_asset", "medium", fmt.Sprintf("镜头 %d 缺少派生资产要求", shotNo), "该镜头没有任何 shot_asset_requirements，角色、场景和道具的一致性约束不足。", "分析镜头派生资产或手工补充要求。", "storyboard_shot", id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	reqRows, err := r.db.Query(ctx, `
		SELECT req.id::text, req.requirement_type, COALESCE(asset.name, ''), COALESCE(asset.id::text, ''),
		       COALESCE(req.pose, ''), COALESCE(req.expression, ''), COALESCE(req.action, ''),
		       COALESCE(req.review_status, ''), COALESCE(req.stale_state, '')
		FROM shot_asset_requirements req
		LEFT JOIN canonical_assets asset ON asset.id = req.asset_id
		WHERE req.project_id = $1
		ORDER BY req.created_at, req.id
	`, r.projectID)
	if err != nil {
		return err
	}
	defer reqRows.Close()
	for reqRows.Next() {
		var id, reqType, assetName, assetID, pose, expression, action, reviewStatus, staleState string
		if err := reqRows.Scan(&id, &reqType, &assetName, &assetID, &pose, &expression, &action, &reviewStatus, &staleState); err != nil {
			return err
		}
		if strings.TrimSpace(assetID) == "" {
			r.add("issue", "shot_asset", "high", "派生资产要求缺少基础资产", "存在 shot_asset_requirement 没有关联 canonical_asset。", "重新分析镜头派生资产。", "shot_asset_requirement", id)
		}
		if staleState != "" && staleState != "fresh" {
			r.add("issue", "shot_asset", "medium", "派生资产要求已过期", labelOr(assetName, "派生资产要求")+" 的 stale_state 为 "+staleState+"。", "重新生成或确认该派生资产要求。", "shot_asset_requirement", id)
		}
		if reviewStatus != "" && reviewStatus != "approved" {
			r.add("warning", "shot_asset", "low", "派生资产要求尚未审阅通过", labelOr(assetName, "派生资产要求")+" 的审阅状态为 "+reviewStatus+"。", "审阅派生资产要求并标记通过。", "shot_asset_requirement", id)
		}
		if reqType == "character" && (strings.TrimSpace(pose) == "" || strings.TrimSpace(expression) == "" || strings.TrimSpace(action) == "") {
			r.add("warning", "shot_asset", "medium", "角色派生资产要求不完整", labelOr(assetName, "角色")+" 缺少 pose/action/expression 中的至少一项。", "补充角色在镜头中的姿态、动作和表情要求。", "shot_asset_requirement", id)
		}
	}
	return reqRows.Err()
}

func (r *deterministicRunner) checkTimelineAndFinalVideo(ctx context.Context) error {
	var timelineCount, enabledClips, succeededVideos, staleEnabledClips int
	if err := r.db.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM project_timelines WHERE project_id = $1),
		  (SELECT COUNT(*) FROM timeline_clips WHERE project_id = $1 AND enabled = true),
		  (SELECT COUNT(*) FROM storyboard_shots WHERE project_id = $1 AND deleted_at IS NULL AND (video_status = 'succeeded' OR video_artifact_id IS NOT NULL)),
		  (SELECT COUNT(*)
		   FROM timeline_clips c
		   LEFT JOIN storyboard_shots s ON s.id = c.storyboard_shot_id
		   WHERE c.project_id = $1 AND c.enabled = true AND (
		     COALESCE(s.stale_state, 'fresh') <> 'fresh'
		     OR COALESCE(s.video_status, '') = 'stale'
		     OR COALESCE(c.source_storage_key, '') = ''
		   ))
	`, r.projectID).Scan(&timelineCount, &enabledClips, &succeededVideos, &staleEnabledClips); err != nil {
		return err
	}
	if timelineCount == 0 {
		r.add("issue", "timeline", "medium", "缺少时间线", "项目没有时间线，无法稳定编排镜头并合成最终成片。", "从已完成镜头视频创建时间线。", "project", "")
	} else if enabledClips == 0 {
		r.add("issue", "timeline", "high", "时间线没有启用片段", "当前时间线没有任何 enabled clips，最终成片会为空。", "启用有效片段或从镜头视频重新创建时间线。", "project", "")
	}
	if enabledClips > 0 && succeededVideos > enabledClips {
		r.add("warning", "timeline", "low", "部分成功镜头未进入时间线", fmt.Sprintf("已成功镜头视频 %d 个，启用片段 %d 个。", succeededVideos, enabledClips), "检查是否有镜头遗漏在时间线之外。", "project", "")
	}
	var activeID string
	var activeStorage sql.NullString
	err := r.db.QueryRow(ctx, `
		SELECT f.id::text, f.storage_key
		FROM final_video_versions f
		JOIN projects p ON p.id = f.project_id
		WHERE f.project_id = $1
		  AND (f.status = 'active' OR f.id = p.active_final_video_version_id)
		ORDER BY CASE WHEN f.id = p.active_final_video_version_id THEN 0 ELSE 1 END, f.version DESC, f.created_at DESC
		LIMIT 1
	`, r.projectID).Scan(&activeID, &activeStorage)
	if err != nil {
		if err == pgx.ErrNoRows {
			r.add("issue", "final_video", "high", "缺少当前最终成片", "项目没有 active final video，无法交付或导出最终成片。", "在时间线中合成最终成片。", "project", "")
			return nil
		}
		return err
	}
	if !validString(activeStorage) {
		r.add("issue", "final_video", "high", "最终成片缺少存储对象", "当前最终成片没有 storage_key，下载和归档会失败。", "重新合成最终成片。", "final_video_version", activeID)
	}
	if staleEnabledClips > 0 {
		r.add("issue", "final_video", "medium", "最终成片可能已过期", fmt.Sprintf("时间线中有 %d 个启用片段可能已过期或缺少源文件。", staleEnabledClips), "检查时间线片段并重新合成最终成片。", "final_video_version", activeID)
	}
	return nil
}

func (r *deterministicRunner) add(itemType, category, severity, title, description, suggestion, entityType, entityID string) {
	item := ReviewItemDraft{
		ItemType:    itemType,
		Category:    category,
		Severity:    normalizeSeverity(severity),
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Suggestion:  strings.TrimSpace(suggestion),
		EntityType:  entityType,
		EntityID:    strings.TrimSpace(entityID),
		Metadata: map[string]any{
			"source": "deterministic",
		},
	}
	item.Metadata["actions"] = r.actions(item)
	r.items = append(r.items, item)
}

func (r *deterministicRunner) actions(item ReviewItemDraft) []Action {
	return ActionsForItem(r.projectID, item)
}

func ActionsForItem(projectID string, item ReviewItemDraft) []Action {
	actions := []Action{}
	switch item.EntityType {
	case "canonical_asset":
		actions = append(actions, Action{Label: "打开资产设定卡", ActionType: "navigate", Href: "/projects/" + projectID + "/assets?assetId=" + item.EntityID})
		actions = append(actions, Action{Label: "重新生成参考图", ActionType: "regenerate", TargetType: "canonical_asset_image", TargetID: item.EntityID})
	case "storyboard_shot":
		actions = append(actions, Action{Label: "打开分镜镜头", ActionType: "navigate", Href: "/projects/" + projectID + "/storyboard?shotId=" + item.EntityID})
		if item.Category == "shot_image" {
			actions = append(actions, Action{Label: "重新生成镜头图片", ActionType: "regenerate", TargetType: "shot_image", TargetID: item.EntityID})
		}
		if item.Category == "shot_video" {
			actions = append(actions, Action{Label: "重新生成镜头视频", ActionType: "regenerate", TargetType: "shot_video", TargetID: item.EntityID})
		}
	case "shot_asset_requirement":
		actions = append(actions, Action{Label: "打开派生资产要求", ActionType: "navigate", Href: "/projects/" + projectID + "/storyboard?requirementId=" + item.EntityID})
		actions = append(actions, Action{Label: "重新生成派生资产图", ActionType: "regenerate", TargetType: "derived_asset_image", TargetID: item.EntityID})
	case "script_scene":
		actions = append(actions, Action{Label: "打开分场", ActionType: "navigate", Href: "/projects/" + projectID + "/sources?sceneId=" + item.EntityID})
		actions = append(actions, Action{Label: "重新生成分场", ActionType: "regenerate", TargetType: "script_scene", TargetID: item.EntityID})
	case "timeline_clip":
		actions = append(actions, Action{Label: "打开时间线", ActionType: "navigate", Href: "/projects/" + projectID + "/timeline?clipId=" + item.EntityID})
	case "final_video_version":
		actions = append(actions, Action{Label: "打开时间线", ActionType: "navigate", Href: "/projects/" + projectID + "/timeline?finalVideoId=" + item.EntityID})
		actions = append(actions, Action{Label: "重新合成成片", ActionType: "regenerate", TargetType: "final_video", TargetID: item.EntityID})
	default:
		actions = append(actions, Action{Label: "打开项目", ActionType: "navigate", Href: "/projects/" + projectID})
	}
	return actions
}

func normalizeSeverity(value string) string {
	switch value {
	case "critical", "high", "medium", "low":
		return value
	default:
		return "medium"
	}
}

func assetTypeLabel(value string) string {
	switch value {
	case "character":
		return "角色"
	case "scene":
		return "场景"
	case "prop":
		return "道具"
	default:
		return "资产"
	}
}

func labelOr(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validString(value sql.NullString) bool {
	return value.Valid && strings.TrimSpace(value.String) != ""
}
