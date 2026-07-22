package api

import (
	"database/sql"
	"testing"
)

func TestMatchExistingSourceChapterUsesVolumeIdentityForRepeatedTitles(t *testing.T) {
	existing := []sourceChapterIdentityRecord{
		{ID: "chapter-volume-1", ChapterIndex: 1, VolumeIndex: sql.NullInt32{Int32: 1, Valid: true}, SectionIndex: sql.NullInt32{Int32: 1, Valid: true}, VolumeTitle: sql.NullString{String: "第一卷", Valid: true}, ChapterTitle: sql.NullString{String: "第一节", Valid: true}, Content: "old first volume"},
		{ID: "chapter-volume-2", ChapterIndex: 2, VolumeIndex: sql.NullInt32{Int32: 2, Valid: true}, SectionIndex: sql.NullInt32{Int32: 1, Valid: true}, VolumeTitle: sql.NullString{String: "第二卷", Valid: true}, ChapterTitle: sql.NullString{String: "第一节", Valid: true}, Content: "old second volume"},
	}
	volumeIndex, sectionIndex := 2, 1
	volumeTitle, chapterTitle := "第二卷", "第一节"
	request := sourceChapterRequest{
		VolumeIndex: &volumeIndex, SectionIndex: &sectionIndex,
		VolumeTitle: &volumeTitle, ChapterTitle: &chapterTitle,
		Content: "edited second volume",
	}
	if got := matchExistingSourceChapter(existing, map[string]bool{}, request, 2, request.Content); got != "chapter-volume-2" {
		t.Fatalf("matched chapter = %q, want chapter-volume-2", got)
	}
}

func TestMatchExistingSourceChapterPreservesIdentityAcrossReorderByExactContent(t *testing.T) {
	existing := []sourceChapterIdentityRecord{
		{ID: "chapter-a", ChapterIndex: 1, ChapterTitle: sql.NullString{String: "第一节", Valid: true}, Content: "content A"},
		{ID: "chapter-b", ChapterIndex: 2, ChapterTitle: sql.NullString{String: "第二节", Valid: true}, Content: "content B"},
	}
	chapterTitle := "第二节"
	request := sourceChapterRequest{ChapterTitle: &chapterTitle, Content: "content B"}
	if got := matchExistingSourceChapter(existing, map[string]bool{}, request, 1, request.Content); got != "chapter-b" {
		t.Fatalf("matched chapter = %q, want chapter-b after reorder", got)
	}
}

func TestMatchExistingSourceChapterDoesNotGuessAmbiguousRepeatedTitle(t *testing.T) {
	existing := []sourceChapterIdentityRecord{
		{ID: "chapter-a", ChapterIndex: 1, ChapterTitle: sql.NullString{String: "第一节", Valid: true}, Content: "content A"},
		{ID: "chapter-b", ChapterIndex: 2, ChapterTitle: sql.NullString{String: "第一节", Valid: true}, Content: "content B"},
	}
	chapterTitle := "第一节"
	request := sourceChapterRequest{ChapterTitle: &chapterTitle, Content: "new content"}
	if got := matchExistingSourceChapter(existing, map[string]bool{}, request, 3, request.Content); got != "" {
		t.Fatalf("matched chapter = %q, want no guess for ambiguous title", got)
	}
}
