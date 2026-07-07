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
