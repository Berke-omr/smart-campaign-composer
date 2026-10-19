//go:build live

// Live smoke test against the real Claude API. It is excluded from `make
// test` by the build tag and needs ANTHROPIC_API_KEY. It sends the requests
// below, records each raw HTTP response body under testdata/recorded/ (the
// replay tests in client_test.go serve them back to the real SDK), and prints
// latency and token usage.
//
//	set -a; . ../.env; set +a
//	go test -tags live -run TestLive -v ./internal/llm/anthropic/
//
// Response headers are not recorded (they carry account identifiers).
package anthropic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// recorder keeps the body of the last response it saw.
type recorder struct {
	mu   sync.Mutex
	last bytes.Buffer
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.last.Reset()
	r.mu.Unlock()
	resp.Body = &teeBody{ReadCloser: resp.Body, r: r}
	return resp, nil
}

type teeBody struct {
	io.ReadCloser
	r *recorder
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.r.mu.Lock()
	t.r.last.Write(p[:n])
	t.r.mu.Unlock()
	return n, err
}

// Same cities as the seed, most common first, as the api reads them.
var liveCities = []string{
	"İstanbul", "Ankara", "İzmir", "Bursa", "Antalya", "Adana", "Konya", "Gaziantep",
	"Kocaeli", "Eskişehir", "Mersin", "Kayseri", "Samsun", "Trabzon", "Diyarbakır",
}

const sampleBrief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun."

// Longest values among the sample audience's reachable people (measured on
// the seed with psql; identical for SMS and email).
var sampleLongest = content.Values{
	content.FirstName: "Muhammed Emin",
	content.LastName:  "Montgomery-Fitzgerald",
	content.City:      "İstanbul",
}

func TestLive(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set")
	}
	reg := segment.NewRegistry(liveCities)
	rec := &recorder{}
	newClient := func(effort sdk.OutputConfigEffort) *Client {
		return New(Config{
			APIKey: key, Model: "claude-sonnet-5-5", RequestTimeout: 90 * time.Second, MaxRetries: 0,
			Effort:     map[llm.Task]sdk.OutputConfigEffort{llm.TaskSegment: effort, llm.TaskSMS: effort, llm.TaskEmail: effort},
			HTTPClient: &http.Client{Transport: rec},
		})
	}
	low, medium := newClient(sdk.OutputConfigEffortLow), newClient(sdk.OutputConfigEffortMedium)

	cb := llm.ContentBrief{
		Brief: sampleBrief, Tone: llm.ToneFriendly, CampaignName: "Cart reminder — İstanbul",
		Audience: []string{"City is İstanbul", "Added to cart in the last 30 days", "No purchase in the last 30 days"},
		Longest:  sampleLongest,
	}
	type call struct {
		name   string
		client *Client
		req    llm.Request
		stream bool
	}
	segReq := func(brief string) llm.Request {
		return llm.Request{Task: llm.TaskSegment, Brief: brief, Attempt: 1,
			System: llm.SegmentSystem(reg), Prompt: llm.SegmentPrompt(brief, nil), Schema: llm.SegmentSchema(reg)}
	}
	draftReq := func(ch content.Channel, task llm.Task) llm.Request {
		return llm.Request{Task: task, Brief: sampleBrief, Attempt: 1,
			System: llm.ContentSystem(ch), Prompt: llm.ContentPrompt(ch, cb, nil), Schema: llm.ContentSchema(ch)}
	}
	calls := []call{
		{"segment_sample_low", low, segReq(sampleBrief), false},
		{"segment_sample_medium", medium, segReq(sampleBrief), false},
		{"segment_any_city_low", low, segReq("İstanbul veya Ankara'da yaşayıp son 90 gündür alışveriş yapmamış kullanıcılara bizi özlediniz mi mesajı gönder."), false},
		{"segment_unsupported_low", low, segReq("Son 60 günde sepete ürün ekleyen kadın kullanıcılara yeni sezon duyurusu yap."), false},
		{"sms_sample_low", low, draftReq(content.SMS, llm.TaskSMS), true},
		{"email_sample_low", low, draftReq(content.Email, llm.TaskEmail), true},
		{"sms_sample_medium", medium, draftReq(content.SMS, llm.TaskSMS), true},
		{"email_sample_medium", medium, draftReq(content.Email, llm.TaskEmail), true},
	}

	var inTok, outTok int64
	for _, c := range calls {
		start := time.Now()
		var first time.Duration
		var onText func(string)
		if c.stream {
			onText = func(string) {
				if first == 0 {
					first = time.Since(start)
				}
			}
		}
		resp, err := c.client.Complete(context.Background(), c.req, onText)
		total := time.Since(start)

		ext := ".json"
		if c.stream {
			ext = ".sse"
		}
		rec.mu.Lock()
		body := append([]byte(nil), rec.last.Bytes()...)
		rec.mu.Unlock()
		if werr := os.WriteFile(filepath.Join("testdata", "recorded", c.name+ext), body, 0o644); werr != nil {
			t.Fatal(werr)
		}
		var refusal *llm.RefusalError
		if errors.As(err, &refusal) {
			// A refusal is an outcome to record, not a broken client.
			fmt.Printf("\n== %s  total %.2fs  REFUSED (%s)\n", c.name, total.Seconds(), refusal.Category)
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		inTok += resp.Usage.InputTokens + resp.Usage.CacheReadTokens + resp.Usage.CacheWriteTokens
		outTok += resp.Usage.OutputTokens

		fmt.Printf("\n== %s  total %.2fs", c.name, total.Seconds())
		if c.stream {
			fmt.Printf("  first text %.2fs", first.Seconds())
		}
		fmt.Printf("  tokens in %d (cache read %d, write %d) out %d\n", resp.Usage.InputTokens,
			resp.Usage.CacheReadTokens, resp.Usage.CacheWriteTokens, resp.Usage.OutputTokens)
		fmt.Println(resp.Text)

		switch c.req.Task {
		case llm.TaskSegment:
			p, err := llm.ParseSegment(resp.Text)
			if err != nil {
				t.Errorf("%s: parse: %v", c.name, err)
				continue
			}
			v, err := segment.ParseAndValidate(p.Rule, reg)
			if err != nil {
				t.Errorf("%s: rule rejected: %v", c.name, err)
				continue
			}
			fmt.Printf("-> %q %q unsupported=%q\n", p.CampaignName, segment.Describe(v).Lines, p.UnsupportedCriteria)
		default:
			ch := content.Channel(c.req.Task)
			m, err := llm.ParseDraft(ch, resp.Text)
			if err != nil {
				t.Errorf("%s: parse: %v", c.name, err)
				continue
			}
			for field, s := range map[string]string{"text": m.Text, "subject": m.Subject} {
				if s == "" {
					continue
				}
				filled := s
				for v, val := range sampleLongest {
					filled = strings.ReplaceAll(filled, v.Placeholder(), val)
				}
				fmt.Printf("-> %s: template %d chars, worst case %d chars\n", field,
					utf8.RuneCountInString(s), utf8.RuneCountInString(filled))
			}
		}
	}
	fmt.Printf("\n== total tokens in %d out %d, cost at $2/$10 per MTok: $%.4f\n",
		inTok, outTok, float64(inTok)*2/1e6+float64(outTok)*10/1e6)
}
