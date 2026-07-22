package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

const (
	profileLoadEpisodeCount        = 10
	profileLoadShotsPerEpisode     = 24
	profileLoadLongEpisodeMinutes  = 70
	profileLoadLongShotSeconds     = 5
	profileLoadMaximumTestDuration = 30 * time.Second
)

func TestAvailableVideoProfilesSustainTenEpisodeAndSeventyMinuteLoad(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video profile load integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video profile load integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	versions, err := videoproduction.ListProfiles(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	latest := map[string]videoproduction.ProfileVersion{}
	for _, version := range versions {
		if !version.Available() {
			continue
		}
		if current, ok := latest[version.ProfileKey]; !ok || version.Version > current.Version {
			latest[version.ProfileKey] = version
		}
	}
	expectedProfiles := []string{
		videoproduction.ProfileSingleFrameI2V,
		videoproduction.ProfileFirstLastFrame,
		videoproduction.ProfileMultimodalReference,
		videoproduction.ProfileStoryboardSheet,
	}
	if len(latest) != len(expectedProfiles) {
		t.Fatalf("available profile families = %d, want %d: %+v", len(latest), len(expectedProfiles), latest)
	}

	startedAt := time.Now()
	compiler := videoproduction.NewProfileCompiler()
	longEpisodeShots := profileLoadLongEpisodeMinutes * 60 / profileLoadLongShotSeconds
	totalShots := 0
	for _, profileKey := range expectedProfiles {
		version, ok := latest[profileKey]
		if !ok {
			t.Fatalf("missing available profile %s", profileKey)
		}
		compiled, compileErr := compiler.Compile(version, true)
		if compileErr != nil {
			t.Fatalf("compile %s v%d: %v", profileKey, version.Version, compileErr)
		}
		profileStartedAt := time.Now()
		ordinal := 0
		for episode := 0; episode < profileLoadEpisodeCount; episode++ {
			for shot := 0; shot < profileLoadShotsPerEpisode; shot++ {
				exerciseCompiledProfileShot(t, compiled, ordinal)
				ordinal++
				totalShots++
			}
		}
		for shot := 0; shot < longEpisodeShots; shot++ {
			exerciseCompiledProfileShot(t, compiled, ordinal)
			ordinal++
			totalShots++
		}
		t.Logf("profile=%s version=%d regularEpisodes=%d longEpisodeMinutes=%d shots=%d duration=%s",
			profileKey, version.Version, profileLoadEpisodeCount, profileLoadLongEpisodeMinutes, ordinal, time.Since(profileStartedAt))
	}
	if totalShots != len(expectedProfiles)*(profileLoadEpisodeCount*profileLoadShotsPerEpisode+longEpisodeShots) {
		t.Fatalf("processed shots = %d", totalShots)
	}
	if elapsed := time.Since(startedAt); elapsed > profileLoadMaximumTestDuration {
		t.Fatalf("profile contract load took %s, limit %s", elapsed, profileLoadMaximumTestDuration)
	}
}

func exerciseCompiledProfileShot(t *testing.T, compiled videoproduction.CompiledProfile, ordinal int) {
	t.Helper()
	anchorCounts := map[string]int{}
	for _, requirement := range compiled.AnchorRequirements {
		anchorCounts[requirement.Role] = requirement.Minimum
	}
	if err := compiled.Strategy.Anchors().ValidateReadyAnchors(anchorCounts); err != nil {
		t.Fatalf("%s anchors at shot %d: %v", compiled.Version.ProfileKey, ordinal, err)
	}

	candidates, requiredAssets := profileLoadReferenceCandidates(compiled.Version.ProfileKey, ordinal)
	pack, err := videoproduction.ResolveReferencePack(videoproduction.ReferenceResolveInput{
		ProfileKey: compiled.Version.ProfileKey, Purpose: videoproduction.ReferencePurposeVideo,
		ShotStateRevision:      ordinal + 1,
		ProfileSnapshotHash:    profileLoadHash(compiled.Version.ProfileKey, compiled.Version.Version, ordinal, "profile"),
		ShotStateHash:          profileLoadHash(compiled.Version.ProfileKey, compiled.Version.Version, ordinal, "state"),
		CapabilitySnapshotHash: profileLoadHash(compiled.Version.ProfileKey, compiled.Version.Version, ordinal, "capability"),
		RequiredAssetIDs:       requiredAssets,
		MaxReferences:          8, MaxImageReferences: 8, MaxVideoReferences: 2, MaxAudioReferences: 2,
		Candidates: candidates,
	})
	if err != nil {
		t.Fatalf("%s reference pack at shot %d: %v", compiled.Version.ProfileKey, ordinal, err)
	}
	if err := compiled.Strategy.InputAdapter().ValidateReferenceRoles(pack.Manifest.Items); err != nil {
		t.Fatalf("%s input contract at shot %d: %v", compiled.Version.ProfileKey, ordinal, err)
	}
	if pack.ManifestHash == "" || len(pack.Manifest.Items) == 0 {
		t.Fatalf("%s produced empty reference provenance at shot %d", compiled.Version.ProfileKey, ordinal)
	}

	dialogue := fmt.Sprintf("第%d镜继续前进", ordinal+1)
	cues := []videoproduction.DialogueCue{{Speaker: "角色甲", Text: dialogue, Kind: "dialogue"}}
	imageReview := compiled.Strategy.Prompts().ReviewImage("角色甲在场景中转身，电影构图，清晰首帧", cues)
	if !imageReview.Approved {
		t.Fatalf("%s image prompt rejected at shot %d: %+v", compiled.Version.ProfileKey, ordinal, imageReview.Issues)
	}
	videoReview := compiled.Strategy.Prompts().ReviewVideo("角色甲转身，并用中文逐字说："+dialogue, cues, true, true)
	if !videoReview.Approved {
		t.Fatalf("%s video prompt rejected at shot %d: %+v", compiled.Version.ProfileKey, ordinal, videoReview.Issues)
	}
}

func profileLoadReferenceCandidates(profileKey string, ordinal int) ([]videoproduction.ReferenceCandidate, []string) {
	makeCandidate := func(role, assetID string, required bool, priority int) videoproduction.ReferenceCandidate {
		return videoproduction.ReferenceCandidate{
			ReferenceKey: fmt.Sprintf("%s:%d:%s", profileKey, ordinal, role), Role: role,
			Required: required, Priority: priority, SourceType: "load_fixture", AssetID: assetID,
			ContentHash: profileLoadHash(profileKey, 0, ordinal, role), Active: true, Fresh: true,
		}
	}
	switch profileKey {
	case videoproduction.ProfileSingleFrameI2V:
		return []videoproduction.ReferenceCandidate{makeCandidate(videoproduction.ReferenceRoleFirstFrame, "", true, 1000)}, nil
	case videoproduction.ProfileFirstLastFrame:
		return []videoproduction.ReferenceCandidate{
			makeCandidate(videoproduction.ReferenceRoleFirstFrame, "", true, 1000),
			makeCandidate(videoproduction.ReferenceRoleLastFrame, "", true, 900),
		}, nil
	case videoproduction.ProfileMultimodalReference:
		return []videoproduction.ReferenceCandidate{
			makeCandidate(videoproduction.ReferenceRoleFirstFrame, "", true, 1000),
			makeCandidate(videoproduction.ReferenceRoleCharacterIdentity, "character", true, 900),
			makeCandidate(videoproduction.ReferenceRoleSceneIdentity, "scene", true, 800),
			makeCandidate(videoproduction.ReferenceRolePropIdentity, "prop", true, 700),
		}, []string{"character", "scene", "prop"}
	case videoproduction.ProfileStoryboardSheet:
		return []videoproduction.ReferenceCandidate{makeCandidate(videoproduction.ReferenceRoleStoryboardSheet, "", true, 1000)}, nil
	default:
		return nil, nil
	}
}

func profileLoadHash(profileKey string, version, ordinal int, field string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s", profileKey, version, ordinal, field)))
	return hex.EncodeToString(digest[:])
}
