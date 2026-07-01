package exporter

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
)

type storageObjectRow struct {
	StorageKey  string
	Path        string
	Type        string
	ArtifactID  string
	MediaFileID string
	ShotID      string
	AssetID     string
}

func (e *Exporter) ExportAssetPackage(ctx context.Context, req Request) (Result, error) {
	snapshot, err := e.LoadSnapshot(ctx, req.OrganizationID, req.ProjectID)
	if err != nil {
		return Result{}, err
	}
	applySnapshotOptions(&snapshot, req.Options)
	objects, err := e.projectStorageObjects(ctx, req.OrganizationID, req.ProjectID, false, req.Options)
	if err != nil {
		return Result{}, err
	}
	tempFile, cleanup, err := createTempZip("cineweave-assets", func(writer *zip.Writer) error {
		for _, object := range objects {
			e.addStorageObject(ctx, writer, &snapshot, ExportedObject(object))
		}
		return addZipJSON(writer, "metadata.json", snapshot)
	})
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	storageKey := fmt.Sprintf("org/%s/project/%s/exports/%s/asset-package.zip", req.OrganizationID, req.ProjectID, req.ExportID)
	put, err := e.storage.PutFile(ctx, storageKey, tempFile, "application/zip")
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExportID:    req.ExportID,
		ExportType:  req.ExportType,
		Format:      "zip",
		StorageKey:  put.StorageKey,
		ByteSize:    put.ByteSize,
		ContentHash: put.ContentHash,
		MimeType:    "application/zip",
		Output: map[string]any{
			"storageKey":      put.StorageKey,
			"includedObjects": len(snapshot.IncludedStorageObjects),
			"skippedObjects":  len(snapshot.SkippedStorageObjects),
		},
	}, nil
}

func (e *Exporter) ExportProjectArchive(ctx context.Context, req Request) (Result, error) {
	snapshot, err := e.LoadSnapshot(ctx, req.OrganizationID, req.ProjectID)
	if err != nil {
		return Result{}, err
	}
	applySnapshotOptions(&snapshot, req.Options)
	projectName := SafeFileName(stringField(firstMap(snapshot.Project), "name", "CineWeave 项目"), "CineWeave 项目")
	objects, err := e.projectStorageObjects(ctx, req.OrganizationID, req.ProjectID, true, req.Options)
	if err != nil {
		return Result{}, err
	}
	tempFile, cleanup, err := createTempZip("cineweave-archive", func(writer *zip.Writer) error {
		if err := addZipBytes(writer, path.Join(projectName, "README.md"), []byte(e.archiveReadme(snapshot))); err != nil {
			return err
		}
		if err := addZipJSON(writer, path.Join(projectName, "project.json"), snapshot); err != nil {
			return err
		}
		if err := addZipBytes(writer, path.Join(projectName, "scripts", "script.md"), []byte(RenderMarkdown(snapshot))); err != nil {
			return err
		}
		if err := addZipBytes(writer, path.Join(projectName, "storyboard", "storyboard-shots.json"), append(snapshot.StoryboardShots, '\n')); err != nil {
			return err
		}
		if err := addZipBytes(writer, path.Join(projectName, "storyboard", "shot-requirements.json"), append(snapshot.ShotAssetRequirements, '\n')); err != nil {
			return err
		}
		if err := addZipBytes(writer, path.Join(projectName, "timelines", "timeline.json"), append(snapshot.Timelines, '\n')); err != nil {
			return err
		}
		for _, object := range objects {
			object.Path = path.Join(projectName, object.Path)
			e.addStorageObject(ctx, writer, &snapshot, ExportedObject(object))
		}
		return addZipJSON(writer, path.Join(projectName, "metadata.json"), snapshot)
	})
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	storageKey := fmt.Sprintf("org/%s/project/%s/exports/%s/project-archive.zip", req.OrganizationID, req.ProjectID, req.ExportID)
	put, err := e.storage.PutFile(ctx, storageKey, tempFile, "application/zip")
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExportID:    req.ExportID,
		ExportType:  req.ExportType,
		Format:      "zip",
		StorageKey:  put.StorageKey,
		ByteSize:    put.ByteSize,
		ContentHash: put.ContentHash,
		MimeType:    "application/zip",
		Output: map[string]any{
			"storageKey":      put.StorageKey,
			"includedObjects": len(snapshot.IncludedStorageObjects),
			"skippedObjects":  len(snapshot.SkippedStorageObjects),
			"root":            projectName,
		},
	}, nil
}

