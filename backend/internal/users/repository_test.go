package users_test

import (
	"context"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/users"
)

func TestStatsMatchesGeneratedUsers(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	generated := seed.Generate(1000, 42, time.Now())
	if _, err := seed.Load(ctx, pool, generated, false); err != nil {
		t.Fatal(err)
	}

	var want users.Stats
	for _, u := range generated {
		want.Total++
		if u.SMSOptIn {
			want.SMSOptIn++
		}
		if u.EmailOptIn {
			want.EmailOptIn++
		}
	}

	got, err := users.NewRepository(pool).Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Stats() = %+v, want %+v", got, want)
	}
}

func TestStatsOnEmptyTable(t *testing.T) {
	pool := testdb.New(t)
	got, err := users.NewRepository(pool).Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (users.Stats{}) {
		t.Errorf("Stats() on empty table = %+v, want zeros", got)
	}
}
