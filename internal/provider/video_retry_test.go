package provider

import "testing"

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
	for _, code := range []string{CodeUpstreamTimeout, CodeUpstreamInternalError, CodeUpstreamOutputMismatch, CodeProviderRateLimited, CodeMediaDownloadFailed, "PROVIDER_VIDEO_POLLING_TIMEOUT"} {
		if !videoSegmentFailureRetryable(code) {
			t.Fatalf("%s should be retryable", code)
		}
	}
	for _, code := range []string{CodeInvalidRequest, CodeAuthFailed, CodeContentRejected} {
		if videoSegmentFailureRetryable(code) {
			t.Fatalf("%s should not be retried automatically", code)
		}
	}
}
