package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/jackc/pgx/v5"
)

type promptVersionCreateActionInput struct {
	TemplateID      string          `json:"templateId"`
	Title           *string         `json:"title,omitempty"`
	Content         string          `json:"content"`
	ContentFormat   string          `json:"contentFormat,omitempty"`
	VariablesSchema json.RawMessage `json:"variablesSchema,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	Activate        bool            `json:"activate,omitempty"`
}

type promptVersionCreateActionResult struct {
	TemplateID string        `json:"templateId"`
	Version    PromptVersion `json:"version"`
	Activated  bool          `json:"activated"`
}

type promptVersionActivateActionInput struct {
	VersionID string `json:"versionId"`
}

type promptVersionActivateActionResult struct {
	TemplateID string        `json:"templateId"`
	Version    PromptVersion `json:"version"`
	Idempotent bool          `json:"idempotent"`
}

type promptRenderActionInput struct {
	TemplateKey string         `json:"templateKey"`
	Variables   map[string]any `json:"variables,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
}

type promptRenderActionResult struct {
	TemplateKey     string `json:"templateKey"`
	PromptVersionID string `json:"promptVersionId"`
	RenderedHash    string `json:"renderedHash"`
	ContentHash     string `json:"contentHash"`
	PromptSource    string `json:"promptSource"`
	Text            string `json:"text"`
}

func (s *Server) createPromptVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	actorUserID string,
	source string,
	input promptVersionCreateActionInput,
) (promptVersionCreateActionResult, error) {
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.Content = strings.TrimSpace(input.Content)
	input.ContentFormat = firstNonEmpty(strings.TrimSpace(input.ContentFormat), "text")
	if input.TemplateID == "" || input.Content == "" {
		return promptVersionCreateActionResult{}, controlValidationError("templateId 和 content 不能为空")
	}
	if input.ContentFormat != "text" && input.ContentFormat != "markdown" {
		return promptVersionCreateActionResult{}, controlValidationError("contentFormat 必须是 text 或 markdown")
	}
	variablesSchema, err := normalizePromptJSONObject(input.VariablesSchema, "variablesSchema")
	if err != nil {
		return promptVersionCreateActionResult{}, err
	}
	metadata, err := normalizePromptMetadata(input.Metadata, source)
	if err != nil {
		return promptVersionCreateActionResult{}, err
	}
	if err := lockPromptTemplateForOrganization(ctx, tx, input.TemplateID, organizationID); err != nil {
		return promptVersionCreateActionResult{}, err
	}

	var versionNo int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(COALESCE(version, version_no)), 0) + 1
		FROM prompt_versions
		WHERE COALESCE(template_id, prompt_template_id) = $1
	`, input.TemplateID).Scan(&versionNo); err != nil {
		return promptVersionCreateActionResult{}, err
	}
	status := "draft"
	var activatedAt any
	if input.Activate {
		status = "active"
		activatedAt = time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE prompt_versions
			SET status = 'archived'
			WHERE COALESCE(template_id, prompt_template_id) = $1 AND status = 'active'
		`, input.TemplateID); err != nil {
			return promptVersionCreateActionResult{}, err
		}
	}
	var title *string
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value != "" {
			title = &value
		}
	}
	var versionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO prompt_versions(
			prompt_template_id, template_id, version_no, version, status, title, content, content_format,
			variables_schema, metadata, content_hash, created_by, activated_at
		)
		VALUES ($1, $1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text
	`, input.TemplateID, versionNo, status, title, input.Content, input.ContentFormat,
		variablesSchema, metadata, promptsvc.HashText(input.Content), actorUserID, activatedAt).Scan(&versionID); err != nil {
		return promptVersionCreateActionResult{}, err
	}
	version, err := scanPromptVersion(tx.QueryRow(ctx, promptVersionSelect(`WHERE id = $1`), versionID))
	if err != nil {
		return promptVersionCreateActionResult{}, err
	}
	return promptVersionCreateActionResult{TemplateID: input.TemplateID, Version: version, Activated: input.Activate}, nil
}

func (s *Server) activatePromptVersionActionTx(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	source string,
	input promptVersionActivateActionInput,
) (promptVersionActivateActionResult, error) {
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.VersionID == "" {
		return promptVersionActivateActionResult{}, controlValidationError("versionId 不能为空")
	}
	version, err := scanPromptVersion(tx.QueryRow(ctx, promptVersionSelect(`WHERE id = $1 FOR UPDATE`), input.VersionID))
	if err != nil {
		return promptVersionActivateActionResult{}, err
	}
	if err := lockPromptTemplateForOrganization(ctx, tx, version.TemplateID, organizationID); err != nil {
		return promptVersionActivateActionResult{}, err
	}
	if version.Status == "active" {
		return promptVersionActivateActionResult{TemplateID: version.TemplateID, Version: version, Idempotent: true}, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE prompt_versions
		SET status = 'archived'
		WHERE COALESCE(template_id, prompt_template_id) = $1 AND status = 'active'
	`, version.TemplateID); err != nil {
		return promptVersionActivateActionResult{}, err
	}
	metadata, err := normalizePromptMetadata(nil, source)
	if err != nil {
		return promptVersionActivateActionResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE prompt_versions
		SET status = 'active', activated_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || $2::jsonb
		WHERE id = $1
	`, version.ID, metadata); err != nil {
		return promptVersionActivateActionResult{}, err
	}
	updated, err := scanPromptVersion(tx.QueryRow(ctx, promptVersionSelect(`WHERE id = $1`), version.ID))
	if err != nil {
		return promptVersionActivateActionResult{}, err
	}
	return promptVersionActivateActionResult{TemplateID: updated.TemplateID, Version: updated}, nil
}

func (s *Server) renderPromptAction(
	ctx context.Context,
	organizationID string,
	projectID string,
	projectVariables map[string]any,
	input promptRenderActionInput,
) (promptRenderActionResult, error) {
	input.TemplateKey = strings.TrimSpace(input.TemplateKey)
	if input.TemplateKey == "" {
		return promptRenderActionResult{}, controlValidationError("templateKey 不能为空")
	}
	variables := make(map[string]any, len(input.Variables)+2)
	if len(projectVariables) > 0 {
		variables["project"] = projectVariables
	}
	if len(input.Input) > 0 {
		variables["input"] = input.Input
	}
	for key, value := range input.Variables {
		variables[key] = value
	}
	resolved, err := promptsvc.NewService(s.db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: organizationID, ProjectID: strings.TrimSpace(projectID), TemplateKey: input.TemplateKey,
	})
	if err != nil {
		return promptRenderActionResult{}, err
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return promptRenderActionResult{}, err
	}
	return promptRenderActionResult{
		TemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		RenderedHash: rendered.RenderedHash, ContentHash: rendered.ContentHash,
		PromptSource: rendered.Source, Text: rendered.RenderedText,
	}, nil
}

func (s *Server) executePromptVersionCreateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validatePromptActionCommand(command, "prompt.create_version"); err != nil {
		return agentToolResult{}, err
	}
	var input promptVersionCreateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.createPromptVersionActionTx(ctx, tx, project.OrganizationID, principal.UserID, string(command.ControllerType), input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := promptActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("prompt.create_version", arguments, fmt.Sprintf("已创建提示词版本 v%d。", result.Version.Version), map[string]any{
		"templateId": result.TemplateID, "version": result.Version,
		"versionId": result.Version.ID, "activated": result.Activated,
	}), nil
}

func (s *Server) executePromptVersionActivateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	_ auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validatePromptActionCommand(command, "prompt.activate_version"); err != nil {
		return agentToolResult{}, err
	}
	var input promptVersionActivateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.activatePromptVersionActionTx(ctx, tx, project.OrganizationID, string(command.ControllerType), input)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := promptActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已激活提示词版本 v%d。", result.Version.Version)
	if result.Idempotent {
		summary = fmt.Sprintf("提示词版本 v%d 已是激活版本。", result.Version.Version)
	}
	return agentToolOK("prompt.activate_version", arguments, summary, map[string]any{
		"templateId": result.TemplateID, "version": result.Version,
		"versionId": result.Version.ID, "status": result.Version.Status, "idempotent": result.Idempotent,
	}), nil
}

func (e *projectControlExecutor) promptRenderTest(
	ctx context.Context,
	identity controlmcp.Identity,
	raw json.RawMessage,
) (projectcontrol.Result, error) {
	var input struct {
		ProjectID   string         `json:"projectId"`
		TemplateKey string         `json:"templateKey"`
		Variables   map[string]any `json:"variables,omitempty"`
		Input       map[string]any `json:"input,omitempty"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	project, _, err := e.authorizedProjectID(ctx, identity.Principal, strings.TrimSpace(input.ProjectID), authz.PermissionPromptRead)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	rendered, err := e.server.renderPromptAction(
		ctx, project.OrganizationID, project.ID, projectPromptVariables(project), promptRenderActionInput{
			TemplateKey: input.TemplateKey, Variables: input.Variables, Input: input.Input,
		},
	)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("提示词渲染测试已完成", rendered), nil
}

