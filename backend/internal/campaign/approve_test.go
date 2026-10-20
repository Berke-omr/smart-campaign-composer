package campaign

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm/mock"
)

// ready creates the scenario's campaign and drafts its messages.
func (f *fixture) ready(t *testing.T, scenario string) int64 {
	t.Helper()
	ctx := context.Background()
	v, err := f.svc.Create(ctx, f.brief(t, scenario))
	if err != nil {
		t.Fatal(err)
	}
	var rec recorder
	if err := f.svc.StreamDrafts(ctx, v.ID, rec.emit); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(ctx, v.ID)
	if err != nil || got.Status != StatusReady {
		t.Fatalf("campaign is %s (err %v), want ready", got.Status, err)
	}
	return v.ID
}

func (f *fixture) count(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestApprovalFreezesTheAudience: the deliveries are exactly the people the
// preview counted per channel (R4), each message rendered for its recipient.
func TestApprovalFreezesTheAudience(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	id := f.ready(t, "cart-reminder-istanbul")

	a, err := f.svc.Approve(ctx, id)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// The preview's numbers at the same anchor: 99 SMS + 133 email.
	if a.Status != StatusSending || a.ApprovedAt.IsZero() || a.Deliveries != (DeliveryCounts{SMS: 99, Email: 133, Total: 232}) {
		t.Fatalf("approval = %+v", a)
	}
	v, err := f.svc.Get(ctx, id)
	if err != nil || v.Status != StatusSending || v.ApprovedAt == nil || v.FinishedAt != nil {
		t.Fatalf("view after approval: status %s, approved_at %v, finished_at %v (err %v)", v.Status, v.ApprovedAt, v.FinishedAt, err)
	}

	// Nobody receives a message on a channel without consent and contact data.
	if n := f.count(t, `
		SELECT count(*) FROM deliveries d JOIN users u ON u.id = d.user_id
		WHERE (d.channel = 'sms'   AND NOT (u.sms_opt_in   AND d.recipient = u.phone)) OR
		      (d.channel = 'email' AND NOT (u.email_opt_in AND d.recipient = u.email))`); n != 0 {
		t.Errorf("%d deliveries go to people without consent, or to an address that is not theirs", n)
	}
	if n := f.count(t, `SELECT count(*) FROM deliveries WHERE status <> 'queued' OR attempts <> 0`); n != 0 {
		t.Errorf("%d deliveries are not freshly queued", n)
	}

	// Every message is the stored template rendered with its recipient's data.
	msgs := map[content.Channel]content.Message{}
	for _, ch := range content.Channels {
		msgs[ch] = *v.Messages[ch].Message
	}
	rows, err := f.pool.Query(ctx, `
		SELECT d.channel, coalesce(d.text, ''), coalesce(d.subject, ''), coalesce(d.body, ''), u.first_name, u.last_name, u.city
		FROM deliveries d JOIN users u ON u.id = d.user_id WHERE d.campaign_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var ch content.Channel
		var got content.Message
		var first, last, city string
		if err := rows.Scan(&ch, &got.Text, &got.Subject, &got.Body, &first, &last, &city); err != nil {
			t.Fatal(err)
		}
		want := content.RenderMessage(msgs[ch], content.Values{content.FirstName: first, content.LastName: last, content.City: city})
		if got != want || strings.Contains(got.Text+got.Subject+got.Body, "{{") {
			t.Fatalf("%s delivery for %s = %+v, want %+v", ch, first, got, want)
		}
		checked++
	}
	if checked != 232 {
		t.Errorf("checked %d deliveries, want 232", checked)
	}
}

// TestConcurrentApprovalsCreateOneSet: double clicks and concurrent
// requests create one set of deliveries, and every caller gets the same
// answer (D5 layer 1).
func TestConcurrentApprovalsCreateOneSet(t *testing.T) {
	f := setup(t, mock.Options{})
	id := f.ready(t, "cart-reminder-istanbul")

	const n = 10
	results := make([]Approval, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.svc.Approve(context.Background(), id)
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("approval %d: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("approval %d = %+v, approval 0 = %+v", i, results[i], results[0])
		}
	}
	if got := f.count(t, `SELECT count(*) FROM deliveries WHERE campaign_id = $1`, id); got != 232 {
		t.Errorf("%d deliveries after %d approvals, want 232", got, n)
	}

	// Approving later still answers the same, and writes nothing.
	again, err := f.svc.Approve(context.Background(), id)
	if err != nil || again != results[0] {
		t.Errorf("approving again = %+v (err %v), want %+v", again, err, results[0])
	}
}

// TestApprovalRevalidatesAgainstTheFrozenAudience: someone with a longer name
// joins the audience after the draft was validated. The SMS would exceed 160
// characters for them, so the approval is refused and nothing is written
// (D15, K1).
func TestApprovalRevalidatesAgainstTheFrozenAudience(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	id := f.ready(t, "cart-reminder-istanbul")

	// İstanbul, cart a day before the anchor, never purchased, SMS consent.
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO users (first_name, last_name, phone, city, sms_opt_in, last_cart_add_at, created_at)
		VALUES ($1, 'Uzun', '+905000000001', 'İstanbul', true, $2::timestamptz - interval '1 day', $2::timestamptz - interval '2 days')`,
		strings.Repeat("Ş", 80), asOf); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.Approve(ctx, id)
	var stale *StaleContentError
	if !errors.As(err, &stale) || stale.Channel != content.SMS || stale.Report.Valid ||
		stale.Report.Errors[0].Code != content.CodeTooLong || stale.Report.LongestValues[content.FirstName] != strings.Repeat("Ş", 80) {
		t.Fatalf("err = %v, want the SMS rejected as too long for the new longest name", err)
	}
	if n := f.count(t, `SELECT count(*) FROM deliveries`); n != 0 {
		t.Errorf("%d deliveries written by a refused approval", n)
	}
	if v, _ := f.svc.Get(ctx, id); v.Status != StatusReady || v.ApprovedAt != nil {
		t.Errorf("status %s, approved_at %v after a refused approval; want ready and none", v.Status, v.ApprovedAt)
	}
}

func TestApprovalNeedsAReadyCampaignWithRecipients(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()

	draft, err := f.svc.Create(ctx, f.brief(t, "cart-reminder-istanbul"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Approve(ctx, draft.ID); !errors.Is(err, ErrNotReady) {
		t.Errorf("draft: err = %v, want ErrNotReady", err)
	}
	if _, err := f.svc.Approve(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}

	// Everyone withdraws consent after the draft: nobody to send to.
	id := f.ready(t, "cart-reminder-istanbul")
	if _, err := f.pool.Exec(ctx, `UPDATE users SET sms_opt_in = false, email_opt_in = false`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Approve(ctx, id); !errors.Is(err, ErrNoRecipients) {
		t.Errorf("nobody reachable: err = %v, want ErrNoRecipients", err)
	}
	if v, _ := f.svc.Get(ctx, id); v.Status != StatusReady {
		t.Errorf("status %s, want ready", v.Status)
	}
}

// TestContentIsLockedAfterApproval (A12).
func TestContentIsLockedAfterApproval(t *testing.T) {
	f := setup(t, mock.Options{})
	ctx := context.Background()
	id := f.ready(t, "cart-reminder-istanbul")
	if _, err := f.svc.Approve(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.EditMessage(ctx, id, content.SMS, content.Message{Text: "Merhaba"}); !errors.Is(err, ErrLocked) {
		t.Errorf("edit: err = %v, want ErrLocked", err)
	}
	if err := f.svc.StreamDrafts(ctx, id, func(string, any) error { return nil }); !errors.Is(err, ErrLocked) {
		t.Errorf("stream: err = %v, want ErrLocked", err)
	}
}
