package campaign

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

var asOf = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type fixture struct {
	pool    *pgxpool.Pool
	svc     *Service
	mock    *mock.Client
	catalog *mock.Catalog
}

func setup(t *testing.T, opts mock.Options) *fixture {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, seed.Generate(5000, 42, asOf), false); err != nil {
		t.Fatal(err)
	}
	cities, err := users.NewRepository(pool).DistinctCities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := mock.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	opts.Cities = cities
	m := mock.New(cat, opts)
	svc := &Service{
		Repo:      NewRepository(pool),
		Generator: &llm.Generator{Client: m, AttemptTimeout: 100 * time.Millisecond},
		Registry:  segment.NewRegistry(cities),
		Audience:  segment.NewCounter(pool),
		LLMMode:   "mock",
		Logger:    logging.Discard(),
		Now:       func() time.Time { return asOf },
	}
	return &fixture{pool: pool, svc: svc, mock: m, catalog: cat}
}

func (f *fixture) brief(t *testing.T, id string) CreateInput {
	t.Helper()
	s, ok := f.catalog.Scenario(id)
	if !ok {
		t.Fatalf("no scenario %s", id)
	}
	return CreateInput{Brief: s.Brief, Channels: s.Channels, Tone: s.Tone}
}

type sse struct {
	event string
	data  any
}

type recorder struct {
	mu     sync.Mutex
	events []sse
}

func (r *recorder) emit(event string, data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, sse{event, data})
	return nil
}

func (r *recorder) terminal() map[content.Channel]llm.Event {
	out := map[content.Channel]llm.Event{}
	for _, e := range r.events {
		if ev, ok := e.data.(llm.Event); ok && ev.State.Terminal() {
			out[ev.Channel] = ev
		}
	}
	return out
}

func (r *recorder) last() sse { return r.events[len(r.events)-1] }

