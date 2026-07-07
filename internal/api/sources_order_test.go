package api

import (
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
