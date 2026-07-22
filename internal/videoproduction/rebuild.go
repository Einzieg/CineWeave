package videoproduction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func BuildRebuildImpact(ctx context.Context, db queryer, projectID string, target ProfileVersion, targetConfiguration ProductionConfigurationSnapshot) (RebuildImpact, error) {
	if !target.Available() {
		return RebuildImpact{}, Error{Code: CodeProfileUnavailable, Message: "目标视频生产方案暂不可用"}
	}
	active, err := LoadActiveContext(ctx, db, projectID)
	if err != nil {
		return RebuildImpact{}, err
	}
	var impact RebuildImpact
	impact.ProjectID = projectID
	impact.SourceBindingID = active.Binding.ID
	impact.SourceBindingRevision = active.Binding.Revision
	impact.SourceGenerationID = active.Generation.ID
	impact.SourceGenerationNo = active.Generation.GenerationNo
	impact.TargetProfileVersionID = target.ID
	impact.TargetProfileKey = target.ProfileKey
	impact.TargetProfileVersion = target.Version
	targetConfiguration, targetConfigurationHash, err := ProductionConfigurationHash(targetConfiguration)
	if err != nil {
		return RebuildImpact{}, err
	}
	impact.TargetConfiguration = targetConfiguration
	impact.TargetConfigurationHash = targetConfigurationHash
	currentConfigurationHash := ""
	if currentConfiguration, decodeErr := DecodeProductionConfiguration(active.Binding.ProfileSnapshot); decodeErr == nil {
		_, currentConfigurationHash, err = ProductionConfigurationHash(currentConfiguration)
		if err != nil {
			return RebuildImpact{}, err
		}
	} else if typed, ok := AsError(decodeErr); !ok || typed.Code != CodeConfigurationRebuildRequired {
		return RebuildImpact{}, decodeErr
	}
	profileChanged := target.ID != active.Binding.ProfileVersionID
	configurationChanged := currentConfigurationHash == "" || currentConfigurationHash != targetConfigurationHash
	switch {
	case profileChanged && configurationChanged:
		impact.Reason = "profile_and_configuration_change"
	case profileChanged:
		impact.Reason = "profile_change"
	case configurationChanged:
		impact.Reason = "configuration_change"
	default:
		return RebuildImpact{}, Error{Code: CodeRebuildConflict, Message: "目标视频生产方案和配置均未变化"}
	}
	impact.Episodes = make([]RebuildEpisodeImpact, 0)
	if err := db.QueryRow(ctx, `SELECT revision FROM projects WHERE id = $1`, projectID).Scan(&impact.ExpectedProjectRevision); err != nil {
		return RebuildImpact{}, err
	}
	if err := db.QueryRow(ctx, `
		SELECT script.id::text, script.current_version_id::text
		FROM scripts script
		JOIN projects project ON project.id = script.project_id
		WHERE script.project_id = $1
		  AND script.current_version_id IS NOT NULL
		  AND script.status = 'active'
		ORDER BY CASE WHEN script.id = project.active_script_id THEN 0 ELSE 1 END,
		         script.updated_at DESC, script.created_at DESC
		LIMIT 1
	`, projectID).Scan(&impact.ScriptID, &impact.ScriptVersionID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return RebuildImpact{}, err
	}
	if impact.ScriptID != "" {
		rows, err := db.Query(ctx, `
		SELECT episode.id::text, episode.episode_index, episode.revision, episode.content_hash,
		       plan.id::text
		FROM script_episodes episode
		LEFT JOIN LATERAL (
			SELECT candidate.id
			FROM storyboard_plans candidate
			WHERE candidate.project_id = episode.project_id
			  AND candidate.script_episode_id = episode.id
			  AND candidate.production_generation_id = $4
			  AND candidate.active = true
			ORDER BY candidate.revision DESC, candidate.created_at DESC
			LIMIT 1
		) plan ON true
		WHERE episode.project_id = $1
		  AND episode.script_id = $2
		  AND episode.script_version_id = $3
		ORDER BY episode.episode_index, episode.id
		`, projectID, impact.ScriptID, impact.ScriptVersionID, impact.SourceGenerationID)
		if err != nil {
			return RebuildImpact{}, err
		}
		for rows.Next() {
			var episode RebuildEpisodeImpact
			if err := rows.Scan(
				&episode.ScriptEpisodeID,
				&episode.EpisodeOrdinal,
				&episode.ScriptEpisodeRevision,
				&episode.ScriptEpisodeHash,
				&episode.SourceStoryboardPlanID,
			); err != nil {
				rows.Close()
				return RebuildImpact{}, err
			}
			impact.Episodes = append(impact.Episodes, episode)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return RebuildImpact{}, err
		}
	}
	if err := db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM storyboard_plans WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM storyboard_shots WHERE project_id = $1 AND production_generation_id = $2 AND deleted_at IS NULL),
			(SELECT count(*) FROM shot_asset_requirements WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM storyboard_shots WHERE project_id = $1 AND production_generation_id = $2 AND image_artifact_id IS NOT NULL),
			(SELECT count(*) FROM storyboard_shots WHERE project_id = $1 AND production_generation_id = $2 AND video_artifact_id IS NOT NULL),
			(SELECT count(*) FROM video_render_plans WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM project_timelines WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM timeline_clips WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM final_video_versions WHERE project_id = $1 AND production_generation_id = $2),
			(SELECT count(*) FROM canonical_assets WHERE project_id = $1 AND status <> 'archived')
	`, projectID, impact.SourceGenerationID).Scan(
		&impact.Counts.StoryboardPlans,
		&impact.Counts.StoryboardShots,
		&impact.Counts.ShotRequirements,
		&impact.Counts.ShotImages,
		&impact.Counts.ShotVideos,
		&impact.Counts.VideoRenderPlans,
		&impact.Counts.Timelines,
		&impact.Counts.TimelineClips,
		&impact.Counts.FinalVideos,
		&impact.Counts.RetainedAssets,
	); err != nil {
		return RebuildImpact{}, err
	}
	impact.Counts.Episodes = len(impact.Episodes)
	if impact.Counts.StoryboardPlans > 0 && len(impact.Episodes) == 0 {
		return RebuildImpact{}, Error{Code: CodeRebuildConflict, Message: "项目存在分镜数据，但没有可用于重建的活动剧本"}
	}
	token, err := rebuildImpactToken(impact)
	if err != nil {
		return RebuildImpact{}, err
	}
	impact.ImpactToken = token
	return impact, nil
}

func VerifyRebuildImpact(expected RebuildImpact, submittedRevision int64, submittedToken string) error {
	if expected.ExpectedProjectRevision != submittedRevision || expected.ImpactToken != submittedToken {
		return Error{Code: CodeRebuildImpactStale, Message: "视频生产方案重建影响已变化，请重新确认"}
	}
	return nil
}

func rebuildImpactToken(impact RebuildImpact) (string, error) {
	impact.ImpactToken = ""
	raw, err := json.Marshal(impact)
	if err != nil {
		return "", fmt.Errorf("marshal rebuild impact: %w", err)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}
