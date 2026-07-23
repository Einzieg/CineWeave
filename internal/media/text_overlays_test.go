package media

import (
	"strings"
	"testing"
)

func TestBuildASSDocumentPreservesUnicodeAndEscapesControlText(t *testing.T) {
	document := buildASSDocument(1080, 1920, []TextOverlay{{
		Text:         "限时优惠{立即下单}\\第二行\n马上购买",
		StartSeconds: 0.25,
		EndSeconds:   2.75,
		Position:     "center",
	}})
	for _, expected := range []string{"PlayResX: 1080", "Noto Sans CJK SC", "0:00:00.25", "0:00:02.75", "{\\an5}", "限时优惠\\{立即下单\\}\\\\第二行\\N马上购买"} {
		if !strings.Contains(document, expected) {
			t.Fatalf("ASS document does not contain %q:\n%s", expected, document)
		}
	}
}

func TestNormalizeTextOverlaysRejectsInvalidEntries(t *testing.T) {
	result := normalizeTextOverlays([]TextOverlay{
		{Text: "", StartSeconds: 0, EndSeconds: 1},
		{Text: "结束时间错误", StartSeconds: 2, EndSeconds: 1},
		{Text: "保留", StartSeconds: 0, EndSeconds: 1, Position: "unknown"},
	})
	if len(result) != 1 || result[0].Position != "bottom" {
		t.Fatalf("normalizeTextOverlays() = %#v", result)
	}
}
