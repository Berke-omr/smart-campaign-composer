package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	env := MapLookup(map[string]string{"DATABASE_URL": "postgres://x"})

	common, err := LoadCommon(env)
	if err != nil {
		t.Fatalf("LoadCommon: %v", err)
	}
	if common.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", common.LogLevel)
	}

	httpCfg, err := LoadHTTP(env)
	if err != nil {
		t.Fatalf("LoadHTTP: %v", err)
	}
	if httpCfg.Addr != ":8080" || httpCfg.ShutdownTimeout != 10*time.Second || httpCfg.AllowedHosts != nil {
		t.Errorf("HTTP = %+v, want :8080 / 10s / no allowed hosts (the api's default)", httpCfg)
	}

	db, err := LoadDatabase(env)
	if err != nil {
		t.Fatalf("LoadDatabase: %v", err)
	}
	if db.MaxConns != 10 || db.ConnectTimeout != 30*time.Second {
		t.Errorf("Database = %+v, want 10 conns / 30s", db)
	}

	seed, err := LoadSeed(env)
	if err != nil {
		t.Fatalf("LoadSeed: %v", err)
	}
	if seed.Users != 5000 || seed.RandomSeed != 42 {
		t.Errorf("Seed = %+v, want 5000 / 42", seed)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := MapLookup(map[string]string{
		"LOG_LEVEL":             "debug",
		"HTTP_ADDR":             ":9999",
		"HTTP_SHUTDOWN_TIMEOUT": "3s",
		"HTTP_ALLOWED_HOSTS":    " composer.example.com, ,localhost ",
		"DATABASE_URL":          "postgres://y",
		"DATABASE_MAX_CONNS":    "25",
		"SEED_USERS":            "1200",
		"SEED_RANDOM_SEED":      "7",
	})

	common, _ := LoadCommon(env)
	httpCfg, _ := LoadHTTP(env)
	db, _ := LoadDatabase(env)
	seed, _ := LoadSeed(env)

	if common.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", common.LogLevel)
	}
	if httpCfg.Addr != ":9999" || httpCfg.ShutdownTimeout != 3*time.Second ||
		strings.Join(httpCfg.AllowedHosts, "|") != "composer.example.com|localhost" {
		t.Errorf("HTTP = %+v", httpCfg)
	}
	if db.URL != "postgres://y" || db.MaxConns != 25 {
		t.Errorf("Database = %+v", db)
	}
	if seed.Users != 1200 || seed.RandomSeed != 7 {
		t.Errorf("Seed = %+v", seed)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		load    func(Lookup) error
		env     map[string]string
		wantErr []string
	}{
		{
			name:    "database url required",
			load:    func(e Lookup) error { _, err := LoadDatabase(e); return err },
			env:     map[string]string{},
			wantErr: []string{"DATABASE_URL is required"},
		},
		{
			name:    "all bad values reported together",
			load:    func(e Lookup) error { _, err := LoadDatabase(e); return err },
			env:     map[string]string{"DATABASE_MAX_CONNS": "lots", "DATABASE_CONNECT_TIMEOUT": "-1s"},
			wantErr: []string{"DATABASE_URL is required", "DATABASE_MAX_CONNS", "DATABASE_CONNECT_TIMEOUT"},
		},
		{
			name:    "seed users out of range",
			load:    func(e Lookup) error { _, err := LoadSeed(e); return err },
			env:     map[string]string{"SEED_USERS": "0"},
			wantErr: []string{"SEED_USERS: 0 is out of range"},
		},
		{
			name:    "unknown log level",
			load:    func(e Lookup) error { _, err := LoadCommon(e); return err },
			env:     map[string]string{"LOG_LEVEL": "loud"},
			wantErr: []string{"LOG_LEVEL"},
		},
		{
			name:    "duration without unit",
			load:    func(e Lookup) error { _, err := LoadHTTP(e); return err },
			env:     map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "10"},
			wantErr: []string{"HTTP_SHUTDOWN_TIMEOUT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.load(MapLookup(tt.env))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestLoadLLMProviderSelection(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want LLMMode
	}{
		{"auto without key runs the mock", map[string]string{}, LLMModeMock},
		{"auto with key runs anthropic", map[string]string{"ANTHROPIC_API_KEY": "k"}, LLMModeAnthropic},
		{"explicit auto with key", map[string]string{"LLM_PROVIDER": "auto", "ANTHROPIC_API_KEY": "k"}, LLMModeAnthropic},
		{"whitespace key counts as no key", map[string]string{"ANTHROPIC_API_KEY": "  \n"}, LLMModeMock},
		{"mock wins over a key", map[string]string{"LLM_PROVIDER": "mock", "ANTHROPIC_API_KEY": "k"}, LLMModeMock},
		{"explicit anthropic", map[string]string{"LLM_PROVIDER": "anthropic", "ANTHROPIC_API_KEY": "k"}, LLMModeAnthropic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := LoadLLM(MapLookup(tt.env))
			if err != nil {
				t.Fatalf("LoadLLM: %v", err)
			}
			if c.Mode != tt.want {
				t.Errorf("Mode = %q, want %q", c.Mode, tt.want)
			}
		})
	}
}

