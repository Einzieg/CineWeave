package workflows

import (
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"go.temporal.io/sdk/workflow"
)

func resolveShotAnchorWorkItemsForWorkflow(
	ctx workflow.Context,
	input TextToStoryboardInput,
	shotIDs []string,
) ([]ShotAnchorWorkItem, error) {
	version := workflow.GetVersion(ctx, "profile-anchor-work-items-v1", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		items := make([]ShotAnchorWorkItem, 0, len(shotIDs))
		for _, shotID := range shotIDs {
			items = append(items, ShotAnchorWorkItem{ShotID: shotID})
		}
		return items, nil
	}
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var items []ShotAnchorWorkItem
	if err := workflow.ExecuteActivity(activityCtx, "ResolveShotAnchorWorkItems", ResolveShotAnchorWorkItemsInput{
		ProjectID:     input.ProjectID,
		WorkflowRunID: input.WorkflowRunID,
		ShotIDs:       shotIDs,
	}).Get(activityCtx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

type shotAnchorWorkItemOutcome struct {
	Item ShotAnchorWorkItem
	Err  error
}

func finalizeStoryboardSheetImage(ctx workflow.Context, input TextToStoryboardInput, image GenerateShotImageOutput) error {
	if image.AnchorRole != videoproduction.AnchorRoleStoryboardSheet {
		return nil
	}
	mediaCtx := workflow.WithActivityOptions(ctx, mediaProcessingActivityOptions())
	var panels ProcessStoryboardSheetPanelsOutput
	if err := workflow.ExecuteActivity(mediaCtx, "ProcessStoryboardSheetPanels", ProcessStoryboardSheetPanelsInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ShotID: image.ShotID, SheetAnchorID: image.VisualAnchorID,
		SheetArtifactID: image.ImageArtifactID, SheetMediaFileID: image.ImageMediaFileID,
		SheetStorageKey: image.ImageStorageKey,
	}).Get(mediaCtx, &panels); err != nil {
		return err
	}
	reviewCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var review ReviewStoryboardSheetOutput
	if err := workflow.ExecuteActivity(reviewCtx, "ReviewStoryboardSheetOutput", ReviewStoryboardSheetOutputInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ShotID: image.ShotID, SheetAnchorID: image.VisualAnchorID,
		SheetArtifactID: image.ImageArtifactID, SheetMediaFileID: image.ImageMediaFileID,
		SheetStorageKey: image.ImageStorageKey, PanelManifestID: panels.PanelManifestID,
	}).Get(reviewCtx, &review); err != nil {
		return err
	}
	if !review.Approved {
		return fmt.Errorf("分镜板实际成图审核未通过")
	}
	return nil
}

func summarizeShotAnchorWorkItemOutcomes(
	shotIDs []string,
	outcomes []shotAnchorWorkItemOutcome,
) (succeeded, failed []string, messages, codes map[string]string) {
	messages = map[string]string{}
	codes = map[string]string{}
	failedByShot := map[string][]string{}
	failedCodesByShot := map[string][]string{}
	seenByShot := map[string]int{}
	for _, outcome := range outcomes {
		seenByShot[outcome.Item.ShotID]++
		if outcome.Err == nil {
			continue
		}
		role := strings.TrimSpace(outcome.Item.AnchorRole)
		if role == "" {
			role = "planned_first_frame"
		}
		code, message := workflowExecutionError(outcome.Err)
		failedByShot[outcome.Item.ShotID] = append(
			failedByShot[outcome.Item.ShotID],
			fmt.Sprintf("%s：%s", role, message),
		)
		failedCodesByShot[outcome.Item.ShotID] = append(failedCodesByShot[outcome.Item.ShotID], code)
	}
	for _, shotID := range shotIDs {
		if failures := failedByShot[shotID]; len(failures) > 0 {
			failed = append(failed, shotID)
			messages[shotID] = strings.Join(failures, "；")
			codes[shotID] = commonWorkflowErrorCode(failedCodesByShot[shotID])
			continue
		}
		if seenByShot[shotID] > 0 {
			succeeded = append(succeeded, shotID)
		}
	}
	return succeeded, failed, messages, codes
}

func commonWorkflowErrorCode(codes []string) string {
	common := ""
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			code = codeActivityFailed
		}
		if common == "" {
			common = code
			continue
		}
		if common != code {
			return codeActivityFailed
		}
	}
	if common == "" {
		return codeActivityFailed
	}
	return common
}
