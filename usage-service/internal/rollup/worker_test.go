package rollup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestWorkerCatchesUpInBatchesAndResumesAfterRestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := db.InsertEvents(ctx, []usage.Event{{
			EventHash:    "event-" + string(rune('a'+i)),
			TimestampMS:  1_700_000_000_000 + int64(i)*time.Hour.Milliseconds(),
			Timestamp:    "2023-11-14T22:13:20Z",
			Model:        "gpt-test",
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
			CreatedAtMS:  1_700_000_000_001,
		}}); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}

	worker := NewWorker(db, Config{BatchSize: 2, PollInterval: time.Millisecond})
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.CheckpointID != 2 || state.CoverageEventID != 2 || state.TargetEventID != 3 || state.Status != StatusCatchingUp {
		t.Fatalf("state after first batch = %#v", state)
	}

	// 新 worker 代表进程重启，应从持久化 coverage 继续而不是重复聚合。
	restartedWorker := NewWorker(db, Config{BatchSize: 2, PollInterval: time.Millisecond})
	if err := restartedWorker.RunOnce(ctx); err != nil {
		t.Fatalf("second batch after restart: %v", err)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load resumed state: %v", err)
	}
	if state.CheckpointID != 3 || state.CoverageEventID != 3 || state.TargetEventID != 3 || state.Status != StatusReady {
		t.Fatalf("resumed state = %#v", state)
	}
	if err := restartedWorker.RunOnce(ctx); err != nil {
		t.Fatalf("ready batch: %v", err)
	}
	state, err = db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load final state: %v", err)
	}
	if state.CheckpointID != 3 || state.Status != StatusReady {
		t.Fatalf("final state = %#v", state)
	}

	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups: %v", err)
	}
	if len(rows) != 3 || rows[0].Requests != 1 {
		t.Fatalf("rollups = %#v", rows)
	}
}

func TestWorkerIncludesLateTimestampEventInItsEventIDBucket(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.InsertEvents(ctx, []usage.Event{{
		EventHash: "on-time", TimestampMS: 1_700_000_000_000, Model: "gpt-test", TotalTokens: 1, CreatedAtMS: 1,
	}}); err != nil {
		t.Fatalf("insert on-time event: %v", err)
	}
	worker := NewWorker(db, Config{BatchSize: 1000})
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("initial batch: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{
		// timestamp 比已消费事件更早，但 event id 更大，必须进入同一小时 bucket。
		EventHash: "late", TimestampMS: 1_700_000_100_000, Model: "gpt-test", TotalTokens: 2, CreatedAtMS: 2,
	}}); err != nil {
		t.Fatalf("insert late event: %v", err)
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("late event batch: %v", err)
	}
	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups: %v", err)
	}
	if len(rows) != 1 || rows[0].Requests != 2 || rows[0].TotalTokens != 3 {
		t.Fatalf("late event rollups = %#v", rows)
	}
}
