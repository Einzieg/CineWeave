package api

import (
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
)

func TestProjectRouteExpectedKind(t *testing.T) {
	tests := []struct {
		path string
		kind commercepkg.ProjectKind
		ok   bool
	}{
		{path: "/api/projects/project-1/commerce/product", kind: commercepkg.ProjectKindCommerceVideo, ok: true},
		{path: "/api/projects/project-1/commerce", kind: commercepkg.ProjectKindCommerceVideo, ok: true},
		{path: "/api/projects/project-1/scripts/script-1", kind: commercepkg.ProjectKindNarrative, ok: true},
		{path: "/api/projects/project-1/storyboard-shots", kind: commercepkg.ProjectKindNarrative, ok: true},
		{path: "/api/projects/project-1/storyboard-shots/shot-1/render-plan", ok: false},
		{path: "/api/projects/project-1/storyboard-shots/shot-1/render-plan/audio-verification", ok: false},
		{path: "/api/projects/project-1", ok: false},
		{path: "/api/projects/project-1/agent/tasks", ok: false},
		{path: "/api/projects/project-1/agent/sessions", ok: false},
		{path: "/api/projects/project-1/agent/sessions/session-1/messages", ok: false},
		{path: "/api/projects/project-1/video-production-profile", ok: false},
		{path: "/api/workflow-runs/run-1", ok: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			kind, ok := projectRouteExpectedKind(test.path)
			if kind != test.kind || ok != test.ok {
				t.Fatalf("projectRouteExpectedKind(%q) = (%q, %t), want (%q, %t)", test.path, kind, ok, test.kind, test.ok)
			}
		})
	}
}
