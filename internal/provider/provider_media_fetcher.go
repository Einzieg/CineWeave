package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider/outbound"
)

type providerMediaEgressConfig struct {
	AllowedPrivateHosts []string `json:"allowedPrivateHosts"`
	AllowedPrivateCIDRs []string `json:"allowedPrivateCidrs"`
}

type gatewayMediaStageError struct {
	stage string
	err   error
}

func (e *gatewayMediaStageError) Error() string {
	if e == nil {
		return "provider media transfer failed"
	}
	return fmt.Sprintf("provider media %s failed: %v", e.stage, e.err)
}

func (e *gatewayMediaStageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func gatewayMediaStageFailure(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &gatewayMediaStageError{stage: strings.TrimSpace(stage), err: err}
}

func (s *Service) SetMediaFetcher(fetcher *outbound.MediaFetcher) {
	if fetcher == nil {
		s.mediaFetcher = outbound.NewMediaFetcher(outbound.Config{})
		return
	}
	s.mediaFetcher = fetcher
}

func (s *Service) downloadGatewayImageURL(ctx context.Context, account Account, rawURL, upstreamMIMEType string, timeout time.Duration) (gatewayImageMedia, error) {
	result, err := s.fetchProviderMedia(ctx, account, rawURL, outbound.FetchOptions{
		Kind:                  "image",
		MaxBytes:              maxGatewayImageBytes,
		Timeout:               timeout,
		ResponseHeaderTimeout: firstByteTimeout(timeout),
		UpstreamMIMEType:      upstreamMIMEType,
		AllowedMIMEPrefixes:   []string{"image/"},
	})
	if err != nil {
		return gatewayImageMedia{}, err
	}
	width, height := imageDimensionsFromFile(result.Path)
	return gatewayImageMedia{
		TempPath:    result.Path,
		MimeType:    result.MIMEType,
		ByteSize:    result.ByteSize,
		ContentHash: result.ContentHash,
		Width:       width,
		Height:      height,
	}, nil
}

func (s *Service) downloadGatewayVideoURL(ctx context.Context, selection gatewayModelSelection, externalTaskID, rawURL, upstreamMIMEType string, timeout time.Duration) (gatewayVideoMedia, error) {
	fetchURL, requestHeaders, authenticatedOrigin, err := gatewayVideoMediaRequest(selection, externalTaskID, rawURL)
	if err != nil {
		return gatewayVideoMedia{}, err
	}
	result, err := s.fetchProviderMedia(ctx, selection.Account, fetchURL, outbound.FetchOptions{
		Kind:                  "video",
		MaxBytes:              gatewayVideoMaxBytes(),
		Timeout:               timeout,
		ResponseHeaderTimeout: firstByteTimeout(timeout),
		UpstreamMIMEType:      upstreamMIMEType,
		AllowedMIMEPrefixes:   []string{"video/"},
		RequestHeaders:        requestHeaders,
		AuthenticatedOrigin:   authenticatedOrigin,
	})
	if err != nil {
		return gatewayVideoMedia{}, err
	}
	return gatewayVideoMedia{
		TempPath:    result.Path,
		MimeType:    result.MIMEType,
		ByteSize:    result.ByteSize,
		ContentHash: result.ContentHash,
	}, nil
}

func gatewayVideoMediaRequest(selection gatewayModelSelection, externalTaskID, rawURL string) (string, http.Header, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil, "", fmt.Errorf("%w: provider video URL is required", ErrValidation)
	}
	config := parseOpenAICompatibleConfig(selection.Account.Config)
	if !usesNativeOpenAICompatibleRuntime(selection.Account) || !strings.EqualFold(strings.TrimSpace(config.VideoProtocol), "new_api") {
		return rawURL, nil, "", nil
	}
	resultURL, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, "", fmt.Errorf("%w: provider video URL is invalid", ErrValidation)
	}
	expectedPath := "/v1/videos/" + strings.TrimSpace(externalTaskID) + "/content"
	if strings.TrimSpace(externalTaskID) == "" || path.Clean(resultURL.Path) != expectedPath {
		return rawURL, nil, "", nil
	}
	proxyURL, err := buildProviderURL(selection.Account.BaseURL, expectedPath, false)
	if err != nil {
		return "", nil, "", err
	}
	proxyTarget, err := url.Parse(proxyURL)
	if err != nil || proxyTarget.Scheme == "" || proxyTarget.Host == "" {
		return "", nil, "", fmt.Errorf("%w: provider video proxy URL is invalid", ErrValidation)
	}
	request, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		return "", nil, "", fmt.Errorf("%w: provider video proxy request is invalid", ErrValidation)
	}
	request.Header.Set("Accept", "video/*")
	applyAuth(request, selection.Account.AuthType, selection.APIKey)
	authenticatedOrigin := strings.ToLower(proxyTarget.Scheme) + "://" + strings.ToLower(proxyTarget.Host)
	return proxyURL, request.Header.Clone(), authenticatedOrigin, nil
}

