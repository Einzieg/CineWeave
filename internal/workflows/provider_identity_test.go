package workflows

import "testing"

func TestStableProviderRequestKeyUsesDatabaseOperationIdentity(t *testing.T) {
	identity := workflowExecutionIdentity{
		WorkflowRunID: "retry-run", RootWorkflowRunID: "root-run",
		ExecutionToken: "token", AttemptGeneration: 2,
	}
	first := stableProviderRequestKey("source-script", identity, "episode:chapter-1", "prompt-hash-a")
	second := stableProviderRequestKey("source-script", identity, "episode:chapter-1", "prompt-hash-a")
	if first != second {
		t.Fatalf("same logical request produced different keys: %q != %q", first, second)
	}
	activityRetry := identity
	activityRetry.WorkflowRunID = "retry-run"
	if got := stableProviderRequestKey("source-script", activityRetry, "episode:chapter-1", "prompt-hash-a"); got != first {
		t.Fatalf("activity retry changed key: %q != %q", got, first)
	}
	userRetry := identity
	userRetry.AttemptGeneration++
	if got := stableProviderRequestKey("source-script", userRetry, "episode:chapter-1", "prompt-hash-a"); got == first {
		t.Fatal("explicit retry generation did not change key")
	}
	if got := stableProviderRequestKey("source-script", identity, "episode:chapter-2", "prompt-hash-a"); got == first {
		t.Fatal("node identity did not change key")
	}
	if got := stableProviderRequestKey("source-script", identity, "episode:chapter-1", "prompt-hash-b"); got == first {
		t.Fatal("semantic input version did not change key")
	}
}
