package sources

import "testing"

func TestSplitNovelChaptersArabicChinese(t *testing.T) {
	chapters := SplitNovelChapters("第一章 初见\n她推开门。\n\n第二章 重逢\n雨停了。")
	assertChapterTitles(t, chapters, []string{"第一章 初见", "第二章 重逢"})
}

func TestSplitNovelChaptersSpacedArabic(t *testing.T) {
	chapters := SplitNovelChapters("第 1 章 开场\nA\n第 2 章 转折\nB")
	assertChapterTitles(t, chapters, []string{"第 1 章 开场", "第 2 章 转折"})
}

func TestSplitNovelChaptersChineseNumerals(t *testing.T) {
	chapters := SplitNovelChapters("第十二章 旧城\nA\n第二十三章 新城\nB")
	assertChapterTitles(t, chapters, []string{"第十二章 旧城", "第二十三章 新城"})
}

func TestSplitNovelChaptersMarkdown(t *testing.T) {
	chapters := SplitNovelChapters("# 第一章 初见\nA\n## 第二章 追问\nB")
	assertChapterTitles(t, chapters, []string{"第一章 初见", "第二章 追问"})
}

func TestSplitNovelChaptersFallback(t *testing.T) {
	chapters := SplitNovelChapters("没有章节标题。\n\n只有正文。")
	if len(chapters) != 1 || chapters[0].Title != "正文" || chapters[0].Content != "没有章节标题。\n\n只有正文。" {
		t.Fatalf("chapters = %+v", chapters)
	}
}

func TestSplitNovelChaptersVolumeAndChapter(t *testing.T) {
	chapters := SplitNovelChapters("第一卷 北境\n第一章 风雪\nA\n第二章 入城\nB")
	assertChapterTitles(t, chapters, []string{"第一章 风雪", "第二章 入城"})
	if chapters[0].VolumeTitle != "第一卷 北境" || chapters[1].VolumeTitle != "第一卷 北境" {
		t.Fatalf("volume titles = %+v", chapters)
	}
}

func assertChapterTitles(t *testing.T, chapters []ChapterDraft, want []string) {
	t.Helper()
	if len(chapters) != len(want) {
		t.Fatalf("len(chapters) = %d, want %d: %+v", len(chapters), len(want), chapters)
	}
	for i := range want {
		if chapters[i].Index != i+1 || chapters[i].Title != want[i] || chapters[i].Content == "" {
			t.Fatalf("chapter[%d] = %+v, want title %q with content", i, chapters[i], want[i])
		}
	}
}
