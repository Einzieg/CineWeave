package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (e *Exporter) Export(ctx context.Context, req Request) (Result, error) {
	switch req.ExportType {
	case "final_video":
		return e.ExportFinalVideo(ctx, req)
	case "documents":
		return e.ExportDocuments(ctx, req)
	case "asset_package":
		return e.ExportAssetPackage(ctx, req)
	case "project_archive":
		return e.ExportProjectArchive(ctx, req)
	default:
		return Result{}, fmt.Errorf("unsupported export type %q", req.ExportType)
	}
}

func (e *Exporter) ExportDocuments(ctx context.Context, req Request) (Result, error) {
	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "json"
	}
	snapshot, err := e.LoadSnapshot(ctx, req.OrganizationID, req.ProjectID)
	if err != nil {
		return Result{}, err
	}
	applySnapshotOptions(&snapshot, req.Options)
	var body []byte
	extension := ".json"
	mimeType := "application/json"
	switch format {
	case "json":
		body, err = json.MarshalIndent(snapshot, "", "  ")
	case "markdown":
		body = []byte(RenderMarkdown(snapshot))
		extension = ".md"
		mimeType = "text/markdown"
	default:
		return Result{}, fmt.Errorf("documents export format %q is not supported", format)
	}
	if err != nil {
		return Result{}, err
	}
	body = append(body, '\n')
	tempFile, cleanup, err := writeTempFile("cineweave-documents-*"+extension, body)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	storageKey := fmt.Sprintf("org/%s/project/%s/exports/%s/documents%s", req.OrganizationID, req.ProjectID, req.ExportID, extension)
	put, err := e.storage.PutFile(ctx, storageKey, tempFile, mimeType)
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExportID:    req.ExportID,
		ExportType:  req.ExportType,
		Format:      format,
		StorageKey:  put.StorageKey,
		ByteSize:    put.ByteSize,
		ContentHash: put.ContentHash,
		MimeType:    mimeType,
		Output: map[string]any{
			"documentFormat": format,
			"storageKey":     put.StorageKey,
		},
	}, nil
}

func applySnapshotOptions(snapshot *ProjectSnapshot, options map[string]any) {
	empty := json.RawMessage("[]")
	if !boolOption(options, "includeSources", true) {
		snapshot.Sources = empty
	}
	if !boolOption(options, "includeEvents", true) {
		snapshot.Events = empty
		snapshot.AdaptationPlans = empty
	}
	if !boolOption(options, "includeScripts", true) {
		snapshot.Scripts = empty
		snapshot.ScriptScenes = empty
	}
	if !boolOption(options, "includeAssets", true) {
		snapshot.Assets = empty
		snapshot.ShotAssetRequirements = empty
	}
	if !boolOption(options, "includeFinalVideos", true) {
		snapshot.FinalVideos = empty
	}
}

