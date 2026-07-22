package workflows

import "testing"

func TestNormalizeEpisodeVideoOutcomesRequiresExactDurableSuccess(t *testing.T) {
	snapshot := episodeVideoCheckpointSnapshot{Status: "running"}
	valid := episodeVideoDurableOutcome{
		ShotID: "shot-success", ItemID: "item-success", ItemStatus: "running", ItemAttempt: 1,
		IdentityVersion: 2, PlanID: "plan-success", PlanStatus: "succeeded", PlanIdentityMatches: true,
		PlanOutputArtifactID: "artifact-success", PlanOutputMediaFileID: "media-success",
		SegmentCount: 2, SucceededSegmentCount: 2,
		ProviderTaskCount: 2, SucceededProviderTaskCount: 2,
	}
	invalid := valid
	invalid.ShotID = "shot-invalid"
	invalid.ItemID = "item-invalid"
	invalid.PlanID = "plan-invalid"
	invalid.PlanOutputMediaFileID = ""

	outcomes := normalizeEpisodeVideoOutcomes(snapshot, []episodeVideoDurableOutcome{valid, invalid})
	if outcomes[0].Status != "succeeded" {
		t.Fatalf("valid outcome = %+v", outcomes[0])
	}
	if outcomes[1].Status != "failed" || outcomes[1].ErrorCode != episodeVideoIncompleteResultCode {
		t.Fatalf("invalid outcome = %+v", outcomes[1])
	}
}

func TestNormalizeEpisodeVideoOutcomesConservativelyClassifiesMissingTargets(t *testing.T) {
	failed := normalizeEpisodeVideoOutcomes(
		episodeVideoCheckpointSnapshot{Status: "failed"},
		[]episodeVideoDurableOutcome{{ShotID: "shot-missing"}},
	)
	if failed[0].Status != "failed" || failed[0].ErrorCode != episodeVideoMissingItemCode {
		t.Fatalf("failed missing target = %+v", failed[0])
	}
	cancelled := normalizeEpisodeVideoOutcomes(
		episodeVideoCheckpointSnapshot{Status: "cancelled"},
		[]episodeVideoDurableOutcome{{ShotID: "shot-missing"}},
	)
	if cancelled[0].Status != "cancelled" || cancelled[0].ErrorCode != "VIDEO_PRODUCTION_ITEM_CANCELLED" {
		t.Fatalf("cancelled missing target = %+v", cancelled[0])
	}
}

func TestSummarizeEpisodeVideoReconciliationHonorsTerminalUpperBound(t *testing.T) {
	mixed := summarizeEpisodeVideoReconciliation("running", []episodeVideoNormalizedOutcome{
		{ShotID: "shot-1", Status: "succeeded"},
		{ShotID: "shot-2", Status: "failed", Diagnostic: "missing_item"},
	})
	if mixed.Status != "partial_succeeded" || mixed.SucceededCount != 1 || mixed.FailedCount != 1 || mixed.DiagnosticCount != 1 {
		t.Fatalf("mixed summary = %+v", mixed)
	}
	cancelled := summarizeEpisodeVideoReconciliation("cancelled", []episodeVideoNormalizedOutcome{
		{ShotID: "shot-1", Status: "succeeded"},
		{ShotID: "shot-2", Status: "cancelled"},
	})
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled summary = %+v", cancelled)
	}
	failedAfterMedia := summarizeEpisodeVideoReconciliation("failed", []episodeVideoNormalizedOutcome{
		{ShotID: "shot-1", Status: "succeeded"},
	})
	if failedAfterMedia.Status != "partial_succeeded" {
		t.Fatalf("failed checkpoint with durable media = %+v", failedAfterMedia)
	}
}

func TestEpisodeVideoAggregateStatusRejectsEmptyWorkset(t *testing.T) {
	if status := episodeVideoAggregateStatus(0, 0, 0, 0); status != "failed" {
		t.Fatalf("empty workset status = %s", status)
	}
	if status := episodeVideoAggregateStatus(2, 2, 0, 0); status != "succeeded" {
		t.Fatalf("successful workset status = %s", status)
	}
	if status := episodeVideoAggregateStatus(2, 0, 0, 2); status != "cancelled" {
		t.Fatalf("cancelled workset status = %s", status)
	}
}
