package controlmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxRequestBodyBytes = int64(1 << 20)
	defaultMaxResultBytes      = 512 << 10
	defaultRequestsPerMinute   = 120
	defaultConcurrentRequests  = 8
	defaultActiveCommands      = 20
)

const serverInstructions = "先读取当前项目和真实 ID，再执行修改。分卷、章节、分集必须使用存储的 ID 和 ordinal，不得按标题猜测。workflow start 成功不等于工作流完成，应使用 control.command.get 或 control.command.wait 等待真实终态。所有 AI 调用只能经过 Provider Gateway。对象存在歧义时返回候选并向用户询问。"

var resultSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["schemaVersion","status","summary","workflowRunIds","retryable","nextActions"],
  "properties":{
    "schemaVersion":{"type":"string","const":"project-control.v1"},
    "commandId":{"type":"string"},
    "status":{"type":"string"},
    "summary":{"type":"string"},
    "data":{},
    "workflowRunIds":{"type":"array","items":{"type":"string"}},
    "nextCursor":{"type":"string"},
    "retryable":{"type":"boolean"},
    "error":{"type":"object"},
    "nextActions":{"type":"array","items":{"type":"object"}}
  }
}`)

type Identity struct {
	Principal      auth.Principal
	Key            auth.ControlKeyMetadata
	ControllerType projectcontrol.ControllerType
	RequestID      string
}

type Authenticator interface {
	AuthenticateControlKey(context.Context, string) (auth.Principal, auth.ControlKeyMetadata, error)
}

type Executor interface {
	Descriptors() []projectcontrol.Descriptor
	Execute(context.Context, Identity, string, json.RawMessage) (projectcontrol.Result, error)
	ActiveCommandCount(context.Context, Identity) (int, error)
}

type Options struct {
	Version                 string
	AllowedOrigins          []string
	MaxRequestBodyBytes     int64
	MaxResultBytes          int
	RequestsPerMinute       int
	ConcurrentRequests      int
	ActiveCommandLimit      int
	DisableCommandLimit     bool
	DisableRequestRateLimit bool
}

type Handler struct {
	authenticator   Authenticator
	executor        Executor
	descriptors     map[string]projectcontrol.Descriptor
	allowedOrigin   map[string]struct{}
	maxBodyBytes    int64
	maxResult       int
	activeLimit     int
	limiter         *requestLimiter
	authFailures    *authenticationFailureWindow
	toolCatalogHash string
	mcp             http.Handler
}

type AuthenticationFailureAggregate struct {
	Reason         string    `json:"reason"`
	Count          int64     `json:"count"`
	FirstFailureAt time.Time `json:"firstFailureAt"`
	LastFailureAt  time.Time `json:"lastFailureAt"`
}

type Diagnostics struct {
	Enabled                      bool                             `json:"enabled"`
	ToolCatalogHash              string                           `json:"toolCatalogHash"`
	RecentAuthenticationFailures []AuthenticationFailureAggregate `json:"recentAuthenticationFailures"`
}

func NewHandler(authenticator Authenticator, executor Executor, options Options) (*Handler, error) {
	if authenticator == nil {
		return nil, fmt.Errorf("control MCP authenticator is required")
	}
	if executor == nil {
		return nil, fmt.Errorf("control MCP executor is required")
	}
	maxBodyBytes := options.MaxRequestBodyBytes
	if maxBodyBytes == 0 {
		maxBodyBytes = defaultMaxRequestBodyBytes
	}
	if maxBodyBytes < 1 {
		return nil, fmt.Errorf("control MCP request body limit must be positive")
	}
	maxResult := options.MaxResultBytes
	if maxResult == 0 {
		maxResult = defaultMaxResultBytes
	}
	if maxResult < 1024 {
		return nil, fmt.Errorf("control MCP result limit must be at least 1024 bytes")
	}
	requestsPerMinute := options.RequestsPerMinute
	if requestsPerMinute == 0 {
		requestsPerMinute = defaultRequestsPerMinute
	}
	concurrentRequests := options.ConcurrentRequests
	if concurrentRequests == 0 {
		concurrentRequests = defaultConcurrentRequests
	}
	activeLimit := options.ActiveCommandLimit
	if activeLimit == 0 {
		activeLimit = defaultActiveCommands
	}
	if options.DisableCommandLimit {
		activeLimit = 0
	}
	if options.DisableRequestRateLimit {
		requestsPerMinute = 0
		concurrentRequests = 0
	}

	h := &Handler{
		authenticator: authenticator,
		executor:      executor,
		descriptors:   make(map[string]projectcontrol.Descriptor),
		allowedOrigin: make(map[string]struct{}),
		maxBodyBytes:  maxBodyBytes,
		maxResult:     maxResult,
		activeLimit:   activeLimit,
		limiter:       newRequestLimiter(requestsPerMinute, concurrentRequests),
		authFailures:  newAuthenticationFailureWindow(15 * time.Minute),
	}
	for _, origin := range options.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			h.allowedOrigin[origin] = struct{}{}
		}
	}

	version := strings.TrimSpace(options.Version)
	if version == "" {
		version = "development"
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:        "cineweave-project-control",
		Title:       "CineWeave 项目控制",
		Description: "通过 CineWeave 持久命令运行时读取和控制项目。",
		Version:     version,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		// Codex 0.146 consumes only the first tools/list page. The catalog builder
		// enforces this bound so every exported tool remains discoverable.
		PageSize:     projectcontrol.MaxMCPToolCatalogSize,
		Capabilities: &mcp.ServerCapabilities{},
	})

	descriptors := executor.Descriptors()
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].Version < descriptors[j].Version
		}
		return descriptors[i].Name < descriptors[j].Name
	})
	catalog, err := projectcontrol.BuildToolCatalog(descriptors)
	if err != nil {
		return nil, fmt.Errorf("build MCP tool catalog: %w", err)
	}
	h.toolCatalogHash = catalog.CatalogHash
	for _, descriptor := range descriptors {
		if !descriptor.ExportToMCP {
			continue
		}
		if err := descriptor.Validate(); err != nil {
			return nil, err
		}
		toolName, err := projectcontrol.MCPToolName(descriptor.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := h.descriptors[toolName]; exists {
			return nil, fmt.Errorf("duplicate MCP wire tool %s", toolName)
		}
		h.descriptors[toolName] = descriptor.Clone()
		descriptor := descriptor.Clone()
		server.AddTool(toolFromDescriptor(toolName, descriptor), func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return h.callTool(ctx, descriptor, request)
		})
	}
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "cineweave_project_content",
		Title:       "CineWeave 项目长文本分块",
		Description: "按项目和稳定对象 ID 读取原文、章节、剧本分集或带货脚本；通过 cursor 继续读取后续分块。",
		MIMEType:    "application/json",
		URITemplate: "cineweave://projects/{projectId}/content/{targetType}/{targetId}{?cursor,maxBytes,contentHash}",
	}, h.readContentResource)

	h.mcp = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          maxBodyBytes,
		PropagateRequestCancellation: true,
	})
	return h, nil
}

func (h *Handler) readContentResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	startedAt := time.Now()
	resultCode := "succeeded"
	defer func() {
		identity, _ := identityFromContext(ctx)
		observability.RecordMCPTool("content.read", string(identity.ControllerType), resultCode, time.Since(startedAt))
	}()
	identity, ok := identityFromContext(ctx)
	if !ok {
		resultCode = "identity_missing"
		return nil, fmt.Errorf("authenticated MCP identity is missing")
	}
	if request == nil || request.Params == nil {
		resultCode = "request_invalid"
		return nil, fmt.Errorf("resource request is missing")
	}
	resourceURI, err := url.Parse(request.Params.URI)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(resourceURI.Path, "/"), "/")
	if resourceURI.Scheme != "cineweave" || resourceURI.Host != "projects" ||
		len(parts) != 4 || parts[1] != "content" {
		return nil, fmt.Errorf("unsupported CineWeave resource URI")
	}
	arguments := map[string]any{
		"projectId": parts[0], "targetType": parts[2], "targetId": parts[3],
	}
	query := resourceURI.Query()
	if cursor := strings.TrimSpace(query.Get("cursor")); cursor != "" {
		arguments["cursor"] = cursor
	}
	if contentHash := strings.TrimSpace(query.Get("contentHash")); contentHash != "" {
		arguments["contentHash"] = contentHash
	}
	if value := strings.TrimSpace(query.Get("maxBytes")); value != "" {
		maxBytes, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return nil, fmt.Errorf("maxBytes is invalid")
		}
		arguments["maxBytes"] = maxBytes
	}
	result, err := h.executor.Execute(ctx, identity, "content.read", mustJSON(arguments))
	if err != nil {
		resultCode = "internal_error"
		return nil, err
	}
	if result.Error != nil {
		resultCode = result.Error.Code
		return nil, fmt.Errorf("%s: %s", result.Error.Code, result.Error.UserMessage)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > h.maxResult {
		resultCode = "result_too_large"
		return nil, fmt.Errorf("resource result exceeds %d bytes", h.maxResult)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: request.Params.URI, MIMEType: "application/json", Text: string(encoded),
	}}}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	operation := "http"
	recorder := &mcpResponseRecorder{ResponseWriter: w, status: http.StatusOK}
	w = recorder
	defer func() {
		observability.RecordMCPRequest(operation, mcpHTTPResult(recorder.status), time.Since(startedAt))
	}()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.writeHTTPError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "MCP 端点只接受 POST 请求", 0)
		return
	}
	if !h.originAllowed(r.Header.Get("Origin")) {
		h.writeHTTPError(w, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源不允许访问 MCP 端点", 0)
		return
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		h.recordAuthenticationFailure("missing_bearer_token", time.Now())
		observability.RecordMCPAuthentication("missing_bearer_token")
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeHTTPError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要有效的 Codex 用户安全密钥", 0)
		return
	}
	principal, key, err := h.authenticator.AuthenticateControlKey(r.Context(), token)
	if err != nil {
		if errors.Is(err, auth.ErrControlKeyInvalid) || errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, auth.ErrForbidden) {
			h.recordAuthenticationFailure("invalid_control_key", time.Now())
			observability.RecordMCPAuthentication("invalid_control_key")
			w.Header().Set("WWW-Authenticate", "Bearer")
			h.writeHTTPError(w, r, http.StatusUnauthorized, "CODEX_CONTROL_KEY_INVALID", "Codex 用户安全密钥无效、已撤销或需要轮换", 0)
			return
		}
		h.recordAuthenticationFailure("authentication_unavailable", time.Now())
		observability.RecordMCPAuthentication("authentication_unavailable")
		h.writeHTTPError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "CineWeave 暂时无法验证用户安全密钥", 1)
		return
	}
	observability.RecordMCPAuthentication("succeeded")
	identity := Identity{
		Principal: principal, Key: key, ControllerType: projectcontrol.ControllerCodexMCP,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	}
	release, retryAfter := h.limiter.acquire(key.ID, time.Now())
	if release == nil {
		h.writeHTTPError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后重试", retryAfter)
		return
	}
	defer release()

	body, err := readBoundedBody(r.Body, h.maxBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			h.writeHTTPError(w, r, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "MCP 请求体超过 1 MiB 限制", 0)
			return
		}
		h.writeHTTPError(w, r, http.StatusBadRequest, "REQUEST_BODY_INVALID", "无法读取 MCP 请求体", 0)
		return
	}
	operation = mcpOperation(body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if h.activeLimit > 0 && h.isCommandCreatingCall(body) {
		active, err := h.executor.ActiveCommandCount(r.Context(), identity)
		if err != nil {
			h.writeHTTPError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "暂时无法检查活动命令数量", 1)
			return
		}
		if active >= h.activeLimit {
			h.writeHTTPError(w, r, http.StatusTooManyRequests, "COMMAND_CONCURRENCY_LIMIT", "活动命令已达到并发上限，请等待已有任务完成", 5)
			return
		}
	}
	r = r.WithContext(withIdentity(r.Context(), identity))
	h.mcp.ServeHTTP(w, r)
}

func (h *Handler) callTool(ctx context.Context, descriptor projectcontrol.Descriptor, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startedAt := time.Now()
	resultCode := "succeeded"
	identity, ok := identityFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("authenticated MCP identity is missing")
	}
	defer func() {
		observability.RecordMCPTool(
			descriptor.Name, string(identity.ControllerType), resultCode, time.Since(startedAt),
		)
	}()
	arguments := json.RawMessage(`{}`)
	if request != nil && request.Params != nil && len(request.Params.Arguments) > 0 {
		arguments = append(json.RawMessage(nil), request.Params.Arguments...)
	}
	result, err := h.executor.Execute(ctx, identity, descriptor.Name, arguments)
	if err != nil {
		resultCode = "INTERNAL_ERROR"
		result = projectcontrol.NewResult("failed", "操作失败，请稍后重试")
		result.Retryable = true
		result.Error = &projectcontrol.Error{
			Code:        "INTERNAL_ERROR",
			UserMessage: "操作失败，请稍后重试",
			Retryable:   true,
			Details:     mustJSON(map[string]any{"requestId": identity.RequestID}),
		}
	}
	if result.SchemaVersion == "" {
		result.SchemaVersion = projectcontrol.SchemaVersionV1
	}
	if result.WorkflowRunIDs == nil {
		result.WorkflowRunIDs = []string{}
	}
	if result.NextActions == nil {
		result.NextActions = []projectcontrol.NextAction{}
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal project control result: %w", marshalErr)
	}
	if len(encoded) > h.maxResult {
		resultCode = "RESULT_TOO_LARGE"
		result = projectcontrol.NewResult("failed", "结果超过单次返回限制，请改用分页或内容分块读取")
		result.Error = &projectcontrol.Error{
			Code:        "RESULT_TOO_LARGE",
			UserMessage: "结果超过单次返回限制，请改用分页或内容分块读取",
			Retryable:   false,
			Details:     mustJSON(map[string]any{"maximumBytes": h.maxResult}),
		}
		result.NextActions = []projectcontrol.NextAction{{Label: "改用分页或 content.read"}}
		encoded, marshalErr = json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal bounded project control result: %w", marshalErr)
		}
	}
	if result.Error != nil {
		resultCode = result.Error.Code
	} else if strings.TrimSpace(result.Status) != "" {
		resultCode = result.Status
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
		StructuredContent: result,
		IsError:           result.Error != nil || result.Status == "failed" || result.Status == "blocked",
	}, nil
}

func (h *Handler) Diagnostics(now time.Time) Diagnostics {
	result := Diagnostics{Enabled: h != nil, RecentAuthenticationFailures: []AuthenticationFailureAggregate{}}
	if h == nil || h.authFailures == nil {
		return result
	}
	result.ToolCatalogHash = h.toolCatalogHash
	result.RecentAuthenticationFailures = h.authFailures.snapshot(now)
	return result
}

func (h *Handler) recordAuthenticationFailure(reason string, now time.Time) {
	if h != nil && h.authFailures != nil {
		h.authFailures.record(reason, now)
	}
}

func toolFromDescriptor(toolName string, descriptor projectcontrol.Descriptor) *mcp.Tool {
	destructive := descriptor.Destructive
	openWorld := descriptor.Costed
	return &mcp.Tool{
		Name:         toolName,
		Title:        descriptor.Label,
		Description:  descriptor.Description,
		InputSchema:  descriptor.InputSchema,
		OutputSchema: resultSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           descriptor.Label,
			ReadOnlyHint:    descriptor.ReadOnly,
			DestructiveHint: &destructive,
			IdempotentHint:  descriptor.Idempotent,
			OpenWorldHint:   &openWorld,
		},
	}
}

func (h *Handler) isCommandCreatingCall(body []byte) bool {
	var request struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &request) != nil || request.Method != "tools/call" {
		return false
	}
	descriptor, ok := h.descriptors[request.Params.Name]
	return ok && descriptor.ExecutionMode != projectcontrol.ExecutionModeSync
}

func (h *Handler) originAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	_, ok := h.allowedOrigin[origin]
	return ok
}

func (h *Handler) writeHTTPError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryAfter int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"requestId": httpx.RequestID(r),
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"retryable": status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable,
		},
	})
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

var errBodyTooLarge = errors.New("request body exceeds limit")

func readBoundedBody(body io.ReadCloser, maximum int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errBodyTooLarge
	}
	return data, nil
}

type identityContextKey struct{}

func withIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func identityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func mcpOperation(body []byte) string {
	var request struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Method) == "" {
		return "invalid_jsonrpc"
	}
	return strings.TrimSpace(request.Method)
}

func mcpHTTPResult(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "succeeded"
	case status >= 400 && status < 500:
		return "client_error"
	case status >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

type mcpResponseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *mcpResponseRecorder) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *mcpResponseRecorder) Write(payload []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *mcpResponseRecorder) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *mcpResponseRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

type authenticationFailureWindow struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]AuthenticationFailureAggregate
}

func newAuthenticationFailureWindow(window time.Duration) *authenticationFailureWindow {
	return &authenticationFailureWindow{
		window: window, entries: make(map[string]AuthenticationFailureAggregate),
	}
}

func (w *authenticationFailureWindow) record(reason string, now time.Time) {
	if w == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(now)
	entry := w.entries[reason]
	entry.Reason = reason
	entry.Count++
	if entry.FirstFailureAt.IsZero() {
		entry.FirstFailureAt = now
	}
	entry.LastFailureAt = now
	w.entries[reason] = entry
}

func (w *authenticationFailureWindow) snapshot(now time.Time) []AuthenticationFailureAggregate {
	if w == nil {
		return []AuthenticationFailureAggregate{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(now)
	result := make([]AuthenticationFailureAggregate, 0, len(w.entries))
	for _, entry := range w.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Reason < result[j].Reason
		}
		return result[i].Count > result[j].Count
	})
	return result
}

func (w *authenticationFailureWindow) pruneLocked(now time.Time) {
	cutoff := now.Add(-w.window)
	for reason, entry := range w.entries {
		if entry.LastFailureAt.Before(cutoff) {
			delete(w.entries, reason)
		}
	}
}

type requestLimiter struct {
	mu                sync.Mutex
	requestsPerMinute int
	concurrentLimit   int
	states            map[string]*requestLimitState
}

type requestLimitState struct {
	windowStart time.Time
	requests    int
	concurrent  int
	lastSeen    time.Time
}

func newRequestLimiter(requestsPerMinute, concurrentLimit int) *requestLimiter {
	return &requestLimiter{
		requestsPerMinute: requestsPerMinute,
		concurrentLimit:   concurrentLimit,
		states:            make(map[string]*requestLimitState),
	}
}

func (l *requestLimiter) acquire(keyID string, now time.Time) (func(), int) {
	if l == nil || (l.requestsPerMinute <= 0 && l.concurrentLimit <= 0) {
		return func() {}, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.states[keyID]
	if state == nil {
		state = &requestLimitState{windowStart: now}
		l.states[keyID] = state
	}
	if now.Sub(state.windowStart) >= time.Minute {
		state.windowStart = now
		state.requests = 0
	}
	if l.requestsPerMinute > 0 && state.requests >= l.requestsPerMinute {
		retryAfter := int(time.Until(state.windowStart.Add(time.Minute)).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return nil, retryAfter
	}
	if l.concurrentLimit > 0 && state.concurrent >= l.concurrentLimit {
		return nil, 1
	}
	state.requests++
	state.concurrent++
	state.lastSeen = now
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if current := l.states[keyID]; current != nil && current.concurrent > 0 {
			current.concurrent--
			current.lastSeen = time.Now()
		}
	}, 0
}
