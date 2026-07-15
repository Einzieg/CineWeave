package audioquality

import "testing"

func TestReviewPassesExactChineseDialogue(t *testing.T) {
	metrics := Review(
		[]ExpectedLine{{Speaker: "方源", Text: "今日便到此为止。", StartTick: 0, EndTick: 180000}},
		"今日便到此为止。",
		[]TranscriptSegment{{Speaker: "方源", Text: "今日便到此为止。", Start: 0.05, End: 1.95}},
		90000,
	)
	if !metrics.Passed || metrics.DialogueCoverage != 1 || metrics.TextAccuracy != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestReviewRejectsWrongDialogue(t *testing.T) {
	metrics := Review(
		[]ExpectedLine{{Speaker: "方源", Text: "今日便到此为止。", StartTick: 0, EndTick: 180000}},
		"明天再来。",
		[]TranscriptSegment{{Text: "明天再来。", Start: 0, End: 1}},
		90000,
	)
	if metrics.Passed || metrics.DialogueCoverage >= 0.95 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestReviewRequiresDiarizationForMultipleSpeakers(t *testing.T) {
	metrics := Review(
		[]ExpectedLine{
			{Speaker: "甲", Text: "你来了。", StartTick: 0, EndTick: 90000},
			{Speaker: "乙", Text: "我来了。", StartTick: 90000, EndTick: 180000},
		},
		"你来了。我来了。",
		[]TranscriptSegment{{Text: "你来了。我来了。", Start: 0, End: 2}},
		90000,
	)
	if metrics.Passed || metrics.SpeakerTurnAccuracy != 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
}
