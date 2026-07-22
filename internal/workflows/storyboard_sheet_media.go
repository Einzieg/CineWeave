package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/jackc/pgx/v5"
)

type ProcessStoryboardSheetPanelsInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	CreatedBy        string `json:"createdBy,omitempty"`
	ShotID           string `json:"shotId"`
	SheetAnchorID    string `json:"sheetAnchorId"`
	SheetArtifactID  string `json:"sheetArtifactId"`
	SheetMediaFileID string `json:"sheetMediaFileId"`
	SheetStorageKey  string `json:"sheetStorageKey"`
}

type ProcessStoryboardSheetPanelOutput struct {
	PanelID        string                           `json:"panelId"`
	VisualAnchorID string                           `json:"visualAnchorId"`
	Ordinal        int                              `json:"ordinal"`
	ArtifactID     string                           `json:"artifactId"`
	MediaFileID    string                           `json:"mediaFileId"`
	StorageKey     string                           `json:"storageKey"`
	ContentHash    string                           `json:"contentHash"`
	Width          int                              `json:"width"`
	Height         int                              `json:"height"`
	Crop           mediapkg.StoryboardSheetCropRect `json:"crop"`
}

type ProcessStoryboardSheetPanelsOutput struct {
	PanelManifestID   string                              `json:"panelManifestId"`
	PanelManifestHash string                              `json:"panelManifestHash"`
	SheetAnchorID     string                              `json:"sheetAnchorId"`
	Panels            []ProcessStoryboardSheetPanelOutput `json:"panels"`
	SourceWidth       int                                 `json:"sourceWidth"`
	SourceHeight      int                                 `json:"sourceHeight"`
}

