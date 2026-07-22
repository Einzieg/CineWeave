package workflows

import (
	"strings"
	"testing"
)

func TestNormalizeStoryboardSheetOutputReviewApprovesCompleteReview(t *testing.T) {
	review := normalizeStoryboardSheetOutputReview(StoryboardSheetOutputReview{
		Approved: true, PanelCountObserved: 3, Ordered: true, NoVisibleText: true,
		IdentityConsistent: true, SceneConsistent: true, ActionSequenceValid: true,
	}, 3)
	if !review.Approved || len(review.Issues) != 0 {
		t.Fatalf("complete storyboard sheet review = %+v", review)
	}
}

func TestNormalizeStoryboardSheetOutputReviewRejectsVisibleTextAndMissingPanel(t *testing.T) {
	review := normalizeStoryboardSheetOutputReview(StoryboardSheetOutputReview{
		Approved: true, PanelCountObserved: 2, Ordered: true, NoVisibleText: false,
		IdentityConsistent: true, SceneConsistent: true, ActionSequenceValid: true,
	}, 3)
	if review.Approved || len(review.Issues) != 2 {
		t.Fatalf("invalid storyboard sheet review = %+v", review)
	}
	joined := strings.Join(review.Issues, " ")
	if !strings.Contains(joined, "画格数") || !strings.Contains(joined, "可见文字") {
		t.Fatalf("review issues = %#v", review.Issues)
	}
}
