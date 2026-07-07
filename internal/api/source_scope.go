package api

import (
	"database/sql"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	sourceutil "github.com/Einzieg/cineweave/internal/sources"
)

type novelChapterScope struct {
	VolumeIndex  int
	SectionIndex int
}

type novelChapterScopeCandidate struct {
	ID            string
	SourceID      string
	SourceTitle   string
	ChapterIndex  int
	VolumeIndex   int
	SectionIndex  int
	VolumeTitle   string
	ChapterTitle  string
	SourceCreated time.Time
}

var (
	sourceScopeNumberPattern  = `[0-9０-９一二两三四五六七八九十百千万亿〇零壹贰叁肆伍陆柒捌玖拾佰仟]+`
	sourceScopeVolumePattern  = regexp.MustCompile(`(?i)(?:第\s*)?(` + sourceScopeNumberPattern + `)\s*(?:卷|部|篇)|(?:卷|部|篇)\s*(` + sourceScopeNumberPattern + `)`)
	sourceScopeSectionPattern = regexp.MustCompile(`(?i)(?:第\s*)?(` + sourceScopeNumberPattern + `)\s*(?:章|节|回|幕|集|话)|(?:chapter|scene|episode)\s+([0-9ivxlcdm]+)`)
)

func parseNovelChapterScope(text string) (novelChapterScope, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return novelChapterScope{}, false
	}
	scope := novelChapterScope{
		VolumeIndex:  parseVolumeOrdinalFromText(text),
		SectionIndex: parseSectionOrdinalFromText(text),
	}
	return scope, scope.VolumeIndex > 0 || scope.SectionIndex > 0
}

func parseVolumeOrdinalFromText(text string) int {
	return parseOrdinalFromPattern(sourceScopeVolumePattern, text)
}

func parseSectionOrdinalFromText(text string) int {
	return parseOrdinalFromPattern(sourceScopeSectionPattern, text)
}

func parseOrdinalFromPattern(pattern *regexp.Regexp, text string) int {
	match := pattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return 0
	}
	for _, value := range match[1:] {
		if parsed := sourceutil.ParseOrdinalNumber(value); parsed > 0 {
			return parsed
		}
	}
	return 0
}

func (s *Server) resolveNovelChapterScope(r *http.Request, projectID, explicitSourceID, scopeText string) (string, []string, bool, error) {
	scope, ok := parseNovelChapterScope(scopeText)
	if !ok {
		return "", nil, false, nil
	}
	explicitSourceID = strings.TrimSpace(explicitSourceID)
	rows, err := s.db.Query(r.Context(), `
		SELECT c.id::text, c.source_id::text, ps.title, c.chapter_index,
		       c.volume_index, c.section_index,
		       COALESCE(c.volume_title, ''), COALESCE(c.chapter_title, ''),
		       ps.created_at
		FROM novel_chapters c
		JOIN project_sources ps ON ps.id = c.source_id
		WHERE c.project_id = $1
		  AND ps.source_type = 'novel'
		  AND ($2 = '' OR c.source_id = $2::uuid)
		ORDER BY ps.created_at ASC, c.chapter_index ASC
	`, projectID, explicitSourceID)
	if err != nil {
		return "", nil, false, err
	}
	defer rows.Close()

	matches := make([]novelChapterScopeCandidate, 0)
	for rows.Next() {
		var item novelChapterScopeCandidate
		var volumeIndex, sectionIndex sql.NullInt32
		if err := rows.Scan(
			&item.ID,
			&item.SourceID,
			&item.SourceTitle,
			&item.ChapterIndex,
			&volumeIndex,
			&sectionIndex,
			&item.VolumeTitle,
			&item.ChapterTitle,
			&item.SourceCreated,
		); err != nil {
			return "", nil, true, err
		}
		item.VolumeIndex = intFromNullInt32(volumeIndex)
		item.SectionIndex = intFromNullInt32(sectionIndex)
		if item.VolumeIndex <= 0 {
			item.VolumeIndex = firstPositiveInt(parseVolumeOrdinalFromText(item.VolumeTitle), parseVolumeOrdinalFromText(item.SourceTitle))
		}
		if item.SectionIndex <= 0 {
			item.SectionIndex = firstPositiveInt(parseSectionOrdinalFromText(item.ChapterTitle), item.ChapterIndex)
		}
		if scope.VolumeIndex > 0 && item.VolumeIndex != scope.VolumeIndex {
			continue
		}
		if scope.SectionIndex > 0 && item.SectionIndex != scope.SectionIndex {
			continue
		}
		matches = append(matches, item)
	}
	if err := rows.Err(); err != nil {
		return "", nil, true, err
	}
	if len(matches) == 0 {
		return "", nil, true, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "未找到匹配的分卷分集")
	}

	sort.SliceStable(matches, func(i, j int) bool {
		left := matches[i]
		right := matches[j]
		if left.VolumeIndex != right.VolumeIndex {
			return ordinalSortValue(left.VolumeIndex) < ordinalSortValue(right.VolumeIndex)
		}
		if left.SectionIndex != right.SectionIndex {
			return ordinalSortValue(left.SectionIndex) < ordinalSortValue(right.SectionIndex)
		}
		if left.ChapterIndex != right.ChapterIndex {
			return left.ChapterIndex < right.ChapterIndex
		}
		if !left.SourceCreated.Equal(right.SourceCreated) {
			return left.SourceCreated.Before(right.SourceCreated)
		}
		return strings.ToLower(left.SourceTitle) < strings.ToLower(right.SourceTitle)
	})

	sourceID := matches[0].SourceID
	chapterIDs := make([]string, 0)
	for _, item := range matches {
		if item.SourceID != sourceID {
			continue
		}
		chapterIDs = append(chapterIDs, item.ID)
		if scope.SectionIndex > 0 {
			break
		}
	}
	return sourceID, chapterIDs, true, nil
}

func (s *Server) sourceIDForNovelChapters(r *http.Request, projectID string, chapterIDs []string) (string, error) {
	cleanIDs := make([]string, 0, len(chapterIDs))
	for _, id := range chapterIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			cleanIDs = append(cleanIDs, trimmed)
		}
	}
	if len(cleanIDs) == 0 {
		return "", nil
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT DISTINCT c.source_id::text
		FROM novel_chapters c
		JOIN project_sources ps ON ps.id = c.source_id
		WHERE c.project_id = $1
		  AND ps.source_type = 'novel'
		  AND c.id::text = ANY($2::text[])
	`, projectID, cleanIDs)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	sourceIDs := make([]string, 0, 1)
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			return "", err
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(sourceIDs) {
	case 0:
		return "", newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "chapterIds do not match any novel chapter")
	case 1:
		return sourceIDs[0], nil
	default:
		return "", newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "chapterIds must belong to the same source")
	}
}

func intFromNullInt32(value sql.NullInt32) int {
	if !value.Valid {
		return 0
	}
	return int(value.Int32)
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func ordinalSortValue(value int) int {
	if value > 0 {
		return value
	}
	return 1 << 30
}
