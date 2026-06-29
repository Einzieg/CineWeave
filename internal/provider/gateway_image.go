package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxGatewayImageBytes int64 = 64 << 20

type ObjectStorage interface {
	PutBytes(ctx context.Context, key string, body []byte, contentType string) (storage.PutResult, error)
}

type gatewayImageInput struct {
	Prompt  string
	Size    string
	Quality string
	N       int
}

type gatewayImageMedia struct {
	Body        []byte
	MimeType    string
	ByteSize    int64
	ContentHash string
	Width       *int
	Height      *int
}

type gatewayStoredImage struct {
	ArtifactID  string
	MediaFileID string
	Output      GatewayImageOutput
	Media       gatewayImageMedia
}

func (s *Service) SetStorage(objectStorage ObjectStorage) {
	s.objectStorage = objectStorage
}

func (s *Service) GenerateImage(ctx context.Context, req GatewayImageRequest) (GatewayImageResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayImageResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayImageResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	imageInput, err := parseGatewayImageInput(input)
	if err != nil {
		return GatewayImageResponse{}, err
	}
	if s.objectStorage == nil {
		return GatewayImageResponse{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
	}
	req.Input = input

	if strings.TrimSpace(req.ProviderModelID) != "" {
		selection, err := s.selectGatewayImageModel(ctx, req)
		if err != nil {
			return GatewayImageResponse{}, err
		}
		response, _, err := s.executeGatewayImageAttempt(ctx, req, imageInput, selection, 1, 1, string(RoutingPriority))
		return response, err
	}

	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  req.OrganizationID,
		ModelProfileKey: req.ModelProfileKey,
		TaskType:        TaskTypeImageGenerate,
		Modality:        "image",
		ImageSize:       imageInput.Size,
		ImageQuality:    imageInput.Quality,
	})
	if err != nil {
		return GatewayImageResponse{}, err
	}
	strategy := candidates[0].FallbackStrategy
	maxAttempts := fallbackMaxAttempts(strategy, len(candidates))
	attempts := make([]GatewayAttempt, 0, maxAttempts)
	var final GatewayImageResponse
	for i := 0; i < maxAttempts; i++ {
		candidate := candidates[i]
		selection, err := s.completeGatewaySelectionFromCandidate(ctx, req.OrganizationID, candidate)
		if err != nil {
			return GatewayImageResponse{}, err
		}
		response, attempt, err := s.executeGatewayImageAttempt(ctx, req, imageInput, selection, i+1, maxAttempts, candidate.RoutingStrategy)
		if err != nil {
			return GatewayImageResponse{}, err
		}
		attempts = append(attempts, attempt)
		response.Attempts = append([]GatewayAttempt(nil), attempts...)
		final = response
		if response.Status == "succeeded" {
			return response, nil
		}
		if i+1 >= maxAttempts || !shouldFallback(gatewayErrorCode(response.Error), strategy) {
			return response, nil
		}
	}
	return final, nil
}

