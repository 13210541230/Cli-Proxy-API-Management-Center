package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

// TestAnalyticsProductionScale is opt-in because it creates the production-sized
// seven-day equivalent fixture (1.4m events). The Windows verification script
// enables it and records the timings and bounded response size.
func TestAnalyticsProductionScale(t *testing.T) {
	if os.Getenv("USAGE_SCALE_TEST") != "1" {
		t.Skip("set USAGE_SCALE_TEST=1 to run the 1.4m-event verification")
	}
	const (
		totalEvents = 1_400_000
		insertBatch = 5000
		dayMS       = int64(24 * 60 * 60 * 1000)
	)
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	baseDay := (time.Now().UnixMilli() / dayMS) * dayMS

	insertStart := time.Now()
	for start := 0; start < totalEvents; start += insertBatch {
		end := start + insertBatch
		events := make([]usage.Event, 0, end-start)
		for i := start; i < end; i++ {
			day := int64(i%30) * dayMS
			events = append(events, usage.Event{
				EventHash:       fmt.Sprintf("scale-%07d", i),
				TimestampMS:     baseDay + day + int64(i%86_400_000),
				Timestamp:       "scale",
				Provider:        fmt.Sprintf("provider-%d", i%4),
				Model:           fmt.Sprintf("model-%d", i%8),
				APIKeyHash:      fmt.Sprintf("api-hash-%03d", i%200),
				AccountSnapshot: fmt.Sprintf("account-%03d", i%200),
				InputTokens:     int64(i % 101), OutputTokens: int64(i % 37),
				TotalTokens: int64(i%101 + i%37), CreatedAtMS: int64(i + 1),
			})
		}
		if _, err := db.InsertEvents(ctx, events); err != nil {
			t.Fatalf("insert batch %d: %v", start/insertBatch, err)
		}
	}
	t.Logf("insert: %s", time.Since(insertStart))

	rollupStart := time.Now()
	processed := 0
	for {
		n, err := db.ApplyHourlyRollupBatch(ctx, insertBatch)
		if err != nil {
			t.Fatalf("rollup: %v", err)
		}
		processed += n
		if n == 0 {
			break
		}
	}
	if processed != totalEvents {
		t.Fatalf("rollup processed %d, want %d", processed, totalEvents)
	}
	t.Logf("rollup: %s", time.Since(rollupStart))

	queryStart := time.Now()
	response, err := Query(ctx, db, Request{
		FromMS: baseDay, ToMS: baseDay + 30*dayMS,
		Include: IncludeList{"summary", "timeline", "model_stats", "account_stats", "api_key_stats", "api_key_timeline"},
	})
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	if response.Summary == nil || response.Summary.Requests != totalEvents || !response.Meta.Complete {
		t.Fatalf("summary/meta = %#v / %#v", response.Summary, response.Meta)
	}
	if len(response.ModelStats) != 8 || len(response.AccountStats) != 200 || len(response.APIKeyStats) != 200 || len(response.APIKeyTimeline) != 200 {
		t.Fatalf("section sizes timeline=%d models=%d accounts=%d keys=%d key_timeline=%d", len(response.Timeline), len(response.ModelStats), len(response.AccountStats), len(response.APIKeyStats), len(response.APIKeyTimeline))
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal analytics response: %v", err)
	}
	t.Logf("analytics query: %s; response: %d bytes; source=%s", time.Since(queryStart), len(encoded), response.Meta.Source)
	if len(encoded) > 5*1024*1024 {
		t.Fatalf("analytics response is unexpectedly large: %d bytes", len(encoded))
	}
}
