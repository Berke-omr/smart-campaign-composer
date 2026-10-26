// Package config loads service configuration from environment variables.
//
// Every binary composes the sections it needs (Common, HTTP, Database, Seed, LLM).
// Loaders take a Lookup function instead of reading os.Getenv directly so they
// can be tested with a plain map.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Lookup returns the value of an environment variable and whether it was set.
type Lookup func(key string) (string, bool)

// OSLookup reads from the process environment.
var OSLookup Lookup = os.LookupEnv

// MapLookup adapts a map to a Lookup (used in tests).
func MapLookup(m map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// Common holds settings shared by every service.
type Common struct {
	LogLevel slog.Level
}

// HTTP holds settings for services that expose an HTTP server.
type HTTP struct {
	Addr            string
	ShutdownTimeout time.Duration
	// AllowedHosts are the host names the api answers under
	// (HTTP_ALLOWED_HOSTS, comma-separated); nil means the api's default.
	AllowedHosts []string
}

// Database holds PostgreSQL connection settings.
type Database struct {
	URL      string
	MaxConns int32
	// ConnectTimeout bounds how long a service waits for the database to become
	// reachable at startup before giving up.
	ConnectTimeout time.Duration
}

// Seed holds settings for the fake user generator.
type Seed struct {
	Users      int
	RandomSeed uint64
}

// LLMMode is the LLM implementation the api runs with.
type LLMMode string

const (
	LLMModeAnthropic LLMMode = "anthropic"
	LLMModeMock      LLMMode = "mock"
)

// DefaultLLMModel is Claude Sonnet 5.5 (DECISIONS.md D13).
const DefaultLLMModel = "claude-sonnet-5-5"

// LLM holds the Claude settings. Mode is already resolved: LLM_PROVIDER=auto
// becomes anthropic when ANTHROPIC_API_KEY is set and mock otherwise, so the
// rest of the program never has to interpret "auto".
type LLM struct {
	Mode           LLMMode
	Model          string
	APIKey         string
	RequestTimeout time.Duration
	MaxRetries     int
}

func LoadLLM(env Lookup) (LLM, error) {
	r := reader{env: env}
	key, _ := env("ANTHROPIC_API_KEY")
	c := LLM{
		Model:          r.str("LLM_MODEL", DefaultLLMModel),
		APIKey:         strings.TrimSpace(key),
		RequestTimeout: r.duration("LLM_TIMEOUT", 60*time.Second),
		MaxRetries:     r.intRange("LLM_MAX_RETRIES", 2, 0, 10),
	}
	switch provider := r.str("LLM_PROVIDER", "auto"); provider {
	case "auto":
		c.Mode = LLMModeMock
		if c.APIKey != "" {
			c.Mode = LLMModeAnthropic
		}
	case "anthropic":
		c.Mode = LLMModeAnthropic
		if c.APIKey == "" {
			r.errs = append(r.errs, errors.New("LLM_PROVIDER=anthropic needs ANTHROPIC_API_KEY"))
		}
	case "mock":
		c.Mode = LLMModeMock
	default:
		r.errs = append(r.errs, fmt.Errorf("LLM_PROVIDER: %q is not one of auto, anthropic, mock", provider))
	}
	return c, r.err()
}

// MockProvider is how the fake SMS/email provider misbehaves (DECISIONS.md
// D24, K5). Rates are fractions of requests, drawn in this order: 503,
// random 429, slow accept; their sum is at most 1. Rejections are decided by
// recipient, so a rejected address is rejected every time.
type MockProvider struct {
	LatencyMin   time.Duration
	LatencyMax   time.Duration
	ErrorRate    float64       // 503
	ThrottleRate float64       // 429 with Retry-After 1–2 s
	SlowRate     float64       // accepted, but answered after SlowDelay
	SlowDelay    time.Duration // longer than the worker's timeout
	RejectRate   float64       // 400 invalid recipient
	RateLimit    int           // requests per channel in any 1 s window; 0 = no limit
}

func LoadMockProvider(env Lookup) (MockProvider, error) {
	r := reader{env: env}
	c := MockProvider{
		LatencyMin:   r.durationOrZero("MOCK_LATENCY_MIN", 20*time.Millisecond),
		LatencyMax:   r.durationOrZero("MOCK_LATENCY_MAX", 120*time.Millisecond),
		ErrorRate:    r.fraction("MOCK_ERROR_RATE", 0.03),
		ThrottleRate: r.fraction("MOCK_THROTTLE_RATE", 0.03),
		SlowRate:     r.fraction("MOCK_SLOW_RATE", 0.005),
		SlowDelay:    r.duration("MOCK_SLOW_DELAY", 10*time.Second),
		RejectRate:   r.fraction("MOCK_REJECT_RATE", 0.01),
		RateLimit:    r.intRange("MOCK_RATE_LIMIT", 100, 0, 100_000),
	}
	if c.LatencyMin > c.LatencyMax {
		r.errs = append(r.errs, fmt.Errorf("MOCK_LATENCY_MIN (%s) is above MOCK_LATENCY_MAX (%s)", c.LatencyMin, c.LatencyMax))
	}
	if sum := c.ErrorRate + c.ThrottleRate + c.SlowRate; sum > 1 {
		r.errs = append(r.errs, fmt.Errorf("MOCK_ERROR_RATE + MOCK_THROTTLE_RATE + MOCK_SLOW_RATE = %g; at most 1", sum))
	}
	return c, r.err()
}

// Worker holds the delivery worker's settings (DECISIONS.md D24).
type Worker struct {
	ProviderURL     string
	ProviderTimeout time.Duration // one provider call; a slower answer is a timeout
	Senders         int           // concurrent provider calls per channel
	Lease           time.Duration // how long a claimed row stays claimed
}

func LoadWorker(env Lookup) (Worker, error) {
	r := reader{env: env}
	c := Worker{
		ProviderURL:     strings.TrimRight(r.str("PROVIDER_URL", "http://mock-provider:8080"), "/"),
		ProviderTimeout: r.duration("PROVIDER_TIMEOUT", 3*time.Second),
		Senders:         r.intRange("WORKER_CONCURRENCY", 32, 1, 1000),
		Lease:           r.duration("WORKER_LEASE", 30*time.Second),
	}
	// A lease shorter than a call would let a second claim send while the
	// first call is still waiting for its answer.
	if c.Lease < 2*c.ProviderTimeout {
		r.errs = append(r.errs, fmt.Errorf("WORKER_LEASE (%s) must be at least twice PROVIDER_TIMEOUT (%s)", c.Lease, c.ProviderTimeout))
	}
	return c, r.err()
}

func LoadCommon(env Lookup) (Common, error) {
	r := reader{env: env}
	c := Common{LogLevel: r.logLevel("LOG_LEVEL", slog.LevelInfo)}
	return c, r.err()
}

func LoadHTTP(env Lookup) (HTTP, error) {
	r := reader{env: env}
	c := HTTP{
		Addr:            r.str("HTTP_ADDR", ":8080"),
		ShutdownTimeout: r.duration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		AllowedHosts:    r.list("HTTP_ALLOWED_HOSTS"),
	}
	return c, r.err()
}

func LoadDatabase(env Lookup) (Database, error) {
	r := reader{env: env}
	c := Database{
		URL:            r.required("DATABASE_URL"),
		MaxConns:       int32(r.intRange("DATABASE_MAX_CONNS", 10, 1, 200)),
		ConnectTimeout: r.duration("DATABASE_CONNECT_TIMEOUT", 30*time.Second),
	}
	return c, r.err()
}

func LoadSeed(env Lookup) (Seed, error) {
	r := reader{env: env}
	c := Seed{
		Users:      r.intRange("SEED_USERS", 5000, 1, 1_000_000),
		RandomSeed: uint64(r.intRange("SEED_RANDOM_SEED", 42, 0, 1<<62)),
	}
	return c, r.err()
}

// reader collects every problem instead of stopping at the first one, so a
// misconfigured service reports all bad variables in a single error.
type reader struct {
	env  Lookup
	errs []error
}

func (r *reader) err() error { return errors.Join(r.errs...) }

func (r *reader) str(key, def string) string {
	if v, ok := r.env(key); ok && v != "" {
		return v
	}
	return def
}

// list reads a comma-separated list; unset or empty is nil.
func (r *reader) list(key string) []string {
	v, _ := r.env(key)
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (r *reader) required(key string) string {
	v, ok := r.env(key)
	if !ok || v == "" {
		r.errs = append(r.errs, fmt.Errorf("%s is required", key))
	}
	return v
}

func (r *reader) intRange(key string, def, min, max int) int {
	v, ok := r.env(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not an integer", key, v))
		return def
	}
	if n < min || n > max {
		r.errs = append(r.errs, fmt.Errorf("%s: %d is out of range [%d, %d]", key, n, min, max))
		return def
	}
	return n
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v, ok := r.env(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a positive duration (e.g. 10s)", key, v))
		return def
	}
	return d
}

// durationOrZero is duration, but accepts 0 (e.g. no latency).
func (r *reader) durationOrZero(key string, def time.Duration) time.Duration {
	v, ok := r.env(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a duration of 0 or more (e.g. 50ms)", key, v))
		return def
	}
	return d
}

// fraction reads a number between 0 and 1.
func (r *reader) fraction(key string, def float64) float64 {
	v, ok := r.env(key)
	if !ok || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not a number between 0 and 1", key, v))
		return def
	}
	return f
}

func (r *reader) logLevel(key string, def slog.Level) slog.Level {
	v, ok := r.env(key)
	if !ok || v == "" {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(v)); err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %q is not one of debug, info, warn, error", key, v))
		return def
	}
	return lvl
}
