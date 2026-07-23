package api

import (
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCommerceReferenceImageBatchRequest(t *testing.T) {
	req := commerceReferenceImageBatchRequest{
		Operation:                " generate_images ",
		PlanID:                   "00000000-0000-4000-8000-000000000001",
		ExpectedPlanRevision:     3,
		ExpectedUnitGenerationID: "00000000-0000-4000-8000-000000000002",
		ShotIDs: []string{
			"00000000-0000-4000-8000-000000000004",
			" 00000000-0000-4000-8000-000000000003 ",
		},
	}

	require.NoError(t, normalizeCommerceReferenceImageBatchRequest(&req))
	require.Equal(t, "generate_images", req.Operation)
	require.Equal(t, 5, req.Concurrency)
	require.Equal(t, []string{
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
	}, req.ShotIDs)
}

func TestNormalizeCommerceReferenceImageBatchRequestRejectsDuplicateShots(t *testing.T) {
	req := commerceReferenceImageBatchRequest{
		Operation:                "generate_prompts",
		PlanID:                   "00000000-0000-4000-8000-000000000001",
		ExpectedPlanRevision:     1,
		ExpectedUnitGenerationID: "00000000-0000-4000-8000-000000000002",
		ShotIDs: []string{
			"00000000-0000-4000-8000-000000000003",
			"00000000-0000-4000-8000-000000000003",
		},
	}

	err := normalizeCommerceReferenceImageBatchRequest(&req)
	typed, ok := commercepkg.AsError(err)
	require.True(t, ok)
	require.Equal(t, commercepkg.CodeStoryboardInvalid, typed.Code)
}

func TestNormalizeCommerceVideoBatchRequest(t *testing.T) {
	req := commerceVideoBatchRequest{
		PlanID:                   "00000000-0000-4000-8000-000000000001",
		ExpectedPlanRevision:     2,
		ExpectedUnitGenerationID: "00000000-0000-4000-8000-000000000002",
		ShotIDs: []string{
			" 00000000-0000-4000-8000-000000000004 ",
			"00000000-0000-4000-8000-000000000003",
		},
		Resolution: " 1080P ",
	}

	require.NoError(t, normalizeCommerceVideoBatchRequest(&req))
	require.Equal(t, 5, req.Concurrency)
	require.Equal(t, "1080p", req.Resolution)
	require.Equal(t, []string{
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
	}, req.ShotIDs)
}

func TestNormalizeCommerceVideoBatchRequestRejectsEmptySelection(t *testing.T) {
	req := commerceVideoBatchRequest{
		PlanID:                   "00000000-0000-4000-8000-000000000001",
		ExpectedPlanRevision:     1,
		ExpectedUnitGenerationID: "00000000-0000-4000-8000-000000000002",
	}

	err := normalizeCommerceVideoBatchRequest(&req)
	typed, ok := commercepkg.AsError(err)
	require.True(t, ok)
	require.Equal(t, commercepkg.CodeStoryboardInvalid, typed.Code)
}

func TestSelectRetryableCommerceRunItems(t *testing.T) {
	items := []commercepkg.ProductionRunItem{
		{ID: "00000000-0000-4000-8000-000000000001", Status: commercepkg.ItemSucceeded},
		{ID: "00000000-0000-4000-8000-000000000002", Status: commercepkg.ItemFailedRetryable},
		{ID: "00000000-0000-4000-8000-000000000003", Status: commercepkg.ItemFailedTerminal},
	}

	selected, err := selectRetryableCommerceRunItems(items, nil)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	require.Equal(t, items[1].ID, selected[0].ID)
	require.Equal(t, items[2].ID, selected[1].ID)
}

func TestSelectRetryableCommerceRunItemsRejectsSucceededSelection(t *testing.T) {
	items := []commercepkg.ProductionRunItem{{
		ID: "00000000-0000-4000-8000-000000000001", Status: commercepkg.ItemSucceeded,
	}}

	_, err := selectRetryableCommerceRunItems(items, []string{items[0].ID})
	typed, ok := commercepkg.AsError(err)
	require.True(t, ok)
	require.Equal(t, commercepkg.CodeRunStateConflict, typed.Code)
}
