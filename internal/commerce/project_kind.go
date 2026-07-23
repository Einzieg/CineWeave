package commerce

import (
	"errors"
	"fmt"
	"strings"
)

type ProjectKind string

const (
	ProjectKindNarrative     ProjectKind = "narrative"
	ProjectKindCommerceVideo ProjectKind = "commerce_video"
)

const (
	ProjectTypeShortFilm   = "short_film"
	ProjectTypeComicDrama  = "comic_drama"
	ProjectTypeBrandAd     = "brand_ad"
	ProjectTypeCharacterIP = "character_ip"
	ProjectTypeOther       = "other"
	ProjectTypeCommerce    = "commerce_video"
)

const (
	ContentTypeNovel           = "novel"
	ContentTypeScript          = "script"
	ContentTypeStoryboardFirst = "storyboard_first"
	ContentTypeOriginal        = "original"
)

var ErrInvalidProjectClassification = errors.New("invalid project classification")

type ProjectClassification struct {
	Kind        ProjectKind
	ProjectType string
	ContentType *string
}

func ResolveProjectClassification(kindValue string, projectType, contentType *string) (ProjectClassification, error) {
	kind, err := ParseProjectKind(kindValue)
	if err != nil {
		return ProjectClassification{}, err
	}
	if kind == ProjectKindCommerceVideo {
		if nonEmpty(projectType) || nonEmpty(contentType) {
			return ProjectClassification{}, fmt.Errorf("%w: commerce projects derive projectType and contentType", ErrInvalidProjectClassification)
		}
		return ProjectClassification{
			Kind:        kind,
			ProjectType: ProjectTypeCommerce,
			ContentType: nil,
		}, nil
	}

	resolvedProjectType := normalizedValue(projectType, ProjectTypeShortFilm)
	if !validNarrativeProjectType(resolvedProjectType) {
		return ProjectClassification{}, fmt.Errorf("%w: unsupported narrative projectType %q", ErrInvalidProjectClassification, resolvedProjectType)
	}
	resolvedContentType := normalizedValue(contentType, ContentTypeScript)
	if !validNarrativeContentType(resolvedContentType) {
		return ProjectClassification{}, fmt.Errorf("%w: unsupported narrative contentType %q", ErrInvalidProjectClassification, resolvedContentType)
	}
	return ProjectClassification{
		Kind:        kind,
		ProjectType: resolvedProjectType,
		ContentType: &resolvedContentType,
	}, nil
}

func ParseProjectKind(value string) (ProjectKind, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ProjectKindNarrative, nil
	}
	kind := ProjectKind(value)
	if kind != ProjectKindNarrative && kind != ProjectKindCommerceVideo {
		return "", fmt.Errorf("%w: unsupported projectKind %q", ErrInvalidProjectClassification, value)
	}
	return kind, nil
}

func (kind ProjectKind) IsCommerce() bool {
	return kind == ProjectKindCommerceVideo
}

func validNarrativeProjectType(value string) bool {
	switch value {
	case ProjectTypeShortFilm, ProjectTypeComicDrama, ProjectTypeBrandAd, ProjectTypeCharacterIP, ProjectTypeOther:
		return true
	default:
		return false
	}
}

func validNarrativeContentType(value string) bool {
	switch value {
	case ContentTypeNovel, ContentTypeScript, ContentTypeStoryboardFirst, ContentTypeOriginal:
		return true
	default:
		return false
	}
}

func normalizedValue(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func nonEmpty(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
