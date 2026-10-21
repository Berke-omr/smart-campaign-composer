package metrics

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/delivery"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
)

// scrape fetches h's text exposition as a map from series (name with its
// labels, exactly as printed) to value.
func scrape(t *testing.T, h http.Handler) map[string]float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics answered %d: %s", rec.Code, rec.Body)
	}
	return parse(t, rec.Body)
}

func parse(t *testing.T, r io.Reader) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			t.Fatalf("bad sample line %q", line)
		}
		out[line[:i]] = v
	}
	return out
}

func TestLLMMetrics(t *testing.T) {
	reg := NewRegistry()
	m := NewLLM(reg)
	m.Observe(llm.AttemptStat{Task: llm.TaskSMS, Attempt: 1, Duration: 300 * time.Millisecond, Result: "invalid"})
	m.Observe(llm.AttemptStat{Task: llm.TaskSMS, Attempt: 2, Duration: 700 * time.Millisecond, Result: "valid"})
	m.Observe(llm.AttemptStat{Task: llm.TaskSegment, Attempt: 1, Duration: 3 * time.Second, Result: "valid"})

	got := scrape(t, Handler(reg, logging.Discard()))
	want := map[string]float64{
		`scc_llm_attempts_total{result="invalid",task="sms"}`:             1,
		`scc_llm_attempts_total{result="valid",task="sms"}`:               1,
		`scc_llm_attempts_total{result="valid",task="segment"}`:           1,
		`scc_llm_attempts_total{result="timeout",task="email"}`:           0, // every series exists from the start
		`scc_llm_repairs_total{task="sms"}`:                               1,
		`scc_llm_repairs_total{task="segment"}`:                           0,
		`scc_llm_attempt_duration_seconds_count{task="sms"}`:              2,
		`scc_llm_attempt_duration_seconds_sum{task="sms"}`:                1,
		`scc_llm_attempt_duration_seconds_bucket{task="sms",le="0.5"}`:    1,
		`scc_llm_attempt_duration_seconds_bucket{task="segment",le="2"}`:  0,
		`scc_llm_attempt_duration_seconds_bucket{task="segment",le="4"}`:  1,
		`scc_llm_attempt_duration_seconds_bucket{task="email",le="+Inf"}`: 0,
	}
	for series, w := range want {
		if v, ok := got[series]; !ok || v != w {
			t.Errorf("%s = %v (present %v), want %v", series, v, ok, w)
		}
	}
	if _, ok := got["go_goroutines"]; !ok {
		t.Error("go_goroutines missing: the runtime collector is not registered")
	}
	if problems, err := testutil.GatherAndLint(reg); err != nil || len(problems) > 0 {
		t.Errorf("lint: %v %+v", err, problems)
	}
}

