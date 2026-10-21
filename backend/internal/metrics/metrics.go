// Package metrics holds the Prometheus metrics of the api and the worker
// (DECISIONS.md D18, D25). Each process counts what happens in it: the
// worker its provider calls, the api its LLM calls. The state that lives in
// the database, deliveries by channel and status, is read from it when the
// api is scraped, so it is right after any restart.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/delivery"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

// NewRegistry returns a registry with the Go runtime and process metrics.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// Handler serves reg in the Prometheus text format. A collector that fails
// (the database is down) is logged and left out; the rest is still served.
func Handler(reg *prometheus.Registry, logger *slog.Logger) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog:      errorLog{logger},
		ErrorHandling: promhttp.ContinueOnError,
	})
}

type errorLog struct{ logger *slog.Logger }

// Println logs promhttp's message as text: an error value inside v would
// otherwise be printed as {} by the JSON log handler.
func (l errorLog) Println(v ...any) {
	l.logger.Error("metrics could not be gathered", "err", fmt.Sprint(v...))
}

// ---- worker ----

// Worker counts the worker's provider calls.
type Worker struct {
	calls    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func NewWorker(reg prometheus.Registerer) *Worker {
	m := &Worker{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "scc_provider_calls_total",
			Help: "Provider calls by channel, result (sent, retry, failed) and HTTP status (none: timeout or network error).",
		}, []string{"channel", "result", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "scc_provider_call_duration_seconds",
			Help:    "Time from the rate limiter letting a call go to its answer or timeout.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"channel"}),
	}
	reg.MustRegister(m.calls, m.duration)
	for _, ch := range content.Channels {
		m.duration.WithLabelValues(string(ch))
	}
	return m
}

// Observe records one call; it is the delivery worker's Observe hook.
func (m *Worker) Observe(j delivery.Job, c delivery.Call, d delivery.Decision) {
	code := "none"
	if c.Result.Status != 0 {
		code = strconv.Itoa(c.Result.Status)
	}
	m.calls.WithLabelValues(string(j.Channel), d.Outcome(), code).Inc()
	m.duration.WithLabelValues(string(j.Channel)).Observe(c.Finished.Sub(c.Started).Seconds())
}

// ---- api: LLM ----

var (
	llmTasks   = []llm.Task{llm.TaskSegment, llm.TaskSMS, llm.TaskEmail}
	llmResults = []string{"valid", "invalid", "refused", "truncated", "timeout", "error"}
)

// LLM counts the api's model attempts.
type LLM struct {
	duration *prometheus.HistogramVec
	attempts *prometheus.CounterVec
	repairs  *prometheus.CounterVec
}

func NewLLM(reg prometheus.Registerer) *LLM {
	m := &LLM{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "scc_llm_attempt_duration_seconds",
			Help:    "One model attempt (streaming included) and its validation, by task.",
			Buckets: []float64{.25, .5, 1, 2, 4, 8, 15, 30, 60},
		}, []string{"task"}),
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "scc_llm_attempts_total",
			Help: "Model attempts by task and result; result invalid is an answer the validator rejected.",
		}, []string{"task", "result"}),
		repairs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "scc_llm_repairs_total",
			Help: "Repair attempts (attempt 2 or 3) by task.",
		}, []string{"task"}),
	}
	reg.MustRegister(m.duration, m.attempts, m.repairs)
	for _, task := range llmTasks {
		m.duration.WithLabelValues(string(task))
		m.repairs.WithLabelValues(string(task))
		for _, r := range llmResults {
			m.attempts.WithLabelValues(string(task), r)
		}
	}
	return m
}

// Observe records one attempt; it is the generator's Observe hook.
func (m *LLM) Observe(a llm.AttemptStat) {
	task := string(a.Task)
	m.duration.WithLabelValues(task).Observe(a.Duration.Seconds())
	m.attempts.WithLabelValues(task, a.Result).Inc()
	if a.Attempt > 1 {
		m.repairs.WithLabelValues(task).Inc()
	}
}

// ---- api: deliveries in the database ----

var deliveryStatuses = []string{
	delivery.StatusQueued, delivery.StatusSending, delivery.StatusRetrying, delivery.StatusSent, delivery.StatusFailed,
}

// deliveries reads the deliveries table by channel and status when scraped.
type deliveries struct {
	pool *pgxpool.Pool
	desc *prometheus.Desc
}

// RegisterDeliveries adds the scc_deliveries gauge to reg.
func RegisterDeliveries(reg prometheus.Registerer, pool *pgxpool.Pool) {
	reg.MustRegister(&deliveries{pool: pool, desc: prometheus.NewDesc("scc_deliveries",
		"Deliveries by channel and status, read from the database when scraped.", []string{"channel", "status"}, nil)})
}

func (c *deliveries) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *deliveries) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	counts := map[[2]string]int64{}
	rows, err := c.pool.Query(ctx, `SELECT channel, status, count(*) FROM deliveries GROUP BY channel, status`)
	if err == nil {
		for rows.Next() {
			var channel, status string
			var n int64
			if err = rows.Scan(&channel, &status, &n); err != nil {
				break
			}
			counts[[2]string{channel, status}] = n
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
	}
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, err)
		return
	}
	// Every combination, zeros included, so each series exists from the start.
	for _, channel := range content.Channels {
		for _, status := range deliveryStatuses {
			ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue,
				float64(counts[[2]string{string(channel), status}]), string(channel), status)
		}
	}
}
