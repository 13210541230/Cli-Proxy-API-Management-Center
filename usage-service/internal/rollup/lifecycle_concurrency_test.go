package rollup

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestWorkerStartStopAndParentCancellationAreIdempotent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	worker := NewWorker(db, Config{BatchSize: 10, PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(started)
	}()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start blocked")
	}
	// 重复 Start 不得创建第二个消费循环。
	worker.Start(ctx)
	cancel()
	stopped := make(chan struct{})
	go func() {
		worker.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop did not return after parent cancellation")
	}
	// 重复 Stop 和重新启动/停止都应安全返回。
	worker.Stop()
	secondCtx, secondCancel := context.WithCancel(context.Background())
	worker.Start(secondCtx)
	secondCancel()
	worker.Stop()
	worker.Stop()
}

func TestWorkerConcurrentInsertAndReadHasNoDuplicateRollups(t *testing.T) {
	// 并发生命周期测试不依赖文件持久化，使用内存库避免 Windows 清理临时 SQLite 文件时的句柄竞争。
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	worker := NewWorker(db, Config{BatchSize: 10, PollInterval: time.Millisecond})
	worker.Start(ctx)
	worker.Start(ctx)

	const total = 100
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	readerDone := make(chan struct{})
	readerErr := make(chan error, 1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-readerDone:
				return
			default:
			}
			if _, err := db.LoadHourlyRollups(ctx, 0, 0); err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					select {
					case readerErr <- err:
					default:
					}
				}
				return
			}
		}
	}()

	for start := 0; start < total; start += 10 {
		events := make([]usage.Event, 0, 10)
		for i := start; i < start+10; i++ {
			events = append(events, usage.Event{
				EventHash:   "concurrent-" + string(rune('a'+i/26)) + string(rune('a'+i%26)),
				TimestampMS: 1_700_000_000_000,
				Model:       "concurrent-model",
				TotalTokens: 1,
				CreatedAtMS: int64(i + 1),
			})
		}
		if _, err := db.InsertEvents(ctx, events); err != nil {
			t.Fatalf("insert batch %d: %v", start/10, err)
		}
	}
	latestID, err := db.LatestUsageEventID(ctx)
	if err != nil {
		t.Fatalf("load raw high-water mark: %v", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := db.LoadRollupState(ctx)
		if err != nil {
			t.Fatalf("load state: %v", err)
		}
		if state.CoverageEventID == latestID && state.CheckpointID == latestID && state.Status == StatusReady {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("worker did not catch up: %#v, raw latest id = %d", state, latestID)
		case <-ticker.C:
		}
	}
	cancel()
	worker.Stop()
	close(readerDone)
	readerWG.Wait()
	select {
	case err := <-readerErr:
		t.Fatalf("concurrent reader: %v", err)
	default:
	}
	rows, err := db.LoadHourlyRollups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("load final rollups: %v", err)
	}
	if len(rows) != 1 || rows[0].Requests != total || rows[0].TotalTokens != total {
		t.Fatalf("final concurrent rollups = %#v", rows)
	}
}