func (e *Exporter) LoadSnapshot(ctx context.Context, organizationID, projectID string) (ProjectSnapshot, error) {
	queryOne := func(sql string) (json.RawMessage, error) {
		var raw []byte
		if err := e.db.QueryRow(ctx, sql, organizationID, projectID).Scan(&raw); err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
	queryArray := func(sql string) (json.RawMessage, error) {
		var raw []byte
		if err := e.db.QueryRow(ctx, sql, organizationID, projectID).Scan(&raw); err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
	var err error
	snapshot := ProjectSnapshot{ExportedAt: e.now().Format("2006-01-02T15:04:05Z07:00")}
	snapshot.Project, err = queryOne(`
		SELECT COALESCE(to_jsonb(p), '{}'::jsonb)
		FROM (
			SELECT * FROM projects
			WHERE organization_id = $1 AND id = $2
		) p
	`)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.Sources, err = queryArray(jsonArraySQL("project_sources", "created_at, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.Events, err = queryArray(jsonArraySQL("novel_events", "sequence_no, created_at, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.AdaptationPlans, err = queryArray(jsonArraySQL("adaptation_plans", "created_at DESC, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.Scripts, err = queryArray(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t.created_at DESC, t.id), '[]'::jsonb)
		FROM (
			SELECT s.*, v.version AS current_version, v.content AS current_content, v.content_format AS current_content_format
			FROM scripts s
			LEFT JOIN script_versions v ON v.id = s.current_version_id
			WHERE s.organization_id = $1 AND s.project_id = $2
		) t
	`)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.ScriptScenes, err = queryArray(jsonArraySQL("script_scenes", "scene_index, scene_no, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.Assets, err = queryArray(jsonArraySQL("canonical_assets", "asset_type, name, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.StoryboardShots, err = queryArray(jsonArraySQL("storyboard_shots", "shot_index, shot_no, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.ShotAssetRequirements, err = queryArray(jsonArraySQL("shot_asset_requirements", "created_at, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.Timelines, err = queryArray(jsonArraySQL("project_timelines", "created_at DESC, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.TimelineClips, err = queryArray(jsonArraySQL("timeline_clips", "timeline_id, clip_index, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	snapshot.FinalVideos, err = queryArray(jsonArraySQL("final_video_versions", "version DESC, created_at DESC, id"))
	if err != nil {
		return ProjectSnapshot{}, err
	}
	return snapshot, nil
}

func jsonArraySQL(table, orderBy string) string {
	return fmt.Sprintf(`
		SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY %s), '[]'::jsonb)
		FROM (
			SELECT * FROM %s
			WHERE organization_id = $1 AND project_id = $2
		) t
	`, orderBy, table)
}

func RenderMarkdown(snapshot ProjectSnapshot) string {
	project := firstMap(snapshot.Project)
	name := stringField(project, "name", "CineWeave 项目")
	var builder strings.Builder
	builder.WriteString("# " + name + "\n\n")
	builder.WriteString("导出时间：" + snapshot.ExportedAt + "\n\n")
	writeObjectSection(&builder, "项目设定", project, []string{"description", "project_type", "content_type", "video_ratio", "art_style", "production_mode"})
	writeListSection(&builder, "小说事件", snapshot.Events, []string{"sequence_no", "title", "summary"})
	writeListSection(&builder, "改编计划", snapshot.AdaptationPlans, []string{"title", "status", "content"})
	writeListSection(&builder, "剧本", snapshot.Scripts, []string{"title", "status", "current_content"})
	writeListSection(&builder, "分场", snapshot.ScriptScenes, []string{"scene_no", "title", "summary", "content"})
	writeListSection(&builder, "资产设定", snapshot.Assets, []string{"asset_type", "name", "description", "base_prompt"})
	writeListSection(&builder, "分镜镜头", snapshot.StoryboardShots, []string{"shot_no", "visual", "camera", "motion", "mood", "video_prompt"})
	writeListSection(&builder, "时间线", snapshot.Timelines, []string{"title", "status", "resolution", "aspect_ratio"})
	writeListSection(&builder, "成片版本", snapshot.FinalVideos, []string{"version", "title", "status", "storage_key"})
	return builder.String()
}

func writeObjectSection(builder *strings.Builder, title string, item map[string]any, keys []string) {
	builder.WriteString("## " + title + "\n\n")
	if len(item) == 0 {
		builder.WriteString("暂无\n\n")
		return
	}
	for _, key := range keys {
		value := printableValue(item[key])
		if value != "" {
			builder.WriteString("- " + key + ": " + value + "\n")
		}
	}
	builder.WriteString("\n")
}

func writeListSection(builder *strings.Builder, title string, raw json.RawMessage, keys []string) {
	builder.WriteString("## " + title + "\n\n")
	items := mapSlice(raw)
	if len(items) == 0 {
		builder.WriteString("暂无\n\n")
		return
	}
	for _, item := range items {
		heading := firstNonEmpty(printableValue(item["title"]), printableValue(item["name"]), printableValue(item["visual"]), printableValue(item["id"]))
		builder.WriteString("### " + heading + "\n\n")
		for _, key := range keys {
			value := printableValue(item[key])
			if value != "" && value != heading {
				builder.WriteString("- " + key + ": " + value + "\n")
			}
		}
		builder.WriteString("\n")
	}
}

func firstMap(raw json.RawMessage) map[string]any {
	var item map[string]any
	_ = json.Unmarshal(raw, &item)
	if item == nil {
		return map[string]any{}
	}
	return item
}

func mapSlice(raw json.RawMessage) []map[string]any {
	var items []map[string]any
	_ = json.Unmarshal(raw, &items)
	return items
}

func stringField(item map[string]any, key, fallback string) string {
	value := printableValue(item[key])
	if value == "" {
		return fallback
	}
	return value
}

func printableValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%g", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "未命名"
}
