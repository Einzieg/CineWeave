package api

import "testing"

func TestProjectSettingsDefaults(t *testing.T) {
	if got := normalizedProjectString(nil, "16:9"); got != "16:9" {
		t.Fatalf("default video ratio = %q", got)
	}
	value := " 9:16 "
	if got := normalizedProjectString(&value, "16:9"); got != "9:16" {
		t.Fatalf("trimmed value = %q", got)
	}
	blank := " "
	if got := normalizedProjectString(&blank, "silent_video"); got != "silent_video" {
		t.Fatalf("blank fallback = %q", got)
	}
}

func TestSourceValidation(t *testing.T) {
	if !validSourceType("novel") || !validSourceType("script") || validSourceType("audio") {
		t.Fatalf("source type validation failed")
	}
	if !validContentFormat("plain_text") || !validContentFormat("markdown") || validContentFormat("html") {
		t.Fatalf("content format validation failed")
	}
}

func TestScriptValidation(t *testing.T) {
	if !validScriptStatus("draft") || !validScriptStatus("active") || !validScriptStatus("archived") || validScriptStatus("deleted") {
		t.Fatalf("script status validation failed")
	}
	if !validScriptContentFormat("markdown") || !validScriptContentFormat("plain_text") || validScriptContentFormat("html") {
		t.Fatalf("script content format validation failed")
	}
}
