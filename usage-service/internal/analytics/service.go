// Package analytics provides the additive, bounded monitoring analytics API.
package analytics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

const (
	analyticsVersion     = "v1"
	analyticsPageDefault = 50
	analyticsPageMax     = 200
	hourMS               = int64(60 * 60 * 1000)
)

var allowedIncludes = map[string]struct{}{
	"summary":          {},
	"timeline":         {},
	"model_stats":      {},
	"account_stats":    {},
	"api_key_stats":    {},
	"api_key_timeline": {},
	"events":           {},
	"filter_options":   {},
}

// IncludeList accepts both the additive string-array form and the Plus-style
// object whose true-valued keys request sections.
type IncludeList []string

func (i *IncludeList) UnmarshalJSON(data []byte) error {
	values, _, err := parseInclude(data)
	if err != nil {
		return err
	}
	*i = values
	return nil
}

func parseInclude(data []byte) (IncludeList, *EventsPageRequest, error) {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		return dedupeIncludes(values), nil, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, nil, errors.New("include must be an array or object")
	}
	values = make([]string, 0, len(object))
	var eventsPage *EventsPageRequest
	for name, raw := range object {
		if name == "events_page" {
			var page EventsPageRequest
			if err := json.Unmarshal(raw, &page); err != nil {
				return nil, nil, errors.New("include.events_page must be an object")
			}
			eventsPage = &page
			values = append(values, "events")
			continue
		}
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, nil, fmt.Errorf("include.%s must be a boolean", name)
		}
		if enabled {
			values = append(values, name)
		}
	}
	return dedupeIncludes(values), eventsPage, nil
}

func dedupeIncludes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.ToLower(strings.TrimSpace(value))
		if name == "events_page" {
			name = "events"
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

type Filters struct {
	APIKeyHash      string `json:"api_key_hash,omitempty"`
	Model           string `json:"model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	AccountSnapshot string `json:"account_snapshot,omitempty"`
	AuthIndex       string `json:"auth_index,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
}

type EventsPageRequest struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeMS *int64 `json:"before_ms,omitempty"`
	BeforeID *int64 `json:"before_id,omitempty"`
}

type Request struct {
	FromMS     int64              `json:"from_ms"`
	ToMS       int64              `json:"to_ms"`
	Include    IncludeList        `json:"include"`
	Filters    Filters            `json:"filters,omitempty"`
	Cursor     string             `json:"cursor,omitempty"`
	Limit      int                `json:"limit,omitempty"`
	EventsPage *EventsPageRequest `json:"events_page,omitempty"`
}

