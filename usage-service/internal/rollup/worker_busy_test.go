package rollup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestWorkerBusyFailureSurvivesCloseReopenAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "restart-ready", TimestampMS: 1_700_000_000_000, Model: "restart-model", TotalTokens: 1, CreatedAtMS: 1}}); err != nil {
		db.Close()
		t.Fatalf("insert ready event: %v", err)
	}
	worker := NewWorker(db, Config{BatchSize: 1000, PollInterval: 10 * time.Millisecond})
	if err := worker.RunOnce(ctx); err != nil {
		db.Close()
		t.Fatalf("initial batch: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "restart-tail", TimestampMS: 1_700_000_100_000, Model: "restart-model", TotalTokens: 2, CreatedAtMS: 2}}); err != nil {
		db.Close()
		t.Fatalf("insert tail event: %v", err)
	}

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		db.Close()
		t.Fatalf("open lock connection: %v", err)
	}
	if _, err := locker.Exec(`begin immediate`); err != nil {
		locker.Close()
		db.Close()
		t.Fatalf("acquire sqlite write lock: %v", err)
	}
	_, applyErr := db.ApplyHourlyRollupBatch(ctx, 1000)
	if applyErr == nil || (!strings.Contains(strings.ToLower(applyErr.Error()), "busy") && !strings.Contains(strings.ToLower(applyErr.Error()), "deadline")) {
		t.Fatalf("busy batch error = %v, want busy/deadline failure", applyErr)
	}
	if !db.HasPendingRollupFailure() {
		locker.Exec(`rollback`)
		locker.Close()
		db.Close()
		t.Fatalf("busy failure was not retained before restart")
	}
	if err := db.Close(); err != nil {
		locker.Close()
		t.Fatalf("close store before restart: %v", err)
	}
	if _, err := locker.Exec(`rollback`); err != nil {
		locker.Close()
		t.Fatalf("release sqlite write lock: %v", err)
	}
	if err := locker.Close(); err != nil {
		t.Fatalf("close lock connection: %v", err)
	}

	restarted, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	state, err := restarted.LoadRollupState(ctx)
	if err != nil {
		restarted.Close()
		t.Fatalf("load state after reopen: %v", err)
	}
	if state.Status != StatusFailed || state.LastError == "" || state.CheckpointID != 1 || state.CoverageEventID != 1 {
		restarted.Close()
		t.Fatalf("reopened failed state = %#v", state)
	}
	markerPath := path + ".rollup-failure"
	if _, err := os.Stat(markerPath); err != nil {
		restarted.Close()
		t.Fatalf("failure marker after reopen: %v", err)
	}

	restartedWorker := NewWorker(restarted, Config{BatchSize: 1000, PollInterval: 10 * time.Millisecond})
	t.Cleanup(func() {
		restartedWorker.Stop()
		_ = restarted.Close()
	})
	restartedWorker.Start(ctx)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := restarted.LoadRollupState(ctx)
		if err != nil {
			t.Fatalf("load recovered state: %v", err)
		}
		if state.Status == StatusReady && state.LastError == "" && state.CheckpointID == 2 && state.CoverageEventID == 2 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("worker did not recover after restart: %#v", state)
		case <-ticker.C:
		}
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failure marker after successful retry = %v", err)
	}
	rows, err := restarted.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load recovered rollups: %v", err)
	}
	if len(rows) != 1 || rows[0].Requests != 2 || rows[0].TotalTokens != 3 {
		t.Fatalf("recovered rollups = %#v", rows)
	}
	restartedWorker.Stop()
	if err := restarted.Close(); err != nil {
		t.Fatalf("close restarted store: %v", err)
	}
}

func TestWorkerPersistsBusyFailureAfterUnlockThenRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "ready", TimestampMS: 1_700_000_000_000, Model: "worker-model", TotalTokens: 1, CreatedAtMS: 1}}); err != nil {
		t.Fatalf("insert ready event: %v", err)
	}
	worker := NewWorker(db, Config{BatchSize: 1000, PollInterval: 25 * time.Millisecond})
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("initial batch: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "tail", TimestampMS: 1_700_000_100_000, Model: "worker-model", TotalTokens: 2, CreatedAtMS: 2}}); err != nil {
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
	worker.Start(ctx)
	// 等待 worker 明确记录 pending failure，再释放外部锁，避免依赖固定 sleep。
	pendingDeadline := time.NewTimer(2 * time.Second)
	pendingTicker := time.NewTicker(5 * time.Millisecond)
	for !db.HasPendingRollupFailure() {
		select {
		case <-pendingDeadline.C:
			pendingTicker.Stop()
			t.Fatalf("worker did not record pending busy failure")
		case <-pendingTicker.C:
		}
	}
	pendingTicker.Stop()
	pendingDeadline.Stop()
	if _, err := locker.Exec(`rollback`); err != nil {
		t.Fatalf("release sqlite write lock: %v", err)
	}

	failedDeadline := time.NewTimer(2 * time.Second)
	defer failedDeadline.Stop()
	failedTicker := time.NewTicker(5 * time.Millisecond)
	defer failedTicker.Stop()
	failedSeen := false
	for !failedSeen {
		state, err := db.LoadRollupState(ctx)
		if err != nil {
			t.Fatalf("load worker failure state: %v", err)
		}
		failedSeen = state.Status == StatusFailed && state.LastError != ""
		if failedSeen {
			if state.CheckpointID != 1 || state.CoverageEventID != 1 {
				t.Fatalf("failed state advanced coverage: %#v", state)
			}
			break
		}
		select {
		case <-failedDeadline.C:
			t.Fatalf("worker did not persist failed state")
		case <-failedTicker.C:
		}
	}

	readyDeadline := time.NewTimer(2 * time.Second)
	defer readyDeadline.Stop()
	readyTicker := time.NewTicker(5 * time.Millisecond)
	defer readyTicker.Stop()
	for {
		state, err := db.LoadRollupState(ctx)
		if err != nil {
			t.Fatalf("load worker ready state: %v", err)
		}
		if state.Status == StatusReady && state.LastError == "" && state.CheckpointID == 2 && state.CoverageEventID == 2 {
			break
		}
		select {
		case <-readyDeadline.C:
			t.Fatalf("worker did not recover: %#v", state)
		case <-readyTicker.C:
		}
	}
	cancel()
	worker.Stop()
	rows, err := db.LoadHourlyRollups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("load final worker rollup: %v", err)
	}
	if len(rows) != 1 || rows[0].Requests != 2 || rows[0].TotalTokens != 3 {
		t.Fatalf("final worker rollup = %#v", rows)
	}
}
