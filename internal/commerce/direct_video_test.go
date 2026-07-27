package commerce

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestBuildDirectVideoOptionsUsesExecutableVideoRoutes(t *testing.T) {
	production := directVideoTestProductionContext(t)
	options, err := BuildDirectVideoOptions(production)
	if err != nil {
		t.Fatalf("BuildDirectVideoOptions() error = %v", err)
	}
	if got, want := options.ExecutableDurationSeconds, []int{6, 10, 12, 16}; !equalDirectVideoInts(got, want) {
		t.Fatalf("durations = %v, want %v", got, want)
	}
	if options.DefaultResolution != "720p" {
		t.Fatalf("default resolution = %q, want 720p", options.DefaultResolution)
	}
	if len(options.Routes) != 2 {
		t.Fatalf("route count = %d, want 2", len(options.Routes))
	}
	if options.Routes[0].ProviderModelKey != "priority-model" {
		t.Fatalf("first route model = %q, want priority-model", options.Routes[0].ProviderModelKey)
	}
}

func TestBuildDirectVideoOptionsFallsBackToFirstFrameWithoutDeclaredImageContract(t *testing.T) {
	production := directVideoTestProductionContext(t)
	candidates := []map[string]any{
		directVideoTestCandidate("fallback-model", 200, 100, []int{6, 12}, "b"),
		directVideoTestCandidate("priority-model", 100, 50, []int{6, 10, 16}, "a"),
	}
	for _, candidate := range candidates {
		variants := candidate["videoGenerationVariants"].([]map[string]any)
		capability := variants[0]["capability"].(map[string]any)
		capability["inputContract"] = map[string]any{
			"contractKey": "text_only", "requestMode": "async_create", "slots": []any{},
		}
	}
	raw, err := json.Marshal(map[string]any{
		"videoGenerator": map[string]any{"candidates": candidates},
	})
	if err != nil {
		t.Fatal(err)
	}
	production.CommerceBinding.CapabilitySnapshot = raw

	options, err := BuildDirectVideoOptions(production)
	if err != nil {
		t.Fatalf("BuildDirectVideoOptions() error = %v", err)
	}
	if len(options.Routes) != 2 {
		t.Fatalf("route count = %d, want 2", len(options.Routes))
	}
	for _, route := range options.Routes {
		if route.InputContract.ContractKey != "first_frame" ||
			len(route.InputContract.Slots) != 1 ||
			route.InputContract.Slots[0].Role != "first_frame" {
			t.Fatalf("fallback input contract = %+v", route.InputContract)
		}
	}
}

func TestSelectDirectVideoRouteOnlyHardMatchesDurationAndResolution(t *testing.T) {
	options, err := BuildDirectVideoOptions(directVideoTestProductionContext(t))
	if err != nil {
		t.Fatalf("BuildDirectVideoOptions() error = %v", err)
	}
	route, err := SelectDirectVideoRoute(options, 10, "720p")
	if err != nil {
		t.Fatalf("SelectDirectVideoRoute() error = %v", err)
	}
	if route.ProviderModelKey != "priority-model" {
		t.Fatalf("selected model = %q, want priority-model", route.ProviderModelKey)
	}
	if _, err := SelectDirectVideoRoute(options, 8, "720p"); err == nil {
		t.Fatal("SelectDirectVideoRoute() accepted unsupported duration")
	}
	if _, err := SelectDirectVideoRoute(options, 10, "4k"); err == nil {
		t.Fatal("SelectDirectVideoRoute() accepted unsupported resolution")
	}
}

