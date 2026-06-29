package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ProviderManifest struct {
	Kind      string                      `json:"kind"`
	Version   string                      `json:"version"`
	ID        string                      `json:"id"`
	Name      string                      `json:"name"`
	Transport string                      `json:"transport"`
	BaseURL   string                      `json:"baseUrl"`
	Auth      ManifestAuth                `json:"auth"`
	Models    []ManifestModel             `json:"models"`
	Endpoints map[string]ManifestEndpoint `json:"endpoints"`
}

type ManifestAuth struct {
	Type          string `json:"type"`
	Header        string `json:"header"`
	ValueTemplate string `json:"valueTemplate"`
}

type ManifestModel struct {
	ID           string          `json:"id"`
	DisplayName  string          `json:"displayName"`
	Modality     string          `json:"modality"`
	Capabilities json.RawMessage `json:"capabilities"`
}

type ManifestEndpoint struct {
	EndpointType    string          `json:"endpointType"`
	Method          string          `json:"method"`
	PathTemplate    string          `json:"pathTemplate"`
	HeadersTemplate json.RawMessage `json:"headersTemplate"`
	RequestTemplate json.RawMessage `json:"requestTemplate"`
	ResponseMapping json.RawMessage `json:"responseMapping"`
	TimeoutMS       int             `json:"timeoutMs"`
	PollEndpointKey string          `json:"pollEndpointKey"`
	PollIntervalMS  int             `json:"pollIntervalMs"`
	MaxPolls        int             `json:"maxPolls"`
}

type ManifestValidationIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ManifestValidationResult struct {
	Valid  bool                      `json:"valid"`
	Errors []ManifestValidationIssue `json:"errors"`
}

type ManifestTestRunRequest struct {
	AccountID       string          `json:"accountId"`
	EndpointKey     string          `json:"endpointKey"`
	PollEndpointKey string          `json:"pollEndpointKey"`
	Input           json.RawMessage `json:"input"`
	Manifest        json.RawMessage `json:"manifest"`
	ManifestText    string          `json:"manifestText"`
	MaxPolls        int             `json:"maxPolls"`
	IdempotencyKey  string          `json:"idempotencyKey,omitempty"`
}

type ValidateManifestRequest struct {
	Manifest     json.RawMessage `json:"manifest"`
	ManifestText string          `json:"manifestText"`
}

type ManifestTestRunResult struct {
	TestRunID        string          `json:"testRunId"`
	ProviderCallID   string          `json:"providerCallId"`
	EndpointKey      string          `json:"endpointKey"`
	Status           string          `json:"status"`
	LatencyMS        int             `json:"latencyMs"`
	ErrorCode        *string         `json:"errorCode,omitempty"`
	ErrorMessage     *string         `json:"errorMessage,omitempty"`
	NormalizedOutput json.RawMessage `json:"normalizedOutput"`
}

type manifestRunResult struct {
	Status           string
	LatencyMS        int
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
}

var manifestIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-_.]*$`)
var templatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.\-\[\]'"]+)\s*\}\}`)

type manifestCallContext struct {
	References []map[string]any
	Model      map[string]any
	Account    map[string]any
	Task       map[string]any
}

func ParseManifest(raw json.RawMessage, text string) (ProviderManifest, json.RawMessage, error) {
	var jsonBytes []byte
	switch {
	case strings.TrimSpace(text) != "":
		decoded, err := parseYAMLOrJSONText(text)
		if err != nil {
			return ProviderManifest{}, nil, err
		}
		jsonBytes = decoded
	case len(raw) > 0:
		if !json.Valid(raw) {
			return ProviderManifest{}, nil, fmt.Errorf("%w: manifest must be valid JSON", ErrValidation)
		}
		jsonBytes = raw
	default:
		return ProviderManifest{}, nil, fmt.Errorf("%w: manifest is required", ErrValidation)
	}

	var manifest ProviderManifest
	if err := json.Unmarshal(jsonBytes, &manifest); err != nil {
		return ProviderManifest{}, nil, fmt.Errorf("%w: manifest shape is invalid", ErrValidation)
	}
	return manifest, jsonBytes, nil
}

