package provider

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/commerce"
)

func TestDecodeCommerceDirectVideoExecutionContractPreservesCompleteWireHash(t *testing.T) {
	supportsVoiceover := true
	inputContract := commerce.DirectVideoInputContract{
		ContractKey: "first_frame",
		RequestMode: "async_create",
		Slots: []commerce.DirectVideoInputSlot{{
			Role: "first_frame", MediaType: "image", Semantics: "initial_frame",
			Min: 1, Max: 1, Ordered: true,
		}},
	}
	inputHash, err := commerce.DirectVideoHash(inputContract)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"contractVersion": commerce.CommerceDirectVideoContractV1,
		"route": commerce.DirectVideoRoute{
			RouteKey:       "binding:model:variant",
			ModelProfileID: "profile", ModelProfileKey: "video_generator",
			ModelProfileBindingID: "binding", ProviderModelID: "model",
			ProviderAccountID: "account", ProviderModelKey: "video-model",
			Priority: 100, Weight: 50, VariantKey: "image-to-video",
			CapabilitySnapshotHash:    "capability-hash",
			ExecutableDurationSeconds: []int{6, 10, 12, 16},
			Resolutions:               []string{"720p", "1080p"},
			AspectRatios:              []string{"9:16", "16:9"},
			InputContract:             inputContract,
			NativeAudio: commerce.DirectVideoNativeAudio{
				Support: "true", SupportsVoiceover: &supportsVoiceover,
			},
		},
		"inputContractHash": inputHash,
		"durationSeconds":   6,
		"resolution":        "720p",
		"aspectRatio":       "9:16",
		"generateAudio":     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	executionHash, err := commerce.DirectVideoHash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	var narrowed commerceDirectVideoExecutionContract
	if err := json.Unmarshal(raw, &narrowed); err != nil {
		t.Fatal(err)
	}
	narrowedHash, err := stableJSONHash(narrowed)
	if err != nil {
		t.Fatal(err)
	}
	if narrowedHash == executionHash {
		t.Fatal("test fixture must include producer fields omitted by the gateway semantic view")
	}

	contract, gotInputHash, err := decodeCommerceDirectVideoExecutionContract(raw, executionHash)
	if err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	if contract.Route.ProviderModelID != "model" || contract.Route.InputContract.ContractKey != "first_frame" {
		t.Fatalf("decoded contract = %+v", contract)
	}
	if gotInputHash != inputHash {
		t.Fatalf("input contract hash = %s, want %s", gotInputHash, inputHash)
	}
}

func TestDecodeCommerceDirectVideoExecutionContractRejectsHashMismatch(t *testing.T) {
	raw := []byte(`{"contractVersion":"commerce-direct-video/v1","route":{"inputContract":{"contractKey":"first_frame"}}}`)
	_, _, err := decodeCommerceDirectVideoExecutionContract(raw, strings.Repeat("0", 64))
	var standard *StandardErrorError
	if !errors.As(err, &standard) ||
		standard.Standard.Code != CodeRenderPlanReplanRequired ||
		standard.Standard.Message != "带货视频直生成执行契约完整性校验失败" {
		t.Fatalf("error = %v, want integrity failure", err)
	}
}
