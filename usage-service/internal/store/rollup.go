package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	RollupStatusPending        = "pending"
	RollupStatusCatchingUp     = "catching_up"
	RollupStatusReady          = "ready"
	RollupStatusFailed         = "failed"
	defaultRollupBatchSize     = 1000
	hourMilliseconds           = int64(60 * 60 * 1000)
	rollupBusyTimeoutMS        = 200
	defaultSQLiteBusyTimeout   = 5000
	rollupFailureMarkerVersion = 1
)

type rollupFailureMarker struct {
	Version         int    `json:"version"`
	Error           string `json:"error"`
	OccurredAtMS    int64  `json:"occurredAtMs"`
	CoverageEventID int64  `json:"coverageEventId"`
}

// rollupFailureMarkerPath enables the sidecar only for ordinary file-backed
// databases. In-memory databases and URIs do not reliably map to a single file,
// so they safely fall back to in-process pending state.
func rollupFailureMarkerPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == ":memory:" || strings.HasPrefix(trimmed, "file:") || strings.ContainsAny(trimmed, "?#") {
		return ""
	}
	return trimmed + ".rollup-failure"
}

// HourlyRollup is a rebuildable hourly derived aggregate of usage_events; it
// never stores raw requests or plaintext credentials.
type HourlyRollup struct {
	BucketMS                 int64  `json:"bucketMs"`
	Model                    string `json:"model"`
	Requests                 int64  `json:"requests"`
	Successes                int64  `json:"successes"`
	Failures                 int64  `json:"failures"`
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	ReasoningTokens          int64  `json:"reasoningTokens"`
	CachedTokens             int64  `json:"cachedTokens"`
	CacheTokens              int64  `json:"cacheTokens"`
	TotalTokens              int64  `json:"totalTokens"`
	LatencySumMS             int64  `json:"latencySumMs"`
	LatencySamples           int64  `json:"latencySamples"`
	ZeroTokenCalls           int64  `json:"zeroTokenCalls"`
	LastSeenMS               int64  `json:"lastSeenMs"`
	BillablePromptTokens     int64  `json:"billablePromptTokens"`
	BillableCacheTokens      int64  `json:"billableCacheTokens"`
	BillableCompletionTokens int64  `json:"billableCompletionTokens"`
}

