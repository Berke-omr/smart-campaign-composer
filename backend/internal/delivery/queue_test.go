package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/config"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/mockprovider"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
)

var asOf = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// fastPolicy is DefaultPolicy with short waits, so retries finish within a
// test. It caps a provider's Retry-After (1–2 s) at 50 ms too.
var fastPolicy = Policy{MaxAttempts: 5, MaxThrottled: 10, Base: 5 * time.Millisecond, Cap: 50 * time.Millisecond,
	RetryAfterJitter: 10 * time.Millisecond}

// rig is a database with users, a mock provider and the queue.
type rig struct {
	pool     *pgxpool.Pool
	provider *mockprovider.Provider
	url      string // the provider's base URL
	queue    *Queue
}

func newRig(t *testing.T, users int, cfg config.MockProvider) *rig {
	t.Helper()
	pool := testdb.New(t)
	if _, err := seed.Load(context.Background(), pool, seed.Generate(users, 42, asOf), false); err != nil {
		t.Fatal(err)
	}
	p := mockprovider.New(cfg, 7)
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return &rig{pool: pool, provider: p, url: srv.URL, queue: NewQueue(pool, 30*time.Second)}
}

// campaign inserts an approved campaign with deliveries to the first sms
// and email consenting users.
func (r *rig) campaign(t *testing.T, sms, email int) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := r.pool.QueryRow(ctx, `
		INSERT INTO campaigns (name, brief, tone, channels, segment_rule, audience_as_of, llm_mode, status, approved_at)
		VALUES ('test', 'b', 'friendly', ARRAY['sms', 'email'], '{}', now(), 'mock', 'sending', now())
		RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text)
		SELECT $1, id, 'sms', phone, 'Merhaba ' || first_name FROM users WHERE sms_opt_in ORDER BY id LIMIT $2`, id, sms); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO deliveries (campaign_id, user_id, channel, recipient, subject, body)
		SELECT $1, id, 'email', email, first_name || ', sepetin seni bekliyor', 'Merhaba ' || first_name || E',\n\nSepetin seni bekliyor.'
		FROM users WHERE email_opt_in ORDER BY id LIMIT $2`, id, email); err != nil {
		t.Fatal(err)
	}
	return id
}

func (r *rig) worker(timeout time.Duration) *Worker {
	return &Worker{
		Queue: r.queue, Client: &Client{BaseURL: r.url, HTTP: &http.Client{Timeout: timeout}},
		Policy: fastPolicy, Logger: logging.Discard(),
		PerSecond: 100, Senders: 16, Batch: 10, Poll: 20 * time.Millisecond, DrainTimeout: 2 * time.Second,
	}
}

func (r *rig) count(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := r.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// dsn is the connection string of the rig's own database, for a worker in
// another process.
func (r *rig) dsn(t *testing.T) string {
	t.Helper()
	var name string
	if err := r.pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// TestClaimTakesDisjointRows: concurrent claimers never get the same row
// (FOR UPDATE SKIP LOCKED), and together they get every row once.
func TestClaimTakesDisjointRows(t *testing.T) {
	r := newRig(t, 2000, config.MockProvider{})
	r.campaign(t, 1000, 0)
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				jobs, err := r.queue.Claim(context.Background(), content.SMS, 25)
				if err != nil {
					t.Error(err)
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					seen[j.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for id, n := range seen {
		if n != 1 {
			t.Errorf("delivery %d claimed %d times", id, n)
		}
	}
	if len(seen) != 1000 {
		t.Errorf("%d deliveries claimed, want 1000", len(seen))
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sending' AND attempts = 1 AND locked_until > now()`); n != 1000 {
		t.Errorf("%d rows leased with one attempt, want 1000", n)
	}
}

// TestClaimTakesOnlyDueRows: what is waiting for its time, or leased by a
// live worker, or final, is left alone; a row whose lease ran out is taken
// back.
func TestClaimTakesOnlyDueRows(t *testing.T) {
	r := newRig(t, 200, config.MockProvider{})
	id := r.campaign(t, 8, 0)
	ctx := context.Background()
	states := []string{
		`status = 'queued'`, // due
		`status = 'queued', next_attempt_at = now() + interval '1 minute'`,
		`status = 'retrying', attempts = 1`, // due
		`status = 'retrying', attempts = 1, next_attempt_at = now() + interval '1 minute'`,
		`status = 'sending', attempts = 1, locked_until = now() + interval '1 minute'`,
		`status = 'sending', attempts = 1, locked_until = now() - interval '1 second'`, // its worker died
		`status = 'sent', attempts = 1, sent_at = now()`,
		`status = 'failed', attempts = 1`,
	}
	var ids []int64
	rows, _ := r.pool.Query(ctx, `SELECT id FROM deliveries WHERE campaign_id = $1 ORDER BY id`, id)
	for rows.Next() {
		var d int64
		_ = rows.Scan(&d)
		ids = append(ids, d)
	}
	rows.Close()
	for i, set := range states {
		if _, err := r.pool.Exec(ctx, `UPDATE deliveries SET `+set+` WHERE id = $1`, ids[i]); err != nil {
			t.Fatalf("%s: %v", set, err)
		}
	}
	jobs, err := r.queue.Claim(ctx, content.SMS, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]int{}
	for _, j := range jobs {
		got[j.ID] = j.Attempt
	}
	want := map[int64]int{ids[0]: 1, ids[2]: 2, ids[5]: 2}
	if len(got) != len(want) {
		t.Fatalf("claimed %v (id: attempt), want %v", got, want)
	}
	for id, attempt := range want {
		if got[id] != attempt {
			t.Errorf("claimed %v (id: attempt), want %v", got, want)
		}
	}
	if jobs, _ := r.queue.Claim(ctx, content.Email, 100); len(jobs) != 0 {
		t.Errorf("email claim took %d SMS rows", len(jobs))
	}
}