func TestSampleBriefEndToEnd(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()

	v, err := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v.Name != "Cart reminder — İstanbul" || v.Status != StatusDraft || v.LLM.Mode != "mock" {
		t.Errorf("view = %s / %s / %s", v.Name, v.Status, v.LLM.Mode)
	}
	// The case's numbers (P1/P2) at the fixed anchor, with consent exclusions.
	a := v.Audience
	if a.Total != 177 || a.SMSReachable != 99 || a.EmailReachable != 133 || a.SMSExcluded != 78 || a.EmailExcluded != 44 || a.ChannelMessages != 232 {
		t.Errorf("audience = %+v", a.Reach)
	}
	if strings.Join(a.Description.Lines, "; ") != "City is İstanbul; Added to cart in the last 30 days; No purchase in the last 30 days" {
		t.Errorf("description = %q", a.Description.Lines)
	}
	if v.Messages[content.SMS] != nil || v.Messages[content.Email] != nil {
		t.Error("messages exist before drafting")
	}

	var rec recorder
	if err := f.svc.StreamDrafts(ctx, v.ID, rec.emit); err != nil {
		t.Fatalf("StreamDrafts: %v", err)
	}
	for _, ch := range content.Channels {
		if e := rec.terminal()[ch]; e.State != llm.StateValid {
			t.Errorf("%s ended %s", ch, e.State)
		}
	}
	if last := rec.last(); last.event != EventDone || last.data.(map[string]any)["status"] != StatusReady {
		t.Errorf("last event = %+v, want draft.done with status ready", last)
	}

	got, err := f.svc.Get(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	sms := got.Messages[content.SMS]
	if got.Status != StatusReady || sms == nil || sms.State != llm.StateValid || sms.Source != SourceGenerated || !sms.Report.Valid {
		t.Fatalf("stored SMS = %+v, status %s", sms, got.Status)
	}
	// Worst case uses the longest name among SMS recipients.
	if sms.Report.LongestValues[content.FirstName] != "Muhammed Emin" || sms.Report.Preview.Sample.Text == sms.Message.Text {
		t.Errorf("report = %+v", sms.Report)
	}
}

func TestReopeningAFinishedDraftMakesNoModelCalls(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	var first recorder
	_ = f.svc.StreamDrafts(ctx, v.ID, first.emit)
	calls := f.mock.Calls()

	var again recorder
	if err := f.svc.StreamDrafts(ctx, v.ID, again.emit); err != nil {
		t.Fatal(err)
	}
	if f.mock.Calls() != calls {
		t.Errorf("reopening made %d model calls", f.mock.Calls()-calls)
	}
	if len(again.events) != 3 || len(again.terminal()) != 2 || again.last().event != EventDone {
		t.Errorf("replay = %+v, want two stored states and draft.done", again.events)
	}
}

func TestManualEditAfterFailedDraft(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, err := f.svc.Create(ctx, f.brief(t, "fault-long-sms"))
	if err != nil {
		t.Fatal(err)
	}
	var rec recorder
	if err := f.svc.StreamDrafts(ctx, v.ID, rec.emit); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.Get(ctx, v.ID)
	sms := got.Messages[content.SMS]
	if got.Status != StatusDraft || sms.State != llm.StateNeedsManualEdit || sms.Attempts != 3 || len(sms.Problems) == 0 {
		t.Fatalf("after drafting: status %s, SMS %+v", got.Status, sms)
	}
	original := sms.Message.Text

	// An over-limit edit is rejected and nothing changes.
	_, _, err = f.svc.EditMessage(ctx, v.ID, content.SMS, content.Message{Text: strings.Repeat("ş", 161)})
	var ce *ContentError
	if !errors.As(err, &ce) || ce.Report.Errors[0].Code != content.CodeTooLong {
		t.Fatalf("over-limit edit: err = %v", err)
	}
	if got, _ := f.svc.Get(ctx, v.ID); got.Messages[content.SMS].Message.Text != original || got.Status != StatusDraft {
		t.Error("a rejected edit changed the stored message or the status")
	}

	// A valid edit is stored, and with both channels valid the campaign is ready.
	mv, status, err := f.svc.EditMessage(ctx, v.ID, content.SMS, content.Message{Text: "Merhaba {{first_name}}, sepetin seni bekliyor!"})
	if err != nil {
		t.Fatalf("valid edit: %v", err)
	}
	if status != StatusReady || mv.State != llm.StateValid || mv.Source != SourceEdited || !mv.Report.Valid || mv.Attempts != 3 {
		t.Errorf("after edit: status %s, message %+v", status, mv)
	}
}

func TestRefusedTwiceStoresNoContent(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, f.brief(t, "fault-refusal"))
	var rec recorder
	_ = f.svc.StreamDrafts(ctx, v.ID, rec.emit)

	got, _ := f.svc.Get(ctx, v.ID)
	sms, email := got.Messages[content.SMS], got.Messages[content.Email]
	if sms.State != llm.StateNeedsManualEdit || sms.Message != nil || !strings.Contains(sms.Problems[0], "general_harms") {
		t.Errorf("SMS = %+v", sms)
	}
	if email.State != llm.StateValid || email.Attempts != 2 {
		t.Errorf("email = %+v", email)
	}
	if got.Status != StatusDraft {
		t.Errorf("status = %s", got.Status)
	}
}