// DailyDimensionRollup stores the same additive metrics as the hourly layer,
// fanned out by a safe identity dimension and model. The dimension key is a
// hash or snapshot, never a plaintext API credential.
type DailyDimensionRollup struct {
	BucketMS     int64
	Dimension    string
	DimensionKey string
	Model        string
	Metric       UsageMetric
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func updateUsageMetric(metric *UsageMetric, timestampMS int64, failed bool, input, output, reasoning, cached, cache, total, latency int64, latencyValid bool) {
	metric.Requests++
	if timestampMS > metric.LastSeenMS {
		metric.LastSeenMS = timestampMS
	}
	if failed {
		metric.Failures++
	} else {
		metric.Successes++
	}
	metric.InputTokens += input
	metric.OutputTokens += output
	metric.ReasoningTokens += reasoning
	metric.CachedTokens += cached
	metric.CacheTokens += cache
	metric.TotalTokens += total
	if latencyValid {
		metric.LatencySumMS += latency
		metric.LatencySamples++
	}
	if input == 0 && output == 0 && reasoning == 0 && cached == 0 && cache == 0 && total == 0 {
		metric.ZeroTokenCalls++
	}
	cachedForBilling := cached
	if cache > cachedForBilling {
		cachedForBilling = cache
	}
	metric.BillablePromptTokens += maxInt64(0, input-cachedForBilling)
	metric.BillableCacheTokens += cachedForBilling
	metric.BillableCompletionTokens += maxInt64(0, output)
}

func updateHourlyRollupMetric(rollup *HourlyRollup, timestampMS int64, failed bool, input, output, reasoning, cached, cache, total, latency int64, latencyValid bool) {
	metric := UsageMetric{
		Requests: rollup.Requests, Successes: rollup.Successes, Failures: rollup.Failures,
		InputTokens: rollup.InputTokens, OutputTokens: rollup.OutputTokens, ReasoningTokens: rollup.ReasoningTokens,
		CachedTokens: rollup.CachedTokens, CacheTokens: rollup.CacheTokens, TotalTokens: rollup.TotalTokens,
		LatencySumMS: rollup.LatencySumMS, LatencySamples: rollup.LatencySamples, ZeroTokenCalls: rollup.ZeroTokenCalls,
		LastSeenMS:           rollup.LastSeenMS,
		BillablePromptTokens: rollup.BillablePromptTokens, BillableCacheTokens: rollup.BillableCacheTokens,
		BillableCompletionTokens: rollup.BillableCompletionTokens,
	}
	updateUsageMetric(&metric, timestampMS, failed, input, output, reasoning, cached, cache, total, latency, latencyValid)
	rollup.Requests, rollup.Successes, rollup.Failures = metric.Requests, metric.Successes, metric.Failures
	rollup.InputTokens, rollup.OutputTokens, rollup.ReasoningTokens = metric.InputTokens, metric.OutputTokens, metric.ReasoningTokens
	rollup.CachedTokens, rollup.CacheTokens, rollup.TotalTokens = metric.CachedTokens, metric.CacheTokens, metric.TotalTokens
	rollup.LatencySumMS, rollup.LatencySamples, rollup.ZeroTokenCalls = metric.LatencySumMS, metric.LatencySamples, metric.ZeroTokenCalls
	rollup.LastSeenMS = metric.LastSeenMS
	rollup.BillablePromptTokens = metric.BillablePromptTokens
	rollup.BillableCacheTokens = metric.BillableCacheTokens
	rollup.BillableCompletionTokens = metric.BillableCompletionTokens
}

// RollupState describes derived rollup consumption progress and availability.
// CheckpointID and CoverageEventID are always equal; CheckpointID remains for
// compatibility with earlier state readers.
type RollupState struct {
	CheckpointID    int64  `json:"checkpointId"`
	CoverageEventID int64  `json:"coverageEventId"`
	TargetEventID   int64  `json:"targetEventId"`
	Status          string `json:"status"`
	LastError       string `json:"lastError,omitempty"`
	UpdatedAtMS     int64  `json:"updatedAtMs"`
}

// ensureRollupStateSchema adds missing high-water fields for existing state
// tables created by earlier experimental builds.
func (s *Store) ensureRollupStateSchema() error {
	if err := s.ensureTableColumns("usage_rollup_state", []tableColumn{
		{name: "coverage_event_id", definition: "coverage_event_id integer not null default 0"},
		{name: "target_event_id", definition: "target_event_id integer not null default 0"},
	}); err != nil {
		return err
	}
	metricColumns := []string{"zero_token_calls", "last_seen_ms", "billable_prompt_tokens", "billable_cache_tokens", "billable_completion_tokens"}
	needsRebuild := false
	for _, column := range metricColumns {
		exists, err := s.tableHasColumn("usage_hourly_rollups", column)
		if err != nil {
			return err
		}
		needsRebuild = needsRebuild || !exists
	}
	if err := s.ensureTableColumns("usage_hourly_rollups", []tableColumn{
		{name: "zero_token_calls", definition: "zero_token_calls integer not null default 0"},
		{name: "last_seen_ms", definition: "last_seen_ms integer not null default 0"},
		{name: "billable_prompt_tokens", definition: "billable_prompt_tokens integer not null default 0"},
		{name: "billable_cache_tokens", definition: "billable_cache_tokens integer not null default 0"},
		{name: "billable_completion_tokens", definition: "billable_completion_tokens integer not null default 0"},
	}); err != nil {
		return err
	}
	for _, column := range []string{"last_seen_ms", "billable_prompt_tokens", "billable_cache_tokens", "billable_completion_tokens"} {
		exists, err := s.tableHasColumn("usage_daily_dimension_rollups", column)
		if err != nil {
			return err
		}
		needsRebuild = needsRebuild || !exists
	}
	if err := s.ensureTableColumns("usage_daily_dimension_rollups", []tableColumn{
		{name: "last_seen_ms", definition: "last_seen_ms integer not null default 0"},
		{name: "billable_prompt_tokens", definition: "billable_prompt_tokens integer not null default 0"},
		{name: "billable_cache_tokens", definition: "billable_cache_tokens integer not null default 0"},
		{name: "billable_completion_tokens", definition: "billable_completion_tokens integer not null default 0"},
	}); err != nil {
		return err
	}
	if needsRebuild {
		if _, err := s.db.Exec(`delete from usage_hourly_rollups`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`delete from usage_daily_dimension_rollups`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`update usage_rollup_state set checkpoint_id = 0, coverage_event_id = 0, target_event_id = 0, status = ?, last_error = null, updated_at_ms = ? where id = 1`, RollupStatusPending, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`update usage_rollup_state
		set coverage_event_id = checkpoint_id, target_event_id = checkpoint_id
		where coverage_event_id = 0 and checkpoint_id > 0`)
	return err
}

// ensureDimensionRollupSchema invalidates the hourly layer once when the new
// daily fan-out table is introduced into a database that was already caught
// up by T1. This makes the shared checkpoint truthful instead of presenting
// an empty dimension rollup as complete.
func (s *Store) ensureDimensionRollupSchema() error {
	var coverage, rows int64
	if err := s.db.QueryRow(`select coverage_event_id from usage_rollup_state where id = 1`).Scan(&coverage); err != nil {
		return err
	}
	if err := s.db.QueryRow(`select count(*) from usage_daily_dimension_rollups`).Scan(&rows); err != nil {
		return err
	}
	if coverage <= 0 || rows > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`delete from usage_hourly_rollups`); err != nil {
		return err
	}
	if _, err := tx.Exec(`update usage_rollup_state set checkpoint_id = 0, coverage_event_id = 0, target_event_id = 0, status = ?, last_error = null, updated_at_ms = ? where id = 1`, RollupStatusPending, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// ApplyHourlyRollupBatch locks the target, reads events up to the id high-water mark,
// aggregates them, and advances the checkpoint in one transaction.
// Rollups therefore represent only id <= coverage_event_id; late timestamped events
// are still consumed according to their event ids. Any failure rolls back both the
// aggregates and checkpoint, then records the failed state separately.
func (s *Store) ApplyHourlyRollupBatch(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = defaultRollupBatchSize
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, s.recordRollupFailure(err)
	}
	defer func() {
		_ = tx.Rollback()
		s.restoreSQLiteBusyTimeout()
	}()
	// Rollup waits only a bounded window for external write locks so it does not hold
	// the single connection and block the collector or HTTP paths.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`pragma busy_timeout = %d`, rollupBusyTimeoutMS)); err != nil {
		return s.rollupBatchError(tx, err)
	}

	var state RollupState
	if err := tx.QueryRowContext(ctx, `select checkpoint_id, coverage_event_id, target_event_id
		from usage_rollup_state where id = 1`).Scan(&state.CheckpointID, &state.CoverageEventID, &state.TargetEventID); err != nil {
		return s.rollupBatchError(tx, err)
	}
	if state.CoverageEventID < state.CheckpointID {
		state.CoverageEventID = state.CheckpointID
	}
	// Write state first to acquire the SQLite write lock before reading the target;
	// newly inserted events cannot change the in-transaction target.
	if _, err := tx.ExecContext(ctx, `update usage_rollup_state set status = ?, updated_at_ms = ? where id = 1`, RollupStatusCatchingUp, time.Now().UnixMilli()); err != nil {
		return s.rollupBatchError(tx, err)
	}
	if err := tx.QueryRowContext(ctx, `select coalesce(max(id), 0) from usage_events`).Scan(&state.TargetEventID); err != nil {
		return s.rollupBatchError(tx, err)
	}
	if state.TargetEventID < state.CoverageEventID {
		state.TargetEventID = state.CoverageEventID
	}
	if _, err := tx.ExecContext(ctx, `update usage_rollup_state set status = ?, target_event_id = ?, updated_at_ms = ? where id = 1`, RollupStatusCatchingUp, state.TargetEventID, time.Now().UnixMilli()); err != nil {
		return s.rollupBatchError(tx, err)
	}

	rows, err := tx.QueryContext(ctx, `select id, timestamp_ms, model, provider, auth_index, api_key_hash, account_snapshot, reasoning_effort,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, total_tokens, latency_ms, failed
		from usage_events where id > ? and id <= ? order by id asc limit ?`, state.CoverageEventID, state.TargetEventID, batchSize)
	if err != nil {
		return s.rollupBatchError(tx, err)
	}
	type eventRow struct {
		id                                                               int64
		timestampMS, bucket, input, output, reasoning, cached, cache     int64
		total, latency                                                   int64
		model, provider, authIndex, apiKeyHash, account, reasoningEffort string
		latencyValid                                                     bool
		failed                                                           bool
	}
	events := make([]eventRow, 0, batchSize)
	for rows.Next() {
		var event eventRow
		var provider, authIndex, apiKeyHash, account, reasoningEffort sql.NullString
		var latency sql.NullInt64
		var failed int
		if err := rows.Scan(&event.id, &event.timestampMS, &event.model, &provider, &authIndex, &apiKeyHash, &account, &reasoningEffort,
			&event.input, &event.output, &event.reasoning, &event.cached, &event.cache, &event.total, &latency, &failed); err != nil {
			_ = rows.Close()
			return s.rollupBatchError(tx, err)
		}
		event.bucket = event.timestampMS - event.timestampMS%hourMilliseconds
		event.provider = provider.String
		event.authIndex = authIndex.String
		event.apiKeyHash = apiKeyHash.String
		event.account = account.String
		event.reasoningEffort = reasoningEffort.String
		event.latencyValid = latency.Valid
		if latency.Valid {
			event.latency = latency.Int64
		}
		event.failed = failed != 0
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return s.rollupBatchError(tx, err)
	}
	if err := rows.Close(); err != nil {
		return s.rollupBatchError(tx, err)
	}

	if len(events) == 0 {
		if _, err := tx.ExecContext(ctx, `update usage_rollup_state set checkpoint_id = ?, coverage_event_id = ?, status = ?, last_error = null, updated_at_ms = ? where id = 1`, state.CoverageEventID, state.CoverageEventID, RollupStatusReady, time.Now().UnixMilli()); err != nil {
			return s.rollupBatchError(tx, err)
		}
		if err := tx.Commit(); err != nil {
			return 0, s.recordRollupFailure(err)
		}
		if err := s.clearPendingRollupFailure(); err != nil {
			return 0, fmt.Errorf("clear successful rollup failure marker: %w", err)
		}
		return 0, nil
	}

	aggregates := make(map[string]HourlyRollup)
	dimensionAggregates := make(map[string]DailyDimensionRollup)
	for _, event := range events {
		key := fmt.Sprintf("%d\x00%s", event.bucket, event.model)
		aggregate := aggregates[key]
		aggregate.BucketMS = event.bucket
		aggregate.Model = event.model
		updateHourlyRollupMetric(&aggregate, event.timestampMS, event.failed, event.input, event.output, event.reasoning, event.cached, event.cache, event.total, event.latency, event.latencyValid)
		aggregates[key] = aggregate

		dimensionBucket := event.bucket - event.bucket%dayMilliseconds
		for _, dimension := range []struct{ name, key string }{
			{name: "account", key: event.account},
			{name: "api_key", key: event.apiKeyHash},
			{name: "provider", key: event.provider},
			{name: "auth_index", key: event.authIndex},
			{name: "reasoning_effort", key: event.reasoningEffort},
		} {
			dimensionKey := fmt.Sprintf("%d\x00%s\x00%s\x00%s", dimensionBucket, dimension.name, dimension.key, event.model)
			dimensionAggregate := dimensionAggregates[dimensionKey]
			dimensionAggregate.BucketMS = dimensionBucket
			dimensionAggregate.Dimension = dimension.name
			dimensionAggregate.DimensionKey = dimension.key
			dimensionAggregate.Model = event.model
			updateUsageMetric(&dimensionAggregate.Metric, event.timestampMS, event.failed, event.input, event.output, event.reasoning, event.cached, event.cache, event.total, event.latency, event.latencyValid)
			dimensionAggregates[dimensionKey] = dimensionAggregate
		}
	}

	for _, aggregate := range aggregates {
		if _, err := tx.ExecContext(ctx, `insert into usage_hourly_rollups(
			bucket_ms, model, requests, successes, failures, input_tokens, output_tokens,
			reasoning_tokens, cached_tokens, cache_tokens, total_tokens,
			billable_prompt_tokens, billable_cache_tokens, billable_completion_tokens,
			latency_sum_ms, latency_samples, zero_token_calls, last_seen_ms
		) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 on conflict(bucket_ms, model) do update set
			requests = usage_hourly_rollups.requests + excluded.requests,
			successes = usage_hourly_rollups.successes + excluded.successes,
			failures = usage_hourly_rollups.failures + excluded.failures,
			input_tokens = usage_hourly_rollups.input_tokens + excluded.input_tokens,
			output_tokens = usage_hourly_rollups.output_tokens + excluded.output_tokens,
			reasoning_tokens = usage_hourly_rollups.reasoning_tokens + excluded.reasoning_tokens,
			cached_tokens = usage_hourly_rollups.cached_tokens + excluded.cached_tokens,
			cache_tokens = usage_hourly_rollups.cache_tokens + excluded.cache_tokens,
			total_tokens = usage_hourly_rollups.total_tokens + excluded.total_tokens,
			latency_sum_ms = usage_hourly_rollups.latency_sum_ms + excluded.latency_sum_ms,
			latency_samples = usage_hourly_rollups.latency_samples + excluded.latency_samples,
			zero_token_calls = usage_hourly_rollups.zero_token_calls + excluded.zero_token_calls,
			last_seen_ms = max(usage_hourly_rollups.last_seen_ms, excluded.last_seen_ms),
			billable_prompt_tokens = usage_hourly_rollups.billable_prompt_tokens + excluded.billable_prompt_tokens,
			billable_cache_tokens = usage_hourly_rollups.billable_cache_tokens + excluded.billable_cache_tokens,
			billable_completion_tokens = usage_hourly_rollups.billable_completion_tokens + excluded.billable_completion_tokens`,
			aggregate.BucketMS, aggregate.Model, aggregate.Requests, aggregate.Successes, aggregate.Failures,
			aggregate.InputTokens, aggregate.OutputTokens, aggregate.ReasoningTokens, aggregate.CachedTokens,
			aggregate.CacheTokens, aggregate.TotalTokens, aggregate.BillablePromptTokens, aggregate.BillableCacheTokens,
			aggregate.BillableCompletionTokens, aggregate.LatencySumMS, aggregate.LatencySamples, aggregate.ZeroTokenCalls,
			aggregate.LastSeenMS); err != nil {
			return s.rollupBatchError(tx, err)
		}
	}

	for _, aggregate := range dimensionAggregates {
		if _, err := tx.ExecContext(ctx, `insert into usage_daily_dimension_rollups(
			bucket_ms, dimension, dimension_key, model, requests, successes, failures,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, total_tokens,
			billable_prompt_tokens, billable_cache_tokens, billable_completion_tokens,
			latency_sum_ms, latency_samples, zero_token_calls, last_seen_ms
		) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			on conflict(bucket_ms, dimension, dimension_key, model) do update set
			requests = usage_daily_dimension_rollups.requests + excluded.requests,
			successes = usage_daily_dimension_rollups.successes + excluded.successes,
			failures = usage_daily_dimension_rollups.failures + excluded.failures,
			input_tokens = usage_daily_dimension_rollups.input_tokens + excluded.input_tokens,
			output_tokens = usage_daily_dimension_rollups.output_tokens + excluded.output_tokens,
			reasoning_tokens = usage_daily_dimension_rollups.reasoning_tokens + excluded.reasoning_tokens,
			cached_tokens = usage_daily_dimension_rollups.cached_tokens + excluded.cached_tokens,
			cache_tokens = usage_daily_dimension_rollups.cache_tokens + excluded.cache_tokens,
			total_tokens = usage_daily_dimension_rollups.total_tokens + excluded.total_tokens,
			latency_sum_ms = usage_daily_dimension_rollups.latency_sum_ms + excluded.latency_sum_ms,
			latency_samples = usage_daily_dimension_rollups.latency_samples + excluded.latency_samples,
			zero_token_calls = usage_daily_dimension_rollups.zero_token_calls + excluded.zero_token_calls,
			last_seen_ms = max(usage_daily_dimension_rollups.last_seen_ms, excluded.last_seen_ms),
			billable_prompt_tokens = usage_daily_dimension_rollups.billable_prompt_tokens + excluded.billable_prompt_tokens,
			billable_cache_tokens = usage_daily_dimension_rollups.billable_cache_tokens + excluded.billable_cache_tokens,
			billable_completion_tokens = usage_daily_dimension_rollups.billable_completion_tokens + excluded.billable_completion_tokens`,
			aggregate.BucketMS, aggregate.Dimension, aggregate.DimensionKey, aggregate.Model,
			aggregate.Metric.Requests, aggregate.Metric.Successes, aggregate.Metric.Failures, aggregate.Metric.InputTokens,
			aggregate.Metric.OutputTokens, aggregate.Metric.ReasoningTokens, aggregate.Metric.CachedTokens, aggregate.Metric.CacheTokens,
			aggregate.Metric.TotalTokens, aggregate.Metric.BillablePromptTokens, aggregate.Metric.BillableCacheTokens,
			aggregate.Metric.BillableCompletionTokens, aggregate.Metric.LatencySumMS, aggregate.Metric.LatencySamples,
			aggregate.Metric.ZeroTokenCalls, aggregate.Metric.LastSeenMS); err != nil {
			return s.rollupBatchError(tx, err)
		}
	}

	lastID := events[len(events)-1].id
	status := RollupStatusCatchingUp
	if lastID >= state.TargetEventID {
		status = RollupStatusReady
	}
	if _, err := tx.ExecContext(ctx, `update usage_rollup_state set checkpoint_id = ?, coverage_event_id = ?, status = ?, last_error = null, updated_at_ms = ? where id = 1`, lastID, lastID, status, time.Now().UnixMilli()); err != nil {
		return s.rollupBatchError(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, s.recordRollupFailure(err)
	}
	if err := s.clearPendingRollupFailure(); err != nil {
		return len(events), fmt.Errorf("clear successful rollup failure marker: %w", err)
	}
	return len(events), nil
}

func (s *Store) rollupBatchError(tx *sql.Tx, err error) (int, error) {
	_ = tx.Rollback()
	return 0, s.recordRollupFailure(err)
}

// loadRollupFailureMarker restores sidecar failures that were not persisted before startup.
func (s *Store) loadRollupFailureMarker() error {
	marker, err := s.readRollupFailureMarker()
	if err != nil {
		return err
	}
	if marker == nil {
		return nil
	}
	s.rollupFailureMu.Lock()
	s.pendingRollupFailure = marker
	s.rollupFailureMu.Unlock()
	return nil
}

func (s *Store) readRollupFailureMarker() (*rollupFailureMarker, error) {
	if s.rollupFailurePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(s.rollupFailurePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker rollupFailureMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil, fmt.Errorf("decode rollup failure marker: %w", err)
	}
	if marker.Version != rollupFailureMarkerVersion || strings.TrimSpace(marker.Error) == "" {
		return nil, errors.New("invalid rollup failure marker")
	}
	return &marker, nil
}

// writeRollupFailureMarker writes through a same-directory temp file and rename so startup never reads partial JSON.
func (s *Store) writeRollupFailureMarker(marker *rollupFailureMarker) error {
	if s.rollupFailurePath == "" {
		return nil
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.rollupFailurePath), ".rollup-failure-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.rollupFailurePath)
}

// recordRollupFailure records the sidecar/in-memory pending state before a bounded persistence attempt; the worker retries lock conflicts.
func (s *Store) recordRollupFailure(err error) error {
	if err == nil {
		return nil
	}
	coverage := int64(0)
	coverageCtx, cancel := context.WithTimeout(context.Background(), time.Duration(rollupBusyTimeoutMS)*time.Millisecond)
	_ = s.db.QueryRowContext(coverageCtx, `select coalesce(coverage_event_id, checkpoint_id, 0) from usage_rollup_state where id = 1`).Scan(&coverage)
	cancel()
	marker := &rollupFailureMarker{
		Version: rollupFailureMarkerVersion, Error: err.Error(), OccurredAtMS: time.Now().UnixMilli(), CoverageEventID: coverage,
	}
	s.rollupFailureMu.Lock()
	s.pendingRollupFailure = marker
	s.rollupFailureMu.Unlock()
	markerErr := s.writeRollupFailureMarker(marker)
	if _, persistErr := s.RetryPendingRollupFailure(context.Background()); persistErr != nil {
		if markerErr != nil {
			return fmt.Errorf("rollup batch failed: %w; marker persistence pending: %v; failed-state persistence pending: %v", err, markerErr, persistErr)
		}
		return fmt.Errorf("rollup batch failed: %w; failed-state persistence pending: %v", err, persistErr)
	}
	if markerErr != nil {
		return fmt.Errorf("rollup batch failed: %w; marker persistence failed: %v", err, markerErr)
	}
	return err
}

// RetryPendingRollupFailure persists a previous failure state that could not be written because SQLite was locked.
// It uses a short timeout and keeps the pending error until the database confirms success.
func (s *Store) RetryPendingRollupFailure(ctx context.Context) (bool, error) {
	marker, err := s.pendingRollupMarker()
	if err != nil {
		return true, err
	}
	if marker == nil {
		return false, nil
	}
	retryCtx, cancel := context.WithTimeout(ctx, time.Duration(rollupBusyTimeoutMS)*time.Millisecond)
	defer cancel()
	var coverage int64
	if err := s.db.QueryRowContext(retryCtx, `select coalesce(coverage_event_id, checkpoint_id, 0) from usage_rollup_state where id = 1`).Scan(&coverage); err != nil {
		return true, fmt.Errorf("load rollup coverage for pending failure: %w", err)
	}
	if coverage > marker.CoverageEventID {
		if err := s.clearPendingRollupFailure(); err != nil {
			return true, fmt.Errorf("clear stale rollup failure marker: %w", err)
		}
		return false, nil
	}
	if _, err := s.db.ExecContext(retryCtx, `update usage_rollup_state set status = ?, last_error = ?, updated_at_ms = ? where id = 1`, RollupStatusFailed, marker.Error, time.Now().UnixMilli()); err != nil {
		return true, fmt.Errorf("persist pending rollup failure: %w", err)
	}
	if err := s.clearPendingRollupFailure(); err != nil {
		return true, fmt.Errorf("clear persisted rollup failure marker: %w", err)
	}
	return true, nil
}

func (s *Store) pendingRollupMarker() (*rollupFailureMarker, error) {
	s.rollupFailureMu.Lock()
	marker := s.pendingRollupFailure
	s.rollupFailureMu.Unlock()
	if marker != nil {
		copy := *marker
		return &copy, nil
	}
	marker, err := s.readRollupFailureMarker()
	if err != nil || marker == nil {
		return marker, err
	}
	s.rollupFailureMu.Lock()
	if s.pendingRollupFailure == nil {
		s.pendingRollupFailure = marker
	}
	s.rollupFailureMu.Unlock()
	return marker, nil
}

// HasPendingRollupFailure exposes whether a failed state still awaits persistence.
func (s *Store) HasPendingRollupFailure() bool {
	marker, err := s.pendingRollupMarker()
	return err == nil && marker != nil
}

func (s *Store) clearPendingRollupFailure() error {
	if s.rollupFailurePath != "" {
		if err := os.Remove(s.rollupFailurePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	s.rollupFailureMu.Lock()
	s.pendingRollupFailure = nil
	s.rollupFailureMu.Unlock()
	return nil
}

func (s *Store) restoreSQLiteBusyTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`pragma busy_timeout = %d`, defaultSQLiteBusyTimeout))
}

// LatestUsageEventID returns the current raw event high-water mark without exposing event payloads.
func (s *Store) LatestUsageEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `select coalesce(max(id), 0) from usage_events`).Scan(&id)
	return id, err
}

