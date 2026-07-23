package provider

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestApplyCompiledVideoPlanProfileVersionUsesRuntimeContinuationContracts(t *testing.T) {
	version := singleFrameVideoProfileVersionFixture()
	contract := videoPlanProductionContract{
		ProfileVersionID:             version.ID,
		RequiredInitialInputContract: videoproduction.InputContractFirstFrame,
		InputContractVersion:         version.InputContractVersion,
	}

	if err := applyCompiledVideoPlanProfileVersion(&contract, version); err != nil {
		t.Fatalf("applyCompiledVideoPlanProfileVersion: %v", err)
	}
	if contract.RequiredInitialInputContract != videoproduction.InputContractFirstFrame {
		t.Fatalf("initial input contract = %q", contract.RequiredInitialInputContract)
	}
	want := []string{
		videoproduction.InputContractVideoExtension,
		videoproduction.InputContractFirstFrame,
	}
	if !sameNormalizedVideoStringSlice(contract.AllowedContinuationInputContracts, want) {
		t.Fatalf("continuation contracts = %#v, want %#v", contract.AllowedContinuationInputContracts, want)
	}
}

func TestApplyCompiledVideoPlanProfileVersionRejectsDeclaredContinuationDrift(t *testing.T) {
	version := singleFrameVideoProfileVersionFixture()
	contract := videoPlanProductionContract{
		ProfileVersionID:                  version.ID,
		RequiredInitialInputContract:      videoproduction.InputContractFirstFrame,
		AllowedContinuationInputContracts: []string{videoproduction.InputContractFirstLastFrames},
		InputContractVersion:              version.InputContractVersion,
	}

	err := applyCompiledVideoPlanProfileVersion(&contract, version)
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeProductionProfileIncompatible {
		t.Fatalf("error = %v, want %s", err, CodeProductionProfileIncompatible)
	}
}

func singleFrameVideoProfileVersionFixture() videoproduction.ProfileVersion {
	configuration, _ := json.Marshal(map[string]any{
		"anchorRoles": []string{videoproduction.AnchorRolePlannedFirstFrame},
	})
	capabilities, _ := json.Marshal(map[string]any{
		"taskType":      "video.image_to_video",
		"inputContract": videoproduction.InputContractFirstFrame,
		"maxFirstFrames": map[string]any{
			"minimum": 1,
		},
	})
	promptContract, _ := json.Marshal(map[string]string{
		"anchorPlan":     "video_profile.single_frame_i2v.anchor.plan",
		"anchorGenerate": "video_profile.single_frame_i2v.anchor.generate",
		"anchorReview":   "video_profile.single_frame_i2v.anchor.review",
		"videoGenerate":  "video_profile.single_frame_i2v.video.generate",
		"videoReview":    "video_profile.single_frame_i2v.video.review",
	})
	return videoproduction.ProfileVersion{
		ID:                     "00000000-0000-4000-8000-000000000001",
		ProfileKey:             videoproduction.ProfileSingleFrameI2V,
		Version:                1,
		LifecycleState:         videoproduction.LifecyclePublished,
		ImplementationState:    videoproduction.ImplementationAvailable,
		Configuration:          configuration,
		CapabilityRequirements: capabilities,
		PromptContract:         promptContract,
		InputContractVersion:   "video-input-contract/v1",
	}
}
