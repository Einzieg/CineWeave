package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type WorkflowArtifact struct {
	ArtifactID string          `json:"artifactId"`
	StorageKey string          `json:"storageKey"`
	Type       string          `json:"type"`
	NodeKey    string          `json:"nodeKey"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type VideoProductionOutput struct {
	StoryboardArtifactID  string                       `json:"storyboardArtifactId"`
	Shots                 []VideoProductionShotOutput  `json:"shots"`
	FinalVideoArtifactID  string                       `json:"finalVideoArtifactId,omitempty"`
	FinalVideoMediaFileID string                       `json:"finalVideoMediaFileId,omitempty"`
	FinalVideoStorageKey  string                       `json:"finalVideoStorageKey,omitempty"`
	TimelineArtifactID    string                       `json:"timelineArtifactId,omitempty"`
	ImageArtifactID       string                       `json:"imageArtifactId,omitempty"`
	ImageMediaFileID      string                       `json:"imageMediaFileId,omitempty"`
	ImageStorageKey       string                       `json:"imageStorageKey,omitempty"`
	VideoArtifactID       string                       `json:"videoArtifactId,omitempty"`
	VideoMediaFileID      string                       `json:"videoMediaFileId,omitempty"`
	VideoStorageKey       string                       `json:"videoStorageKey,omitempty"`
	ProviderAsyncTaskID   string                       `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID        string                       `json:"externalTaskId,omitempty"`
	ProviderCalls         VideoProductionProviderCalls `json:"providerCalls"`
}

type VideoProductionShotOutput struct {
	ShotID              string  `json:"shotId"`
	ShotIndex           int     `json:"shotIndex"`
	ShotNo              int     `json:"shotNo"`
	Duration            float64 `json:"duration"`
	ImageArtifactID     string  `json:"imageArtifactId"`
	ImageMediaFileID    string  `json:"imageMediaFileId"`
	ImageStorageKey     string  `json:"imageStorageKey"`
	VideoArtifactID     string  `json:"videoArtifactId"`
	VideoMediaFileID    string  `json:"videoMediaFileId"`
	VideoStorageKey     string  `json:"videoStorageKey"`
	ProviderAsyncTaskID string  `json:"providerAsyncTaskId"`
	ExternalTaskID      string  `json:"externalTaskId,omitempty"`
}

type VideoProductionProviderCalls struct {
	Storyboard   string   `json:"storyboard,omitempty"`
	Images       []string `json:"images,omitempty"`
	VideoCreates []string `json:"videoCreates,omitempty"`
	VideoPolls   []string `json:"videoPolls,omitempty"`
	Image        string   `json:"image,omitempty"`
	VideoCreate  string   `json:"videoCreate,omitempty"`
	VideoPoll    string   `json:"videoPoll,omitempty"`
}

type ProviderWebhookSignal struct {
	ProviderAsyncTaskID string         `json:"providerAsyncTaskId"`
	ProviderCallID      string         `json:"providerCallId"`
	ExternalTaskID      string         `json:"externalTaskId"`
	Status              string         `json:"status"`
	Payload             map[string]any `json:"payload"`
}

func StoryboardToImageWorkflow(ctx workflow.Context, input TextToStoryboardInput, storyboard WorkflowArtifact) (WorkflowArtifact, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output WorkflowArtifact
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardImages", input, storyboard).Get(ctx, &output); err != nil {
		return WorkflowArtifact{}, err
	}
	return output, nil
}

func StoryboardToVideoWorkflow(ctx workflow.Context, input TextToStoryboardInput, images WorkflowArtifact) (WorkflowArtifact, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output WorkflowArtifact
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardVideos", input, images).Get(ctx, &output); err != nil {
		return WorkflowArtifact{}, err
	}
	return output, nil
}

func VideoComposeWorkflow(ctx workflow.Context, input TextToStoryboardInput, clips WorkflowArtifact) (WorkflowArtifact, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output WorkflowArtifact
	if err := workflow.ExecuteActivity(ctx, "ComposeTimeline", input, clips).Get(ctx, &output); err != nil {
		return WorkflowArtifact{}, err
	}
	return output, nil
}

