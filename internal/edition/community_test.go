package edition

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommunityRuntimeCannotBeUnlockedByEditionEnvironment(t *testing.T) {
	for _, requested := range []string{"cloud", "enterprise"} {
		t.Run(requested, func(t *testing.T) {
			_, err := NewCommunityRuntime(CommunityOptions{RequestedEdition: requested})
			if err == nil {
				t.Fatalf("NewCommunityRuntime(%q) succeeded, want edition mismatch", requested)
			}
			var authorizationErr AuthorizationError
			if !errors.As(err, &authorizationErr) || authorizationErr.Code != DenialFeatureNotCompiled {
				t.Fatalf("error = %v, want %s", err, DenialFeatureNotCompiled)
			}
		})
	}
}

func TestCommunityManifestAndEntitlementsAreConsistent(t *testing.T) {
	evaluatedAt := time.Date(2026, 7, 29, 1, 2, 3, 0, time.FixedZone("test", 8*60*60))
	runtime, err := NewCommunityRuntime(CommunityOptions{
		CoreReleaseID: "core-release",
		Now:           func() time.Time { return evaluatedAt },
	})
	if err != nil {
		t.Fatalf("NewCommunityRuntime: %v", err)
	}
	status, err := runtime.SystemEdition(context.Background())
	if err != nil {
		t.Fatalf("SystemEdition: %v", err)
	}
	if status.DeploymentEdition != EditionCommunity || status.DistributionID != CommunityDistributionID {
		t.Fatalf("status = %+v", status)
	}
	if status.OperationalState.Mode != OperationalModeNormal || status.RestrictionReason != "" {
		t.Fatalf("operational state = %+v", status.OperationalState)
	}
	if len(status.CompiledFeatures) != len(communityCompiledFeatures) {
		t.Fatalf("compiled features = %v", status.CompiledFeatures)
	}

	snapshot, err := runtime.Entitlements.Evaluate(context.Background(), EntitlementRequest{
		Subject: EntitlementSubject{UserID: "user-1", OrganizationID: "org-1"},
		FeatureKeys: []FeatureKey{
			FeatureCoreWorkflow,
			FeatureBillingBalance,
			FeatureKey("unknown.feature"),
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !snapshot.EvaluatedAt.Equal(evaluatedAt.UTC()) {
		t.Fatalf("evaluatedAt = %v", snapshot.EvaluatedAt)
	}
	if len(snapshot.Decisions) != 3 {
		t.Fatalf("decisions = %+v", snapshot.Decisions)
	}
	if !snapshot.Decisions[0].Allowed || snapshot.Decisions[0].Reason != "" {
		t.Fatalf("core decision = %+v", snapshot.Decisions[0])
	}
	if snapshot.Decisions[1].Allowed || snapshot.Decisions[1].Reason != DenialFeatureNotCompiled {
		t.Fatalf("commercial decision = %+v", snapshot.Decisions[1])
	}
	if snapshot.Decisions[2].Allowed || snapshot.Decisions[2].Reason != DenialFeatureUnknown {
		t.Fatalf("unknown decision = %+v", snapshot.Decisions[2])
	}
}

func TestCommunityBillingRoutingRejectsCommercialContextAndSystemCredentials(t *testing.T) {
	runtime := MustCommunityRuntime()
	_, err := runtime.BillingRoutingAuthorizer.Authorize(context.Background(), BillingRoutingRequest{
		OrganizationID: "org-1",
		BillingContext: &BillingContextReference{ID: "billing-context", Revision: 1, SnapshotHash: "hash"},
	})
	var authorizationErr AuthorizationError
	if !errors.As(err, &authorizationErr) || authorizationErr.Code != DenialFeatureNotCompiled {
		t.Fatalf("commercial context error = %v, want %s", err, DenialFeatureNotCompiled)
	}

	decision, err := runtime.BillingRoutingAuthorizer.Authorize(context.Background(), BillingRoutingRequest{
		OrganizationID: "org-1",
		Candidates: []BillingRoutingCandidate{
			{CredentialID: "tenant-b", OrganizationID: "org-1", ManagementScope: ProviderManagementScopeTenant},
			{CredentialID: "system", OrganizationID: "org-1", ManagementScope: ProviderManagementScopeSystem},
			{CredentialID: "other-org", OrganizationID: "org-2", ManagementScope: ProviderManagementScopeTenant},
			{CredentialID: "tenant-a", OrganizationID: "org-1", ManagementScope: ProviderManagementScopeTenant},
		},
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	want := []string{"tenant-a", "tenant-b"}
	if len(decision.AllowedCredentialIDs) != len(want) {
		t.Fatalf("allowed = %v, want %v", decision.AllowedCredentialIDs, want)
	}
	for index := range want {
		if decision.AllowedCredentialIDs[index] != want[index] {
			t.Fatalf("allowed = %v, want %v", decision.AllowedCredentialIDs, want)
		}
	}
	if decision.AuditSnapshot.AllowedCandidateCount != 2 || decision.AuditSnapshot.CandidateSetHash == "" {
		t.Fatalf("audit snapshot = %+v", decision.AuditSnapshot)
	}
}

func TestEffectiveSpendAuthorizationKeepsRBACAndSponsorshipIndependent(t *testing.T) {
	validPersonal := EffectiveSpendAuthorizationFacts{
		ProjectOperationAllowedByRBAC:          true,
		BillingSpendAllowedByRBACForProject:    true,
		ActiveBindingMatchesContextRevision:    true,
		AccountScope:                           BillingAccountScopePersonal,
		AccountOwnerMatchesSponsorshipSponsor:  true,
		SponsorshipActiveForProjectAndRevision: true,
	}
	if err := EvaluateEffectiveSpendAuthorization(validPersonal); err != nil {
		t.Fatalf("valid personal authorization: %v", err)
	}

	sponsorshipWithoutSpend := validPersonal
	sponsorshipWithoutSpend.BillingSpendAllowedByRBACForProject = false
	assertAuthorizationDenial(t, EvaluateEffectiveSpendAuthorization(sponsorshipWithoutSpend), DenialPermission)

	spendWithoutSponsorship := validPersonal
	spendWithoutSponsorship.SponsorshipActiveForProjectAndRevision = false
	assertAuthorizationDenial(t, EvaluateEffectiveSpendAuthorization(spendWithoutSponsorship), DenialBillingSponsorshipRequired)

	validOrganization := EffectiveSpendAuthorizationFacts{
		ProjectOperationAllowedByRBAC:       true,
		BillingSpendAllowedByRBACForProject: true,
		ActiveBindingMatchesContextRevision: true,
		AccountScope:                        BillingAccountScopeOrganization,
		AccountAndProjectSameOrganization:   true,
	}
	if err := EvaluateEffectiveSpendAuthorization(validOrganization); err != nil {
		t.Fatalf("valid organization authorization: %v", err)
	}
}

func TestFeatureRegistryReturnsDefensiveCopies(t *testing.T) {
	registry := DefaultFeatureRegistry()
	first := registry.All()
	first[0].RequiredPermissions = append(first[0].RequiredPermissions, "mutated")
	second := registry.All()
	for _, permission := range second[0].RequiredPermissions {
		if permission == "mutated" {
			t.Fatalf("registry returned mutable descriptor: %+v", second[0])
		}
	}
}

func assertAuthorizationDenial(t *testing.T, err error, want DenialCode) {
	t.Helper()
	var authorizationErr AuthorizationError
	if !errors.As(err, &authorizationErr) || authorizationErr.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}
