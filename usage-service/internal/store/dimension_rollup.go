package store

import (
	"context"
	"strings"
)

const dayMilliseconds = int64(24 * 60 * 60 * 1000)

// LoadDailyDimensionRollups reads complete UTC-day rows produced by the same
// event-id checkpoint as the hourly rollup. A zero bound is unbounded.
func (s *Store) LoadDailyDimensionRollups(ctx context.Context, fromMS, toMS int64, dimension string) ([]DailyDimensionRollup, error) {
	dimension = strings.ToLower(strings.TrimSpace(dimension))
	if dimension != "account" && dimension != "api_key" && dimension != "provider" && dimension != "auth_index" {
		return nil, &UnsupportedDimensionError{Dimension: dimension}
	}
	query := `select bucket_ms, dimension, dimension_key, model, requests, successes, failures,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, total_tokens,
		latency_sum_ms, latency_samples, zero_token_calls
		from usage_daily_dimension_rollups where dimension = ?`
	args := []any{dimension}
	if fromMS != 0 {
		query += " and bucket_ms >= ?"
		args = append(args, fromMS)
	}
	if toMS != 0 {
		query += " and bucket_ms < ?"
		args = append(args, toMS)
	}
	query += " order by bucket_ms asc, dimension_key asc, model asc"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]DailyDimensionRollup, 0)
	for rows.Next() {
		var row DailyDimensionRollup
		if err := rows.Scan(
			&row.BucketMS, &row.Dimension, &row.DimensionKey, &row.Model,
			&row.Metric.Requests, &row.Metric.Successes, &row.Metric.Failures,
			&row.Metric.InputTokens, &row.Metric.OutputTokens, &row.Metric.ReasoningTokens,
			&row.Metric.CachedTokens, &row.Metric.CacheTokens, &row.Metric.TotalTokens,
			&row.Metric.LatencySumMS, &row.Metric.LatencySamples, &row.Metric.ZeroTokenCalls,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// UnsupportedDimensionError is returned before any interpolated SQL is built.
type UnsupportedDimensionError struct{ Dimension string }

func (e *UnsupportedDimensionError) Error() string { return "unsupported dimension " + e.Dimension }
