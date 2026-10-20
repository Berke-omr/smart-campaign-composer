package delivery

import (
	"testing"
	"time"
)

func TestRetryPolicy(t *testing.T) {
	high := DefaultPolicy
	high.Rand = func() float64 { return 0.999 } // just under the ceiling of every draw
	low := DefaultPolicy
	low.Rand = func() float64 { return 0 }

	transient := Result{Kind: Transient, Error: "503 service unavailable"}
	throttled := func(ra time.Duration) Result { return Result{Kind: Throttled, Status: 429, RetryAfter: ra} }
	tests := []struct {
		name                string
		p                   Policy
		r                   Result
		attempts, throttled int
		status              string
		minDelay, maxDelay  time.Duration
	}{
		{"2xx is sent", high, Result{Kind: Sent}, 1, 0, StatusSent, 0, 0},
		{"400 fails at once", high, Result{Kind: Permanent, Status: 400}, 1, 0, StatusFailed, 0, 0},
		{"1st error: up to 1 s", high, transient, 1, 0, StatusRetrying, 990 * time.Millisecond, time.Second},
		{"2nd error: up to 2 s", high, transient, 2, 0, StatusRetrying, 1990 * time.Millisecond, 2 * time.Second},
		{"4th error: up to 8 s", high, transient, 4, 0, StatusRetrying, 7990 * time.Millisecond, 8 * time.Second},
		{"full jitter reaches 0", low, transient, 4, 0, StatusRetrying, 0, 0},
		{"5th error fails", high, transient, 5, 0, StatusFailed, 0, 0},
		{"429 waits Retry-After", low, throttled(2 * time.Second), 1, 1, StatusRetrying, 2 * time.Second, 2 * time.Second},
		{"429 adds up to 0.5 s jitter", high, throttled(2 * time.Second), 1, 1, StatusRetrying, 2490 * time.Millisecond, 2500 * time.Millisecond},
		{"Retry-After is capped", low, throttled(10 * time.Minute), 1, 1, StatusRetrying, time.Minute, time.Minute},
		{"429 without Retry-After backs off", high, throttled(0), 1, 1, StatusRetrying, 990 * time.Millisecond, time.Second},
		// K7: 429s do not use up the five attempts for errors...
		{"4 errors after 6 429s still retry", high, transient, 10, 6, StatusRetrying, 7990 * time.Millisecond, 8 * time.Second},
		{"9th 429 after 4 errors retries", low, throttled(time.Second), 13, 9, StatusRetrying, time.Second, time.Second},
		// ...but have their own budget, so a delivery always ends.
		{"10th 429 fails", high, throttled(time.Second), 10, 10, StatusFailed, 0, 0},
		{"5th error after 3 429s fails", high, transient, 8, 3, StatusFailed, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.p.Next(tt.r, tt.attempts, tt.throttled)
			if d.Status != tt.status || d.Delay < tt.minDelay || d.Delay > tt.maxDelay {
				t.Errorf("Next = %+v, want %s with a delay in [%s, %s]", d, tt.status, tt.minDelay, tt.maxDelay)
			}
		})
	}
}

// TestFullJitterSpreadsRetries: the default random source spreads the
// retries of one wave over the whole interval instead of bunching them.
func TestFullJitterSpreadsRetries(t *testing.T) {
	var buckets [4]int
	for range 4000 {
		d := DefaultPolicy.Next(Result{Kind: Transient}, 2, 0).Delay // up to 2 s
		if d < 0 || d >= 2*time.Second {
			t.Fatalf("delay %s outside [0, 2s)", d)
		}
		buckets[d/(500*time.Millisecond)]++
	}
	for i, n := range buckets {
		if n < 800 || n > 1200 {
			t.Errorf("quarter %d of the interval got %d of 4000 retries; want about 1000", i, n)
		}
	}
}
