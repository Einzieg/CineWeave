package executioncontract

import (
	"testing"

	"github.com/google/uuid"
)

func TestVideoIdentityV2RequiresCompleteImmutableIdentity(t *testing.T) {
	identity := VideoIdentityV2{
		BaseIdentity: BaseIdentity{
			SchemaVersion:  SchemaVersionV2,
			OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(), WorkflowRunID: uuid.NewString(),
			OperationID: uuid.NewString(), OperationItemID: uuid.NewString(), Attempt: 1,
		},
		ProductionGenerationID: uuid.NewString(), VideoProductionBindingID: uuid.NewString(),
		VideoProductionBindingRevision: 3, StoryboardShotID: uuid.NewString(),
		ConfigurationSnapshotHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ExecutionPlanID:           uuid.NewString(), RenderSegmentID: uuid.NewString(),
	}
	if err := identity.Validate(true, true); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	identity.OperationItemID = ""
	if err := identity.Validate(true, true); err == nil {
		t.Fatal("Validate() error = nil, want missing operation item rejection")
	}
}

func TestTerminalStatusCannotMoveBackToRunning(t *testing.T) {
	for _, status := range []Status{StatusSucceeded, StatusPartialSucceeded, StatusFailed, StatusCancelled, StatusDiscarded} {
		if CanTransition(status, StatusRunning) {
			t.Fatalf("terminal status %q transitioned to running", status)
		}
	}
}

func TestHashRequestIsDeterministic(t *testing.T) {
	left, err := HashRequest(struct {
		Name string `json:"name"`
		Page int    `json:"page"`
	}{Name: "video", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	right, err := HashRequest(struct {
		Name string `json:"name"`
		Page int    `json:"page"`
	}{Name: "video", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if left != right || len(left) != 64 {
		t.Fatalf("hashes = %q, %q", left, right)
	}
}
