package analytics

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
	_ "modernc.org/sqlite"
)

func TestAnalyticsFallsBackToFullRawRangeWhenRollupRowsAreMissing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	const day = int64(24 * 60 * 60 * 1000)
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "fallback-a", TimestampMS: day + 1, Timestamp: "a", Model: "model", AccountSnapshot: "account", APIKeyHash: "key-a", TotalTokens: 3, CreatedAtMS: 1},
		{EventHash: "fallback-b", TimestampMS: 2*day + 1, Timestamp: "b", Model: "model", AccountSnapshot: "account", APIKeyHash: "key-a", TotalTokens: 4, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if _, err := db.ApplyHourlyRollupBatch(ctx, 100); err != nil {
		t.Fatalf("apply rollup: %v", err)
	}
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sql connection: %v", err)
	}
	if _, err := locker.Exec(`delete from usage_hourly_rollups`); err != nil {
		locker.Close()
		t.Fatalf("delete hourly rollups: %v", err)
	}
	if _, err := locker.Exec(`delete from usage_daily_dimension_rollups`); err != nil {
		locker.Close()
		t.Fatalf("delete daily rollups: %v", err)
	}
	if err := locker.Close(); err != nil {
		t.Fatalf("close sql connection: %v", err)
	}

	response, err := Query(ctx, db, Request{FromMS: day, ToMS: 3 * day, Include: IncludeList{"summary", "account_stats", "api_key_timeline"}})
	if err != nil {
		t.Fatalf("fallback query: %v", err)
	}
	if response.Summary == nil || response.Summary.Requests != 2 || response.Summary.TotalTokens != 7 {
		t.Fatalf("fallback summary = %#v", response.Summary)
	}
	if len(response.AccountStats) != 1 || response.AccountStats[0].Requests != 2 {
		t.Fatalf("fallback account stats = %#v", response.AccountStats)
	}
	if len(response.APIKeyTimeline) != 1 || len(response.APIKeyTimeline[0].Timeline) != 2 {
		t.Fatalf("fallback API key timeline = %#v", response.APIKeyTimeline)
	}
	if response.Meta.Source != "raw" {
		t.Fatalf("fallback source = %q, want raw", response.Meta.Source)
	}
}
