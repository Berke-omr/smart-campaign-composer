package delivery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
)

// Worker sends due deliveries (D24 K3). Each channel has its own claimer,
// limiter and pool of senders, so a slow or failing channel never holds up
// the other. The claimer keeps a small buffer of claimed rows; a sender
// takes one, waits for the channel's limiter, calls the provider and
// records the call.
type Worker struct {
	Queue  *Queue
	Client *Client
	Policy Policy
	Logger *slog.Logger

	PerSecond    int           // sends per channel per second (100)
	Senders      int           // senders per channel (32)
	Batch        int           // rows claimed at a time, and the buffer size (10)
	Poll         time.Duration // wait when nothing is due (200 ms)
	DrainTimeout time.Duration // on stop, time left to finish what is in hand (8 s)
	// Observe, when set, is told about every provider call that returned
	// (metrics), whether or not recording it succeeds.
	Observe func(Job, Call, Decision)
}

// Run works until ctx is cancelled. It then stops claiming, lets the
// senders finish the rows already claimed (up to DrainTimeout) and returns.
// Rows still unfinished after that keep their lease and are claimed again
// once it runs out, exactly as after a crash.
func (w *Worker) Run(ctx context.Context) {
	work, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopped:
			return
		}
		t := time.NewTimer(w.DrainTimeout)
		defer t.Stop()
		select {
		case <-t.C:
			cancelWork()
		case <-stopped:
		}
	}()

	var wg sync.WaitGroup
	for _, ch := range content.Channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.runChannel(ctx, work, ch)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			w.complete(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.Poll):
			}
		}
	}()
	wg.Wait()
	w.complete(work) // campaigns the drained rows finished
}

func (w *Worker) runChannel(ctx, work context.Context, ch content.Channel) {
	jobs := make(chan Job, w.Batch)
	limiter := NewSpacer(w.PerSecond)
	var wg sync.WaitGroup
	for range w.Senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				w.send(work, limiter, j)
			}
		}()
	}
	w.claim(ctx, work, ch, jobs)
	close(jobs)
	wg.Wait()
}

// claim fills the buffer until ctx is cancelled. Handing a claimed row to a
// sender blocks while every sender is busy, which keeps the number of rows
// claimed but not yet sent small. A claim runs under work, not ctx: a stop
// that arrives mid-claim must not cancel a query whose update may already
// have leased the rows, or those rows would sit out their lease.
func (w *Worker) claim(ctx, work context.Context, ch content.Channel, jobs chan<- Job) {
	for ctx.Err() == nil {
		claimed, err := w.Queue.Claim(work, ch, w.Batch)
		if err != nil && ctx.Err() == nil {
			w.Logger.Error("claim failed", "channel", ch, "err", err)
		}
		for _, j := range claimed {
			select {
			case jobs <- j:
			case <-work.Done():
				return
			}
		}
		if len(claimed) == w.Batch {
			continue // probably more due
		}
		select {
		case <-ctx.Done():
		case <-time.After(w.Poll):
		}
	}
}

func (w *Worker) send(work context.Context, limiter *Spacer, j Job) {
	started, err := limiter.Wait(work)
	if err != nil {
		return // stopping: the lease brings the row back
	}
	r, err := w.Client.Send(work, Message{Key: j.Key, Channel: j.Channel, To: j.To, Text: j.Text, Subject: j.Subject})
	if err != nil {
		return // stopped mid-call: nothing is recorded, the lease brings the row back
	}
	call := Call{Result: r, Started: started, Finished: time.Now()}
	throttled := j.Throttled
	if r.Kind == Throttled {
		throttled++
	}
	d := w.Policy.Next(r, j.Attempt, throttled)
	if w.Observe != nil {
		w.Observe(j, call, d)
	}
	ok, err := w.Queue.Finish(work, j, call, d)
	switch {
	case err != nil:
		w.Logger.Error("recording a call failed", "delivery", j.ID, "err", err)
	case !ok:
		w.Logger.Warn("lease lost; the delivery was claimed again", "delivery", j.ID, "attempt", j.Attempt)
	case d.Status == StatusFailed:
		w.Logger.Info("delivery failed", "delivery", j.ID, "campaign", j.CampaignID, "channel", j.Channel,
			"attempts", j.Attempt, "error", r.Error)
	case d.Status == StatusRetrying:
		w.Logger.Debug("delivery will be retried", "delivery", j.ID, "attempt", j.Attempt, "in", d.Delay, "error", r.Error)
	}
}

func (w *Worker) complete(ctx context.Context) {
	done, err := w.Queue.Complete(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.Logger.Error("completing campaigns failed", "err", err)
		}
		return
	}
	for id, status := range done {
		w.Logger.Info("campaign finished", "campaign", id, "status", status)
	}
}