func ValidateManifest(manifest ProviderManifest) ManifestValidationResult {
	var issues []ManifestValidationIssue
	add := func(path, message string) {
		issues = append(issues, ManifestValidationIssue{Path: path, Message: message})
	}

	if manifest.Kind != "ProviderConnector" {
		add("$.kind", "kind must be ProviderConnector")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		add("$.version", "version is required")
	}
	if !manifestIDPattern.MatchString(manifest.ID) {
		add("$.id", "id must match ^[a-z0-9][a-z0-9-_.]*$")
	}
	if strings.TrimSpace(manifest.Name) == "" {
		add("$.name", "name is required")
	}
	if manifest.Transport != "http" {
		add("$.transport", "transport must be http")
	}
	if _, err := url.ParseRequestURI(manifest.BaseURL); strings.TrimSpace(manifest.BaseURL) == "" || err != nil {
		add("$.baseUrl", "baseUrl must be an absolute URI")
	}
	switch manifest.Auth.Type {
	case "none", "bearer", "api_key", "basic":
	default:
		add("$.auth.type", "auth.type must be one of none, bearer, api_key, basic")
	}
	if len(manifest.Models) == 0 {
		add("$.models", "at least one model is required")
	}
	for i, model := range manifest.Models {
		prefix := fmt.Sprintf("$.models[%d]", i)
		if strings.TrimSpace(model.ID) == "" {
			add(prefix+".id", "model id is required")
		}
		if strings.TrimSpace(model.DisplayName) == "" {
			add(prefix+".displayName", "displayName is required")
		}
		switch model.Modality {
		case "text", "image", "video", "audio", "embedding", "multimodal":
		default:
			add(prefix+".modality", "modality is invalid")
		}
		if len(model.Capabilities) == 0 || !json.Valid(model.Capabilities) {
			add(prefix+".capabilities", "capabilities must be valid JSON")
		}
	}
	if len(manifest.Endpoints) == 0 {
		add("$.endpoints", "at least one endpoint is required")
	}
	for key, endpoint := range manifest.Endpoints {
		prefix := fmt.Sprintf("$.endpoints.%s", key)
		if strings.TrimSpace(key) == "" {
			add("$.endpoints", "endpoint key is required")
		}
		switch endpointType(endpoint.EndpointType) {
		case "sync", "async_create", "async_poll":
		default:
			add(prefix+".endpointType", "endpointType must be sync, async_create, or async_poll")
		}
		switch strings.ToUpper(strings.TrimSpace(endpoint.Method)) {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			add(prefix+".method", "method is invalid")
		}
		if strings.TrimSpace(endpoint.PathTemplate) == "" {
			add(prefix+".pathTemplate", "pathTemplate is required")
		}
		if len(endpoint.HeadersTemplate) > 0 && !json.Valid(endpoint.HeadersTemplate) {
			add(prefix+".headersTemplate", "headersTemplate must be valid JSON")
		}
		if len(endpoint.RequestTemplate) > 0 && !json.Valid(endpoint.RequestTemplate) {
			add(prefix+".requestTemplate", "requestTemplate must be valid JSON")
		}
		if len(endpoint.ResponseMapping) == 0 || !json.Valid(endpoint.ResponseMapping) {
			add(prefix+".responseMapping", "responseMapping must be valid JSON")
		}
	}
	return ManifestValidationResult{
		Valid:  len(issues) == 0,
		Errors: issues,
	}
}

