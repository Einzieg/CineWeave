package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/provider"
)

type providerActionTarget struct {
	AccountID   string         `json:"accountId,omitempty"`
	ModelID     string         `json:"modelId,omitempty"`
	ProviderKey string         `json:"providerKey,omitempty"`
	Patch       map[string]any `json:"patch,omitempty"`
}

type providerTestModelActionInput struct {
	ModelID  string         `json:"modelId"`
	TestType string         `json:"testType,omitempty"`
	Prompt   string         `json:"prompt,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
}

type providerVideoCapabilityActionInput struct {
	ModelID                string         `json:"modelId"`
	VariantKey             string         `json:"variantKey"`
	CapabilitySnapshotHash string         `json:"capabilitySnapshotHash"`
	Decision               string         `json:"decision,omitempty"`
	Reason                 string         `json:"reason,omitempty"`
	Evidence               map[string]any `json:"evidence,omitempty"`
	VerificationMode       string         `json:"verificationMode,omitempty"`
	ProviderTestRunID      string         `json:"providerTestRunId,omitempty"`
}

func (s *Server) requireProviderControlService() error {
	if s.providers == nil {
		return apiError{Status: http.StatusServiceUnavailable, Code: "PROVIDER_SERVICE_UNAVAILABLE", Message: "供应商服务不可用", Retryable: true}
	}
	return nil
}

func (s *Server) executeProviderUpdateAccountAsyncAction(ctx context.Context, _ auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var input providerActionTarget
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	if input.AccountID == "" {
		return agentToolResult{}, controlValidationError("accountId 不能为空")
	}
	request := provider.UpdateAccountRequest{}
	if value, ok := optionalStringPatch(input.Patch, "name"); ok {
		request.Name = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "baseUrl"); ok {
		request.BaseURL = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "authType"); ok {
		request.AuthType = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "status"); ok {
		request.Status = &value
	}
	if config := mapPatch(input.Patch, "config"); len(config) > 0 {
		request.Config = mustRawJSON(config)
	}
	item, err := s.providers.UpdateAccount(ctx, project.OrganizationID, input.AccountID, request)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已更新供应商账号。", map[string]any{"account": item, "accountId": item.ID}), nil
}

func (s *Server) executeProviderUpdateModelAsyncAction(ctx context.Context, _ auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var input providerActionTarget
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.ModelID = strings.TrimSpace(input.ModelID)
	if input.ModelID == "" {
		return agentToolResult{}, controlValidationError("modelId 不能为空")
	}
	request := provider.UpdateModelRequest{}
	if value, ok := optionalStringPatch(input.Patch, "modelKey"); ok {
		request.ModelKey = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "displayName"); ok {
		request.DisplayName = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "modality"); ok {
		request.Modality = &value
	}
	if value, ok := optionalStringPatch(input.Patch, "status"); ok {
		request.Status = &value
	}
	if value, ok := input.Patch["capabilities"]; ok && value != nil {
		var capabilities provider.CapabilityInput
		if err := json.Unmarshal(mustMarshal(value), &capabilities); err != nil {
			return agentToolResult{}, controlValidationError("capabilities 无效")
		}
		request.Capabilities = &capabilities
	}
	item, err := s.providers.UpdateModel(ctx, project.OrganizationID, input.ModelID, request)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已更新供应商模型。", map[string]any{"model": item, "modelId": item.ID}), nil
}

func (s *Server) executeProviderInstallCatalogAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var target providerActionTarget
	if err := decodeControlInput(raw, &target); err != nil {
		return agentToolResult{}, err
	}
	target.ProviderKey = strings.TrimSpace(target.ProviderKey)
	if target.ProviderKey == "" {
		return agentToolResult{}, controlValidationError("providerKey 不能为空")
	}
	var request provider.InstallCatalogRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return agentToolResult{}, controlValidationError("供应商预设安装参数无效")
	}
	request.OrganizationID = project.OrganizationID
	item, err := s.providers.InstallCatalogEntry(ctx, project.OrganizationID, principal.UserID, target.ProviderKey, request)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已安装供应商预设。", map[string]any{"result": item, "accountId": item.Account.ID}), nil
}

func (s *Server) executeProviderTestModelAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var input providerTestModelActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.ModelID = strings.TrimSpace(input.ModelID)
	if input.ModelID == "" {
		return agentToolResult{}, controlValidationError("modelId 不能为空")
	}
	if len(input.Input) == 0 {
		input.Input = map[string]any{"messages": []map[string]string{{"role": "user", "content": firstNonEmpty(strings.TrimSpace(input.Prompt), "Say ok.")}}}
	}
	item, err := s.providers.RecordProviderModelTest(ctx, project.OrganizationID, principal.UserID, input.ModelID, provider.TestProviderModelRequest{
		TestType: firstNonEmpty(strings.TrimSpace(input.TestType), "text_generation_test"), Input: mustRawJSON(input.Input),
		IdempotencyKey: "project-control-command:" + command.ID,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "供应商模型测试已完成。", map[string]any{
		"result": item, "testRunId": item.TestRunID, "providerCallId": item.ProviderCallID, "status": item.Status, "latencyMs": item.LatencyMS,
	}), nil
}

func (s *Server) executeProviderAttestVideoCapabilityAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var input providerVideoCapabilityActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.providers.CreateVideoCapabilityAttestation(ctx, project.OrganizationID, principal.UserID, strings.TrimSpace(input.ModelID), provider.CreateVideoCapabilityAttestationRequest{
		VariantKey: strings.TrimSpace(input.VariantKey), CapabilitySnapshotHash: strings.TrimSpace(input.CapabilitySnapshotHash),
		Decision: strings.TrimSpace(input.Decision), Reason: strings.TrimSpace(input.Reason), Evidence: mustRawJSON(input.Evidence),
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已保存视频模型能力快照结论。", map[string]any{"attestation": item}), nil
}

func (s *Server) executeProviderVerifyVideoCapabilityAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	if err := s.requireProviderControlService(); err != nil {
		return agentToolResult{}, err
	}
	var input providerVideoCapabilityActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	item, err := s.providers.VerifyVideoCapability(ctx, project.OrganizationID, principal.UserID, strings.TrimSpace(input.ModelID), provider.VerifyVideoCapabilityRequest{
		VariantKey: strings.TrimSpace(input.VariantKey), CapabilitySnapshotHash: strings.TrimSpace(input.CapabilitySnapshotHash),
		VerificationMode: strings.TrimSpace(input.VerificationMode), ProviderTestRunID: strings.TrimSpace(input.ProviderTestRunID), Reason: strings.TrimSpace(input.Reason),
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "视频模型能力快照已完成验证。", map[string]any{"attestation": item}), nil
}
