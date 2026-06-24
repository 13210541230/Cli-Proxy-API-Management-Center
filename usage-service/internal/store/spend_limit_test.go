package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestSaveAndLoadSpendLimitConfig(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := SpendLimitConfig{
		Enabled:     true,
		DailyCents:  15000,
		WeeklyCents: 50000,
	}

	if err := db.SaveSpendLimitConfig(context.Background(), cfg); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}

	loaded, ok, err := db.LoadSpendLimitConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadSpendLimitConfig failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after save")
	}
	if loaded.Enabled != true {
		t.Fatal("expected enabled=true")
	}
	if loaded.DailyCents != 15000 {
		t.Fatalf("expected daily_cents=15000, got %d", loaded.DailyCents)
	}
	if loaded.WeeklyCents != 50000 {
		t.Fatalf("expected weekly_cents=50000, got %d", loaded.WeeklyCents)
	}
}

func TestQueryKeySpend_ReturnsAggregatedCents(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	prices := map[string]ModelPrice{
		"gpt-4": {Prompt: 10.0, Completion: 30.0, Cache: 5.0},
	}
	if _, err := db.UpsertSyncedModelPrices(ctx, prices); err != nil {
		t.Fatalf("SyncModelPrices failed: %v", err)
	}

	nowMS := time.Now().UnixMilli()
	nowStr := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{
			EventHash:   "e-spend-1",
			TimestampMS: nowMS,
			Timestamp:   nowStr,
			Model:       "gpt-4",
			APIKeyHash:  "hash-spend",
			InputTokens: 100000,
			TotalTokens: 100000,
			CreatedAtMS: nowMS,
		},
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	results, err := db.QueryKeySpend(ctx)
	if err != nil {
		t.Fatalf("QueryKeySpend failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 key spend result")
	}
	found := false
	for _, r := range results {
		if r.KeyHash == "hash-spend" {
			found = true
			t.Logf("Key %s: today=%d cents, week=%d cents", r.KeyHash, r.TodayCents, r.WeekCents)
		}
	}
	if !found {
		t.Fatal("hash-spend should be in query results")
	}
}
