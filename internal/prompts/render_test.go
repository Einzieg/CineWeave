package prompts

import (
	"strings"
	"testing"
)

func TestRenderDotPath(t *testing.T) {
	rendered, err := Render(testResolvedPrompt("Hello {{ input.prompt }} in {{ project.aspectRatio }}."), map[string]any{
		"input":   map[string]any{"prompt": "sunrise"},
		"project": map[string]any{"aspectRatio": "16:9"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if rendered.RenderedText != "Hello sunrise in 16:9." {
		t.Fatalf("rendered text = %q", rendered.RenderedText)
	}
	if !strings.HasPrefix(rendered.RenderedHash, "sha256:") {
		t.Fatalf("rendered hash = %q", rendered.RenderedHash)
	}
}

func TestRenderMissingVariableAsEmptyString(t *testing.T) {
	rendered, err := Render(testResolvedPrompt("Shot: {{ shot.visual }}."), map[string]any{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if rendered.RenderedText != "Shot: ." {
		t.Fatalf("rendered text = %q", rendered.RenderedText)
	}
}

func TestRenderDoesNotExecuteCode(t *testing.T) {
	rendered, err := Render(testResolvedPrompt(`Value: {{ printf "%s" input.prompt }} {{ input.prompt }}`), map[string]any{
		"input": map[string]any{"prompt": "safe"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(rendered.RenderedText, "printf") || rendered.RenderedText != "Value:  safe" {
		t.Fatalf("rendered text = %q", rendered.RenderedText)
	}
}

func TestRenderedHashStable(t *testing.T) {
	prompt := testResolvedPrompt("Hello {{ input.prompt }}.")
	first, err := Render(prompt, map[string]any{"input": map[string]any{"prompt": "same"}})
	if err != nil {
		t.Fatalf("Render first: %v", err)
	}
	second, err := Render(prompt, map[string]any{"input": map[string]any{"prompt": "same"}})
	if err != nil {
		t.Fatalf("Render second: %v", err)
	}
	if first.RenderedHash != second.RenderedHash {
		t.Fatalf("hashes differ: %s != %s", first.RenderedHash, second.RenderedHash)
	}
}

func testResolvedPrompt(content string) ResolvedPrompt {
	return ResolvedPrompt{
		TemplateID:  "template-id",
		TemplateKey: "storyboard_planner",
		VersionID:   "version-id",
		Version:     1,
		Content:     content,
		ContentHash: "sha256:content",
		Source:      "system_active",
	}
}
