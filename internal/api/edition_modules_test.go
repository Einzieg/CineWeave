package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
)

type entitlementServiceFunc func(context.Context, editionpkg.EntitlementRequest) (editionpkg.EntitlementSnapshot, error)

func (f entitlementServiceFunc) Evaluate(ctx context.Context, request editionpkg.EntitlementRequest) (editionpkg.EntitlementSnapshot, error) {
	return f(ctx, request)
}

type editionPermissionAuthorizerFunc func(context.Context, auth.Principal, string, authz.Resource) error

func (f editionPermissionAuthorizerFunc) Authorize(ctx context.Context, principal auth.Principal, permission string, resource authz.Resource) error {
	return f(ctx, principal, permission, resource)
}

func TestAuthorizeEditionAPIModuleSeparatesEntitlementAndRBACDenials(t *testing.T) {
	principal := auth.Principal{UserID: "user-1", OrganizationID: "org-1"}
	resource := authz.Resource{OrganizationID: "org-1"}
	registration := editionpkg.APIModuleRegistration{
		ModuleKey:           "billing-api",
		FeatureKey:          editionpkg.FeatureBillingBalance,
		OperationID:         "getBillingBalance",
		Operation:           editionpkg.CommercialOperationReadOrExport,
		RequiredPermissions: []string{"billing.read"},
	}

	t.Run("tenant entitlement", func(t *testing.T) {
		permissionCalls := 0
		err := authorizeEditionAPIModule(
			t.Context(),
			fixedEntitlementService(principal, "", registration.FeatureKey, editionpkg.EntitlementDecision{
				FeatureKey:        registration.FeatureKey,
				Compiled:          true,
				DeploymentEnabled: true,
				TenantEntitled:    false,
				Allowed:           false,
				Reason:            editionpkg.DenialPlanEntitlementRequired,
			}),
			editionPermissionAuthorizerFunc(func(context.Context, auth.Principal, string, authz.Resource) error {
				permissionCalls++
				return nil
			}),
			principal,
			resource,
			"",
			registration,
		)
		assertEditionAuthorizationCode(t, err, editionpkg.DenialPlanEntitlementRequired)
		if permissionCalls != 0 {
			t.Fatalf("RBAC evaluated before entitlement denial: calls=%d", permissionCalls)
		}
	})

	t.Run("RBAC", func(t *testing.T) {
		err := authorizeEditionAPIModule(
			t.Context(),
			fixedEntitlementService(principal, "", registration.FeatureKey, allowedEntitlementDecision(registration.FeatureKey)),
			editionPermissionAuthorizerFunc(func(_ context.Context, _ auth.Principal, permission string, got authz.Resource) error {
				return authz.AccessError{Permission: permission, Resource: got}
			}),
			principal,
			resource,
			"",
			registration,
		)
		assertEditionAuthorizationCode(t, err, editionpkg.DenialPermission)
	})
}

func TestAuthorizeEditionAPIModuleBindsOperationAndBillingAccount(t *testing.T) {
	principal := auth.Principal{UserID: "user-1", OrganizationID: "org-1"}
	registration := editionpkg.APIModuleRegistration{
		ModuleKey:           "billing-api",
		FeatureKey:          editionpkg.FeatureBillingOrganizationWallet,
		OperationID:         "updateProjectBillingAccount",
		Operation:           editionpkg.CommercialOperationWrite,
		RequiredPermissions: []string{"billing.manage"},
	}
	var evaluated editionpkg.EntitlementRequest
	var authorizedPermission string
	err := authorizeEditionAPIModule(
		t.Context(),
		entitlementServiceFunc(func(_ context.Context, request editionpkg.EntitlementRequest) (editionpkg.EntitlementSnapshot, error) {
			evaluated = request
			return editionpkg.EntitlementSnapshot{
				ContractVersion: editionpkg.ContractVersionV2,
				Edition:         editionpkg.EditionCloud,
				Subject:         request.Subject,
				Decisions:       []editionpkg.EntitlementDecision{allowedEntitlementDecision(registration.FeatureKey)},
			}, nil
		}),
		editionPermissionAuthorizerFunc(func(_ context.Context, _ auth.Principal, permission string, _ authz.Resource) error {
			authorizedPermission = permission
			return nil
		}),
		principal,
		authz.Resource{OrganizationID: principal.OrganizationID},
		"billing-account-1",
		registration,
	)
	if err != nil {
		t.Fatalf("authorizeEditionAPIModule: %v", err)
	}
	if evaluated.Operation != editionpkg.CommercialOperationWrite ||
		evaluated.Subject.BillingAccountID != "billing-account-1" {
		t.Fatalf("entitlement request = %+v", evaluated)
	}
	if authorizedPermission != "billing.manage" {
		t.Fatalf("authorized permission = %q", authorizedPermission)
	}
}

