package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
)

// Message is what one provider call carries.
type Message struct {
	Key     string // Idempotency-Key: the delivery's UUID (D5, K8)
	Channel content.Channel
	To      string
	Text    string // the SMS, or the email's plain-text body
	Subject string // email only
}

// Client calls the SMS/email provider.
type Client struct {
	BaseURL string
	// HTTP must have a Timeout: a call that never answers is a timeout, and
	// the delivery is retried with the same key.
	HTTP *http.Client
}

type smsRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

type emailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// Send makes one call. Every outcome, failures included, is a Result the
// retry policy decides on; only a cancelled ctx (the worker stopping) is
// reported as an error, because then nothing should be recorded.
func (c *Client) Send(ctx context.Context, m Message) (Result, error) {
	var payload any = smsRequest{To: m.To, Text: m.Text}
	if m.Channel == content.Email {
		payload = emailRequest{To: m.To, Subject: m.Subject, Text: m.Text, HTML: content.EmailHTML(m.Subject, m.Text)}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/"+string(m.Channel), bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", m.Key)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return Result{Kind: Transient, Error: fmt.Sprintf("no answer within %s", c.HTTP.Timeout)}, nil
		}
		return Result{Kind: Transient, Error: "network error: " + unwrapAll(err).Error()}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var answer struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)

	r := Result{Status: resp.StatusCode}
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
		r.Kind, r.MessageID = Sent, answer.ID
		return r, nil
	case s == http.StatusTooManyRequests:
		r.Kind, r.RetryAfter = Throttled, retryAfter(resp.Header.Get("Retry-After"), time.Now())
	case s >= 400 && s < 500:
		r.Kind = Permanent
	default:
		r.Kind = Transient
	}
	r.Error = strings.TrimSpace(fmt.Sprintf("%d %s", resp.StatusCode, answer.Error))
	return r, nil
}

// retryAfter reads a Retry-After header: seconds, or an HTTP date. An
// absent or unreadable value is 0, and the policy falls back to backoff.
func retryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	if s, err := strconv.Atoi(h); err == nil {
		return max(0, time.Duration(s)*time.Second)
	}
	if t, err := http.ParseTime(h); err == nil {
		return max(0, t.Sub(now))
	}
	return 0
}

// unwrapAll returns the innermost error, without the URL that *url.Error
// wraps around it.
func unwrapAll(err error) error {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err
		}
		err = inner
	}
}
