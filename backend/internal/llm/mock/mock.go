package mock

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

// Options shape the mock's timing. Tests use zero delays; the api streams
// with small delays so the UI behaves as it does with Claude.
type Options struct {
	// Cities are the registry's cities, for the keyword fallback.
	Cities []string
	// FirstTokenDelay is the pause before the first fragment.
	FirstTokenDelay time.Duration
	// ChunkDelay is the pause between fragments of ChunkRunes characters.
	ChunkDelay time.Duration
	ChunkRunes int
}

// DemoOptions are the delays the api uses.
func DemoOptions(cities []string) Options {
	return Options{Cities: cities, FirstTokenDelay: 400 * time.Millisecond, ChunkDelay: 30 * time.Millisecond, ChunkRunes: 6}
}

// Client is an llm.Client that answers from the catalog.
type Client struct {
	cat   *Catalog
	opts  Options
	calls atomic.Int64
}

func New(cat *Catalog, opts Options) *Client {
	if opts.ChunkRunes <= 0 {
		opts.ChunkRunes = 6
	}
	return &Client{cat: cat, opts: opts}
}

// Calls counts the requests made, so tests can prove that no request was.
func (c *Client) Calls() int64 { return c.calls.Load() }

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request, onText func(string)) (llm.Response, error) {
	c.calls.Add(1)
	a := c.attempt(req)
	switch a.Fault {
	case "refusal":
		return llm.Response{}, &llm.RefusalError{Category: a.Category, Explanation: "scripted refusal (mock LLM)"}
	case "timeout":
		<-ctx.Done() // never answers: the caller's deadline decides
		return llm.Response{}, ctx.Err()
	}

	text := a.Text()
	if err := sleep(ctx, c.opts.FirstTokenDelay); err != nil {
		return llm.Response{}, err
	}
	if onText != nil {
		runes := []rune(text)
		for i := 0; i < len(runes); i += c.opts.ChunkRunes {
			if i > 0 {
				if err := sleep(ctx, c.opts.ChunkDelay); err != nil {
					return llm.Response{}, err
				}
			}
			onText(string(runes[i:min(i+c.opts.ChunkRunes, len(runes))]))
		}
	}
	return llm.Response{Text: text, Model: "mock"}, nil
}

// attempt picks the scripted answer: the scenario's entry for this attempt
// (the last entry repeats), or a keyword-based answer for unknown briefs.
func (c *Client) attempt(req llm.Request) Attempt {
	s, ok := c.cat.Match(req.Brief)
	if !ok {
		return fallback(req, c.opts.Cities)
	}
	list := s.Attempts(req.Task)
	if len(list) == 0 {
		return fallback(req, c.opts.Cities)
	}
	i := min(max(req.Attempt, 1), len(list)) - 1
	return list[i]
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