func TestLoadLLMDefaultsAndOverrides(t *testing.T) {
	c, err := LoadLLM(MapLookup(map[string]string{"ANTHROPIC_API_KEY": " k \n"}))
	if err != nil {
		t.Fatalf("LoadLLM: %v", err)
	}
	if c.Model != "claude-sonnet-5-5" || c.RequestTimeout != 60*time.Second || c.MaxRetries != 2 || c.APIKey != "k" {
		t.Errorf("defaults = %+v", c)
	}

	c, err = LoadLLM(MapLookup(map[string]string{
		"LLM_MODEL": "claude-opus-5-5", "LLM_TIMEOUT": "20s", "LLM_MAX_RETRIES": "0",
	}))
	if err != nil {
		t.Fatalf("LoadLLM: %v", err)
	}
	if c.Model != "claude-opus-5-5" || c.RequestTimeout != 20*time.Second || c.MaxRetries != 0 {
		t.Errorf("overrides = %+v", c)
	}
}

func TestLoadLLMErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr []string
	}{
		{
			name:    "anthropic without key fails at startup",
			env:     map[string]string{"LLM_PROVIDER": "anthropic"},
			wantErr: []string{"LLM_PROVIDER=anthropic needs ANTHROPIC_API_KEY"},
		},
		{
			name:    "all bad values reported together",
			env:     map[string]string{"LLM_PROVIDER": "openai", "LLM_TIMEOUT": "soon", "LLM_MAX_RETRIES": "99"},
			wantErr: []string{`LLM_PROVIDER: "openai"`, "LLM_TIMEOUT", "LLM_MAX_RETRIES"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadLLM(MapLookup(tt.env))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestLoadMockProvider(t *testing.T) {
	c, err := LoadMockProvider(MapLookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := MockProvider{LatencyMin: 20 * time.Millisecond, LatencyMax: 120 * time.Millisecond, ErrorRate: 0.03,
		ThrottleRate: 0.03, SlowRate: 0.005, SlowDelay: 10 * time.Second, RejectRate: 0.01, RateLimit: 100}
	if c != want {
		t.Errorf("defaults = %+v, want %+v", c, want)
	}

	c, err = LoadMockProvider(MapLookup(map[string]string{
		"MOCK_LATENCY_MIN": "0", "MOCK_LATENCY_MAX": "0s", "MOCK_ERROR_RATE": "0", "MOCK_THROTTLE_RATE": "0.5",
		"MOCK_SLOW_RATE": "0.5", "MOCK_REJECT_RATE": "1", "MOCK_RATE_LIMIT": "0",
	}))
	if err != nil || c.LatencyMax != 0 || c.ThrottleRate != 0.5 || c.RejectRate != 1 || c.RateLimit != 0 {
		t.Errorf("overrides = %+v (err %v)", c, err)
	}

	// Every bad value is reported at once.
	_, err = LoadMockProvider(MapLookup(map[string]string{
		"MOCK_ERROR_RATE": "1.5", "MOCK_THROTTLE_RATE": "abc", "MOCK_LATENCY_MIN": "-1s", "MOCK_RATE_LIMIT": "-3",
	}))
	for _, key := range []string{"MOCK_ERROR_RATE", "MOCK_THROTTLE_RATE", "MOCK_LATENCY_MIN", "MOCK_RATE_LIMIT"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("error %v does not mention %s", err, key)
		}
	}
	_, err = LoadMockProvider(MapLookup(map[string]string{"MOCK_LATENCY_MIN": "2s", "MOCK_LATENCY_MAX": "1s"}))
	if err == nil || !strings.Contains(err.Error(), "above MOCK_LATENCY_MAX") {
		t.Errorf("min above max: %v", err)
	}
	_, err = LoadMockProvider(MapLookup(map[string]string{"MOCK_ERROR_RATE": "0.6", "MOCK_THROTTLE_RATE": "0.5"}))
	if err == nil || !strings.Contains(err.Error(), "at most 1") {
		t.Errorf("rates above 1: %v", err)
	}
}

func TestLoadWorker(t *testing.T) {
	c, err := LoadWorker(MapLookup(nil))
	want := Worker{ProviderURL: "http://mock-provider:8080", ProviderTimeout: 3 * time.Second, Senders: 32, Lease: 30 * time.Second}
	if err != nil || c != want {
		t.Errorf("defaults = %+v (err %v), want %+v", c, err, want)
	}
	c, err = LoadWorker(MapLookup(map[string]string{"PROVIDER_URL": "http://localhost:18090/", "PROVIDER_TIMEOUT": "500ms",
		"WORKER_CONCURRENCY": "8", "WORKER_LEASE": "1s"}))
	if err != nil || c.ProviderURL != "http://localhost:18090" || c.Senders != 8 || c.Lease != time.Second {
		t.Errorf("overrides = %+v (err %v)", c, err)
	}
	_, err = LoadWorker(MapLookup(map[string]string{"PROVIDER_TIMEOUT": "20s", "WORKER_CONCURRENCY": "0"}))
	if err == nil || !strings.Contains(err.Error(), "WORKER_LEASE (30s) must be at least twice") || !strings.Contains(err.Error(), "WORKER_CONCURRENCY") {
		t.Errorf("bad values: %v", err)
	}
}
