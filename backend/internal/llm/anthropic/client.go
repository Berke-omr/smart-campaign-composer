// Package anthropic implements llm.Client with the official Anthropic Go SDK.
//
// Requests use Structured Outputs (output_config.format) so the answer is
// constrained to the task's JSON schema (DECISIONS.md D8). Refusals and
// max_tokens cut-offs are turned into typed errors before anything is parsed
// (D10). The client is built only from explicit options: the SDK does not
// read base URLs, tokens or profiles from the environment (D12).
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

// Config is everything the client needs. Nothing else is read.
type Config struct {
	APIKey         string
	Model          string
	RequestTimeout time.Duration // per HTTP attempt
	MaxRetries     int           // SDK retries for 408/409/429/5xx and connection errors
	// Effort overrides the per-task effort levels (used to compare levels in
	// the live smoke test). Nil means the defaults below.
	Effort map[llm.Task]sdk.OutputConfigEffort
	// BaseURL points the client at a test server that replays recorded
	// responses. Empty means the production API.
	BaseURL string
	// HTTPClient lets the smoke test record raw responses. Nil means a plain
	// http.Client.
	HTTPClient *http.Client
}

// DefaultEffort is set explicitly for every request: Sonnet 5.5's default
// is "high", and its levels are recalibrated (DECISIONS.md D13). The smoke
// test compared "low" and "medium" on the sample brief (PLAN.md, P3 log):
// "low" produced the same rule and equally valid drafts, while the
// "medium" segment request thought for 3,500 tokens and was then declined.
var DefaultEffort = map[llm.Task]sdk.OutputConfigEffort{
	llm.TaskSegment: sdk.OutputConfigEffortLow,
	llm.TaskSMS:     sdk.OutputConfigEffortLow,
	llm.TaskEmail:   sdk.OutputConfigEffortLow,
}

// maxTokens bounds each answer. Thinking counts toward it, so it is well
// above the size of the JSON itself.
var maxTokens = map[llm.Task]int64{
	llm.TaskSegment: 8000,
	llm.TaskSMS:     4000,
	llm.TaskEmail:   8000,
}

// Client is an llm.Client backed by the Claude API.
type Client struct {
	api    sdk.Client
	model  string
	effort map[llm.Task]sdk.OutputConfigEffort
}

func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithHTTPClient(httpClient),
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(cfg.MaxRetries),
		option.WithRequestTimeout(cfg.RequestTimeout),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	effort := cfg.Effort
	if effort == nil {
		effort = DefaultEffort
	}
	return &Client{api: sdk.NewClient(opts...), model: cfg.Model, effort: effort}
}

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request, onText func(string)) (llm.Response, error) {
	params, err := c.params(req)
	if err != nil {
		return llm.Response{}, err
	}
	if onText == nil {
		msg, err := c.api.Messages.New(ctx, params)
		if err != nil {
			return llm.Response{}, fmt.Errorf("claude request failed: %w", err)
		}
		return toResponse(msg)
	}

	stream := c.api.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var msg sdk.Message
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return llm.Response{}, fmt.Errorf("claude stream: %w", err)
		}
		if d, ok := ev.AsAny().(sdk.ContentBlockDeltaEvent); ok {
			if t, ok := d.Delta.AsAny().(sdk.TextDelta); ok && t.Text != "" {
				onText(t.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return llm.Response{}, fmt.Errorf("claude stream failed: %w", err)
	}
	return toResponse(&msg)
}

func (c *Client) params(req llm.Request) (sdk.MessageNewParams, error) {
	schema, err := schemaParam(req.Schema)
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	effort, ok := c.effort[req.Task]
	if !ok {
		return sdk.MessageNewParams{}, fmt.Errorf("no effort level for task %q", req.Task)
	}
	return sdk.MessageNewParams{
		Model:     sdk.Model(c.model),
		MaxTokens: maxTokens[req.Task],
		// The system prompt is the stable prefix; marking it lets repeated
		// requests read it from the prompt cache once it is long enough.
		System: []sdk.TextBlockParam{{
			Text:         req.System,
			CacheControl: sdk.NewCacheControlEphemeralParam(),
		}},
		Messages: []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock(req.Prompt))},
		// Adaptive thinking at an explicit effort (DECISIONS.md D13, K3):
		// on Sonnet 5.5 "disabled" is rejected, and "low" keeps thinking
		// short for simple requests.
		Thinking: sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}},
		OutputConfig: sdk.OutputConfigParam{
			Effort: effort,
			Format: sdk.JSONOutputFormatParam{Schema: schema},
		},
	}, nil
}

// schemaParam turns the schema into the SDK's map type without re-encoding
// the nested parts: each top-level value stays raw JSON, so the order of
// "properties" (the order the model writes them in) is preserved.
func schemaParam(raw json.RawMessage) (map[string]any, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("output schema is not a JSON object: %w", err)
	}
	m := make(map[string]any, len(top))
	for k, v := range top {
		m[k] = v
	}
	return m, nil
}

// toResponse checks why the model stopped before anything reads the text.
func toResponse(msg *sdk.Message) (llm.Response, error) {
	switch msg.StopReason {
	case sdk.StopReasonEndTurn:
	case sdk.StopReasonRefusal:
		return llm.Response{}, &llm.RefusalError{
			Category:    string(msg.StopDetails.Category),
			Explanation: msg.StopDetails.Explanation,
		}
	case sdk.StopReasonMaxTokens:
		return llm.Response{}, llm.ErrTruncated
	default:
		return llm.Response{}, fmt.Errorf("unexpected stop reason %q", msg.StopReason)
	}

	var text strings.Builder
	for _, block := range msg.Content {
		// Thinking blocks come first and are skipped; the JSON is in text blocks.
		if t, ok := block.AsAny().(sdk.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if text.Len() == 0 {
		return llm.Response{}, errors.New("the answer has no text")
	}
	return llm.Response{
		Text:  text.String(),
		Model: string(msg.Model),
		Usage: llm.Usage{
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}, nil
}
