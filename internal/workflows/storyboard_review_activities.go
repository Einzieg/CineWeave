package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
)

const (
	promptKeyStoryboardPlanReviewer = "storyboard_plan_reviewer"
	nodeReviewStoryboardPlanPrefix  = "storyboard_plan_review"
)

type ReviewStoryboardPlanInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	CreatedBy        string `json:"createdBy"`
	ScriptEpisodeID  string `json:"scriptEpisodeId"`
	StoryboardPlanID string `json:"storyboardPlanId"`
	ReviewAttempt    int    `json:"reviewAttempt"`
}

type ReviewStoryboardPlanOutput struct {
	ReviewID               string                                       `json:"reviewId"`
	StoryboardPlanID       string                                       `json:"storyboardPlanId"`
	ReviewAttempt          int                                          `json:"reviewAttempt"`
	Approved               bool                                         `json:"approved"`
	Issues                 []storyboardpkg.StoryboardReviewerIssue      `json:"issues"`
	Corrections            []storyboardpkg.StoryboardCorrection         `json:"corrections"`
	NeedsRevisionSceneKeys []string                                     `json:"needsRevisionSceneKeys,omitempty"`
	DeterministicReport    storyboardpkg.StoryboardPlanValidationReport `json:"deterministicReport"`
	ProviderCallID         string                                       `json:"providerCallId,omitempty"`
	ModelID                string                                       `json:"modelId,omitempty"`
	PromptVersionID        string                                       `json:"promptVersionId,omitempty"`
	PromptHash             string                                       `json:"promptHash,omitempty"`
}

func (a Activities) ReviewStoryboardPlan(ctx context.Context, input ReviewStoryboardPlanInput) (ReviewStoryboardPlanOutput, error) {
	if input.ReviewAttempt <= 0 {
		input.ReviewAttempt = 1
	}
	if strings.TrimSpace(input.StoryboardPlanID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" {
		return ReviewStoryboardPlanOutput{}, fmt.Errorf("storyboardPlanId and scriptEpisodeId are required")
	}
	nodeKey := storyboardReviewNodeKey(input.StoryboardPlanID, input.ReviewAttempt)
	if existing, ok, err := a.existingStoryboardReviewOutput(ctx, input.WorkflowRunID, nodeKey); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	} else if ok {
		return existing, nil
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "agent.storyboard_plan_review",
		Input: mustJSON(map[string]any{
			"storyboardPlanId": input.StoryboardPlanID,
			"reviewAttempt":    input.ReviewAttempt,
		}),
	})
	if err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	report, err := a.stitchAndValidateStoryboardPlan(ctx, input, nodeExecution)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	reviewContext, sceneKeys, unitIDs, shotCount, err := a.storyboardReviewContext(ctx, input)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardPlanReviewer, map[string]any{
		"context": map[string]any{"json": string(reviewContext)},
	})
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	if a.gateway == nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, provider.CodeProviderGatewayRequired, "provider gateway client is not configured")
	}
	gatewayResp, err := a.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeExecution.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 12_000}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		workflowErr := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(workflowErr, codeActivityFailed)
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, code, message)
	}
	review, err := storyboardpkg.ParseStoryboardReviewerOutput(stripJSONFence(gatewayResp.Output.Text), sceneKeys, unitIDs, shotCount)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, provider.CodeInvalidRequest, err.Error())
	}
	output, err := a.storeStoryboardReview(ctx, input, nodeExecution, rendered.PromptVersionID, rendered.RenderedHash, gatewayResp, report, review)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, a.failStoryboardReview(ctx, input, nodeExecution, codeActivityFailed, err.Error())
	}
	return output, nil
}

func storyboardReviewNodeKey(storyboardPlanID string, reviewAttempt int) string {
	return fmt.Sprintf("%s_attempt_%d", nodeKeyForID(nodeReviewStoryboardPlanPrefix, storyboardPlanID), reviewAttempt)
}

