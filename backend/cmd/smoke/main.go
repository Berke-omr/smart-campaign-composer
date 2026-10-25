// Command smoke runs a few scenarios of docs/TEST-SCENARIOS.md end to end
// through the HTTP API of a running stack and checks the answers.
//
// It refuses to start unless the api answers with the mock LLM, so it never
// makes a paid call. Every run creates new campaigns and sends their messages
// to the mock provider. Only numbers that do not move with the clock are
// pinned (the default seed: 5,000 users, seed 42); the others are checked
// against each other. Run it with `make smoke`.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	base := os.Getenv("SMOKE_API_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	cl := &client{base: strings.TrimRight(base, "/"), http: &http.Client{}}

	if err := requireMock(ctx, cl); err != nil {
		fmt.Println("smoke:", err)
		os.Exit(1)
	}

	c := &checker{}
	start := time.Now()
	var sending []watch
	for _, scenario := range []func(context.Context, *client, *checker) *watch{s05, s11, s18, s17, s03} {
		if w := scenario(ctx, cl, c); w != nil {
			sending = append(sending, *w)
		}
	}
	for _, w := range sending {
		followUntilDone(ctx, cl, c, w)
	}

	fmt.Printf("\n%d checks passed, %d failed (%.1f s)\n", c.passed, c.failed, time.Since(start).Seconds())
	if c.failed > 0 {
		os.Exit(1)
	}
}

func requireMock(ctx context.Context, cl *client) error {
	var cfg struct {
		LLM struct {
			Mode string `json:"mode"`
		} `json:"llm"`
	}
	if err := cl.expect(ctx, http.MethodGet, "/api/config", nil, http.StatusOK, &cfg); err != nil {
		return err
	}
	if cfg.LLM.Mode != "mock" {
		return fmt.Errorf("the api runs with LLM mode %q; the smoke test only runs against the mock LLM (start the stack with LLM_PROVIDER=mock)", cfg.LLM.Mode)
	}
	fmt.Println("api LLM mode: mock")
	return nil
}

// S05: repair on screen, approve twice. City and order count do not depend on
// the clock, so the audience is pinned.
func s05(ctx context.Context, cl *client, c *checker) *watch {
	const sc = "S05"
	v, err := cl.create(ctx, "İzmir'de en az 5 siparişi olan müşterilere teşekkür mesajı ve %15 indirim gönder.", []string{"sms", "email"}, "friendly")
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "people / sms / email", v.Audience.counts(), "79 / 45 / 49")
	c.equal(sc, "unsupported criteria", len(v.UnsupportedCriteria), 0)

	d, err := cl.draft(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "sms draft states", d.states["sms"], "writing1 validating1 fixing2 validating2 valid2")
	c.equal(sc, "email draft states", d.states["email"], "writing1 validating1 valid1")
	c.equal(sc, "status after drafting", d.status, "ready")

	first, err := cl.approve(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "approve: status", first.Status, "sending")
	c.equal(sc, "approve: deliveries sms / email / total", first.Deliveries.counts(), "45 / 49 / 94")

	again, err := cl.approve(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "approve again: same campaign and deliveries",
		fmt.Sprintf("%d %s", again.CampaignID, again.Deliveries.counts()),
		fmt.Sprintf("%d %s", v.ID, first.Deliveries.counts()))
	return &watch{scenario: sc, id: v.ID, total: 94}
}

// S11: zero audience. No seeded user has spent more than 20,000 TL.
func s11(ctx context.Context, cl *client, c *checker) *watch {
	const sc = "S11"
	v, err := cl.create(ctx, "Toplam harcaması 20.000 TL'yi geçen müşterilere özel davet maili gönder.", []string{"email"}, "professional")
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "people / sms / email", v.Audience.counts(), "0 / 0 / 0")

	d, err := cl.draft(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "email draft states", d.states["email"], "writing1 validating1 valid1")
	c.equal(sc, "status after drafting", d.status, "ready")

	status, body, err := cl.do(ctx, http.MethodPost, fmt.Sprintf("/api/campaigns/%d/approve", v.ID), nil)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "approve: answer", fmt.Sprintf("%d %s", status, errorText(body)),
		"422 no one in this audience can be reached on the selected channels")

	after, err := cl.campaign(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "status after the refused approval", after.Status, "ready")
	return nil
}

