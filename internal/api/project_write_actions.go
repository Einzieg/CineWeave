package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type projectUpdateActionInput struct {
	Name                          *string         `json:"name"`
	Description                   *string         `json:"description"`
	ProjectType                   *string         `json:"projectType"`
	ContentType                   *string         `json:"contentType"`
	AspectRatio                   *string         `json:"aspectRatio"`
	VideoRatio                    *string         `json:"videoRatio"`
	ArtStyle                      *string         `json:"artStyle"`
	DirectorManual                *string         `json:"directorManual"`
	VisualManual                  *string         `json:"visualManual"`
	DirectorManualPromptVersionID *string         `json:"directorManualPromptVersionId"`
	VisualManualPromptVersionID   *string         `json:"visualManualPromptVersionId"`
	ImageModelProfileKey          *string         `json:"imageModelProfileKey"`
	VideoModelProfileKey          *string         `json:"videoModelProfileKey"`
	ScriptModelProfileKey         *string         `json:"scriptModelProfileKey"`
	TTSModelProfileKey            *string         `json:"ttsModelProfileKey"`
	ASRModelProfileKey            *string         `json:"asrModelProfileKey"`
	AudioStrategy                 *string         `json:"audioStrategy"`
	AudioRequirement              *string         `json:"audioRequirement"`
	ImageQuality                  *string         `json:"imageQuality"`
	TimelineTimebase              *int64          `json:"timelineTimebase"`
	FPSNumerator                  *int            `json:"fpsNumerator"`
	FPSDenominator                *int            `json:"fpsDenominator"`
	Settings                      json.RawMessage `json:"settings"`
	ExpectedRevision              int64           `json:"expectedRevision"`
}

func decodeProjectUpdateActionInput(raw json.RawMessage) (projectUpdateActionInput, error) {
	var input projectUpdateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return projectUpdateActionInput{}, controlValidationError("project.update 输入格式无效")
	}
	if input.ExpectedRevision <= 0 {
		return projectUpdateActionInput{}, controlValidationError("expectedRevision 必须为正整数")
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) == "" {
		return projectUpdateActionInput{}, controlValidationError("项目名称不能为空")
	}
	if input.hasProductionConfiguration() {
		return projectUpdateActionInput{}, videoproduction.NewError(
			videoproduction.CodeConfigurationRebuildRequired,
			"视频生产配置必须先分析影响并确认换代",
			false,
		)
	}
	return input, nil
}

func (input projectUpdateActionInput) hasProductionConfiguration() bool {
	return input.ProjectType != nil || input.ContentType != nil || input.AspectRatio != nil ||
		input.VideoRatio != nil || input.ArtStyle != nil || input.DirectorManual != nil || input.VisualManual != nil ||
		input.DirectorManualPromptVersionID != nil || input.VisualManualPromptVersionID != nil ||
		input.ImageModelProfileKey != nil || input.VideoModelProfileKey != nil || input.ScriptModelProfileKey != nil ||
		input.TTSModelProfileKey != nil || input.ASRModelProfileKey != nil || input.AudioStrategy != nil ||
		input.AudioRequirement != nil || input.ImageQuality != nil || input.TimelineTimebase != nil ||
		input.FPSNumerator != nil || input.FPSDenominator != nil || len(input.Settings) > 0
}

func (s *Server) updateProjectActionTx(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, input projectUpdateActionInput) (Project, error) {
	item, err := scanProject(tx.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		return Project{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return Project{}, projectRevisionConflict(item, input.ExpectedRevision)
	}
	if input.Name != nil || input.Description != nil {
		command, err := tx.Exec(ctx, `
			UPDATE projects
			SET name = COALESCE($2, name),
			    description = COALESCE($3, description),
			    revision = revision + 1,
			    updated_at = now()
			WHERE id = $1 AND revision = $4
		`, project.ID, normalizedOptionalString(input.Name), input.Description, item.Revision)
		if err != nil {
			return Project{}, err
		}
		if command.RowsAffected() != 1 {
			return Project{}, projectRevisionConflict(item, input.ExpectedRevision)
		}
		item, err = scanProject(tx.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1`), project.ID))
		if err != nil {
			return Project{}, err
		}
	}
	if err := s.attachVideoProductionContext(ctx, tx, &item); err != nil {
		return Project{}, err
	}
	return item, nil
}

func projectUpdateAgentResult(args map[string]any, project Project) agentToolResult {
	return agentToolOK("project.update", args, fmt.Sprintf("项目基本信息已保存，当前 revision 为 %d。", project.Revision), map[string]any{"project": project})
}

func validateProjectUpdateActionCommand(projectID, actorUserID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(actorUserID) == "" {
		return newAPIError(http.StatusUnprocessableEntity, "PROJECT_CONTROL_CONTEXT_INVALID", "project.update 缺少项目或执行用户")
	}
	return nil
}
