package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
)

var asOf = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// now is the api's clock in these tests: 300 ms into 12:00:40. The window
// then ends at 12:00:39 (the current second and one settling second are
// left out) and starts at 12:00:09.
var now = time.Date(2026, 10, 21, 12, 0, 40, 300_000_000, time.UTC)

func at(sec, micro int) time.Time {
	return time.Date(2026, 10, 21, 12, 0, sec, micro*1000, time.UTC)
}

type fixture struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	if _, err := seed.Load(context.Background(), pool, seed.Generate(300, 42, asOf), false); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, pool: pool}
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

// campaign inserts a campaign in status with sms and email queued
// deliveries to consenting users.
func (f *fixture) campaign(channels []string, status string, sms, email int) int64 {
	f.t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO campaigns (name, brief, tone, channels, segment_rule, audience_as_of, llm_mode, status,
		                       approved_at, finished_at)
		VALUES ('test', 'b', 'friendly', $1, '{}', now(), 'mock', $2,
		        CASE WHEN $2 NOT IN ('draft', 'ready') THEN now() END,
		        CASE WHEN $2 IN ('completed', 'failed') THEN now() END)
		RETURNING id`, channels, status).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, text)
		SELECT $1, id, 'sms', phone, 'Merhaba' FROM users WHERE sms_opt_in ORDER BY id LIMIT $2`, id, sms)
	f.exec(`INSERT INTO deliveries (campaign_id, user_id, channel, recipient, subject, body)
		SELECT $1, id, 'email', email, 'Konu', 'Merhaba' FROM users WHERE email_opt_in ORDER BY id LIMIT $2`, id, email)
	return id
}

// move puts n queued deliveries of a channel into status.
func (f *fixture) move(id int64, ch, status string, n int) {
	f.t.Helper()
	f.exec(`
		UPDATE deliveries SET status = $3, attempts = 1,
		       locked_until = CASE WHEN $3 = 'sending' THEN now() + interval '30 seconds' END,
		       sent_at = CASE WHEN $3 = 'sent' THEN now() END
		WHERE id IN (SELECT id FROM deliveries WHERE campaign_id = $1 AND channel = $2 AND status = 'queued'
		             ORDER BY id LIMIT $4)`, id, ch, status, n)
}

// call logs one provider call on the channel's first delivery. code 0 means
// no answer; retryIn is used when the outcome is retry.
func (f *fixture) call(id int64, ch string, started time.Time, code int, outcome, errText string, retryIn time.Duration) {
	f.t.Helper()
	finished := started.Add(50 * time.Millisecond)
	var status *int
	if code != 0 {
		status = &code
	}
	var next *time.Time
	if outcome == "retry" {
		n := finished.Add(retryIn)
		next = &n
	}
	var errPtr *string
	if errText != "" {
		errPtr = &errText
	}
	f.exec(`
		INSERT INTO delivery_attempts (delivery_id, campaign_id, channel, attempt, started_at, finished_at,
		                               http_status, outcome, error, next_attempt_at)
		SELECT d.id, $1, $2, coalesce((SELECT max(attempt) FROM delivery_attempts a WHERE a.delivery_id = d.id), 0) + 1,
		       $3, $4, $5, $6, $7, $8
		FROM deliveries d WHERE d.campaign_id = $1 AND d.channel = $2 ORDER BY d.id LIMIT 1`,
		id, ch, started, finished, status, outcome, errPtr, next)
}

