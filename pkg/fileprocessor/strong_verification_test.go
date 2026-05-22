package fileprocessor

import "testing"

func TestPlanStrongVerificationAction(t *testing.T) {
	tests := []struct {
		name         string
		initialDiffs int
		retryDiffs   int
		threshold    int
		wantRetry    bool
		wantFallback bool
	}{
		{
			name:         "accept immediately when within threshold",
			initialDiffs: 0,
			retryDiffs:   0,
			threshold:    0,
			wantRetry:    false,
			wantFallback: false,
		},
		{
			name:         "retry once when initial exceeds threshold",
			initialDiffs: 5,
			retryDiffs:   0,
			threshold:    1,
			wantRetry:    true,
			wantFallback: false,
		},
		{
			name:         "fallback after retry still exceeds threshold",
			initialDiffs: 5,
			retryDiffs:   3,
			threshold:    1,
			wantRetry:    true,
			wantFallback: true,
		},
		{
			name:         "no fallback when retry reaches threshold",
			initialDiffs: 3,
			retryDiffs:   1,
			threshold:    1,
			wantRetry:    true,
			wantFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRetry, gotFallback := planStrongVerificationAction(tt.initialDiffs, tt.retryDiffs, tt.threshold)
			if gotRetry != tt.wantRetry || gotFallback != tt.wantFallback {
				t.Fatalf("planStrongVerificationAction(%d,%d,%d)=(%v,%v), want (%v,%v)",
					tt.initialDiffs, tt.retryDiffs, tt.threshold, gotRetry, gotFallback, tt.wantRetry, tt.wantFallback)
			}
		})
	}
}
