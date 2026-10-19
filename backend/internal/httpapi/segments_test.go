package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/logging"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

var fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

type fakeCounter struct {
	audience segment.Audience
	err      error
	gotAsOf  time.Time
	calls    int
}

func (f *fakeCounter) Count(_ context.Context, _ segment.Validated, asOf time.Time) (segment.Audience, error) {
	f.calls++
	f.gotAsOf = asOf
	return f.audience, f.err
}

func segmentDeps(c *fakeCounter) Deps {
	return Deps{
		Logger:   logging.Discard(),
		Segments: segment.NewRegistry([]string{"İstanbul", "Ankara"}),
		Audience: c,
		Now:      func() time.Time { return fixedNow },
	}
}

func post(t *testing.T, d Deps, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/segments/preview", strings.NewReader(body))
	NewRouter(d).ServeHTTP(rec, req)
	return rec
}

func TestSegmentPreview(t *testing.T) {
	c := &fakeCounter{audience: segment.Audience{Total: 177, SMSReachable: 99, EmailReachable: 133}}
	rec := post(t, segmentDeps(c), `{"match":"all","conditions":[
		{"field":"city","op":"eq","value":"İstanbul"},
		{"field":"last_cart_add_at","op":"within_last_days","value":30}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got previewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := segment.Reach{Audience: c.audience, SMSExcluded: 78, EmailExcluded: 44, ChannelMessages: 232}
	if got.Audience != want {
		t.Errorf("audience = %+v, want %+v", got.Audience, want)
	}
	for _, key := range []string{`"total":177`, `"sms_reachable":99`, `"sms_excluded":78`, `"email_excluded":44`, `"channel_messages":232`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("body lacks %s: %s", key, rec.Body)
		}
	}
	if len(got.Description.Lines) != 2 || got.Description.Lines[0] != "City is İstanbul" {
		t.Errorf("description = %+v", got.Description)
	}
	if !got.AsOf.Equal(fixedNow) || !c.gotAsOf.Equal(fixedNow) {
		t.Errorf("as_of = %v, counter got %v, want %v", got.AsOf, c.gotAsOf, fixedNow)
	}
}

func TestSegmentPreviewRejectsInvalidRule(t *testing.T) {
	c := &fakeCounter{}
	rec := post(t, segmentDeps(c), `{"match":"all","conditions":[
		{"field":"city","op":"eq","value":"Paris"},
		{"field":"sms_opt_in","op":"eq","value":false}]}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var got validationErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Details) != 2 || got.Details[0].Code != segment.CodeInvalidValue || got.Details[1].Code != segment.CodeUnknownField {
		t.Errorf("details = %+v", got.Details)
	}
	if c.calls != 0 {
		t.Error("database was queried for an invalid rule")
	}
}

func TestSegmentPreviewMalformedJSON(t *testing.T) {
	rec := post(t, segmentDeps(&fakeCounter{}), `{"match":`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"invalid_json"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestSegmentPreviewBodyLimit(t *testing.T) {
	huge := `{"match":"all","conditions":[],"pad":"` + strings.Repeat("x", maxRuleBytes) + `"}`
	rec := post(t, segmentDeps(&fakeCounter{}), huge)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestSegmentPreviewCountFailureIsGeneric(t *testing.T) {
	c := &fakeCounter{err: errors.New("pq: canceling statement due to statement timeout")}
	rec := post(t, segmentDeps(c), `{"match":"all","conditions":[]}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "statement") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}

func TestSegmentFields(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(segmentDeps(&fakeCounter{})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/segments/fields", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got fieldsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Fields) != 7 || got.Fields[0].Name != "city" || got.Limits["max_days"] != segment.MaxDays {
		t.Errorf("response = %+v", got)
	}
	if strings.Contains(rec.Body.String(), "column") {
		t.Error("response exposes SQL column names")
	}
}
