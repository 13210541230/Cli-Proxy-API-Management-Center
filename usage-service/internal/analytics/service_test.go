package analytics

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestQueryMergesRollupAndLateRawTailWithoutDoubleCounting(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.SaveModelPrices(ctx, map[string]store.ModelPrice{
		"model-a": {Prompt: 1, Completion: 2, Cache: 0.5},
		"model-b": {Prompt: 3, Completion: 4, Cache: 1},
	}); err != nil {
		t.Fatalf("save prices: %v", err)
	}

	const hour = int64(60 * 60 * 1000)
	from, to := hour+100, 4*hour+100
	latency := int64(120)
	initial := []usage.Event{
		{EventHash: "initial-boundary", TimestampMS: hour + 200, Timestamp: "boundary", Model: "model-a", Provider: "p1", APIKeyHash: "hash-a", InputTokens: 10, OutputTokens: 20, TotalTokens: 30, LatencyMS: &latency, CreatedAtMS: 1},
		{EventHash: "initial-full-a", TimestampMS: 2*hour + 200, Timestamp: "full-a", Model: "model-a", Provider: "p1", APIKeyHash: "hash-a", InputTokens: 100, OutputTokens: 50, TotalTokens: 150, CreatedAtMS: 2},
		{EventHash: "initial-full-b", TimestampMS: 3*hour + 200, Timestamp: "full-b", Model: "model-b", Provider: "p2", APIKeyHash: "hash-b", InputTokens: 200, OutputTokens: 70, TotalTokens: 270, Failed: true, CreatedAtMS: 3},
	}
	if _, err := db.InsertEvents(ctx, initial); err != nil {
		t.Fatalf("insert initial events: %v", err)
	}
	for {
		n, err := db.ApplyHourlyRollupBatch(ctx, 2)
		if err != nil {
			t.Fatalf("initial rollup: %v", err)
		}
		if n == 0 {
			break
		}
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		// Late event: its timestamp belongs to a complete bucket already in the rollup.
		{EventHash: "late-full", TimestampMS: 2*hour + 300, Timestamp: "late", Model: "model-a", Provider: "p1", APIKeyHash: "hash-a", InputTokens: 7, OutputTokens: 8, TotalTokens: 15, CreatedAtMS: 4},
		{EventHash: "trailing-boundary", TimestampMS: 4 * hour, Timestamp: "trailing", Model: "model-a", Provider: "p1", APIKeyHash: "hash-a", InputTokens: 1, OutputTokens: 2, TotalTokens: 3, CreatedAtMS: 5},
	}); err != nil {
		t.Fatalf("insert tail events: %v", err)
	}

	response, err := Query(ctx, db, Request{FromMS: from, ToMS: to, Include: IncludeList{"summary", "timeline", "model_stats", "api_key_stats"}})
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	if response.Meta.Complete != true || response.Meta.CoverageEventID != 3 || response.Meta.Source != "rollup+raw" {
		t.Fatalf("analytics meta = %#v", response.Meta)
	}
	if response.Summary == nil {
		t.Fatal("summary is missing")
	}

	rawRows, err := db.AggregateUsageEvents(ctx, store.UsageAggregateFilter{FromMS: from, ToMS: to})
	if err != nil {
		t.Fatalf("raw baseline: %v", err)
	}
	var expected store.UsageMetric
	for _, row := range rawRows {
		expected = addMetric(expected, row.Metric)
	}
	actual := response.Summary
	if actual.Requests != expected.Requests || actual.Successes != expected.Successes || actual.Failures != expected.Failures ||
		actual.InputTokens != expected.InputTokens || actual.OutputTokens != expected.OutputTokens || actual.TotalTokens != expected.TotalTokens ||
		actual.LatencySumMS != expected.LatencySumMS || actual.LatencySamples != expected.LatencySamples || actual.ZeroTokenCalls != expected.ZeroTokenCalls ||
		math.Abs(actual.CostUSD-expected.CostUSD) > 1e-12 {
		t.Fatalf("summary=%#v expected=%#v", actual, expected)
	}
	if len(response.Timeline) != 4 || len(response.ModelStats) != 2 || len(response.APIKeyStats) != 2 {
		t.Fatalf("section sizes timeline=%d models=%d keys=%d", len(response.Timeline), len(response.ModelStats), len(response.APIKeyStats))
	}
}