func TestAuthorizeEditionAPIModuleRejectsInconsistentAllowDecision(t *testing.T) {
	principal := auth.Principal{UserID: "user-1", OrganizationID: "org-1"}
	registration := editionpkg.APIModuleRegistration{
		FeatureKey:          editionpkg.FeatureBillingBalance,
		Operation:           editionpkg.CommercialOperationReadOrExport,
		RequiredPermissions: []string{"billing.read"},
	}
	err := authorizeEditionAPIModule(
		t.Context(),
		fixedEntitlementService(principal, "", registration.FeatureKey, editionpkg.EntitlementDecision{
			FeatureKey:        registration.FeatureKey,
			Compiled:          true,
			DeploymentEnabled: false,
			TenantEntitled:    true,
			Allowed:           true,
		}),
		editionPermissionAuthorizerFunc(func(context.Context, auth.Principal, string, authz.Resource) error {
			t.Fatal("RBAC must not run for an inconsistent entitlement response")
			return nil
		}),
		principal,
		authz.Resource{OrganizationID: principal.OrganizationID},
		"",
		registration,
	)
	if err == nil {
		t.Fatal("inconsistent allow decision was accepted")
	}
}

func TestEditionModuleResourceUsesDeclaredCoreScope(t *testing.T) {
	principal := auth.Principal{UserID: "user-1", OrganizationID: "org-1"}

	organizationRequest := httptest.NewRequest(http.MethodGet, "/api/billing/accounts", nil)
	organization, err := editionModuleResource(organizationRequest, principal, editionpkg.APIModuleRegistration{
		OperationID:   "listBillingAccounts",
		ResourceScope: editionpkg.APIResourceScopeOrganization,
	})
	if err != nil || organization.OrganizationID != principal.OrganizationID {
		t.Fatalf("organization resource = %+v err=%v", organization, err)
	}

	projectRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/billing-account", nil)
	projectRequest.SetPathValue("projectId", "project-1")
	project, err := editionModuleResource(projectRequest, principal, editionpkg.APIModuleRegistration{
		OperationID:           "getProjectBillingAccount",
		ResourceScope:         editionpkg.APIResourceScopeProject,
		ResourcePathParameter: "projectId",
	})
	if err != nil || project.ProjectID != "project-1" {
		t.Fatalf("project resource = %+v err=%v", project, err)
	}
}

func fixedEntitlementService(
	principal auth.Principal,
	billingAccountID string,
	featureKey editionpkg.FeatureKey,
	decision editionpkg.EntitlementDecision,
) editionpkg.EntitlementService {
	return entitlementServiceFunc(func(_ context.Context, request editionpkg.EntitlementRequest) (editionpkg.EntitlementSnapshot, error) {
		if len(request.FeatureKeys) != 1 || request.FeatureKeys[0] != featureKey {
			return editionpkg.EntitlementSnapshot{}, errors.New("unexpected feature request")
		}
		return editionpkg.EntitlementSnapshot{
			ContractVersion: editionpkg.ContractVersionV2,
			Edition:         editionpkg.EditionCloud,
			Subject: editionpkg.EntitlementSubject{
				UserID:           principal.UserID,
				OrganizationID:   principal.OrganizationID,
				BillingAccountID: billingAccountID,
			},
			Decisions: []editionpkg.EntitlementDecision{decision},
		}, nil
	})
}

func allowedEntitlementDecision(featureKey editionpkg.FeatureKey) editionpkg.EntitlementDecision {
	return editionpkg.EntitlementDecision{
		FeatureKey:        featureKey,
		Compiled:          true,
		DeploymentEnabled: true,
		TenantEntitled:    true,
		Allowed:           true,
	}
}

func assertEditionAuthorizationCode(t *testing.T, err error, want editionpkg.DenialCode) {
	t.Helper()
	var authorizationErr editionpkg.AuthorizationError
	if !errors.As(err, &authorizationErr) || authorizationErr.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}
