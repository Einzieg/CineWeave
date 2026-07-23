package provider

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeCapabilityInputPersistsLanguageMetadata(t *testing.T) {
	capability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:                     json.RawMessage(`["text.generate"]`),
		ProviderOptionsSchema:         json.RawMessage(`{"xCapabilities":{"supportsStreaming":false}}`),
		SupportedInputLanguages:       []string{"ZH-cn", "en-us", "zh-CN"},
		SupportedOutputLanguages:      []string{"*"},
		SupportedPromptLanguages:      []string{},
		SupportedNativeAudioLanguages: []string{},
		Source:                        CapabilitySourceManual,
		ApprovalStatus:                CapabilityApprovalApproved,
	})
	if err != nil {
		t.Fatalf("normalizeCapabilityInput: %v", err)
	}
	if want := []string{"en-US", "zh-CN"}; !reflect.DeepEqual(capability.SupportedInputLanguages, want) {
		t.Fatalf("SupportedInputLanguages = %#v, want %#v", capability.SupportedInputLanguages, want)
	}
	metadata := capabilityLanguageMetadataFromSchema(capability.ProviderOptionsSchema)
	if !reflect.DeepEqual(metadata.SupportedInputLanguages, capability.SupportedInputLanguages) {
		t.Fatalf("persisted input languages = %#v", metadata.SupportedInputLanguages)
	}
	if metadata.Source != CapabilitySourceManual || metadata.ApprovalStatus != CapabilityApprovalApproved {
		t.Fatalf("metadata provenance = %s/%s", metadata.Source, metadata.ApprovalStatus)
	}
}

func TestNormalizeCapabilityInputRejectsInvalidLanguageTag(t *testing.T) {
	_, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:                json.RawMessage(`["image.generate"]`),
		ProviderOptionsSchema:    json.RawMessage(`{}`),
		SupportedPromptLanguages: []string{"中文"},
		Source:                   CapabilitySourceManual,
		ApprovalStatus:           CapabilityApprovalApproved,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestValidateModelLanguageCapabilitiesRequiresApproval(t *testing.T) {
	model := languageCapabilityTestModel(Capability{
		TaskTypes:                json.RawMessage(`["text.generate"]`),
		SupportedInputLanguages:  []string{"zh-CN"},
		SupportedOutputLanguages: []string{"zh-CN"},
		Source:                   CapabilitySourceInferred,
		ApprovalStatus:           CapabilityApprovalInferred,
	})
	err := ValidateModelLanguageCapabilities(model, TaskTypeTextGenerate, LanguageCapabilityRequirement{
		InputLanguage: "zh-CN", OutputLanguage: "zh-CN", RequireApproved: true,
	})
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeModelCapabilityApprovalRequired {
		t.Fatalf("error = %v, want %s", err, CodeModelCapabilityApprovalRequired)
	}
}

func TestValidateModelLanguageCapabilitiesRejectsUnsupportedLanguage(t *testing.T) {
	model := languageCapabilityTestModel(Capability{
		TaskTypes:                json.RawMessage(`["image.generate"]`),
		SupportedPromptLanguages: []string{"en-US"},
		Source:                   CapabilitySourceOfficial,
		ApprovalStatus:           CapabilityApprovalApproved,
	})
	err := ValidateModelLanguageCapabilities(model, TaskTypeImageGenerate, LanguageCapabilityRequirement{PromptLanguage: "zh-CN"})
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeUnsupportedCapability {
		t.Fatalf("error = %v, want %s", err, CodeUnsupportedCapability)
	}
}

func TestFilterRoutingCandidatesByLanguageSkipsIncompatiblePriorityModel(t *testing.T) {
	candidates := []RoutingCandidate{
		{
			ProviderModelID: "model-en", Priority: 100,
			Capabilities: []Capability{{
				TaskTypes: json.RawMessage(`["text.generate"]`), SupportedInputLanguages: []string{"en-US"},
				SupportedOutputLanguages: []string{"en-US"}, Source: CapabilitySourceOfficial, ApprovalStatus: CapabilityApprovalApproved,
			}},
		},
		{
			ProviderModelID: "model-zh", Priority: 200,
			Capabilities: []Capability{{
				TaskTypes: json.RawMessage(`["text.generate"]`), SupportedInputLanguages: []string{"zh-CN"},
				SupportedOutputLanguages: []string{"zh-CN"}, Source: CapabilitySourceOfficial, ApprovalStatus: CapabilityApprovalApproved,
			}},
		},
	}
	filtered, err := filterRoutingCandidatesByLanguage(candidates, RoutingRequest{
		TaskType: TaskTypeTextGenerate, InputLanguage: "zh-CN", OutputLanguage: "zh-CN", RequireApprovedLanguageCapabilities: true,
	})
	if err != nil {
		t.Fatalf("filterRoutingCandidatesByLanguage: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProviderModelID != "model-zh" {
		t.Fatalf("filtered candidates = %#v", filtered)
	}
}

func TestVideoVariantMetadataDerivesLanguageProvenance(t *testing.T) {
	metadata := capabilityLanguageMetadataFromSchema(json.RawMessage(`{
		"xCapabilities": {"videoGenerationVariants": [{
			"supportedPromptLanguages": ["zh-CN"],
			"nativeAudio": {"supportedDialogueLanguages": ["zh-CN"]},
			"source": "official",
			"verificationStatus": "official"
		}]}
	}`))
	if !reflect.DeepEqual(metadata.SupportedPromptLanguages, []string{"zh-CN"}) ||
		!reflect.DeepEqual(metadata.SupportedNativeAudioLanguages, []string{"zh-CN"}) {
		t.Fatalf("derived languages = %#v", metadata)
	}
	if metadata.Source != CapabilitySourceOfficial || metadata.ApprovalStatus != CapabilityApprovalApproved {
		t.Fatalf("derived provenance = %s/%s", metadata.Source, metadata.ApprovalStatus)
	}
}

func languageCapabilityTestModel(capability Capability) Model {
	return Model{ID: "model", Status: "active", Capabilities: []Capability{capability}}
}
