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
	enums "go.temporal.io/api/enums/v1"
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
	Status                string                       `json:"status"`
	SucceededShotIDs      []string                     `json:"succeededShotIds,omitempty"`
	FailedShotIDs         []string                     `json:"failedShotIds,omitempty"`
	Errors                map[string]string            `json:"errors,omitempty"`
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
	ctx = workflow.WithActivityOptions(ctx, providerImageActivityOptions())
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
	imageCtx := workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	workflowTerminal := false
	defer func() {
		if ctx.Err() == nil || workflowTerminal {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		reason := "Workflow cancellation requested"
		var cancelOutput CancelShotVideoTaskOutput
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
	if options.MaxShots > 0 && len(shots) > options.MaxShots {
		shots = shots[:options.MaxShots]
	}
	if workflow.GetVersion(ctx, "video-production-shot-image-prompts-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		if err := prepareShotImagePromptsForProduction(ctx, input, shots, options.AspectRatio, options.MaxImageConcurrency); err != nil {
			return VideoProductionOutput{}, err
		}
	}
	imageResults := make([]shotImageGenerationResult, len(shots))
	imageConcurrencyVersion := workflow.GetVersion(ctx, "video-production-shot-image-concurrency-v1", workflow.DefaultVersion, 1)
	if imageConcurrencyVersion != workflow.DefaultVersion {
		imageRequests := make([]shotImageGenerationRequest, 0, len(shots))
		for _, shot := range shots {
			imageRequests = append(imageRequests, shotImageGenerationRequest{
				ShotID:         shot.ID,
				ShotIndex:      shot.ShotIndex,
				ShotNo:         shot.ShotNo,
				WorkflowPrompt: input.Prompt,
				AspectRatio:    options.AspectRatio,
			})
		}
		var err error
		imageResults, err = generateShotImagesConcurrently(ctx, imageCtx, input, imageRequests, options.MaxImageConcurrency)
		if err != nil {
			return VideoProductionOutput{}, err
		}
		for _, imageResult := range imageResults {
			if imageResult.Err != nil {
				return VideoProductionOutput{}, imageResult.Err
			}
		}
	}

	providerCalls := VideoProductionProviderCalls{Storyboard: storyboard.ProviderCallID}
	shotOutputs := make([]VideoProductionShotOutput, 0, len(shots))
	imagesByShotID := make(map[string]GenerateShotImageOutput, len(shots))
	for index, shot := range shots {
		var image GenerateShotImageOutput
		if imageConcurrencyVersion == workflow.DefaultVersion {
			if err := workflow.ExecuteActivity(imageCtx, "GenerateShotImage", GenerateShotImageInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
				CreatedBy: input.CreatedBy, ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
				WorkflowPrompt: input.Prompt, AspectRatio: options.AspectRatio,
			}).Get(imageCtx, &image); err != nil {
				return VideoProductionOutput{}, err
			}
		} else {
			image = imageResults[index].Output
		}
		imagesByShotID[shot.ID] = image
		if image.ProviderCallID != "" {
			providerCalls.Images = append(providerCalls.Images, image.ProviderCallID)
		}
	}
	videoBatchInput := input
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	videoBatchInput.Input = mustJSON(BatchShotProductionOptions{
		ShotIDs: shotIDs, Force: true, MaxConcurrency: DefaultShotVideoConcurrency,
		AspectRatio: options.AspectRatio, Resolution: options.Resolution, AudioStrategy: "native_av", AudioRequirement: "preferred",
		PollIntervalSeconds: int(options.PollInterval / time.Second), MaxPolls: options.MaxPolls, SkipCompletion: true,
	})
	childOptions := workflow.ChildWorkflowOptions{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID + ":video-batches", WorkflowExecutionTimeout: 7 * 24 * time.Hour,
		WorkflowRunTimeout: 24 * time.Hour, WaitForCancellation: true,
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	var videoBatch BatchShotProductionOutput
	if err := workflow.ExecuteChildWorkflow(workflow.WithChildOptions(ctx, childOptions), BatchGenerateShotVideosWorkflow, videoBatchInput).Get(ctx, &videoBatch); err != nil {
		return VideoProductionOutput{}, err
	}
	providerCalls.VideoCreates = append(providerCalls.VideoCreates, videoBatch.VideoCreateProviderCallIDs...)
	providerCalls.VideoPolls = append(providerCalls.VideoPolls, videoBatch.VideoPollProviderCallIDs...)
	videoByShotID := make(map[string]ComposeShotRenderPlanMediaOutput, len(videoBatch.ShotVideoOutputs))
	for _, video := range videoBatch.ShotVideoOutputs {
		videoByShotID[video.ShotID] = video
	}
	for _, shot := range shots {
		video, ok := videoByShotID[shot.ID]
		if !ok {
			continue
		}
		image := imagesByShotID[shot.ID]
		duration := shot.Duration
		if duration <= 0 {
			duration = options.Duration
		}
		shotOutputs = append(shotOutputs, VideoProductionShotOutput{
			ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo, Duration: duration,
			ImageArtifactID: image.ImageArtifactID, ImageMediaFileID: image.ImageMediaFileID, ImageStorageKey: image.ImageStorageKey,
			VideoArtifactID: video.ArtifactID, VideoMediaFileID: video.MediaFileID, VideoStorageKey: video.StorageKey,
			ProviderAsyncTaskID: videoBatch.ProviderAsyncTaskIDs[shot.ID],
		})
	}
	result = BuildMultiShotVideoProductionOutput(storyboard, shotOutputs, providerCalls)
	result.Status = videoBatch.Status
	result.SucceededShotIDs = videoBatch.SucceededShotIDs
	result.FailedShotIDs = videoBatch.FailedShotIDs
	result.Errors = videoBatch.Errors
	if !options.SkipCompose {
		composeOptions := defaultActivityOptions()
		composeOptions.TaskQueue = MediaTaskQueue
		composeOptions.StartToCloseTimeout = 30 * time.Minute
		composeCtx := workflow.WithActivityOptions(ctx, composeOptions)
		var composeOutput ComposeFinalVideoOutput
		if err := workflow.ExecuteActivity(composeCtx, "ComposeFinalVideo", ComposeFinalVideoInput{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			CreatedBy:         input.CreatedBy,
			AspectRatio:       options.AspectRatio,
			Resolution:        options.Resolution,
			ProductionPartial: videoBatch.Status == "partial_succeeded",
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
	Duration            float64
	AspectRatio         string
	Resolution          string
	PollInterval        time.Duration
	MaxPolls            int
	MaxShots            int
	MaxImageConcurrency int
	SkipCompose         bool
}

func resolveVideoProductionOptions(raw json.RawMessage) videoProductionOptions {
	options := videoProductionOptions{
		Duration:            5,
		AspectRatio:         "16:9",
		Resolution:          "720p",
		PollInterval:        5 * time.Second,
		MaxPolls:            120,
		MaxShots:            0,
		MaxImageConcurrency: DefaultShotImageConcurrency,
		SkipCompose:         false,
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
		MaxImageConcurrency int     `json:"maxImageConcurrency"`
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
	if decoded.MaxShots > 0 {
		options.MaxShots = decoded.MaxShots
	}
	options.MaxImageConcurrency = clampConcurrency(decoded.MaxImageConcurrency, DefaultShotImageConcurrency, MaxShotImageConcurrency)
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
	nodeExecution, err := a.markArtifactNodeStarted(ctx, input, node)
	if err != nil {
		return WorkflowArtifact{}, err
	}
	storageKey := fmt.Sprintf("artifacts/%s/%s/%s/%s/%s.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID, node.NodeKey, node.ArtifactType)
	put, err := a.storage.PutJSON(ctx, storageKey, node.Payload)
	if err != nil {
		_ = a.markArtifactNodeFailed(ctx, input, nodeExecution, node.NodeKey, err)
		return WorkflowArtifact{}, err
	}
	artifact := WorkflowArtifact{
		StorageKey: put.StorageKey,
		Type:       node.ArtifactType,
		NodeKey:    node.NodeKey,
		Payload:    mustJSON(node.Payload),
	}
	if err := a.markArtifactNodeSucceeded(ctx, input, nodeExecution, put, node, &artifact); err != nil {
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

func (a Activities) markArtifactNodeStarted(ctx context.Context, input TextToStoryboardInput, node artifactNode) (NodeExecution, error) {
	return StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        node.NodeKey,
		NodeType:       node.NodeType,
		Input:          mustJSON(map[string]any{"prompt": input.Prompt}),
	})
}

func (a Activities) markArtifactNodeSucceeded(ctx context.Context, input TextToStoryboardInput, execution NodeExecution, put storage.PutResult, node artifactNode, artifact *WorkflowArtifact) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		if errors.Is(err, ErrWorkflowWriteFenced) || errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx)
		}
		return err
	}

	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'application/json', $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, execution.NodeRunID, node.ArtifactType, put.StorageKey, put.ContentHash, mustJSON(map[string]any{"byteSize": put.ByteSize}), input.CreatedBy).Scan(&artifactID); err != nil {
		return err
	}
	artifact.ArtifactID = artifactID
	output := mustJSON(map[string]any{
		"artifactId":   artifactID,
		"artifactType": node.ArtifactType,
		"storageKey":   put.StorageKey,
		"payload":      node.Payload,
	})
	if _, err := completeNodeRunTx(ctx, tx, execution, output); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", artifactID, output); err != nil {
		return err
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
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, "succeeded", "", "", workflowOutput); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a Activities) markArtifactNodeFailed(ctx context.Context, input TextToStoryboardInput, execution NodeExecution, nodeKey string, cause error) error {
	errorMessage := cause.Error()
	output := mustJSON(map[string]any{"message": errorMessage, "nodeKey": nodeKey})
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if _, err := failNodeRunTx(ctx, tx, execution, "ACTIVITY_FAILED", errorMessage, output); err != nil {
		return err
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, "failed", "ACTIVITY_FAILED", errorMessage, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
