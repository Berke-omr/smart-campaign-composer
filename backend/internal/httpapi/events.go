package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/tracking"
)

// Live dashboard events (docs/API.md).
const (
	EventSnapshot = "delivery.snapshot"
	EventDone     = "delivery.done"
	EventError    = "delivery.error"
)

// reconnectMS is the browser's wait before it reopens a dashboard stream that
// closed without delivery.done (the "retry:" field of server-sent events).
const reconnectMS = 3000

// Snapshotter reads a campaign's live state; tracking.Reader implements it.
type Snapshotter interface {
	Snapshot(ctx context.Context, id int64, now time.Time) (tracking.Snapshot, error)
}

// sse writes server-sent events. The headers go out with the first event, so
// an error found before it can still be an ordinary JSON response.
type sse struct {
	w       http.ResponseWriter
	flusher http.Flusher
	retryMS int // sent once before the first event when set
	started bool
}

func newSSE(w http.ResponseWriter) (*sse, bool) {
	f, ok := w.(http.Flusher)
	return &sse{w: w, flusher: f}, ok
}

// emit writes one event and flushes it. An error means the client is gone.
func (s *sse) emit(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if !s.started {
		s.started = true
		h := s.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // nginx: do not buffer this response
		s.w.WriteHeader(http.StatusOK)
		if s.retryMS > 0 {
			if _, err := fmt.Fprintf(s.w, "retry: %d\n\n", s.retryMS); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// streamEvents is the live dashboard of a sending campaign (DECISIONS.md D7,
// D25): a full snapshot at once, then one every SnapshotEvery. When the
// campaign is finished it sends the final snapshot and delivery.done, then
// closes. The loop ends as soon as the client leaves.
func (d Deps) streamEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := campaignID(w, r)
	if !ok {
		return
	}
	out, ok := newSSE(w)
	if !ok {
		internalError(w, r, d.Logger, errors.New("response writer cannot flush"))
		return
	}
	out.retryMS = reconnectMS
	ctx := r.Context()

	s, err := d.Tracking.Snapshot(ctx, id, d.Now())
	switch {
	case errors.Is(err, tracking.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "campaign not found")
		return
	case err != nil:
		internalError(w, r, d.Logger, err)
		return
	case s.Status == "draft" || s.Status == "ready":
		httpx.WriteError(w, http.StatusConflict, "campaign is not sending yet: the live view starts once it is approved")
		return
	}

	tick := time.NewTicker(d.SnapshotEvery)
	defer tick.Stop()
	for {
		if err := out.emit(EventSnapshot, s); err != nil {
			return // the client is gone
		}
		if s.Final() {
			_ = out.emit(EventDone, map[string]any{"campaign_id": id, "status": s.Status})
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			s, err = d.Tracking.Snapshot(ctx, id, d.Now())
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return
			}
			// The stream stays open and tries again on the next tick; the
			// client keeps showing the last snapshot meanwhile.
			d.Logger.Error("dashboard snapshot failed", "campaign", id, "err", err)
			if err := out.emit(EventError, map[string]string{"message": "live data is unavailable; retrying"}); err != nil {
				return
			}
		}
	}
}