func (e *Exporter) archiveReadme(snapshot ProjectSnapshot) string {
	project := firstMap(snapshot.Project)
	var builder strings.Builder
	builder.WriteString("# " + stringField(project, "name", "CineWeave 项目") + "\n\n")
	builder.WriteString("导出时间：" + snapshot.ExportedAt + "\n\n")
	description := printableValue(project["description"])
	if description != "" {
		builder.WriteString(description + "\n\n")
	}
	builder.WriteString("此归档包含项目设定、原文/剧本、资产设定、分镜、时间线、镜头媒体和最终成片文件。\n")
	return builder.String()
}

func (e *Exporter) projectStorageObjects(ctx context.Context, organizationID, projectID string, archivePaths bool, options map[string]any) ([]storageObjectRow, error) {
	var objects []storageObjectRow
	appendRows := func(query string, args ...any) error {
		rows, err := e.db.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item storageObjectRow
			var artifactID, mediaFileID, shotID, assetID sql.NullString
			if err := rows.Scan(&item.StorageKey, &item.Path, &item.Type, &artifactID, &mediaFileID, &shotID, &assetID); err != nil {
				return err
			}
			item.ArtifactID = nullString(artifactID)
			item.MediaFileID = nullString(mediaFileID)
			item.ShotID = nullString(shotID)
			item.AssetID = nullString(assetID)
			if strings.TrimSpace(item.StorageKey) != "" {
				if path.Ext(item.Path) == "" {
					item.Path += storageExtension(item.StorageKey, "", fallbackExtension(item.Type))
				}
				objects = append(objects, item)
			}
		}
		return rows.Err()
	}
	if boolOption(options, "includeAssets", true) {
		if err := appendRows(`
		SELECT reference_storage_key,
		       'assets/' || asset_type || '/' || id::text AS path,
		       'asset_reference', reference_artifact_id::text, reference_media_file_id::text, NULL::text, id::text
		FROM canonical_assets
		WHERE organization_id = $1 AND project_id = $2 AND COALESCE(reference_storage_key, '') <> ''
		ORDER BY asset_type, name
	`, organizationID, projectID); err != nil {
			return nil, err
		}
	}
	if boolOption(options, "includeAssets", true) {
		if err := appendRows(`
		SELECT derived_storage_key,
		       'assets/derived/' || id::text AS path,
		       'derived_asset_image', derived_artifact_id::text, derived_media_file_id::text, storyboard_shot_id::text, asset_id::text
		FROM shot_asset_requirements
		WHERE organization_id = $1 AND project_id = $2 AND COALESCE(derived_storage_key, '') <> ''
		ORDER BY created_at, id
	`, organizationID, projectID); err != nil {
			return nil, err
		}
	}
	if boolOption(options, "includeShotImages", true) {
		if err := appendRows(`
		SELECT image_storage_key,
		       'shot-images/shot-' || LPAD(COALESCE(shot_no, shot_index + 1)::text, 3, '0') AS path,
		       'shot_image', image_artifact_id::text, image_media_file_id::text, id::text, NULL::text
		FROM storyboard_shots
		WHERE organization_id = $1 AND project_id = $2 AND deleted_at IS NULL AND COALESCE(image_storage_key, '') <> ''
		ORDER BY shot_index, id
	`, organizationID, projectID); err != nil {
			return nil, err
		}
	}
	if boolOption(options, "includeShotVideos", true) {
		if err := appendRows(`
		SELECT video_storage_key,
		       'shot-videos/shot-' || LPAD(COALESCE(shot_no, shot_index + 1)::text, 3, '0') AS path,
		       'shot_video', video_artifact_id::text, video_media_file_id::text, id::text, NULL::text
		FROM storyboard_shots
		WHERE organization_id = $1 AND project_id = $2 AND deleted_at IS NULL AND COALESCE(video_storage_key, '') <> ''
		ORDER BY shot_index, id
	`, organizationID, projectID); err != nil {
			return nil, err
		}
	}
	if boolOption(options, "includeFinalVideos", true) {
		if err := appendRows(`
		SELECT storage_key,
		       'final-videos/v' || version::text AS path,
		       'final_video', artifact_id::text, media_file_id::text, NULL::text, NULL::text
		FROM final_video_versions
		WHERE organization_id = $1 AND project_id = $2 AND COALESCE(storage_key, '') <> ''
		ORDER BY version, created_at
	`, organizationID, projectID); err != nil {
			return nil, err
		}
	}
	if !archivePaths {
		return objects, nil
	}
	for index := range objects {
		switch objects[index].Type {
		case "shot_image":
			objects[index].Path = path.Join("media", objects[index].Path)
		case "shot_video":
			objects[index].Path = path.Join("media", objects[index].Path)
		case "final_video":
			objects[index].Path = path.Join("media", objects[index].Path)
		}
	}
	return objects, nil
}

func fallbackExtension(objectType string) string {
	switch objectType {
	case "asset_reference", "derived_asset_image", "shot_image":
		return ".png"
	case "shot_video", "final_video":
		return ".mp4"
	default:
		return ".bin"
	}
}
