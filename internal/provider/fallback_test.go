package provider

import "testing"

func TestFallbackStrategyDecision(t *testing.T) {
	strategy := defaultFallbackStrategy()
	cases := []struct {
		name         string
		code         string
		wantFallback bool
		wantStop     bool
	}{
		{name: "circuit open fallback", code: CodeProviderCircuitOpen, wantFallback: true},
		{name: "concurrency fallback", code: CodeProviderConcurrencyLimited, wantFallback: true},
		{name: "timeout fallback", code: CodeUpstreamTimeout, wantFallback: true},
		{name: "auth stop", code: CodeAuthFailed, wantStop: true},
		{name: "invalid request stop", code: CodeInvalidRequest, wantStop: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFallback(tc.code, strategy); got != tc.wantFallback {
				t.Fatalf("shouldFallback(%s) = %v, want %v", tc.code, got, tc.wantFallback)
			}
			if got := shouldStop(tc.code, strategy); got != tc.wantStop {
				t.Fatalf("shouldStop(%s) = %v, want %v", tc.code, got, tc.wantStop)
			}
		})
	}
}

func TestFallbackMaxAttempts(t *testing.T) {
	strategy := defaultFallbackStrategy()
	strategy.MaxAttempts = 2
	if got := fallbackMaxAttempts(strategy, 5); got != 2 {
		t.Fatalf("fallbackMaxAttempts = %d, want 2", got)
	}
	strategy.MaxAttempts = 20
	if got := fallbackMaxAttempts(strategy, 3); got != 3 {
		t.Fatalf("fallbackMaxAttempts clamps to candidates = %d, want 3", got)
	}
	strategy.Enabled = false
	if got := fallbackMaxAttempts(strategy, 3); got != 1 {
		t.Fatalf("disabled fallback maxAttempts = %d, want 1", got)
	}
}

func TestValidateFallbackStrategy(t *testing.T) {
	if _, err := validateFallbackStrategy([]byte(`[]`)); err == nil {
		t.Fatal("validateFallbackStrategy accepted non-object JSON")
	}
	if _, err := validateFallbackStrategy([]byte(`{"maxAttempts":11}`)); err == nil {
		t.Fatal("validateFallbackStrategy accepted maxAttempts > 10")
	}
	if _, err := validateFallbackStrategy([]byte(`{"enabled":true,"maxAttempts":1}`)); err != nil {
		t.Fatalf("validateFallbackStrategy valid object: %v", err)
	}
}
