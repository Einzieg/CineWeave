package controlmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandlerAuthenticatesBeforeMCPAndEnforcesHTTPBoundaries(t *testing.T) {
	handler := mustTestHandler(t, &fakeExecutor{descriptors: testDescriptors()})

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverRequest()))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("missing key response = %d, authenticate=%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}

	request = authorizedRequest(discoverRequest())
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusForbidden, "ORIGIN_NOT_ALLOWED")

	request = authorizedRequest(strings.Repeat("x", 2049))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE")

	request = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}

func TestHandlerSupportsDiscoverLegacyInitializeAndDeterministicTools(t *testing.T) {
	handler := mustTestHandler(t, &fakeExecutor{descriptors: testDescriptors()})

	discovered := callMCP(t, handler, discoverRequest())
	result := resultObject(t, discovered)
	versions := anySlice(t, result["supportedVersions"])
	if len(versions) == 0 || versions[0] != "2026-07-28" || !containsAnyString(versions, "2025-11-25") || !containsAnyString(versions, "2025-06-18") {
		t.Fatalf("supportedVersions = %#v", versions)
	}

	for index, protocolVersion := range []string{"2025-11-25", "2025-06-18"} {
		legacy := callMCP(t, handler, fmt.Sprintf(`{
      "jsonrpc":"2.0","id":%d,"method":"initialize",
      "params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"test","version":"1"}}
    }`, 2+index, protocolVersion))
		legacyResult := resultObject(t, legacy)
		if legacyResult["protocolVersion"] != protocolVersion {
			t.Fatalf("legacy protocolVersion = %#v, want %s", legacyResult["protocolVersion"], protocolVersion)
		}
	}

	listed := callMCP(t, handler, rpcRequest(3, "tools/list", map[string]any{}))
	tools := anySlice(t, resultObject(t, listed)["tools"])
	if len(tools) != 2 {
		t.Fatalf("tools count = %d", len(tools))
	}
	first := anyObject(t, tools[0])
	second := anyObject(t, tools[1])
	if first["name"] != "project_get" || second["name"] != "project_update" {
		t.Fatalf("tool order = %v, %v", first["name"], second["name"])
	}
	annotations := anyObject(t, second["annotations"])
	if annotations["readOnlyHint"] != false || annotations["idempotentHint"] != true {
		t.Fatalf("write annotations = %#v", annotations)
	}
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestHandlerReturnsCompleteToolCatalogInFirstPageForCodex(t *testing.T) {
	descriptors := make([]projectcontrol.Descriptor, 0, 178)
	for index := 0; index < 178; index++ {
		descriptors = append(descriptors, testDescriptor(fmt.Sprintf("catalog.tool_%03d", index), true))
	}
	handler := mustTestHandler(t, &fakeExecutor{descriptors: descriptors})

	listed := callMCP(t, handler, rpcRequest(31, "tools/list", map[string]any{}))
	result := resultObject(t, listed)
	tools := anySlice(t, result["tools"])
	if len(tools) != len(descriptors) {
		t.Fatalf("first tools/list page count = %d, want %d", len(tools), len(descriptors))
	}
	if cursor, ok := result["nextCursor"].(string); ok && cursor != "" {
		t.Fatalf("first tools/list page unexpectedly returned nextCursor %q", cursor)
	}
}

func TestHandlerReturnsStructuredToolResultsAndBusinessErrors(t *testing.T) {
	executor := &fakeExecutor{descriptors: testDescriptors()}
	handler := mustTestHandler(t, executor)

	response := callMCP(t, handler, rpcRequest(4, "tools/call", map[string]any{
		"name":      "project_get",
		"arguments": map[string]any{"projectId": "p1"},
	}))
	result := resultObject(t, response)
	if result["isError"] == true {
		t.Fatalf("successful call unexpectedly failed: %#v", result)
	}
	structured := anyObject(t, result["structuredContent"])
	if structured["schemaVersion"] != projectcontrol.SchemaVersionV1 || structured["status"] != "succeeded" {
		t.Fatalf("structured result = %#v", structured)
	}
	if executor.lastIdentity.Principal.UserID != "user-1" || executor.lastIdentity.Key.ID != "key-1" {
		t.Fatalf("identity = %#v", executor.lastIdentity)
	}

	executor.failBusiness = true
	response = callMCP(t, handler, rpcRequest(5, "tools/call", map[string]any{
		"name":      "project_get",
		"arguments": map[string]any{"projectId": "missing"},
	}))
	result = resultObject(t, response)
	if result["isError"] != true {
		t.Fatalf("business error did not set isError: %#v", result)
	}
	structured = anyObject(t, result["structuredContent"])
	errorValue := anyObject(t, structured["error"])
	if errorValue["code"] != "PROJECT_NOT_FOUND" {
		t.Fatalf("business error = %#v", errorValue)
	}
}

func TestHandlerSeparatesAuthenticationAvailabilityAndPermissionErrors(t *testing.T) {
	executor := &fakeExecutor{descriptors: testDescriptors(), businessCode: "PERMISSION_DENIED"}
	handler := mustTestHandler(t, executor)

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverRequest()))
	request.Header.Set("Authorization", "Bearer invalid-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusUnauthorized, "CODEX_CONTROL_KEY_INVALID")

	unavailable, err := NewHandler(failingAuthenticator{err: errors.New("database unavailable")}, executor, Options{
		MaxRequestBodyBytes: 2048, MaxResultBytes: 4096,
		DisableCommandLimit: true, DisableRequestRateLimit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request = authorizedRequest(discoverRequest())
	response = httptest.NewRecorder()
	unavailable.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")

	result := resultObject(t, callMCP(t, handler, rpcRequest(29, "tools/call", map[string]any{
		"name": "project_get", "arguments": map[string]any{"projectId": "forbidden"},
	})))
	if result["isError"] != true {
		t.Fatalf("permission failure did not use an MCP tool result: %#v", result)
	}
	structured := anyObject(t, result["structuredContent"])
	permissionError := anyObject(t, structured["error"])
	if permissionError["code"] != "PERMISSION_DENIED" {
		t.Fatalf("permission error = %#v", permissionError)
	}
}

func TestHandlerDiagnosticsAggregateRecentAuthenticationFailuresWithoutSecrets(t *testing.T) {
	handler := mustTestHandler(t, &fakeExecutor{descriptors: testDescriptors()})

	missing := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverRequest()))
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)

	invalid := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(discoverRequest()))
	invalid.Header.Set("Authorization", "Bearer invalid-key-must-not-leak")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)

	diagnostics := handler.Diagnostics(time.Now())
	if !diagnostics.Enabled || len(diagnostics.ToolCatalogHash) != 64 {
		t.Fatalf("diagnostics identity = %+v", diagnostics)
	}
	if len(diagnostics.RecentAuthenticationFailures) != 2 {
		t.Fatalf("authentication failures = %+v", diagnostics.RecentAuthenticationFailures)
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "invalid-key-must-not-leak") {
		t.Fatalf("diagnostics leaked the bearer token: %s", encoded)
	}
}

