package provider

import "testing"

func TestParseOpenAIModels(t *testing.T) {
	models, err := parseOpenAIModels([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1-mini"}]}`))
	if err != nil {
		t.Fatalf("parseOpenAIModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ModelKey != "gpt-4o-mini" || models[0].Modality != "text" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
}

func TestParseChatCompletionText(t *testing.T) {
	text, err := parseChatCompletionText([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	if err != nil {
		t.Fatalf("parseChatCompletionText() error = %v", err)
	}
	if text != "pong" {
		t.Fatalf("text = %q, want pong", text)
	}
}
