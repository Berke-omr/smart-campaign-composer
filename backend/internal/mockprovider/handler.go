// Package mockprovider implements the fake SMS/email provider the worker
// sends to. Like a real one, it is slow at times, fails, throttles and
// rejects some recipients (DECISIONS.md D24, K5), and it deduplicates by
// Idempotency-Key: a repeated key gets the original answer and never makes
// a second message (D5 layer 2).
//
//	POST /v1/sms    {"to": "+905...", "text": "..."}
//	POST /v1/email  {"to": "a@b.c", "subject": "...", "text": "...", "html": "..."}
//	GET  /stats     counters per channel
//
// Everything is kept in memory; a restart forgets it.
package mockprovider

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
)

// Outcome is how the provider answers a request that is not a replay.
type Outcome int

const (
	Accept      Outcome = iota // 202
	SlowAccept                 // stored at once, answered after SlowDelay
	ServerError                // 503, not stored: a retry is processed again
	Throttle                   // 429 with Retry-After, not stored
	Reject                     // 400 invalid recipient, stored: a retry gets it again
)

// Request is what an outcome is decided from. Call is 1 for the first
// request with this Idempotency-Key, 2 for the second, and so on.
type Request struct {
	Channel string
	Key     string
	To      string
	Call    int
}

// Message is a message the provider accepted.
type Message struct {
	ID         string    `json:"id"`
	Channel    string    `json:"channel"`
	Key        string    `json:"idempotency_key"`
	To         string    `json:"to"`
	Text       string    `json:"text"`
	Subject    string    `json:"subject,omitempty"`
	HTML       string    `json:"html,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// Stats are one channel's counters.
type Stats struct {
	Received  int `json:"received"`  // every request
	Accepted  int `json:"accepted"`  // messages recorded (one per key)
	Replayed  int `json:"replayed"`  // repeated keys answered from the store
	Rejected  int `json:"rejected"`  // 400 invalid recipient (first answer only)
	Errors    int `json:"errors"`    // 503
	Throttled int `json:"throttled"` // random 429
	Limited   int `json:"limited"`   // 429 from the rate limit
	Slow      int `json:"slow"`      // accepted but answered late
	// PeakPerSecond is the most requests received in any 1 s window: an
	// independent measurement of the sender's rate limit.
	PeakPerSecond int `json:"peak_per_second"`
}

type stored struct {
	status int
	body   []byte
}

type channelState struct {
	stats    Stats
	received []time.Time // requests in the last second (for the peak)
	admitted []time.Time // requests past the rate limit in the last second
}

// Provider is the fake provider. Its zero value is not usable; use New.
type Provider struct {
	cfg config.MockProvider
	// Decide picks the outcome of a request that is not a replay. New sets
	// the random, rate-driven default; tests replace it to script faults.
	Decide func(Request) Outcome

	mu       sync.Mutex
	rng      *rand.Rand
	results  map[string]stored
	calls    map[string]int
	messages []Message
	channels map[string]*channelState
}

// New returns a provider that misbehaves as cfg says. seed drives the random
// faults and latency.
func New(cfg config.MockProvider, seed uint64) *Provider {
	p := &Provider{
		cfg:      cfg,
		rng:      rand.New(rand.NewPCG(seed, seed)),
		results:  map[string]stored{},
		calls:    map[string]int{},
		channels: map[string]*channelState{"sms": {}, "email": {}},
	}
	p.Decide = p.randomOutcome
	return p
}

// Handler returns the provider's HTTP routes.
func (p *Provider) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/v1/{channel}", p.send)
	r.Get("/stats", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, p.Stats())
	})
	return r
}

// randomOutcome is the default Decide: a recipient is rejected by a hash of
// its address (so always or never), and the other faults are drawn at the
// configured rates.
func (p *Provider) randomOutcome(r Request) Outcome {
	if rejected(r.To, p.cfg.RejectRate) {
		return Reject
	}
	p.mu.Lock()
	u := p.rng.Float64()
	p.mu.Unlock()
	switch {
	case u < p.cfg.ErrorRate:
		return ServerError
	case u < p.cfg.ErrorRate+p.cfg.ThrottleRate:
		return Throttle
	case u < p.cfg.ErrorRate+p.cfg.ThrottleRate+p.cfg.SlowRate:
		return SlowAccept
	}
	return Accept
}

func rejected(to string, rate float64) bool {
	h := fnv.New32a()
	h.Write([]byte(to))
	return float64(h.Sum32()%10_000) < rate*10_000
}

type payload struct {
	To      string `json:"to"`
	Text    string `json:"text"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

func (p *Provider) send(w http.ResponseWriter, r *http.Request) {
	channel := chi.URLParam(r, "channel")
	if channel != "sms" && channel != "email" {
		httpx.WriteError(w, http.StatusNotFound, "unknown channel")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}
	var body payload
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Read to the end: only then does the server notice a client that gives
	// up (and cancel r.Context), which ends a slow answer early.
	_, _ = io.Copy(io.Discard, r.Body)
	if msg := missing(channel, body); msg != "" {
		httpx.WriteError(w, http.StatusBadRequest, msg)
		return
	}
	now := time.Now()
	p.mu.Lock()
	cs := p.channels[channel]
	cs.stats.Received++
	cs.received = append(window(cs.received, now), now)
	cs.stats.PeakPerSecond = max(cs.stats.PeakPerSecond, len(cs.received))
	latency := p.cfg.LatencyMin
	if spread := p.cfg.LatencyMax - p.cfg.LatencyMin; spread > 0 {
		latency += time.Duration(p.rng.Int64N(int64(spread)))
	}
	p.mu.Unlock()

	if !sleep(r, latency) {
		return
	}

	p.mu.Lock()
	if res, ok := p.results[key]; ok {
		cs.stats.Replayed++
		p.mu.Unlock()
		w.Header().Set("Idempotent-Replayed", "true")
		writeRaw(w, res)
		return
	}
	p.calls[key]++
	req := Request{Channel: channel, Key: key, To: body.To, Call: p.calls[key]}
	p.mu.Unlock()

	outcome := p.Decide(req)

	p.mu.Lock()
	if res, ok := p.results[key]; ok { // a concurrent request with this key won
		cs.stats.Replayed++
		p.mu.Unlock()
		w.Header().Set("Idempotent-Replayed", "true")
		writeRaw(w, res)
		return
	}
	switch outcome {
	case ServerError:
		cs.stats.Errors++
		p.mu.Unlock()
		httpx.WriteError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	case Throttle:
		cs.stats.Throttled++
		retryAfter := 1 + p.rng.IntN(2)
		p.mu.Unlock()
		w.Header().Set("Retry-After", fmt.Sprint(retryAfter))
		httpx.WriteError(w, http.StatusTooManyRequests, "provider busy")
		return
	case Reject:
		cs.stats.Rejected++
		res := result(http.StatusBadRequest, map[string]string{"error": "invalid recipient"})
		p.results[key] = res
		p.mu.Unlock()
		writeRaw(w, res)
		return
	}

	// The limit is checked when the request is processed, after its latency.
	at := time.Now()
	cs.admitted = window(cs.admitted, at)
	if p.cfg.RateLimit > 0 && len(cs.admitted) >= p.cfg.RateLimit {
		cs.stats.Limited++
		// Seconds until the oldest request leaves the window, rounded up.
		retryAfter := max(1, int(math.Ceil(cs.admitted[0].Add(time.Second).Sub(at).Seconds())))
		p.mu.Unlock()
		w.Header().Set("Retry-After", fmt.Sprint(retryAfter))
		httpx.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	cs.admitted = append(cs.admitted, at)
	cs.stats.Accepted++
	m := Message{ID: fmt.Sprintf("msg_%06d", len(p.messages)+1), Channel: channel, Key: key, To: body.To,
		Text: body.Text, Subject: body.Subject, HTML: body.HTML, ReceivedAt: now}
	p.messages = append(p.messages, m)
	res := result(http.StatusAccepted, map[string]string{"id": m.ID, "status": "accepted"})
	p.results[key] = res
	slow := outcome == SlowAccept
	if slow {
		cs.stats.Slow++
	}
	p.mu.Unlock()

	if slow && !sleep(r, p.cfg.SlowDelay) {
		return // the client gave up; the message is recorded anyway
	}
	writeRaw(w, res)
}

func missing(channel string, b payload) string {
	switch {
	case b.To == "":
		return `"to" is required`
	case b.Text == "":
		return `"text" is required`
	case channel == "email" && b.Subject == "":
		return `"subject" is required`
	case channel == "sms" && (b.Subject != "" || b.HTML != ""):
		return "an SMS has only text"
	}
	return ""
}

// window drops the times older than one second before now.
func window(ts []time.Time, now time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(now.Add(-time.Second)) {
		i++
	}
	return ts[i:]
}

// sleep waits d, or until the client goes away (then it reports false).
func sleep(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

func result(status int, v any) stored {
	b, _ := json.Marshal(v)
	return stored{status: status, body: append(b, '\n')}
}

func writeRaw(w http.ResponseWriter, res stored) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.status)
	_, _ = w.Write(res.body)
}

// Stats returns the counters per channel.
func (p *Provider) Stats() map[string]Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]Stats{}
	for ch, cs := range p.channels {
		out[ch] = cs.stats
	}
	return out
}

// Messages returns every accepted message, in the order received.
func (p *Provider) Messages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Message(nil), p.messages...)
}
