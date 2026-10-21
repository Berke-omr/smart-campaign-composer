package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/delivery"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/tracking"
)

// fakeTracking returns scripted snapshots, then repeats the last one, and
// counts the calls.
type fakeTracking struct {
	mu     sync.Mutex
	script []result
	calls  atomic.Int64
}

type result struct {
	s   tracking.Snapshot
	err error
}

func (f *fakeTracking) Snapshot(_ context.Context, id int64, now time.Time) (tracking.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := int(f.calls.Add(1)) - 1
	r := f.script[min(n, len(f.script)-1)]
	r.s.CampaignID, r.s.At = id, now
	return r.s, r.err
}

func snap(status string, done int) result {
	return result{s: tracking.Snapshot{Status: status, Total: 10, Done: done}}
}

// sseEvent is one parsed server-sent event; retry is set on the block that
// carries only a "retry:" field.
type sseEvent struct {
	name  string
	data  json.RawMessage
	retry string
}

// sseReader parses a text/event-stream incrementally.
type sseReader struct{ sc *bufio.Scanner }

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	return &sseReader{sc: sc}
}

// next returns the next event, or false at the end of the stream.
func (r *sseReader) next() (sseEvent, bool) {
	var e sseEvent
	for r.sc.Scan() {
		line := r.sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			e.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			e.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		case strings.HasPrefix(line, "retry: "):
			e.retry = strings.TrimPrefix(line, "retry: ")
		case line == "":
			return e, true
		}
	}
	return e, false
}

// client bounds every request of these tests, so a stream that never ends
// fails the test instead of hanging it.
var client = &http.Client{Timeout: 60 * time.Second}

func eventsServer(t *testing.T, f Snapshotter, every time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter(Deps{Logger: logging.Discard(), Tracking: f, SnapshotEvery: every}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEventsUntilTheCampaignFinishes: a snapshot at once and one per tick;
// the finished campaign's snapshot is followed by delivery.done and the end
// of the stream.
func TestEventsUntilTheCampaignFinishes(t *testing.T) {
	f := &fakeTracking{script: []result{snap("sending", 2), snap("sending", 6), snap("completed", 10)}}
	srv := eventsServer(t, f, 5*time.Millisecond)

	resp, err := client.Get(srv.URL + "/api/campaigns/7/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("X-Accel-Buffering") != "no" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	r := newSSEReader(resp.Body)
	var got []string
	for {
		e, ok := r.next()
		if !ok {
			break
		}
		switch {
		case e.retry != "":
			got = append(got, "retry "+e.retry)
		case e.name == EventSnapshot:
			var s tracking.Snapshot
			if err := json.Unmarshal(e.data, &s); err != nil {
				t.Fatal(err)
			}
			got = append(got, fmt.Sprintf("snapshot %s %d/%d #%d", s.Status, s.Done, s.Total, s.CampaignID))
		default:
			got = append(got, e.name+" "+string(e.data))
		}
	}
	want := []string{"retry 3000", "snapshot sending 2/10 #7", "snapshot sending 6/10 #7", "snapshot completed 10/10 #7",
		`delivery.done {"campaign_id":7,"status":"completed"}`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := f.calls.Load(); n != 3 {
		t.Errorf("%d snapshots read, want 3", n)
	}
}

// TestEventsOfAFinishedCampaign: opening (or reopening) the stream of a
// finished campaign gives its state and delivery.done at once.
func TestEventsOfAFinishedCampaign(t *testing.T) {
	f := &fakeTracking{script: []result{snap("failed", 10)}}
	srv := eventsServer(t, f, time.Hour)
	resp, err := client.Get(srv.URL + "/api/campaigns/3/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := newSSEReader(resp.Body)
	var names []string
	for e, ok := r.next(); ok; e, ok = r.next() {
		if e.name != "" {
			names = append(names, e.name)
		}
	}
	if strings.Join(names, " ") != EventSnapshot+" "+EventDone {
		t.Errorf("events = %v, want a snapshot and done", names)
	}
}

// TestEventsAnswersBeforeTheStream: problems found before the first event
// are ordinary JSON answers, which also stop EventSource from reconnecting.
func TestEventsAnswersBeforeTheStream(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		result result
		status int
		body   string
	}{
		{"unknown campaign", "/api/campaigns/9/events", result{err: tracking.ErrNotFound}, 404, "campaign not found"},
		{"invalid id", "/api/campaigns/x/events", snap("sending", 0), 404, "campaign not found"},
		{"draft", "/api/campaigns/9/events", snap("draft", 0), 409, "not sending yet"},
		{"ready", "/api/campaigns/9/events", snap("ready", 0), 409, "not sending yet"},
		{"database error", "/api/campaigns/9/events", result{err: errors.New("connection refused")}, 500, "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := eventsServer(t, &fakeTracking{script: []result{tt.result}}, time.Hour)
			resp, err := client.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.status || !strings.Contains(string(body), tt.body) ||
				!strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				t.Errorf("status %d %s body %s; want %d with %q", resp.StatusCode, resp.Header.Get("Content-Type"), body, tt.status, tt.body)
			}
		})
	}
}

// TestEventsReportAFailedSnapshotAndGoOn: a database error mid-stream is
// reported and the next tick tries again.
func TestEventsReportAFailedSnapshotAndGoOn(t *testing.T) {
	f := &fakeTracking{script: []result{snap("sending", 1), {err: errors.New("boom")}, snap("completed", 10)}}
	srv := eventsServer(t, f, 5*time.Millisecond)
	resp, err := client.Get(srv.URL + "/api/campaigns/1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := newSSEReader(resp.Body)
	var names []string
	for e, ok := r.next(); ok; e, ok = r.next() {
		if e.name != "" {
			names = append(names, e.name)
		}
	}
	want := []string{EventSnapshot, EventError, EventSnapshot, EventDone}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("events = %v, want %v", names, want)
	}
}

// recorder is a ResponseWriter whose writes never fail, so only the request
// context can tell the handler that the client left. failAfter > 0 makes
// writes fail after that many events instead.
type recorder struct {
	mu        sync.Mutex
	header    http.Header
	events    int
	failAfter int
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(int)     {}
func (r *recorder) Flush()              {}
func (r *recorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failAfter > 0 && r.events >= r.failAfter {
		return 0, errors.New("broken pipe")
	}
	r.events += strings.Count(string(b), "event: ")
	return len(b), nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events
}

// serveUntilReturn runs the handler and reports when it returns.
func serveUntilReturn(h http.Handler, w http.ResponseWriter, req *http.Request) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, req)
	}()
	return done
}

