package workflows

import (
	"errors"
	"testing"
)

func TestRetryTimingAnalysisStoreAfterTransientWriteFenceRetriesOnce(t *testing.T) {
	attempts := 0
	output, err := retryTimingAnalysisStoreAfterTransientWriteFence(func() (TimingAnalysisActivityOutput, error) {
		attempts++
		if attempts == 1 {
			return TimingAnalysisActivityOutput{}, ErrWorkflowWriteFenced
		}
		return TimingAnalysisActivityOutput{AnalysisID: "analysis-1"}, nil
	})
	if err != nil {
		t.Fatalf("retry timing analysis store: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("store attempts = %d, want 2", attempts)
	}
	if output.AnalysisID != "analysis-1" {
		t.Fatalf("analysis id = %q, want analysis-1", output.AnalysisID)
	}
}

func TestRetryTimingAnalysisStoreAfterTransientWriteFenceKeepsPersistentFence(t *testing.T) {
	attempts := 0
	_, err := retryTimingAnalysisStoreAfterTransientWriteFence(func() (TimingAnalysisActivityOutput, error) {
		attempts++
		return TimingAnalysisActivityOutput{}, ErrWorkflowWriteFenced
	})
	if !isWorkflowWriteFenced(err) {
		t.Fatalf("retry error = %v, want workflow write fence", err)
	}
	if attempts != 2 {
		t.Fatalf("store attempts = %d, want 2", attempts)
	}
}

func TestRetryTimingAnalysisStoreAfterTransientWriteFenceDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	expected := errors.New("store failed")
	_, err := retryTimingAnalysisStoreAfterTransientWriteFence(func() (TimingAnalysisActivityOutput, error) {
		attempts++
		return TimingAnalysisActivityOutput{}, expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("retry error = %v, want %v", err, expected)
	}
	if attempts != 1 {
		t.Fatalf("store attempts = %d, want 1", attempts)
	}
}
