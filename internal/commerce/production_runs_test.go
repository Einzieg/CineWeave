package commerce

import (
	"strings"
	"testing"
)

func TestProductionSubjectValidation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		runType ProductionRunType
		subject ProductionSubject
		valid   bool
	}{
		{name: "storyboard plan phase", runType: RunTypeStoryboardPlan, subject: ProductionSubject{Type: SubjectPlanPhase, Key: "analysis", InputHash: hash}, valid: true},
		{name: "storyboard candidate", runType: RunTypeStoryboardPlan, subject: ProductionSubject{Type: SubjectCandidateShot, Key: "candidate-001", InputHash: hash}, valid: true},
		{name: "reference image shot", runType: RunTypeReferenceImages, subject: ProductionSubject{Type: SubjectStoryboardShot, Key: "shot-001", StoryboardShotID: "shot-id", InputHash: hash}, valid: true},
		{name: "video prompt shot", runType: RunTypeVideoPrompts, subject: ProductionSubject{Type: SubjectStoryboardShot, Key: "shot-001", StoryboardShotID: "shot-id", InputHash: hash}, valid: true},
		{name: "shot video shot", runType: RunTypeShotVideos, subject: ProductionSubject{Type: SubjectStoryboardShot, Key: "shot-001", StoryboardShotID: "shot-id", InputHash: hash}, valid: true},
		{name: "final compose", runType: RunTypeFinalCompose, subject: ProductionSubject{Type: SubjectFinalCompose, Key: "final", InputHash: hash}, valid: true},
		{name: "planning cannot use committed shot", runType: RunTypeStoryboardPlan, subject: ProductionSubject{Type: SubjectPlanPhase, Key: "analysis", StoryboardShotID: "shot-id", InputHash: hash}},
		{name: "shot stage needs shot id", runType: RunTypeShotVideos, subject: ProductionSubject{Type: SubjectStoryboardShot, Key: "shot-001", InputHash: hash}},
		{name: "final cannot use shot", runType: RunTypeFinalCompose, subject: ProductionSubject{Type: SubjectStoryboardShot, Key: "shot-001", StoryboardShotID: "shot-id", InputHash: hash}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.subject.Validate(tt.runType)
			if tt.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("Validate() accepted an invalid subject")
			}
		})
	}
}

func TestAggregateProductionRun(t *testing.T) {
	tests := []struct {
		name     string
		current  ProductionRunStatus
		statuses []ProductionItemStatus
		want     ProductionRunAggregate
	}{
		{name: "all queued", current: RunQueued, statuses: []ProductionItemStatus{ItemQueued, ItemQueued}, want: ProductionRunAggregate{Status: RunQueued, Total: 2, Active: 2}},
		{name: "running with committed item", current: RunRunning, statuses: []ProductionItemStatus{ItemSucceeded, ItemRunning}, want: ProductionRunAggregate{Status: RunRunning, Total: 2, Completed: 1, Active: 1}},
		{name: "success", current: RunRunning, statuses: []ProductionItemStatus{ItemSucceeded, ItemSkipped}, want: ProductionRunAggregate{Status: RunSucceeded, Total: 2, Completed: 2}},
		{name: "all failed", current: RunRunning, statuses: []ProductionItemStatus{ItemFailedRetryable, ItemFailedTerminal}, want: ProductionRunAggregate{Status: RunFailed, Total: 2, Failed: 2}},
		{name: "partial", current: RunRunning, statuses: []ProductionItemStatus{ItemSucceeded, ItemFailedTerminal}, want: ProductionRunAggregate{Status: RunPartiallySucceeded, Total: 2, Completed: 1, Failed: 1}},
		{name: "cancelled", current: RunCancelling, statuses: []ProductionItemStatus{ItemCancelled, ItemCancelled}, want: ProductionRunAggregate{Status: RunCancelled, Total: 2, Cancelled: 2}},
		{name: "cancelling remains active", current: RunCancelling, statuses: []ProductionItemStatus{ItemCancelled, ItemRunning}, want: ProductionRunAggregate{Status: RunCancelling, Total: 2, Cancelled: 1, Active: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AggregateProductionRun(tt.current, tt.statuses); got != tt.want {
				t.Fatalf("AggregateProductionRun() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
