package seed_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/seed"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/testdb"
)

var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestLoadIsIdempotentAndResettable(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	users := seed.Generate(2000, 42, now)

	res, err := seed.Load(ctx, pool, users, false)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if res.Inserted != 2000 || res.Skipped {
		t.Fatalf("first load = %+v, want 2000 inserted", res)
	}

	res, err = seed.Load(ctx, pool, users, false)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !res.Skipped || res.Inserted != 0 {
		t.Fatalf("second load = %+v, want skipped", res)
	}

	res, err = seed.Load(ctx, pool, users, true)
	if err != nil {
		t.Fatalf("reset load: %v", err)
	}
	var n, minID, maxID int64
	if err := pool.QueryRow(ctx, "SELECT count(*), min(id), max(id) FROM users").Scan(&n, &minID, &maxID); err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 2000 || n != 2000 || minID != 1 || maxID != 2000 {
		t.Fatalf("after reset: inserted=%d count=%d ids=[%d,%d], want 2000 rows with ids 1..2000", res.Inserted, n, minID, maxID)
	}
}

func TestLoadRoundTripsValues(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	users := seed.Generate(500, 7, now)
	if _, err := seed.Load(ctx, pool, users, false); err != nil {
		t.Fatal(err)
	}

	// Money must survive exactly: compare the sum in minor units.
	var wantKurus int64
	for _, u := range users {
		wantKurus += u.TotalSpentKurus
	}
	var gotKurus int64
	if err := pool.QueryRow(ctx, "SELECT (sum(total_spent) * 100)::bigint FROM users").Scan(&gotKurus); err != nil {
		t.Fatal(err)
	}
	if gotKurus != wantKurus {
		t.Errorf("total_spent sum = %d kuruş, want %d", gotKurus, wantKurus)
	}

	// Ids follow insertion order, so id 1 is users[0].
	first := users[0]
	var (
		firstName, city string
		email           *string
		lastPurchase    *time.Time
		createdAt       time.Time
	)
	err := pool.QueryRow(ctx, "SELECT first_name, city, email, last_purchase_at, created_at FROM users WHERE id = 1").
		Scan(&firstName, &city, &email, &lastPurchase, &createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if firstName != first.FirstName || city != first.City {
		t.Errorf("row 1 = %s/%s, want %s/%s", firstName, city, first.FirstName, first.City)
	}
	if (email == nil) != (first.Email == nil) || (email != nil && *email != *first.Email) {
		t.Errorf("email = %v, want %v", email, first.Email)
	}
	if (lastPurchase == nil) != (first.LastPurchaseAt == nil) || (lastPurchase != nil && !lastPurchase.Equal(*first.LastPurchaseAt)) {
		t.Errorf("last_purchase_at = %v, want %v", lastPurchase, first.LastPurchaseAt)
	}
	if !createdAt.Equal(first.CreatedAt) {
		t.Errorf("created_at = %v, want %v", createdAt, first.CreatedAt)
	}
}

func TestConcurrentLoadsInsertOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	users := seed.Generate(1000, 42, now)

	var wg sync.WaitGroup
	results := make([]seed.Result, 3)
	errs := make([]error, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = seed.Load(ctx, pool, users, false)
		}()
	}
	wg.Wait()

	inserted := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("loader %d: %v", i, errs[i])
		}
		if !results[i].Skipped {
			inserted++
		}
	}
	var n int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if inserted != 1 || n != 1000 {
		t.Fatalf("%d loaders inserted, table has %d rows; want exactly one insert of 1000", inserted, n)
	}
}
