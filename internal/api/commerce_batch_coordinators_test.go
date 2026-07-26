package api

import (
	"slices"
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/google/uuid"
)

func TestNormalizeCommerceScriptUnitBatchAdvanceRequestFreezesSelection(t *testing.T) {
	firstUnit := uuid.NewString()
	secondUnit := uuid.NewString()
	firstShot := uuid.NewString()
	secondShot := uuid.NewString()
	req := commerceScriptUnitBatchAdvanceRequest{
		TargetStage:     " shot_videos ",
		UnitConcurrency: 99,
		MaxConcurrency:  0,
		Items: []commerceScriptUnitBatchAdvanceItem{
			{
				ScriptUnitID:             firstUnit,
				ExpectedUnitGenerationID: uuid.NewString(),
				PlanID:                   uuid.NewString(),
				ExpectedPlanRevision:     2,
				ShotIDs:                  []string{secondShot, firstShot, secondShot},
			},
			{
				ScriptUnitID:             secondUnit,
				ExpectedUnitGenerationID: uuid.NewString(),
				PlanID:                   uuid.NewString(),
				ExpectedPlanRevision:     3,
				ShotIDs:                  []string{uuid.NewString()},
				Resolution:               " 720P ",
			},
		},
	}

	if err := normalizeCommerceScriptUnitBatchAdvanceRequest(&req); err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	if req.TargetStage != "shot_videos" || req.UnitConcurrency != 16 || req.MaxConcurrency != 4 {
		t.Fatalf("normalized coordinator settings = %+v", req)
	}
	wantShotIDs := []string{firstShot, secondShot}
	slices.Sort(wantShotIDs)
	if !slices.Equal(req.Items[0].ShotIDs, wantShotIDs) {
		t.Fatalf("normalized first selection = %+v", req.Items[0].ShotIDs)
	}
	if req.Items[0].AttemptGeneration != 1 || req.Items[0].Resolution != "" || req.Items[1].Resolution != "720p" {
		t.Fatalf("normalized item defaults = %+v", req.Items)
	}
}

func TestNormalizeCommerceScriptUnitBatchAdvanceRequestRejectsDuplicateUnit(t *testing.T) {
	unitID := uuid.NewString()
	req := commerceScriptUnitBatchAdvanceRequest{
		TargetStage: "storyboard",
		Items: []commerceScriptUnitBatchAdvanceItem{
			{ScriptUnitID: unitID, ExpectedUnitGenerationID: uuid.NewString()},
			{ScriptUnitID: unitID, ExpectedUnitGenerationID: uuid.NewString()},
		},
	}
	err := normalizeCommerceScriptUnitBatchAdvanceRequest(&req)
	apiErr, ok := err.(apiError)
	if !ok || apiErr.Code != "COMMERCE_BATCH_SELECTION_INVALID" {
		t.Fatalf("error = %#v, want COMMERCE_BATCH_SELECTION_INVALID", err)
	}
}

func TestNormalizeCommerceScriptUnitBatchAdvanceRequestRequiresFrozenStageInputs(t *testing.T) {
	req := commerceScriptUnitBatchAdvanceRequest{
		TargetStage: "video_prompts",
		Items: []commerceScriptUnitBatchAdvanceItem{{
			ScriptUnitID:             uuid.NewString(),
			ExpectedUnitGenerationID: uuid.NewString(),
		}},
	}
	err := normalizeCommerceScriptUnitBatchAdvanceRequest(&req)
	commerceErr, ok := err.(commercepkg.Error)
	if !ok || commerceErr.Code != commercepkg.CodeStoryboardInvalid {
		t.Fatalf("error = %#v, want %s", err, commercepkg.CodeStoryboardInvalid)
	}
}
