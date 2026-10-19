package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/campaign"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

// maxCampaignBytes bounds request bodies; a brief is at most 2,000 characters.
const maxCampaignBytes = 64 << 10

type problemsResponse struct {
	Error   string   `json:"error"`
	Details []string `json:"details"`
}

type contentErrorResponse struct {
	Error   string          `json:"error"`
	Channel content.Channel `json:"channel,omitempty"`
	Report  content.Report  `json:"report"`
}

// readJSON decodes a request body strictly into v.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCampaignBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		} else {
			httpx.WriteError(w, http.StatusBadRequest, "could not read request body")
		}
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, problemsResponse{Error: "invalid request body", Details: []string{err.Error()}})
		return false
	}
	return true
}

func campaignID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "campaign not found")
		return 0, false
	}
	return id, true
}

// campaignError maps service errors to responses.
func (d Deps) campaignError(w http.ResponseWriter, r *http.Request, err error) {
	var input *campaign.InputError
	var seg *llm.SegmentError
	var ce *campaign.ContentError
	var stale *campaign.StaleContentError
	switch {
	case errors.As(err, &input):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, problemsResponse{Error: "invalid request", Details: input.Problems})
	case errors.As(err, &seg):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, problemsResponse{
			Error: fmt.Sprintf("could not build an audience from this brief (%d attempts)", seg.Attempts), Details: seg.Problems})
	case errors.As(err, &ce):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, contentErrorResponse{Error: "invalid content", Report: ce.Report})
	case errors.As(err, &stale):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, contentErrorResponse{
			Error:   fmt.Sprintf("the %s message no longer fits this audience; edit it and approve again", stale.Channel),
			Channel: stale.Channel, Report: stale.Report})
	case errors.Is(err, campaign.ErrNoRecipients):
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, campaign.ErrNotReady):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, campaign.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "campaign not found")
	case errors.Is(err, campaign.ErrLocked):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, campaign.ErrBusy):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		internalError(w, r, d.Logger, err)
	}
}

// createCampaign: brief -> validated audience rule -> stored draft.
func (d Deps) createCampaign(w http.ResponseWriter, r *http.Request) {
	var in campaign.CreateInput
	if !readJSON(w, r, &in) {
		return
	}
	v, err := d.Campaigns.Create(r.Context(), in)
	if err != nil {
		d.campaignError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (d Deps) listCampaigns(w http.ResponseWriter, r *http.Request) {
	list, err := d.Campaigns.List(r.Context(), 50)
	if err != nil {
		d.campaignError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]campaign.Summary{"campaigns": list})
}

func (d Deps) getCampaign(w http.ResponseWriter, r *http.Request) {
	id, ok := campaignID(w, r)
	if !ok {
		return
	}
	v, err := d.Campaigns.Get(r.Context(), id)
	if err != nil {
		d.campaignError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

// approveCampaign freezes the audience and starts sending. Approving again
// returns the same result.
func (d Deps) approveCampaign(w http.ResponseWriter, r *http.Request) {
	id, ok := campaignID(w, r)
	if !ok {
		return
	}
	a, err := d.Campaigns.Approve(r.Context(), id)
	if err != nil {
		d.campaignError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

// streamDrafts is the server-sent event stream of the message drafts
// (docs/API.md). Errors found before the first event are ordinary JSON
// responses; after that the stream itself carries draft.error.
func (d Deps) streamDrafts(w http.ResponseWriter, r *http.Request) {
	id, ok := campaignID(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		internalError(w, r, d.Logger, errors.New("response writer cannot flush"))
		return
	}
	started := false
	emit := func(event string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if !started {
			started = true
			h := w.Header()
			h.Set("Content-Type", "text/event-stream")
			h.Set("Cache-Control", "no-cache")
			h.Set("X-Accel-Buffering", "no") // nginx: do not buffer this response
			w.WriteHeader(http.StatusOK)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := d.Campaigns.StreamDrafts(r.Context(), id, emit); err != nil {
		if started {
			d.Logger.Error("draft stream failed", "campaign", id, "err", err)
			_ = emit(campaign.EventError, map[string]string{"message": "the drafts could not be written"})
			return
		}
		d.campaignError(w, r, err)
	}
}

// editMessage replaces one channel's message after validating it.
func (d Deps) editMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := campaignID(w, r)
	if !ok {
		return
	}
	ch := content.Channel(chi.URLParam(r, "channel"))
	if !ch.Valid() {
		httpx.WriteError(w, http.StatusNotFound, "unknown channel")
		return
	}
	var m content.Message
	if !readJSON(w, r, &m) {
		return
	}
	if ch == content.SMS && (m.Subject != "" || m.Body != "") || ch == content.Email && m.Text != "" {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, problemsResponse{Error: "invalid request body",
			Details: []string{"an SMS has only text; an email has only subject and body"}})
		return
	}
	mv, status, err := d.Campaigns.EditMessage(r.Context(), id, ch, m)
	if err != nil {
		d.campaignError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"message": mv, "status": status})
}
