package delivery

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

// maxInWindow returns the most timestamps that fall in any half-open window
// [t, t+w). The busiest window can always be shifted to start at a
// timestamp, so checking those windows is enough.
func maxInWindow(ts []time.Time, w time.Duration) int {
	ts = slices.Clone(ts)
	slices.SortFunc(ts, func(a, b time.Time) int { return a.Compare(b) })
	most, j := 0, 0
	for i := range ts {
		for j < len(ts) && ts[j].Before(ts[i].Add(w)) {
			j++
		}
		most = max(most, j-i)
	}
	return most
}

func TestMaxInWindow(t *testing.T) {
	at := func(ms ...int) []time.Time {
		var out []time.Time
		for _, m := range ms {
			out = append(out, time.Unix(0, 0).Add(time.Duration(m)*time.Millisecond))
		}
		return out
	}
	for _, tt := range []struct {
		ts   []time.Time
		want int
	}{
		{nil, 0},
		{at(0, 999), 2},
		{at(0, 1000), 1}, // half-open: 1000 ms is in the next window
		{at(500, 0, 999, 1499, 1500), 3},
	} {
		if got := maxInWindow(tt.ts, time.Second); got != tt.want {
			t.Errorf("maxInWindow(%v) = %d, want %d", tt.ts, got, tt.want)
		}
	}
}

// TestSpacerHoldsTheLimitInEveryWindow: 32 goroutines compete for 300 sends
// at 100/s. No half-open 1 s window holds more than 100 of the returned
// moments, and the run takes at least 2.99 s, so the limiter really held
// the callers back (a no-op limiter would finish at once).
func TestSpacerHoldsTheLimitInEveryWindow(t *testing.T) {
	s := NewSpacer(100)
	ctx := context.Background()
	var mu sync.Mutex
	var moments []time.Time
	var wg sync.WaitGroup
	start := time.Now()
	jobs := make(chan struct{}, 300)
	for range 300 {
		jobs <- struct{}{}
	}
	close(jobs)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				at, err := s.Wait(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				moments = append(moments, at)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	if most := maxInWindow(moments, time.Second); most > 100 {
		t.Errorf("%d sends in one 1 s window, limit 100", most)
	}
	if elapsed < 2990*time.Millisecond {
		t.Errorf("300 sends took %s; at 100/s they need at least 2.99 s", elapsed)
	}
	slices.SortFunc(moments, func(a, b time.Time) int { return a.Compare(b) })
	for i := 1; i < len(moments); i++ {
		if gap := moments[i].Sub(moments[i-1]); gap < 10*time.Millisecond {
			t.Fatalf("sends %d and %d are %s apart, want at least 10 ms", i-1, i, gap)
		}
	}
	t.Logf("300 sends in %s; busiest second held %d", elapsed.Round(time.Millisecond), maxInWindow(moments, time.Second))
}

func TestSpacerStopsWaitingWhenCancelled(t *testing.T) {
	s := NewSpacer(1) // one per second: the second caller must wait
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := s.Wait(ctx); err == nil || time.Since(start) > 500*time.Millisecond {
		t.Errorf("err = %v after %s; want the context's error quickly", err, time.Since(start))
	}
}