func (a Activities) ProcessStoryboardSheetPanels(ctx context.Context, input ProcessStoryboardSheetPanelsInput) (_ ProcessStoryboardSheetPanelsOutput, err error) {
	var execution NodeExecution
	defer func() { err = finalizeWorkflowActivityError(ctx, a.db, execution, err) }()
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" ||
		strings.TrimSpace(input.SheetAnchorID) == "" || strings.TrimSpace(input.SheetArtifactID) == "" ||
		strings.TrimSpace(input.SheetMediaFileID) == "" || strings.TrimSpace(input.SheetStorageKey) == "" {
		return ProcessStoryboardSheetPanelsOutput{}, fmt.Errorf("storyboard sheet panel processing input is incomplete")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	manifest, err := a.loadStoryboardSheetManifestForMedia(ctx, input, false)
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	if existing, ok, err := a.findProcessedStoryboardSheetPanels(ctx, input, manifest); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	} else if ok {
		return existing, nil
	}
	objectStore, ok := a.storage.(mediapkg.ObjectStore)
	if !ok {
		return ProcessStoryboardSheetPanelsOutput{}, fmt.Errorf("object storage does not support storyboard sheet cropping")
	}
	execution, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey: nodeKeyForID("storyboard_sheet_panels_crop", manifest.ID), NodeType: "media.storyboard_sheet.crop",
		Input: mustJSON(input),
	})
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	outputPrefix := fmt.Sprintf(
		"organizations/%s/projects/%s/storyboard-shots/%s/panel-manifests/%s",
		input.OrganizationID, input.ProjectID, input.ShotID, manifest.ID,
	)
	cropped, err := mediapkg.CropStoryboardSheet(ctx, mediapkg.StoryboardSheetCropRequest{
		SourceStorageKey: input.SheetStorageKey, OutputPrefix: outputPrefix,
		Rows: manifest.Manifest.Rows, Columns: manifest.Manifest.Columns,
		PanelCount: manifest.Manifest.PanelCount, PanelAspectRatio: manifest.Manifest.VideoAspectRatio,
	}, objectStore)
	if err != nil {
		_ = FailNodeRun(ctx, a.db, execution, codeActivityFailed, err.Error())
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	manifest, err = loadStoryboardSheetManifestForMediaQuery(ctx, tx, input, true)
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	if existing, ok, err := findProcessedStoryboardSheetPanelsTx(ctx, tx, input, manifest); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	} else if ok {
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(existing)); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		} else if !applied {
			return ProcessStoryboardSheetPanelsOutput{}, ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		return existing, nil
	}
	output := ProcessStoryboardSheetPanelsOutput{
		PanelManifestID: manifest.ID, PanelManifestHash: manifest.Manifest.ManifestHash,
		SheetAnchorID: input.SheetAnchorID, SourceWidth: cropped.SourceWidth, SourceHeight: cropped.SourceHeight,
		Panels: make([]ProcessStoryboardSheetPanelOutput, 0, len(cropped.Panels)),
	}
	for _, panel := range cropped.Panels {
		var panelID, anchorID string
		if err := tx.QueryRow(ctx, `
			SELECT id::text, visual_anchor_id::text
			FROM storyboard_sheet_panels
			WHERE manifest_id = $1 AND ordinal = $2 AND status = 'planned'
			FOR UPDATE
		`, manifest.ID, panel.Ordinal).Scan(&panelID, &anchorID); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		metadata := mustJSON(map[string]any{
			"source": "storyboard_sheet_deterministic_crop", "panelManifestId": manifest.ID,
			"panelManifestHash": manifest.Manifest.ManifestHash, "panelOrdinal": panel.Ordinal,
			"sheetAnchorId": input.SheetAnchorID, "sheetArtifactId": input.SheetArtifactID,
			"sheetMediaFileId": input.SheetMediaFileID, "sheetStorageKey": input.SheetStorageKey,
			"crop": panel.Crop,
		})
		var artifactID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO artifacts(
				organization_id, project_id, workflow_run_id, node_run_id, type,
				storage_key, mime_type, content_hash, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, 'storyboard_sheet_panel', $5, 'image/png', $6, $7, NULLIF($8, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID,
			panel.StorageKey, panel.ContentHash, metadata, input.CreatedBy).Scan(&artifactID); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		var mediaFileID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO media_files(
				organization_id, project_id, artifact_id, storage_key, mime_type,
				byte_size, width, height, checksum, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, 'image/png', $5, $6, $7, $8, $9, NULLIF($10, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, artifactID, panel.StorageKey, panel.ByteSize,
			panel.Width, panel.Height, panel.ContentHash, metadata, input.CreatedBy).Scan(&mediaFileID); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_sheet_panels
			SET status = 'cropped', review_status = 'pending', artifact_id = $2,
			    media_file_id = $3, storage_key = $4, content_hash = $5,
			    crop = $6, metadata = metadata || $7::jsonb, updated_at = now()
			WHERE id = $1
		`, panelID, artifactID, mediaFileID, panel.StorageKey, panel.ContentHash,
			mustJSON(panel.Crop), metadata); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'ready', review_status = 'pending', artifact_id = $2,
			    media_file_id = $3, storage_key = $4,
			    metadata = metadata || $5::jsonb, updated_at = now()
			WHERE id = $1
		`, anchorID, artifactID, mediaFileID, panel.StorageKey, metadata); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, err
		}
		output.Panels = append(output.Panels, ProcessStoryboardSheetPanelOutput{
			PanelID: panelID, VisualAnchorID: anchorID, Ordinal: panel.Ordinal,
			ArtifactID: artifactID, MediaFileID: mediaFileID, StorageKey: panel.StorageKey,
			ContentHash: panel.ContentHash, Width: panel.Width, Height: panel.Height, Crop: panel.Crop,
		})
	}
	if len(output.Panels) != manifest.Manifest.PanelCount {
		return ProcessStoryboardSheetPanelsOutput{}, fmt.Errorf("cropped panel count %d does not match manifest %d", len(output.Panels), manifest.Manifest.PanelCount)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_sheet_manifests
		SET status = 'processing', review_status = 'pending',
		    metadata = metadata || jsonb_build_object(
		      'panelsCroppedAt', now(), 'sourceWidth', $2::integer, 'sourceHeight', $3::integer,
		      'sheetArtifactId', $4::text, 'sheetMediaFileId', $5::text, 'sheetStorageKey', $6::text
		    )
		WHERE id = $1
	`, manifest.ID, cropped.SourceWidth, cropped.SourceHeight, input.SheetArtifactID, input.SheetMediaFileID, input.SheetStorageKey); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	var episodeID string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(script_episode_id::text, '')
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND production_generation_id = $3
	`, input.ShotID, input.ProjectID, manifest.ProductionGenerationID).Scan(&episodeID); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	payload := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": input.ShotID,
		"panelManifestId": manifest.ID, "panelManifestHash": manifest.Manifest.ManifestHash,
		"panelCount": len(output.Panels), "productionGenerationId": manifest.ProductionGenerationID,
		"bindingId": project.VideoProductionBindingID, "bindingRevision": project.VideoProductionBindingRevision,
		"episodeId": episodeID,
	})
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.storyboard_sheet.cropped", "storyboard_sheet_manifest", manifest.ID, payload); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	} else if !applied {
		return ProcessStoryboardSheetPanelsOutput{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, err
	}
	return output, nil
}

func (a Activities) loadStoryboardSheetManifestForMedia(ctx context.Context, input ProcessStoryboardSheetPanelsInput, lock bool) (storyboardSheetManifestRuntime, error) {
	return loadStoryboardSheetManifestForMediaQuery(ctx, a.db, input, lock)
}

