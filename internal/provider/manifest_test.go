package provider

import (
	"encoding/json"
	"testing"
)

func TestParseAndValidateManifestYAML(t *testing.T) {
	manifest, _, err := ParseManifest(nil, `
kind: ProviderConnector
version: v1
id: mock-image
name: Mock Image
transport: http
baseUrl: http://127.0.0.1:19181
auth:
  type: bearer
models:
  - id: mock-image
    displayName: Mock Image
    modality: image
    capabilities:
      taskTypes:
        - image.generate
endpoints:
  image_generate:
    endpointType: sync
    method: POST
    pathTemplate: /images
    requestTemplate:
      prompt: "{{input.prompt}}"
    responseMapping:
      imageUrl: "$.data[0].url"
`)
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	result := ValidateManifest(manifest)
	if !result.Valid {
		t.Fatalf("ValidateManifest() errors = %+v", result.Errors)
	}
}

func TestValidateManifestReportsMissingFields(t *testing.T) {
	result := ValidateManifest(ProviderManifest{})
	if result.Valid {
		t.Fatal("ValidateManifest() returned valid for empty manifest")
	}
	if len(result.Errors) == 0 {
		t.Fatal("ValidateManifest() returned no errors")
	}
}

func TestRenderTemplateJSONPreservesScalarType(t *testing.T) {
	raw := json.RawMessage(`{"width":"{{input.width}}","prompt":"hello {{input.name}}"}`)
	rendered, err := renderTemplateJSON(raw, map[string]any{
		"input": map[string]any{"width": float64(1024), "name": "Ada"},
	})
	if err != nil {
		t.Fatalf("renderTemplateJSON() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rendered, &decoded); err != nil {
		t.Fatalf("rendered JSON is invalid: %v", err)
	}
	if decoded["width"] != float64(1024) {
		t.Fatalf("width = %#v, want 1024", decoded["width"])
	}
	if decoded["prompt"] != "hello Ada" {
		t.Fatalf("prompt = %#v", decoded["prompt"])
	}
}

func TestMapResponseJSONPath(t *testing.T) {
	mapped, err := mapResponse(
		[]byte(`{"data":[{"url":"https://cdn.example/image.png"}],"status":"succeeded"}`),
		json.RawMessage(`{"imageUrl":"$.data[0].url","status":"$.status"}`),
	)
	if err != nil {
		t.Fatalf("mapResponse() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(mapped, &decoded); err != nil {
		t.Fatalf("mapped JSON is invalid: %v", err)
	}
	if decoded["imageUrl"] != "https://cdn.example/image.png" {
		t.Fatalf("imageUrl = %#v", decoded["imageUrl"])
	}
	if decoded["status"] != "succeeded" {
		t.Fatalf("status = %#v", decoded["status"])
	}
}