// S18: the SMS needs a manual edit, approval is refused until it is fixed.
func s18(ctx context.Context, cl *client, c *checker) *watch {
	const sc = "S18"
	v, err := cl.create(ctx, "[test] Every SMS answer is 200 characters long, so the SMS needs a manual edit.", []string{"sms", "email"}, "friendly")
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	d, err := cl.draft(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "sms draft states", d.states["sms"],
		"writing1 validating1 fixing2 validating2 fixing3 validating3 needs_manual_edit3")
	c.equal(sc, "email draft states", d.states["email"], "writing1 validating1 valid1")
	c.equal(sc, "status after drafting", d.status, "draft")

	status, body, err := cl.do(ctx, http.MethodPost, fmt.Sprintf("/api/campaigns/%d/approve", v.ID), nil)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "approve before the edit", fmt.Sprintf("%d %s", status, errorText(body)),
		"409 campaign is not ready to send: every selected channel needs a valid message")

	edited, err := cl.editSMS(ctx, v.ID, s17ValidText)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "edit: source / status", edited.Message.Source+" / "+edited.Status, "edited / ready")

	a, err := cl.approve(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	want := v.Audience.SMSReachable + v.Audience.EmailReachable
	c.equal(sc, "approve: deliveries = reachable per channel", a.Deliveries.counts(),
		fmt.Sprintf("%d / %d / %d", v.Audience.SMSReachable, v.Audience.EmailReachable, want))
	return &watch{scenario: sc, id: v.ID, total: want}
}

const s17ValidText = "{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! Gece yarısına kadar geçerli. Kod: FLASH30"

// S17: every content error code an SMS edit can get, then a valid edit.
func s17(ctx context.Context, cl *client, c *checker) *watch {
	const sc = "S17"
	v, err := cl.create(ctx, "Sadece bugün geçerli %30 flash sale! Son 30 günde alışveriş yapan müşterilere SMS ile duyur, acil bir dil olsun.", []string{"sms"}, "urgent")
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	d, err := cl.draft(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "sms draft states", d.states["sms"], "writing1 validating1 valid1")

	for _, e := range []struct{ text, code string }{
		{"{{first_name}}, sadece bugün geçerli FLASH SALE başladı! Tüm ürünlerde %30 indirim seni bekliyor. Kampanya gece yarısı bitiyor, sakın kaçırma. Kod: FLASH30. Hemen alışverişe başla!", "too_long"},
		{"{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! 🔥 Kod: FLASH30", "emoji"},
		{"{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! Kod: {{coupon_code}}", "unknown_variable"},
	} {
		status, body, err := cl.do(ctx, http.MethodPut, fmt.Sprintf("/api/campaigns/%d/messages/sms", v.ID), map[string]string{"text": e.text})
		if err != nil {
			c.fail(sc, err)
			return nil
		}
		c.equal(sc, "rejected edit ("+e.code+")", fmt.Sprintf("%d %s", status, strings.Join(reportCodes(body), ",")), "422 "+e.code)
	}

	edited, err := cl.editSMS(ctx, v.ID, s17ValidText)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "valid edit: source / status", edited.Message.Source+" / "+edited.Status, "edited / ready")
	return nil
}

