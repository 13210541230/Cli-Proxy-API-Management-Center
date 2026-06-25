package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestSaveAndLoadSpendLimitConfig(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()

	cfg := SpendLimitConfig{
		Enabled: true,
		Default: SpendLimit{
			DailyCents:  15000,
			WeeklyCents: 50000,
		},
		Overrides: []SpendLimitEntry{
			{ApplyTo: "api-key", ApplyValue: "hash-a", DailyCents: 1000, WeeklyCents: 2000},
		},
	}

	if err := db.SaveSpendLimitConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}

	loaded, ok, err := db.LoadSpendLimitConfig(ctx)
	if err != nil {
		t.Fatalf("LoadSpendLimitConfig failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after save")
	}
	if !loaded.Enabled {
		t.Fatal("expected enabled=true")
	}
	if got := loaded.DefaultLimit(); got.DailyCents != 15000 || got.WeeklyCents != 50000 {
		t.Fatalf("default limit = %+v", got)
	}
	if len(loaded.Overrides) != 1 {
		t.Fatalf("len(overrides) = %d, want 1", len(loaded.Overrides))
	}
	limit := loaded.LimitForKey("hash-a")
	if limit.DailyCents != 1000 || limit.WeeklyCents != 2000 {
		t.Fatalf("override limit = %+v", limit)
	}
	limit = loaded.LimitForKey("hash-b")
	if limit.DailyCents != 15000 || limit.WeeklyCents != 50000 {
		t.Fatalf("fallback limit = %+v", limit)
	}
}

func TestQueryKeySpend_MatchesCostFormulaAndExcludesFailed(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.Local)
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "base",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-spend",
		InputTokens:  100000,
		CachedTokens: 20000,
		OutputTokens: 10000,
		TotalTokens:  110000,
		CreatedAtMS:  now.UnixMilli(),
	})
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "failed",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-spend",
		InputTokens:  1000000,
		OutputTokens: 1000000,
		TotalTokens:  2000000,
		Failed:       true,
		CreatedAtMS:  now.UnixMilli(),
	})

	result := findSpend(t, db, ctx, now, "hash-spend")
	if result.TodayCents != 120 || result.WeekCents != 120 {
		t.Fatalf("spend = %+v, want today/week 120 cents", result)
	}
}

func TestQueryKeySpend_UsesLargerCacheTokenField(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.Local)
	insertSpendEvent(t, db, usage.Event{
		EventHash:    "cache-token",
		TimestampMS:  now.UnixMilli(),
		Timestamp:    now.UTC().Format(time.RFC3339),
		Model:        "gpt-4",
		APIKeyHash:   "hash-cache",
		InputTokens:  100000,
		CachedTokens: 10000,
		CacheTokens:  50000,
		TotalTokens:  100000,
		CreatedAtMS:  now.UnixMilli(),
	})

	result := findSpend(t, db, ctx, now, "hash-cache")
	if result.TodayCents != 75 || result.WeekCents != 75 {
		t.Fatalf("spend = %+v, want today/week 75 cents", result)
	}
}

func TestQueryKeySpend_UsesLocalDayAndMondayWeek(t *testing.T) {
	db := newSpendLimitTestStore(t)
	ctx := context.Background()
	seedSpendPrice(t, db)

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	daysSinceMonday := (int(dayStart.Weekday()) + 6) % 7
	weekStart := dayStart.AddDate(0, 0, -daysSinceMonday)

	insertSpendEvent(t, db, spendEvent("today", "hash-window", dayStart.Add(time.Hour)))
	insertSpendEvent(t, db, spendEvent("week", "hash-window", weekStart.Add(time.Hour)))
	insertSpendEvent(t, db, spendEvent("old", "hash-window", weekStart.Add(-time.Millisecond)))

	result := findSpend(t, db, ctx, now, "hash-window")
	if result.TodayCents != 100 {
		t.Fatalf("today cents = %d, want 100", result.TodayCents)
	}
	if result.WeekCents != 200 {
		t.Fatalf("week cents = %d, want 200", result.WeekCents)
	}
}

func newSpendLimitTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSpendPrice(t *testing.T, db *Store) {
	t.Helper()
	_, err := db.UpsertSyncedModelPrices(context.Background(), map[string]ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	})
	if err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
}

func insertSpendEvent(t *testing.T, db *Store, event usage.Event) {
	t.Helper()
	if _, err := db.InsertEvents(context.Background(), []usage.Event{event}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}
}

func spendEvent(hash, keyHash string, at time.Time) usage.Event {
	return usage.Event{
		EventHash:   hash,
		TimestampMS: at.UnixMilli(),
		Timestamp:   at.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  keyHash,
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: at.UnixMilli(),
	}
}

func findSpend(t *testing.T, db *Store, ctx context.Context, now time.Time, keyHash string) KeySpend {
	t.Helper()
	results, err := db.queryKeySpendAt(ctx, now)
	if err != nil {
		t.Fatalf("queryKeySpendAt failed: %v", err)
	}
	for _, result := range results {
		if result.KeyHash == keyHash {
			return result
		}
	}
	t.Fatalf("key %q not found in %+v", keyHash, results)
	return KeySpend{}
}