// TestWorkerCountersMatchTheAttemptLog runs the real worker against a
// provider with faults: the call counters equal the attempt log grouped the
// same way, and the deliveries gauge equals the deliveries table.
func TestWorkerCountersMatchTheAttemptLog(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, seed.Generate(600, 42, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)), false); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO campaigns (name, brief, tone, channels, segment_rule, audience_as_of, llm_mode, status, approved_at)
		VALUES ('m', 'b', 'friendly', ARRAY['sms', 'email'], '{}', now(), 'mock', 'sending', now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text)
		 SELECT $1, id, 'sms', phone, 'Merhaba' FROM users WHERE sms_opt_in ORDER BY id LIMIT 150`,
		`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, subject, body)
		 SELECT $1, id, 'email', email, 'Konu', 'Merhaba' FROM users WHERE email_opt_in ORDER BY id LIMIT 150`,
	} {
		if _, err := pool.Exec(ctx, sql, id); err != nil {
			t.Fatal(err)
		}
	}

	// 503s, 429s, rejections and answers that come after the timeout.
	provider := mockprovider.New(config.MockProvider{LatencyMin: time.Millisecond, LatencyMax: 5 * time.Millisecond,
		ErrorRate: 0.08, ThrottleRate: 0.08, SlowRate: 0.03, SlowDelay: 300 * time.Millisecond, RejectRate: 0.03}, 5)
	provSrv := httptest.NewServer(provider.Handler())
	defer provSrv.Close()

	reg := NewRegistry()
	m := NewWorker(reg)
	RegisterDeliveries(reg, pool)
	w := &delivery.Worker{
		Queue:  delivery.NewQueue(pool, 30*time.Second),
		Client: &delivery.Client{BaseURL: provSrv.URL, HTTP: &http.Client{Timeout: 100 * time.Millisecond}},
		Policy: delivery.Policy{MaxAttempts: 5, MaxThrottled: 10, Base: 5 * time.Millisecond, Cap: 50 * time.Millisecond,
			RetryAfterJitter: 10 * time.Millisecond},
		Logger: logging.Discard(), PerSecond: delivery.MessagesPerSecond, Senders: 16, Batch: 10,
		Poll: 20 * time.Millisecond, DrainTimeout: 2 * time.Second, Observe: m.Observe,
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); w.Run(runCtx) }()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "sending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the campaign did not finish within 60 s")
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	<-done

	got := scrape(t, Handler(reg, logging.Discard()))

	// Calls: the counters and the log, grouped by channel, outcome and status.
	rows, err := pool.Query(ctx, `
		SELECT channel, outcome, coalesce(http_status::text, 'none'), count(*)
		FROM delivery_attempts GROUP BY 1, 2, 3`)
	if err != nil {
		t.Fatal(err)
	}
	logged, series := map[string]float64{}, map[string]bool{}
	for rows.Next() {
		var ch, outcome, code string
		var n float64
		if err := rows.Scan(&ch, &outcome, &code, &n); err != nil {
			t.Fatal(err)
		}
		s := fmt.Sprintf(`scc_provider_calls_total{channel="%s",code="%s",result="%s"}`, ch, code, outcome)
		series[s] = true
		if got[s] != n {
			t.Errorf("%s = %v, the log has %v", s, got[s], n)
		}
		logged[ch] += n
	}
	rows.Close()
	for s := range got {
		if strings.HasPrefix(s, "scc_provider_calls_total{") && !series[s] {
			t.Errorf("%s = %v has no rows in the log", s, got[s])
		}
	}
	for _, code := range []string{"429", "503", "400", "none"} { // the faults really happened
		found := false
		for s := range series {
			found = found || strings.Contains(s, `code="`+code+`"`)
		}
		if !found {
			t.Errorf("no call answered %s; the test does not exercise it", code)
		}
	}
	for ch, n := range logged {
		if c := got[fmt.Sprintf(`scc_provider_call_duration_seconds_count{channel="%s"}`, ch)]; c != n {
			t.Errorf("%s: %v durations observed, %v calls logged", ch, c, n)
		}
	}

	// Deliveries: the gauge and the table, every combination.
	for _, ch := range []string{"sms", "email"} {
		for _, status := range deliveryStatuses {
			var n float64
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE channel = $1 AND status = $2`, ch, status).Scan(&n); err != nil {
				t.Fatal(err)
			}
			s := fmt.Sprintf(`scc_deliveries{channel="%s",status="%s"}`, ch, status)
			if v, ok := got[s]; !ok || v != n {
				t.Errorf("%s = %v (present %v), the table has %v", s, v, ok, n)
			}
		}
	}
	if problems, err := testutil.GatherAndLint(reg); err != nil || len(problems) > 0 {
		t.Errorf("lint: %v %+v", err, problems)
	}
}

// TestMetricsWithoutTheDatabase: when the database is down, /metrics still
// answers with everything else; only the deliveries gauge is missing.
func TestMetricsWithoutTheDatabase(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	pool, err := pgxpool.New(context.Background(), "postgres://scc:scc@"+addr+"/scc?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	reg := NewRegistry()
	NewLLM(reg)
	RegisterDeliveries(reg, pool)
	got := scrape(t, Handler(reg, logging.Discard()))
	if _, ok := got[`scc_llm_repairs_total{task="sms"}`]; !ok {
		t.Error("the LLM metrics are missing")
	}
	for s := range got {
		if strings.HasPrefix(s, "scc_deliveries{") {
			t.Errorf("%s served without a database", s)
		}
	}
}
