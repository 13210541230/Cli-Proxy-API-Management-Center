package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestHourlyRollupFailedBatchRollsBackCheckpointAndRows(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{{
		EventHash: "event-failure", TimestampMS: 1_700_000_000_000,
		Timestamp: "2023-11-14T22:13:20Z", Model: "gpt-test", CreatedAtMS: 1,
	}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.db.Exec(`create trigger fail_rollup before insert on usage_hourly_rollups
		begin select raise(abort, 'injected rollup failure'); end`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err == nil {
		t.Fatal("ApplyHourlyRollupBatch should fail")
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load failed state: %v", err)
	}
	if state.CheckpointID != 0 || state.CoverageEventID != 0 || state.TargetEventID != 0 || state.Status != RollupStatusFailed {
		t.Fatalf("failed state = %#v", state)
	}
	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups after failure: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("partial rollups = %#v", rows)
	}

	if _, err := db.db.Exec(`drop trigger fail_rollup`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if n, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil || n != 1 {
		t.Fatalf("retry batch = (%d, %v), want (1, nil)", n, err)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load retried state: %v", err)
	}
	if state.CheckpointID != 1 || state.CoverageEventID != 1 {
		t.Fatalf("retried state = %#v", state)
	}
}
