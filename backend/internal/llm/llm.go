// Package llm is the boundary to the language model: what we ask (prompts and
// JSON schemas), what we accept back (strict parsing), and the Client
// interface that the Claude implementation and the mock both satisfy.
//
// A Client only carries text. Every answer, whether it comes from Claude, the
// mock or a recorded fixture, goes through the same parsing here and the same
// business validation in the segment and content packages (DECISIONS.md D10,
// D12).
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Task is one kind of request.
type Task string

const (
	TaskSegment Task = "segment" // brief -> audience rule + campaign name
	TaskSMS     Task = "sms"     // one SMS draft
	TaskEmail   Task = "email"   // one email draft (subject + body)
)

// Request is one call to the model.
type Request struct {
	Task Task
	// Brief is the marketer's brief, verbatim. Prompt already contains it;
	// it is passed separately so the mock can pick its scripted answer.
	Brief string
	// Attempt is 1 for the first try; higher numbers are repairs.
	Attempt int
	// System holds the stable instructions; it is the cached prefix.
	System string
	// Prompt is the per-request user message.
	Prompt string
	// Schema constrains the answer (Structured Outputs, DECISIONS.md D8).
	Schema json.RawMessage
}

// Response is the model's answer, not yet parsed.
type Response struct {
	Text  string
	Model string
	Usage Usage
}

// Usage is token accounting reported by the API (zero for the mock).
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_input_tokens"`
	CacheWriteTokens int64 `json:"cache_creation_input_tokens"`
}

// Client sends requests to a model.
type Client interface {
	// Complete sends one request and returns the whole answer. When onText
	// is not nil the answer is streamed and onText receives each text
	// fragment as it arrives (DECISIONS.md D11); the returned Text is the
	// concatenation of those fragments.
	Complete(ctx context.Context, req Request, onText func(string)) (Response, error)
}

// RefusalError means the model declined the request (stop_reason "refusal").
// The answer is not parsed and the request is not retried (DECISIONS.md D10).
type RefusalError struct {
	Category    string
	Explanation string
}

func (e *RefusalError) Error() string {
	if e.Category == "" {
		return "the model declined this request"
	}
	return fmt.Sprintf("the model declined this request (category %s)", e.Category)
}

// ErrTruncated means the answer stopped at max_tokens, so the JSON is
// incomplete. It counts as a failed attempt.
var ErrTruncated = errors.New("the answer was cut off before it was complete")
