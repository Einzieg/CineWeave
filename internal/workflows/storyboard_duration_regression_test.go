package workflows

import (
	"encoding/json"
	"os"
	"testing"
)

type storyboardDurationRegressionFixture struct {
	Name                 string           `json:"name"`
	ScriptCharacterCount int              `json:"scriptCharacterCount"`
	ExpectedTotalSeconds float64          `json:"expectedTotalSeconds"`
	Shots                []StoryboardShot `json:"shots"`
}

func TestStoryboardDurationRegressionFixturePreservesLongShots(t *testing.T) {
	fixture := loadStoryboardDurationRegressionFixture(t)
	if fixture.ScriptCharacterCount != 6355 || len(fixture.Shots) != 16 {
		t.Fatalf("fixture dimensions = %d chars, %d shots", fixture.ScriptCharacterCount, len(fixture.Shots))
	}

	rawTotal := storyboardShotDurationTotal(fixture.Shots)
	if rawTotal != fixture.ExpectedTotalSeconds {
		t.Fatalf("raw duration = %.1f, want %.1f", rawTotal, fixture.ExpectedTotalSeconds)
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
