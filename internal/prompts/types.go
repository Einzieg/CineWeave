package prompts

import "fmt"

const (
	CodePromptTemplateNotFound = "PROMPT_TEMPLATE_NOT_FOUND"
	CodePromptVersionNotFound  = "PROMPT_VERSION_NOT_FOUND"
	CodePromptRenderFailed     = "PROMPT_RENDER_FAILED"
)

type Error struct {
	Code    string
	Message string
}

func (e Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type ResolveRequest struct {
	OrganizationID string
	ProjectID      string
	TemplateKey    string
}

type ResolvedPrompt struct {
	TemplateID  string
	TemplateKey string
	VersionID   string
	Version     int
	Content     string
	ContentHash string
	Source      string
}

type RenderRequest struct {
	Prompt    ResolvedPrompt
	Variables map[string]any
}

type RenderedPrompt struct {
	PromptVersionID string
	TemplateKey     string
	RenderedText    string
	RenderedHash    string
	ContentHash     string
	Source          string
}
