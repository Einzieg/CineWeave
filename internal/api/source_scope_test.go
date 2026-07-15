package api

import "testing"

func TestParseNovelChapterScopeChineseCompact(t *testing.T) {
	scope, ok := parseNovelChapterScope("提取第一卷第一节")
	if !ok {
		t.Fatal("expected scope")
	}
	if scope.VolumeIndex != 1 || scope.SectionIndex != 1 {
		t.Fatalf("scope = %+v, want volume=1 section=1", scope)
	}
}

func TestParseNovelChapterScopeArabicSpaced(t *testing.T) {
	scope, ok := parseNovelChapterScope("只处理第 12 卷第 3 章")
	if !ok {
		t.Fatal("expected scope")
	}
	if scope.VolumeIndex != 12 || scope.SectionIndex != 3 {
		t.Fatalf("scope = %+v, want volume=12 section=3", scope)
	}
}

func TestParseNovelChapterScopeSectionOnly(t *testing.T) {
	scope, ok := parseNovelChapterScope("第一节")
	if !ok {
		t.Fatal("expected scope")
	}
	if scope.VolumeIndex != 0 || scope.SectionIndex != 1 {
		t.Fatalf("scope = %+v, want volume=0 section=1", scope)
	}
}

func TestParseNovelChapterRangeScopeFirstTenSections(t *testing.T) {
	scope, ok := parseNovelChapterRangeScope("改编第一卷前十节")
	if !ok {
		t.Fatal("expected range scope")
	}
	if scope.VolumeIndex != 1 || scope.StartSectionIndex != 1 || scope.EndSectionIndex != 10 || scope.Limit != 10 {
		t.Fatalf("scope = %+v, want volume=1 range=1-10 limit=10", scope)
	}
}

func TestParseNovelChapterRangeScopeCompactArabicRange(t *testing.T) {
	scope, ok := parseNovelChapterRangeScope("生成1-10集剧本")
	if !ok {
		t.Fatal("expected range scope")
	}
	if scope.VolumeIndex != 0 || scope.StartSectionIndex != 1 || scope.EndSectionIndex != 10 || scope.Limit != 10 {
		t.Fatalf("scope = %+v, want range=1-10 limit=10", scope)
	}
}

func TestParseNovelChapterRangeScopeChineseVolumeRange(t *testing.T) {
	scope, ok := parseNovelChapterRangeScope("只改编第一卷第三节到第五节")
	if !ok {
		t.Fatal("expected range scope")
	}
	if scope.VolumeIndex != 1 || scope.StartSectionIndex != 3 || scope.EndSectionIndex != 5 || scope.Limit != 3 {
		t.Fatalf("scope = %+v, want volume=1 range=3-5 limit=3", scope)
	}
}