func TestHandlerWorksWithOfficialSDKClient(t *testing.T) {
	executor := &fakeExecutor{descriptors: testDescriptors()}
	handler := mustTestHandler(t, executor)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "cineweave-test", Version: "1"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{},
	})
	transport := &mcp.StreamableClientTransport{
		Endpoint: server.URL,
		HTTPClient: &http.Client{Transport: authorizationRoundTripper{
			base: http.DefaultTransport, token: "valid-key",
		}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatalf("connect official MCP client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	listed, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools with official MCP client: %v", err)
	}
	if len(listed.Tools) != 2 || listed.Tools[0].Name != "project_get" || listed.Tools[1].Name != "project_update" {
		t.Fatalf("official client tool list = %#v", listed.Tools)
	}
	called, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "project_get", Arguments: map[string]any{"projectId": "project-1"},
	})
	if err != nil || called.IsError {
		t.Fatalf("call tool with official MCP client: isError=%t err=%v", called != nil && called.IsError, err)
	}
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{
		URI: "cineweave://projects/project-1/content/project_source/source-1?maxBytes=1024",
	})
	if err != nil || len(resource.Contents) != 1 {
		t.Fatalf("read resource with official MCP client: contents=%d err=%v", len(resource.Contents), err)
	}
}

func TestHandlerExposesAuthenticatedChunkedContentResource(t *testing.T) {
	executor := &fakeExecutor{descriptors: testDescriptors()}
	handler := mustTestHandler(t, executor)

	listed := callMCP(t, handler, rpcRequest(31, "resources/templates/list", map[string]any{}))
	templates := anySlice(t, resultObject(t, listed)["resourceTemplates"])
	if len(templates) != 1 {
		t.Fatalf("resource templates=%d", len(templates))
	}
	template := anyObject(t, templates[0])
	if template["uriTemplate"] != "cineweave://projects/{projectId}/content/{targetType}/{targetId}{?cursor,maxBytes,contentHash}" {
		t.Fatalf("resource template=%#v", template)
	}

	resourceURI := "cineweave://projects/project-1/content/project_source/source-1?maxBytes=1024"
	read := callMCP(t, handler, rpcRequest(32, "resources/read", map[string]any{"uri": resourceURI}))
	contents := anySlice(t, resultObject(t, read)["contents"])
	if len(contents) != 1 {
		t.Fatalf("resource contents=%d", len(contents))
	}
	content := anyObject(t, contents[0])
	if content["uri"] != resourceURI || content["mimeType"] != "application/json" {
		t.Fatalf("resource content=%#v", content)
	}
	if executor.lastIdentity.Principal.UserID != "user-1" {
		t.Fatalf("resource identity=%#v", executor.lastIdentity)
	}
	if executor.executeCount != 1 {
		t.Fatalf("resource execute count=%d", executor.executeCount)
	}
}

