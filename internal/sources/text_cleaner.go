package sources

import (
	"regexp"
	"strings"
)

var excessiveBlankLinesPattern = regexp.MustCompile(`\n{4,}`)

func CleanImportedText(input string) string {
	text := strings.TrimPrefix(input, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	text = excessiveBlankLinesPattern.ReplaceAllString(text, "\n\n\n")
	return text
}
