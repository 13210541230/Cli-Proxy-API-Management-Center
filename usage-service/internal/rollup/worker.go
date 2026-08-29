// Package rollup 提供 usage_events 到小时派生层的后台追赶 worker。
package rollup

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

const defaultPollInterval = time.Second

const (
	StatusPending    = store.RollupStatusPending
	StatusCatchingUp = store.RollupStatusCatchingUp
	StatusReady      = store.RollupStatusReady
	StatusFailed     = store.RollupStatusFailed
)

// Config controls the bounded background catch-up loop.
type Config struct {
	BatchSize    int
	PollInterval time.Duration
}

// Worker 单实例消费 SQLite 中的 usage_events checkpoint。
type Worker struct {
	store        *store.Store
	batchSize    int
	pollInterval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWorker creates a worker with a bounded default batch of 1000 events.
func NewWorker(s *store.Store, cfg Config) *Worker {
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	return &Worker{store: s, batchSize: batchSize, pollInterval: pollInterval}
}

// Start launches catch-up asynchronously and never blocks the HTTP startup path.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.wg.Add(1)
	go w.run(runCtx)
}

// Stop cancels the loop and waits until its goroutine exits.
func (w *Worker) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	w.wg.Wait()
	w.mu.Lock()
	w.cancel = nil
	w.mu.Unlock()
}

// RunOnce executes exactly one bounded batch, primarily for deterministic tests
// and for callers that want to drive catch-up without starting a goroutine.
func (w *Worker) RunOnce(ctx context.Context) error {
	_, err := w.store.ApplyHourlyRollupBatch(ctx, w.batchSize)
	return err
}

func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		if pending, err := w.store.RetryPendingRollupFailure(ctx); pending {
			if err != nil {
				log.Printf("rollup: pending failure state retry: %v", err)
			}
			// 先让 failed 状态对读取方可见，再进入下一次成功追赶。
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}
		processed, err := w.store.ApplyHourlyRollupBatch(ctx, w.batchSize)
		if err != nil && ctx.Err() == nil {
			log.Printf("rollup: batch failed: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil && processed > 0 {
			// 批次未耗尽时立即处理下一批；每批仍受 batchSize 限制。
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
