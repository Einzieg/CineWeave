package commerce

import (
	"errors"
	"testing"
)

func TestResolveProjectClassification(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		projectType *string
		contentType *string
		want        ProjectClassification
		wantErr     bool
	}{
		{
			name: "narrative defaults",
			want: ProjectClassification{Kind: ProjectKindNarrative, ProjectType: ProjectTypeShortFilm, ContentType: pointer(ContentTypeScript)},
		},
		{
			name:        "narrative explicit",
			kind:        string(ProjectKindNarrative),
			projectType: pointer(ProjectTypeComicDrama),
			contentType: pointer(ContentTypeNovel),
			want:        ProjectClassification{Kind: ProjectKindNarrative, ProjectType: ProjectTypeComicDrama, ContentType: pointer(ContentTypeNovel)},
		},
		{
			name: "commerce derives storage fields",
			kind: string(ProjectKindCommerceVideo),
			want: ProjectClassification{Kind: ProjectKindCommerceVideo, ProjectType: ProjectTypeCommerce},
		},
		{name: "commerce rejects project type", kind: string(ProjectKindCommerceVideo), projectType: pointer(ProjectTypeCommerce), wantErr: true},
		{name: "commerce rejects content type", kind: string(ProjectKindCommerceVideo), contentType: pointer(ContentTypeScript), wantErr: true},
		{name: "rejects display labels", projectType: pointer("短片"), contentType: pointer("剧本创作"), wantErr: true},
		{name: "rejects unknown kind", kind: "commerce", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveProjectClassification(test.kind, test.projectType, test.contentType)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidProjectClassification) {
					t.Fatalf("ResolveProjectClassification() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProjectClassification() error = %v", err)
			}
			if got.Kind != test.want.Kind || got.ProjectType != test.want.ProjectType || !equalStringPointers(got.ContentType, test.want.ContentType) {
				t.Fatalf("ResolveProjectClassification() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func pointer(value string) *string {
	return &value
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
