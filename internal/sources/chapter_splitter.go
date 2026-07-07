package sources

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ChapterDraft struct {
	Index        int
	VolumeIndex  int
	SectionIndex int
	VolumeTitle  string
	Title        string
	Content      string
}

var (
	markdownHeadingPattern = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*$`)
	headingNumberPattern   = `[0-9０-９一二两三四五六七八九十百千万亿〇零壹贰叁肆伍陆柒捌玖拾佰仟]+`
	headingSeparator       = `[\s　:：.．、,，;；\-—~～]+`
	volumeTitlePattern     = regexp.MustCompile(`(?i)^\s*(?:第\s*)?` + headingNumberPattern + `\s*(卷|部|篇)(?:$|` + headingSeparator + `.*)|^\s*(卷|部|篇)\s*` + headingNumberPattern + `(?:$|` + headingSeparator + `.*)|^\s*(part|book)\s+[ivxlcdm0-9]+(?:$|` + headingSeparator + `.*)`)
	chapterTitlePattern    = regexp.MustCompile(`(?i)^\s*(?:第\s*)?` + headingNumberPattern + `\s*(章|节|回|幕|集|话)(?:$|` + headingSeparator + `.*)|^\s*(chapter|scene|episode)\s+[0-9ivxlcdm]+(?:$|` + headingSeparator + `.*)|^\s*(序章|楔子|尾声|番外|引子|后记|终章)(?:$|` + headingSeparator + `.*)`)
	volumeOrdinalPattern   = regexp.MustCompile(`(?i)^\s*(?:第\s*)?(` + headingNumberPattern + `)\s*(?:卷|部|篇)|^\s*(?:卷|部|篇)\s*(` + headingNumberPattern + `)|^\s*(?:part|book)\s+([ivxlcdm0-9]+)`)
	chapterOrdinalPattern  = regexp.MustCompile(`(?i)^\s*(?:第\s*)?(` + headingNumberPattern + `)\s*(?:章|节|回|幕|集|话)|^\s*(?:chapter|scene|episode)\s+([0-9ivxlcdm]+)`)
)

const maxPlainHeadingRunes = 80

func SplitNovelChapters(content string) []ChapterDraft {
	cleaned := CleanImportedText(content)
	if cleaned == "" {
		return []ChapterDraft{{Index: 1, Title: "正文"}}
	}

	var chapters []ChapterDraft
	currentVolume := ""
	currentVolumeIndex := 0
	currentTitle := ""
	currentSectionIndex := 0
	currentLines := make([]string, 0)
	nextVolumeIndex := 0

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
			Index:        len(chapters) + 1,
			VolumeIndex:  currentVolumeIndex,
			SectionIndex: currentSectionIndex,
			VolumeTitle:  currentVolume,
			Title:        title,
			Content:      CleanImportedText(body),
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
				currentVolumeIndex = ordinalFromPattern(volumeOrdinalPattern, title)
				if currentVolumeIndex <= 0 {
					nextVolumeIndex++
					currentVolumeIndex = nextVolumeIndex
				} else if currentVolumeIndex > nextVolumeIndex {
					nextVolumeIndex = currentVolumeIndex
				}
				currentSectionIndex = 0
				continue
			}
			if isChapterTitle(title) {
				flush()
				currentTitle = title
				currentSectionIndex = ordinalFromPattern(chapterOrdinalPattern, title)
				if currentSectionIndex <= 0 {
					currentSectionIndex++
				}
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
		if chapters[i].SectionIndex <= 0 {
			chapters[i].SectionIndex = i + 1
		}
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
	if utf8.RuneCountInString(trimmed) > maxPlainHeadingRunes {
		return trimmed, false
	}
	return trimmed, isVolumeTitle(trimmed) || isChapterTitle(trimmed)
}

func isVolumeTitle(title string) bool {
	return volumeTitlePattern.MatchString(strings.ToLower(strings.TrimSpace(title)))
}

func isChapterTitle(title string) bool {
	return chapterTitlePattern.MatchString(strings.ToLower(strings.TrimSpace(title)))
}

func ordinalFromPattern(pattern *regexp.Regexp, title string) int {
	match := pattern.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return 0
	}
	for _, value := range match[1:] {
		if parsed := ParseOrdinalNumber(value); parsed > 0 {
			return parsed
		}
	}
	return 0
}

func ParseOrdinalNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	normalizedDigits := strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return '0' + (r - '０')
		}
		return r
	}, value)
	if parsed, err := strconv.Atoi(normalizedDigits); err == nil {
		return parsed
	}
	if parsed := parseRomanOrdinal(strings.ToUpper(normalizedDigits)); parsed > 0 {
		return parsed
	}
	return parseChineseOrdinal(normalizedDigits)
}

func parseRomanOrdinal(value string) int {
	if value == "" {
		return 0
	}
	values := map[rune]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	total := 0
	prev := 0
	for i := len(value) - 1; i >= 0; i-- {
		r := rune(value[i])
		current := values[r]
		if current == 0 {
			return 0
		}
		if current < prev {
			total -= current
		} else {
			total += current
			prev = current
		}
	}
	return total
}

func parseChineseOrdinal(value string) int {
	digit := map[rune]int{
		'零': 0, '〇': 0,
		'一': 1, '壹': 1,
		'二': 2, '两': 2, '贰': 2,
		'三': 3, '叁': 3,
		'四': 4, '肆': 4,
		'五': 5, '伍': 5,
		'六': 6, '陆': 6,
		'七': 7, '柒': 7,
		'八': 8, '捌': 8,
		'九': 9, '玖': 9,
	}
	unit := map[rune]int{'十': 10, '拾': 10, '百': 100, '佰': 100, '千': 1000, '仟': 1000, '万': 10000, '亿': 100000000}
	total := 0
	section := 0
	number := 0
	seen := false
	for _, r := range value {
		if n, ok := digit[r]; ok {
			number = n
			seen = true
			continue
		}
		u := unit[r]
		if u == 0 {
			return 0
		}
		seen = true
		if u == 10000 || u == 100000000 {
			section += number
			if section == 0 {
				section = 1
			}
			total += section * u
			section = 0
			number = 0
			continue
		}
		if number == 0 {
			number = 1
		}
		section += number * u
		number = 0
	}
	if !seen {
		return 0
	}
	return total + section + number
}