// TestEventsStopWhenTheClientLeaves: the loop ends with the request's
// context even when writing never fails, and reads no snapshot afterwards.
func TestEventsStopWhenTheClientLeaves(t *testing.T) {
	f := &fakeTracking{script: []result{snap("sending", 1)}}
	h := NewRouter(Deps{Logger: logging.Discard(), Tracking: f, SnapshotEvery: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	rec := &recorder{header: http.Header{}}
	done := serveUntilReturn(h, rec, httptest.NewRequest(http.MethodGet, "/api/campaigns/1/events", nil).WithContext(ctx))

	for rec.count() < 3 {
		select {
		case <-done:
			t.Fatal("the stream ended by itself")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler is still running 10 s after the client left")
	}
	calls := f.calls.Load()
	time.Sleep(50 * time.Millisecond) // ten intervals
	if n := f.calls.Load(); n != calls {
		t.Errorf("%d snapshots read after the handler returned", n-calls)
	}
}

// TestEventsStopWhenWritingFails: a failed write ends the loop even while
// the request's context is alive.
func TestEventsStopWhenWritingFails(t *testing.T) {
	f := &fakeTracking{script: []result{snap("sending", 1)}}
	h := NewRouter(Deps{Logger: logging.Discard(), Tracking: f, SnapshotEvery: 5 * time.Millisecond})
	rec := &recorder{header: http.Header{}, failAfter: 2}
	done := serveUntilReturn(h, rec, httptest.NewRequest(http.MethodGet, "/api/campaigns/1/events", nil))
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler is still running 10 s after its writes started failing")
	}
}

// TestLiveDashboardDuringASend runs the real worker against the mock
// provider and follows the campaign over the events stream: every snapshot
// adds up, progress only moves forward, no second holds more than 100 calls
// per channel, a reconnect mid-send gets the whole state at once, and the
// final snapshot equals the database.
func TestLiveDashboardDuringASend(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, seed.Generate(1000, 42, fixedNow), false); err != nil {
		t.Fatal(err)
	}
	const perChannel = 300
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO campaigns (name, brief, tone, channels, segment_rule, audience_as_of, llm_mode, status, approved_at)
		VALUES ('live', 'b', 'friendly', ARRAY['sms', 'email'], '{}', now(), 'mock', 'sending', now())
		RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text)
		 SELECT $1, id, 'sms', phone, 'Merhaba' FROM users WHERE sms_opt_in ORDER BY id LIMIT $2`,
		`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, subject, body)
		 SELECT $1, id, 'email', email, 'Konu', 'Merhaba' FROM users WHERE email_opt_in ORDER BY id LIMIT $2`,
	} {
		if _, err := pool.Exec(ctx, sql, id, perChannel); err != nil {
			t.Fatal(err)
		}
	}

	// Faults, so that issues and retries show up; no rate limit of its own.
	provider := mockprovider.New(config.MockProvider{LatencyMin: time.Millisecond, LatencyMax: 5 * time.Millisecond,
		ErrorRate: 0.05, ThrottleRate: 0.05, RejectRate: 0.02}, 11)
	provSrv := httptest.NewServer(provider.Handler())
	defer provSrv.Close()
	worker := &delivery.Worker{
		Queue:  delivery.NewQueue(pool, 30*time.Second),
		Client: &delivery.Client{BaseURL: provSrv.URL, HTTP: &http.Client{Timeout: 2 * time.Second}},
		Policy: delivery.Policy{MaxAttempts: 5, MaxThrottled: 10, Base: 5 * time.Millisecond, Cap: 50 * time.Millisecond,
			RetryAfterJitter: 10 * time.Millisecond},
		Logger: logging.Discard(), PerSecond: delivery.MessagesPerSecond, Senders: 16, Batch: 10,
		Poll: 20 * time.Millisecond, DrainTimeout: 2 * time.Second,
	}
	workCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.Run(workCtx) }()
	defer func() { stopWorker(); <-workerDone }()

	srv := eventsServer(t, tracking.NewReader(pool), 100*time.Millisecond)
	url := fmt.Sprintf("%s/api/campaigns/%d/events", srv.URL, id)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var snaps []tracking.Snapshot
	ended := false
	reconnected := false
	r := newSSEReader(resp.Body)
	for e, ok := r.next(); ok; e, ok = r.next() {
		switch e.name {
		case EventSnapshot:
			var s tracking.Snapshot
			if err := json.Unmarshal(e.data, &s); err != nil {
				t.Fatal(err)
			}
			snaps = append(snaps, s)
		case EventDone:
			ended = true
		case EventError:
			t.Errorf("delivery.error: %s", e.data)
		}
		// Mid-send, a second client connects: its first event is the whole
		// state, not a change.
		if len(snaps) == 0 || e.name != EventSnapshot {
			continue
		}
		if last := snaps[len(snaps)-1]; !reconnected && last.Done > 0 && last.Done < last.Total {
			reconnected = true
			second, err := client.Get(url)
			if err != nil {
				t.Fatal(err)
			}
			rr := newSSEReader(second.Body)
			first, _ := rr.next() // the retry field
			first, _ = rr.next()
			second.Body.Close()
			var s tracking.Snapshot
			if first.name != EventSnapshot || json.Unmarshal(first.data, &s) != nil {
				t.Fatalf("reconnect: first event %q %s, want a snapshot", first.name, first.data)
			}
			if s.Total != 2*perChannel || len(s.Channels) != 2 || s.Done < last.Done {
				t.Errorf("reconnect: total %d, %d channels, done %d (stream had %d); want the whole state",
					s.Total, len(s.Channels), s.Done, last.Done)
			}
		}
	}
	if !ended {
		t.Fatal("the stream ended without delivery.done")
	}
	if !reconnected {
		t.Error("no snapshot was taken mid-send")
	}

	// Every snapshot adds up, and progress never goes back.
	prevDone := 0
	for i, s := range snaps {
		sum := 0
		for ch, c := range s.Channels {
			if n := c.Queued + c.Sending + c.Retrying + c.Sent + c.Failed; n != perChannel || c.Total != perChannel {
				t.Errorf("snapshot %d %s: states add up to %d, total %d; want %d", i, ch, n, c.Total, perChannel)
			}
			if c.Done != c.Sent+c.Failed {
				t.Errorf("snapshot %d %s: done %d != sent %d + failed %d", i, ch, c.Done, c.Sent, c.Failed)
			}
			for sec, v := range c.PerSecond {
				if v > delivery.MessagesPerSecond {
					t.Errorf("snapshot %d %s: %d calls in second %d of the window", i, ch, v, sec)
				}
			}
			sum += c.Done
		}
		if s.Done != sum || s.Done < prevDone {
			t.Errorf("snapshot %d: done %d (channels %d), previous %d", i, s.Done, sum, prevDone)
		}
		prevDone = s.Done
	}
	if len(snaps) < 5 {
		t.Errorf("%d snapshots, want several during the send", len(snaps))
	}

	// The final snapshot equals the database.
	final := snaps[len(snaps)-1]
	if final.Status != "completed" || final.FinishedAt == nil || final.Done != final.Total {
		t.Errorf("final: status %s finished %v done %d of %d", final.Status, final.FinishedAt, final.Done, final.Total)
	}
	rows, err := pool.Query(ctx, `SELECT channel, status, count(*) FROM deliveries WHERE campaign_id = $1 GROUP BY 1, 2`, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ch, status string
		var n int
		if err := rows.Scan(&ch, &status, &n); err != nil {
			t.Fatal(err)
		}
		c := final.Channels[content.Channel(ch)]
		if got := map[string]int{"sent": c.Sent, "failed": c.Failed}[status]; got != n {
			t.Errorf("final %s %s: snapshot %d, table %d", ch, status, got, n)
		}
	}
	rows.Close()

	// Per second: the final window covers the whole send, so it must equal
	// the attempt log counted independently (date_trunc, not the snapshot's
	// epoch arithmetic). Earlier snapshots never show more than the log.
	perSecond := map[string]map[int64]int{"sms": {}, "email": {}}
	rows, err = pool.Query(ctx, `
		SELECT channel, extract(epoch FROM date_trunc('second', started_at AT TIME ZONE 'UTC'))::bigint, count(*)
		FROM delivery_attempts WHERE campaign_id = $1 GROUP BY 1, 2`, id)
	if err != nil {
		t.Fatal(err)
	}
	logged := 0
	for rows.Next() {
		var ch string
		var sec int64
		var n int
		if err := rows.Scan(&ch, &sec, &n); err != nil {
			t.Fatal(err)
		}
		perSecond[ch][sec] = n
		logged += n
	}
	rows.Close()
	shown := 0
	for ch, c := range final.Channels {
		for i, v := range c.PerSecond {
			sec := final.Window.From.Unix() + int64(i)
			if v != perSecond[string(ch)][sec] {
				t.Errorf("final %s second %d: snapshot %d, log %d", ch, sec, v, perSecond[string(ch)][sec])
			}
			shown += v
		}
	}
	if shown != logged {
		t.Errorf("final window shows %d calls, the log has %d", shown, logged)
	}
	exact, settled := 0, 0
	for _, s := range snaps[:len(snaps)-1] {
		for ch, c := range s.Channels {
			for i, v := range c.PerSecond {
				log := perSecond[string(ch)][s.Window.From.Unix()+int64(i)]
				if v > log {
					t.Errorf("snapshot at %v %s second %d: %d calls, the log has only %d", s.At, ch, i, v, log)
				}
				if log > 0 {
					settled++
					if v == log {
						exact++
					}
				}
			}
		}
	}
	t.Logf("%d snapshots; %d calls logged; during the send %d of %d non-empty seconds already showed their final count",
		len(snaps), logged, exact, settled)

	// The busiest half-open second of the log, measured independently.
	var busiest int
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(max(n), 0) FROM (
			SELECT count(*) AS n FROM delivery_attempts a JOIN delivery_attempts b
			  ON a.channel = b.channel AND b.started_at >= a.started_at AND b.started_at < a.started_at + interval '1 second'
			WHERE a.campaign_id = $1 AND b.campaign_id = $1 GROUP BY a.id) w`, id).Scan(&busiest); err != nil {
		t.Fatal(err)
	}
	if busiest > delivery.MessagesPerSecond {
		t.Errorf("busiest 1 s window of the log holds %d calls", busiest)
	}
	t.Logf("busiest half-open 1 s window: %d calls", busiest)
}
