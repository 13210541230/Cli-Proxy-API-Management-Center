package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestAnalyticsHTTPContractAuthIncludeAndNoDetailsTree(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.SaveSetup(ctx, store.Setup{CPAUpstreamURL: "http://example.test", ManagementKey: "management-key"}); err != nil {
		t.Fatalf("save setup: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{
		EventHash: "analytics-http-event", TimestampMS: 1_700_000_000_000, Timestamp: "now", Model: "http-model",
		APIKeyHash: "hash-http", InputTokens: 10, OutputTokens: 20, TotalTokens: 30,
	}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	manager := collector.NewManager(cfg, db, nil, collector.AlertConfig{})
	handler := New(cfg, db, manager).Handler()

	body := `{"from_ms":1699999999999,"to_ms":1700000000001,"include":["summary","timeline"]}`
	req := httptest.NewRequest(http.MethodPost, "/v0/management/monitoring/analytics", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("analytics status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode analytics: %v", err)
	}
	if _, ok := payload["summary"]; !ok {
		t.Fatalf("summary missing: %s", rr.Body.String())
	}
	if _, ok := payload["timeline"]; !ok {
		t.Fatalf("timeline missing: %s", rr.Body.String())
	}
	if _, ok := payload["events"]; ok {
		t.Fatalf("events returned without include: %s", rr.Body.String())
	}
	if _, ok := payload["model_stats"]; ok {
		t.Fatalf("model stats returned without include: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "details") || strings.Contains(rr.Body.String(), "raw_json") {
		t.Fatalf("analytics response contains detail/raw tree: %s", rr.Body.String())
	}
	meta, ok := payload["meta"].(map[string]any)
	if !ok || meta["complete"] != true || meta["coverage_event_id"] == nil {
		t.Fatalf("invalid analytics meta: %#v", payload["meta"])
	}
}

func TestAnalyticsHTTPRejectsUnauthorizedInvalidIncludeAndOversizedPage(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)
	cases := []struct {
		name string
		body string
		want int
		auth string
	}{
		{name: "unauthorized", body: `{"from_ms":1,"to_ms":2,"include":["summary"]}`, want: http.StatusUnauthorized, auth: "Bearer wrong"},
		{name: "unknown include", body: `{"from_ms":1,"to_ms":2,"include":["not_real"]}`, want: http.StatusBadRequest, auth: "Bearer management-key"},
		{name: "oversized page", body: `{"from_ms":1,"to_ms":2,"include":["events"],"limit":201}`, want: http.StatusBadRequest, auth: "Bearer management-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v0/management/monitoring/analytics", strings.NewReader(tc.body))
			req.Header.Set("Authorization", tc.auth)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, body = %s; want %d", rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
}