func VideoProductionWorkflow(ctx workflow.Context, input TextToStoryboardInput) (result VideoProductionOutput, err error) {
	options := resolveVideoProductionOptions(input.Input)
	if scriptOptions := resolveScriptProductionOptions(input.Input); strings.TrimSpace(scriptOptions.ScriptID) != "" {
		return ScriptDrivenVideoProduction(ctx, input, options, scriptOptions)
	}
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var currentCreate CreateShotVideoTaskOutput
	var currentShot StoryboardShotRecord
	workflowTerminal := false
	defer func() {
		if ctx.Err() == nil || workflowTerminal {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		reason := "Workflow cancellation requested"
		var cancelOutput CancelShotVideoTaskOutput
		if currentCreate.ProviderAsyncTaskID != "" && currentCreate.NodeRunID != "" {
			_ = workflow.ExecuteActivity(cleanupCtx, "CancelShotVideoTask", CancelShotVideoTaskInput{
				OrganizationID:      input.OrganizationID,
				ProjectID:           input.ProjectID,
				WorkflowRunID:       input.WorkflowRunID,
				ShotID:              currentShot.ID,
				ShotIndex:           currentShot.ShotIndex,
				ShotNo:              currentShot.ShotNo,
				NodeRunID:           currentCreate.NodeRunID,
				ProviderAsyncTaskID: currentCreate.ProviderAsyncTaskID,
				ExternalTaskID:      currentCreate.ExternalTaskID,
				Reason:              reason,
			}).Get(cleanupCtx, &cancelOutput)
		}
		_ = workflow.ExecuteActivity(cleanupCtx, "CancelVideoProductionWorkflow", input, cancelOutput, reason).Get(cleanupCtx, nil)
	}()

	var storyboard GenerateStoryboardTextOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardText", generateStoryboardTextInput(input)).Get(ctx, &storyboard); err != nil {
		return VideoProductionOutput{}, err
	}

	var shots []StoryboardShotRecord
	if err := workflow.ExecuteActivity(ctx, "ListStoryboardShots", ListStoryboardShotsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
	}).Get(ctx, &shots); err != nil {
		return VideoProductionOutput{}, err
	}
	if len(shots) == 0 {
		shots = storyboard.Shots
	}
	if len(shots) > options.MaxShots {
		shots = shots[:options.MaxShots]
	}

	createActivityOptions := defaultActivityOptions()
	createActivityOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createActivityOptions)
	providerCalls := VideoProductionProviderCalls{Storyboard: storyboard.ProviderCallID}
	shotOutputs := make([]VideoProductionShotOutput, 0, len(shots))
	for _, shot := range shots {
		currentShot = shot
		currentCreate = CreateShotVideoTaskOutput{}
		var image GenerateShotImageOutput
		if err := workflow.ExecuteActivity(ctx, "GenerateShotImage", GenerateShotImageInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: input.Prompt,
			AspectRatio:    options.AspectRatio,
		}).Get(ctx, &image); err != nil {
			return VideoProductionOutput{}, err
		}
		if image.ProviderCallID != "" {
			providerCalls.Images = append(providerCalls.Images, image.ProviderCallID)
		}

		duration := shot.Duration
		if duration <= 0 {
			duration = options.Duration
		}
		if duration > maxShotDuration {
			duration = maxShotDuration
		}
		var createOutput CreateShotVideoTaskOutput
		if err := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: input.Prompt,
			Duration:       duration,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
		}).Get(createCtx, &createOutput); err != nil {
			return VideoProductionOutput{}, err
		}
		currentCreate = createOutput
		if createOutput.ProviderCallID != "" {
			providerCalls.VideoCreates = append(providerCalls.VideoCreates, createOutput.ProviderCallID)
		}

		var terminalPoll PollShotVideoTaskOutput
		shotTerminal := false
		for pollCount := 1; pollCount <= options.MaxPolls; pollCount++ {
			var pollOutput PollShotVideoTaskOutput
			pollInput := PollShotVideoTaskInput{
				OrganizationID:      input.OrganizationID,
				ProjectID:           input.ProjectID,
				WorkflowRunID:       input.WorkflowRunID,
				ShotID:              shot.ID,
				ShotIndex:           shot.ShotIndex,
				ShotNo:              shot.ShotNo,
				NodeRunID:           createOutput.NodeRunID,
				ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
				ExternalTaskID:      createOutput.ExternalTaskID,
				PollCount:           pollCount,
			}
			if err := workflow.ExecuteActivity(ctx, "PollShotVideoTask", pollInput).Get(ctx, &pollOutput); err != nil {
				return VideoProductionOutput{}, err
			}
			if pollOutput.ProviderCallID != "" {
				providerCalls.VideoPolls = append(providerCalls.VideoPolls, pollOutput.ProviderCallID)
			}
			if pollOutput.Status == "succeeded" {
				terminalPoll = pollOutput
				shotTerminal = true
				break
			}
			if pollOutput.Status == "failed" || pollOutput.Status == "cancelled" {
				return VideoProductionOutput{}, temporal.NewApplicationError("provider video task "+pollOutput.Status, codeActivityFailed)
			}
			if err := workflow.Sleep(ctx, options.PollInterval); err != nil {
				return VideoProductionOutput{}, err
			}
		}
		if !shotTerminal {
			timeoutMessage := "provider video task polling timed out"
			if err := workflow.ExecuteActivity(ctx, "FailVideoProductionWorkflow", input, createOutput.NodeRunID, codeProviderVideoPollingTimeout, timeoutMessage).Get(ctx, nil); err != nil {
				return VideoProductionOutput{}, err
			}
			return VideoProductionOutput{}, temporal.NewApplicationError(timeoutMessage, codeProviderVideoPollingTimeout)
		}
		shotOutputs = append(shotOutputs, VideoProductionShotOutput{
			ShotID:              shot.ID,
			ShotIndex:           shot.ShotIndex,
			ShotNo:              shot.ShotNo,
			Duration:            duration,
			ImageArtifactID:     image.ImageArtifactID,
			ImageMediaFileID:    image.ImageMediaFileID,
			ImageStorageKey:     image.ImageStorageKey,
			VideoArtifactID:     terminalPoll.ArtifactID,
			VideoMediaFileID:    terminalPoll.MediaFileID,
			VideoStorageKey:     terminalPoll.StorageKey,
			ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
			ExternalTaskID:      firstNonEmptyString(terminalPoll.ExternalTaskID, createOutput.ExternalTaskID),
		})
		currentCreate = CreateShotVideoTaskOutput{}
		currentShot = StoryboardShotRecord{}
	}
	result = BuildMultiShotVideoProductionOutput(storyboard, shotOutputs, providerCalls)
	if !options.SkipCompose {
		composeOptions := defaultActivityOptions()
		composeOptions.TaskQueue = MediaTaskQueue
		composeOptions.StartToCloseTimeout = 30 * time.Minute
		composeCtx := workflow.WithActivityOptions(ctx, composeOptions)
		var composeOutput ComposeFinalVideoOutput
		if err := workflow.ExecuteActivity(composeCtx, "ComposeFinalVideo", ComposeFinalVideoInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
		}).Get(composeCtx, &composeOutput); err != nil {
			return VideoProductionOutput{}, err
		}
		result.FinalVideoArtifactID = composeOutput.ArtifactID
		result.FinalVideoMediaFileID = composeOutput.MediaFileID
		result.FinalVideoStorageKey = composeOutput.StorageKey
		result.TimelineArtifactID = composeOutput.TimelineArtifactID
	}
	workflowTerminal = true
	if err := workflow.ExecuteActivity(ctx, "CompleteVideoProductionWorkflow", input, result).Get(ctx, nil); err != nil {
		return VideoProductionOutput{}, err
	}
	return result, nil
}

