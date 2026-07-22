package workerkit

import (
	"strings"
	"testing"

	"go.temporal.io/sdk/workflow"
)

func TestEnvironmentBool(t *testing.T) {
	t.Setenv("TEMPORAL_WORKER_VERSIONING_ENABLED", "false")
	if environmentBool("TEMPORAL_WORKER_VERSIONING_ENABLED", true) {
		t.Fatal("versioning flag should be false")
	}
	t.Setenv("TEMPORAL_WORKER_VERSIONING_ENABLED", "true")
	if !environmentBool("TEMPORAL_WORKER_VERSIONING_ENABLED", false) {
		t.Fatal("versioning flag should be true")
	}
}

func TestResolveTemporalWorkerVersionUsesImmutableReleaseID(t *testing.T) {
	t.Setenv("CINEWEAVE_ENV", "production")
	t.Setenv("TEMPORAL_WORKER_DEPLOYMENT_PREFIX", "CineWeave.Release")
	t.Setenv("CINEWEAVE_RELEASE_ID", "20260714-a1b2c3d4.17")
	version, err := resolveTemporalWorkerVersion("script.worker")
	if err != nil {
		t.Fatalf("resolve version: %v", err)
	}
	if version.DeploymentName != "CineWeave-Release-script-worker" || version.BuildID != "20260714-a1b2c3d4.17" {
		t.Fatalf("version = %+v", version)
	}
}

func TestResolveTemporalWorkerVersionRejectsMissingOrMutableProductionRelease(t *testing.T) {
	t.Setenv("CINEWEAVE_ENV", "production")
	t.Setenv("CINEWEAVE_RELEASE_ID", "")
	t.Setenv("CINEWEAVE_BUILD_ID", "previous-build")
	if _, err := resolveTemporalWorkerVersion("script-worker"); err == nil || !strings.Contains(err.Error(), "CINEWEAVE_RELEASE_ID is required") {
		t.Fatalf("expected missing release error, got %v", err)
	}

	t.Setenv("CINEWEAVE_RELEASE_ID", "latest")
	if _, err := resolveTemporalWorkerVersion("script-worker"); err == nil || !strings.Contains(err.Error(), "mutable") {
		t.Fatalf("expected mutable release error, got %v", err)
	}
}

func TestResolveTemporalWorkerVersionAllowsDevelopmentFallback(t *testing.T) {
	t.Setenv("CINEWEAVE_ENV", "development")
	t.Setenv("CINEWEAVE_RELEASE_ID", "")
	t.Setenv("CINEWEAVE_BUILD_ID", "worktree-42")
	version, err := resolveTemporalWorkerVersion("media-worker")
	if err != nil {
		t.Fatalf("resolve development version: %v", err)
	}
	if version.BuildID != "worktree-42" {
		t.Fatalf("build id = %q", version.BuildID)
	}
}

func TestTemporalWorkerOptionsRejectsDisabledVersioningInProduction(t *testing.T) {
	t.Setenv("CINEWEAVE_ENV", "production")
	t.Setenv("TEMPORAL_WORKER_VERSIONING_ENABLED", "false")
	if _, err := TemporalWorkerOptions("script-worker"); err == nil {
		t.Fatal("production worker should reject disabled versioning")
	}
}

func TestTemporalWorkerVersioningBehavior(t *testing.T) {
	t.Setenv("TEMPORAL_WORKER_VERSIONING_BEHAVIOR", "auto_upgrade")
	behavior, err := temporalWorkerVersioningBehavior()
	if err != nil || behavior != workflow.VersioningBehaviorAutoUpgrade {
		t.Fatalf("auto upgrade behavior = %v, %v", behavior, err)
	}
	t.Setenv("TEMPORAL_WORKER_VERSIONING_BEHAVIOR", "pinned")
	behavior, err = temporalWorkerVersioningBehavior()
	if err != nil || behavior != workflow.VersioningBehaviorPinned {
		t.Fatalf("pinned behavior = %v, %v", behavior, err)
	}
	t.Setenv("TEMPORAL_WORKER_VERSIONING_BEHAVIOR", "invalid")
	if _, err := temporalWorkerVersioningBehavior(); err == nil {
		t.Fatal("invalid behavior should fail")
	}
}

func TestTemporalWorkerVersioningBehaviorDefaultsToPinned(t *testing.T) {
	t.Setenv("TEMPORAL_WORKER_VERSIONING_BEHAVIOR", "")
	behavior, err := temporalWorkerVersioningBehavior()
	if err != nil || behavior != workflow.VersioningBehaviorPinned {
		t.Fatalf("default behavior = %v, %v", behavior, err)
	}
}
