package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectGatewayTextImageReferencesBuildsMultimodalUserMessage(t *testing.T) {
	input := json.RawMessage(`{"prompt":"compare product packaging","temperature":0}`)
	result, err := injectGatewayTextImageReferences(input, []openAICompatibleImageReference{{
		MimeType: "image/png",
		Body:     []byte("image-bytes"),
	}})
	if err != nil {
		t.Fatalf("injectGatewayTextImageReferences: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if _, exists := decoded["prompt"]; exists {
		t.Fatal("prompt must be converted into a multimodal user message")
	}
	messages, ok := decoded["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", decoded["messages"])
	}
	encoded := string(result)
	if !strings.Contains(encoded, "data:image/png;base64,aW1hZ2UtYnl0ZXM=") {
		t.Fatalf("multimodal image content missing: %s", encoded)
	}
}

func TestGatewayTextRequestSnapshotDoesNotPersistReferenceURLOrBytes(t *testing.T) {
	reference := GatewayImageReference{
		Type:       "commerce_product_reference",
		ArtifactID: "artifact-1",
		StorageKey: "commerce/product/reference.png",
		URL:        "https://temporary.example/signed-secret",
		Metadata:   json.RawMessage(`{"referenceKey":"product:front"}`),
	}
	snapshot := gatewayTextRequestSnapshot(json.RawMessage(`{"prompt":"review"}`), []GatewayImageReference{reference})
	text := string(snapshot)
	if strings.Contains(text, reference.URL) || strings.Contains(text, "base64") {
		t.Fatalf("snapshot leaked transient media content: %s", text)
	}
	if !strings.Contains(text, reference.StorageKey) || !strings.Contains(text, "product:front") {
		t.Fatalf("snapshot lost durable reference provenance: %s", text)
	}
}