// LoadRollupState returns the persisted checkpoint state used by the worker.
func (s *Store) LoadRollupState(ctx context.Context) (RollupState, error) {
	var state RollupState
	if err := s.db.QueryRowContext(ctx, `select checkpoint_id, coverage_event_id, target_event_id, status, coalesce(last_error, ''), updated_at_ms from usage_rollup_state where id = 1`).Scan(
		&state.CheckpointID, &state.CoverageEventID, &state.TargetEventID, &state.Status, &state.LastError, &state.UpdatedAtMS); err != nil {
		return state, err
	}
	marker, err := s.pendingRollupMarker()
	if err != nil {
		return state, err
	}
	if marker == nil {
		return state, nil
	}
	// If the process crashed after a successful commit but before deleting the marker,
	// coverage has already advanced and the stale marker can be discarded safely.
	if state.CoverageEventID > marker.CoverageEventID {
		if err := s.clearPendingRollupFailure(); err != nil {
			return state, err
		}
		return state, nil
	}
	state.Status = RollupStatusFailed
	state.LastError = marker.Error
	return state, nil
}

// LoadHourlyRollups loads complete hourly rows. A zero bound means unbounded.
func (s *Store) LoadHourlyRollups(ctx context.Context, fromMS, toMS int64) ([]HourlyRollup, error) {
	query := `select bucket_ms, model, requests, successes, failures, input_tokens, output_tokens,
		reasoning_tokens, cached_tokens, cache_tokens, total_tokens, billable_prompt_tokens,
		billable_cache_tokens, billable_completion_tokens, latency_sum_ms, latency_samples, zero_token_calls, last_seen_ms
		from usage_hourly_rollups`
	args := make([]any, 0, 2)
	conditions := make([]string, 0, 2)
	if fromMS != 0 {
		conditions = append(conditions, "bucket_ms >= ?")
		args = append(args, fromMS)
	}
	if toMS != 0 {
		conditions = append(conditions, "bucket_ms < ?")
		args = append(args, toMS)
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by bucket_ms asc, model asc"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]HourlyRollup, 0)
	for rows.Next() {
		var row HourlyRollup
		if err := rows.Scan(&row.BucketMS, &row.Model, &row.Requests, &row.Successes, &row.Failures,
			&row.InputTokens, &row.OutputTokens, &row.ReasoningTokens, &row.CachedTokens, &row.CacheTokens,
			&row.TotalTokens, &row.BillablePromptTokens, &row.BillableCacheTokens, &row.BillableCompletionTokens,
			&row.LatencySumMS, &row.LatencySamples, &row.ZeroTokenCalls, &row.LastSeenMS); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