// TestViewHasLongestValuesPerChannel: the campaign view carries, for each
// selected channel, the values the worst case is computed with, also for a
// channel without a message (the UI's counter needs them before the first
// report exists). They equal what the validator itself uses.
func TestViewHasLongestValuesPerChannel(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, err := f.svc.Create(ctx, f.brief(t, "fault-refusal"))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Audience.LongestValues) != 2 || v.Audience.LongestValues[content.SMS][content.FirstName] == "" {
		t.Fatalf("before drafting: longest values = %v", v.Audience.LongestValues)
	}
	var rec recorder
	_ = f.svc.StreamDrafts(ctx, v.ID, rec.emit)
	got, _ := f.svc.Get(ctx, v.ID)

	// Email has a message: the view's values are its report's values.
	if want := got.Messages[content.Email].Report.LongestValues; !maps.Equal(got.Audience.LongestValues[content.Email], want) {
		t.Errorf("email: view %v, report %v", got.Audience.LongestValues[content.Email], want)
	}
	// The SMS was declined twice and has none: a rejected edit's report shows what the validator uses.
	if got.Messages[content.SMS].Message != nil {
		t.Fatal("expected an SMS without content")
	}
	_, _, err = f.svc.EditMessage(ctx, v.ID, content.SMS, content.Message{Text: ""})
	var ce *ContentError
	if !errors.As(err, &ce) {
		t.Fatalf("empty edit: err = %v", err)
	}
	if !maps.Equal(got.Audience.LongestValues[content.SMS], ce.Report.LongestValues) {
		t.Errorf("sms: view %v, validator %v", got.Audience.LongestValues[content.SMS], ce.Report.LongestValues)
	}

	// When every name is shorter than the fallback, the fallback is the worst case.
	if _, err := f.pool.Exec(ctx, "UPDATE users SET first_name = 'Ali'"); err != nil {
		t.Fatal(err)
	}
	short, _ := f.svc.Get(ctx, v.ID)
	if got := short.Audience.LongestValues[content.SMS][content.FirstName]; got != content.Fallbacks[content.FirstName] {
		t.Errorf("short names: first_name = %q, want the fallback %q", got, content.Fallbacks[content.FirstName])
	}

	// Only the selected channels are listed.
	sms, err := f.svc.Create(ctx, f.brief(t, "new-season-sms"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sms.Audience.LongestValues[content.Email]; ok || len(sms.Audience.LongestValues) != 1 {
		t.Errorf("SMS-only campaign: longest values = %v", sms.Audience.LongestValues)
	}
}

func TestCreateRejectsBadInputWithoutCallingTheModel(t *testing.T) {
	f := setup(t, mock.Options{})
	for _, in := range []CreateInput{
		{Brief: "  ", Channels: []content.Channel{content.SMS}},
		{Brief: strings.Repeat("ş", MaxBriefRunes+1), Channels: []content.Channel{content.SMS}},
		{Brief: "x", Channels: nil},
		{Brief: "x", Channels: []content.Channel{"whatsapp"}},
		{Brief: "x", Channels: []content.Channel{content.SMS}, Tone: "sarcastic"},
	} {
		_, err := f.svc.Create(context.Background(), in)
		var ie *InputError
		if !errors.As(err, &ie) {
			t.Errorf("%+v: err = %v, want InputError", in, err)
		}
	}
	if f.mock.Calls() != 0 {
		t.Errorf("%d model calls for invalid input", f.mock.Calls())
	}
}

func TestCreateNormalizesChannelsAndTone(t *testing.T) {
	f := setup(t, mock.Options{})
	in := f.brief(t, "cart-reminder-istanbul")
	in.Channels, in.Tone = []content.Channel{content.Email, content.SMS, content.Email}, ""
	v, err := f.svc.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Channels) != 2 || v.Channels[0] != content.SMS || v.Tone != llm.ToneFriendly {
		t.Errorf("channels %v, tone %s", v.Channels, v.Tone)
	}
}

type failing struct{}

func (failing) Complete(context.Context, llm.Request, func(string)) (llm.Response, error) {
	return llm.Response{}, llm.ErrTruncated
}

