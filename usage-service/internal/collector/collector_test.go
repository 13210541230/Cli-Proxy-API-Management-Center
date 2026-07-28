package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestManagerConsumesHTTPUsageQueue(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			if r.Header.Get("Authorization") != "Bearer management-key" {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"auth_index":"auth-1","account":"alice@example.com","label":"Alice","name":"alice.json","provider":"codex"}]}`))
			return
		}
		if r.URL.Path != "/v0/management/usage-queue" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer management-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`[{
				"timestamp": "2026-05-06T00:00:00Z",
				"model": "gpt-test",
				"endpoint": "POST /v1/chat/completions",
				"auth_index": "auth-1",
				"input_tokens": 10,
				"output_tokens": 5
			}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	cfg := testConfig(t, "auto")
	manager := NewManager(cfg, db, nil, AlertConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx, RuntimeConfig{
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
	})

	waitFor(t, func() bool {
		events, _, err := db.Counts(context.Background())
		return err == nil && events == 1
	})

	status := manager.Status()
	if status.Transport != "http" {
		t.Fatalf("transport = %q, want http", status.Transport)
	}
	if status.TotalInserted != 1 {
		t.Fatalf("total inserted = %d, want 1", status.TotalInserted)
	}
	events, err := db.RecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].AccountSnapshot != "alice@example.com" {
		t.Fatalf("account snapshot = %q", events[0].AccountSnapshot)
	}
	if events[0].AuthLabelSnapshot != "Alice" {
		t.Fatalf("auth label snapshot = %q", events[0].AuthLabelSnapshot)
	}
}

func TestManagerFallsBackToRESPWhenHTTPQueueUnsupported(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	cfg := testConfig(t, "auto")
	manager := NewManager(cfg, db, nil, AlertConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx, RuntimeConfig{
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
	})

	waitFor(t, func() bool {
		status := manager.Status()
		return status.Transport == "resp" && strings.Contains(status.LastError, "unsupported RESP prefix")
	})
}

func TestCheckAndEnforceLimitsUsesDefaultAndOverrides(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/config" {
			t.Fatal("CheckAndEnforceLimits must not fetch quota config from CPA")
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer management-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Default: store.SpendLimit{DailyCents: 1000, WeeklyCents: 1000},
		Overrides: []store.SpendLimitEntry{
			{ApplyTo: "api-key", ApplyValue: "hash-b", DailyCents: 100, WeeklyCents: 1000},
		},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	now := time.Now()
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-a", "hash-a", now),
		spendLimitEvent("event-b", "hash-b", now),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if paused["hash-a"] {
		t.Fatal("hash-a should use default limit and stay active")
	}
	if !paused["hash-b"] {
		t.Fatal("hash-b should use override limit and be paused")
	}
}

func TestCheckAndEnforceLimitsMatchesShortOverrideToFullUsageHash(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("b", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash[:8],
			DailyCents:  1,
			WeeklyCents: 1000,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("event-full-hash", fullHash, time.Now())}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused[fullHash[:8]] {
		t.Fatalf("short override should pause full usage hash as %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyOverrideMeansNoDailyLimit(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("a", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash,
			DailyCents:  0,
			WeeklyCents: 0,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("event-zero", fullHash, time.Now())}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if paused[fullHash[:8]] {
		t.Fatalf("zero daily/weekly override should not pause key %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyOverrideStillEnforcesWeekly(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("b", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash,
			DailyCents:  0,
			WeeklyCents: 100,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-weekly", fullHash, time.Now()),
		spendLimitEvent("event-weekly-2", fullHash, time.Now()),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused[fullHash[:8]] {
		t.Fatalf("weekly override should pause key %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyDefaultStillEnforcesWeekly(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Default: store.SpendLimit{DailyCents: 0, WeeklyCents: 100},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-default-weekly", "hash-default", time.Now()),
		spendLimitEvent("event-default-weekly-2", "hash-default", time.Now()),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused["hash-default"] {
		t.Fatal("weekly default should pause key when daily default is unlimited")
	}
}

func spendLimitEvent(hash, keyHash string, at time.Time) usage.Event {
	return usage.Event{
		EventHash:   hash,
		TimestampMS: at.UnixMilli(),
		Timestamp:   at.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  keyHash,
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: at.UnixMilli(),
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func testConfig(t *testing.T, mode string) config.Config {
	t.Helper()
	return config.Config{
		DBPath:        filepath.Join(t.TempDir(), "usage.sqlite"),
		CollectorMode: mode,
		Queue:         "usage",
		PopSide:       "right",
		BatchSize:     10,
		PollInterval:  10 * time.Millisecond,
	}
}

func TestQueryAccountQuotaSupportsCamelCaseAndWindowFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/api-call":
			if r.Header.Get("Authorization") != "Bearer management-key" {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status_code":200,
				"body":"{\"rateLimit\":{\"primaryWindow\":{\"usedPercent\":100,\"limitWindowSeconds\":604800,\"resetAt\":1751904000},\"secondaryWindow\":{\"usedPercent\":100,\"limitWindowSeconds\":18000,\"resetAt\":1751328000}}}"
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	checker := newPoolQuotaChecker(nil, nil, upstream.URL, "management-key")
	result := checker.queryAccountQuota(context.Background(), "auth-1", "account-1")

	if result.err != "" {
		t.Fatalf("queryAccountQuota returned error: %s", result.err)
	}
	if result.fiveHourUsed != 100 {
		t.Fatalf("fiveHourUsed = %v, want 100", result.fiveHourUsed)
	}
	if result.weeklyUsed != 100 {
		t.Fatalf("weeklyUsed = %v, want 100", result.weeklyUsed)
	}
	if result.fiveHourReset == "" {
		t.Fatal("fiveHourReset should not be empty")
	}
	if result.weeklyReset == "" {
		t.Fatal("weeklyReset should not be empty")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}