func lockPromptTemplateForOrganization(ctx context.Context, tx pgx.Tx, templateID, organizationID string) error {
	var templateOrganizationID sql.NullString
	if err := tx.QueryRow(ctx, `
		SELECT organization_id::text
		FROM prompt_templates
		WHERE id = $1
		FOR UPDATE
	`, templateID).Scan(&templateOrganizationID); err != nil {
		return err
	}
	if templateOrganizationID.Valid && strings.TrimSpace(templateOrganizationID.String) != strings.TrimSpace(organizationID) {
		return auth.ErrForbidden
	}
	return nil
}

func normalizePromptJSONObject(raw json.RawMessage, field string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, controlValidationError(field + " 必须是 JSON 对象")
	}
	return json.Marshal(value)
}

func normalizePromptMetadata(raw json.RawMessage, source string) (json.RawMessage, error) {
	normalized, err := normalizePromptJSONObject(raw, "metadata")
	if err != nil {
		return nil, err
	}
	var metadata map[string]any
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, err
	}
	if source = strings.TrimSpace(source); source != "" {
		metadata["writeSource"] = source
	}
	return json.Marshal(metadata)
}

func promptActionArguments(raw json.RawMessage) (map[string]any, error) {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, fmt.Errorf("decode prompt action arguments: %w", err)
	}
	return arguments, nil
}

func validatePromptActionCommand(command projectcontrol.Command, action string) error {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" {
		return fmt.Errorf("%s command identity is incomplete", action)
	}
	return nil
}
