package analytics

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestPartialRollupCoverageDoesNotShrinkWiderRanges(t *testing.T) {
	// Regression: with rollup coverage only reaching part of the request
	// window, a 14-day query must return at least as much as a 7-day query.
	// Production reported 14-day analytics looking like 2-3 days of data,
	// which indicates the wider window lost events somewhere in the
	// rollup+raw merge.
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const hour = int64(60 * 60 * 1000)
	const day = hour * 24
	now := time.Unix(1_800_000_000, 0)
	// 40 days of events, one request per hour, ids and timestamps both
	// increasing on the same axis (id order == time order).
	events := make([]usage.Event, 0, 40*24)
	for dayOffset := 39; dayOffset >= 0; dayOffset-- {
		ts := now.Add(-time.Duration(dayOffset) * 24 * time.Hour)
		for h := 0; h < 24; h++ {
			t := ts.Add(time.Duration(h) * time.Hour)
			events = append(events, usage.Event{
				EventHash:    fmt.Sprintf("ev-d%d-h%d", dayOffset, h),
				TimestampMS:  t.UnixMilli(),
				Timestamp:    t.Format(time.RFC3339),
				Model:        "model-a",
				Provider:     "p1",
				APIKeyHash:   "hash-a",
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			})
		}
	}
	if _, err := db.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	latest, err := db.LatestUsageEventID(ctx)
	if err != nil {
		t.Fatalf("latest id: %v", err)
	}
	if latest != int64(len(events)) {
		t.Fatalf("latest id = %d want %d", latest, len(events))
	}
	// Partial coverage: the 14-day window overlaps both persisted rollup
	// buckets and raw tail, while the 7-day window comes only from raw tail.
	covered := latest - 10*24
	for {
		state, err := db.LoadRollupState(ctx)
		if err != nil {
			t.Fatalf("load rollup state: %v", err)
		}
		if state.CoverageEventID >= covered {
			break
		}
		if _, err := db.ApplyHourlyRollupBatch(ctx, int(covered)); err != nil {
			t.Fatalf("rollup batch: %v", err)
		}
	}
	state, err := db.LoadRollupState(ctx)
	if err != nil {
		t.Fatalf("load rollup state: %v", err)
	}
	if state.CoverageEventID != covered {
		t.Fatalf("coverage = %d want %d", state.CoverageEventID, covered)
	}

	from7 := now.AddDate(0, 0, -7).UnixMilli()
	from14 := now.AddDate(0, 0, -14).UnixMilli()
	to := now.Add(time.Minute).UnixMilli()

	seven, err := Query(ctx, db, Request{FromMS: from7, ToMS: to, Include: IncludeList{"summary", "account_stats", "api_key_stats"}})
	if err != nil {
		t.Fatalf("7d query: %v", err)
	}
	fourteen, err := Query(ctx, db, Request{FromMS: from14, ToMS: to, Include: IncludeList{"summary", "account_stats", "api_key_stats"}})
	if err != nil {
		t.Fatalf("14d query: %v", err)
	}
	if seven.Summary == nil || fourteen.Summary == nil {
		t.Fatalf("summary missing: 7d=%#v 14d=%#v", seven.Meta, fourteen.Meta)
	}
	if fourteen.Summary.Requests < seven.Summary.Requests {
		t.Fatalf("14d requests=%d < 7d requests=%d (meta7=%#v meta14=%#v)", fourteen.Summary.Requests, seven.Summary.Requests, seven.Meta, fourteen.Meta)
	}

	for name, item := range map[string]struct {
		from     int64
		response Response
	}{
		"7d":  {from: from7, response: seven},
		"14d": {from: from14, response: fourteen},
	} {
		rawRows, err := db.AggregateUsageEvents(ctx, store.UsageAggregateFilter{FromMS: item.from, ToMS: to})
		if err != nil {
			t.Fatalf("%s raw baseline: %v", name, err)
		}
		var expected store.UsageMetric
		for _, row := range rawRows {
			expected = addMetric(expected, row.Metric)
		}
		actual := item.response.Summary
		if actual.Requests != expected.Requests || actual.Successes != expected.Successes || actual.Failures != expected.Failures ||
			actual.InputTokens != expected.InputTokens || actual.OutputTokens != expected.OutputTokens || actual.TotalTokens != expected.TotalTokens ||
			actual.LastSeenMS != expected.LastSeenMS {
			t.Fatalf("%s summary=%#v expected=%#v", name, actual, expected)
		}
		if len(item.response.AccountStats) != 1 || item.response.AccountStats[0].LastSeenMS != expected.LastSeenMS ||
			len(item.response.APIKeyStats) != 1 || item.response.APIKeyStats[0].LastSeenMS != expected.LastSeenMS {
			t.Fatalf("%s dimension last_seen account=%#v api_key=%#v expected=%d", name, item.response.AccountStats, item.response.APIKeyStats, expected.LastSeenMS)
		}
	}
}