func loadStoryboardSheetManifestForMediaQuery(ctx context.Context, db storyboardSheetManifestQuerier, input ProcessStoryboardSheetPanelsInput, lock bool) (storyboardSheetManifestRuntime, error) {
	query := `
		SELECT manifest.id::text, manifest.production_generation_id::text,
		       manifest.revision, manifest.status, manifest.review_status, manifest.manifest
		FROM storyboard_sheet_manifests manifest
		JOIN shot_visual_anchors anchor ON anchor.id = manifest.sheet_anchor_id
		JOIN storyboard_shots shot ON shot.id = manifest.storyboard_shot_id
		WHERE manifest.organization_id = $1 AND manifest.project_id = $2
		  AND manifest.storyboard_shot_id = $3 AND manifest.sheet_anchor_id = $4
		  AND manifest.status IN ('draft', 'processing')
		  AND anchor.artifact_id = $5 AND anchor.media_file_id = $6 AND anchor.storage_key = $7
		  AND anchor.status = 'ready' AND anchor.review_status = 'pending'
		  AND shot.production_generation_id = manifest.production_generation_id
		  AND shot.deleted_at IS NULL
		ORDER BY manifest.revision DESC LIMIT 1`
	if lock {
		query += ` FOR UPDATE OF manifest`
	}
	var runtime storyboardSheetManifestRuntime
	var raw []byte
	err := db.QueryRow(ctx, query, input.OrganizationID, input.ProjectID, input.ShotID, input.SheetAnchorID,
		input.SheetArtifactID, input.SheetMediaFileID, input.SheetStorageKey).Scan(
		&runtime.ID, &runtime.ProductionGenerationID, &runtime.Revision,
		&runtime.Status, &runtime.ReviewStatus, &raw,
	)
	if err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if err := json.Unmarshal(raw, &runtime.Manifest); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	return runtime, nil
}

func (a Activities) findProcessedStoryboardSheetPanels(ctx context.Context, input ProcessStoryboardSheetPanelsInput, manifest storyboardSheetManifestRuntime) (ProcessStoryboardSheetPanelsOutput, bool, error) {
	return findProcessedStoryboardSheetPanelsQuery(ctx, a.db, input, manifest, false)
}

func findProcessedStoryboardSheetPanelsTx(ctx context.Context, tx pgx.Tx, input ProcessStoryboardSheetPanelsInput, manifest storyboardSheetManifestRuntime) (ProcessStoryboardSheetPanelsOutput, bool, error) {
	return findProcessedStoryboardSheetPanelsQuery(ctx, tx, input, manifest, true)
}

type storyboardSheetPanelQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func findProcessedStoryboardSheetPanelsQuery(ctx context.Context, db storyboardSheetPanelQuerier, input ProcessStoryboardSheetPanelsInput, manifest storyboardSheetManifestRuntime, lock bool) (ProcessStoryboardSheetPanelsOutput, bool, error) {
	query := `
		SELECT panel.id::text, panel.visual_anchor_id::text, panel.ordinal,
		       panel.artifact_id::text, panel.media_file_id::text, panel.storage_key,
		       panel.content_hash, media.width, media.height,
		       COALESCE((panel.crop->>'x')::integer, 0), COALESCE((panel.crop->>'y')::integer, 0),
		       COALESCE((panel.crop->>'width')::integer, 0), COALESCE((panel.crop->>'height')::integer, 0),
		       COALESCE((manifest.metadata->>'sourceWidth')::integer, 0),
		       COALESCE((manifest.metadata->>'sourceHeight')::integer, 0)
		FROM storyboard_sheet_panels panel
		JOIN media_files media ON media.id = panel.media_file_id
		JOIN storyboard_sheet_manifests manifest ON manifest.id = panel.manifest_id
		WHERE panel.manifest_id = $1 AND panel.status = 'cropped'
		ORDER BY panel.ordinal`
	if lock {
		query += ` FOR UPDATE OF panel`
	}
	rows, err := db.Query(ctx, query, manifest.ID)
	if err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, false, err
	}
	defer rows.Close()
	output := ProcessStoryboardSheetPanelsOutput{
		PanelManifestID: manifest.ID, PanelManifestHash: manifest.Manifest.ManifestHash,
		SheetAnchorID: input.SheetAnchorID, Panels: []ProcessStoryboardSheetPanelOutput{},
	}
	for rows.Next() {
		var panel ProcessStoryboardSheetPanelOutput
		if err := rows.Scan(
			&panel.PanelID, &panel.VisualAnchorID, &panel.Ordinal, &panel.ArtifactID,
			&panel.MediaFileID, &panel.StorageKey, &panel.ContentHash, &panel.Width, &panel.Height,
			&panel.Crop.X, &panel.Crop.Y, &panel.Crop.Width, &panel.Crop.Height,
			&output.SourceWidth, &output.SourceHeight,
		); err != nil {
			return ProcessStoryboardSheetPanelsOutput{}, false, err
		}
		output.Panels = append(output.Panels, panel)
	}
	if err := rows.Err(); err != nil {
		return ProcessStoryboardSheetPanelsOutput{}, false, err
	}
	if len(output.Panels) == 0 {
		return ProcessStoryboardSheetPanelsOutput{}, false, nil
	}
	if len(output.Panels) != manifest.Manifest.PanelCount {
		return ProcessStoryboardSheetPanelsOutput{}, false, errors.New("storyboard sheet has a partial persisted crop result")
	}
	return output, true, nil
}
