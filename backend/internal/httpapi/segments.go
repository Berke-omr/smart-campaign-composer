package httpapi

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/httpx"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// maxRuleBytes bounds request bodies; a valid rule is well under 2 KB.
const maxRuleBytes = 64 << 10

type fieldsResponse struct {
	Fields []segment.Field `json:"fields"`
	Limits map[string]int  `json:"limits"`
}

// segmentFields describes the targetable fields (for the UI's rule editor).
func (d Deps) segmentFields(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, fieldsResponse{
		Fields: d.Segments.Fields(),
		Limits: map[string]int{
			"max_conditions":  segment.MaxConditions,
			"max_list_values": segment.MaxListValues,
			"min_days":        segment.MinDays,
			"max_days":        segment.MaxDays,
		},
	})
}

type previewResponse struct {
	Audience    segment.Audience    `json:"audience"`
	Description segment.Description `json:"description"`
	AsOf        time.Time           `json:"as_of"`
}

type validationErrorResponse struct {
	Error   string         `json:"error"`
	Details segment.Errors `json:"details"`
}

// segmentPreview validates a rule and returns its audience size.
// 422 carries the structured validation errors.
func (d Deps) segmentPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRuleBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "rule is too large")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "could not read request body")
		return
	}

	v, err := segment.ParseAndValidate(body, d.Segments)
	if err != nil {
		var errs segment.Errors
		if !errors.As(err, &errs) {
			internalError(w, r, d.Logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, validationErrorResponse{Error: "invalid segment rule", Details: errs})
		return
	}

	asOf := d.Now().UTC()
	audience, err := d.Audience.Count(r.Context(), v, asOf)
	if err != nil {
		internalError(w, r, d.Logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, previewResponse{
		Audience:    audience,
		Description: segment.Describe(v),
		AsOf:        asOf,
	})
}
