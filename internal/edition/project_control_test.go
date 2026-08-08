package edition

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestValidateProjectControlActionRegistrations(t *testing.T) {
	apiRegistration := APIModuleRegistration{
		ModuleKey: "billing.organization-wallets", FeatureKey: FeatureBillingOrganizationWallet,
		Method: http.MethodPut, Pattern: "/api/projects/{projectId}/billing-account",
		OperationID: "putProjectBillingAccount", Operation: CommercialOperationWrite,
		RequiredPermissions: []string{"project.write", "billing.manage"},
		ResourceScope:       APIResourceScopeProject, ResourcePathParameter: "projectId",
		Handler: func(http.ResponseWriter, *http.Request, APIPrincipal) {},
	}
	descriptor := projectcontrol.Descriptor{
		Name: "billing.project_account.set", Version: 1,
		Label: "绑定项目付费账户", Summary: "绑定项目付费账户", Description: "绑定项目付费账户",
		Risk: projectcontrol.RiskAdmin, Scope: projectcontrol.ScopeProject,
		Permissions:      []string{"billing.manage", "project.write"},
		InputSchema:      json.RawMessage(`{"type":"object","properties":{"projectId":{"type":"string"}}}`),
		OutputSchema:     json.RawMessage(`{"type":"object","additionalProperties":true}`),
		RequiresApproval: true, Effects: projectcontrol.Effects{WritesProject: true},
		ReadOnly: false, Idempotent: true,
		ExecutionMode:      projectcontrol.ExecutionModeAsyncCommand,
		ActivityVisibility: projectcontrol.ActivityVisibilityPrimary,
		ExportToMCP:        true,
	}
	registration := ProjectControlActionRegistration{
		ModuleKey: apiRegistration.ModuleKey, FeatureKey: apiRegistration.FeatureKey,
		APIOperationID: apiRegistration.OperationID, Descriptor: descriptor,
		Handler: func(context.Context, ProjectControlActionRequest) (projectcontrol.Result, error) {
			return projectcontrol.NewResult("succeeded", "ok"), nil
		},
	}
	if err := validateProjectControlActionRegistrations(
		[]ProjectControlActionRegistration{registration},
		[]APIModuleRegistration{apiRegistration},
	); err != nil {
		t.Fatalf("valid registration: %v", err)
	}

	t.Run("permission drift", func(t *testing.T) {
		invalid := registration
		invalid.Descriptor = descriptor.Clone()
		invalid.Descriptor.Permissions = []string{"project.write"}
		if err := validateProjectControlActionRegistrations(
			[]ProjectControlActionRegistration{invalid},
			[]APIModuleRegistration{apiRegistration},
		); err == nil {
			t.Fatal("expected permission drift to fail")
		}
	})

	t.Run("non-project API", func(t *testing.T) {
		invalidAPI := apiRegistration
		invalidAPI.ResourceScope = APIResourceScopeOrganization
		invalidAPI.ResourcePathParameter = ""
		if err := validateProjectControlActionRegistrations(
			[]ProjectControlActionRegistration{registration},
			[]APIModuleRegistration{invalidAPI},
		); err == nil {
			t.Fatal("expected organization API binding to fail")
		}
	})
}