func (a Activities) stitchAndValidateStoryboardPlan(ctx context.Context, input ReviewStoryboardPlanInput, execution NodeExecution) (storyboardpkg.StoryboardPlanValidationReport, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	var pendingCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM storyboard_scene_plans
		WHERE storyboard_plan_id = $1 AND status <> 'ready'
	`, input.StoryboardPlanID).Scan(&pendingCount); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	if pendingCount > 0 {
		return storyboardpkg.StoryboardPlanValidationReport{}, fmt.Errorf("storyboard plan still has %d incomplete scene plans", pendingCount)
	}
	if _, err := tx.Exec(ctx, `
		WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY start_tick, end_tick, id) - 1 AS ordinal
			FROM storyboard_shots
			WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		)
		UPDATE storyboard_shots shot
		SET shot_index = -ordered.ordinal - 1,
		    shot_no = ordered.ordinal + 1,
		    episode_shot_index = ordered.ordinal
		FROM ordered
		WHERE shot.id = ordered.id
	`, input.StoryboardPlanID); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET shot_index = -shot_index - 1
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
	`, input.StoryboardPlanID); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET status = 'reviewing',
		    actual_shot_count = (
		      SELECT COUNT(*) FROM storyboard_shots WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		    )
		WHERE id = $1 AND status IN ('planning', 'reviewing')
	`, input.StoryboardPlanID); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	report, err := storyboardpkg.ValidateStoryboardPlanTx(ctx, tx, input.ProjectID, input.ScriptEpisodeID, input.StoryboardPlanID)
	if err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.plan.reviewing", "storyboard_plan", input.StoryboardPlanID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "reviewAttempt": input.ReviewAttempt, "deterministicReport": report,
	})); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return storyboardpkg.StoryboardPlanValidationReport{}, err
	}
	return report, nil
}

func (a Activities) storyboardReviewContext(ctx context.Context, input ReviewStoryboardPlanInput) (json.RawMessage, []string, []string, int, error) {
	rows, err := a.db.Query(ctx, `
		SELECT shot.id::text, shot.shot_index, shot.start_tick, shot.end_tick,
		       COALESCE(shot.title, ''), COALESCE(shot.visual, ''), COALESCE(shot.camera, ''),
		       COALESCE(shot.motion, ''), COALESCE(shot.mood, ''), shot.one_take,
		       shot.script_dialogue, shot.metadata->>'sceneKey'
		FROM storyboard_shots shot
		WHERE shot.storyboard_plan_id = $1 AND shot.deleted_at IS NULL
		ORDER BY shot.shot_index
	`, input.StoryboardPlanID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	shots := make([]map[string]any, 0)
	sceneSet := map[string]bool{}
	for rows.Next() {
		var id, title, visual, camera, motion, mood, sceneKey string
		var ordinal int
		var startTick, endTick int64
		var oneTake bool
		var dialogue json.RawMessage
		if err := rows.Scan(&id, &ordinal, &startTick, &endTick, &title, &visual, &camera, &motion, &mood, &oneTake, &dialogue, &sceneKey); err != nil {
			rows.Close()
			return nil, nil, nil, 0, err
		}
		sceneSet[sceneKey] = true
		shots = append(shots, map[string]any{
			"id": id, "shotOrdinal": ordinal, "sceneKey": sceneKey, "startTick": startTick, "endTick": endTick,
			"title": title, "visual": visual, "camera": camera, "motion": motion, "mood": mood,
			"oneTake": oneTake, "scriptDialogue": dialogue,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, nil, 0, err
	}
	rows.Close()
	unitRows, err := a.db.Query(ctx, `
		SELECT unit.id::text, unit.unit_ordinal, unit.unit_type, unit.track, COALESCE(unit.speaker, ''),
		       unit.source_text, COALESCE(unit.delivery, ''), unit.source_start_offset, unit.source_end_offset,
		       unit.start_tick, unit.end_tick, unit.metadata->>'sceneKey'
		FROM script_timing_units unit
		JOIN storyboard_plans plan ON plan.timing_analysis_id = unit.timing_analysis_id
		WHERE plan.id = $1
		ORDER BY unit.unit_ordinal
	`, input.StoryboardPlanID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	units := make([]map[string]any, 0)
	unitIDs := make([]string, 0)
	for unitRows.Next() {
		var id, unitType, track, speaker, text, delivery, sceneKey string
		var ordinal int
		var sourceStart, sourceEnd *int
		var startTick, endTick int64
		if err := unitRows.Scan(&id, &ordinal, &unitType, &track, &speaker, &text, &delivery, &sourceStart, &sourceEnd, &startTick, &endTick, &sceneKey); err != nil {
			unitRows.Close()
			return nil, nil, nil, 0, err
		}
		unitIDs = append(unitIDs, id)
		units = append(units, map[string]any{
			"id": id, "ordinal": ordinal, "type": unitType, "track": track, "speaker": speaker,
			"sourceText": text, "delivery": delivery, "sourceStartOffset": sourceStart,
			"sourceEndOffset": sourceEnd, "startTick": startTick, "endTick": endTick, "sceneKey": sceneKey,
		})
	}
	if err := unitRows.Err(); err != nil {
		unitRows.Close()
		return nil, nil, nil, 0, err
	}
	unitRows.Close()
	sceneKeys := make([]string, 0, len(sceneSet))
	for key := range sceneSet {
		sceneKeys = append(sceneKeys, key)
	}
	sort.Strings(sceneKeys)
	return mustJSON(map[string]any{
		"storyboardPlanId": input.StoryboardPlanID,
		"scriptEpisodeId":  input.ScriptEpisodeID,
		"timingUnits":      units,
		"shots":            shots,
	}), sceneKeys, unitIDs, len(shots), nil
}

func (a Activities) storeStoryboardReview(
	ctx context.Context,
	input ReviewStoryboardPlanInput,
	execution NodeExecution,
	promptVersionID, promptHash string,
	gatewayResp provider.GatewayTextResponse,
	report storyboardpkg.StoryboardPlanValidationReport,
	review storyboardpkg.StoryboardReviewerOutput,
) (ReviewStoryboardPlanOutput, error) {
	needsRevision := reviewRevisionSceneKeys(review)
	output := ReviewStoryboardPlanOutput{
		StoryboardPlanID:       input.StoryboardPlanID,
		ReviewAttempt:          input.ReviewAttempt,
		Approved:               review.Approved,
		Issues:                 review.Issues,
		Corrections:            review.Corrections,
		NeedsRevisionSceneKeys: needsRevision,
		DeterministicReport:    report,
		ProviderCallID:         gatewayResp.ProviderCallID,
		ModelID:                gatewayResp.ModelID,
		PromptVersionID:        promptVersionID,
		PromptHash:             promptHash,
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	nodeRunID := execution.NodeRunID
	status := "changes_requested"
	planStatus := "planning"
	if review.Approved {
		status = "approved"
		planStatus = "ready"
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_plan_reviews(
			organization_id, project_id, storyboard_plan_id, revision, status, approved,
			issues, corrections, deterministic_report, prompt_version_id, prompt_hash,
			provider_call_id, model_id, metadata, created_by, completed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::uuid,
		        NULLIF($11, ''), NULLIF($12, '')::uuid, NULLIF($13, '')::uuid, $14,
		        NULLIF($15, '')::uuid, now())
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.StoryboardPlanID, input.ReviewAttempt,
		status, review.Approved, mustJSON(review.Issues), mustJSON(review.Corrections), mustJSON(report),
		promptVersionID, promptHash, gatewayResp.ProviderCallID, gatewayResp.ModelID,
		mustJSON(map[string]any{"workflowRunId": input.WorkflowRunID, "nodeRunId": nodeRunID}), input.CreatedBy).Scan(&output.ReviewID); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET status = $2,
		    metadata = metadata || jsonb_build_object(
		      'lastReviewId', $3::text,
		      'lastReviewAttempt', $4::int,
		      'reviewApproved', $5::boolean
		    )
		WHERE id = $1
	`, input.StoryboardPlanID, planStatus, output.ReviewID, input.ReviewAttempt, review.Approved); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plan_reviews
		SET metadata = metadata || jsonb_build_object('activityOutput', $2::jsonb)
		WHERE id = $1
	`, output.ReviewID, mustJSON(output)); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	eventPayload := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "reviewId": output.ReviewID, "reviewAttempt": input.ReviewAttempt,
		"approved": review.Approved, "issues": review.Issues, "corrections": review.Corrections,
		"needsRevisionSceneKeys": needsRevision,
	})
	if review.Approved {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.plan.ready", "storyboard_plan", input.StoryboardPlanID, eventPayload); err != nil {
			return ReviewStoryboardPlanOutput{}, err
		}
	} else {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.plan.review.changes_requested", "storyboard_plan", input.StoryboardPlanID, eventPayload); err != nil {
			return ReviewStoryboardPlanOutput{}, err
		}
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewStoryboardPlanOutput{}, err
	}
	return output, nil
}

func reviewRevisionSceneKeys(review storyboardpkg.StoryboardReviewerOutput) []string {
	seen := map[string]bool{}
	keys := make([]string, 0)
	for _, issue := range review.Issues {
		if issue.SceneKey != "" && (issue.Severity == "error" || issue.Severity == "critical") && !seen[issue.SceneKey] {
			seen[issue.SceneKey] = true
			keys = append(keys, issue.SceneKey)
		}
	}
	for _, correction := range review.Corrections {
		if correction.SceneKey != "" && !seen[correction.SceneKey] {
			seen[correction.SceneKey] = true
			keys = append(keys, correction.SceneKey)
		}
	}
	sort.Strings(keys)
	return keys
}

func (a Activities) failStoryboardReview(ctx context.Context, input ReviewStoryboardPlanInput, execution NodeExecution, code, message string) error {
	if strings.Contains(message, workflowWriteFenceMessage) {
		return discardWorkflowResult(ctx, a.db, execution, message)
	}
	persistCtx, cancel := workflowFailurePersistenceContext(ctx)
	defer cancel()
	tx, err := a.db.Begin(persistCtx)
	if err == nil {
		defer tx.Rollback(persistCtx)
		if _, lockErr := lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, execution); lockErr == nil {
			_, err = tx.Exec(persistCtx, `
		UPDATE storyboard_plans
		SET status = 'failed', metadata = metadata || jsonb_build_object('reviewErrorCode', $2::text, 'reviewErrorMessage', $3::text)
		WHERE id = $1
	`, input.StoryboardPlanID, code, message)
			if err == nil {
				_, err = failNodeRunTx(persistCtx, tx, execution, code, message, mustJSON(map[string]any{
					"storyboardPlanId": input.StoryboardPlanID, "code": code, "message": message,
				}))
			}
			if err == nil {
				_ = tx.Commit(persistCtx)
			}
		}
	}
	return fmt.Errorf("%s: %s", code, message)
}

func (a Activities) existingStoryboardReviewOutput(ctx context.Context, workflowRunID, nodeKey string) (ReviewStoryboardPlanOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return ReviewStoryboardPlanOutput{}, false, nil
	}
	if err != nil {
		return ReviewStoryboardPlanOutput{}, false, err
	}
	var output ReviewStoryboardPlanOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return ReviewStoryboardPlanOutput{}, false, err
	}
	return output, output.ReviewID != "", nil
}
