package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestHourlyRollupAggregatesAllTokenAndLatencyFields(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	latency := int64(120)
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "success", TimestampMS: 1_700_000_100_000, Model: "gpt-test", InputTokens: 10, OutputTokens: 20, ReasoningTokens: 3, CachedTokens: 4, CacheTokens: 5, TotalTokens: 42, LatencyMS: &latency, CreatedAtMS: 1},
		{EventHash: "failed-zero", TimestampMS: 1_700_000_200_000, Model: "gpt-test", Failed: true, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if n, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil || n != 2 {
		t.Fatalf("apply batch = (%d, %v), want (2, nil)", n, err)
	}
	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Requests != 2 || row.Successes != 1 || row.Failures != 1 {
		t.Fatalf("request counts = %#v", row)
	}
	if row.InputTokens != 10 || row.OutputTokens != 20 || row.ReasoningTokens != 3 || row.CachedTokens != 4 || row.CacheTokens != 5 || row.TotalTokens != 42 {
		t.Fatalf("token sums = %#v", row)
	}
	if row.LatencySumMS != 120 || row.LatencySamples != 1 {
		t.Fatalf("latency = %#v", row)
	}
	if row.ZeroTokenCalls != 1 {
		t.Fatalf("zero-token calls = %d, want 1", row.ZeroTokenCalls)
	}
}

func TestPurgeResetsRollupAfterInFlightFailureAndCatchupUsesRemainingRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "old-ready", TimestampMS: 1_000, Model: "purge-model", TotalTokens: 1, CreatedAtMS: 1},
		{EventHash: "keep-ready", TimestampMS: 3_000, Model: "purge-model", TotalTokens: 2, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert initial events: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil {
		t.Fatalf("initial rollup: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "old-tail", TimestampMS: 1_500, Model: "purge-model", TotalTokens: 4, CreatedAtMS: 3},
		{EventHash: "keep-tail", TimestampMS: 4_000, Model: "purge-model", TotalTokens: 8, CreatedAtMS: 4},
	}); err != nil {
		t.Fatalf("insert tail events: %v", err)
	}

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec(`begin immediate`); err != nil {
		t.Fatalf("acquire sqlite write lock: %v", err)
	}
	busyCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, rollupErr := db.ApplyHourlyRollupBatch(busyCtx, 1000)
	cancel()
	if rollupErr == nil {
		t.Fatal("in-flight rollup should fail while external lock is held")
	}
	if _, err := locker.Exec(`rollback`); err != nil {
		t.Fatalf("release sqlite write lock: %v", err)
	}

	purged, err := db.PurgeEventsBefore(ctx, 2_000)
	if err != nil {
		t.Fatalf("purge after in-flight failure: %v", err)
	}
	if purged != 2 {
		t.Fatalf("purged rows = %d, want 2", purged)
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load state after purge: %v", err)
	}
	if state.CheckpointID != 0 || state.CoverageEventID != 0 || state.TargetEventID != 0 || state.Status != RollupStatusPending {
		t.Fatalf("state after purge = %#v", state)
	}
	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups after purge: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rollups after purge = %#v, want empty", rows)
	}
	var rawCount int
	if err := db.db.QueryRow(`select count(*) from usage_events`).Scan(&rawCount); err != nil {
		t.Fatalf("count remaining raw events: %v", err)
	}
	if rawCount != 2 {
		t.Fatalf("remaining raw events = %d, want 2", rawCount)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil {
		t.Fatalf("catch up remaining raw events: %v", err)
	}
	rows, err = db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rebuilt rollups: %v", err)
	}
	if len(rows) != 1 || rows[0].Requests != 2 || rows[0].TotalTokens != 10 {
		t.Fatalf("rebuilt rollups = %#v, want two remaining events only", rows)
	}
}

func TestPurgeInvalidatesHourlyRollupBeforeRebuild(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "old", TimestampMS: 1_000, Model: "old-model", TotalTokens: 1, CreatedAtMS: 1},
		{EventHash: "new", TimestampMS: 2_000, Model: "new-model", TotalTokens: 2, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil {
		t.Fatalf("apply batch: %v", err)
	}
	if _, err := db.PurgeEventsBefore(ctx, 2_000); err != nil {
		t.Fatalf("purge events: %v", err)
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.CheckpointID != 0 || state.Status != RollupStatusPending {
		t.Fatalf("state after purge = %#v", state)
	}
	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load invalidated rollups: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rollups after purge = %#v", rows)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil {
		t.Fatalf("rebuild batch: %v", err)
	}
	rows, err = db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rebuilt rollups: %v", err)
	}
	if len(rows) != 1 || rows[0].Model != "new-model" || rows[0].TotalTokens != 2 {
		t.Fatalf("rebuilt rollups = %#v", rows)
	}
}
