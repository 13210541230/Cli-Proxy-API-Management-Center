package collector

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

func TestProcessItemsReplaysDurablePendingBatchAfterInsertFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	manager := NewManager(config.Config{}, db, nil, AlertConfig{})
	cfg := RuntimeConfig{BatchSize: 10}
	ctx := context.Background()
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open schema connection: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec(`alter table usage_events rename to usage_events_broken`); err != nil {
		t.Fatalf("break usage table: %v", err)
	}
	raw := `{"timestamp":"2026-05-06T00:00:00Z","model":"pending-model","input_tokens":3}`
	if err := manager.processItems(ctx, cfg, []string{raw}); err == nil {
		t.Fatal("processItems unexpectedly succeeded with broken usage table")
	}
	pending, err := db.CollectorPendingItemCount(ctx)
	if err != nil || pending != 1 {
		t.Fatalf("pending count = %d, %v", pending, err)
	}
	if _, err := locker.Exec(`alter table usage_events_broken rename to usage_events`); err != nil {
		t.Fatalf("restore usage table: %v", err)
	}
	if err := manager.drainPendingItems(ctx, cfg); err != nil {
		t.Fatalf("replay pending items: %v", err)
	}
	events, _, err := db.Counts(ctx)
	if err != nil || events != 1 {
		t.Fatalf("events = %d, %v", events, err)
	}
	pending, err = db.CollectorPendingItemCount(ctx)
	if err != nil || pending != 0 {
		t.Fatalf("pending after replay = %d, %v", pending, err)
	}
}
