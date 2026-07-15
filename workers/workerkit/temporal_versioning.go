package workerkit

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/Einzieg/cineweave/internal/observability"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

var (
	invalidDeploymentNameCharacters = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	validReleaseID                  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
)

var mutableProductionReleaseIDs = map[string]struct{}{
	"cineweave-dev": {},
	"dev":           {},
	"development":   {},
	"latest":        {},
	"local":         {},
	"local-dev":     {},
	"main":          {},
	"master":        {},
}

type temporalWorkerVersion struct {
	DeploymentName string
	BuildID        string
}

// TemporalWorkerOptions declares a Deployment Version. Routing changes are
// intentionally owned by cmd/temporal-release and never happen at worker boot.
func TemporalWorkerOptions(workerName string) (worker.Options, error) {
	if !environmentBool("TEMPORAL_WORKER_VERSIONING_ENABLED", true) {
		if isProductionEnvironment() {
			return worker.Options{}, fmt.Errorf("TEMPORAL_WORKER_VERSIONING_ENABLED must be true in production")
		}
		return worker.Options{}, nil
	}
	version, err := resolveTemporalWorkerVersion(workerName)
	if err != nil {
		return worker.Options{}, err
	}
	behavior, err := temporalWorkerVersioningBehavior()
	if err != nil {
		return worker.Options{}, err
	}
	behaviorLabel := "pinned"
	if behavior == workflow.VersioningBehaviorAutoUpgrade {
		behaviorLabel = "auto_upgrade"
	}
	observability.RecordTemporalWorkerDeployment(version.DeploymentName, version.BuildID, behaviorLabel)
	slog.Info("temporal worker deployment configured",
		"deploymentName", version.DeploymentName,
		"buildId", version.BuildID,
		"versioningMode", behaviorLabel,
	)
	return worker.Options{
		DeploymentOptions: worker.DeploymentOptions{
			UseVersioning: true,
			Version: worker.WorkerDeploymentVersion{
				DeploymentName: version.DeploymentName,
				BuildID:        version.BuildID,
			},
			DefaultVersioningBehavior: behavior,
		},
	}, nil
}

func RunTemporalWorker(temporalWorker worker.Worker, interrupt <-chan interface{}) error {
	if temporalWorker == nil {
		return fmt.Errorf("temporal worker is required")
	}
	return temporalWorker.Run(interrupt)
}

func resolveTemporalWorkerVersion(workerName string) (temporalWorkerVersion, error) {
	workerName = sanitizeTemporalDeploymentName(workerName)
	if workerName == "" {
		return temporalWorkerVersion{}, fmt.Errorf("worker name is required for worker versioning")
	}
	prefix := sanitizeTemporalDeploymentName(firstNonEmptyEnvironment("TEMPORAL_WORKER_DEPLOYMENT_PREFIX", "cineweave"))
	if prefix == "" {
		return temporalWorkerVersion{}, fmt.Errorf("TEMPORAL_WORKER_DEPLOYMENT_PREFIX must contain a letter or number")
	}

	releaseID := strings.TrimSpace(os.Getenv("CINEWEAVE_RELEASE_ID"))
	if releaseID == "" && !isProductionEnvironment() {
		releaseID = strings.TrimSpace(firstNonEmptyEnvironment("CINEWEAVE_BUILD_ID", "local-dev"))
	}
	if releaseID == "" {
		return temporalWorkerVersion{}, fmt.Errorf("CINEWEAVE_RELEASE_ID is required for production worker versioning")
	}
	if !validReleaseID.MatchString(releaseID) {
		return temporalWorkerVersion{}, fmt.Errorf("CINEWEAVE_RELEASE_ID must match %s", validReleaseID.String())
	}
	if isProductionEnvironment() {
		if _, mutable := mutableProductionReleaseIDs[strings.ToLower(releaseID)]; mutable {
			return temporalWorkerVersion{}, fmt.Errorf("CINEWEAVE_RELEASE_ID %q is mutable and cannot be used in production", releaseID)
		}
	}
	return temporalWorkerVersion{DeploymentName: prefix + "-" + workerName, BuildID: releaseID}, nil
}

func temporalWorkerVersioningBehavior() (workflow.VersioningBehavior, error) {
	switch strings.ToLower(strings.TrimSpace(firstNonEmptyEnvironment("TEMPORAL_WORKER_VERSIONING_BEHAVIOR", "pinned"))) {
	case "auto_upgrade", "autoupgrade", "auto-upgrade":
		return workflow.VersioningBehaviorAutoUpgrade, nil
	case "pinned":
		return workflow.VersioningBehaviorPinned, nil
	default:
		return workflow.VersioningBehaviorUnspecified, fmt.Errorf("TEMPORAL_WORKER_VERSIONING_BEHAVIOR must be auto_upgrade or pinned")
	}
}

func sanitizeTemporalDeploymentName(value string) string {
	value = invalidDeploymentNameCharacters.ReplaceAllString(strings.TrimSpace(value), "-")
	return strings.Trim(value, "-_")
}

func firstNonEmptyEnvironment(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func environmentBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func isProductionEnvironment() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CINEWEAVE_ENV")), "production")
}
