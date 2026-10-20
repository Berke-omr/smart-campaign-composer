package delivery

import (
	"context"
	"time"
)

// Spacer is the per-channel rate limiter (DECISIONS.md D4, D24 K6). It lets
// one send through at a time, never sooner than interval after the moment
// it let the previous one through. Wait returns that moment, and the worker
// stores it as the attempt's started_at, so the guarantee holds for the very
// timestamps the dashboard charts: consecutive moments are at least 10 ms
// apart at 100/s, 101 of them span at least one second, and therefore no
// half-open 1 s window can hold more than 100.
//
// golang.org/x/time/rate spaces its reservations instead; a goroutine that
// wakes late can then land next to one that woke on time, and a measured
// window can hold 101.
type Spacer struct {
	interval time.Duration
	slot     chan struct{} // held while waiting for and taking a turn
	last     time.Time
}

// NewSpacer allows perSecond sends per second.
func NewSpacer(perSecond int) *Spacer {
	return &Spacer{interval: time.Second / time.Duration(perSecond), slot: make(chan struct{}, 1)}
}

// Wait blocks until the caller may send and returns the moment it was let
// through, or ctx's error.
func (s *Spacer) Wait(ctx context.Context) (time.Time, error) {
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	}
	defer func() { <-s.slot }()

	if !s.last.IsZero() {
		if d := time.Until(s.last.Add(s.interval)); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return time.Time{}, ctx.Err()
			}
		}
	}
	// Taken after the wait: the timer fires no earlier than asked, so this
	// moment is at least interval after the previous one.
	s.last = time.Now()
	return s.last, nil
}
