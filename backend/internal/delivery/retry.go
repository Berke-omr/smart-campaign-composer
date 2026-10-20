package delivery

import (
	"math/rand/v2"
	"time"
)

// Kind classifies a provider answer for the retry policy.
type Kind int

const (
	Sent      Kind = iota // 2xx
	Throttled             // 429: the provider asks us to slow down
	Transient             // 5xx, timeout, network error: may work next time
	Permanent             // any other 4xx: retrying cannot help
)

// Result is the outcome of one provider call.
type Result struct {
	Kind       Kind
	Status     int           // HTTP status; 0 for a timeout or a network error
	RetryAfter time.Duration // the provider's Retry-After on a 429; 0 if absent or unreadable
	MessageID  string        // the provider's id, when sent
	Error      string        // what went wrong, for last_error and the attempt log
}

// Policy is the retry policy (DECISIONS.md D6, with the 429 budget of D24
// K7). A 429 is the provider asking us to slow down, not a fault of the
// message, so it does not use up the attempts for errors; it has its own,
// larger budget so that every delivery still ends.
type Policy struct {
	MaxAttempts  int           // error answers before a message fails (5)
	MaxThrottled int           // 429 answers before a message fails (10)
	Base         time.Duration // full-jitter backoff after the first error (1 s)
	Cap          time.Duration // longest backoff, and longest Retry-After honoured (60 s)
	// RetryAfterJitter spreads the retries that one Retry-After would
	// otherwise send back at the same instant.
	RetryAfterJitter time.Duration
	// Rand returns a number in [0, 1); nil uses math/rand/v2.
	Rand func() float64
}

// DefaultPolicy is the policy of D6 and K7.
var DefaultPolicy = Policy{MaxAttempts: 5, MaxThrottled: 10, Base: time.Second, Cap: 60 * time.Second,
	RetryAfterJitter: 500 * time.Millisecond}

// Decision is what happens to a delivery after a call.
type Decision struct {
	Status string        // StatusSent, StatusRetrying or StatusFailed
	Delay  time.Duration // until the next attempt, when retrying
}

// Delivery statuses (the UI's Queued, Sending, Sent, Retrying, Failed).
const (
	StatusQueued   = "queued"
	StatusSending  = "sending"
	StatusSent     = "sent"
	StatusRetrying = "retrying"
	StatusFailed   = "failed"
)

// Next decides after a call. attempts counts every call started for the
// delivery, this one included (a call cut short by a crash counts as an
// error); throttled counts the calls answered 429, this one included.
func (p Policy) Next(r Result, attempts, throttled int) Decision {
	switch r.Kind {
	case Sent:
		return Decision{Status: StatusSent}
	case Permanent:
		return Decision{Status: StatusFailed}
	case Throttled:
		if throttled >= p.MaxThrottled {
			return Decision{Status: StatusFailed}
		}
		if r.RetryAfter <= 0 {
			return Decision{Status: StatusRetrying, Delay: p.backoff(throttled)}
		}
		return Decision{Status: StatusRetrying, Delay: min(r.RetryAfter, p.Cap) + p.jitter(p.RetryAfterJitter)}
	}
	errors := attempts - throttled
	if errors >= p.MaxAttempts {
		return Decision{Status: StatusFailed}
	}
	return Decision{Status: StatusRetrying, Delay: p.backoff(errors)}
}

// backoff is "full jitter": a uniform wait between 0 and Base·2^(n-1),
// capped. Spreading retries over the whole interval avoids a wave of
// retries that hits the provider at the same instant.
func (p Policy) backoff(n int) time.Duration {
	n = max(n, 1)
	ceiling := p.Cap
	if n < 32 {
		ceiling = min(p.Cap, p.Base<<(n-1))
	}
	return p.jitter(ceiling)
}

func (p Policy) jitter(upTo time.Duration) time.Duration {
	f := p.Rand
	if f == nil {
		f = rand.Float64
	}
	return time.Duration(f() * float64(upTo))
}