func TestAssignDirectVideoReferenceRolesUsesFirstFrameThenSemanticReferences(t *testing.T) {
	contract := DirectVideoInputContract{
		ContractKey: "first_frame_plus_references",
		RequestMode: "async",
		Slots: []DirectVideoInputSlot{
			{Role: "first_frame", MediaType: "image", Min: 1, Max: 1},
			{Role: "semantic_reference", MediaType: "image", Min: 0, Max: 3},
		},
	}
	roles, err := AssignDirectVideoReferenceRoles(contract, 6)
	if err != nil {
		t.Fatalf("AssignDirectVideoReferenceRoles() error = %v", err)
	}
	want := []string{"first_frame", "semantic_reference", "semantic_reference", "semantic_reference"}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for index := range want {
		if roles[index] != want[index] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
}

func TestPrioritizeDirectVideoReferencesUsesProductPrimaryAsFirstFrameCandidate(t *testing.T) {
	references := []DirectVideoReferenceSnapshot{
		{SourceType: "product", SourceID: "detail", Snapshot: json.RawMessage(`{"isPrimary":false}`)},
		{SourceType: "custom", SourceID: "custom"},
		{SourceType: "product", SourceID: "primary", Snapshot: json.RawMessage(`{"isPrimary":true}`)},
	}

	prioritizeDirectVideoReferences(references)

	if references[0].SourceID != "primary" {
		t.Fatalf("first reference = %q, want primary", references[0].SourceID)
	}
	if references[1].SourceID != "detail" || references[2].SourceID != "custom" {
		t.Fatalf("non-primary order changed: %#v", references)
	}
}

func TestDirectVideoReferenceSetHashIgnoresPreviewURLs(t *testing.T) {
	references := []DirectVideoReferenceSnapshot{{
		ID: "reference", SourceType: "product", SourceID: "source",
		ProductReferenceID: directStringPointer("source"),
		ArtifactID:         "artifact", MediaFileID: "media", StorageKey: "products/reference.png",
		MimeType: "image/png", ReferenceRole: "first_frame", Ordinal: 0,
		ContentHash: "content-hash", SourceRevision: 3,
		Snapshot:   json.RawMessage(`{"sourceType":"product","isPrimary":true}`),
		PreviewURL: "https://storage.example/first-signature",
	}}
	firstHash, err := DirectVideoReferenceSetHash(references)
	if err != nil {
		t.Fatal(err)
	}
	references[0].PreviewURL = "https://storage.example/refreshed-signature"
	secondHash, err := DirectVideoReferenceSetHash(references)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("reference hash changed with preview URL: %s != %s", firstHash, secondHash)
	}
}

func directVideoTestProductionContext(t *testing.T) ProductionContext {
	t.Helper()
	configuration, err := json.Marshal(map[string]any{
		"productionConfiguration": videoproduction.ProductionConfigurationSnapshot{
			SchemaVersion:    videoproduction.ProductionConfigurationSnapshotVersion,
			AspectRatio:      "9:16",
			VideoRatio:       "9:16",
			TimelineTimebase: 90000,
			FPSNumerator:     24,
			FPSDenominator:   1,
			Settings:         json.RawMessage(`{"videoResolution":"720p"}`),
		},
	})
	if err != nil {
		t.Fatalf("marshal configuration: %v", err)
	}
	capability, err := json.Marshal(map[string]any{
		"videoGenerator": map[string]any{
			"candidates": []map[string]any{
				directVideoTestCandidate("fallback-model", 200, 100, []int{6, 12}, "b"),
				directVideoTestCandidate("priority-model", 100, 50, []int{6, 10, 16}, "a"),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal capability snapshot: %v", err)
	}
	return ProductionContext{
		Generation: ProjectGenerationIdentity{ID: "generation", Status: "active"},
		VideoBinding: VideoBindingIdentity{
			ID: "video-binding", Revision: 1, Status: "active",
			ProfileVersionID: "profile-version", ProfileSnapshotHash: strings.Repeat("f", 64),
		},
		CommerceBinding: WorkflowBindingIdentity{
			ID: "commerce-binding", Revision: 1, Status: "active",
			ConfigurationSnapshot: configuration, CapabilitySnapshot: capability,
		},
	}
}

func directVideoTestCandidate(modelKey string, priority int, weight int, durations []int, hashChar string) map[string]any {
	return map[string]any{
		"modelProfileId":        "profile-" + modelKey,
		"modelProfileKey":       "video_generation_default",
		"modelProfileBindingId": "binding-" + modelKey,
		"providerModelId":       "provider-model-" + modelKey,
		"providerAccountId":     "provider-account-" + modelKey,
		"modelKey":              modelKey,
		"priority":              priority,
		"weight":                weight,
		"videoGenerationVariants": []map[string]any{{
			"variantKey":                "i2v",
			"capabilitySnapshotHash":    strings.Repeat(hashChar, 64),
			"executableDurationSeconds": durations,
			"resolutions":               []string{"720p"},
			"aspectRatios":              []string{"9:16"},
			"capability": map[string]any{
				"inputContract": map[string]any{
					"contractKey": "first_frame",
					"requestMode": "async",
					"slots": []map[string]any{{
						"role": "first_frame", "mediaType": "image",
						"semantics": "first_frame", "min": 1, "max": 1, "ordered": true,
					}},
				},
				"nativeAudio": map[string]any{"support": "true", "supportsVoiceover": true},
			},
		}},
	}
}

func equalDirectVideoInts(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
