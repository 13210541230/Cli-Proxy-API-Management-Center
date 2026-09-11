package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

const maxUsageEventsPageSize = 500

// UsageAggregateFilter is the bounded predicate shared by analytics reads.
// FromMS/ToMS always use the half-open interval [FromMS, ToMS).
type UsageAggregateFilter struct {
	FromMS          int64
	ToMS            int64
	MinEventID      int64
	APIKeyHash      string
	Model           string
	Provider        string
	AccountSnapshot string
	AuthIndex       string
	Endpoint        string
	ReasoningEffort string
}

// UsageMetric is an aggregate over usage_events. It intentionally contains no
// raw payload or secret material.
type UsageMetric struct {
	Requests                 int64
	Successes                int64
	Failures                 int64
	InputTokens              int64
	OutputTokens             int64
	ReasoningTokens          int64
	CachedTokens             int64
	CacheTokens              int64
	TotalTokens              int64
	LatencySumMS             int64
	LatencySamples           int64
	ZeroTokenCalls           int64
	BillablePromptTokens     int64
	BillableCacheTokens      int64
	BillableCompletionTokens int64
	CostUSD                  float64
	LastSeenMS               int64
}

type UsageAggregateRow struct {
	BucketMS int64
	Model    string
	Metric   UsageMetric
}

type UsageDimensionRow struct {
	Dimension string
	Metric    UsageMetric
}

type UsageDimensionTimelineRow struct {
	BucketMS  int64
	Dimension string
	Metric    UsageMetric
}

