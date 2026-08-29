package analytics

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestDimensionAnalyticsUsesDailyRollupAndMergesLateTail(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	const day = int64(24 * 60 * 60 * 1000)
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{EventHash: "daily-a", TimestampMS: day + 100, Timestamp: "a", Model: "model-a", APIKeyHash: "key-a", AccountSnapshot: "account-a", ReasoningEffort: "high", InputTokens: 10, ReasoningTokens: 4, TotalTokens: 14, CreatedAtMS: 1},
		{EventHash: "daily-b", TimestampMS: 2*day + 100, Timestamp: "b", Model: "model-a", APIKeyHash: "key-b", AccountSnapshot: "account-b", ReasoningEffort: "low", OutputTokens: 20, TotalTokens: 20, CreatedAtMS: 2},
	}); err != nil {
		t.Fatalf("insert initial events: %v", err)
	}
	for {
		n, err := db.ApplyHourlyRollupBatch(ctx, 1)
		if err != nil {
			t.Fatalf("rollup batch: %v", err)
		}
		if n == 0 {
			break
		}
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{
		EventHash: "daily-late", TimestampMS: day + 200, Timestamp: "late", Model: "model-a", APIKeyHash: "key-a", AccountSnapshot: "account-a", ReasoningEffort: "high", InputTokens: 2, ReasoningTokens: 1, TotalTokens: 3, CreatedAtMS: 3,
	}}); err != nil {
		t.Fatalf("insert late event: %v", err)
	}

	response, err := Query(ctx, db, Request{FromMS: day, ToMS: 3 * day, Include: IncludeList{"account_stats", "api_key_stats", "reasoning_stats"}})
	if err != nil {
		t.Fatalf("dimension query: %v", err)
	}
	if response.Meta.Source != "rollup+raw" || !response.Meta.Complete {
		t.Fatalf("meta = %#v", response.Meta)
	}
	if len(response.AccountStats) != 2 || len(response.APIKeyStats) != 2 || len(response.ReasoningStats) != 2 {
		t.Fatalf("account stats=%#v api stats=%#v reasoning stats=%#v", response.AccountStats, response.APIKeyStats, response.ReasoningStats)
	}
	if response.AccountStats[0].Key != "account-a" || response.AccountStats[0].Requests != 2 || response.APIKeyStats[0].Key != "key-a" || response.APIKeyStats[0].Requests != 2 || response.ReasoningStats[0].Key != "high" || response.ReasoningStats[0].Requests != 2 || response.ReasoningStats[0].ReasoningTokens != 5 {
		t.Fatalf("dimension stats account=%#v api=%#v reasoning=%#v", response.AccountStats, response.APIKeyStats, response.ReasoningStats)
	}

	rawAccount, err := db.AggregateUsageDimension(ctx, store.UsageAggregateFilter{FromMS: day, ToMS: 3 * day}, "account")
	if err != nil {
		t.Fatalf("raw account baseline: %v", err)
	}
	if len(rawAccount) != len(response.AccountStats) || math.Abs(response.AccountStats[0].CostUSD-rawAccount[0].Metric.CostUSD) > 1e-12 {
		t.Fatalf("daily rollup differs from raw baseline: got=%#v raw=%#v", response.AccountStats, rawAccount)
	}
}
