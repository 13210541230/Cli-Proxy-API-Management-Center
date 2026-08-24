package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

func TestQuotaConfigValidatesDowngradeBeforeSave(t *testing.T) {
	validationCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/validate-model":
			validationCalls++
			if r.Method != http.MethodPost {
				t.Fatalf("validation method = %s", r.Method)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid fallback model"}`))
		case "/v0/management/quota/paused", "/v0/management/quota/downgraded":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		DBPath:         filepath.Join(t.TempDir(), "usage.sqlite"),
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
		Queue:          "usage",
		PopSide:        "right",
		CORSOrigins:    []string{"*"},
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	defer db.Close()
	manager := collector.NewManager(cfg, db, nil, collector.AlertConfig{})
	handler := New(cfg, db, manager).Handler()

	req := httptest.NewRequest(http.MethodPut, "/v0/management/quota/config", strings.NewReader(`{"enabled":true,"exceeded_action":"downgrade","fallback_model":"bad-model"}`))
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid model status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if validationCalls != 1 {
		t.Fatalf("validation calls = %d, want 1", validationCalls)
	}
	if _, ok, err := db.LoadSpendLimitConfig(context.Background()); err != nil || ok {
		t.Fatalf("config after rejected validation = ok:%v err:%v", ok, err)
	}
}

func TestQuotaConfigReturnsDowngradeDefaults(t *testing.T) {
	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	defer db.Close()
	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v0/management/quota/config", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET quota config status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"exceeded_action":"pause"`) || !strings.Contains(body, `"fallback_model":"gpt-5.6-luna"`) {
		t.Fatalf("quota defaults missing: %s", body)
	}
}
