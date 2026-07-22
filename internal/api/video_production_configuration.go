package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

type videoProductionConfigurationRequest struct {
	ProjectType                   *string         `json:"projectType"`
	ContentType                   *string         `json:"contentType"`
	AspectRatio                   *string         `json:"aspectRatio"`
	VideoRatio                    *string         `json:"videoRatio"`
	ArtStyle                      *string         `json:"artStyle"`
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
}

func (s *Server) resolveTargetProductionConfiguration(
	ctx context.Context,
	db videoProductionQueryer,
	organizationID string,
	projectID string,
	req videoProductionConfigurationRequest,
) (videoproduction.ProductionConfigurationSnapshot, error) {
	active, err := videoproduction.LoadActiveContext(ctx, db, projectID)
	if err != nil {
		return videoproduction.ProductionConfigurationSnapshot{}, err
	}
	configuration, err := videoproduction.DecodeProductionConfiguration(active.Binding.ProfileSnapshot)
	if err != nil {
		var typed videoproduction.Error
		if !errors.As(err, &typed) || typed.Code != videoproduction.CodeConfigurationRebuildRequired {
			return videoproduction.ProductionConfigurationSnapshot{}, err
		}
		configuration, err = videoproduction.LoadProductionConfiguration(ctx, db, projectID)
		if err != nil {
			return videoproduction.ProductionConfigurationSnapshot{}, err
		}
	}
	assignString := func(target *string, source *string) {
		if source != nil {
			*target = strings.TrimSpace(*source)
		}
	}
	assignString(&configuration.ProjectType, req.ProjectType)
	assignString(&configuration.ContentType, req.ContentType)
	assignString(&configuration.AspectRatio, req.AspectRatio)
	assignString(&configuration.VideoRatio, req.VideoRatio)
	assignString(&configuration.ArtStyle, req.ArtStyle)
	assignString(&configuration.ImageModelProfileKey, req.ImageModelProfileKey)
	assignString(&configuration.VideoModelProfileKey, req.VideoModelProfileKey)
	assignString(&configuration.ScriptModelProfileKey, req.ScriptModelProfileKey)
	assignString(&configuration.TTSModelProfileKey, req.TTSModelProfileKey)
	assignString(&configuration.ASRModelProfileKey, req.ASRModelProfileKey)
	assignString(&configuration.AudioStrategy, req.AudioStrategy)
	assignString(&configuration.AudioRequirement, req.AudioRequirement)
	assignString(&configuration.ImageQuality, req.ImageQuality)
	if req.TimelineTimebase != nil {
		configuration.TimelineTimebase = *req.TimelineTimebase
	}
	if req.FPSNumerator != nil {
		configuration.FPSNumerator = *req.FPSNumerator
	}
	if req.FPSDenominator != nil {
		configuration.FPSDenominator = *req.FPSDenominator
	}
	if len(req.Settings) > 0 {
		configuration.Settings = req.Settings
	}
	if !validProjectTimebase(configuration.TimelineTimebase, configuration.FPSNumerator, configuration.FPSDenominator) {
		return videoproduction.ProductionConfigurationSnapshot{}, videoproduction.NewError(
			videoproduction.CodeRebuildConflict,
			"时间基准和帧率必须为正数且能够精确换算",
			false,
		)
	}
	if !validProjectAudioSettings(configuration.AudioStrategy, configuration.AudioRequirement) {
		return videoproduction.ProductionConfigurationSnapshot{}, videoproduction.NewError(
			videoproduction.CodeRebuildConflict,
			"音频策略或原生音频要求无效",
			false,
		)
	}
	if req.DirectorManualPromptVersionID != nil {
		configuration, err = videoproduction.SetProductionManualVersion(ctx, db, configuration, organizationID, "director", *req.DirectorManualPromptVersionID)
		if err != nil {
			return videoproduction.ProductionConfigurationSnapshot{}, err
		}
	}
	if req.VisualManualPromptVersionID != nil {
		configuration, err = videoproduction.SetProductionManualVersion(ctx, db, configuration, organizationID, "visual", *req.VisualManualPromptVersionID)
		if err != nil {
			return videoproduction.ProductionConfigurationSnapshot{}, err
		}
	}
	return videoproduction.NormalizeProductionConfiguration(configuration)
}

func projectWithProductionConfiguration(project Project, configuration videoproduction.ProductionConfigurationSnapshot) Project {
	project.ProjectType = optionalString(configuration.ProjectType)
	project.ContentType = optionalString(configuration.ContentType)
	project.AspectRatio = optionalString(configuration.AspectRatio)
	project.VideoRatio = configuration.VideoRatio
	project.ArtStyle = configuration.ArtStyle
	project.DirectorManual = configuration.DirectorManual
	project.VisualManual = configuration.VisualManual
	project.ImageModelProfileKey = configuration.ImageModelProfileKey
	project.VideoModelProfileKey = configuration.VideoModelProfileKey
	project.ScriptModelProfileKey = configuration.ScriptModelProfileKey
	project.TTSModelProfileKey = configuration.TTSModelProfileKey
	project.ASRModelProfileKey = configuration.ASRModelProfileKey
	project.AudioStrategy = configuration.AudioStrategy
	project.AudioRequirement = configuration.AudioRequirement
	project.ImageQuality = configuration.ImageQuality
	project.TimelineTimebase = configuration.TimelineTimebase
	project.FPSNumerator = configuration.FPSNumerator
	project.FPSDenominator = configuration.FPSDenominator
	project.Settings = configuration.Settings
	return project
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
