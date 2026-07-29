package edition

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type testCommercialModuleRegistry struct {
	api        []APIModuleRegistration
	events     []EventConsumerRegistration
	background []BackgroundTaskRegistration
}

func (r testCommercialModuleRegistry) APIModules(context.Context) ([]APIModuleRegistration, error) {
	return r.api, nil
}

func (r testCommercialModuleRegistry) EventConsumers(context.Context) ([]EventConsumerRegistration, error) {
	return r.events, nil
}

func (r testCommercialModuleRegistry) BackgroundTasks(context.Context) ([]BackgroundTaskRegistration, error) {
	return r.background, nil
}

func TestCommercialAPIModuleRegistrationRequiresManifestEntitlementAndRBAC(t *testing.T) {
	manifest := testCommercialManifest()
	valid := testCommercialAPIRegistration()
	if err := validateModuleRegistry(
		t.Context(),
		manifest,
		testCommercialModuleRegistry{api: []APIModuleRegistration{valid}},
		DefaultFeatureRegistry(),
	); err != nil {
		t.Fatalf("valid commercial API registration: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*APIModuleRegistration)
	}{
		{
			name: "module is not in manifest",
			mutate: func(registration *APIModuleRegistration) {
				registration.ModuleKey = "undeclared"
			},
		},
		{
			name: "feature is not commercial",
			mutate: func(registration *APIModuleRegistration) {
				registration.FeatureKey = FeatureCoreWorkflow
			},
		},
		{
			name: "method is not normalized",
			mutate: func(registration *APIModuleRegistration) {
				registration.Method = "get"
			},
		},
		{
			name: "license operation is missing",
			mutate: func(registration *APIModuleRegistration) {
				registration.LicenseOperation = ""
			},
		},
		{
			name: "RBAC permission is missing",
			mutate: func(registration *APIModuleRegistration) {
				registration.RequiredPermissions = nil
			},
		},
		{
			name: "RBAC permission is outside feature",
			mutate: func(registration *APIModuleRegistration) {
				registration.RequiredPermissions = []string{"admin.manage"}
			},
		},
		{
			name: "project resource path parameter is missing",
			mutate: func(registration *APIModuleRegistration) {
				registration.Pattern = "/api/projects/billing-account"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registration := valid
			registration.RequiredPermissions = append([]string(nil), valid.RequiredPermissions...)
			test.mutate(&registration)
			err := validateModuleRegistry(
				t.Context(),
				manifest,
				testCommercialModuleRegistry{api: []APIModuleRegistration{registration}},
				DefaultFeatureRegistry(),
			)
			if err == nil {
				t.Fatal("invalid commercial API registration was accepted")
			}
		})
	}
}

func TestCommercialAPIModuleRegistrationRejectsRouteAndOperationCollisions(t *testing.T) {
	manifest := testCommercialManifest()
	first := testCommercialAPIRegistration()

	duplicateRoute := first
	duplicateRoute.OperationID = "getProjectBillingAccountAgain"
	err := validateModuleRegistry(
		t.Context(),
		manifest,
		testCommercialModuleRegistry{api: []APIModuleRegistration{first, duplicateRoute}},
		DefaultFeatureRegistry(),
	)
	if err == nil || !strings.Contains(err.Error(), "route") {
		t.Fatalf("duplicate route error = %v", err)
	}

	duplicateOperation := first
	duplicateOperation.Pattern = "/api/projects/{projectId}/billing-availability"
	err = validateModuleRegistry(
		t.Context(),
		manifest,
		testCommercialModuleRegistry{api: []APIModuleRegistration{first, duplicateOperation}},
		DefaultFeatureRegistry(),
	)
	if err == nil || !strings.Contains(err.Error(), "operationId") {
		t.Fatalf("duplicate operation error = %v", err)
	}
}

func TestManifestRejectsCommercialModuleForUncompiledFeature(t *testing.T) {
	manifest := testCommercialManifest()
	manifest.CompiledFeatures = []FeatureKey{FeatureCoreWorkflow}
	if err := validateManifest(manifest, DefaultFeatureRegistry()); err == nil {
		t.Fatal("manifest accepted a module for an uncompiled feature")
	}
}

func testCommercialManifest() Manifest {
	releaseID := "commercial-release"
	return Manifest{
		DeploymentEdition:   EditionCloud,
		DistributionID:      "cineweave-cloud",
		CoreReleaseID:       "core-release",
		CommercialReleaseID: &releaseID,
		ContractVersion:     ContractVersionV1,
		ContractHash:        strings.Repeat("a", 64),
		CompiledFeatures:    []FeatureKey{FeatureBillingBalance},
		CompiledModules: []CompiledModule{
			{
				Key:         "billing-api",
				FeatureKey:  FeatureBillingBalance,
				ContentHash: strings.Repeat("b", 64),
			},
		},
	}
}

func testCommercialAPIRegistration() APIModuleRegistration {
	return APIModuleRegistration{
		ModuleKey:             "billing-api",
		FeatureKey:            FeatureBillingBalance,
		Method:                http.MethodGet,
		Pattern:               "/api/projects/{projectId}/billing-account",
		OperationID:           "getProjectBillingAccount",
		LicenseOperation:      LicenseOperationReadOrExport,
		RequiredPermissions:   []string{"billing.read"},
		ResourceScope:         APIResourceScopeProject,
		ResourcePathParameter: "projectId",
		Handler: func(http.ResponseWriter, *http.Request, APIPrincipal) {
		},
	}
}