func (s *Service) executeGatewayImageAttempt(ctx context.Context, req GatewayImageRequest, imageInput gatewayImageInput, selection gatewayModelSelection, attemptIndex, maxAttempts int, selectedBy string) (GatewayImageResponse, GatewayAttempt, error) {
	cfg := parseOpenAICompatibleConfig(selection.Account.Config)
	if req.Options.TimeoutMS > 0 {
		cfg.TimeoutMS = req.Options.TimeoutMS
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callID := uuid.NewString()
	usage := estimateImageCost(imageInput, selection.Model.Capabilities)
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: req.OrganizationID,
		Selection:      selection,
		TaskType:       TaskTypeImageGenerate,
		EstimatedCost:  usage.EstimatedCost,
		Currency:       usage.Currency,
		LeaseTTL:       timeout + 30*time.Second,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		standard, ok := blockedGatewayStandard(guardErr)
		if !ok {
			return GatewayImageResponse{}, GatewayAttempt{}, guardErr
		}
		call, err := s.recordGatewayImageCall(ctx, selection, req, callID, RecordCallRequest{
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
			IdempotencyKey:        gatewayImageIdempotencyKey(req),
			TaskType:              TaskTypeImageGenerate,
			ExecutionMode:         "sync",
			Status:                "blocked",
			ErrorCode:             standard.Code,
			ErrorMessage:          standard.Message,
			RequestSnapshot:       req.Input,
			ResponseSnapshot:      blockedResponseSnapshot(standard),
			NormalizedOutput:      withRoutingNormalizedOutput(blockedNormalizedOutput(standard), selection, attemptIndex, maxAttempts, selectedBy),
		}, usage, nil, imageInput, imageGenerationResult{})
		if err != nil {
			return GatewayImageResponse{}, GatewayAttempt{}, err
		}
		attempt := gatewayAttemptFromCall(call, selection, standard, 0)
		return GatewayImageResponse{
			ProviderCallID: call.ID,
			ModelID:        selection.Model.ID,
			Status:         "blocked",
			Usage:          GatewayUsage{EstimatedCost: "0.00000000", Currency: usage.Currency},
			Error:          standard,
			Attempts:       []GatewayAttempt{attempt},
		}, attempt, nil
	}
	providerCallID := ""
	defer func() {
		s.releaseGatewayLease(lease, providerCallID)
	}()

	client := newOpenAICompatibleClient(timeout)
	started := time.Now()
	result, runErr := client.imageGeneration(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, req.Input)
	latencyMS := int(time.Since(started).Milliseconds())
	if result.LatencyMS > latencyMS {
		latencyMS = result.LatencyMS
	}

	status := "succeeded"
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	responseSnapshot := result.ResponseSnapshot
	normalizedOutput := result.NormalizedOutput
	output := GatewayImageOutput{}

	var stored *gatewayStoredImage
	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	} else {
		media, mediaErr := materializeGatewayImageMedia(callCtx, result, timeout)
		if mediaErr == nil {
			stored, mediaErr = s.storeGatewayImageMedia(callCtx, callID, req, selection, result, media, imageInput)
		}
		if mediaErr != nil {
			status = "failed"
			errorCode = CodeMediaDownloadFailed
			errorMessage = mediaErr.Error()
			standardError = &StandardError{Code: CodeMediaDownloadFailed, Message: "provider image media could not be stored", Retryable: true}
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		} else {
			output = stored.Output
			normalizedOutput = mustJSON(output)
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = json.RawMessage(`null`)
	}
	normalizedOutput = withRoutingNormalizedOutput(normalizedOutput, selection, attemptIndex, maxAttempts, selectedBy)

	call, err := s.recordGatewayImageCall(ctx, selection, req, callID, RecordCallRequest{
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
		LeaseID:               lease.LeaseID,
		IdempotencyKey:        gatewayImageIdempotencyKey(req),
		TaskType:              TaskTypeImageGenerate,
		ExecutionMode:         "sync",
		Status:                status,
		LatencyMS:             &latencyMS,
		EstimatedCost:         usage.EstimatedCost,
		Currency:              usage.Currency,
		ErrorCode:             errorCode,
		ErrorMessage:          errorMessage,
		UpstreamStatus:        upstreamStatus,
		UpstreamErrorCode:     upstreamErrorCode,
		RequestSnapshot:       result.RequestSnapshot,
		ResponseSnapshot:      responseSnapshot,
		NormalizedOutput:      normalizedOutput,
	}, usage, stored, imageInput, result)
	if err != nil {
		return GatewayImageResponse{}, GatewayAttempt{}, err
	}
	providerCallID = call.ID
	if runErr != nil {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}

	attempt := gatewayAttemptFromCall(call, selection, standardError, latencyMS)
	return GatewayImageResponse{
		ProviderCallID: call.ID,
		ModelID:        selection.Model.ID,
		Status:         status,
		Output:         output,
		Usage:          usage,
		Error:          standardError,
		LatencyMS:      latencyMS,
		Attempts:       []GatewayAttempt{attempt},
	}, attempt, nil
}

func (s *Service) selectGatewayImageModel(ctx context.Context, req GatewayImageRequest) (gatewayModelSelection, error) {
	if strings.TrimSpace(req.ProviderModelID) != "" {
		model, err := s.GetModel(ctx, req.OrganizationID, req.ProviderModelID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		if model.Status != "active" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
		}
		if model.Modality != "image" && model.Modality != "multimodal" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support image generation", ErrValidation)
		}
		account, err := s.GetAccount(ctx, req.OrganizationID, model.ProviderAccountID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		return s.completeGatewaySelection(ctx, req.OrganizationID, account, model, "", "", "")
	}

	profileKey := strings.TrimSpace(req.ModelProfileKey)
	if profileKey == "" {
		return gatewayModelSelection{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  req.OrganizationID,
		ModelProfileKey: profileKey,
		TaskType:        TaskTypeImageGenerate,
		Modality:        "image",
	})
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return s.completeGatewaySelectionFromCandidate(ctx, req.OrganizationID, candidates[0])
}