func (r *Request) UnmarshalJSON(data []byte) error {
	var wire struct {
		FromMS     int64              `json:"from_ms"`
		ToMS       int64              `json:"to_ms"`
		Include    json.RawMessage    `json:"include"`
		Filters    Filters            `json:"filters"`
		Cursor     string             `json:"cursor"`
		Limit      int                `json:"limit"`
		EventsPage *EventsPageRequest `json:"events_page"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = Request{
		FromMS: wire.FromMS, ToMS: wire.ToMS, Filters: wire.Filters,
		Cursor: wire.Cursor, Limit: wire.Limit, EventsPage: wire.EventsPage,
	}
	if len(wire.Include) == 0 || string(wire.Include) == "null" {
		return nil
	}
	include, eventsPage, err := parseInclude(wire.Include)
	if err != nil {
		return err
	}
	r.Include = include
	if eventsPage != nil {
		r.EventsPage = eventsPage
	}
	return nil
}

type Meta struct {
	Version         string `json:"version"`
	Complete        bool   `json:"complete"`
	Source          string `json:"source"`
	RollupStatus    string `json:"rollup_status"`
	CoverageEventID int64  `json:"coverage_event_id"`
	TargetEventID   int64  `json:"target_event_id"`
}

type Metric struct {
	Requests        int64   `json:"requests"`
	Successes       int64   `json:"successes"`
	Failures        int64   `json:"failures"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
	CachedTokens    int64   `json:"cached_tokens"`
	CacheTokens     int64   `json:"cache_tokens"`
	TotalTokens     int64   `json:"total_tokens"`
	LatencySumMS    int64   `json:"latency_sum_ms"`
	LatencySamples  int64   `json:"latency_samples"`
	ZeroTokenCalls  int64   `json:"zero_token_calls"`
	CostUSD         float64 `json:"cost_usd"`
}

type TimelineItem struct {
	BucketMS int64 `json:"bucket_ms"`
	Metric
}

type ModelStat struct {
	Model string `json:"model"`
	Metric
}

type DimensionStat struct {
	Key string `json:"key"`
	Metric
}

type DimensionTimeline struct {
	Key      string         `json:"key"`
	Timeline []TimelineItem `json:"timeline"`
}

type EventItem struct {
	ID                   int64  `json:"id"`
	RequestID            string `json:"request_id,omitempty"`
	EventHash            string `json:"event_hash"`
	TimestampMS          int64  `json:"timestamp_ms"`
	Timestamp            string `json:"timestamp"`
	Provider             string `json:"provider,omitempty"`
	Model                string `json:"model"`
	Endpoint             string `json:"endpoint,omitempty"`
	Method               string `json:"method,omitempty"`
	Path                 string `json:"path,omitempty"`
	AuthType             string `json:"auth_type,omitempty"`
	AuthIndex            string `json:"auth_index,omitempty"`
	Source               string `json:"source,omitempty"`
	SourceHash           string `json:"source_hash,omitempty"`
	APIKeyHash           string `json:"api_key_hash,omitempty"`
	AccountSnapshot      string `json:"account_snapshot,omitempty"`
	AuthLabelSnapshot    string `json:"auth_label_snapshot,omitempty"`
	AuthFileSnapshot     string `json:"auth_file_snapshot,omitempty"`
	AuthProviderSnapshot string `json:"auth_provider_snapshot,omitempty"`
	AuthSnapshotAtMS     int64  `json:"auth_snapshot_at_ms,omitempty"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	ReasoningTokens      int64  `json:"reasoning_tokens"`
	CachedTokens         int64  `json:"cached_tokens"`
	CacheTokens          int64  `json:"cache_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	LatencyMS            *int64 `json:"latency_ms,omitempty"`
	Failed               bool   `json:"failed"`
	CreatedAtMS          int64  `json:"created_at_ms"`
}

type EventsPage struct {
	Items        []EventItem `json:"items"`
	TotalCount   int64       `json:"total_count"`
	HasMore      bool        `json:"has_more"`
	NextCursor   string      `json:"next_cursor,omitempty"`
	NextBeforeMS int64       `json:"next_before_ms"`
	NextBeforeID int64       `json:"next_before_id"`
}

type Response struct {
	Meta           Meta                `json:"meta"`
	Summary        *Metric             `json:"summary,omitempty"`
	Timeline       []TimelineItem      `json:"timeline,omitempty"`
	ModelStats     []ModelStat         `json:"model_stats,omitempty"`
	AccountStats   []DimensionStat     `json:"account_stats,omitempty"`
	APIKeyStats    []DimensionStat     `json:"api_key_stats,omitempty"`
	APIKeyTimeline []DimensionTimeline `json:"api_key_timeline,omitempty"`
	Events         *EventsPage         `json:"events,omitempty"`
	FilterOptions  map[string][]string `json:"filter_options,omitempty"`
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func ValidateRequest(req Request) error {
	if req.FromMS < 0 || req.ToMS < 0 || req.ToMS <= req.FromMS {
		return &ValidationError{Message: "from_ms and to_ms must define a non-empty range [from_ms,to_ms)"}
	}
	if len(req.Include) == 0 {
		req.Include = IncludeList{"summary"}
	}
	for _, name := range req.Include {
		if _, ok := allowedIncludes[name]; !ok {
			return &ValidationError{Message: fmt.Sprintf("unknown include section %q", name)}
		}
	}
	pageLimit := req.Limit
	if req.EventsPage != nil {
		if req.EventsPage.Limit < 0 || req.EventsPage.Limit > analyticsPageMax {
			return &ValidationError{Message: fmt.Sprintf("events_page.limit must be between 0 and %d", analyticsPageMax)}
		}
		if pageLimit == 0 {
			pageLimit = req.EventsPage.Limit
		}
		if (req.EventsPage.BeforeMS == nil) != (req.EventsPage.BeforeID == nil) {
			return &ValidationError{Message: "events_page.before_ms and before_id must be provided together"}
		}
		if req.EventsPage.BeforeMS != nil && (*req.EventsPage.BeforeMS < 0 || *req.EventsPage.BeforeID < 0) {
			return &ValidationError{Message: "events_page cursor values must be non-negative"}
		}
	}
	if pageLimit < 0 || pageLimit > analyticsPageMax {
		return &ValidationError{Message: fmt.Sprintf("limit must be between 0 and %d", analyticsPageMax)}
	}
	if strings.TrimSpace(req.Cursor) != "" {
		if _, err := decodeCursor(req.Cursor); err != nil {
			return &ValidationError{Message: "cursor is invalid"}
		}
	}
	return nil
}

func includes(req Request, name string) bool {
	for _, item := range req.Include {
		if item == name {
			return true
		}
	}
	return false
}

func toStoreFilter(req Request) store.UsageAggregateFilter {
	return store.UsageAggregateFilter{
		FromMS:          req.FromMS,
		ToMS:            req.ToMS,
		APIKeyHash:      req.Filters.APIKeyHash,
		Model:           req.Filters.Model,
		Provider:        req.Filters.Provider,
		AccountSnapshot: req.Filters.AccountSnapshot,
		AuthIndex:       req.Filters.AuthIndex,
		Endpoint:        req.Filters.Endpoint,
	}
}

// Query builds small aggregate sections and keeps event details on an
// independent keyset-paginated path. Raw tail rows are disjoint from the
// rollup's id coverage, including late timestamp events.
func Query(ctx context.Context, st *store.Store, req Request) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
	}
	if len(req.Include) == 0 {
		req.Include = IncludeList{"summary"}
	}
	state, stateErr := st.LoadRollupState(ctx)
	latestID, latestErr := st.LatestUsageEventID(ctx)
	useRollup := stateErr == nil && latestErr == nil && state.CoverageEventID >= 0 && state.CoverageEventID <= latestID
	if stateErr != nil || latestErr != nil {
		state.Status = "unavailable"
		state.CoverageEventID = 0
		state.TargetEventID = 0
		useRollup = false
	}

	response := Response{Meta: Meta{
		Version:         analyticsVersion,
		Complete:        true,
		Source:          "raw",
		RollupStatus:    state.Status,
		CoverageEventID: state.CoverageEventID,
		TargetEventID:   state.TargetEventID,
	}}
	filter := toStoreFilter(req)

	needCore := includes(req, "summary") || includes(req, "timeline") || includes(req, "model_stats")
	if needCore {
		core, source, err := queryCore(ctx, st, filter, state.CoverageEventID, useRollup)
		if err != nil {
			return Response{}, err
		}
		response.Meta.Source = source
		if includes(req, "summary") {
			metric := metricFromAggregate(sumAggregate(core))
			response.Summary = &metric
		}
		if includes(req, "timeline") {
			response.Timeline = buildTimeline(core)
		}
		if includes(req, "model_stats") {
			response.ModelStats = buildModelStats(core)
		}
	}

	if includes(req, "account_stats") {
		rows, source, err := queryDimension(ctx, st, filter, "account", state.CoverageEventID, useRollup)
		if err != nil {
			return Response{}, err
		}
		response.AccountStats = dimensionStats(rows)
		response.Meta.Source = combineSource(response.Meta.Source, source)
	}
	if includes(req, "api_key_stats") {
		rows, source, err := queryDimension(ctx, st, filter, "api_key", state.CoverageEventID, useRollup)
		if err != nil {
			return Response{}, err
		}
		response.APIKeyStats = dimensionStats(rows)
		response.Meta.Source = combineSource(response.Meta.Source, source)
	}
	if includes(req, "api_key_timeline") {
		timeline, source, err := queryDimensionTimeline(ctx, st, filter, "api_key", state.CoverageEventID, useRollup)
		if err != nil {
			return Response{}, err
		}
		response.APIKeyTimeline = timeline
		response.Meta.Source = combineSource(response.Meta.Source, source)
	}
	if includes(req, "filter_options") {
		options, err := st.LoadUsageFilterOptions(ctx, filter)
		if err != nil {
			return Response{}, err
		}
		response.FilterOptions = options
		response.Meta.Source = combineSource(response.Meta.Source, "raw")
	}
	if includes(req, "events") {
		pageLimit := req.Limit
		if req.EventsPage != nil && pageLimit == 0 {
			pageLimit = req.EventsPage.Limit
		}
		pageQuery := store.UsageEventPageQuery{UsageAggregateFilter: filter, Limit: pageLimit}
		if pageQuery.Limit == 0 {
			pageQuery.Limit = analyticsPageDefault
		}
		if req.EventsPage != nil && req.EventsPage.BeforeMS != nil {
			pageQuery.CursorTimestampMS = req.EventsPage.BeforeMS
			pageQuery.CursorID = req.EventsPage.BeforeID
		} else if strings.TrimSpace(req.Cursor) != "" {
			cursor, err := decodeCursor(req.Cursor)
			if err != nil {
				return Response{}, &ValidationError{Message: "cursor is invalid"}
			}
			pageQuery.CursorTimestampMS = &cursor.TimestampMS
			pageQuery.CursorID = &cursor.ID
		}
		page, err := st.PageUsageEvents(ctx, pageQuery)
		if err != nil {
			return Response{}, err
		}
		response.Events = &EventsPage{TotalCount: page.TotalCount, HasMore: page.HasMore, Items: eventItems(page.Items)}
		if page.HasMore && len(page.Items) > 0 {
			last := page.Items[len(page.Items)-1]
			response.Events.NextCursor = encodeCursor(eventCursor{TimestampMS: last.TimestampMS, ID: last.ID})
			response.Events.NextBeforeMS = last.TimestampMS
			response.Events.NextBeforeID = last.ID
		}
		response.Meta.Source = combineSource(response.Meta.Source, "raw")
	}
	return response, nil
}

