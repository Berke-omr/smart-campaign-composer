// Package config loads service configuration from environment variables.
//
// Every binary composes the sections it needs (Common, HTTP, Database, Seed).
// Loaders take a Lookup function instead of reading os.Getenv directly so they
// can be tested with a plain map.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
