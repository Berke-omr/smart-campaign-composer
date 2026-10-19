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
	if httpCfg.Addr != ":8080" || httpCfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("HTTP = %+v, want :8080 / 10s", httpCfg)
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
	if httpCfg.Addr != ":9999" || httpCfg.ShutdownTimeout != 3*time.Second {
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