const dayMS = int64(24 * 60 * 60 * 1000)

func queryDimension(ctx context.Context, st *store.Store, filter store.UsageAggregateFilter, dimension string, coverage int64, useRollup bool) ([]store.UsageDimensionRow, string, error) {
	if !useRollup || len(filtersForcesRaw(filter)) > 0 {
		rows, err := st.AggregateUsageDimension(ctx, filter, dimension)
		return rows, "raw", err
	}
	fullStart, fullEnd := fullDayRange(filter.FromMS, filter.ToMS)
	if fullEnd <= fullStart {
		rows, err := st.AggregateUsageDimension(ctx, filter, dimension)
		return rows, "raw", err
	}

	rollups, err := st.LoadDailyDimensionRollups(ctx, fullStart, fullEnd, dimension)
	if err != nil {
		return nil, "", err
	}
	result := make(map[string]store.UsageMetric)
	for _, row := range rollups {
		result[row.DimensionKey] = addMetric(result[row.DimensionKey], row.Metric)
	}
	hasRollup := len(rollups) > 0
	source := "raw"
	if hasRollup {
		source = "rollup"
	}

	rawRanges := make([]store.UsageAggregateFilter, 0, 3)
	if filter.FromMS < fullStart {
		rawRanges = append(rawRanges, withRange(filter, filter.FromMS, fullStart, 0))
	}
	fullRangeMinID := int64(0)
	if hasRollup {
		fullRangeMinID = coverage
	}
	rawRanges = append(rawRanges, withRange(filter, fullStart, fullEnd, fullRangeMinID))
	if fullEnd < filter.ToMS {
		rawRanges = append(rawRanges, withRange(filter, fullEnd, filter.ToMS, 0))
	}
	for _, rawFilter := range rawRanges {
		if rawFilter.ToMS <= rawFilter.FromMS {
			continue
		}
		rows, err := st.AggregateUsageDimension(ctx, rawFilter, dimension)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			result[row.Dimension] = addMetric(result[row.Dimension], row.Metric)
		}
		if len(rows) > 0 && hasRollup {
			source = "rollup+raw"
		}
	}

	// Dimension rollups intentionally omit per-event billing counters. Replace
	// only cost with an exact bounded SQL aggregate, preserving the existing
	// model_prices semantics until the pricing fan-out is added later.
	prices, err := st.LoadModelPrices(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(prices) > 0 && hasRollup {
		costRows, err := st.AggregateUsageDimension(ctx, filter, dimension)
		if err != nil {
			return nil, "", err
		}
		for _, row := range costRows {
			metric := result[row.Dimension]
			metric.CostUSD = row.Metric.CostUSD
			result[row.Dimension] = metric
		}
		source = combineSource(source, "raw")
	}

	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return result[keys[i]].Requests > result[keys[j]].Requests ||
			(result[keys[i]].Requests == result[keys[j]].Requests && keys[i] < keys[j])
	})
	rows := make([]store.UsageDimensionRow, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, store.UsageDimensionRow{Dimension: key, Metric: result[key]})
	}
	return rows, source, nil
}

