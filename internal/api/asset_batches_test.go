package api

import (
	"context"
	"errors"
	"testing"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestBuildAssetBatchSnapshotRejectsMissingProductionContext(t *testing.T) {
	_, err := (&Server{}).buildAssetBatchSnapshot(
		context.Background(),
		nil,
		Project{ID: "project-1", OrganizationID: "organization-1"},
		createAssetBatchRequest{Operation: "generate_prompts", AssetIDs: []string{"asset-1"}},
		1,
		"",
	)
	if err == nil {
		t.Fatal("expected missing production context error")
	}
	var typed videoproduction.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want videoproduction.Error", err)
	}
	if typed.Code != videoproduction.CodeGenerationMismatch {
		t.Fatalf("error code = %q, want %q", typed.Code, videoproduction.CodeGenerationMismatch)
	}
}