func TestHandlerEnforcesRateAndActiveCommandLimitsBeforeToolExecution(t *testing.T) {
	executor := &fakeExecutor{descriptors: testDescriptors(), activeCommands: 20}
	handler, err := NewHandler(fakeAuthenticator{}, executor, Options{
		AllowedOrigins:      []string{"https://cineweave.einzieg.site"},
		MaxRequestBodyBytes: 2048,
		MaxResultBytes:      4096,
		RequestsPerMinute:   100,
		ConcurrentRequests:  4,
		ActiveCommandLimit:  20,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := authorizedRequest(rpcRequest(6, "tools/call", map[string]any{
		"name": "project_update", "arguments": map[string]any{"projectId": "p1", "idempotencyKey": "k1"},
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusTooManyRequests, "COMMAND_CONCURRENCY_LIMIT")
	if executor.executeCount != 0 {
		t.Fatalf("write action executed %d times despite concurrency gate", executor.executeCount)
	}

	limited, err := NewHandler(fakeAuthenticator{}, &fakeExecutor{descriptors: testDescriptors()}, Options{
		AllowedOrigins:      []string{"https://cineweave.einzieg.site"},
		MaxRequestBodyBytes: 2048,
		MaxResultBytes:      4096,
		RequestsPerMinute:   1,
		ConcurrentRequests:  4,
		DisableCommandLimit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	callMCP(t, limited, discoverRequest())
	request = authorizedRequest(discoverRequest())
	response = httptest.NewRecorder()
	limited.ServeHTTP(response, request)
	assertHTTPErrorCode(t, response, http.StatusTooManyRequests, "RATE_LIMITED")
}

type fakeAuthenticator struct{}

func (fakeAuthenticator) AuthenticateControlKey(_ context.Context, token string) (auth.Principal, auth.ControlKeyMetadata, error) {
	if token != "valid-key" {
		return auth.Principal{}, auth.ControlKeyMetadata{}, auth.ErrControlKeyInvalid
	}
	return auth.Principal{UserID: "user-1", CredentialVersion: 1}, auth.ControlKeyMetadata{ID: "key-1", Status: "active"}, nil
}

type failingAuthenticator struct{ err error }

func (f failingAuthenticator) AuthenticateControlKey(context.Context, string) (auth.Principal, auth.ControlKeyMetadata, error) {
	return auth.Principal{}, auth.ControlKeyMetadata{}, f.err
}

type authorizationRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (r authorizationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+r.token)
	return r.base.RoundTrip(copy)
}

type fakeExecutor struct {
	mu             sync.Mutex
	descriptors    []projectcontrol.Descriptor
	activeCommands int
	failBusiness   bool
	businessCode   string
	executeCount   int
	lastIdentity   Identity
}

func (f *fakeExecutor) Descriptors() []projectcontrol.Descriptor {
	return append([]projectcontrol.Descriptor(nil), f.descriptors...)
}

func (f *fakeExecutor) ActiveCommandCount(context.Context, Identity) (int, error) {
	return f.activeCommands, nil
}

func (f *fakeExecutor) Execute(_ context.Context, identity Identity, name string, input json.RawMessage) (projectcontrol.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executeCount++
	f.lastIdentity = identity
	if f.failBusiness || f.businessCode != "" {
		result := projectcontrol.NewResult("failed", "项目不存在")
		code := f.businessCode
		if code == "" {
			code = "PROJECT_NOT_FOUND"
		}
		result.Error = &projectcontrol.Error{Code: code, UserMessage: "项目不存在"}
		return result, nil
	}
	if name == "explode" {
		return projectcontrol.Result{}, errors.New("boom")
	}
	result := projectcontrol.NewResult("succeeded", "已读取项目")
	result.Data = append(json.RawMessage(nil), input...)
	return result, nil
}

func testDescriptors() []projectcontrol.Descriptor {
	return []projectcontrol.Descriptor{
		testDescriptor("project.update", false),
		testDescriptor("project.get", true),
	}
}

func testDescriptor(name string, readOnly bool) projectcontrol.Descriptor {
	effects := projectcontrol.Effects{}
	risk := projectcontrol.RiskRead
	executionMode := projectcontrol.ExecutionModeSync
	if !readOnly {
		effects.WritesProject = true
		risk = projectcontrol.RiskWrite
		executionMode = projectcontrol.ExecutionModeAsyncCommand
	}
	return projectcontrol.Descriptor{
		Name: name, Version: 1, Label: name, Summary: name, Description: name,
		Risk: risk, Scope: projectcontrol.ScopeProject,
		Permissions: []string{"project.read"}, ProjectKinds: []string{"narrative", "commerce_video"},
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":true}`),
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":true}`),
		Effects:      effects, ReadOnly: readOnly, Destructive: false, Idempotent: true,
		ExecutionMode:      executionMode,
		ActivityVisibility: projectcontrol.ActivityVisibilityAuditOnly,
		ExportToMCP:        true,
	}
}

func mustTestHandler(t *testing.T, executor Executor) *Handler {
	t.Helper()
	handler, err := NewHandler(fakeAuthenticator{}, executor, Options{
		AllowedOrigins:          []string{"https://cineweave.einzieg.site"},
		MaxRequestBodyBytes:     2048,
		MaxResultBytes:          4096,
		DisableCommandLimit:     true,
		DisableRequestRateLimit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func authorizedRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer valid-key")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	var wire struct {
		Method string `json:"method"`
		Params struct {
			Name            string `json:"name"`
			URI             string `json:"uri"`
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(body), &wire) == nil {
		protocolVersion := "2026-07-28"
		if wire.Method == "initialize" && wire.Params.ProtocolVersion != "" {
			protocolVersion = wire.Params.ProtocolVersion
		}
		request.Header.Set("Mcp-Protocol-Version", protocolVersion)
		if protocolVersion >= "2026-07-28" {
			request.Header.Set("Mcp-Method", wire.Method)
			if wire.Method == "tools/call" || wire.Method == "resources/read" || wire.Method == "prompts/get" {
				name := wire.Params.Name
				if wire.Method == "resources/read" {
					name = wire.Params.URI
				}
				request.Header.Set("Mcp-Name", name)
			}
		}
	}
	return request
}

func callMCP(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	request := authorizedRequest(body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("MCP status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode MCP response: %v; body=%s", err, response.Body.String())
	}
	return envelope
}

func discoverRequest() string {
	return rpcRequest(1, "server/discover", map[string]any{})
}

func rpcRequest(id int, method string, params map[string]any) string {
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "test", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func resultObject(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	if protocolError, ok := response["error"]; ok {
		t.Fatalf("MCP protocol error: %#v", protocolError)
	}
	return anyObject(t, response["result"])
}

func anyObject(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return result
}

func anySlice(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return result
}

func assertHTTPErrorCode(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, status, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != code {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, code)
	}
}