// AggregateUsageEvents groups raw events by hour and model. It is used for
// boundary ranges, raw tails, and unsupported filters; it never materializes
// event details in Go.
func (s *Store) AggregateUsageEvents(ctx context.Context, filter UsageAggregateFilter) ([]UsageAggregateRow, error) {
	where, args := usageWhere(filter, true)
	query := `select
		((ue.timestamp_ms / ?) * ?) as bucket_ms,
		ue.model,
		count(*),
		sum(case when ue.failed = 0 then 1 else 0 end),
		sum(case when ue.failed <> 0 then 1 else 0 end),
		sum(ue.input_tokens), sum(ue.output_tokens), sum(ue.reasoning_tokens),
		sum(ue.cached_tokens), sum(ue.cache_tokens), sum(ue.total_tokens),
		sum(max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, ue.output_tokens)),
		sum(case when ue.latency_ms is not null then ue.latency_ms else 0 end),
		sum(case when ue.latency_ms is not null then 1 else 0 end),
		sum(case when ue.input_tokens = 0 and ue.output_tokens = 0 and ue.reasoning_tokens = 0
			and ue.cached_tokens = 0 and ue.cache_tokens = 0 and ue.total_tokens = 0 then 1 else 0 end),
		max(ue.timestamp_ms),
		sum((
			max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.prompt_per_1m, 0)
			+ max(0, max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.cache_per_1m, 0)
			+ max(0, ue.output_tokens) * coalesce(mp.completion_per_1m, 0)
		) / 1000000.0)
	from usage_events ue
	left join model_prices mp on mp.model = ue.model
	where ` + where + `
	group by bucket_ms, ue.model
	order by bucket_ms asc, ue.model asc`
	args = append([]any{hourMilliseconds, hourMilliseconds}, args...)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]UsageAggregateRow, 0)
	for rows.Next() {
		var row UsageAggregateRow
		if err := scanUsageMetricRow(rows, &row.BucketMS, &row.Model, &row.Metric); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// AggregateUsageDimension groups raw events by one safe, fixed dimension. The
// dimension name is validated before it is interpolated into SQL.
func (s *Store) AggregateUsageDimension(ctx context.Context, filter UsageAggregateFilter, dimension string) ([]UsageDimensionRow, error) {
	groupColumn := usageDimensionColumn(dimension)
	if groupColumn == "" {
		return nil, fmt.Errorf("unsupported usage dimension %q", dimension)
	}
	where, args := usageWhere(filter, true)
	groupExpression := "coalesce(" + groupColumn + ", '')"
	query := `select ` + groupExpression + `,
		count(*),
		sum(case when ue.failed = 0 then 1 else 0 end),
		sum(case when ue.failed <> 0 then 1 else 0 end),
		sum(ue.input_tokens), sum(ue.output_tokens), sum(ue.reasoning_tokens),
		sum(ue.cached_tokens), sum(ue.cache_tokens), sum(ue.total_tokens),
		sum(max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, ue.output_tokens)),
		sum(case when ue.latency_ms is not null then ue.latency_ms else 0 end),
		sum(case when ue.latency_ms is not null then 1 else 0 end),
		sum(case when ue.input_tokens = 0 and ue.output_tokens = 0 and ue.reasoning_tokens = 0
			and ue.cached_tokens = 0 and ue.cache_tokens = 0 and ue.total_tokens = 0 then 1 else 0 end),
		max(ue.timestamp_ms),
		sum((
			max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.prompt_per_1m, 0)
			+ max(0, max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.cache_per_1m, 0)
			+ max(0, ue.output_tokens) * coalesce(mp.completion_per_1m, 0)
		) / 1000000.0)
	from usage_events ue
	left join model_prices mp on mp.model = ue.model
	where ` + where + `
	group by ` + groupExpression + `
	order by count(*) desc, ` + groupExpression + ` asc`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]UsageDimensionRow, 0)
	for rows.Next() {
		var row UsageDimensionRow
		if err := scanUsageMetricRow(rows, nil, &row.Dimension, &row.Metric); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// AggregateUsageDimensionTimeline groups raw events by UTC day and one safe
// dimension. It is used for long-range per-key/account trend fallback and
// never materializes event details in Go.
func (s *Store) AggregateUsageDimensionTimeline(ctx context.Context, filter UsageAggregateFilter, dimension string) ([]UsageDimensionTimelineRow, error) {
	groupColumn := usageDimensionColumn(dimension)
	if groupColumn == "" {
		return nil, fmt.Errorf("unsupported usage dimension %q", dimension)
	}
	where, args := usageWhere(filter, true)
	groupExpression := "coalesce(" + groupColumn + ", '')"
	query := `select
		((ue.timestamp_ms / ?) * ?) as bucket_ms,
		` + groupExpression + `,
		count(*),
		sum(case when ue.failed = 0 then 1 else 0 end),
		sum(case when ue.failed <> 0 then 1 else 0 end),
		sum(ue.input_tokens), sum(ue.output_tokens), sum(ue.reasoning_tokens),
		sum(ue.cached_tokens), sum(ue.cache_tokens), sum(ue.total_tokens),
		sum(max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, max(ue.cached_tokens, ue.cache_tokens))),
		sum(max(0, ue.output_tokens)),
		sum(case when ue.latency_ms is not null then ue.latency_ms else 0 end),
		sum(case when ue.latency_ms is not null then 1 else 0 end),
		sum(case when ue.input_tokens = 0 and ue.output_tokens = 0 and ue.reasoning_tokens = 0
			and ue.cached_tokens = 0 and ue.cache_tokens = 0 and ue.total_tokens = 0 then 1 else 0 end),
		max(ue.timestamp_ms),
		sum((
			max(0, ue.input_tokens - max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.prompt_per_1m, 0)
			+ max(0, max(ue.cached_tokens, ue.cache_tokens)) * coalesce(mp.cache_per_1m, 0)
			+ max(0, ue.output_tokens) * coalesce(mp.completion_per_1m, 0)
		) / 1000000.0)
	from usage_events ue
	left join model_prices mp on mp.model = ue.model
	where ` + where + `
	group by bucket_ms, ` + groupExpression + `
	order by bucket_ms asc, ` + groupExpression + ` asc`
	args = append([]any{dayMilliseconds, dayMilliseconds}, args...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]UsageDimensionTimelineRow, 0)
	for rows.Next() {
		var row UsageDimensionTimelineRow
		if err := scanUsageMetricRow(rows, &row.BucketMS, &row.Dimension, &row.Metric); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func usageDimensionColumn(dimension string) string {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "model":
		return "ue.model"
	case "api_key", "api_key_hash":
		return "ue.api_key_hash"
	case "account", "account_snapshot":
		return "ue.account_snapshot"
	case "provider":
		return "ue.provider"
	case "auth_index":
		return "ue.auth_index"
	case "endpoint":
		return "ue.endpoint"
	case "reasoning_effort", "reasoning":
		return "ue.reasoning_effort"
	default:
		return ""
	}
}

// CountSecuritySignals returns the exact number of cyber-policy events in the
// same bounded scope used by analytics aggregates. It deliberately bypasses
// event-page cursors and rollup coverage because the count covers the full range.
func (s *Store) CountSecuritySignals(ctx context.Context, filter UsageAggregateFilter) (int64, error) {
	where, args := usageWhere(filter, false)
	where += " and ue.security_signal = ?"
	args = append(args, usage.SecuritySignalCyberPolicy)
	var count int64
	if err := s.db.QueryRowContext(ctx, "select count(*) from usage_events ue where "+where, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func usageWhere(filter UsageAggregateFilter, includeMinID bool) (string, []any) {
	where := []string{"ue.timestamp_ms >= ?", "ue.timestamp_ms < ?"}
	args := []any{filter.FromMS, filter.ToMS}
	if includeMinID && filter.MinEventID > 0 {
		where = append(where, "ue.id > ?")
		args = append(args, filter.MinEventID)
	}
	appendText := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		where = append(where, "lower("+column+") = lower(?)")
		args = append(args, strings.TrimSpace(value))
	}
	appendText("ue.api_key_hash", filter.APIKeyHash)
	appendText("ue.model", filter.Model)
	appendText("ue.provider", filter.Provider)
	appendText("ue.account_snapshot", filter.AccountSnapshot)
	appendText("ue.auth_index", filter.AuthIndex)
	appendText("ue.endpoint", filter.Endpoint)
	if strings.EqualFold(strings.TrimSpace(filter.ReasoningEffort), "unknown") {
		where = append(where, "(ue.reasoning_effort is null or trim(ue.reasoning_effort) = '')")
	} else {
		appendText("ue.reasoning_effort", filter.ReasoningEffort)
	}
	return strings.Join(where, " and "), args
}

func scanUsageMetricRow(rows *sql.Rows, bucket *int64, dimension *string, metric *UsageMetric) error {
	values := []any{
		&metric.Requests,
		&metric.Successes,
		&metric.Failures,
		&metric.InputTokens,
		&metric.OutputTokens,
		&metric.ReasoningTokens,
		&metric.CachedTokens,
		&metric.CacheTokens,
		&metric.TotalTokens,
		&metric.BillablePromptTokens,
		&metric.BillableCacheTokens,
		&metric.BillableCompletionTokens,
		&metric.LatencySumMS,
		&metric.LatencySamples,
		&metric.ZeroTokenCalls,
		&metric.LastSeenMS,
		&metric.CostUSD,
	}
	if bucket != nil && dimension != nil {
		return rows.Scan(append([]any{bucket, dimension}, values...)...)
	}
	if dimension != nil {
		return rows.Scan(append([]any{dimension}, values...)...)
	}
	return rows.Scan(values...)
}

// UsageEventPageQuery describes one stable keyset page. Cursor means rows
// strictly older than (CursorTimestampMS, CursorID).
type UsageEventPageQuery struct {
	UsageAggregateFilter
	CursorTimestampMS *int64
	CursorID          *int64
	Limit             int
	IncludeTotalCount bool
}

type UsageEventPageItem struct {
	ID                   int64
	RequestID            string
	EventHash            string
	TimestampMS          int64
	Timestamp            string
	Provider             string
	Model                string
	Endpoint             string
	Method               string
	Path                 string
	AuthType             string
	AuthIndex            string
	Source               string
	SourceHash           string
	APIKeyHash           string
	AccountSnapshot      string
	AuthLabelSnapshot    string
	AuthFileSnapshot     string
	AuthProviderSnapshot string
	AuthSnapshotAtMS     int64
	ReasoningEffort      string
	TTFTMS               *int64
	ServiceTier          string
	RequestServiceTier   string
	ResponseServiceTier  string
	ExecutorType         string
	FailStatusCode       *int64
	FailSummary          string
	SecuritySignal       string
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CachedTokens         int64
	CacheTokens          int64
	TotalTokens          int64
	LatencyMS            *int64
	Failed               bool
	CreatedAtMS          int64
}

type UsageEventPage struct {
	Items      []UsageEventPageItem
	TotalCount int64
	HasMore    bool
}

func (s *Store) PageUsageEvents(ctx context.Context, query UsageEventPageQuery) (UsageEventPage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > maxUsageEventsPageSize {
		limit = maxUsageEventsPageSize
	}
	where, args := usageWhere(query.UsageAggregateFilter, false)
	if query.CursorTimestampMS != nil && query.CursorID != nil {
		where += " and (ue.timestamp_ms < ? or (ue.timestamp_ms = ? and ue.id < ?))"
		args = append(args, *query.CursorTimestampMS, *query.CursorTimestampMS, *query.CursorID)
	}
	countWhere, countArgs := usageWhere(query.UsageAggregateFilter, false)
	var total int64
	if query.IncludeTotalCount {
		if err := s.db.QueryRowContext(ctx, "select count(*) from usage_events ue where "+countWhere, countArgs...).Scan(&total); err != nil {
			return UsageEventPage{}, err
		}
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `select
		ue.id, ue.request_id, ue.event_hash, ue.timestamp_ms, ue.timestamp, ue.provider, ue.model,
		ue.reasoning_effort, ue.ttft_ms, ue.service_tier, ue.request_service_tier, ue.response_service_tier,
		ue.executor_type, ue.fail_status_code, ue.fail_summary, ue.security_signal, ue.endpoint, ue.method, ue.path,
		ue.auth_type, ue.auth_index, ue.source, ue.source_hash,
		ue.api_key_hash, ue.account_snapshot, ue.auth_label_snapshot, ue.auth_file_snapshot,
		ue.auth_provider_snapshot, ue.auth_snapshot_at_ms, ue.input_tokens, ue.output_tokens,
		ue.reasoning_tokens, ue.cached_tokens, ue.cache_tokens, ue.total_tokens, ue.latency_ms,
		ue.failed, ue.created_at_ms
	from usage_events ue where `+where+` order by ue.timestamp_ms desc, ue.id desc limit ?`, args...)
	if err != nil {
		return UsageEventPage{}, err
	}
	defer rows.Close()
	items := make([]UsageEventPageItem, 0, limit)
	for rows.Next() {
		var item UsageEventPageItem
		var requestID, provider, reasoningEffort, serviceTier, requestServiceTier, responseServiceTier, executorType, failSummary, securitySignal, endpoint, method, path, authType, authIndex, source, sourceHash sql.NullString
		var apiKeyHash, accountSnapshot, authLabelSnapshot, authFileSnapshot, authProviderSnapshot sql.NullString
		var ttft, failStatusCode, authSnapshotAt, latency sql.NullInt64
		var failed int
		if err := rows.Scan(
			&item.ID, &requestID, &item.EventHash, &item.TimestampMS, &item.Timestamp, &provider, &item.Model,
			&reasoningEffort, &ttft, &serviceTier, &requestServiceTier, &responseServiceTier, &executorType,
			&failStatusCode, &failSummary, &securitySignal, &endpoint, &method, &path, &authType, &authIndex, &source, &sourceHash, &apiKeyHash,
			&accountSnapshot, &authLabelSnapshot, &authFileSnapshot, &authProviderSnapshot,
			&authSnapshotAt, &item.InputTokens, &item.OutputTokens, &item.ReasoningTokens,
			&item.CachedTokens, &item.CacheTokens, &item.TotalTokens, &latency, &failed, &item.CreatedAtMS,
		); err != nil {
			return UsageEventPage{}, err
		}
		item.RequestID = requestID.String
		item.Provider = provider.String
		item.Endpoint = endpoint.String
		item.Method = method.String
		item.Path = path.String
		item.AuthType = authType.String
		item.AuthIndex = authIndex.String
		item.Source = usage.MaskUsageSource(source.String)
		item.SourceHash = sourceHash.String
		item.APIKeyHash = apiKeyHash.String
		item.AccountSnapshot = accountSnapshot.String
		item.AuthLabelSnapshot = authLabelSnapshot.String
		item.AuthFileSnapshot = authFileSnapshot.String
		item.AuthProviderSnapshot = authProviderSnapshot.String
		item.ReasoningEffort = reasoningEffort.String
		item.ServiceTier = serviceTier.String
		item.RequestServiceTier = requestServiceTier.String
		item.ResponseServiceTier = responseServiceTier.String
		item.ExecutorType = executorType.String
		item.FailSummary = failSummary.String
		item.SecuritySignal = securitySignal.String
		if ttft.Valid {
			value := ttft.Int64
			item.TTFTMS = &value
		}
		if failStatusCode.Valid && failStatusCode.Int64 > 0 {
			value := failStatusCode.Int64
			item.FailStatusCode = &value
		}
		if authSnapshotAt.Valid {
			item.AuthSnapshotAtMS = authSnapshotAt.Int64
		}
		item.Failed = failed != 0
		if latency.Valid {
			value := latency.Int64
			item.LatencyMS = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return UsageEventPage{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return UsageEventPage{Items: items, TotalCount: total, HasMore: hasMore}, nil
}

func (s *Store) LoadUsageFilterOptions(ctx context.Context, filter UsageAggregateFilter) (map[string][]string, error) {
	where, args := usageWhere(filter, false)
	options := map[string][]string{}
	queries := map[string]string{
		"models":            "select distinct coalesce(ue.model, '') from usage_events ue where " + where + " order by 1",
		"providers":         "select distinct coalesce(ue.provider, '') from usage_events ue where " + where + " order by 1",
		"api_key_hashes":    "select distinct coalesce(ue.api_key_hash, '') from usage_events ue where " + where + " order by 1",
		"accounts":          "select distinct coalesce(ue.account_snapshot, '') from usage_events ue where " + where + " order by 1",
		"endpoints":         "select distinct coalesce(ue.endpoint, '') from usage_events ue where " + where + " order by 1",
		"reasoning_efforts": "select distinct coalesce(ue.reasoning_effort, '') from usage_events ue where " + where + " order by 1",
	}
	for name, query := range queries {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0)
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return nil, err
			}
			if strings.TrimSpace(value) != "" {
				values = append(values, value)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		options[name] = values
	}
	return options, nil
}