// TestStaleClaimCannotFinish: a worker whose lease ran out while its call
// was in flight must not overwrite the newer claim (the fence).
func TestStaleClaimCannotFinish(t *testing.T) {
	r := newRig(t, 200, config.MockProvider{})
	r.campaign(t, 1, 0)
	ctx := context.Background()
	first, _ := r.queue.Claim(ctx, content.SMS, 1)
	if _, err := r.pool.Exec(ctx, `UPDATE deliveries SET locked_until = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	second, _ := r.queue.Claim(ctx, content.SMS, 1)
	if len(first) != 1 || len(second) != 1 || second[0].Attempt != 2 {
		t.Fatalf("claims = %+v, %+v", first, second)
	}
	call := Call{Result: Result{Kind: Sent, Status: 202, MessageID: "msg_1"}, Started: time.Now(), Finished: time.Now()}
	ok, err := r.queue.Finish(ctx, first[0], call, Decision{Status: StatusSent})
	if err != nil || ok {
		t.Fatalf("stale Finish = %v (err %v), want false", ok, err)
	}
	if n := r.count(t, `SELECT count(*) FROM delivery_attempts`); n != 0 {
		t.Errorf("a stale finish wrote %d attempt rows", n)
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sending' AND attempts = 2`); n != 1 {
		t.Errorf("the newer claim was overwritten")
	}
	if ok, err := r.queue.Finish(ctx, second[0], call, Decision{Status: StatusSent}); err != nil || !ok {
		t.Fatalf("current Finish = %v (err %v)", ok, err)
	}
	if n := r.count(t, `SELECT count(*) FROM deliveries WHERE status = 'sent' AND provider_message_id = 'msg_1' AND locked_until IS NULL`); n != 1 {
		t.Errorf("delivery not recorded as sent")
	}
}

// TestCampaignOutcome (K4): a campaign finishes when every delivery is
// final; it fails only when nothing was sent.
func TestCampaignOutcome(t *testing.T) {
	r := newRig(t, 300, config.MockProvider{})
	ctx := context.Background()
	allFailed, mixed, unfinished := r.campaign(t, 3, 0), r.campaign(t, 3, 0), r.campaign(t, 3, 0)
	set := func(campaign int64, sql string) {
		if _, err := r.pool.Exec(ctx, `UPDATE deliveries SET `+sql+` WHERE campaign_id = $1`, campaign); err != nil {
			t.Fatal(err)
		}
	}
	set(allFailed, `status = 'failed', attempts = 1`)
	set(mixed, `status = 'failed', attempts = 1`)
	if _, err := r.pool.Exec(ctx, `UPDATE deliveries SET status = 'sent', sent_at = now(), attempts = 1
	                                WHERE id = (SELECT min(id) FROM deliveries WHERE campaign_id = $1)`, mixed); err != nil {
		t.Fatal(err)
	}
	set(unfinished, `status = 'failed', attempts = 1`)
	if _, err := r.pool.Exec(ctx, `UPDATE deliveries SET status = 'retrying'
	                                WHERE id = (SELECT min(id) FROM deliveries WHERE campaign_id = $1)`, unfinished); err != nil {
		t.Fatal(err)
	}
	done, err := r.queue.Complete(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{allFailed: "failed", mixed: "completed"}
	if len(done) != len(want) || done[allFailed] != "failed" || done[mixed] != "completed" {
		t.Errorf("Complete = %v, want %v", done, want)
	}
	if n := r.count(t, `SELECT count(*) FROM campaigns WHERE status = 'sending' AND finished_at IS NULL AND id = $1`, unfinished); n != 1 {
		t.Error("a campaign with a retrying message was finished")
	}
	if again, _ := r.queue.Complete(ctx); len(again) != 0 {
		t.Errorf("second Complete = %v, want nothing", again)
	}
}
