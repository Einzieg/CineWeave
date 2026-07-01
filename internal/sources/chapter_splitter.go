package sources

import (
	"regexp"
	"strings"
)

type ChapterDraft struct {
	Index       int
	VolumeTitle string
	Title       string
	Content     string
}

var (
	markdownHeadingPattern = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*$`)
	volumeTitlePattern     = regexp.MustCompile(`^\s*(?:第\s*)?[0-9一二两三四五六七八九十百千万〇零]+(?:\s*)卷(?:\s+.*)?$|^\s*卷\s*[0-9一二两三四五六七八九十百千万〇零]+(?:\s+.*)?$`)
	chapterTitlePattern    = regexp.MustCompile(`^\s*(?:第\s*)?[0-9一二两三四五六七八九十百千万〇零]+(?:\s*)章(?:\s+.*)?$|^\s*(序章|楔子|尾声|番外)(?:\s+.*)?$`)
)

func SplitNovelChapters(content string) []ChapterDraft {
	cleaned := CleanImportedText(content)
	if cleaned == "" {
		return []ChapterDraft{{Index: 1, Title: "正文"}}
	}

	var chapters []ChapterDraft
	currentVolume := ""
	currentTitle := ""
	currentLines := make([]string, 0)

	flush := func() {
		body := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if currentTitle == "" && body == "" {
			return
		}
		title := currentTitle
		if title == "" {
			title = "正文"
		}
		chapters = append(chapters, ChapterDraft{
			Index:       len(chapters) + 1,
			VolumeTitle: currentVolume,
			Title:       title,
			Content:     CleanImportedText(body),
		})
		currentLines = make([]string, 0)
	}

	for _, line := range strings.Split(cleaned, "\n") {
		title, isHeading := headingTitle(line)
		if isHeading {
			if isVolumeTitle(title) {
				flush()
				currentTitle = ""
				currentVolume = title
				continue
			}
			if isChapterTitle(title) {
				flush()
				currentTitle = title
				continue
			}
		}
		currentLines = append(currentLines, line)
	}
	flush()

	if len(chapters) == 0 {
		return []ChapterDraft{{
			Index:   1,
			Title:   "正文",
			Content: cleaned,
		}}
	}
	for i := range chapters {
		chapters[i].Index = i + 1
		if chapters[i].Content == "" {
			chapters[i].Content = chapters[i].Title
		}
	}
	return chapters
}

func headingTitle(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	if match := markdownHeadingPattern.FindStringSubmatch(trimmed); match != nil {
		return strings.TrimSpace(match[1]), true
	}
	return trimmed, isVolumeTitle(trimmed) || isChapterTitle(trimmed)
}

func isVolumeTitle(title string) bool {
	return volumeTitlePattern.MatchString(strings.TrimSpace(title))
}

func isChapterTitle(title string) bool {
	return chapterTitlePattern.MatchString(strings.TrimSpace(title))
}