// S03: a criterion no field can express is reported, not guessed; SMS only.
func s03(ctx context.Context, cl *client, c *checker) *watch {
	const sc = "S03"
	v, err := cl.create(ctx, "Son 60 günde sepete ürün ekleyen kadın kullanıcılara yeni sezon ürünlerini SMS ile duyur.", []string{"sms"}, "playful")
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "unsupported criteria", strings.Join(v.UnsupportedCriteria, ", "), "kadın kullanıcılar")
	c.equal(sc, "channels", strings.Join(v.Channels, ","), "sms")

	d, err := cl.draft(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "sms draft states", d.states["sms"], "writing1 validating1 valid1")
	c.equal(sc, "no email draft", d.states["email"], "")

	a, err := cl.approve(ctx, v.ID)
	if err != nil {
		c.fail(sc, err)
		return nil
	}
	c.equal(sc, "approve: deliveries sms / email / total", a.Deliveries.counts(),
		fmt.Sprintf("%d / 0 / %d", v.Audience.SMSReachable, v.Audience.SMSReachable))
	return &watch{scenario: sc, id: v.ID, total: v.Audience.SMSReachable}
}

// watch is an approved campaign whose live stream is followed to the end.
type watch struct {
	scenario string
	id       int64
	total    int
}

func followUntilDone(ctx context.Context, cl *client, c *checker, w watch) {
	var last snapshot
	var done struct {
		Status string `json:"status"`
	}
	err := cl.stream(ctx, fmt.Sprintf("/api/campaigns/%d/events", w.id), func(event string, data []byte) (bool, error) {
		switch event {
		case "delivery.snapshot":
			return false, json.Unmarshal(data, &last)
		case "delivery.done":
			return true, json.Unmarshal(data, &done)
		}
		return false, nil
	})
	if err != nil {
		c.fail(w.scenario, fmt.Errorf("live stream: %w", err))
		return
	}
	c.equal(w.scenario, "final status", done.Status, "completed")
	c.equal(w.scenario, "done / total", fmt.Sprintf("%d / %d", last.Done, last.Total), fmt.Sprintf("%d / %d", w.total, w.total))
	for ch, s := range last.Channels {
		c.equal(w.scenario, ch+": sent + failed = total", s.Sent+s.Failed, s.Total)
	}
}

// checker prints one line per check and counts the results.
type checker struct{ passed, failed int }

func (c *checker) equal(scenario, what string, got, want any) {
	g, w := fmt.Sprint(got), fmt.Sprint(want)
	if g == w {
		c.passed++
		fmt.Printf("ok    %s  %s: %s\n", scenario, what, g)
		return
	}
	c.failed++
	fmt.Printf("FAIL  %s  %s: got %q, want %q\n", scenario, what, g, w)
}

func (c *checker) fail(scenario string, err error) {
	c.failed++
	fmt.Printf("FAIL  %s  %v (scenario stopped)\n", scenario, err)
}

// API types: only the fields the checks read (docs/API.md).

type audience struct {
	Total          int `json:"total"`
	SMSReachable   int `json:"sms_reachable"`
	EmailReachable int `json:"email_reachable"`
}

func (a audience) counts() string {
	return fmt.Sprintf("%d / %d / %d", a.Total, a.SMSReachable, a.EmailReachable)
}

type campaignView struct {
	ID                  int64    `json:"id"`
	Status              string   `json:"status"`
	Channels            []string `json:"channels"`
	Audience            audience `json:"audience"`
	UnsupportedCriteria []string `json:"unsupported_criteria"`
}

type deliveries struct {
	SMS   int `json:"sms"`
	Email int `json:"email"`
	Total int `json:"total"`
}

func (d deliveries) counts() string { return fmt.Sprintf("%d / %d / %d", d.SMS, d.Email, d.Total) }

type approval struct {
	CampaignID int64      `json:"campaign_id"`
	Status     string     `json:"status"`
	Deliveries deliveries `json:"deliveries"`
}

type editResult struct {
	Message struct {
		Source string `json:"source"`
	} `json:"message"`
	Status string `json:"status"`
}

