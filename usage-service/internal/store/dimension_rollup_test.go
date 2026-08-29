package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestDailyDimensionRollupSharesCheckpointAndPurgesWithHourlyData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	const day = int64(24 * 60 * 60 * 1000)
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "dimension-a", TimestampMS: day + 100, Timestamp: "a", Model: "model-a", Provider: "provider-a", AuthIndex: "auth-a", APIKeyHash: "hash-a", AccountSnapshot: "account-a", InputTokens: 3, TotalTokens: 3, CreatedAtMS: 1},
		{EventHash: "dimension-b", TimestampMS: day + 200, Timestamp: "b", Model: "model-a", Provider: "provider-a", AuthIndex: "auth-b", APIKeyHash: "hash-b", AccountSnapshot: "account-b", OutputTokens: 4, TotalTokens: 4, Failed: true, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if n, err := db.ApplyHourlyRollupBatch(ctx, 100); err != nil || n != 2 {
		t.Fatalf("apply rollup = %d, %v", n, err)
	}
	wantRows := map[string]int{"account": 2, "api_key": 2, "provider": 1, "auth_index": 2}
	for _, dimension := range []string{"account", "api_key", "provider", "auth_index"} {
		rows, err := db.LoadDailyDimensionRollups(ctx, day, 2*day, dimension)
		if err != nil {
			t.Fatalf("load %s rows: %v", dimension, err)
		}
		if len(rows) != wantRows[dimension] {
			t.Fatalf("%s rows = %#v, want %d", dimension, rows, wantRows[dimension])
		}
		for _, row := range rows {
			if row.BucketMS != day {
				t.Fatalf("%s row bucket = %d, want daily bucket %d", dimension, row.BucketMS, day)
			}
		}
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil || state.CoverageEventID != 2 || state.Status != RollupStatusReady {
		t.Fatalf("state = %#v, %v", state, err)
	}
	if n, err := db.PurgeEventsBefore(ctx, 2*day); err != nil || n != 2 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	rows, err := db.LoadDailyDimensionRollups(ctx, 0, 0, "account")
	if err != nil {
		t.Fatalf("load purged daily rows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("daily rows after purge = %#v", rows)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil || state.CoverageEventID != 0 || state.Status != RollupStatusPending {
		t.Fatalf("state after purge = %#v, %v", state, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func TestOpeningT1DatabaseWithoutDailyRowsResetsSharedCoverage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "legacy-rollup", TimestampMS: 1_700_000_000_000, Timestamp: "legacy", Model: "model", CreatedAtMS: 1}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 10); err != nil {
		t.Fatalf("apply rollup: %v", err)
	}
	if _, err := db.db.Exec(`delete from usage_daily_dimension_rollups`); err != nil {
		t.Fatalf("remove daily rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	state, err := reopened.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load reset state: %v", err)
	}
	if state.CoverageEventID != 0 || state.TargetEventID != 0 || state.Status != RollupStatusPending {
		t.Fatalf("reset state = %#v", state)
	}
	var hourlyRows int
	if err := reopened.db.QueryRow(`select count(*) from usage_hourly_rollups`).Scan(&hourlyRows); err != nil {
		t.Fatalf("count hourly rows: %v", err)
	}
	if hourlyRows != 0 {
		t.Fatalf("hourly rows after reset = %d", hourlyRows)
	}
}
