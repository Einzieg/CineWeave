package workflows

import (
	"encoding/json"
	"os"
	"testing"
)

type storyboardDurationRegressionFixture struct {
	Name                     string           `json:"name"`
	ScriptCharacterCount     int              `json:"scriptCharacterCount"`
	RawTotalSeconds          float64          `json:"rawTotalSeconds"`
	LegacyMaxShotSeconds     float64          `json:"legacyMaxShotSeconds"`
	LegacyStoredTotalSeconds float64          `json:"legacyStoredTotalSeconds"`
	Shots                    []StoryboardShot `json:"shots"`
}

func TestStoryboardDurationRegressionFixtureDocumentsLegacyClamp(t *testing.T) {
	fixture := loadStoryboardDurationRegressionFixture(t)
	if fixture.ScriptCharacterCount != 6355 || len(fixture.Shots) != 16 {
		t.Fatalf("fixture dimensions = %d chars, %d shots", fixture.ScriptCharacterCount, len(fixture.Shots))
	}

	rawTotal := storyboardShotDurationTotal(fixture.Shots)
	if rawTotal != fixture.RawTotalSeconds {
		t.Fatalf("raw duration = %.1f, want %.1f", rawTotal, fixture.RawTotalSeconds)
	}

	legacyStoredTotal := legacyClampedStoryboardDuration(fixture.Shots, fixture.LegacyMaxShotSeconds)
	if legacyStoredTotal != fixture.LegacyStoredTotalSeconds {
		t.Fatalf("legacy stored duration = %.1f, want %.1f", legacyStoredTotal, fixture.LegacyStoredTotalSeconds)
	}
	if loss := rawTotal - legacyStoredTotal; loss != 170 {
		t.Fatalf("legacy duration loss = %.1f, want 170", loss)
	}

	normalized := NormalizeStoryboardShots(fixture.Shots, "fallback")
	metrics := newStoryboardDurationMetrics(fixture.Shots, normalized)
	if got := metrics.PlannedDurationSeconds; got != rawTotal {
		t.Fatalf("current normalized duration = %.1f, want lossless %.1f", got, rawTotal)
	}
	if metrics.RawDurationSeconds != 410 || metrics.DurationLossSeconds != 0 {
		t.Fatalf("pre-storage metrics = %#v", metrics)
	}
	stored := make([]StoryboardShotRecord, 0, len(normalized))
	for _, shot := range normalized {
		stored = append(stored, StoryboardShotRecord{Duration: shot.Duration})
	}
	metrics.recordStored(stored)
	if metrics.StoredDurationSeconds != 410 || metrics.DurationLossSeconds != 0 {
		t.Fatalf("stored metrics = %#v", metrics)
	}
}

func loadStoryboardDurationRegressionFixture(t *testing.T) storyboardDurationRegressionFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/storyboard_duration_regression.json")
	if err != nil {
		t.Fatalf("read regression fixture: %v", err)
	}
	var fixture storyboardDurationRegressionFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode regression fixture: %v", err)
	}
	return fixture
}

func legacyClampedStoryboardDuration(shots []StoryboardShot, maxDuration float64) float64 {
	total := 0.0
	for _, shot := range shots {
		duration := shot.Duration
		if duration > maxDuration {
			duration = maxDuration
		}
		total += duration
	}
	return total
}