func runDeclarativeManifest(ctx context.Context, manifest ProviderManifest, account Account, credential map[string]any, req ManifestTestRunRequest) (manifestRunResult, error) {
	endpointKey := strings.TrimSpace(req.EndpointKey)
	if endpointKey == "" {
		return manifestRunResult{}, fmt.Errorf("%w: endpointKey is required", ErrValidation)
	}
	endpoint, ok := manifest.Endpoints[endpointKey]
	if !ok {
		return manifestRunResult{}, fmt.Errorf("%w: endpointKey was not found in manifest", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return manifestRunResult{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	switch endpointType(endpoint.EndpointType) {
	case "sync":
		return callManifestEndpoint(ctx, manifest, account, credential, endpointKey, endpoint, input)
	case "async_create":
		return runAsyncManifestEndpoint(ctx, manifest, account, credential, endpointKey, endpoint, input, req)
	default:
		return manifestRunResult{}, fmt.Errorf("%w: endpointKey must reference sync or async_create endpoint", ErrValidation)
	}
}

func runAsyncManifestEndpoint(ctx context.Context, manifest ProviderManifest, account Account, credential map[string]any, endpointKey string, endpoint ManifestEndpoint, input json.RawMessage, req ManifestTestRunRequest) (manifestRunResult, error) {
	createResult, err := callManifestEndpoint(ctx, manifest, account, credential, endpointKey, endpoint, input)
	if err != nil {
		return createResult, err
	}
	pollEndpointKey := strings.TrimSpace(req.PollEndpointKey)
	if pollEndpointKey == "" {
		pollEndpointKey = strings.TrimSpace(endpoint.PollEndpointKey)
	}
	if pollEndpointKey == "" {
		pollEndpointKey = endpointKey + "_poll"
	}
	pollEndpoint, ok := manifest.Endpoints[pollEndpointKey]
	if !ok {
		pollEndpoint, ok = manifest.Endpoints["poll"]
	}
	if !ok || endpointType(pollEndpoint.EndpointType) != "async_poll" {
		return createResult, fmt.Errorf("%w: async poll endpoint was not found", ErrValidation)
	}

	maxPolls := firstPositive(req.MaxPolls, endpoint.MaxPolls, pollEndpoint.MaxPolls, 5)
	interval := time.Duration(firstPositive(endpoint.PollIntervalMS, pollEndpoint.PollIntervalMS, 100)) * time.Millisecond
	pollInput := mergeJSONObjects(input, createResult.NormalizedOutput)
	totalLatency := createResult.LatencyMS
	var lastResult manifestRunResult
	for attempt := 0; attempt < maxPolls; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(interval):
			}
		}
		pollResult, err := callManifestEndpoint(ctx, manifest, account, credential, pollEndpointKey, pollEndpoint, pollInput)
		totalLatency += pollResult.LatencyMS
		pollResult.LatencyMS = totalLatency
		lastResult = pollResult
		if err != nil {
			return pollResult, err
		}
		status := mappedStatus(pollResult.NormalizedOutput)
		if status == "" || status == "running" || status == "queued" || status == "pending" {
			pollInput = mergeJSONObjects(pollInput, pollResult.NormalizedOutput)
			continue
		}
		return pollResult, nil
	}
	return lastResult, &UpstreamError{Status: http.StatusRequestTimeout, Code: CodePollingTimeout, Body: `{"error":{"code":"POLLING_TIMEOUT"}}`}
}

func callManifestEndpoint(ctx context.Context, manifest ProviderManifest, account Account, credential map[string]any, endpointKey string, endpoint ManifestEndpoint, input json.RawMessage) (manifestRunResult, error) {
	return callManifestEndpointWithContext(ctx, manifest, account, credential, endpointKey, endpoint, input, manifestCallContext{})
}

func callManifestEndpointWithContext(ctx context.Context, manifest ProviderManifest, account Account, credential map[string]any, endpointKey string, endpoint ManifestEndpoint, input json.RawMessage, extra manifestCallContext) (manifestRunResult, error) {
	baseURL := strings.TrimRight(manifest.BaseURL, "/")
	if account.BaseURL != nil && strings.TrimSpace(*account.BaseURL) != "" {
		baseURL = strings.TrimRight(*account.BaseURL, "/")
	}
	var inputValue any
	if err := json.Unmarshal(input, &inputValue); err != nil {
		return manifestRunResult{}, err
	}
	contextValue := map[string]any{
		"input":      inputValue,
		"references": extra.References,
		"credential": credential,
		"endpoint":   map[string]any{"key": endpointKey},
		"model":      extra.Model,
		"account":    extra.Account,
		"task":       extra.Task,
	}
	path, err := renderTemplateString(endpoint.PathTemplate, contextValue)
	if err != nil {
		return manifestRunResult{}, err
	}
	requestURL := baseURL + "/" + strings.TrimLeft(path, "/")
	headers, err := renderTemplateObject(endpoint.HeadersTemplate, contextValue)
	if err != nil {
		return manifestRunResult{}, err
	}
	requestBody, err := renderTemplateJSON(endpoint.RequestTemplate, contextValue)
	if err != nil {
		return manifestRunResult{}, err
	}

	var bodyReader io.Reader
	requestSnapshot := mustJSON(map[string]any{"method": endpoint.Method, "url": requestURL})
	if len(requestBody) > 0 && string(requestBody) != "null" && strings.ToUpper(endpoint.Method) != http.MethodGet {
		bodyReader = bytes.NewReader(requestBody)
		requestSnapshot = requestBody
	}
	timeout := time.Duration(firstPositive(endpoint.TimeoutMS, 120000)) * time.Millisecond
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, strings.ToUpper(endpoint.Method), requestURL, bodyReader)
	if err != nil {
		return manifestRunResult{}, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	applyManifestAuth(req, manifest.Auth, credential)

	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot}, err
	}
	if resp.StatusCode >= 400 {
		return manifestRunResult{
			LatencyMS:        latencyMS,
			RequestSnapshot:  requestSnapshot,
			ResponseSnapshot: body,
		}, upstreamError(resp.StatusCode, body)
	}
	normalizedOutput, err := mapResponse(body, endpoint.ResponseMapping)
	if err != nil {
		return manifestRunResult{
			LatencyMS:        latencyMS,
			RequestSnapshot:  requestSnapshot,
			ResponseSnapshot: body,
		}, err
	}
	status := mappedStatus(normalizedOutput)
	if status == "" {
		status = "succeeded"
	}
	return manifestRunResult{
		Status:           status,
		LatencyMS:        latencyMS,
		RequestSnapshot:  requestSnapshot,
		ResponseSnapshot: body,
		NormalizedOutput: normalizedOutput,
	}, nil
}

