package workflows

import (
	"context"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

type renderedVideoProductionPrompt struct {
	Rendered promptsvc.RenderedPrompt
	Contract videoproduction.CompiledPromptContract
}

func (a Activities) renderVideoProductionPromptContract(
	ctx context.Context,
	organizationID, projectID string,
	project ProjectProductionSettings,
	role string,
	contextPlan videoproduction.PromptContextPlan,
	contract shotProductionContractContext,
	referencePack videoproduction.ReferencePack,
	extraContext map[string]any,
) (renderedVideoProductionPrompt, error) {
	templateKey := "video_profile." + project.VideoProductionProfileKey + "." + role
	resolved, err := a.renderWorkflowPrompt(ctx, organizationID, projectID, templateKey, map[string]any{
		"input": map[string]any{"context": "{}"},
	})
	if err != nil {
		return renderedVideoProductionPrompt{}, err
	}
	layers := []videoproduction.PromptContractLayer{{
		LayerKey:    templateKey,
		VersionID:   resolved.PromptVersionID,
		ContentHash: resolved.ContentHash,
		Source:      resolved.Source,
	}}
	for _, manual := range []struct {
		key, content string
	}{
		{key: "project.director_manual", content: project.DirectorManual},
		{key: "project.visual_manual", content: project.VisualManual},
	} {
		if strings.TrimSpace(manual.content) == "" {
			continue
		}
		hash, hashErr := videoproduction.HashCanonicalContract(strings.TrimSpace(manual.content))
		if hashErr != nil {
			return renderedVideoProductionPrompt{}, hashErr
		}
		layers = append(layers, videoproduction.PromptContractLayer{
			LayerKey: manual.key, ContentHash: hash, Source: "project_snapshot",
		})
	}
	compiled, err := videoproduction.CompilePromptContract(videoproduction.PromptContractInput{
		ProfileKey:             project.VideoProductionProfileKey,
		ProfileVersionID:       project.VideoProductionProfileVersionID,
		ProfileSnapshotHash:    project.VideoProductionProfileHash,
		Role:                   role,
		InputContractVersion:   project.VideoProductionInputContract,
		ContextPlan:            contextPlan,
		ShotState:              contract.AnchorState,
		Transition:             &contract.Transition,
		ReferencePack:          referencePack,
		CapabilitySnapshotHash: referencePack.CapabilitySnapshotHash,
		Layers:                 layers,
	})
	if err != nil {
		return renderedVideoProductionPrompt{}, err
	}
	contractContext := map[string]any{
		"contract":   compiled.Context,
		"provenance": compiled.Provenance,
	}
	for key, value := range extraContext {
		contractContext[key] = value
	}
	finalRendered, err := a.renderWorkflowPromptVersion(ctx, organizationID, projectID, templateKey, resolved.PromptVersionID, map[string]any{
		"input": map[string]any{"context": contractContext},
	})
	if err != nil {
		return renderedVideoProductionPrompt{}, err
	}
	if role == videoproduction.PromptRoleVideoGenerate || role == videoproduction.PromptRoleVideoReview {
		finalRendered = applyVideoPromptAudioRuntimeContract(finalRendered)
	}
	return renderedVideoProductionPrompt{Rendered: finalRendered, Contract: compiled}, nil
}