func queryDimensionTimeline(ctx context.Context, st *store.Store, filter store.UsageAggregateFilter, dimension string, coverage int64, useRollup bool) ([]DimensionTimeline, string, error) {
	result := make(map[string]map[int64]store.UsageMetric)
	fullStart, fullEnd := fullDayRange(filter.FromMS, filter.ToMS)
	source := "raw"
	hasRollup := false
	rawRanges := make([]store.UsageAggregateFilter, 0, 3)
	if useRollup && len(filtersForcesRaw(filter)) == 0 && fullEnd > fullStart {
		rollups, err := st.LoadDailyDimensionRollups(ctx, fullStart, fullEnd, dimension)
		if err != nil {
			return nil, "", err
		}
		hasRollup = len(rollups) > 0
		for _, row := range rollups {
			byBucket := result[row.DimensionKey]
			if byBucket == nil {
				byBucket = make(map[int64]store.UsageMetric)
				result[row.DimensionKey] = byBucket
			}
			byBucket[row.BucketMS] = addMetric(byBucket[row.BucketMS], row.Metric)
		}
		if hasRollup {
			source = "rollup"
		}
		if filter.FromMS < fullStart {
			rawRanges = append(rawRanges, withRange(filter, filter.FromMS, fullStart, 0))
		}
		fullRangeMinID := int64(0)
		if hasRollup {
			fullRangeMinID = coverage
		}
		rawRanges = append(rawRanges, withRange(filter, fullStart, fullEnd, fullRangeMinID))
		if fullEnd < filter.ToMS {
			rawRanges = append(rawRanges, withRange(filter, fullEnd, filter.ToMS, 0))
		}
	} else {
		rawRanges = append(rawRanges, filter)
		useRollup = false
	}
	if len(rawRanges) == 0 {
		rawRanges = append(rawRanges, filter)
	}
	for _, rawFilter := range rawRanges {
		if rawFilter.ToMS <= rawFilter.FromMS {
			continue
		}
		rows, err := st.AggregateUsageDimensionTimeline(ctx, rawFilter, dimension)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			byBucket := result[row.Dimension]
			if byBucket == nil {
				byBucket = make(map[int64]store.UsageMetric)
				result[row.Dimension] = byBucket
			}
			byBucket[row.BucketMS] = addMetric(byBucket[row.BucketMS], row.Metric)
		}
		if len(rows) > 0 && useRollup && hasRollup {
			source = "rollup+raw"
		}
	}
	if hasRollup {
		prices, err := st.LoadModelPrices(ctx)
		if err != nil {
			return nil, "", err
		}
		if len(prices) > 0 {
			costRows, err := st.AggregateUsageDimensionTimeline(ctx, filter, dimension)
			if err != nil {
				return nil, "", err
			}
			for _, row := range costRows {
				byBucket := result[row.Dimension]
				if byBucket == nil {
					byBucket = make(map[int64]store.UsageMetric)
					result[row.Dimension] = byBucket
				}
				metric := byBucket[row.BucketMS]
				metric.CostUSD = row.Metric.CostUSD
				byBucket[row.BucketMS] = metric
			}
			source = combineSource(source, "raw")
		}
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	response := make([]DimensionTimeline, 0, len(keys))
	for _, key := range keys {
		buckets := make([]int64, 0, len(result[key]))
		for bucket := range result[key] {
			buckets = append(buckets, bucket)
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
		items := make([]TimelineItem, 0, len(buckets))
		for _, bucket := range buckets {
			items = append(items, TimelineItem{BucketMS: bucket, Metric: metricFromAggregate(result[key][bucket])})
		}
		response = append(response, DimensionTimeline{Key: key, Timeline: items})
	}
	return response, source, nil
}

func fullDayRange(fromMS, toMS int64) (int64, int64) {
	start := fromMS - fromMS%dayMS
	if fromMS%dayMS != 0 {
		start += dayMS
	}
	end := toMS - toMS%dayMS
	return start, end
}

func queryCore(ctx context.Context, st *store.Store, filter store.UsageAggregateFilter, coverage int64, useRollup bool) (map[string]store.UsageMetric, string, error) {
	// Keyed as bucket + NUL + model to make merging deterministic without
	// exposing an internal event identifier in the response.
	result := make(map[string]store.UsageMetric)
	fullStart, fullEnd := fullHourRange(filter.FromMS, filter.ToMS)
	source := "raw"
	rawRanges := make([]store.UsageAggregateFilter, 0, 3)
	hasRollup := false
	if useRollup && len(filtersForcesRaw(filter)) == 0 && fullEnd > fullStart {
		rollups, err := st.LoadHourlyRollups(ctx, fullStart, fullEnd)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rollups {
			result[aggregateKey(row.BucketMS, row.Model)] = rollupToMetric(row)
		}
		hasRollup = len(rollups) > 0
		if hasRollup {
			source = "rollup"
		}
		if filter.FromMS < fullStart {
			rawRanges = append(rawRanges, withRange(filter, filter.FromMS, fullStart, 0))
		}
		fullRangeMinID := int64(0)
		if hasRollup {
			fullRangeMinID = coverage
		}
		rawRanges = append(rawRanges, withRange(filter, fullStart, fullEnd, fullRangeMinID))
		if fullEnd < filter.ToMS {
			rawRanges = append(rawRanges, withRange(filter, fullEnd, filter.ToMS, 0))
		}
		if !hasRollup {
			source = "raw"
		}
	} else {
		rawRanges = append(rawRanges, filter)
		useRollup = false
	}
	if len(rawRanges) == 0 {
		rawRanges = append(rawRanges, filter)
	}
	for _, rawFilter := range rawRanges {
		if rawFilter.ToMS <= rawFilter.FromMS {
			continue
		}
		rows, err := st.AggregateUsageEvents(ctx, rawFilter)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			key := aggregateKey(row.BucketMS, row.Model)
			result[key] = addMetric(result[key], row.Metric)
		}
		if len(rows) > 0 && useRollup && hasRollup {
			source = "rollup+raw"
		}
	}
	// T1 intentionally stores operational counters, not per-event billable
	// prompt/cache counters. Recompute cost in one SQL aggregate so cost stays
	// exactly compatible with the existing frontend price semantics while the
	// response remains small. T3 can replace this read with a pricing rollup.
	prices, err := st.LoadModelPrices(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(prices) > 0 && useRollup {
		costRows, err := st.AggregateUsageEvents(ctx, filter)
		if err != nil {
			return nil, "", err
		}
		for _, row := range costRows {
			key := aggregateKey(row.BucketMS, row.Model)
			metric := result[key]
			metric.CostUSD = row.Metric.CostUSD
			result[key] = metric
		}
		source = combineSource(source, "raw")
	}
	return result, source, nil
}

func filtersForcesRaw(filter store.UsageAggregateFilter) []string {
	result := make([]string, 0, 6)
	if strings.TrimSpace(filter.APIKeyHash) != "" {
		result = append(result, "api_key_hash")
	}
	if strings.TrimSpace(filter.Model) != "" {
		result = append(result, "model")
	}
	if strings.TrimSpace(filter.Provider) != "" {
		result = append(result, "provider")
	}
	if strings.TrimSpace(filter.AccountSnapshot) != "" {
		result = append(result, "account_snapshot")
	}
	if strings.TrimSpace(filter.AuthIndex) != "" {
		result = append(result, "auth_index")
	}
	if strings.TrimSpace(filter.Endpoint) != "" {
		result = append(result, "endpoint")
	}
	return result
}

func withRange(filter store.UsageAggregateFilter, fromMS, toMS, minID int64) store.UsageAggregateFilter {
	filter.FromMS = fromMS
	filter.ToMS = toMS
	filter.MinEventID = minID
	return filter
}

func fullHourRange(fromMS, toMS int64) (int64, int64) {
	start := fromMS - fromMS%hourMS
	if fromMS < 0 && fromMS%hourMS != 0 {
		start -= hourMS
	}
	if fromMS%hourMS != 0 {
		start += hourMS
	}
	end := toMS - toMS%hourMS
	if toMS < 0 && toMS%hourMS != 0 {
		end -= hourMS
	}
	return start, end
}

func aggregateKey(bucket int64, model string) string { return fmt.Sprintf("%d\x00%s", bucket, model) }

func rollupToMetric(row store.HourlyRollup) store.UsageMetric {
	return store.UsageMetric{
		Requests: row.Requests, Successes: row.Successes, Failures: row.Failures,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
		CachedTokens: row.CachedTokens, CacheTokens: row.CacheTokens, TotalTokens: row.TotalTokens,
		LatencySumMS: row.LatencySumMS, LatencySamples: row.LatencySamples, ZeroTokenCalls: row.ZeroTokenCalls,
	}
}

func addMetric(left, right store.UsageMetric) store.UsageMetric {
	left.Requests += right.Requests
	left.Successes += right.Successes
	left.Failures += right.Failures
	left.InputTokens += right.InputTokens
	left.OutputTokens += right.OutputTokens
	left.ReasoningTokens += right.ReasoningTokens
	left.CachedTokens += right.CachedTokens
	left.CacheTokens += right.CacheTokens
	left.TotalTokens += right.TotalTokens
	left.LatencySumMS += right.LatencySumMS
	left.LatencySamples += right.LatencySamples
	left.ZeroTokenCalls += right.ZeroTokenCalls
	left.CostUSD += right.CostUSD
	return left
}

func sumAggregate(rows map[string]store.UsageMetric) store.UsageMetric {
	var total store.UsageMetric
	for _, metric := range rows {
		total = addMetric(total, metric)
	}
	return total
}

func metricFromAggregate(metric store.UsageMetric) Metric {
	return Metric{
		Requests: metric.Requests, Successes: metric.Successes, Failures: metric.Failures,
		InputTokens: metric.InputTokens, OutputTokens: metric.OutputTokens, ReasoningTokens: metric.ReasoningTokens,
		CachedTokens: metric.CachedTokens, CacheTokens: metric.CacheTokens, TotalTokens: metric.TotalTokens,
		LatencySumMS: metric.LatencySumMS, LatencySamples: metric.LatencySamples, ZeroTokenCalls: metric.ZeroTokenCalls,
		CostUSD: metric.CostUSD,
	}
}

func buildTimeline(rows map[string]store.UsageMetric) []TimelineItem {
	byBucket := map[int64]store.UsageMetric{}
	for key, metric := range rows {
		bucket, _ := splitAggregateKey(key)
		byBucket[bucket] = addMetric(byBucket[bucket], metric)
	}
	buckets := make([]int64, 0, len(byBucket))
	for bucket := range byBucket {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
	result := make([]TimelineItem, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, TimelineItem{BucketMS: bucket, Metric: metricFromAggregate(byBucket[bucket])})
	}
	return result
}

func buildModelStats(rows map[string]store.UsageMetric) []ModelStat {
	byModel := map[string]store.UsageMetric{}
	for key, metric := range rows {
		_, model := splitAggregateKey(key)
		byModel[model] = addMetric(byModel[model], metric)
	}
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		return byModel[models[i]].Requests > byModel[models[j]].Requests ||
			(byModel[models[i]].Requests == byModel[models[j]].Requests && models[i] < models[j])
	})
	result := make([]ModelStat, 0, len(models))
	for _, model := range models {
		result = append(result, ModelStat{Model: model, Metric: metricFromAggregate(byModel[model])})
	}
	return result
}