func endpointType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "sync"
	}
	return value
}

func applyManifestAuth(req *http.Request, auth ManifestAuth, credential map[string]any) {
	authType := strings.ToLower(strings.TrimSpace(auth.Type))
	if authType == "" || authType == "none" {
		return
	}
	header := strings.TrimSpace(auth.Header)
	if header == "" {
		header = "Authorization"
	}
	valueTemplate := strings.TrimSpace(auth.ValueTemplate)
	if valueTemplate == "" {
		switch authType {
		case "bearer", "api_key":
			valueTemplate = "Bearer {{credential.apiKey}}"
		case "basic":
			valueTemplate = "Basic {{credential.basicToken}}"
		}
	}
	value, err := renderTemplateString(valueTemplate, map[string]any{"credential": credential})
	if err != nil || strings.TrimSpace(value) == "" {
		return
	}
	req.Header.Set(header, value)
}

func renderTemplateObject(raw json.RawMessage, contextValue map[string]any) (map[string]string, error) {
	out := map[string]string{}
	if len(raw) == 0 {
		return out, nil
	}
	rendered, err := renderTemplateJSON(raw, contextValue)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal(rendered, &generic); err != nil {
		return nil, err
	}
	for key, value := range generic {
		out[key] = fmt.Sprintf("%v", value)
	}
	return out, nil
}

func renderTemplateJSON(raw json.RawMessage, contextValue map[string]any) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	rendered, err := renderTemplateValue(value, contextValue)
	if err != nil {
		return nil, err
	}
	output, err := json.Marshal(rendered)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func renderTemplateValue(value any, contextValue map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			rendered, err := renderTemplateValue(item, contextValue)
			if err != nil {
				return nil, err
			}
			out[key] = rendered
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			rendered, err := renderTemplateValue(item, contextValue)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered)
		}
		return out, nil
	case string:
		return renderTemplateScalar(typed, contextValue)
	default:
		return value, nil
	}
}

func renderTemplateScalar(value string, contextValue map[string]any) (any, error) {
	matches := templatePattern.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return value, nil
	}
	if len(matches) == 1 && strings.TrimSpace(value) == matches[0][0] {
		return lookupTemplatePath(contextValue, matches[0][1])
	}
	out := templatePattern.ReplaceAllStringFunc(value, func(match string) string {
		submatch := templatePattern.FindStringSubmatch(match)
		if len(submatch) != 2 {
			return match
		}
		resolved, err := lookupTemplatePath(contextValue, submatch[1])
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%v", resolved)
	})
	return out, nil
}

func renderTemplateString(value string, contextValue map[string]any) (string, error) {
	rendered, err := renderTemplateScalar(value, contextValue)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", rendered), nil
}

