package provider

import (
	"errors"
	"testing"
)

func TestValidateVideoCreateProductionContractRejectsRenderPlanIdentityMismatch(t *testing.T) {
	request := GatewayVideoCreateTaskRequest{
		OrganizationID: "organization", ProjectID: "project", StoryboardShotID: "shot",
		ProductionGenerationID: "generation", VideoProductionBindingID: "binding",
		VideoProductionBindingRevision: 3,
		ProductionProfileVersionID:     "profile", ProductionProfileSnapshotHash: "profile-hash",
		InputContractKey: "first_frame", InputContractHash: "input-hash", InputContractVersion: "v1",
		ShotStateRevision: 2, ShotStateHash: "shot-hash", TransitionHash: "transition-hash",
		ReferencePackID: "reference-pack", ReferencePackHash: "reference-hash",
		PromptContextPlanID: "context-plan", PromptContextPlanHash: "context-hash",
		VideoPromptPlanID: "prompt-plan", PromptHash: "prompt-hash",
	}
	segment := videoExecutionSegment{
		OrganizationID: "organization", ProjectID: "project", StoryboardShotID: "shot",
		ProductionGenerationID: "generation", VideoProductionBindingID: "binding",
		VideoProductionBindingRevision: 3,
		ProductionProfileVersionID:     "profile", ProductionProfileSnapshotHash: "profile-hash",
		InputContractKey: "first_frame", InputContractHash: "input-hash", InputContractVersion: "v1",
		ShotStateRevision: 2, ShotStateHash: "shot-hash", TransitionHash: "transition-hash",
		ReferencePackID: "reference-pack", ReferencePackHash: "reference-hash",
		PromptContextPlanID: "context-plan", PromptContextPlanHash: "context-hash",
		VideoPromptPlanID: "prompt-plan", PromptHash: "prompt-hash", Prompt: "approved prompt",
	}

	tests := []struct {
		name   string
		mutate func(*GatewayVideoCreateTaskRequest)
	}{
		{name: "organization", mutate: func(req *GatewayVideoCreateTaskRequest) { req.OrganizationID = "other" }},
		{name: "project", mutate: func(req *GatewayVideoCreateTaskRequest) { req.ProjectID = "other" }},
		{name: "generation", mutate: func(req *GatewayVideoCreateTaskRequest) { req.ProductionGenerationID = "other" }},
		{name: "binding", mutate: func(req *GatewayVideoCreateTaskRequest) { req.VideoProductionBindingID = "other" }},
		{name: "binding revision", mutate: func(req *GatewayVideoCreateTaskRequest) { req.VideoProductionBindingRevision++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := request
			test.mutate(&candidate)
			err := validateVideoCreateProductionContract(candidate, gatewayVideoInput{Prompt: "approved prompt"}, segment)
			var standard *StandardErrorError
			if !errors.As(err, &standard) || standard.Standard.Code != CodeRenderPlanReplanRequired {
				t.Fatalf("error = %v, want %s", err, CodeRenderPlanReplanRequired)
			}
		})
	}
}
