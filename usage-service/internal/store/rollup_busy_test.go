package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestHourlyRollupBusyDoesNotAdvanceAndCanRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, err := db.InsertEvents(context.Background(), []usage.Event{{
		EventHash: "busy-event", TimestampMS: 1_700_000_000_000, Model: "gpt-test", TotalTokens: 1, CreatedAtMS: 1,
	}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec(`begin immediate`); err != nil {
		t.Fatalf("acquire sqlite write lock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = db.ApplyHourlyRollupBatch(ctx, 1000)
	if err == nil || (!strings.Contains(strings.ToLower(err.Error()), "deadline") && !strings.Contains(strings.ToLower(err.Error()), "busy")) {
		t.Fatalf("busy batch error = %v, want deadline/busy failure", err)
	}
	if _, err := locker.Exec(`rollback`); err != nil {
		t.Fatalf("release sqlite write lock: %v", err)
	}

	state, err := db.LoadRollupState(context.Background())
	if err != nil {
		t.Fatalf("load state after busy: %v", err)
	}
	if state.CheckpointID != 0 || state.CoverageEventID != 0 {
		t.Fatalf("state after busy = %#v", state)
	}
	if n, err := db.ApplyHourlyRollupBatch(context.Background(), 1000); err != nil || n != 1 {
		t.Fatalf("retry batch = (%d, %v), want (1, nil)", n, err)
	}
}

func TestReadyTailBusyFailureBecomesObservableThenSuccessfulRetryClearsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "ready", TimestampMS: 1_700_000_000_000, Model: "gpt-test", TotalTokens: 1, CreatedAtMS: 1}}); err != nil {
		t.Fatalf("insert ready event: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil {
		t.Fatalf("initial batch: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "tail", TimestampMS: 1_700_000_100_000, Model: "gpt-test", TotalTokens: 2, CreatedAtMS: 2}}); err != nil {
		t.Fatalf("insert tail event: %v", err)
	}

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec(`begin immediate`); err != nil {
		t.Fatalf("acquire sqlite write lock: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 1000); err == nil {
		t.Fatal("busy tail batch should fail")
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load state while locked: %v", err)
	}
	if state.CheckpointID != 1 || state.CoverageEventID != 1 || state.Status != RollupStatusFailed || state.LastError == "" {
		t.Fatalf("state while locked = %#v", state)
	}
	if _, err := locker.Exec(`rollback`); err != nil {
		t.Fatalf("release sqlite write lock: %v", err)
	}

	persisted, err := db.RetryPendingRollupFailure(ctx)
	if err != nil || !persisted {
		t.Fatalf("persist pending failure = (%t, %v)", persisted, err)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load failed state: %v", err)
	}
	if state.Status != RollupStatusFailed || state.LastError == "" || state.CheckpointID != 1 || state.CoverageEventID != 1 {
		t.Fatalf("observable failed state = %#v", state)
	}
	if n, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil || n != 1 {
		t.Fatalf("successful retry = (%d, %v)", n, err)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load ready state: %v", err)
	}
	if state.Status != RollupStatusReady || state.LastError != "" || state.CheckpointID != 2 || state.CoverageEventID != 2 {
		t.Fatalf("cleared ready state = %#v", state)
	}
}