func lookupTemplatePath(contextValue map[string]any, path string) (any, error) {
	parts, err := parseTemplatePath(path)
	if err != nil {
		return nil, err
	}
	var current any = contextValue
	for _, part := range parts {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[part]
			if !ok {
				return nil, fmt.Errorf("%w: template path %s was not found", ErrValidation, path)
			}
			current = value
		case []map[string]any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("%w: template path %s index is invalid", ErrValidation, path)
			}
			current = typed[index]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("%w: template path %s index is invalid", ErrValidation, path)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("%w: template path %s is invalid", ErrValidation, path)
		}
	}
	return current, nil
}

func parseTemplatePath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: template path is empty", ErrValidation)
	}
	tokens := make([]string, 0, 4)
	for i := 0; i < len(path); {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("%w: template path %s bracket is not closed", ErrValidation, path)
			}
			token := strings.Trim(path[i+1:i+end], ` "'`)
			if token == "" {
				return nil, fmt.Errorf("%w: template path %s bracket is empty", ErrValidation, path)
			}
			tokens = append(tokens, token)
			i += end + 1
		default:
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			token := strings.TrimSpace(path[start:i])
			if token == "" {
				return nil, fmt.Errorf("%w: template path %s segment is empty", ErrValidation, path)
			}
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func mapResponse(body []byte, mappingRaw json.RawMessage) (json.RawMessage, error) {
	var responseValue any
	if err := json.Unmarshal(body, &responseValue); err != nil {
		return nil, fmt.Errorf("%w: response body is invalid JSON", ErrValidation)
	}
	if len(mappingRaw) == 0 {
		return json.Marshal(responseValue)
	}
	var mapping map[string]string
	if err := json.Unmarshal(mappingRaw, &mapping); err != nil {
		return nil, fmt.Errorf("%w: responseMapping must be an object", ErrValidation)
	}
	out := make(map[string]any, len(mapping))
	for key, path := range mapping {
		value, err := evalJSONPath(responseValue, path)
		if err != nil {
			if strings.Contains(err.Error(), "was not found") {
				out[key] = nil
				continue
			}
			return nil, err
		}
		out[key] = value
	}
	return json.Marshal(out)
}

func evalJSONPath(value any, path string) (any, error) {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}
	current := value
	for _, token := range tokens {
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[token]
			if !ok {
				return nil, fmt.Errorf("%w: JSONPath %s was not found", ErrValidation, path)
			}
			current = value
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("%w: JSONPath %s index is invalid", ErrValidation, path)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("%w: JSONPath %s cannot traverse scalar", ErrValidation, path)
		}
	}
	return current, nil
}

func parseJSONPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "$" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "$.") && !strings.HasPrefix(path, "$[") {
		return nil, fmt.Errorf("%w: JSONPath must start with $", ErrValidation)
	}
	var tokens []string
	for i := 1; i < len(path); {
		switch path[i] {
		case '.':
			i++
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			if start == i {
				return nil, fmt.Errorf("%w: JSONPath segment is empty", ErrValidation)
			}
			tokens = append(tokens, path[start:i])
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("%w: JSONPath bracket is not closed", ErrValidation)
			}
			content := strings.Trim(path[i+1:i+end], `"' `)
			if content == "" {
				return nil, fmt.Errorf("%w: JSONPath bracket is empty", ErrValidation)
			}
			tokens = append(tokens, content)
			i += end + 1
		default:
			return nil, fmt.Errorf("%w: JSONPath syntax is invalid", ErrValidation)
		}
	}
	return tokens, nil
}

func parseYAMLOrJSONText(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("%w: manifestText is empty", ErrValidation)
	}
	if json.Valid([]byte(text)) {
		return []byte(text), nil
	}
	var decoded any
	if err := yaml.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("%w: manifestText must be valid YAML or JSON", ErrValidation)
	}
	normalized := normalizeYAMLValue(decoded)
	output, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func normalizeYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeYAMLValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprintf("%v", key)] = normalizeYAMLValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeYAMLValue(item))
		}
		return out
	default:
		return value
	}
}

func mappedStatus(raw json.RawMessage) string {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	if status, ok := value["status"].(string); ok {
		return strings.ToLower(status)
	}
	return ""
}

func mergeJSONObjects(values ...json.RawMessage) json.RawMessage {
	out := map[string]any{}
	for _, raw := range values {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		for key, item := range value {
			out[key] = item
		}
	}
	return mustJSON(out)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
