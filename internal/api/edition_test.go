package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
)

type editionEnvelope[T any] struct {
	Data T `json:"data"`
}

func TestSystemEditionReturnsSafeCommunityManifest(t *testing.T) {
	handler := (&Server{}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/system/edition", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var envelope editionEnvelope[editionpkg.SystemEdition]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.DeploymentEdition != editionpkg.EditionCommunity ||
		envelope.Data.DistributionID != editionpkg.CommunityDistributionID ||
		envelope.Data.OperationalState.Mode != editionpkg.OperationalModeNormal {
		t.Fatalf("edition response = %+v", envelope.Data)
	}
	if envelope.Data.ContractVersion != editionpkg.ContractVersionV1 || envelope.Data.ContractHash == "" {
		t.Fatalf("contract identity = %+v", envelope.Data.Manifest)
	}
	if envelope.Data.CommercialReleaseID != nil || len(envelope.Data.CompiledModules) != 0 {
		t.Fatalf("community response exposed commercial release data: %+v", envelope.Data.Manifest)
	}
}

func TestMeEntitlementsUsesOrganizationScopedPrincipal(t *testing.T) {
	server := &Server{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/me/entitlements", nil)
	server.meEntitlements(response, request, auth.Principal{
		UserID:         "user-1",
		OrganizationID: "org-1",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var envelope editionEnvelope[editionpkg.EntitlementSnapshot]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Subject.UserID != "user-1" || envelope.Data.Subject.OrganizationID != "org-1" {
		t.Fatalf("subject = %+v", envelope.Data.Subject)
	}
	if envelope.Data.Edition != editionpkg.EditionCommunity || len(envelope.Data.Decisions) == 0 {
		t.Fatalf("snapshot = %+v", envelope.Data)
	}
	for _, decision := range envelope.Data.Decisions {
		if decision.FeatureKey == editionpkg.FeatureBillingBalance {
			if decision.Allowed || decision.Reason != editionpkg.DenialFeatureNotCompiled {
				t.Fatalf("billing balance decision = %+v", decision)
			}
			return
		}
	}
	t.Fatal("billing balance entitlement decision is missing")
}