func (s *Service) fetchProviderMedia(ctx context.Context, account Account, rawURL string, options outbound.FetchOptions) (outbound.Result, error) {
	if s.mediaFetcher == nil {
		return outbound.Result{}, fmt.Errorf("%w: provider media fetcher is not configured", ErrValidation)
	}
	policy, err := providerMediaRequestPolicy(account.Config)
	if err != nil {
		return outbound.Result{}, err
	}
	options.Policy = policy
	return s.mediaFetcher.FetchToTempFile(ctx, rawURL, options)
}

func providerMediaRequestPolicy(raw json.RawMessage) (outbound.RequestPolicy, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return outbound.RequestPolicy{}, nil
	}
	var config struct {
		MediaEgress providerMediaEgressConfig `json:"mediaEgress"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return outbound.RequestPolicy{}, fmt.Errorf("%w: provider account media egress config is invalid", ErrValidation)
	}
	policy := outbound.RequestPolicy{
		AllowedPrivateHosts: make([]string, 0, len(config.MediaEgress.AllowedPrivateHosts)),
		AllowedPrivateCIDRs: make([]netip.Prefix, 0, len(config.MediaEgress.AllowedPrivateCIDRs)),
	}
	for _, host := range config.MediaEgress.AllowedPrivateHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		policy.AllowedPrivateHosts = append(policy.AllowedPrivateHosts, host)
	}
	for _, value := range config.MediaEgress.AllowedPrivateCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return outbound.RequestPolicy{}, fmt.Errorf("%w: provider account media egress CIDR %q is invalid", ErrValidation, value)
		}
		policy.AllowedPrivateCIDRs = append(policy.AllowedPrivateCIDRs, prefix.Masked())
	}
	if len(policy.AllowedPrivateHosts) == 0 && len(policy.AllowedPrivateCIDRs) > 0 || len(policy.AllowedPrivateHosts) > 0 && len(policy.AllowedPrivateCIDRs) == 0 {
		return outbound.RequestPolicy{}, fmt.Errorf("%w: private media egress requires both exact hosts and CIDRs", ErrValidation)
	}
	return policy, nil
}

func firstByteTimeout(total time.Duration) time.Duration {
	const maximum = 30 * time.Second
	if total <= 0 || total > maximum {
		return maximum
	}
	return total
}

func imageDimensionsFromFile(filePath string) (*int, *int) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, nil
	}
	width := config.Width
	height := config.Height
	return &width, &height
}

func readGatewayMediaFile(filePath string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() > maxBytes {
		return nil, fmt.Errorf("provider media exceeds %d byte limit", maxBytes)
	}
	body, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("provider media exceeds %d byte limit", maxBytes)
	}
	return body, nil
}

func (media gatewayImageMedia) close() {
	if strings.TrimSpace(media.TempPath) != "" {
		_ = os.Remove(media.TempPath)
	}
}

func (media gatewayVideoMedia) close() {
	if strings.TrimSpace(media.TempPath) != "" {
		_ = os.Remove(media.TempPath)
	}
}

func (result audioSpeechResult) close() {
	if strings.TrimSpace(result.TempPath) != "" {
		_ = os.Remove(result.TempPath)
	}
}

func normalizedGatewayMediaFailure(err error, kind string) (string, string, *StandardError) {
	if standard, ok := StandardErrorFromError(err); ok {
		return standard.Code, standard.Message, standard
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "media"
	}
	stage := ""
	var stageError *gatewayMediaStageError
	if errors.As(err, &stageError) {
		stage = stageError.stage
	}
	var code, message string
	var retryable bool
	switch {
	case errors.Is(err, outbound.ErrBlockedAddress):
		code = CodeInvalidRequest
		message = fmt.Sprintf("provider returned a blocked %s URL", kind)
	case errors.Is(err, outbound.ErrInvalidURL):
		code = CodeInvalidRequest
		message = fmt.Sprintf("provider returned an invalid %s URL", kind)
	case errors.Is(err, outbound.ErrResponseTooLarge):
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s exceeds the configured size limit", kind)
	case errors.Is(err, outbound.ErrMIMETypeRejected):
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider returned an unsupported %s media type", kind)
	case errors.Is(err, context.DeadlineExceeded) && stage == "storage":
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s object storage upload timed out", kind)
		retryable = true
	case errors.Is(err, context.DeadlineExceeded):
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s download timed out", kind)
		retryable = true
	case stage == "storage":
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s object storage upload failed", kind)
		retryable = true
	case stage == "download":
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s download failed", kind)
		retryable = true
	default:
		code = CodeMediaDownloadFailed
		message = fmt.Sprintf("provider %s media could not be stored", kind)
		retryable = true
	}
	return code, message, &StandardError{Code: code, Message: message, Retryable: retryable}
}
