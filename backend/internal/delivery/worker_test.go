package delivery

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/campaign"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

// The kill test runs the worker in a child process: this test binary,
// started again with these variables set.
const (
	helperDB       = "DELIVERY_TEST_WORKER_DB"
	helperProvider = "DELIVERY_TEST_WORKER_PROVIDER"
)

func TestMain(m *testing.M) {
	if dsn := os.Getenv(helperDB); dsn != "" {
		helperWorker(dsn, os.Getenv(helperProvider))
		return
	}
	os.Exit(m.Run())
}

// helperWorker works until the process is killed.
func helperWorker(dsn, provider string) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		panic(err)
	}
	w := &Worker{
		Queue: NewQueue(pool, time.Second), Client: &Client{BaseURL: provider, HTTP: &http.Client{Timeout: 300 * time.Millisecond}},
		Policy: fastPolicy, Logger: logging.Discard(),
		PerSecond: 100, Senders: 16, Batch: 10, Poll: 20 * time.Millisecond, DrainTimeout: time.Second,
	}
	w.Run(context.Background())
}

// runUntilFinished runs w until the campaign leaves sending, then stops it.
func (r *rig) runUntilFinished(t *testing.T, w *Worker, campaignID int64, limit time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		var status string
		if err := r.pool.QueryRow(context.Background(), `SELECT status FROM campaigns WHERE id = $1`, campaignID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "sending" {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("campaign %d still sending after %s: %s", campaignID, limit, r.summary(t, campaignID))
	return ""
}

func (r *rig) summary(t *testing.T, campaignID int64) string {
	rows, err := r.pool.Query(context.Background(),
		`SELECT status || '=' || count(*) FROM deliveries WHERE campaign_id = $1 GROUP BY status ORDER BY status`, campaignID)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// checkExactlyOnce is the core promise (D5): every delivery ended once,
// every sent delivery is exactly one provider message under its own key,
// and the provider holds nothing else.
func (r *rig) checkExactlyOnce(t *testing.T, campaignID int64, total int64) {
	t.Helper()
	ctx := context.Background()
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE campaign_id = $1 AND status IN ('sent', 'failed')`, campaignID); n != total {
		t.Errorf("%d of %d deliveries are final: %s", n, total, r.summary(t, campaignID))
	}
	// One final attempt row per delivery: nothing ended twice.
	if n := r.count(t, `SELECT count(*) FROM (SELECT delivery_id FROM delivery_attempts WHERE campaign_id = $1 AND outcome <> 'retry'
	                    GROUP BY delivery_id HAVING count(*) <> 1) x`, campaignID); n != 0 {
		t.Errorf("%d deliveries have more than one final attempt", n)
	}
	sent := map[string]string{} // key -> rendered text
	rows, err := r.pool.Query(ctx, `SELECT idempotency_key::text, coalesce(text, body), coalesce(provider_message_id, '')
	                                FROM deliveries WHERE campaign_id = $1 AND status = 'sent'`, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for rows.Next() {
		var key, text, id string
		if err := rows.Scan(&key, &text, &id); err != nil {
			t.Fatal(err)
		}
		sent[key], ids[key] = text, id
	}
	rows.Close()
	perKey := map[string]int{}
	for _, m := range r.provider.Messages() {
		perKey[m.Key]++
		text, ok := sent[m.Key]
		if !ok {
			t.Errorf("provider has message %s (key %s) for no sent delivery", m.ID, m.Key)
			continue
		}
		if m.Text != text || ids[m.Key] != m.ID {
			t.Errorf("provider message %s = %q, delivery has %q / id %s", m.ID, m.Text, text, ids[m.Key])
		}
	}
	for key := range sent {
		if perKey[key] != 1 {
			t.Errorf("key %s: %d provider messages, want 1", key, perKey[key])
		}
	}
}

// TestSampleCampaignEndToEnd: the case's sample brief is drafted with the
// mock LLM, approved, and sent through a provider that fails, throttles,
// answers late and rejects recipients. Everything ends exactly once and
// nobody gets a message on a channel they did not consent to (R4, R9–R12).
func TestSampleCampaignEndToEnd(t *testing.T) {
	r := newRig(t, 5000, config.MockProvider{LatencyMax: 5 * time.Millisecond, ErrorRate: 0.05, ThrottleRate: 0.05,
		SlowRate: 0.02, SlowDelay: time.Second, RejectRate: 0.02, RateLimit: 100})
	ctx := context.Background()
	cities, err := users.NewRepository(r.pool).DistinctCities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := mock.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	svc := &campaign.Service{
		Repo: campaign.NewRepository(r.pool), Generator: &llm.Generator{Client: mock.New(cat, mock.Options{Cities: cities}), AttemptTimeout: time.Second},
		Registry: segment.NewRegistry(cities), Audience: segment.NewCounter(r.pool), LLMMode: "mock", Logger: logging.Discard(),
		Now: func() time.Time { return asOf },
	}
	s, _ := cat.Scenario("cart-reminder-istanbul")
	v, err := svc.Create(ctx, campaign.CreateInput{Brief: s.Brief, Channels: s.Channels, Tone: s.Tone})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StreamDrafts(ctx, v.ID, func(string, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	a, err := svc.Approve(ctx, v.ID)
	if err != nil || a.Deliveries.Total != 232 {
		t.Fatalf("approval = %+v (err %v)", a, err)
	}

	status := r.runUntilFinished(t, r.worker(200*time.Millisecond), v.ID, 30*time.Second)
	if status != "completed" {
		t.Errorf("campaign ended %s, want completed", status)
	}
	r.checkExactlyOnce(t, v.ID, 232)

	// Every message the provider took went to someone who consented on
	// that channel, and nobody without consent got anything.
	keys := []string{}
	for _, m := range r.provider.Messages() {
		keys = append(keys, m.Key)
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries d JOIN users u ON u.id = d.user_id
	                    WHERE d.idempotency_key::text = ANY($1)
	                      AND NOT ((d.channel = 'sms' AND u.sms_opt_in AND u.phone = d.recipient) OR
	                               (d.channel = 'email' AND u.email_opt_in AND u.email = d.recipient))`, keys); n != 0 {
		t.Errorf("%d provider messages went to people without consent", n)
	}
	var smsMsgs, emailMsgs int
	for _, m := range r.provider.Messages() {
		if m.Channel == "sms" {
			smsMsgs++
		} else {
			emailMsgs++
		}
	}
	if smsMsgs > 99 || emailMsgs > 133 {
		t.Errorf("provider took %d SMS and %d emails; at most 99 and 133 people consented", smsMsgs, emailMsgs)
	}
	// Failures are explained, and every call is in the log.
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE campaign_id = $1 AND status = 'failed' AND last_error IS NULL`, v.ID); n != 0 {
		t.Errorf("%d failed deliveries without last_error", n)
	}
	if calls, logged := r.count(t, `SELECT sum(attempts) FROM deliveries WHERE campaign_id = $1`, v.ID),
		r.count(t, `SELECT count(*) FROM delivery_attempts WHERE campaign_id = $1`, v.ID); calls != logged {
		t.Errorf("%d calls made, %d logged", calls, logged)
	}
	st := r.provider.Stats()
	t.Logf("sent %d, failed %d; provider sms %+v; email %+v",
		r.count(t, `SELECT count(*) FROM deliveries WHERE campaign_id = $1 AND status = 'sent'`, v.ID),
		r.count(t, `SELECT count(*) FROM deliveries WHERE campaign_id = $1 AND status = 'failed'`, v.ID), st["sms"], st["email"])
}

// TestAnswerLostAfterAccept: the provider takes the message but answers
// after the worker gave up. The retry carries the same key, so the provider
// replays its answer and holds exactly one message (D5 layer 2).
func TestAnswerLostAfterAccept(t *testing.T) {
	r := newRig(t, 200, config.MockProvider{SlowDelay: 2 * time.Second})
	r.provider.Decide = func(req mockprovider.Request) mockprovider.Outcome {
		if req.Call == 1 {
			return mockprovider.SlowAccept
		}
		return mockprovider.Accept
	}
	id := r.campaign(t, 1, 0)
	if status := r.runUntilFinished(t, r.worker(200*time.Millisecond), id, 10*time.Second); status != "completed" {
		t.Fatalf("campaign ended %s", status)
	}
	msgs := r.provider.Messages()
	if len(msgs) != 1 {
		t.Fatalf("provider holds %d messages, want 1", len(msgs))
	}
	if st := r.provider.Stats()["sms"]; st.Slow != 1 || st.Replayed != 1 {
		t.Errorf("provider stats = %+v, want one slow answer and one replay", st)
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sent' AND attempts = 2 AND provider_message_id = $1`, msgs[0].ID); n != 1 {
		t.Errorf("delivery is not sent after 2 attempts with id %s", msgs[0].ID)
	}
	rows, err := r.pool.Query(context.Background(), `SELECT attempt, coalesce(http_status, 0), outcome, coalesce(error, '')
	                                                 FROM delivery_attempts ORDER BY attempt`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var log []string
	for rows.Next() {
		var attempt, status int
		var outcome, msg string
		_ = rows.Scan(&attempt, &status, &outcome, &msg)
		log = append(log, strings.TrimSpace(strings.Join([]string{outcome, http.StatusText(status), msg}, " ")))
	}
	want := "retry  no answer within 200ms | sent Accepted"
	if got := strings.Join(log, " | "); got != want {
		t.Errorf("attempt log = %q, want %q", got, want)
	}
}

// TestRateLimitPerChannel (R10): with an instant provider the worker is
// limited only by itself. Measured on the moments the limiter let each call
// go (started_at), no half-open 1 s window holds more than 100 calls per
// channel.
func TestRateLimitPerChannel(t *testing.T) {
	r := newRig(t, 1000, config.MockProvider{})
	id := r.campaign(t, 400, 400)
	start := time.Now()
	r.runUntilFinished(t, r.worker(time.Second), id, 30*time.Second)
	elapsed := time.Since(start)
	for _, ch := range content.Channels {
		rows, err := r.pool.Query(context.Background(), `SELECT started_at FROM delivery_attempts WHERE channel = $1`, string(ch))
		if err != nil {
			t.Fatal(err)
		}
		var ts []time.Time
		for rows.Next() {
			var at time.Time
			_ = rows.Scan(&at)
			ts = append(ts, at)
		}
		rows.Close()
		most := maxInWindow(ts, time.Second)
		if len(ts) != 400 || most > 100 {
			t.Errorf("%s: %d calls, busiest 1 s window %d (limit 100)", ch, len(ts), most)
		}
		t.Logf("%s: busiest second %d by our clock, %d as the provider saw it", ch, most, r.provider.Stats()[string(ch)].PeakPerSecond)
	}
	if elapsed < 3990*time.Millisecond {
		t.Errorf("400 messages per channel took %s; at 100/s they need at least 3.99 s", elapsed)
	}
	r.checkExactlyOnce(t, id, 800)
}

// TestRetryMatrix: each recipient gets a scripted sequence of answers and
// ends as the policy says (D6, K7).
func TestRetryMatrix(t *testing.T) {
	r := newRig(t, 200, config.MockProvider{})
	script := map[string]func(call int) mockprovider.Outcome{
		"503-then-ok":   func(c int) mockprovider.Outcome { return pick(c <= 1, mockprovider.ServerError) },
		"503x4-then-ok": func(c int) mockprovider.Outcome { return pick(c <= 4, mockprovider.ServerError) },
		"always-503":    func(int) mockprovider.Outcome { return mockprovider.ServerError },
		"rejected":      func(int) mockprovider.Outcome { return mockprovider.Reject },
		"429-then-ok":   func(c int) mockprovider.Outcome { return pick(c <= 1, mockprovider.Throttle) },
		"always-429":    func(int) mockprovider.Outcome { return mockprovider.Throttle },
		// 429s do not use up the attempts for errors: 6 of them, then 4
		// errors, and the 11th call still goes out.
		"429x6-503x4-then-ok": func(c int) mockprovider.Outcome {
			if c <= 6 {
				return mockprovider.Throttle
			}
			return pick(c <= 10, mockprovider.ServerError)
		},
	}
	r.provider.Decide = func(req mockprovider.Request) mockprovider.Outcome { return script[req.To](req.Call) }
	id := r.campaign(t, len(script), 0)
	i := 0
	for name := range script {
		i++
		if _, err := r.pool.Exec(context.Background(), `UPDATE deliveries SET recipient = $1
		      WHERE id = (SELECT id FROM deliveries WHERE campaign_id = $2 ORDER BY id OFFSET $3 LIMIT 1)`, name, id, i-1); err != nil {
			t.Fatal(err)
		}
	}
	if status := r.runUntilFinished(t, r.worker(time.Second), id, 30*time.Second); status != "completed" {
		t.Errorf("campaign ended %s", status)
	}
	want := map[string]string{
		"503-then-ok":         "sent 2 0 503 service unavailable", // last_error keeps what went wrong before
		"503x4-then-ok":       "sent 5 0 503 service unavailable",
		"always-503":          "failed 5 0 503 service unavailable",
		"rejected":            "failed 1 0 400 invalid recipient",
		"429-then-ok":         "sent 2 1 429 provider busy",
		"always-429":          "failed 10 10 429 provider busy",
		"429x6-503x4-then-ok": "sent 11 6 503 service unavailable",
	}
	rows, err := r.pool.Query(context.Background(), `SELECT recipient, status || ' ' || attempts || ' ' || throttled || ' ' || coalesce(last_error, '')
	                                                 FROM deliveries WHERE campaign_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, got string
		_ = rows.Scan(&name, &got)
		if got != want[name] {
			t.Errorf("%s: %q, want %q", name, got, want[name])
		}
	}
	// The 429's Retry-After and the scheduled retry are in the log.
	if n := r.count(t, `SELECT count(*) FROM delivery_attempts a JOIN deliveries d ON d.id = a.delivery_id
	                    WHERE d.recipient = '429-then-ok' AND a.attempt = 1 AND a.http_status = 429 AND a.outcome = 'retry'
	                      AND a.retry_after BETWEEN interval '1 second' AND interval '2 seconds'
	                      AND a.next_attempt_at >= a.finished_at + interval '50 milliseconds'`); n != 1 {
		t.Error("the 429 attempt is not logged with its Retry-After and next attempt")
	}
	if calls, logged := r.count(t, `SELECT sum(attempts) FROM deliveries`), r.count(t, `SELECT count(*) FROM delivery_attempts`); calls != logged {
		t.Errorf("%d calls made, %d logged", calls, logged)
	}
}

func pick(cond bool, then mockprovider.Outcome) mockprovider.Outcome {
	if cond {
		return then
	}
	return mockprovider.Accept
}

// TestGracefulStopLeavesNothingClaimed: on SIGTERM the worker stops
// claiming and finishes what it holds, so no row waits for its lease. The
// stops land at ten different moments, so some of them hit a claim in
// progress (the claim must complete and hand its rows over).
func TestGracefulStopLeavesNothingClaimed(t *testing.T) {
	r := newRig(t, 2000, config.MockProvider{LatencyMin: 5 * time.Millisecond, LatencyMax: 20 * time.Millisecond})
	r.campaign(t, 1200, 1200)
	for i := range 10 {
		after := 150*time.Millisecond + time.Duration(i)*97*time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), after)
		start := time.Now()
		r.worker(time.Second).Run(ctx)
		cancel()
		if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sending'`); n != 0 {
			t.Errorf("stopped after %s: %d rows still claimed", after, n)
		}
		if took := time.Since(start) - after; took > time.Second {
			t.Errorf("draining took %s", took)
		}
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sent'`); n == 0 || n == 2400 {
		t.Errorf("%d of 2400 sent; the stops should land mid-campaign", n)
	}
}

// TestWorkerKilledMidCampaign: a worker process is killed with SIGKILL in
// the middle of a campaign and a new one is started. Rows the dead worker
// had claimed come back when their lease runs out; every delivery ends
// exactly once and none is lost.
func TestWorkerKilledMidCampaign(t *testing.T) {
	r := newRig(t, 2000, config.MockProvider{LatencyMin: 5 * time.Millisecond, LatencyMax: 20 * time.Millisecond})
	id := r.campaign(t, 500, 500)
	start := func() (*exec.Cmd, *bytes.Buffer) {
		var out bytes.Buffer
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), helperDB+"="+r.dsn(t), helperProvider+"="+r.url)
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &out
	}

	first, out := start()
	deadline := time.Now().Add(20 * time.Second)
	for r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sent'`) < 200 {
		if time.Now().After(deadline) {
			_ = first.Process.Kill()
			t.Fatalf("first worker sent too little; its output:\n%s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := first.Process.Kill(); err != nil { // SIGKILL: no cleanup of any kind
		t.Fatal(err)
	}
	_ = first.Wait()
	inFlight := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sending'`)
	sentAtKill := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sent'`)
	if inFlight == 0 {
		t.Fatal("the killed worker held no claimed rows; the test proves nothing")
	}

	second, out2 := start()
	defer func() {
		_ = second.Process.Kill()
		_ = second.Wait()
	}()
	deadline = time.Now().Add(30 * time.Second)
	for r.count(t, `SELECT count(*) FROM campaigns WHERE id = $1 AND status = 'sending'`, id) == 1 {
		if time.Now().After(deadline) {
			t.Fatalf("campaign not finished after the restart: %s; second worker's output:\n%s", r.summary(t, id), out2)
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.checkExactlyOnce(t, id, 1000)
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'failed'`); n != 0 {
		t.Errorf("%d deliveries failed; the provider never fails here", n)
	}
	st := r.provider.Stats()
	t.Logf("killed with %d sent and %d claimed; provider replayed %d SMS and %d emails after the restart",
		sentAtKill, inFlight, st["sms"].Replayed, st["email"].Replayed)
}
