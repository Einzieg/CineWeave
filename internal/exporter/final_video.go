package exporter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (e *Exporter) ExportFinalVideo(ctx context.Context, req Request) (Result, error) {
	versionID := strings.TrimSpace(stringOption(req.Options, "finalVideoVersionId"))
	var row struct {
		ID              string
		Title           string
		Status          string
		ArtifactID      sql.NullString
		MediaFileID     sql.NullString
		StorageKey      sql.NullString
		ByteSize        sql.NullInt64
		ContentHash     sql.NullString
		DurationSeconds sql.NullFloat64
		Resolution      string
		AspectRatio     string
	}
	err := e.db.QueryRow(ctx, `
		SELECT f.id::text, f.title, f.status, f.artifact_id::text, f.media_file_id::text, f.storage_key,
		       COALESCE(mf.byte_size, 0), COALESCE(mf.checksum, a.content_hash, ''),
		       COALESCE(f.duration_seconds, mf.duration_seconds, 0)::float8, f.resolution, f.aspect_ratio
		FROM final_video_versions f
		JOIN projects p ON p.id = f.project_id
		LEFT JOIN media_files mf ON mf.id = f.media_file_id
		LEFT JOIN artifacts a ON a.id = f.artifact_id
		WHERE f.organization_id = $1
		  AND f.project_id = $2
		  AND (
		    ($3 <> '' AND f.id = $3::uuid)
		    OR ($3 = '' AND (f.id = p.active_final_video_version_id OR p.active_final_video_version_id IS NULL))
		  )
		ORDER BY CASE WHEN f.id = p.active_final_video_version_id THEN 0 WHEN f.status = 'active' THEN 1 WHEN f.status = 'ready' THEN 2 ELSE 3 END,
		         f.version DESC, f.created_at DESC
		LIMIT 1
	`, req.OrganizationID, req.ProjectID, versionID).Scan(
		&row.ID,
		&row.Title,
		&row.Status,
		&row.ArtifactID,
		&row.MediaFileID,
		&row.StorageKey,
		&row.ByteSize,
		&row.ContentHash,
		&row.DurationSeconds,
		&row.Resolution,
		&row.AspectRatio,
	)
	if err != nil {
		return Result{}, err
	}
	if !row.StorageKey.Valid || strings.TrimSpace(row.StorageKey.String) == "" {
		return Result{}, fmt.Errorf("final video version has no storage object")
	}
	output := map[string]any{
		"finalVideoVersionId": row.ID,
		"title":               row.Title,
		"status":              row.Status,
		"storageKey":          row.StorageKey.String,
		"resolution":          row.Resolution,
		"aspectRatio":         row.AspectRatio,
	}
	if row.DurationSeconds.Valid && row.DurationSeconds.Float64 > 0 {
		output["durationSeconds"] = row.DurationSeconds.Float64
	}
	return Result{
		ExportID:    req.ExportID,
		ExportType:  req.ExportType,
		Format:      "mp4",
		StorageKey:  row.StorageKey.String,
		ArtifactID:  nullString(row.ArtifactID),
		MediaFileID: nullString(row.MediaFileID),
		ByteSize:    row.ByteSize.Int64,
		ContentHash: nullString(row.ContentHash),
		MimeType:    "video/mp4",
		Output:      output,
	}, nil
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	switch value := options[key].(type) {
	case string:
		return value
	default:
		return ""
	}
}

func boolOption(options map[string]any, key string, fallback bool) bool {
	if options == nil {
		return fallback
	}
	switch value := options[key].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}
