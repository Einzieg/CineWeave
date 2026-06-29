package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const defaultGatewayVideoMaxBytes int64 = 512 << 20

type gatewayVideoInput struct {
	Prompt          string
	DurationSeconds float64
	AspectRatio     string
	Resolution      string
	Mode            string
}

type gatewayVideoMedia struct {
	Body        []byte
	MimeType    string
	ByteSize    int64
	ContentHash string
}

type gatewayStoredVideo struct {
	ArtifactID  string
	MediaFileID string
	Output      GatewayVideoOutput
	Media       gatewayVideoMedia
}

type gatewayVideoTask struct {
	ID                    string
	ProviderCallID        string
	OrganizationID        string
	ProjectID             string
	WorkflowRunID         string
	NodeRunID             string
	ProviderAccountID     string
	ProviderModelID       string
	CredentialID          string
	ModelProfileID        string
	ModelProfileBindingID string
	ModelProfileKey       string
	ExternalTaskID        string
	Status                string
	Input                 json.RawMessage
	NormalizedOutput      json.RawMessage
	PollCount             int
}

func (s *Service) CreateVideoTask(ctx context.Context, req GatewayVideoCreateTaskRequest) (GatewayVideoCreateTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	videoInput, err := parseGatewayVideoInput(input)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	req.Input = input

	selection, err := s.selectGatewayVideoModel(ctx, req.OrganizationID, req.ProviderModelID, req.ModelProfileKey)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	manifest, err := s.manifestForAccount(ctx, selection.Account)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	endpointKey, endpoint, err := selectVideoCreateEndpoint(selection, manifest)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	timeout := gatewayVideoTimeout(req.Options.TimeoutMS, endpoint.TimeoutMS)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callID := uuid.NewString()
	started := time.Now()
	result, runErr := callManifestEndpointWithContext(callCtx, manifest, selection.Account, selection.Credential, endpointKey, endpoint, input, videoManifestContext(selection, req.References, nil))
	latencyMS := int(time.Since(started).Milliseconds())
	if result.LatencyMS > latencyMS {
		latencyMS = result.LatencyMS
	}

	status := normalizeGatewayVideoStatus(result.Status)
	if status == "" {
		status = normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status"))
	}
	if status == "" {
		status = "running"
	}
	externalTaskID := videoStringField(result.NormalizedOutput, "externalTaskId", "taskId", "id")
	if externalTaskID == "" {
		externalTaskID = videoStringField(input, "externalTaskId", "taskId")
	}
	normalizedOutput := result.NormalizedOutput
	responseSnapshot := result.ResponseSnapshot
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	var stored *gatewayStoredVideo
	usage := GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"}

	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	} else if videoURL := videoStringField(normalizedOutput, "videoUrl", "url", "outputUrl"); status == "succeeded" && strings.TrimSpace(videoURL) != "" {
		if s.objectStorage == nil {
			return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
		}
		media, mediaErr := downloadGatewayVideoURL(callCtx, videoURL, videoStringField(normalizedOutput, "mimeType"), timeout)
		if mediaErr == nil {
			stored, mediaErr = s.storeGatewayVideoMedia(callCtx, callID, req.OrganizationID, req.ProjectID, selection, externalTaskID, result, media, videoInput)
		}
		if mediaErr != nil {
			status = "failed"
			errorCode = CodeMediaDownloadFailed
			errorMessage = mediaErr.Error()
			standardError = &StandardError{Code: CodeMediaDownloadFailed, Message: "provider video media could not be stored", Retryable: true}
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			usage = estimateVideoCost(videoInput, stored.Output.DurationSeconds, selection.Model.Capabilities)
			normalizedOutput = mustJSON(stored.Output)
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = json.RawMessage(`{}`)
	}

	call, taskID, err := s.recordVideoCreateTask(ctx, selection, req, callID, externalTaskID, status, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, result.RequestSnapshot, responseSnapshot, normalizedOutput, usage, stored, videoInput)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	return GatewayVideoCreateTaskResponse{
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: taskID,
		ExternalTaskID:      externalTaskID,
		ModelID:             selection.Model.ID,
		Status:              status,
		Error:               standardError,
		LatencyMS:           latencyMS,
	}, nil
}