func TestNoCampaignWithoutAValidRule(t *testing.T) {
	f := setup(t, mock.Options{})
	f.svc.Generator = &llm.Generator{Client: failing{}}
	_, err := f.svc.Create(context.Background(), CreateInput{Brief: "x", Channels: []content.Channel{content.SMS}})
	var se *llm.SegmentError
	if !errors.As(err, &se) || se.Attempts != 3 {
		t.Fatalf("err = %v", err)
	}
	var n int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM campaigns`).Scan(&n)
	if n != 0 {
		t.Errorf("%d campaigns stored", n)
	}
}

func TestOneStreamPerCampaign(t *testing.T) {
	f := setup(t, mock.Options{ChunkDelay: 2 * time.Millisecond, ChunkRunes: 1})
	f.svc.Generator.AttemptTimeout = 0
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))

	started := make(chan struct{})
	done := make(chan error)
	go func() {
		var once sync.Once
		done <- f.svc.StreamDrafts(ctx, v.ID, func(string, any) error {
			once.Do(func() { close(started) })
			return nil
		})
	}()
	<-started
	if err := f.svc.StreamDrafts(ctx, v.ID, func(string, any) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("second stream: err = %v, want ErrBusy", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientLeavingStoresNothing(t *testing.T) {
	f := setup(t, mock.Options{ChunkDelay: 5 * time.Millisecond, ChunkRunes: 1})
	f.svc.Generator.AttemptTimeout = 0
	v, _ := f.svc.Create(context.Background(), f.brief(t, "cart-reminder-istanbul"))

	var logs lockedBuffer
	f.svc.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	var rec recorder
	_ = f.svc.StreamDrafts(ctx, v.ID, rec.emit)

	got, _ := f.svc.Get(context.Background(), v.ID)
	for ch, m := range got.Messages {
		if m != nil {
			t.Errorf("%s stored after the client left: %+v", ch, m)
		}
	}
	// The unfinished channels are not even attempted to be saved: a save
	// with the dead request would fail and report a spurious error.
	for _, e := range rec.events {
		if e.event == EventError {
			t.Errorf("draft.error sent after the client left: %+v", e.data)
		}
	}
	if strings.Contains(logs.String(), "saving draft failed") {
		t.Errorf("a save was attempted after the client left:\n%s", logs.String())
	}
}

// lockedBuffer is a log sink that is safe for concurrent writes.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestLockedOnceSending(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET status = 'sending', approved_at = now() WHERE id = $1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StreamDrafts(ctx, v.ID, func(string, any) error { return nil }); !errors.Is(err, ErrLocked) {
		t.Errorf("stream: err = %v", err)
	}
	if _, _, err := f.svc.EditMessage(ctx, v.ID, content.SMS, content.Message{Text: "x"}); !errors.Is(err, ErrLocked) {
		t.Errorf("edit: err = %v", err)
	}
	if _, err := f.svc.Get(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err = %v", err)
	}
}

func TestSchemaConstraints(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	for name, sql := range map[string]string{
		"valid SMS without text":  `INSERT INTO campaign_messages (campaign_id, channel, state, source, attempts) VALUES ($1, 'sms', 'valid', 'generated', 1)`,
		"SMS with a subject":      `INSERT INTO campaign_messages (campaign_id, channel, state, text, subject, source, attempts) VALUES ($1, 'sms', 'valid', 't', 's', 'generated', 1)`,
		"unknown state":           `INSERT INTO campaign_messages (campaign_id, channel, state, text, source, attempts) VALUES ($1, 'sms', 'writing', 't', 'generated', 1)`,
		"unknown campaign status": `UPDATE campaigns SET status = 'approved' WHERE id = $1`,
		"unknown channel":         `UPDATE campaigns SET channels = ARRAY['sms','push'] WHERE id = $1`,
	} {
		if _, err := f.pool.Exec(ctx, sql, v.ID); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestList(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	_, _ = f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	_, _ = f.svc.Create(ctx, f.brief(t, "weekend-sale-email"))
	list, err := f.svc.List(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "Weekend sale — all subscribers" || list[1].Audience.Total != 177 {
		t.Errorf("list = %+v", list)
	}
	// Email only: total channel messages counts the email channel alone.
	if list[0].Audience.ChannelMessages != list[0].Audience.EmailReachable {
		t.Errorf("email-only messages = %+v", list[0].Audience)
	}
}