type videoProductionOptions struct {
	Duration     float64
	AspectRatio  string
	Resolution   string
	PollInterval time.Duration
	MaxPolls     int
	MaxShots     int
	SkipCompose  bool
}

func resolveVideoProductionOptions(raw json.RawMessage) videoProductionOptions {
	options := videoProductionOptions{
		Duration:     5,
		AspectRatio:  "16:9",
		Resolution:   "720p",
		PollInterval: 5 * time.Second,
		MaxPolls:     120,
		MaxShots:     defaultMaxStoryboardShots,
		SkipCompose:  false,
	}
	if len(raw) == 0 {
		return options
	}
	var decoded struct {
		Duration            float64 `json:"duration"`
		AspectRatio         string  `json:"aspectRatio"`
		Resolution          string  `json:"resolution"`
		PollIntervalSeconds int     `json:"pollIntervalSeconds"`
		MaxPolls            int     `json:"maxPolls"`
		MaxShots            int     `json:"maxShots"`
		SkipCompose         bool    `json:"skipCompose"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return options
	}
	if decoded.Duration > 0 {
		options.Duration = decoded.Duration
	}
	if strings.TrimSpace(decoded.AspectRatio) != "" {
		options.AspectRatio = strings.TrimSpace(decoded.AspectRatio)
	}
	if strings.TrimSpace(decoded.Resolution) != "" {
		options.Resolution = strings.TrimSpace(decoded.Resolution)
	}
	if decoded.PollIntervalSeconds > 0 {
		options.PollInterval = time.Duration(decoded.PollIntervalSeconds) * time.Second
	}
	if decoded.MaxPolls > 0 {
		options.MaxPolls = decoded.MaxPolls
	}
	if decoded.MaxShots > 0 && decoded.MaxShots <= defaultMaxStoryboardShots {
		options.MaxShots = decoded.MaxShots
	}
	options.SkipCompose = decoded.SkipCompose
	return options
}

func drainProviderWebhookSignals(ctx workflow.Context) []ProviderWebhookSignal {
	signalCh := workflow.GetSignalChannel(ctx, "provider-webhook")
	signals := make([]ProviderWebhookSignal, 0)
	for {
		var signal ProviderWebhookSignal
		if !signalCh.ReceiveAsync(&signal) {
			return signals
		}
		signals = append(signals, signal)
	}
}

func defaultActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	}
}

func (a Activities) GenerateScriptStoryboard(ctx context.Context, input TextToStoryboardInput) (WorkflowArtifact, error) {
	shots := []map[string]any{
		{"shotIndex": 1, "duration": 4, "action": "Establish the world and mood from the prompt.", "dialogue": ""},
		{"shotIndex": 2, "duration": 5, "action": "Follow the subject into the central visual action.", "dialogue": ""},
		{"shotIndex": 3, "duration": 4, "action": "Resolve with a clear cinematic ending beat.", "dialogue": ""},
	}
	payload := map[string]any{
		"kind":          "ScriptToStoryboard",
		"workflowRunId": input.WorkflowRunID,
		"prompt":        input.Prompt,
		"shots":         shots,
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
	}
	return a.writeArtifactNode(ctx, input, artifactNode{
		NodeKey:      "script_to_storyboard",
		NodeType:     "provider_activity",
		ArtifactType: "storyboard",
		Payload:      payload,
	})
}

func (a Activities) GenerateStoryboardImages(ctx context.Context, input TextToStoryboardInput, storyboard WorkflowArtifact) (WorkflowArtifact, error) {
	payload := map[string]any{
		"kind":                "StoryboardToImage",
		"workflowRunId":       input.WorkflowRunID,
		"sourceArtifactId":    storyboard.ArtifactID,
		"sourceStorageKey":    storyboard.StorageKey,
		"imageProviderStatus": "mocked",
		"images": []map[string]any{
			{"shotIndex": 1, "imageUrl": fmt.Sprintf("s3://cineweave/%s/shot-01.png", input.WorkflowRunID)},
			{"shotIndex": 2, "imageUrl": fmt.Sprintf("s3://cineweave/%s/shot-02.png", input.WorkflowRunID)},
			{"shotIndex": 3, "imageUrl": fmt.Sprintf("s3://cineweave/%s/shot-03.png", input.WorkflowRunID)},
		},
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	return a.writeArtifactNode(ctx, input, artifactNode{
		NodeKey:      "storyboard_to_image",
		NodeType:     "provider_activity",
		ArtifactType: "image_collection",
		Payload:      payload,
	})
}

func (a Activities) GenerateStoryboardVideos(ctx context.Context, input TextToStoryboardInput, images WorkflowArtifact) (WorkflowArtifact, error) {
	payload := map[string]any{
		"kind":                "StoryboardToVideo",
		"workflowRunId":       input.WorkflowRunID,
		"sourceArtifactId":    images.ArtifactID,
		"sourceStorageKey":    images.StorageKey,
		"videoProviderStatus": "mocked",
		"clips": []map[string]any{
			{"shotIndex": 1, "duration": 4, "videoUrl": fmt.Sprintf("s3://cineweave/%s/clip-01.mp4", input.WorkflowRunID)},
			{"shotIndex": 2, "duration": 5, "videoUrl": fmt.Sprintf("s3://cineweave/%s/clip-02.mp4", input.WorkflowRunID)},
			{"shotIndex": 3, "duration": 4, "videoUrl": fmt.Sprintf("s3://cineweave/%s/clip-03.mp4", input.WorkflowRunID)},
		},
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	return a.writeArtifactNode(ctx, input, artifactNode{
		NodeKey:      "storyboard_to_video",
		NodeType:     "provider_activity",
		ArtifactType: "video_clips",
		Payload:      payload,
	})
}

func (a Activities) ComposeTimeline(ctx context.Context, input TextToStoryboardInput, clips WorkflowArtifact) (WorkflowArtifact, error) {
	payload := map[string]any{
		"kind":             "VideoCompose",
		"workflowRunId":    input.WorkflowRunID,
		"sourceArtifactId": clips.ArtifactID,
		"sourceStorageKey": clips.StorageKey,
		"duration":         13,
		"videoUrl":         fmt.Sprintf("s3://cineweave/%s/final-video.mp4", input.WorkflowRunID),
		"createdAt":        time.Now().UTC().Format(time.RFC3339),
	}
	return a.writeArtifactNode(ctx, input, artifactNode{
		NodeKey:      "video_compose",
		NodeType:     "compose_activity",
		ArtifactType: "final_video",
		Payload:      payload,
	})
}

func (a Activities) QualityCheck(ctx context.Context, input TextToStoryboardInput, finalVideo WorkflowArtifact) (WorkflowArtifact, error) {
	payload := map[string]any{
		"kind":             "QualityCheck",
		"workflowRunId":    input.WorkflowRunID,
		"sourceArtifactId": finalVideo.ArtifactID,
		"sourceStorageKey": finalVideo.StorageKey,
		"passed":           true,
		"checks": []map[string]any{
			{"key": "artifact_present", "status": "passed"},
			{"key": "timeline_duration", "status": "passed"},
			{"key": "provider_outputs", "status": "passed"},
		},
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	return a.writeArtifactNode(ctx, input, artifactNode{
		NodeKey:      "quality_check",
		NodeType:     "quality_activity",
		ArtifactType: "quality_report",
		Payload:      payload,
		CompleteOutput: map[string]any{
			"finalVideoArtifactId": finalVideo.ArtifactID,
			"finalVideoStorageKey": finalVideo.StorageKey,
		},
	})
}

type artifactNode struct {
	NodeKey        string
	NodeType       string
	ArtifactType   string
	Payload        map[string]any
	CompleteOutput map[string]any
}

func (a Activities) writeArtifactNode(ctx context.Context, input TextToStoryboardInput, node artifactNode) (WorkflowArtifact, error) {
	if input.OrganizationID == "" || input.ProjectID == "" || input.WorkflowRunID == "" {
		return WorkflowArtifact{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if existing, ok, err := a.existingNodeArtifact(ctx, input.WorkflowRunID, node.NodeKey); err != nil {
		return WorkflowArtifact{}, err
	} else if ok {
		return existing, nil
	}
	nodeRunID, err := a.markArtifactNodeStarted(ctx, input, node)
	if err != nil {
		return WorkflowArtifact{}, err
	}
	storageKey := fmt.Sprintf("artifacts/%s/%s/%s/%s/%s.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID, node.NodeKey, node.ArtifactType)
	put, err := a.storage.PutJSON(ctx, storageKey, node.Payload)
	if err != nil {
		_ = a.markArtifactNodeFailed(ctx, input, nodeRunID, node.NodeKey, err)
		return WorkflowArtifact{}, err
	}
	artifact := WorkflowArtifact{
		StorageKey: put.StorageKey,
		Type:       node.ArtifactType,
		NodeKey:    node.NodeKey,
		Payload:    mustJSON(node.Payload),
	}
	if err := a.markArtifactNodeSucceeded(ctx, input, nodeRunID, put, node, &artifact); err != nil {
		return WorkflowArtifact{}, err
	}
	return artifact, nil
}

func (a Activities) existingNodeArtifact(ctx context.Context, workflowRunID, nodeKey string) (WorkflowArtifact, bool, error) {
	var artifact WorkflowArtifact
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT
			COALESCE(n.output->>'artifactId', ''),
			COALESCE(n.output->>'storageKey', ''),
			COALESCE(n.output->>'artifactType', ''),
			n.node_key,
			COALESCE(n.output->'payload', '{}'::jsonb)
		FROM workflow_node_runs n
		WHERE n.workflow_run_id = $1 AND n.node_key = $2 AND n.status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&artifact.ArtifactID, &artifact.StorageKey, &artifact.Type, &artifact.NodeKey, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowArtifact{}, false, nil
	}
	if err != nil {
		return WorkflowArtifact{}, false, err
	}
	artifact.Payload = raw
	return artifact, true, nil
}

func (a Activities) markArtifactNodeStarted(ctx context.Context, input TextToStoryboardInput, node artifactNode) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'running', started_at = COALESCE(started_at, now())
		WHERE id = $1
	`, input.WorkflowRunID); err != nil {
		return "", err
	}
	var nodeRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(organization_id, project_id, workflow_run_id, node_key, node_type, status, input, started_at)
		VALUES ($1, $2, $3, $4, $5, 'running', $6, now())
		ON CONFLICT (workflow_run_id, node_key) DO UPDATE SET
			status = 'running',
			input = EXCLUDED.input,
			retry_count = workflow_node_runs.retry_count + 1,
			error_code = NULL,
			error_message = NULL,
			started_at = now(),
			completed_at = NULL
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, node.NodeKey, node.NodeType, mustJSON(map[string]any{"prompt": input.Prompt})).Scan(&nodeRunID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, 'workflow.node.started', 'workflow_node_run', $3, $4)
	`, input.OrganizationID, input.ProjectID, nodeRunID, mustJSON(map[string]any{"workflowRunId": input.WorkflowRunID, "nodeKey": node.NodeKey})); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return nodeRunID, nil
}

func (a Activities) markArtifactNodeSucceeded(ctx context.Context, input TextToStoryboardInput, nodeRunID string, put storage.PutResult, node artifactNode, artifact *WorkflowArtifact) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'application/json', $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, node.ArtifactType, put.StorageKey, put.ContentHash, mustJSON(map[string]any{"byteSize": put.ByteSize}), input.CreatedBy).Scan(&artifactID); err != nil {
		return err
	}
	artifact.ArtifactID = artifactID
	output := mustJSON(map[string]any{
		"artifactId":   artifactID,
		"artifactType": node.ArtifactType,
		"storageKey":   put.StorageKey,
		"payload":      node.Payload,
	})
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, nodeRunID, output); err != nil {
		return err
	}
	events := []map[string]any{
		{"event_type": "artifact.created", "aggregate_type": "artifact", "aggregate_id": artifactID, "payload": output},
		{"event_type": "workflow.node.completed", "aggregate_type": "workflow_node_run", "aggregate_id": nodeRunID, "payload": output},
	}
	if node.CompleteOutput != nil {
		completeOutput := map[string]any{
			"artifactId":      artifactID,
			"artifactType":    node.ArtifactType,
			"storageKey":      put.StorageKey,
			"qualityArtifact": artifactID,
		}
		for key, value := range node.CompleteOutput {
			completeOutput[key] = value
		}
		workflowOutput := mustJSON(completeOutput)
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET status = 'succeeded', output = $2, completed_at = now()
			WHERE id = $1
		`, input.WorkflowRunID, workflowOutput); err != nil {
			return err
		}
		events = append(events, map[string]any{
			"event_type": "workflow.run.completed", "aggregate_type": "workflow_run", "aggregate_id": input.WorkflowRunID, "payload": workflowOutput,
		})
	}
	for _, event := range events {
		if _, err := tx.Exec(ctx, `
			INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, input.OrganizationID, input.ProjectID, event["event_type"], event["aggregate_type"], event["aggregate_id"], event["payload"]); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a Activities) markArtifactNodeFailed(ctx context.Context, input TextToStoryboardInput, nodeRunID, nodeKey string, cause error) error {
	errorMessage := cause.Error()
	_, err := a.db.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'failed', error_code = 'ACTIVITY_FAILED', error_message = $2, completed_at = now()
		WHERE id = $1;
		UPDATE workflow_runs
		SET status = 'failed', error_code = 'ACTIVITY_FAILED', error_message = $2, completed_at = now()
		WHERE id = $3;
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES
			($4, $5, 'workflow.node.failed', 'workflow_node_run', $1, $6),
			($4, $5, 'workflow.run.failed', 'workflow_run', $3, $7);
	`, nodeRunID, errorMessage, input.WorkflowRunID, input.OrganizationID, input.ProjectID, mustJSON(map[string]any{"message": errorMessage, "nodeKey": nodeKey}), mustJSON(map[string]any{"message": errorMessage}))
	return err
}
