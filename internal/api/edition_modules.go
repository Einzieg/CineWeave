package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
)

type editionPermissionAuthorizer interface {
	Authorize(context.Context, auth.Principal, string, authz.Resource) error
}

func (s *Server) registerEditionAPIModules(mux *http.ServeMux) {
	registrations, err := s.currentEditionRuntime().ValidatedAPIModules(context.Background())
	if err != nil {
		panic(fmt.Sprintf("load validated commercial API modules: %v", err))
	}
	if len(registrations) > 0 && (s.auth == nil || s.authorizer == nil) {
		panic("commercial API modules require Core authentication and authorization services")
	}
	for _, registration := range registrations {
		registration := registration
		pattern := registration.Method + " " + registration.Pattern
		handler := s.withAuth(func(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
			resource, err := editionModuleResource(r, principal, registration)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			if err := authorizeEditionAPIModule(
				r.Context(),
				s.currentEditionRuntime().Entitlements,
				s.authorizer,
				principal,
				resource,
				strings.TrimSpace(r.PathValue("billingAccountId")),
				registration,
			); err != nil {
				s.writeError(w, r, err)
				return
			}
			registration.Handler(w, r, editionpkg.APIPrincipal{
				UserID:           principal.UserID,
				OrganizationID:   principal.OrganizationID,
				BillingAccountID: strings.TrimSpace(r.PathValue("billingAccountId")),
			})
		})
		mustRegisterEditionAPIRoute(mux, pattern, handler, registration.OperationID)
	}
}

func authorizeEditionAPIModule(
	ctx context.Context,
	entitlements editionpkg.EntitlementService,
	authorizer editionPermissionAuthorizer,
	principal auth.Principal,
	resource authz.Resource,
	billingAccountID string,
	registration editionpkg.APIModuleRegistration,
) error {
	snapshot, err := entitlements.Evaluate(ctx, editionpkg.EntitlementRequest{
		Subject: editionpkg.EntitlementSubject{
			UserID:           principal.UserID,
			OrganizationID:   principal.OrganizationID,
			BillingAccountID: strings.TrimSpace(billingAccountID),
		},
		FeatureKeys: []editionpkg.FeatureKey{registration.FeatureKey},
		Operation:   registration.Operation,
	})
	if err != nil {
		return err
	}
	if snapshot.ContractVersion != editionpkg.ContractVersionV2 ||
		snapshot.Subject.UserID != principal.UserID ||
		snapshot.Subject.OrganizationID != principal.OrganizationID ||
		snapshot.Subject.BillingAccountID != strings.TrimSpace(billingAccountID) {
		return fmt.Errorf("commercial entitlement response identity is inconsistent")
	}
	var decision *editionpkg.EntitlementDecision
	for index := range snapshot.Decisions {
		if snapshot.Decisions[index].FeatureKey != registration.FeatureKey {
			continue
		}
		if decision != nil {
			return fmt.Errorf("commercial entitlement response duplicates feature %q", registration.FeatureKey)
		}
		decision = &snapshot.Decisions[index]
	}
	if decision == nil {
		return editionpkg.AuthorizationError{
			Code:    editionpkg.DenialFeatureUnknown,
			Message: "commercial feature entitlement decision is missing",
		}
	}
	if decision.Allowed {
		if !decision.Compiled || !decision.DeploymentEnabled || !decision.TenantEntitled || decision.Reason != "" {
			return fmt.Errorf("commercial entitlement allow decision is internally inconsistent")
		}
	} else {
		if decision.Reason == "" {
			return fmt.Errorf("commercial entitlement deny decision has no reason")
		}
		return editionpkg.AuthorizationError{
			Code:    decision.Reason,
			Message: "commercial feature is not authorized",
			Details: map[string]string{
				"featureKey": string(registration.FeatureKey),
			},
		}
	}
	for _, permission := range registration.RequiredPermissions {
		if err := authorizer.Authorize(ctx, principal, permission, resource); err != nil {
			if errors.Is(err, authz.ErrAccessDenied) {
				return editionpkg.AuthorizationError{
					Code:    editionpkg.DenialPermission,
					Message: "commercial API permission is required",
					Details: map[string]string{
						"featureKey": string(registration.FeatureKey),
						"permission": permission,
					},
				}
			}
			return fmt.Errorf("authorize commercial API permission %q: %w", permission, err)
		}
	}
	return nil
}

func editionModuleResource(
	r *http.Request,
	principal auth.Principal,
	registration editionpkg.APIModuleRegistration,
) (authz.Resource, error) {
	pathParameter := strings.TrimSpace(registration.ResourcePathParameter)
	pathValue := ""
	if pathParameter != "" {
		pathValue = strings.TrimSpace(r.PathValue(pathParameter))
		if pathValue == "" {
			return authz.Resource{}, fmt.Errorf(
				"commercial API operation %q is missing resource path parameter %q",
				registration.OperationID,
				pathParameter,
			)
		}
	}
	switch registration.ResourceScope {
	case editionpkg.APIResourceScopeOrganization:
		if pathValue == "" {
			pathValue = principal.OrganizationID
		}
		return authz.Resource{OrganizationID: pathValue}, nil
	case editionpkg.APIResourceScopeWorkspace:
		return authz.Resource{WorkspaceID: pathValue}, nil
	case editionpkg.APIResourceScopeProject:
		return authz.Resource{ProjectID: pathValue}, nil
	default:
		return authz.Resource{}, fmt.Errorf(
			"commercial API operation %q has invalid resource scope %q",
			registration.OperationID,
			registration.ResourceScope,
		)
	}
}

func mustRegisterEditionAPIRoute(mux *http.ServeMux, pattern string, handler http.Handler, operationID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panic(fmt.Sprintf(
				"register commercial API operation %q on %q: %v",
				operationID,
				pattern,
				recovered,
			))
		}
	}()
	mux.Handle(pattern, handler)
}
