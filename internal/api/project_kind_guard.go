package api

import (
	"net/http"
	"strings"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
)

var narrativeProjectRoutePrefixes = []string{
	"/adaptation-plans",
	"/asset-batches",
	"/assets",
	"/canonical-assets",
	"/character-voices",
	"/exports",
	"/final-videos",
	"/manual-bindings",
	"/novel-events",
	"/production/actions",
	"/production/status",
	"/regenerate",
	"/review-fixes",
	"/review-items",
	"/reviews",
	"/script-agent",
	"/script-episodes",
	"/script-scenes",
	"/scripts",
	"/shot-asset-requirements",
	"/shot-production",
	"/shot-videos",
	"/sources",
	"/storyboard-plans",
	"/storyboard-shots",
	"/timelines",
	"/video-prompts",
}

func projectRouteExpectedKind(path string) (commercepkg.ProjectKind, bool) {
	remainder, ok := projectRouteRemainder(path)
	if !ok || remainder == "" {
		return "", false
	}
	if remainder == "/commerce" || strings.HasPrefix(remainder, "/commerce/") {
		return commercepkg.ProjectKindCommerceVideo, true
	}
	if isSharedStoryboardRenderPlanRoute(remainder) {
		return "", false
	}
	for _, prefix := range narrativeProjectRoutePrefixes {
		if remainder == prefix || strings.HasPrefix(remainder, prefix+"/") {
			return commercepkg.ProjectKindNarrative, true
		}
	}
	return "", false
}

func isSharedStoryboardRenderPlanRoute(remainder string) bool {
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	return len(parts) >= 3 && parts[0] == "storyboard-shots" && parts[1] != "" && parts[2] == "render-plan"
}

func projectRouteRemainder(path string) (string, bool) {
	const prefix = "/api/projects/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	remainder := path[len(prefix):]
	separator := strings.IndexByte(remainder, '/')
	if separator < 0 {
		return "", true
	}
	return remainder[separator:], true
}

func (s *Server) enforceProjectRouteKind(w http.ResponseWriter, r *http.Request, project Project) bool {
	expected, guarded := projectRouteExpectedKind(r.URL.Path)
	if !guarded || project.ProjectKind == expected {
		return true
	}
	httpx.WriteError(w, r, http.StatusConflict, "PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作", map[string]any{
		"actualProjectKind":   project.ProjectKind,
		"expectedProjectKind": expected,
	}, false)
	return false
}