func (f *fixture) snapshot(id int64, now time.Time) Snapshot {
	f.t.Helper()
	s, err := NewReader(f.pool).Snapshot(context.Background(), id, now)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

// TestCountsPerChannelAndStatus: the counts are exactly the table's, per
// channel and status, and done is sent + failed.
func TestCountsPerChannelAndStatus(t *testing.T) {
	f := newFixture(t)
	id := f.campaign([]string{"sms", "email"}, "sending", 20, 30)
	f.move(id, "sms", "sending", 2)
	f.move(id, "sms", "retrying", 3)
	f.move(id, "sms", "sent", 8)
	f.move(id, "sms", "failed", 2)
	f.move(id, "email", "sending", 4)
	f.move(id, "email", "retrying", 1)
	f.move(id, "email", "sent", 13)
	f.move(id, "email", "failed", 2)
	other := f.campaign([]string{"sms", "email"}, "sending", 7, 7) // must not leak into id's counts
	f.move(other, "sms", "sent", 7)

	s := f.snapshot(id, now)

	type counts struct{ Total, Done, Queued, Sending, Retrying, Sent, Failed int }
	want := map[content.Channel]counts{
		"sms":   {Total: 20, Done: 10, Queued: 5, Sending: 2, Retrying: 3, Sent: 8, Failed: 2},
		"email": {Total: 30, Done: 15, Queued: 10, Sending: 4, Retrying: 1, Sent: 13, Failed: 2},
	}
	for ch, w := range want {
		c := s.Channels[ch]
		if c == nil {
			t.Fatalf("channel %s missing", ch)
		}
		got := counts{Total: c.Total, Done: c.Done, Queued: c.Queued, Sending: c.Sending, Retrying: c.Retrying,
			Sent: c.Sent, Failed: c.Failed}
		if got != w {
			t.Errorf("%s = %+v, want %+v", ch, got, w)
		}
		if c.Limit != 100 {
			t.Errorf("%s limit = %d, want 100", ch, c.Limit)
		}
	}
	if s.Total != 50 || s.Done != 25 || s.Status != "sending" || s.ApprovedAt == nil || s.FinishedAt != nil {
		t.Errorf("total %d done %d status %s approved %v finished %v; want 50, 25, sending, set, null",
			s.Total, s.Done, s.Status, s.ApprovedAt, s.FinishedAt)
	}

	// The same numbers straight from the table.
	rows, err := f.pool.Query(context.Background(),
		`SELECT channel, status, count(*) FROM deliveries WHERE campaign_id = $1 GROUP BY 1, 2`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ch, status string
		var n int
		if err := rows.Scan(&ch, &status, &n); err != nil {
			t.Fatal(err)
		}
		c := s.Channels[content.Channel(ch)]
		got := map[string]int{"queued": c.Queued, "sending": c.Sending, "retrying": c.Retrying, "sent": c.Sent, "failed": c.Failed}[status]
		if got != n {
			t.Errorf("%s %s: snapshot %d, table %d", ch, status, got, n)
		}
	}
}

func TestOnlySelectedChannels(t *testing.T) {
	f := newFixture(t)
	id := f.campaign([]string{"email"}, "sending", 0, 5)
	s := f.snapshot(id, now)
	if len(s.Channels) != 1 || s.Channels["email"] == nil || s.Channels["email"].Total != 5 {
		t.Fatalf("channels = %+v, want email only with 5", s.Channels)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"sms"`) {
		t.Errorf("JSON mentions sms: %s", b)
	}
}

// TestPerSecondSeries pins the window: whole seconds, the lower bound
// inclusive, the upper bound exclusive, the newest second left out until it
// settles, and zeros where nothing was sent.
func TestPerSecondSeries(t *testing.T) {
	f := newFixture(t)
	id := f.campaign([]string{"sms", "email"}, "sending", 5, 5)
	f.call(id, "sms", at(8, 999_999), 202, "sent", "", 0) // before the window
	f.call(id, "sms", at(9, 0), 202, "sent", "", 0)       // first second, exactly on its start
	f.call(id, "sms", at(20, 100), 202, "sent", "", 0)    // 11th second: three calls,
	f.call(id, "sms", at(20, 500_000), 503, "retry", "503 service unavailable", time.Second)
	f.call(id, "sms", at(20, 999_999), 429, "retry", "429 provider busy", 2*time.Second) // retries and 429s count
	f.call(id, "sms", at(38, 999_999), 202, "sent", "", 0)                               // newest settled second
	f.call(id, "sms", at(39, 0), 202, "sent", "", 0)                                     // settling: not shown yet
	f.call(id, "sms", at(40, 100_000), 202, "sent", "", 0)                               // current second
	f.call(id, "email", at(38, 500_000), 202, "sent", "", 0)
	f.call(id, "email", at(38, 600_000), 202, "sent", "", 0)

	s := f.snapshot(id, now)

	if !s.Window.From.Equal(at(9, 0)) || !s.Window.To.Equal(at(39, 0)) {
		t.Fatalf("window = %v .. %v, want 12:00:09 .. 12:00:39", s.Window.From, s.Window.To)
	}
	sms := make([]int, WindowSeconds)
	sms[0], sms[11], sms[29] = 1, 3, 1
	email := make([]int, WindowSeconds)
	email[29] = 2
	if got := s.Channels["sms"].PerSecond; !reflect.DeepEqual(got, sms) {
		t.Errorf("sms per second = %v\nwant %v", got, sms)
	}
	if got := s.Channels["email"].PerSecond; !reflect.DeepEqual(got, email) {
		t.Errorf("email per second = %v\nwant %v", got, email)
	}
	if s.Channels["sms"].Rate != 1 || s.Channels["email"].Rate != 2 {
		t.Errorf("rate sms %d email %d, want 1 and 2 (the newest settled second)", s.Channels["sms"].Rate, s.Channels["email"].Rate)
	}

	// One second later the settling second is shown and the window moves.
	s = f.snapshot(id, now.Add(time.Second))
	if got := s.Channels["sms"].PerSecond; got[28] != 1 || got[29] != 1 || got[10] != 3 {
		t.Errorf("a second later: sms per second = %v, want 3 at 10, 1 at 28 and 29", got)
	}
}

// TestFinishedCampaignKeepsItsLastWindow: once the campaign is finished the
// window ends with its last call, however long ago that was, and the rate
// is 0.
func TestFinishedCampaignKeepsItsLastWindow(t *testing.T) {
	f := newFixture(t)
	id := f.campaign([]string{"sms"}, "completed", 3, 0)
	f.move(id, "sms", "sent", 3)
	f.call(id, "sms", at(0, 0), 202, "sent", "", 0)
	f.call(id, "sms", at(1, 0), 202, "sent", "", 0)
	f.call(id, "sms", at(30, 250_000), 202, "sent", "", 0) // the last call

	s := f.snapshot(id, now.Add(10*time.Minute))

	if !s.Window.From.Equal(at(1, 0)) || !s.Window.To.Equal(at(31, 0)) {
		t.Fatalf("window = %v .. %v, want 12:00:01 .. 12:00:31", s.Window.From, s.Window.To)
	}
	want := make([]int, WindowSeconds)
	want[0], want[29] = 1, 1
	if got := s.Channels["sms"].PerSecond; !reflect.DeepEqual(got, want) {
		t.Errorf("per second = %v\nwant %v", got, want)
	}
	if s.Channels["sms"].Rate != 0 || !s.Final() || s.FinishedAt == nil || s.Done != 3 {
		t.Errorf("rate %d final %v finished %v done %d; want 0, true, set, 3",
			s.Channels["sms"].Rate, s.Final(), s.FinishedAt, s.Done)
	}
}

// TestRecentIssues: the newest calls that sent nothing, newest first, at
// most MaxIssues, classified like the retry policy, with the exact delay
// of the retry.
func TestRecentIssues(t *testing.T) {
	f := newFixture(t)
	id := f.campaign([]string{"sms", "email"}, "sending", 5, 5)
	for i := range 8 { // older issues, pushed out of the list
		f.call(id, "email", at(1+i, 0), 503, "retry", "503 service unavailable", time.Second)
	}
	f.call(id, "sms", at(20, 0), 0, "retry", "no answer within 3s", 1500*time.Millisecond)
	f.call(id, "email", at(21, 0), 400, "failed", "400 invalid recipient", 0)
	f.call(id, "sms", at(22, 0), 503, "failed", "503 service unavailable", 0)
	f.call(id, "email", at(23, 0), 429, "retry", "429 provider busy", 2100*time.Millisecond)
	f.call(id, "sms", at(24, 0), 202, "sent", "", 0) // not an issue

	s := f.snapshot(id, now)

	if len(s.RecentIssues) != MaxIssues {
		t.Fatalf("%d issues, want %d", len(s.RecentIssues), MaxIssues)
	}
	ms := func(v int64) *int64 { return &v }
	code := func(v int) *int { return &v }
	want := []Issue{
		{At: at(23, 50_000), Channel: "email", Kind: "throttled", Code: code(429), Outcome: "retry", RetryInMS: ms(2100), Attempt: 10, Error: "429 provider busy"},
		{At: at(22, 50_000), Channel: "sms", Kind: "provider_error", Code: code(503), Outcome: "failed", Attempt: 2, Error: "503 service unavailable"},
		{At: at(21, 50_000), Channel: "email", Kind: "rejected", Code: code(400), Outcome: "failed", Attempt: 9, Error: "400 invalid recipient"},
		{At: at(20, 50_000), Channel: "sms", Kind: "no_answer", Outcome: "retry", RetryInMS: ms(1500), Attempt: 1, Error: "no answer within 3s"},
	}
	for i, w := range want {
		if got := s.RecentIssues[i]; !reflect.DeepEqual(got, w) {
			t.Errorf("issue %d = %s\nwant      %s", i, show(got), show(w))
		}
	}
	for _, is := range s.RecentIssues[4:] {
		if is.Kind != "provider_error" || is.Channel != "email" {
			t.Errorf("older issue = %s, want the 503s", show(is))
		}
	}
	if last := s.RecentIssues[MaxIssues-1]; !last.At.Equal(at(3, 50_000)) {
		t.Errorf("oldest listed issue at %v, want the third 503 (12:00:03.05)", last.At)
	}
}

func show(is Issue) string {
	b, _ := json.Marshal(is)
	return string(b)
}

func TestUnknownCampaign(t *testing.T) {
	f := newFixture(t)
	if _, err := NewReader(f.pool).Snapshot(context.Background(), 999, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestKind(t *testing.T) {
	code := func(v int) *int { return &v }
	for _, tt := range []struct {
		code *int
		want string
	}{{nil, "no_answer"}, {code(429), "throttled"}, {code(400), "rejected"}, {code(404), "rejected"},
		{code(500), "provider_error"}, {code(503), "provider_error"}} {
		if got := kind(tt.code); got != tt.want {
			t.Errorf("kind(%v) = %s, want %s", tt.code, got, tt.want)
		}
	}
}
