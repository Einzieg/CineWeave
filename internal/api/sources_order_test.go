package api

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSortProjectSourcesKeepsNovelVolumesInReadingOrder(t *testing.T) {
	base := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
	items := []ProjectSource{
		{SourceType: "novel", Title: "04_第四卷：魔君纵横", FirstVolumeIndex: 4, CreatedAt: base.Add(4 * time.Minute)},
		{SourceType: "novel", Title: "03_第三卷：魔头乱世", FirstVolumeIndex: 3, CreatedAt: base.Add(3 * time.Minute)},
		{SourceType: "script", Title: "剧本", CreatedAt: base.Add(2 * time.Minute)},
		{SourceType: "novel", Title: "蛊真人", FirstVolumeIndex: 1, CreatedAt: base},
		{SourceType: "novel", Title: "02_第二卷：魔子出山", FirstVolumeIndex: 2, CreatedAt: base.Add(2 * time.Minute)},
	}

	sortProjectSources(items)

	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Title)
	}
	want := []string{
		"蛊真人",
		"02_第二卷：魔子出山",
		"03_第三卷：魔头乱世",
		"04_第四卷：魔君纵横",
		"剧本",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted titles = %#v, want %#v", got, want)
		}
	}
}

func TestSourceTitleSortRankSupportsChineseVolumeTitles(t *testing.T) {
	cases := map[string]int{
		"第一卷：魔性不改":    1,
		"第十二卷 后记":     12,
		"04_第四卷：魔君纵横": 4,
		"正文":          0,
	}
	for title, want := range cases {
		if got := sourceTitleSortRank(title); got != want {
			t.Fatalf("sourceTitleSortRank(%q) = %d, want %d", title, got, want)
		}
	}
}

func TestSourceActionPageCursorRoundTrip(t *testing.T) {
	encoded, err := encodeProjectControlOffsetCursor(37)
	if err != nil {
		t.Fatalf("encode source cursor: %v", err)
	}
	offset, err := decodeProjectControlOffsetCursor(encoded)
	if err != nil {
		t.Fatalf("decode source cursor: %v", err)
	}
	if offset != 37 {
		t.Fatalf("offset = %d, want 37", offset)
	}
	if _, err := decodeProjectControlOffsetCursor("not-a-cursor"); err == nil {
		t.Fatal("invalid source cursor was accepted")
	}
}

func TestSourceListAgentResultIncludesRevisionAndCursor(t *testing.T) {
	page := sourceListActionPage{
		Items: []sourceActionSummary{{
			ID: "source-1", Title: "第一卷", SourceType: "novel",
			Revision: 7, ContentRevision: 3, ContentHash: "hash",
		}},
		Limit: 20, NextCursor: "cursor-2",
	}
	result := sourceListAgentResult(map[string]any{"limit": 20}, page)
	if result.Status != "succeeded" || result.Data["nextCursor"] != "cursor-2" {
		t.Fatalf("result = %+v", result)
	}
	items, ok := result.Data["items"].([]sourceActionSummary)
	if !ok || len(items) != 1 || items[0].Revision != 7 || items[0].ContentRevision != 3 {
		t.Fatalf("items = %#v", result.Data["items"])
	}
}

func TestDecodeSourceCreateActionRequiresStagingForLargeContent(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"sourceType": "novel", "title": "长篇", "content": strings.Repeat("a", sourceCreateMaximumInlineBytes+1),
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	_, err = decodeSourceCreateActionInput(payload)
	if err == nil {
		t.Fatal("oversized source.create content was accepted")
	}
	var appErr apiError
	if !errors.As(err, &appErr) || appErr.Code != "CONTENT_STAGING_REQUIRED" {
		t.Fatalf("error = %v", err)
	}
}