func splitAggregateKey(key string) (int64, string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return 0, key
	}
	var bucket int64
	_, _ = fmt.Sscan(parts[0], &bucket)
	return bucket, parts[1]
}

func dimensionStats(rows []store.UsageDimensionRow) []DimensionStat {
	result := make([]DimensionStat, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Dimension)
		if key == "" {
			key = "unknown"
		}
		result = append(result, DimensionStat{Key: key, Metric: metricFromAggregate(row.Metric)})
	}
	return result
}

func eventItems(items []store.UsageEventPageItem) []EventItem {
	result := make([]EventItem, 0, len(items))
	for _, item := range items {
		result = append(result, EventItem{
			ID: item.ID, RequestID: item.RequestID, EventHash: item.EventHash, TimestampMS: item.TimestampMS, Timestamp: item.Timestamp,
			Provider: item.Provider, Model: item.Model, Endpoint: item.Endpoint, Method: item.Method, Path: item.Path,
			AuthType: item.AuthType, AuthIndex: item.AuthIndex, Source: usage.MaskUsageSource(item.Source), SourceHash: item.SourceHash,
			APIKeyHash: item.APIKeyHash, AccountSnapshot: item.AccountSnapshot, AuthLabelSnapshot: item.AuthLabelSnapshot,
			AuthFileSnapshot: item.AuthFileSnapshot, AuthProviderSnapshot: item.AuthProviderSnapshot, AuthSnapshotAtMS: item.AuthSnapshotAtMS,
			InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, ReasoningTokens: item.ReasoningTokens,
			CachedTokens: item.CachedTokens, CacheTokens: item.CacheTokens, TotalTokens: item.TotalTokens,
			LatencyMS: item.LatencyMS, Failed: item.Failed, CreatedAtMS: item.CreatedAtMS,
		})
	}
	return result
}

type eventCursor struct {
	TimestampMS int64 `json:"t"`
	ID          int64 `json:"i"`
}

func encodeCursor(cursor eventCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(raw string) (eventCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return eventCursor{}, err
	}
	var cursor eventCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.TimestampMS < 0 || cursor.ID <= 0 {
		return eventCursor{}, errors.New("invalid cursor")
	}
	return cursor, nil
}

func combineSource(left, right string) string {
	if left == "" || left == "raw" {
		return right
	}
	if right == "" || right == left {
		return left
	}
	if left == "rollup+raw" || right == "rollup+raw" {
		return "rollup+raw"
	}
	return left + "+" + right
}
