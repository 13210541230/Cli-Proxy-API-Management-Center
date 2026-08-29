package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

type largeRollupExpected struct {
	requests, successes, failures                  int64
	input, output, reasoning, cached, cache, total int64
	latencySum, latencySamples, zeroCalls          int64
}

func TestHourlyRollupCatchesUpOneHundredThousandEventsInBoundedBatches(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	const totalEvents = 100000
	const insertBatch = 1000
	expected := make(map[string]largeRollupExpected)
	for start := 0; start < totalEvents; start += insertBatch {
		end := start + insertBatch
		events := make([]usage.Event, 0, end-start)
		for i := start; i < end; i++ {
			model := fmt.Sprintf("bulk-%d", i%4)
			input := int64(i % 7)
			output := int64(i % 11)
			reasoning := int64(i % 5)
			cached := int64(i % 3)
			cache := int64(i % 4)
			total := input + output + reasoning + cached + cache
			failed := i%13 == 0
			var latency *int64
			if i%2 == 0 {
				value := int64(i % 97)
				latency = &value
			}
			events = append(events, usage.Event{
				EventHash: fmt.Sprintf("large-%06d", i), TimestampMS: 1_700_000_000_000,
				Model: model, InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning,
				CachedTokens: cached, CacheTokens: cache, TotalTokens: total, LatencyMS: latency,
				Failed: failed, CreatedAtMS: int64(i + 1),
			})
			aggregate := expected[model]
			aggregate.requests++
			if failed {
				aggregate.failures++
			} else {
				aggregate.successes++
			}
			aggregate.input += input
			aggregate.output += output
			aggregate.reasoning += reasoning
			aggregate.cached += cached
			aggregate.cache += cache
			aggregate.total += total
			if latency != nil {
				aggregate.latencySum += *latency
				aggregate.latencySamples++
			}
			if total == 0 {
				aggregate.zeroCalls++
			}
			expected[model] = aggregate
		}
		if _, err := db.InsertEvents(ctx, events); err != nil {
			t.Fatalf("insert batch at %d: %v", start, err)
		}
	}

	rawCount, _, err := db.Counts(ctx)
	if err != nil {
		t.Fatalf("count raw events: %v", err)
	}
	if rawCount != totalEvents {
		t.Fatalf("raw count = %d, want %d", rawCount, totalEvents)
	}
	latestID, err := db.LatestUsageEventID(ctx)
	if err != nil {
		t.Fatalf("load raw high-water mark: %v", err)
	}

	processed := 0
	batches := 0
	const batchSize = 1000
	for {
		n, err := db.ApplyHourlyRollupBatch(ctx, batchSize)
		if err != nil {
			t.Fatalf("catch-up batch %d: %v", batches, err)
		}
		if n > batchSize {
			t.Fatalf("batch %d processed %d events, exceeds batch size %d", batches, n, batchSize)
		}
		batches++
		processed += n
		if n == 0 {
			break
		}
	}
	if processed != totalEvents || batches != 101 {
		t.Fatalf("processed=%d batches=%d, want 100000 events in 101 bounded calls", processed, batches)
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load final state: %v", err)
	}
	if state.CheckpointID != latestID || state.CoverageEventID != latestID || state.TargetEventID != latestID || state.Status != RollupStatusReady {
		t.Fatalf("final state = %#v, raw latest id = %d", state, latestID)
	}

	rows, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load final rollups: %v", err)
	}
	if len(rows) != len(expected) {
		t.Fatalf("rollup rows = %d, want %d", len(rows), len(expected))
	}
	for _, row := range rows {
		want, ok := expected[row.Model]
		if !ok {
			t.Fatalf("unexpected rollup model %q", row.Model)
		}
		assertLargeRollupFields(t, row, want)
		delete(expected, row.Model)
	}
	if len(expected) != 0 {
		t.Fatalf("missing rollup models: %#v", expected)
	}
	assertRollupMatchesRawSQL(t, db.db)

	if n, err := db.ApplyHourlyRollupBatch(ctx, 1000); err != nil || n != 0 {
		t.Fatalf("empty repeat batch = (%d, %v), want (0, nil)", n, err)
	}
	rowsAfterRepeat, err := db.LoadHourlyRollups(ctx, 0, 0)
	if err != nil {
		t.Fatalf("load rollups after repeat: %v", err)
	}
	if !reflect.DeepEqual(rows, rowsAfterRepeat) {
		t.Fatalf("repeat changed rollups: before=%#v after=%#v", rows, rowsAfterRepeat)
	}
}

func assertLargeRollupFields(t *testing.T, row HourlyRollup, want largeRollupExpected) {
	t.Helper()
	if row.Requests != want.requests || row.Successes != want.successes || row.Failures != want.failures ||
		row.InputTokens != want.input || row.OutputTokens != want.output || row.ReasoningTokens != want.reasoning ||
		row.CachedTokens != want.cached || row.CacheTokens != want.cache || row.TotalTokens != want.total ||
		row.LatencySumMS != want.latencySum || row.LatencySamples != want.latencySamples || row.ZeroTokenCalls != want.zeroCalls {
		t.Fatalf("rollup %q = %#v, want %#v", row.Model, row, want)
	}
}

func assertRollupMatchesRawSQL(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`select model, count(*), coalesce(sum(case when failed = 0 then 1 else 0 end), 0),
		coalesce(sum(case when failed != 0 then 1 else 0 end), 0), coalesce(sum(input_tokens), 0),
		coalesce(sum(output_tokens), 0), coalesce(sum(reasoning_tokens), 0), coalesce(sum(cached_tokens), 0),
		coalesce(sum(cache_tokens), 0), coalesce(sum(total_tokens), 0), coalesce(sum(latency_ms), 0),
		coalesce(sum(case when latency_ms is not null then 1 else 0 end), 0),
		coalesce(sum(case when input_tokens = 0 and output_tokens = 0 and reasoning_tokens = 0 and cached_tokens = 0 and cache_tokens = 0 and total_tokens = 0 then 1 else 0 end), 0)
		from usage_events group by model order by model`)
	if err != nil {
		t.Fatalf("raw rollup baseline query: %v", err)
	}
	baseline := make(map[string]largeRollupExpected)
	for rows.Next() {
		var model string
		var got largeRollupExpected
		if err := rows.Scan(&model, &got.requests, &got.successes, &got.failures, &got.input, &got.output,
			&got.reasoning, &got.cached, &got.cache, &got.total, &got.latencySum, &got.latencySamples, &got.zeroCalls); err != nil {
			rows.Close()
			t.Fatalf("scan raw rollup baseline: %v", err)
		}
		baseline[model] = got
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate raw rollup baseline: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close raw rollup baseline: %v", err)
	}
	for model, want := range baseline {
		var rollup HourlyRollup
		if err := db.QueryRow(`select requests, successes, failures, input_tokens, output_tokens, reasoning_tokens,
			cached_tokens, cache_tokens, total_tokens, latency_sum_ms, latency_samples, zero_token_calls
			from usage_hourly_rollups where model = ?`, model).Scan(&rollup.Requests, &rollup.Successes, &rollup.Failures,
			&rollup.InputTokens, &rollup.OutputTokens, &rollup.ReasoningTokens, &rollup.CachedTokens, &rollup.CacheTokens,
			&rollup.TotalTokens, &rollup.LatencySumMS, &rollup.LatencySamples, &rollup.ZeroTokenCalls); err != nil {
			t.Fatalf("load rollup baseline for %q: %v", model, err)
		}
		assertLargeRollupFields(t, rollup, want)
	}
}