func parseGatewayImageInput(input json.RawMessage) (gatewayImageInput, error) {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return gatewayImageInput{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	prompt, _ := decoded["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return gatewayImageInput{}, fmt.Errorf("%w: input.prompt is required", ErrValidation)
	}
	n := imageRequestCount(decoded["n"])
	if n <= 0 {
		n = 1
	}
	if n > 1 {
		return gatewayImageInput{}, fmt.Errorf("%w: image.generate only supports n=1 in this version", ErrValidation)
	}
	return gatewayImageInput{
		Prompt:  prompt,
		Size:    imageStringOption(decoded, "size", "1024x1024"),
		Quality: imageStringOption(decoded, "quality", ""),
		N:       n,
	}, nil
}

func materializeGatewayImageMedia(ctx context.Context, result imageGenerationResult, timeout time.Duration) (gatewayImageMedia, error) {
	if strings.TrimSpace(result.ImageURL) != "" {
		return downloadGatewayImageURL(ctx, result.ImageURL, result.MimeType, timeout)
	}
	if strings.TrimSpace(result.B64JSON) != "" {
		return decodeGatewayImageBase64(result.B64JSON, result.MimeType)
	}
	return gatewayImageMedia{}, fmt.Errorf("%w: provider image response did not include media", ErrValidation)
}

func (s *Service) storeGatewayImageMedia(ctx context.Context, callID string, req GatewayImageRequest, selection gatewayModelSelection, result imageGenerationResult, media gatewayImageMedia, input gatewayImageInput) (*gatewayStoredImage, error) {
	storageKey := gatewayImageStorageKey(req.OrganizationID, req.ProjectID, media.MimeType, result.ImageURL)
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
	artifactID := uuid.NewString()
	mediaFileID := uuid.NewString()
	output := GatewayImageOutput{
		ArtifactID:  artifactID,
		MediaFileID: mediaFileID,
		StorageKey:  put.StorageKey,
		MimeType:    media.MimeType,
		Width:       media.Width,
		Height:      media.Height,
		Raw:         result.NormalizedOutput,
	}
	return &gatewayStoredImage{
		ArtifactID:  artifactID,
		MediaFileID: mediaFileID,
		Output:      output,
		Media:       media,
	}, nil
}

func (s *Service) recordGatewayImageCall(ctx context.Context, selection gatewayModelSelection, req GatewayImageRequest, callID string, callReq RecordCallRequest, usage GatewayUsage, stored *gatewayStoredImage, imageInput gatewayImageInput, result imageGenerationResult) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)

	if stored != nil {
		if err := insertGatewayImageArtifact(ctx, tx, selection, req, callID, stored, imageInput); err != nil {
			return CallLog{}, err
		}
		if err := insertGatewayImageMediaFile(ctx, tx, selection, req, callID, stored, result); err != nil {
			return CallLog{}, err
		}
		callReq.ArtifactIDs = mustJSON([]string{stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, err
	}
	if callReq.Status != "blocked" && stored != nil {
		if err := insertImageCostRecord(ctx, tx, call.ID, selection, req, usage, imageInput); err != nil {
			return CallLog{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func insertGatewayImageArtifact(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, req GatewayImageRequest, callID string, stored *gatewayStoredImage, input gatewayImageInput) error {
	metadata := mustJSON(map[string]any{
		"providerCallId":  callID,
		"providerModelId": selection.Model.ID,
		"mediaFileId":     stored.MediaFileID,
		"prompt":          input.Prompt,
		"size":            input.Size,
		"quality":         input.Quality,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO artifacts(
			id, organization_id, project_id, workflow_run_id, node_run_id, type,
			storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 'generated_image', $6, $7, $8, $9, $10, $11, NULL)
	`,
		stored.ArtifactID,
		req.OrganizationID,
		nullString(req.ProjectID),
		nullString(req.WorkflowRunID),
		nullString(req.NodeRunID),
		stored.Output.StorageKey,
		stored.Output.MimeType,
		stored.Media.ContentHash,
		nullString(req.PromptHash),
		selection.Model.ID,
		metadata,
	)
	return err
}

func insertGatewayImageMediaFile(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, req GatewayImageRequest, callID string, stored *gatewayStoredImage, result imageGenerationResult) error {
	metadata := mustJSON(map[string]any{
		"source":          "provider_gateway",
		"providerCallId":  callID,
		"providerModelId": selection.Model.ID,
		"upstream": map[string]any{
			"responseType": result.ResponseType,
		},
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO media_files(
			id, organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, duration_seconds, checksum, created_by, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, $10, NULL, $11)
	`,
		stored.MediaFileID,
		req.OrganizationID,
		nullString(req.ProjectID),
		stored.ArtifactID,
		stored.Output.StorageKey,
		stored.Output.MimeType,
		stored.Media.ByteSize,
		nullInt(stored.Media.Width),
		nullInt(stored.Media.Height),
		stored.Media.ContentHash,
		metadata,
	)
	return err
}

func insertImageCostRecord(ctx context.Context, tx pgx.Tx, providerCallID string, selection gatewayModelSelection, req GatewayImageRequest, usage GatewayUsage, input gatewayImageInput) error {
	metadata := mustJSON(map[string]any{
		"imageCount": 1,
		"size":       input.Size,
		"quality":    input.Quality,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO cost_records(
			organization_id, project_id, workflow_run_id, node_run_id,
			provider_call_id, provider_model_id, credential_id, model_profile_id,
			cost_type, amount, currency, unit, quantity, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'image.generate', $9::numeric, $10, 'image', 1, $11)
	`,
		req.OrganizationID,
		nullString(req.ProjectID),
		nullString(req.WorkflowRunID),
		nullString(req.NodeRunID),
		providerCallID,
		selection.Model.ID,
		selection.CredentialID,
		nullString(selection.ModelProfileID),
		costOrZero(usage.EstimatedCost),
		currencyOrDefault(usage.Currency),
		metadata,
	)
	return err
}

func estimateImageCost(input gatewayImageInput, capabilities []Capability) GatewayUsage {
	currency := "USD"
	amount := 0.0
	for _, capability := range capabilities {
		var policy map[string]any
		if err := json.Unmarshal(capability.PricingPolicy, &policy); err != nil || len(policy) == 0 {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		if value, ok := nestedImageCost(policy, input.Size, input.Quality); ok {
			amount = value
			break
		}
		sizeValue, hasSize := imageCostMapValue(policy, "imageCostBySize", input.Size)
		qualityValue, hasQuality := imageCostMapValue(policy, "imageCostByQuality", input.Quality)
		switch {
		case hasSize && hasQuality:
			amount = qualityValue
		case hasSize:
			amount = sizeValue
		case hasQuality:
			amount = qualityValue
		default:
			amount, _ = imageCostValue(policy["imageCost"])
		}
		break
	}
	return GatewayUsage{
		EstimatedCost: strconv.FormatFloat(math.Round(amount*1e8)/1e8, 'f', 8, 64),
		Currency:      currency,
	}
}

func nestedImageCost(policy map[string]any, size, quality string) (float64, bool) {
	if size == "" || quality == "" {
		return 0, false
	}
	for _, key := range []string{"imageCostBySizeAndQuality", "imageCostByQualityAndSize"} {
		value, ok := policy[key].(map[string]any)
		if !ok {
			continue
		}
		if direct, ok := imageCostValue(value[size+":"+quality]); ok {
			return direct, true
		}
		if bySize, ok := value[size].(map[string]any); ok {
			if amount, ok := imageCostValue(bySize[quality]); ok {
				return amount, true
			}
		}
	}
	return 0, false
}

func imageCostMapValue(policy map[string]any, key, lookup string) (float64, bool) {
	if lookup == "" {
		return 0, false
	}
	values, ok := policy[key].(map[string]any)
	if !ok {
		return 0, false
	}
	return imageCostValue(values[lookup])
}

func imageCostValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func downloadGatewayImageURL(ctx context.Context, rawURL, upstreamMimeType string, timeout time.Duration) (gatewayImageMedia, error) {
	if err := validateGatewayImageURL(rawURL); err != nil {
		return gatewayImageMedia{}, err
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return validateGatewayImageURL(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return gatewayImageMedia{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return gatewayImageMedia{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return gatewayImageMedia{}, fmt.Errorf("provider image download failed: status=%d", resp.StatusCode)
	}
	body, err := readLimitedImageBody(resp.Body)
	if err != nil {
		return gatewayImageMedia{}, err
	}
	mimeType := normalizeMediaType(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = normalizeMediaType(upstreamMimeType)
	}
	if mimeType == "" {
		mimeType = mimeTypeFromURL(rawURL)
	}
	return finalizeGatewayImageMedia(body, mimeType), nil
}

func decodeGatewayImageBase64(value, upstreamMimeType string) (gatewayImageMedia, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) == 2 {
			if mimeValue := strings.TrimPrefix(strings.SplitN(parts[0], ";", 2)[0], "data:"); mimeValue != "" {
				upstreamMimeType = mimeValue
			}
			value = parts[1]
		}
	}
	body, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		body, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		return gatewayImageMedia{}, fmt.Errorf("provider image b64_json is invalid")
	}
	if int64(len(body)) > maxGatewayImageBytes {
		return gatewayImageMedia{}, fmt.Errorf("provider image exceeds 64MB limit")
	}
	mimeType := normalizeMediaType(upstreamMimeType)
	return finalizeGatewayImageMedia(body, mimeType), nil
}

func readLimitedImageBody(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxGatewayImageBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxGatewayImageBytes {
		return nil, fmt.Errorf("provider image exceeds 64MB limit")
	}
	return body, nil
}

func finalizeGatewayImageMedia(body []byte, mimeType string) gatewayImageMedia {
	if mimeType == "" {
		mimeType = http.DetectContentType(body)
	}
	if mimeType == "application/octet-stream" {
		if detected := http.DetectContentType(body); detected != "" {
			mimeType = detected
		}
	}
	width, height := imageDimensions(body)
	return gatewayImageMedia{
		Body:        body,
		MimeType:    normalizeMediaType(mimeType),
		ByteSize:    int64(len(body)),
		ContentHash: sha256ContentHash(body),
		Width:       width,
		Height:      height,
	}
}

func imageDimensions(body []byte) (*int, *int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, nil
	}
	width := cfg.Width
	height := cfg.Height
	return &width, &height
}

func validateGatewayImageURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("provider image URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("provider image URL must use http or https")
	}
	if strings.EqualFold(os.Getenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS"), "true") {
		return nil
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("provider image URL points to a private host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateProviderMediaIP(ip) {
			return fmt.Errorf("provider image URL points to a private address")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("provider image URL host could not be resolved")
	}
	for _, ip := range ips {
		if isPrivateProviderMediaIP(ip) {
			return fmt.Errorf("provider image URL resolves to a private address")
		}
	}
	return nil
}

func isPrivateProviderMediaIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

func normalizeMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(value)
	}
	return strings.ToLower(mediaType)
}

func mimeTypeFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	if ext == "" {
		return ""
	}
	return normalizeMediaType(mime.TypeByExtension(ext))
}

func gatewayImageStorageKey(organizationID, projectID, mimeType, sourceURL string) string {
	now := time.Now().UTC()
	ext := imageFileExtension(mimeType, sourceURL)
	if strings.TrimSpace(projectID) != "" {
		return fmt.Sprintf("org/%s/project/%s/provider-images/%04d/%02d/%s%s", organizationID, projectID, now.Year(), int(now.Month()), uuid.NewString(), ext)
	}
	return fmt.Sprintf("org/%s/provider-images/%04d/%02d/%s%s", organizationID, now.Year(), int(now.Month()), uuid.NewString(), ext)
}

func imageFileExtension(mimeType, sourceURL string) string {
	switch normalizeMediaType(mimeType) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	if sourceURL != "" {
		parsed, err := url.Parse(sourceURL)
		if err == nil {
			ext := strings.ToLower(path.Ext(parsed.Path))
			switch ext {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp":
				if ext == ".jpeg" {
					return ".jpg"
				}
				return ext
			}
		}
	}
	return ".bin"
}

func sha256ContentHash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func gatewayImageIdempotencyKey(req GatewayImageRequest) string {
	if value := strings.TrimSpace(req.IdempotencyKey); value != "" {
		return value
	}
	return strings.TrimSpace(req.Options.IdempotencyKey)
}

func errorsIsNoRows(err error) bool {
	return err == pgx.ErrNoRows || err == sql.ErrNoRows
}
