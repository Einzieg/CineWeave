package provider

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/events"
)

func TestSameVideoCapabilityFamilyRequiresFamilyVariantAndHash(t *testing.T) {
	base := videoFallbackCandidate{ModelFamily: "veo", VariantKey: "native-720p", CapabilitySnapshotHash: "sha256:a"}
	if !sameVideoCapabilityFamily(base, "veo", "native-720p", "sha256:a") {
		t.Fatal("identical capability family should preserve successful sibling segments")
	}
	for name, candidate := range map[string]videoFallbackCandidate{
		"family":  {ModelFamily: "sora", VariantKey: "native-720p", CapabilitySnapshotHash: "sha256:a"},
		"variant": {ModelFamily: "veo", VariantKey: "silent-720p", CapabilitySnapshotHash: "sha256:a"},
		"hash":    {ModelFamily: "veo", VariantKey: "native-720p", CapabilitySnapshotHash: "sha256:b"},
	} {
		t.Run(name, func(t *testing.T) {
			if sameVideoCapabilityFamily(candidate, "veo", "native-720p", "sha256:a") {
				t.Fatal("capability mismatch must require a whole-shot render plan revision")
			}
		})
	}
}

func TestVideoSegmentFailureRetryability(t *testing.T) {
	for _, code := range []string{CodeUpstreamTimeout, CodeUpstreamInternalError, CodeUpstreamStreamTruncated, CodeUpstreamOutputMismatch, CodeProviderRateLimited, CodeMediaDownloadFailed, "PROVIDER_VIDEO_POLLING_TIMEOUT"} {
		if !VideoSegmentFailureRetryable(code) {
			t.Fatalf("%s should be retryable", code)
		}
	}
	for _, code := range []string{CodeInvalidRequest, CodeAuthFailed, CodeContentRejected} {
		if VideoSegmentFailureRetryable(code) {
			t.Fatalf("%s should not be retried automatically", code)
		}
	}
}

func TestSelectVideoRetryCandidateUsesBoundedFinalAttemptForSingleModel(t *testing.T) {
	current := videoFallbackCandidate{
		ProviderModelID: "model-a", ModelFamily: "grok", VariantKey: "variant-1", CapabilitySnapshotHash: "sha256:a",
	}
	active := func(videoFallbackCandidate) bool { return true }
	attempted := map[string]bool{"model-a": true}

	selected := selectVideoRetryCandidate([]videoFallbackCandidate{current}, attempted, "model-a", 1, "grok", "variant-1", "sha256:a", active)
	if selected.ProviderModelID != "model-a" {
		t.Fatalf("selected = %+v, want final bounded retry on model-a", selected)
	}
	selected = selectVideoRetryCandidate([]videoFallbackCandidate{current}, attempted, "model-a", 2, "grok", "variant-1", "sha256:a", active)
	if selected.ProviderModelID != "" {
		t.Fatalf("selected = %+v, want retry budget exhausted", selected)
	}
}

func TestSelectVideoRetryCandidatePrefersUnusedCompatibleFallback(t *testing.T) {
	current := videoFallbackCandidate{
		ProviderModelID: "model-a", ModelFamily: "grok", VariantKey: "variant-1", CapabilitySnapshotHash: "sha256:a",
	}
	fallback := videoFallbackCandidate{
		ProviderModelID: "model-b", ModelFamily: "grok", VariantKey: "variant-1", CapabilitySnapshotHash: "sha256:a",
	}
	selected := selectVideoRetryCandidate(
		[]videoFallbackCandidate{current, fallback},
		map[string]bool{"model-a": true},
		"model-a",
		1,
		"grok",
		"variant-1",
		"sha256:a",
		func(videoFallbackCandidate) bool { return true },
	)
	if selected.ProviderModelID != "model-b" {
		t.Fatalf("selected = %+v, want unused compatible fallback", selected)
	}
}

func TestVideoRenderSegmentEventsAreRegistered(t *testing.T) {
	for _, status := range []string{"planned", "retry_planned", "queued", "running", "succeeded", "failed", "cancelled"} {
		eventType, err := videoRenderSegmentEventType(status)
		if err != nil {
			t.Fatalf("videoRenderSegmentEventType(%q): %v", status, err)
		}
		definition, ok := events.DefinitionFor(eventType)
		if !ok {
			t.Fatalf("video render segment event %q is not registered", eventType)
		}
		if definition.AggregateType != "video_render_segment" {
			t.Fatalf("event %q aggregate = %q", eventType, definition.AggregateType)
		}
	}
}