func (s *Service) PollVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (GatewayVideoPollTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	task, err := s.getGatewayVideoTask(ctx, req)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if task.OrganizationID != req.OrganizationID {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: provider async task belongs to a different organization", ErrValidation)
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, task.ProviderAccountID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	model, err := s.GetModel(ctx, req.OrganizationID, task.ProviderModelID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	credential, credentialID, err := s.activeCredentialPayload(ctx, req.OrganizationID, account.ID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	selection := gatewayModelSelection{
		Account:               account,
		Model:                 model,
		CredentialID:          credentialID,
		Credential:            credential,
		ModelProfileID:        task.ModelProfileID,
		ModelProfileBindingID: task.ModelProfileBindingID,
		ModelProfileKey:       task.ModelProfileKey,
	}
	manifest, err := s.manifestForAccount(ctx, account)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	_, createEndpoint, _ := selectVideoCreateEndpoint(selection, manifest)
	endpointKey, endpoint, err := selectVideoPollEndpoint(selection, manifest, createEndpoint)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	timeout := gatewayVideoTimeout(req.Options.TimeoutMS, endpoint.TimeoutMS)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callID := uuid.NewString()
	started := time.Now()
	result, runErr := callManifestEndpointWithContext(callCtx, manifest, account, credential, endpointKey, endpoint, task.Input, videoManifestContext(selection, nil, &task))
	latencyMS := int(time.Since(started).Milliseconds())
	if result.LatencyMS > latencyMS {
		latencyMS = result.LatencyMS
	}

	status := normalizeGatewayVideoStatus(result.Status)
	if status == "" {
		status = normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status"))
	}
	if status == "" {
		status = "running"
	}
	normalizedOutput := result.NormalizedOutput
	responseSnapshot := result.ResponseSnapshot
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	var output GatewayVideoOutput
	usage := GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"}
	var stored *gatewayStoredVideo
	videoInput, _ := parseGatewayVideoInput(task.Input)

	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	} else if status == "succeeded" {
		videoURL := videoStringField(normalizedOutput, "videoUrl", "url", "outputUrl")
		if strings.TrimSpace(videoURL) != "" {
			if s.objectStorage == nil {
				return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
			}
			media, mediaErr := downloadGatewayVideoURL(callCtx, videoURL, videoStringField(normalizedOutput, "mimeType"), timeout)
			if mediaErr == nil {
				stored, mediaErr = s.storeGatewayVideoMedia(callCtx, callID, task.OrganizationID, firstNonEmpty(req.ProjectID, task.ProjectID), selection, task.ExternalTaskID, result, media, videoInput)
			}
			if mediaErr != nil {
				status = "failed"
				errorCode = CodeMediaDownloadFailed
				errorMessage = mediaErr.Error()
				standardError = &StandardError{Code: CodeMediaDownloadFailed, Message: "provider video media could not be stored", Retryable: true}
				normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
			} else {
				output = stored.Output
				usage = estimateVideoCost(videoInput, output.DurationSeconds, model.Capabilities)
				normalizedOutput = mustJSON(output)
			}
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = json.RawMessage(`{}`)
	}

	call, err := s.recordVideoPollTask(ctx, selection, req, task, callID, status, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, result.RequestSnapshot, responseSnapshot, normalizedOutput, usage, stored, videoInput)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	return GatewayVideoPollTaskResponse{
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: task.ID,
		ExternalTaskID:      task.ExternalTaskID,
		ModelID:             task.ProviderModelID,
		Status:              status,
		Output:              output,
		Usage:               usage,
		Error:               standardError,
		LatencyMS:           latencyMS,
	}, nil
}

func (s *Service) CancelVideoTask(ctx context.Context, req GatewayVideoCancelTaskRequest) (GatewayVideoCancelTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCancelTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	task, err := s.getGatewayVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      req.OrganizationID,
		ProviderAsyncTaskID: req.ProviderAsyncTaskID,
		ExternalTaskID:      req.ExternalTaskID,
		ProviderModelID:     req.ProviderModelID,
		ProviderAccountID:   req.ProviderAccountID,
	})
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, task.ProviderAccountID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	model, err := s.GetModel(ctx, req.OrganizationID, task.ProviderModelID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	credential, credentialID, err := s.activeCredentialPayload(ctx, req.OrganizationID, account.ID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	selection := gatewayModelSelection{Account: account, Model: model, CredentialID: credentialID, Credential: credential, ModelProfileID: task.ModelProfileID, ModelProfileBindingID: task.ModelProfileBindingID, ModelProfileKey: task.ModelProfileKey}
	manifest, err := s.manifestForAccount(ctx, account)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}

	callID := uuid.NewString()
	status := "cancelled"
	latencyMS := 0
	requestSnapshot := mustJSON(map[string]any{"providerAsyncTaskId": task.ID, "externalTaskId": task.ExternalTaskID})
	responseSnapshot := json.RawMessage(`null`)
	normalizedOutput := mustJSON(map[string]any{"status": status})
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	if endpointKey, endpoint, ok := selectVideoCancelEndpoint(selection, manifest); ok {
		timeout := gatewayVideoTimeout(0, endpoint.TimeoutMS)
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		started := time.Now()
		result, runErr := callManifestEndpointWithContext(callCtx, manifest, account, credential, endpointKey, endpoint, task.Input, videoManifestContext(selection, nil, &task))
		latencyMS = int(time.Since(started).Milliseconds())
		if result.LatencyMS > latencyMS {
			latencyMS = result.LatencyMS
		}
		requestSnapshot = result.RequestSnapshot
		responseSnapshot = result.ResponseSnapshot
		if len(result.NormalizedOutput) > 0 {
			normalizedOutput = result.NormalizedOutput
		}
		if runErr != nil {
			status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
			standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
			if len(responseSnapshot) == 0 {
				responseSnapshot = upstreamBody(runErr)
			}
		} else {
			status = "cancelled"
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	call, err := s.recordVideoCancelTask(ctx, selection, task, callID, status, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, requestSnapshot, responseSnapshot, normalizedOutput)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	return GatewayVideoCancelTaskResponse{
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: task.ID,
		ExternalTaskID:      task.ExternalTaskID,
		Status:              status,
		Error:               standardError,
	}, nil
}

func (s *Service) selectGatewayVideoModel(ctx context.Context, organizationID, providerModelID, modelProfileKey string) (gatewayModelSelection, error) {
	if strings.TrimSpace(providerModelID) != "" {
		model, err := s.GetModel(ctx, organizationID, providerModelID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		if model.Status != "active" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
		}
		if model.Modality != "video" && model.Modality != "multimodal" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support video generation", ErrValidation)
		}
		account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		return s.completeGatewaySelection(ctx, organizationID, account, model, "", "", "")
	}
	profileKey := strings.TrimSpace(modelProfileKey)
	if profileKey == "" {
		return gatewayModelSelection{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	var profileID, bindingID, modelID string
	err := s.db.QueryRow(ctx, `
		SELECT p.id, b.id, m.id
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE p.organization_id = $1
		  AND p.profile_key = $2
		  AND b.enabled = true
		  AND m.status = 'active'
		  AND a.status = 'active'
		  AND m.modality IN ('video', 'multimodal')
		ORDER BY b.priority ASC, b.weight DESC, b.created_at ASC
		LIMIT 1
	`, organizationID, profileKey).Scan(&profileID, &bindingID, &modelID)
	if err != nil {
		if errorsIsNoRows(err) {
			return gatewayModelSelection{}, fmt.Errorf("%w: no active video provider model is bound to modelProfileKey", ErrValidation)
		}
		return gatewayModelSelection{}, err
	}
	model, err := s.GetModel(ctx, organizationID, modelID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return s.completeGatewaySelection(ctx, organizationID, account, model, profileID, bindingID, profileKey)
}

func parseGatewayVideoInput(input json.RawMessage) (gatewayVideoInput, error) {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return gatewayVideoInput{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	prompt, _ := decoded["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return gatewayVideoInput{}, fmt.Errorf("%w: input.prompt is required", ErrValidation)
	}
	return gatewayVideoInput{
		Prompt:          prompt,
		DurationSeconds: floatField(decoded["duration"], "duration"),
		AspectRatio:     videoStringOption(decoded, "aspectRatio"),
		Resolution:      videoStringOption(decoded, "resolution"),
		Mode:            videoStringOption(decoded, "mode"),
	}, nil
}

func (s *Service) manifestForAccount(ctx context.Context, account Account) (ProviderManifest, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `
		SELECT c.manifest
		FROM provider_connectors c
		WHERE c.id = $1
	`, account.ConnectorID).Scan(&raw)
	if err != nil {
		return ProviderManifest{}, err
	}
	manifest, _, err := ParseManifest(raw, "")
	return manifest, err
}

func selectVideoCreateEndpoint(selection gatewayModelSelection, manifest ProviderManifest) (string, ManifestEndpoint, error) {
	return firstManifestEndpoint(manifest, "async_create", []string{
		accountConfigString(selection.Account.Config, "videoCreateEndpointKey"),
		modelProviderOptionString(selection.Model, "videoCreateEndpointKey"),
		"video_generate",
		"video_create",
		"createVideo",
	})
}

func selectVideoPollEndpoint(selection gatewayModelSelection, manifest ProviderManifest, createEndpoint ManifestEndpoint) (string, ManifestEndpoint, error) {
	return firstManifestEndpoint(manifest, "async_poll", []string{
		strings.TrimSpace(createEndpoint.PollEndpointKey),
		accountConfigString(selection.Account.Config, "videoPollEndpointKey"),
		modelProviderOptionString(selection.Model, "videoPollEndpointKey"),
		"video_poll",
		"pollVideo",
		"poll",
	})
}

func selectVideoCancelEndpoint(selection gatewayModelSelection, manifest ProviderManifest) (string, ManifestEndpoint, bool) {
	key, endpoint, err := firstManifestEndpoint(manifest, "", []string{
		accountConfigString(selection.Account.Config, "videoCancelEndpointKey"),
		modelProviderOptionString(selection.Model, "videoCancelEndpointKey"),
		"video_cancel",
		"cancelVideo",
		"cancel",
	})
	return key, endpoint, err == nil
}

func firstManifestEndpoint(manifest ProviderManifest, wantType string, keys []string) (string, ManifestEndpoint, error) {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		endpoint, ok := manifest.Endpoints[key]
		if !ok {
			continue
		}
		if wantType != "" && endpointType(endpoint.EndpointType) != wantType {
			continue
		}
		return key, endpoint, nil
	}
	return "", ManifestEndpoint{}, fmt.Errorf("%w: video manifest endpoint was not found", ErrValidation)
}

func videoManifestContext(selection gatewayModelSelection, references []GatewayVideoReference, task *gatewayVideoTask) manifestCallContext {
	refValues := make([]map[string]any, 0, len(references))
	for _, ref := range references {
		refValues = append(refValues, map[string]any{
			"type":        ref.Type,
			"assetId":     ref.AssetID,
			"artifactId":  ref.ArtifactID,
			"mediaFileId": ref.MediaFileID,
			"url":         ref.URL,
			"storageKey":  ref.StorageKey,
			"mimeType":    ref.MimeType,
			"metadata":    rawJSONValue(ref.Metadata),
		})
	}
	taskValue := map[string]any{}
	if task != nil {
		taskValue = map[string]any{
			"externalTaskId":      task.ExternalTaskID,
			"providerAsyncTaskId": task.ID,
		}
	}
	baseURL := ""
	if selection.Account.BaseURL != nil {
		baseURL = *selection.Account.BaseURL
	}
	return manifestCallContext{
		References: refValues,
		Model: map[string]any{
			"id":          selection.Model.ModelKey,
			"displayName": selection.Model.DisplayName,
			"modality":    selection.Model.Modality,
		},
		Account: map[string]any{
			"baseUrl":  baseURL,
			"authType": selection.Account.AuthType,
		},
		Task: taskValue,
	}
}

func (s *Service) storeGatewayVideoMedia(ctx context.Context, callID, organizationID, projectID string, selection gatewayModelSelection, externalTaskID string, result manifestRunResult, media gatewayVideoMedia, input gatewayVideoInput) (*gatewayStoredVideo, error) {
	storageKey := gatewayVideoStorageKey(organizationID, projectID, media.MimeType, videoStringField(result.NormalizedOutput, "videoUrl", "url", "outputUrl"))
	put, err := s.objectStorage.PutBytes(ctx, storageKey, media.Body, media.MimeType)
	if err != nil {
		return nil, err
	}
	media.ContentHash = put.ContentHash
	if media.ContentHash == "" {
		media.ContentHash = sha256ContentHash(media.Body)
	}
	if media.ByteSize == 0 {
		media.ByteSize = put.ByteSize
	}
	duration := firstPositiveFloat(videoFloatField(result.NormalizedOutput, "durationSeconds", "duration"), input.DurationSeconds)
	var durationPtr *float64
	if duration > 0 {
		durationPtr = &duration
	}
	artifactID := uuid.NewString()
	mediaFileID := uuid.NewString()
	output := GatewayVideoOutput{
		ArtifactID:      artifactID,
		MediaFileID:     mediaFileID,
		StorageKey:      put.StorageKey,
		URL:             videoStringField(result.NormalizedOutput, "videoUrl", "url", "outputUrl"),
		MimeType:        media.MimeType,
		ByteSize:        &media.ByteSize,
		DurationSeconds: durationPtr,
		Raw:             result.NormalizedOutput,
	}
	return &gatewayStoredVideo{ArtifactID: artifactID, MediaFileID: mediaFileID, Output: output, Media: media}, nil
}

func (s *Service) recordVideoCreateTask(ctx context.Context, selection gatewayModelSelection, req GatewayVideoCreateTaskRequest, callID, externalTaskID, status string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage, usage GatewayUsage, stored *gatewayStoredVideo, input gatewayVideoInput) (CallLog, string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, "", err
	}
	defer tx.Rollback(ctx)
	taskID := uuid.NewString()
	if stored != nil {
		if err := insertGatewayVideoArtifact(ctx, tx, selection, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, callID, taskID, externalTaskID, stored, input); err != nil {
			return CallLog{}, "", err
		}
		if err := insertGatewayVideoMediaFile(ctx, tx, req.OrganizationID, req.ProjectID, callID, taskID, externalTaskID, selection.Model.ID, stored); err != nil {
			return CallLog{}, "", err
		}
	}
	callReq := RecordCallRequest{
		ID:                    callID,
		OrganizationID:        req.OrganizationID,
		ProjectID:             req.ProjectID,
		WorkflowRunID:         req.WorkflowRunID,
		NodeRunID:             req.NodeRunID,
		ProviderAccountID:     selection.Account.ID,
		ProviderModelID:       selection.Model.ID,
		CredentialID:          selection.CredentialID,
		ModelProfileID:        selection.ModelProfileID,
		ModelProfileBindingID: selection.ModelProfileBindingID,
		ModelProfileKey:       selection.ModelProfileKey,
		PromptVersionID:       req.PromptVersionID,
		PromptHash:            req.PromptHash,
		IdempotencyKey:        gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		TaskType:              "video.create_task",
		ExecutionMode:         "async_create",
		Status:                status,
		LatencyMS:             &latencyMS,
		EstimatedCost:         usage.EstimatedCost,
		Currency:              usage.Currency,
		ErrorCode:             errorCode,
		ErrorMessage:          errorMessage,
		UpstreamStatus:        upstreamStatus,
		UpstreamErrorCode:     upstreamErrorCode,
		RequestSnapshot:       requestSnapshot,
		ResponseSnapshot:      responseSnapshot,
		NormalizedOutput:      normalizedOutput,
	}
	if stored != nil {
		callReq.ArtifactIDs = mustJSON([]string{stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, "", err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_async_tasks(
			id,
			provider_call_id, organization_id, project_id, workflow_run_id, node_run_id,
			provider_account_id, provider_model_id, credential_id, model_profile_id, model_profile_binding_id, model_profile_key,
			external_task_id, task_type, status, execution_mode, input, normalized_output, last_response_snapshot,
			error_code, error_message, poll_count, next_poll_at, started_at, completed_at, cancelled_at, raw_status
		)
		VALUES (
			$1,
			$2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, 'video.generate', $14, 'async_polling', $15, $16, $17,
			$18, $19, 0, NULL, now(),
			CASE WHEN $14 IN ('succeeded', 'failed') THEN now() ELSE NULL END,
			CASE WHEN $14 = 'cancelled' THEN now() ELSE NULL END,
			$16
		)
		RETURNING id
	`, taskID, call.ID, req.OrganizationID, nullString(req.ProjectID), nullString(req.WorkflowRunID), nullString(req.NodeRunID), selection.Account.ID, selection.Model.ID, selection.CredentialID, nullString(selection.ModelProfileID), nullString(selection.ModelProfileBindingID), nullString(selection.ModelProfileKey), nullString(externalTaskID), status, req.Input, nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot), nullString(errorCode), nullString(errorMessage)).Scan(&taskID); err != nil {
		return CallLog{}, "", err
	}
	if stored != nil {
		if err := insertVideoCostRecord(ctx, tx, call.ID, selection, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, taskID, externalTaskID, usage, input, stored.Output.DurationSeconds); err != nil {
			return CallLog{}, "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, "", err
	}
	return call, taskID, nil
}

func (s *Service) recordVideoPollTask(ctx context.Context, selection gatewayModelSelection, req GatewayVideoPollTaskRequest, task gatewayVideoTask, callID, status string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage, usage GatewayUsage, stored *gatewayStoredVideo, input gatewayVideoInput) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)
	projectID := firstNonEmpty(req.ProjectID, task.ProjectID)
	workflowRunID := firstNonEmpty(req.WorkflowRunID, task.WorkflowRunID)
	nodeRunID := firstNonEmpty(req.NodeRunID, task.NodeRunID)
	if stored != nil {
		if err := insertGatewayVideoArtifact(ctx, tx, selection, task.OrganizationID, projectID, workflowRunID, nodeRunID, callID, task.ID, task.ExternalTaskID, stored, input); err != nil {
			return CallLog{}, err
		}
		if err := insertGatewayVideoMediaFile(ctx, tx, task.OrganizationID, projectID, callID, task.ID, task.ExternalTaskID, selection.Model.ID, stored); err != nil {
			return CallLog{}, err
		}
	}
	callReq := RecordCallRequest{
		ID:                    callID,
		OrganizationID:        task.OrganizationID,
		ProjectID:             projectID,
		WorkflowRunID:         workflowRunID,
		NodeRunID:             nodeRunID,
		ProviderAccountID:     selection.Account.ID,
		ProviderModelID:       selection.Model.ID,
		CredentialID:          selection.CredentialID,
		ModelProfileID:        selection.ModelProfileID,
		ModelProfileBindingID: selection.ModelProfileBindingID,
		ModelProfileKey:       selection.ModelProfileKey,
		TaskType:              "video.poll_task",
		ExecutionMode:         "async_poll",
		Status:                status,
		LatencyMS:             &latencyMS,
		EstimatedCost:         usage.EstimatedCost,
		Currency:              usage.Currency,
		ErrorCode:             errorCode,
		ErrorMessage:          errorMessage,
		UpstreamStatus:        upstreamStatus,
		UpstreamErrorCode:     upstreamErrorCode,
		RequestSnapshot:       requestSnapshot,
		ResponseSnapshot:      responseSnapshot,
		NormalizedOutput:      normalizedOutput,
	}
	if stored != nil {
		callReq.ArtifactIDs = mustJSON([]string{stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks
		SET status = $2,
		    normalized_output = $3,
		    last_response_snapshot = $4,
		    raw_status = $3,
		    error_code = $5,
		    error_message = $6,
		    poll_count = poll_count + 1,
		    last_poll_at = now(),
		    completed_at = CASE WHEN $2 IN ('succeeded', 'failed') THEN COALESCE(completed_at, now()) ELSE completed_at END,
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN COALESCE(cancelled_at, now()) ELSE cancelled_at END,
		    finalized_at = CASE WHEN $2 IN ('succeeded', 'failed', 'cancelled') THEN COALESCE(finalized_at, now()) ELSE finalized_at END
		WHERE id = $1
	`, task.ID, status, nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot), nullString(errorCode), nullString(errorMessage)); err != nil {
		return CallLog{}, err
	}
	if stored != nil {
		if err := insertVideoCostRecord(ctx, tx, call.ID, selection, task.OrganizationID, projectID, workflowRunID, nodeRunID, task.ID, task.ExternalTaskID, usage, input, stored.Output.DurationSeconds); err != nil {
			return CallLog{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func (s *Service) recordVideoCancelTask(ctx context.Context, selection gatewayModelSelection, task gatewayVideoTask, callID, status string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)
	call, err := recordCall(ctx, tx, RecordCallRequest{
		ID:                    callID,
		OrganizationID:        task.OrganizationID,
		ProjectID:             task.ProjectID,
		WorkflowRunID:         task.WorkflowRunID,
		NodeRunID:             task.NodeRunID,
		ProviderAccountID:     selection.Account.ID,
		ProviderModelID:       selection.Model.ID,
		CredentialID:          selection.CredentialID,
		ModelProfileID:        selection.ModelProfileID,
		ModelProfileBindingID: selection.ModelProfileBindingID,
		ModelProfileKey:       selection.ModelProfileKey,
		TaskType:              "video.cancel_task",
		ExecutionMode:         "sync",
		Status:                status,
		LatencyMS:             &latencyMS,
		ErrorCode:             errorCode,
		ErrorMessage:          errorMessage,
		UpstreamStatus:        upstreamStatus,
		UpstreamErrorCode:     upstreamErrorCode,
		RequestSnapshot:       requestSnapshot,
		ResponseSnapshot:      responseSnapshot,
		NormalizedOutput:      normalizedOutput,
	})
	if err != nil {
		return CallLog{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks
		SET status = 'cancelled',
		    cancelled_at = COALESCE(cancelled_at, now()),
		    finalized_at = COALESCE(finalized_at, now()),
		    error_code = $2,
		    error_message = $3,
		    normalized_output = $4,
		    last_response_snapshot = $5
		WHERE id = $1
	`, task.ID, nullString(errorCode), nullString(errorMessage), nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot)); err != nil {
		return CallLog{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func insertGatewayVideoArtifact(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, organizationID, projectID, workflowRunID, nodeRunID, callID, providerAsyncTaskID, externalTaskID string, stored *gatewayStoredVideo, input gatewayVideoInput) error {
	metadata := mustJSON(map[string]any{
		"source":              "provider_gateway",
		"providerCallId":      callID,
		"providerAsyncTaskId": providerAsyncTaskID,
		"externalTaskId":      externalTaskID,
		"providerModelId":     selection.Model.ID,
		"mediaFileId":         stored.MediaFileID,
		"prompt":              input.Prompt,
		"duration":            input.DurationSeconds,
		"aspectRatio":         input.AspectRatio,
		"resolution":          input.Resolution,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO artifacts(
			id, organization_id, project_id, workflow_run_id, node_run_id, type,
			storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 'generated_video', $6, $7, $8, NULL, $9, $10, NULL)
	`, stored.ArtifactID, organizationID, nullString(projectID), nullString(workflowRunID), nullString(nodeRunID), stored.Output.StorageKey, stored.Output.MimeType, stored.Media.ContentHash, selection.Model.ID, metadata)
	return err
}

func insertGatewayVideoMediaFile(ctx context.Context, tx pgx.Tx, organizationID, projectID, callID, providerAsyncTaskID, externalTaskID, providerModelID string, stored *gatewayStoredVideo) error {
	metadata := mustJSON(map[string]any{
		"source":              "provider_gateway",
		"providerCallId":      callID,
		"providerAsyncTaskId": providerAsyncTaskID,
		"externalTaskId":      externalTaskID,
		"providerModelId":     providerModelID,
		"upstream":            map[string]any{"responseType": "url"},
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO media_files(
			id, organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, duration_seconds, checksum, created_by, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL, $8, $9, NULL, $10)
	`, stored.MediaFileID, organizationID, nullString(projectID), stored.ArtifactID, stored.Output.StorageKey, stored.Output.MimeType, stored.Media.ByteSize, nullFloat(stored.Output.DurationSeconds), stored.Media.ContentHash, metadata)
	return err
}

func insertVideoCostRecord(ctx context.Context, tx pgx.Tx, providerCallID string, selection gatewayModelSelection, organizationID, projectID, workflowRunID, nodeRunID, providerAsyncTaskID, externalTaskID string, usage GatewayUsage, input gatewayVideoInput, durationSeconds *float64) error {
	quantity := input.DurationSeconds
	if durationSeconds != nil && *durationSeconds > 0 {
		quantity = *durationSeconds
	}
	metadata := mustJSON(map[string]any{
		"durationSeconds":     quantity,
		"resolution":          input.Resolution,
		"aspectRatio":         input.AspectRatio,
		"providerAsyncTaskId": providerAsyncTaskID,
		"externalTaskId":      externalTaskID,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO cost_records(
			organization_id, project_id, workflow_run_id, node_run_id,
			provider_call_id, provider_model_id, credential_id, model_profile_id,
			cost_type, amount, currency, unit, quantity, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'video.generate', $9::numeric, $10, 'second', $11, $12)
	`, organizationID, nullString(projectID), nullString(workflowRunID), nullString(nodeRunID), providerCallID, selection.Model.ID, selection.CredentialID, nullString(selection.ModelProfileID), costOrZero(usage.EstimatedCost), currencyOrDefault(usage.Currency), quantity, metadata)
	return err
}

func (s *Service) getGatewayVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (gatewayVideoTask, error) {
	var task gatewayVideoTask
	var projectID, workflowRunID, nodeRunID, providerModelID, credentialID, modelProfileID, modelProfileBindingID, modelProfileKey, externalTaskID sql.NullString
	var normalizedOutput []byte
	if strings.TrimSpace(req.ProviderAsyncTaskID) != "" {
		err := s.db.QueryRow(ctx, gatewayVideoTaskSelect(`WHERE id = $1 AND organization_id = $2`), req.ProviderAsyncTaskID, req.OrganizationID).Scan(
			&task.ID, &task.ProviderCallID, &task.OrganizationID, &projectID, &workflowRunID, &nodeRunID,
			&task.ProviderAccountID, &providerModelID, &credentialID, &modelProfileID, &modelProfileBindingID, &modelProfileKey,
			&externalTaskID, &task.Status, &task.Input, &normalizedOutput, &task.PollCount,
		)
		if err != nil {
			return gatewayVideoTask{}, err
		}
	} else {
		if strings.TrimSpace(req.ProviderAccountID) == "" || strings.TrimSpace(req.ExternalTaskID) == "" {
			return gatewayVideoTask{}, fmt.Errorf("%w: providerAsyncTaskId or providerAccountId/externalTaskId is required", ErrValidation)
		}
		err := s.db.QueryRow(ctx, gatewayVideoTaskSelect(`WHERE organization_id = $1 AND provider_account_id = $2 AND external_task_id = $3`), req.OrganizationID, req.ProviderAccountID, req.ExternalTaskID).Scan(
			&task.ID, &task.ProviderCallID, &task.OrganizationID, &projectID, &workflowRunID, &nodeRunID,
			&task.ProviderAccountID, &providerModelID, &credentialID, &modelProfileID, &modelProfileBindingID, &modelProfileKey,
			&externalTaskID, &task.Status, &task.Input, &normalizedOutput, &task.PollCount,
		)
		if err != nil {
			return gatewayVideoTask{}, err
		}
	}
	task.ProjectID = nullStringText(projectID)
	task.WorkflowRunID = nullStringText(workflowRunID)
	task.NodeRunID = nullStringText(nodeRunID)
	task.ProviderModelID = nullStringText(providerModelID)
	task.CredentialID = nullStringText(credentialID)
	task.ModelProfileID = nullStringText(modelProfileID)
	task.ModelProfileBindingID = nullStringText(modelProfileBindingID)
	task.ModelProfileKey = nullStringText(modelProfileKey)
	task.ExternalTaskID = nullStringText(externalTaskID)
	task.NormalizedOutput = rawOrDefault(normalizedOutput, "{}")
	if task.ProviderModelID == "" {
		return gatewayVideoTask{}, fmt.Errorf("%w: provider async task has no provider model", ErrValidation)
	}
	return task, nil
}

func gatewayVideoTaskSelect(where string) string {
	return `
		SELECT id, provider_call_id, organization_id, project_id, workflow_run_id, node_run_id,
		       provider_account_id, provider_model_id, credential_id, model_profile_id, model_profile_binding_id, model_profile_key,
		       external_task_id, status, input, normalized_output, poll_count
		FROM provider_async_tasks
	` + where
}

func downloadGatewayVideoURL(ctx context.Context, rawURL, upstreamMimeType string, timeout time.Duration) (gatewayVideoMedia, error) {
	if err := validateGatewayVideoURL(rawURL); err != nil {
		return gatewayVideoMedia{}, err
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return validateGatewayVideoURL(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return gatewayVideoMedia{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return gatewayVideoMedia{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return gatewayVideoMedia{}, fmt.Errorf("provider video download failed: status=%d", resp.StatusCode)
	}
	body, err := readLimitedVideoBody(resp.Body, gatewayVideoMaxBytes())
	if err != nil {
		return gatewayVideoMedia{}, err
	}
	mimeType := normalizeMediaType(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = normalizeMediaType(upstreamMimeType)
	}
	if mimeType == "" {
		mimeType = mimeTypeFromURL(rawURL)
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = "video/mp4"
	}
	return gatewayVideoMedia{Body: body, MimeType: mimeType, ByteSize: int64(len(body)), ContentHash: sha256ContentHash(body)}, nil
}

func readLimitedVideoBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("provider video exceeds %d byte limit", maxBytes)
	}
	return body, nil
}

func validateGatewayVideoURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("provider video URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("provider video URL must use http or https")
	}
	if strings.EqualFold(os.Getenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS"), "true") {
		return nil
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("provider video URL points to a private host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateProviderMediaIP(ip) {
			return fmt.Errorf("provider video URL points to a private address")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("provider video URL host could not be resolved")
	}
	for _, ip := range ips {
		if isPrivateProviderMediaIP(ip) {
			return fmt.Errorf("provider video URL resolves to a private address")
		}
	}
	return nil
}

func gatewayVideoStorageKey(organizationID, projectID, mimeType, sourceURL string) string {
	now := time.Now().UTC()
	ext := videoFileExtension(mimeType, sourceURL)
	if strings.TrimSpace(projectID) != "" {
		return fmt.Sprintf("org/%s/project/%s/provider-videos/%04d/%02d/%s%s", organizationID, projectID, now.Year(), int(now.Month()), uuid.NewString(), ext)
	}
	return fmt.Sprintf("org/%s/provider-videos/%04d/%02d/%s%s", organizationID, now.Year(), int(now.Month()), uuid.NewString(), ext)
}

func videoFileExtension(mimeType, sourceURL string) string {
	switch normalizeMediaType(mimeType) {
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	if sourceURL != "" {
		parsed, err := url.Parse(sourceURL)
		if err == nil {
			ext := strings.ToLower(path.Ext(parsed.Path))
			switch ext {
			case ".mp4", ".webm", ".mov":
				return ext
			}
		}
	}
	if ext, _ := mime.ExtensionsByType(normalizeMediaType(mimeType)); len(ext) > 0 {
		return ext[0]
	}
	return ".mp4"
}

func estimateVideoCost(input gatewayVideoInput, durationSeconds *float64, capabilities []Capability) GatewayUsage {
	currency := "USD"
	seconds := input.DurationSeconds
	if durationSeconds != nil && *durationSeconds > 0 {
		seconds = *durationSeconds
	}
	amount := 0.0
	for _, capability := range capabilities {
		var policy map[string]any
		if err := json.Unmarshal(capability.PricingPolicy, &policy); err != nil || len(policy) == 0 {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		if value, ok := videoCostByResolution(policy, input.Resolution); ok {
			amount = value * seconds
			break
		}
		if value, ok := imageCostValue(policy["videoCostPerSecond"]); ok {
			amount = value * seconds
			break
		}
		if value, ok := imageCostValue(policy["videoCostFlat"]); ok {
			amount = value
			break
		}
		break
	}
	return GatewayUsage{EstimatedCost: strconv.FormatFloat(math.Round(amount*1e8)/1e8, 'f', 8, 64), Currency: currency}
}

func videoCostByResolution(policy map[string]any, resolution string) (float64, bool) {
	if strings.TrimSpace(resolution) == "" {
		return 0, false
	}
	values, ok := policy["videoCostByResolution"].(map[string]any)
	if !ok {
		return 0, false
	}
	return imageCostValue(values[resolution])
}

func normalizeGatewayVideoStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queued", "pending":
		return "queued"
	case "running", "processing", "in_progress", "in-progress":
		return "running"
	case "succeeded", "success", "completed", "done":
		return "succeeded"
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return ""
	}
}

func videoStringField(raw json.RawMessage, keys ...string) string {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := decoded[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func videoFloatField(raw json.RawMessage, keys ...string) float64 {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0
	}
	for _, key := range keys {
		if value := floatField(decoded[key], key); value > 0 {
			return value
		}
	}
	return 0
}

func videoStringOption(decoded map[string]any, key string) string {
	value, _ := decoded[key].(string)
	return strings.TrimSpace(value)
}

func floatField(value any, key string) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func accountConfigString(raw json.RawMessage, key string) string {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	value, _ := decoded[key].(string)
	return strings.TrimSpace(value)
}

func modelProviderOptionString(model Model, key string) string {
	for _, capability := range model.Capabilities {
		var decoded map[string]any
		if err := json.Unmarshal(capability.ProviderOptionsSchema, &decoded); err != nil {
			continue
		}
		if value, ok := decoded[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if nested, ok := decoded["providerOptions"].(map[string]any); ok {
			if value, ok := nested[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func rawJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	return value
}

func gatewayVideoTimeout(optionMS, endpointMS int) time.Duration {
	ms := firstPositive(optionMS, endpointMS, 120000)
	return time.Duration(ms) * time.Millisecond
}

func gatewayVideoMaxBytes() int64 {
	value := strings.TrimSpace(os.Getenv("CINEWEAVE_PROVIDER_VIDEO_MAX_BYTES"))
	if value == "" {
		return defaultGatewayVideoMaxBytes
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultGatewayVideoMaxBytes
	}
	return parsed
}

func gatewayVideoIdempotencyKey(value string, options GatewayVideoOptions) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(options.IdempotencyKey)
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStringText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
