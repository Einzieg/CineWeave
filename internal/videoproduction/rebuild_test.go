package videoproduction

import "testing"

func TestRebuildImpactTokenIsStableAndSensitiveToEpisodeRevision(t *testing.T) {
	impact := RebuildImpact{
		ProjectID:               "project-1",
		ExpectedProjectRevision: 4,
		SourceBindingID:         "binding-1",
		SourceBindingRevision:   2,
		SourceGenerationID:      "generation-1",
		SourceGenerationNo:      2,
		TargetProfileVersionID:  "profile-version-2",
		TargetProfileKey:        ProfileFirstLastFrame,
		TargetProfileVersion:    2,
		ScriptID:                "script-1",
		ScriptVersionID:         "script-version-1",
		Episodes: []RebuildEpisodeImpact{{
			ScriptEpisodeID:       "episode-1",
			EpisodeOrdinal:        1,
			ScriptEpisodeRevision: 3,
			ScriptEpisodeHash:     "hash-1",
		}},
		Counts: RebuildImpactCounts{Episodes: 1, StoryboardShots: 8, RetainedAssets: 4},
	}
	first, err := rebuildImpactToken(impact)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rebuildImpactToken(impact)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("impact token is not stable: %s != %s", first, second)
	}
	impact.Episodes[0].ScriptEpisodeRevision++
	changed, err := rebuildImpactToken(impact)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("episode revision did not invalidate impact token")
	}
}

func TestVerifyRebuildImpactRejectsStaleRevisionOrToken(t *testing.T) {
	impact := RebuildImpact{ExpectedProjectRevision: 9, ImpactToken: "expected"}
	if err := VerifyRebuildImpact(impact, 9, "expected"); err != nil {
		t.Fatalf("verify current impact: %v", err)
	}
	for _, test := range []struct {
		revision int64
		token    string
	}{{8, "expected"}, {9, "old"}} {
		err := VerifyRebuildImpact(impact, test.revision, test.token)
		typed, ok := AsError(err)
		if !ok || typed.Code != CodeRebuildImpactStale {
			t.Fatalf("error = %#v", err)
		}
	}
}
