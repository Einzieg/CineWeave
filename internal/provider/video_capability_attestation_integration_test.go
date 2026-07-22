package provider

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
)

func TestVideoCapabilityAttestationLifecycleAndSnapshotFence(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video capability attestation integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video capability attestation integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	vault, err := NewVault("")
	if err != nil {
		t.Fatal(err)
	}
	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	organizationID, userID, _, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	if _, err := pool.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET provider_options_schema = $2::jsonb
		WHERE provider_model_id = $1
	`, modelID, mustJSON(map[string]any{"xCapabilities": map[string]any{
		"videoGenerationVariants": []map[string]any{{
			"variantKey": "single-frame-inferred", "modelFamily": "integration-video",
			"when":         map[string]any{"taskTypes": []string{"video.image_to_video"}, "referenceModes": []string{"first_frame"}},
			"duration":     map[string]any{"mode": "discrete", "values": []int{5, 10}},
			"frameRate":    map[string]any{"mode": "unknown"},
			"nativeAudio":  map[string]any{"support": "false"},
			"continuation": map[string]any{"supportsFirstFrame": true},
			"requestModes": []string{"async_create", "poll"},
			"source":       "inferred", "verificationStatus": "inferred", "capabilityVersion": "1",
		}},
	}})); err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, vault)
	listed, err := service.ListVideoCapabilityAttestations(ctx, organizationID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Variants) != 1 || listed.Variants[0].CurrentAttestation != nil {
		t.Fatalf("initial variants = %+v", listed.Variants)
	}
	variant := listed.Variants[0]
	approved, err := service.CreateVideoCapabilityAttestation(ctx, organizationID, userID, modelID, CreateVideoCapabilityAttestationRequest{
		VariantKey: variant.VariantKey, CapabilitySnapshotHash: variant.CapabilitySnapshotHash,
		Decision: "approved", Reason: "integration administrator approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, variants, _, err := service.currentVideoCapabilityVariants(ctx, organizationID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := service.resolveVideoCapabilityAttestation(ctx, organizationID, model.ID, variants[0], variant.CapabilitySnapshotHash); err != nil || resolved != approved.ID {
		t.Fatalf("resolved attestation = %s, err=%v", resolved, err)
	}
	if _, err := service.CreateVideoCapabilityAttestation(ctx, organizationID, userID, modelID, CreateVideoCapabilityAttestationRequest{
		VariantKey: variant.VariantKey, CapabilitySnapshotHash: variant.CapabilitySnapshotHash,
		Decision: "rejected", Reason: "conflicting decision",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active decision error = %v", err)
	}
	revoked, err := service.RevokeVideoCapabilityAttestation(ctx, organizationID, userID, modelID, approved.ID, RevokeVideoCapabilityAttestationRequest{
		CapabilitySnapshotHash: variant.CapabilitySnapshotHash, Reason: "replace with tested evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.RevokedAt == nil || revoked.Active {
		t.Fatalf("revoked attestation = %+v", revoked)
	}
	if _, err := service.resolveVideoCapabilityAttestation(ctx, organizationID, model.ID, variants[0], variant.CapabilitySnapshotHash); err == nil {
		t.Fatal("revoked inferred capability remained routable")
	}
	verified, err := service.VerifyVideoCapability(ctx, organizationID, userID, modelID, VerifyVideoCapabilityRequest{
		VariantKey: variant.VariantKey, CapabilitySnapshotHash: variant.CapabilitySnapshotHash,
		VerificationMode: "adapter_contract_test", Reason: "fixture passed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if verified.VerificationStatus != VideoCapabilityVerificationTested || verified.EvidenceType != "adapter_contract_test" {
		t.Fatalf("verified attestation = %+v", verified)
	}
	var attestedEvents, revokedEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE event_type = 'provider.model_capability.attested'),
		       count(*) FILTER (WHERE event_type = 'provider.model_capability.revoked')
		FROM event_outbox
		WHERE organization_id = $1 AND aggregate_id = $2
	`, organizationID, modelID).Scan(&attestedEvents, &revokedEvents); err != nil {
		t.Fatal(err)
	}
	if attestedEvents != 2 || revokedEvents != 1 {
		t.Fatalf("capability events = attested:%d revoked:%d", attestedEvents, revokedEvents)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET provider_options_schema = jsonb_set(provider_options_schema, '{xCapabilities,videoGenerationVariants,0,capabilityVersion}', '"2"')
		WHERE provider_model_id = $1
	`, modelID); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.ListVideoCapabilityAttestations(ctx, organizationID, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Variants[0].CapabilitySnapshotHash == variant.CapabilitySnapshotHash || refreshed.Attestations[0].CurrentSnapshot {
		t.Fatalf("changed capability did not invalidate old snapshot: %+v", refreshed)
	}
	if _, err := service.CreateVideoCapabilityAttestation(ctx, organizationID, userID, modelID, CreateVideoCapabilityAttestationRequest{
		VariantKey: variant.VariantKey, CapabilitySnapshotHash: variant.CapabilitySnapshotHash,
		Decision: "approved", Reason: "stale approval",
	}); err == nil {
		t.Fatal("stale capability snapshot was approved")
	}
}