type snapshot struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Channels map[string]struct {
		Total  int `json:"total"`
		Sent   int `json:"sent"`
		Failed int `json:"failed"`
	} `json:"channels"`
}

// draftResult is one draft stream read to its end: each channel's states as
// "writing1 validating1 valid1", and the campaign status from draft.done.
type draftResult struct {
	states map[string]string
	status string
}

type client struct {
	base string
	http *http.Client
}

func (cl *client) create(ctx context.Context, brief string, channels []string, tone string) (campaignView, error) {
	var v campaignView
	body := map[string]any{"brief": brief, "channels": channels, "tone": tone}
	err := cl.expect(ctx, http.MethodPost, "/api/campaigns", body, http.StatusCreated, &v)
	return v, err
}

func (cl *client) campaign(ctx context.Context, id int64) (campaignView, error) {
	var v campaignView
	err := cl.expect(ctx, http.MethodGet, fmt.Sprintf("/api/campaigns/%d", id), nil, http.StatusOK, &v)
	return v, err
}

func (cl *client) approve(ctx context.Context, id int64) (approval, error) {
	var a approval
	err := cl.expect(ctx, http.MethodPost, fmt.Sprintf("/api/campaigns/%d/approve", id), nil, http.StatusOK, &a)
	return a, err
}

func (cl *client) editSMS(ctx context.Context, id int64, text string) (editResult, error) {
	var r editResult
	err := cl.expect(ctx, http.MethodPut, fmt.Sprintf("/api/campaigns/%d/messages/sms", id), map[string]string{"text": text}, http.StatusOK, &r)
	return r, err
}

func (cl *client) draft(ctx context.Context, id int64) (draftResult, error) {
	res := draftResult{states: map[string]string{}}
	err := cl.stream(ctx, fmt.Sprintf("/api/campaigns/%d/draft/stream", id), func(event string, data []byte) (bool, error) {
		switch event {
		case "draft.state":
			var s struct {
				Channel string `json:"channel"`
				Attempt int    `json:"attempt"`
				State   string `json:"state"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				return false, err
			}
			step := fmt.Sprintf("%s%d", s.State, s.Attempt)
			if prev := res.states[s.Channel]; prev != "" {
				step = prev + " " + step
			}
			res.states[s.Channel] = step
		case "draft.error":
			return false, fmt.Errorf("draft.error: %s", data)
		case "draft.done":
			var d struct {
				Status string `json:"status"`
			}
			err := json.Unmarshal(data, &d)
			res.status = d.Status
			return true, err
		}
		return false, nil
	})
	return res, err
}

// expect sends a request and decodes the answer into out when the status is the expected one.
func (cl *client) expect(ctx context.Context, method, path string, body any, want int, out any) error {
	status, raw, err := cl.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if status != want {
		return fmt.Errorf("%s %s: status %d, want %d: %s", method, path, status, want, bytes.TrimSpace(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

func (cl *client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cl.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// stream reads server-sent events and passes each one to handle until handle
// reports the end or the stream closes (which is an error here: both streams
// end with their own done event).
func (cl *client) stream(ctx context.Context, path string, handle func(event string, data []byte) (bool, error)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cl.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := cl.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, bytes.TrimSpace(raw))
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var event string
	var data []byte
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if event != "" {
				done, err := handle(event, data)
				if err != nil || done {
					return err
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("GET " + path + ": stream ended before its done event")
}

func errorText(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return string(bytes.TrimSpace(body))
	}
	return e.Error
}

func reportCodes(body []byte) []string {
	var e struct {
		Report struct {
			Errors []struct {
				Code string `json:"code"`
			} `json:"errors"`
		} `json:"report"`
	}
	if json.Unmarshal(body, &e) != nil {
		return []string{string(bytes.TrimSpace(body))}
	}
	codes := make([]string, 0, len(e.Report.Errors))
	for _, x := range e.Report.Errors {
		codes = append(codes, x.Code)
	}
	return codes
}
