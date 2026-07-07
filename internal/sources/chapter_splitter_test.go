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
	if chapters[0].VolumeIndex != 1 || chapters[1].VolumeIndex != 1 || chapters[0].SectionIndex != 1 || chapters[1].SectionIndex != 2 {
		t.Fatalf("chapter ordinals = %+v", chapters)
	}
}

func TestSplitNovelChaptersCommonWebNovelHeadings(t *testing.T) {
	chapters := SplitNovelChapters("第一部 旧城\n第001章 雨夜\nA\n第二节 追踪\nB\n番外 灯火\nC")
	assertChapterTitles(t, chapters, []string{"第001章 雨夜", "第二节 追踪", "番外 灯火"})
	if chapters[0].VolumeTitle != "第一部 旧城" || chapters[2].VolumeTitle != "第一部 旧城" {
		t.Fatalf("volume titles = %+v", chapters)
	}
}

func TestSplitNovelChaptersChineseColonSectionHeadings(t *testing.T) {
	chapters := SplitNovelChapters("第一卷：魔性不改\n第一节：纵身亡魔心仍不悔\nA\n第二节：逆光阴五百年觉悟\nB")
	assertChapterTitles(t, chapters, []string{"第一节：纵身亡魔心仍不悔", "第二节：逆光阴五百年觉悟"})
	if chapters[0].VolumeTitle != "第一卷：魔性不改" || chapters[1].VolumeTitle != "第一卷：魔性不改" {
		t.Fatalf("volume titles = %+v", chapters)
	}
	if chapters[0].VolumeIndex != 1 || chapters[1].VolumeIndex != 1 || chapters[0].SectionIndex != 1 || chapters[1].SectionIndex != 2 {
		t.Fatalf("chapter ordinals = %+v", chapters)
	}
}

func TestSplitNovelChaptersDoesNotTreatClassTextAsSectionHeading(t *testing.T) {
	chapters := SplitNovelChapters("第一节课就是月光蛊的使用考核。\nA\n第二节课继续练习。\nB")
	if len(chapters) != 1 || chapters[0].Title != "正文" {
		t.Fatalf("chapters = %+v", chapters)
	}
}

func TestSplitNovelChaptersEnglishHeadings(t *testing.T) {
	chapters := SplitNovelChapters("Part I North\nChapter 1 Snow\nA\nScene 2 Gate\nB")
	assertChapterTitles(t, chapters, []string{"Chapter 1 Snow", "Scene 2 Gate"})
	if chapters[0].VolumeTitle != "Part I North" {
		t.Fatalf("volume title = %q", chapters[0].VolumeTitle)
	}
}

func TestSplitNovelChaptersEpisodes(t *testing.T) {
	chapters := SplitNovelChapters("第1集 雨夜\nA\n第2集 追踪\nB\nEpisode 3 Reveal\nC")
	assertChapterTitles(t, chapters, []string{"第1集 雨夜", "第2集 追踪", "Episode 3 Reveal"})
}

func TestParseOrdinalNumber(t *testing.T) {
	cases := map[string]int{
		"001":   1,
		"１２":    12,
		"IV":    4,
		"一百九十九": 199,
		"两千零三":  2003,
	}
	for value, want := range cases {
		if got := ParseOrdinalNumber(value); got != want {
			t.Fatalf("ParseOrdinalNumber(%q) = %d, want %d", value, got, want)
		}
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