func TestEventsPageUsesStableKeysetAndRealCount(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	const timestamp = int64(1_700_000_000_000)
	events := make([]usage.Event, 0, 5)
	for i := 0; i < 5; i++ {
		events = append(events, usage.Event{
			EventHash:   string(rune('a' + i)),
			TimestampMS: timestamp,
			Timestamp:   "same-time",
			Model:       "page-model",
			Source: func() string {
				if i == 0 {
					return "sk-test-abcdefghijklmnopqrstuvwxyz123456"
				}
				return "safe-source"
			}(),
			APIKeyHash:  "page-key",
			CreatedAtMS: int64(i + 1),
		})
	}
	if _, err := db.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	request := Request{FromMS: timestamp - 1, ToMS: timestamp + 1, Include: IncludeList{"events"}, Limit: 2}
	seen := map[int64]bool{}
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		response, err := Query(ctx, db, request)
		if err != nil {
			t.Fatalf("page %d: %v", pageNumber, err)
		}
		if response.Events == nil || response.Events.TotalCount != 5 {
			t.Fatalf("page %d events = %#v", pageNumber, response.Events)
		}
		for _, item := range response.Events.Items {
			if item.Source == "sk-test-abcdefghijklmnopqrstuvwxyz123456" {
				t.Fatalf("raw source leaked in page item %d", item.ID)
			}
			if seen[item.ID] {
				t.Fatalf("duplicate event id %d", item.ID)
			}
			seen[item.ID] = true
		}
		if !response.Events.HasMore {
			if response.Events.NextCursor != "" || len(seen) != 5 {
				t.Fatalf("final page cursor=%q seen=%d", response.Events.NextCursor, len(seen))
			}
			break
		}
		if response.Events.NextCursor == "" || response.Events.NextBeforeID == 0 {
			t.Fatalf("page %d has_more without cursor: %#v", pageNumber, response.Events)
		}
		request.Cursor = response.Events.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("seen event count = %d, want 5", len(seen))
	}
}

func TestAnalyticsSecuritySignalCountCoversFullRangeBeyondEventPage(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const start = int64(1_700_000_000_000)
	events := make([]usage.Event, 0, 3)
	for i := 0; i < 3; i++ {
		events = append(events, usage.Event{
			EventHash:      string(rune('s' + i)),
			TimestampMS:    start + int64(i),
			Timestamp:      "security-time",
			Model:          "security-model",
			SecuritySignal: usage.SecuritySignalCyberPolicy,
			CreatedAtMS:    int64(i + 1),
		})
	}
	if _, err := db.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert security events: %v", err)
	}

	response, err := Query(ctx, db, Request{
		FromMS:  start - 1,
		ToMS:    start + 10,
		Include: IncludeList{"summary", "events"},
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	if response.SecuritySignalCount != 3 {
		t.Fatalf("security signal count = %d, want 3", response.SecuritySignalCount)
	}
	if response.Events == nil || len(response.Events.Items) != 1 || response.Events.TotalCount != 3 {
		t.Fatalf("bounded events = %#v", response.Events)
	}
	if response.Events.Items[0].SecuritySignal != usage.SecuritySignalCyberPolicy {
		t.Fatalf("event marker = %#v", response.Events.Items[0])
	}
}

func TestAnalyticsRequestRejectsUnknownIncludeAndInvalidCursor(t *testing.T) {
	if err := ValidateRequest(Request{FromMS: 1, ToMS: 2, Include: IncludeList{"unknown"}}); err == nil {
		t.Fatal("unknown include accepted")
	}
	if err := ValidateRequest(Request{FromMS: 2, ToMS: 2, Include: IncludeList{"summary"}}); err == nil {
		t.Fatal("empty range accepted")
	}
	if err := ValidateRequest(Request{FromMS: 1, ToMS: 2, Include: IncludeList{"events"}, Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestIncludeObjectAndResponseDoesNotContainRawJSON(t *testing.T) {
	var request Request
	if err := json.Unmarshal([]byte(`{"from_ms":1,"to_ms":2,"include":{"summary":true,"events_page":{"limit":10}}}`), &request); err != nil {
		t.Fatalf("decode include object: %v", err)
	}
	if len(request.Include) != 2 || !includes(request, "summary") || !includes(request, "events") || request.EventsPage == nil || request.EventsPage.Limit != 10 {
		t.Fatalf("include = %#v, events page = %#v", request.Include, request.EventsPage)
	}
}
