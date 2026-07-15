package audioquality

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

type ExpectedLine struct {
	Speaker   string
	Text      string
	StartTick int64
	EndTick   int64
}

type TranscriptSegment struct {
	Speaker string
	Text    string
	Start   float64
	End     float64
}

type Metrics struct {
	DialogueCoverage    float64 `json:"dialogueCoverage"`
	TextAccuracy        float64 `json:"textAccuracy"`
	TimingAccuracy      float64 `json:"timingAccuracy"`
	SpeakerTurnAccuracy float64 `json:"speakerTurnAccuracy"`
	Passed              bool    `json:"passed"`
}

func Review(expected []ExpectedLine, transcript string, segments []TranscriptSegment, timebase int64) Metrics {
	expectedText := strings.Builder{}
	for _, line := range expected {
		expectedText.WriteString(line.Text)
	}
	want := normalizeSpeechText(expectedText.String())
	got := normalizeSpeechText(transcript)
	coverage := sequenceCoverage(want, got)
	accuracy := textAccuracy(want, got)
	timing := timingAccuracy(expected, segments, timebase)
	speaker := speakerTurnAccuracy(expected, segments)
	passed := len(want) > 0 && coverage >= 0.95 && accuracy >= 0.90 && timing >= 0.75 && speaker >= 0.80
	return Metrics{
		DialogueCoverage: roundMetric(coverage), TextAccuracy: roundMetric(accuracy), TimingAccuracy: roundMetric(timing),
		SpeakerTurnAccuracy: roundMetric(speaker), Passed: passed,
	}
}

func normalizeSpeechText(value string) []rune {
	result := make([]rune, 0, len(value))
	for _, r := range strings.ToLower(value) {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			result = append(result, r)
		}
	}
	return result
}

func sequenceCoverage(expected, actual []rune) float64 {
	if len(expected) == 0 {
		return 1
	}
	return clamp01(float64(longestCommonSubsequence(expected, actual)) / float64(len(expected)))
}

func textAccuracy(expected, actual []rune) float64 {
	if len(expected) == 0 {
		if len(actual) == 0 {
			return 1
		}
		return 0
	}
	denominator := maxInt(len(expected), len(actual))
	if denominator == 0 {
		return 1
	}
	return clamp01(1 - float64(editDistance(expected, actual))/float64(denominator))
}

func timingAccuracy(expected []ExpectedLine, segments []TranscriptSegment, timebase int64) float64 {
	if len(expected) == 0 {
		return 1
	}
	if len(segments) == 0 || timebase <= 0 {
		return 0
	}
	wantStart, wantEnd := expected[0].StartTick, expected[0].EndTick
	for _, line := range expected[1:] {
		if line.StartTick < wantStart {
			wantStart = line.StartTick
		}
		if line.EndTick > wantEnd {
			wantEnd = line.EndTick
		}
	}
	ordered := append([]TranscriptSegment(nil), segments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	actualStart := int64(math.Round(ordered[0].Start * float64(timebase)))
	actualEnd := int64(math.Round(ordered[len(ordered)-1].End * float64(timebase)))
	wantDuration := wantEnd - wantStart
	if wantDuration <= 0 {
		return 0
	}
	delta := absInt64(actualStart-wantStart) + absInt64(actualEnd-wantEnd)
	return clamp01(1 - float64(delta)/float64(2*wantDuration))
}

func speakerTurnAccuracy(expected []ExpectedLine, segments []TranscriptSegment) float64 {
	want := compactSpeakers(expected)
	if len(want) <= 1 {
		return 1
	}
	got := compactTranscriptSpeakers(segments)
	if len(got) == 0 {
		return 0
	}
	wantRunes := []rune(strings.Join(want, "\x00"))
	gotRunes := []rune(strings.Join(got, "\x00"))
	return textAccuracy(wantRunes, gotRunes)
}

func compactSpeakers(lines []ExpectedLine) []string {
	result := make([]string, 0)
	for _, line := range lines {
		value := strings.ToLower(strings.TrimSpace(line.Speaker))
		if value == "" {
			continue
		}
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func compactTranscriptSpeakers(segments []TranscriptSegment) []string {
	result := make([]string, 0)
	for _, segment := range segments {
		value := strings.ToLower(strings.TrimSpace(segment.Speaker))
		if value == "" {
			continue
		}
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func longestCommonSubsequence(left, right []rune) int {
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for _, l := range left {
		for index, r := range right {
			if l == r {
				current[index+1] = previous[index] + 1
			} else {
				current[index+1] = maxInt(current[index], previous[index+1])
			}
		}
		previous, current = current, previous
		clear(current)
	}
	return previous[len(right)]
}

func editDistance(left, right []rune) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	current := make([]int, len(right)+1)
	for leftIndex, l := range left {
		current[0] = leftIndex + 1
		for rightIndex, r := range right {
			cost := 0
			if l != r {
				cost = 1
			}
			current[rightIndex+1] = minInt(previous[rightIndex+1]+1, current[rightIndex]+1, previous[rightIndex]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

func minInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func roundMetric(value float64) float64 {
	return math.Round(clamp01(value)*10000) / 10000
}
