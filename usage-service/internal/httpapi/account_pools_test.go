package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountPoolManagementAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			_, _ = w.Write([]byte(`{"files":[{"id":"auth-a","auth_index":"auth-a","name":"auth-a.json","provider":"codex","type":"codex"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	handler := newTestHandler(t, upstream.URL, true)
	create := httptest.NewRequest(http.MethodPost, "/v0/management/account-pools", strings.NewReader(`{"id":"engineering","name":"Engineering","provider":"codex","enabled":true}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Authorization", "Bearer management-key")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}

	members := httptest.NewRequest(http.MethodPut, "/v0/management/account-pools/engineering/members", strings.NewReader(`{"items":[{"authId":"auth-a","priority":1,"enabled":true}]}`))
	members.Header.Set("Content-Type", "application/json")
	members.Header.Set("Authorization", "Bearer management-key")
	membersResponse := httptest.NewRecorder()
	handler.ServeHTTP(membersResponse, members)
	if membersResponse.Code != http.StatusOK {
		t.Fatalf("members status = %d, body = %s", membersResponse.Code, membersResponse.Body.String())
	}

	bindings := httptest.NewRequest(http.MethodPut, "/v0/management/account-pools/bindings", strings.NewReader(`{"items":[{"apiKeyHash":"abcdef12","poolId":"engineering"}]}`))
	bindings.Header.Set("Content-Type", "application/json")
	bindings.Header.Set("Authorization", "Bearer management-key")
	bindingsResponse := httptest.NewRecorder()
	handler.ServeHTTP(bindingsResponse, bindings)
	if bindingsResponse.Code != http.StatusOK {
		t.Fatalf("bindings status = %d, body = %s", bindingsResponse.Code, bindingsResponse.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/v0/management/account-pools", nil)
	get.Header.Set("Authorization", "Bearer management-key")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", got.Code, got.Body.String())
	}
	var payload struct {
		Policy struct {
			Pools    []any `json:"pools"`
			Members  []any `json:"members"`
			Bindings []any `json:"bindings"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Policy.Pools) != 1 || len(payload.Policy.Members) != 1 || len(payload.Policy.Bindings) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAccountPoolRejectsNonCodexMember(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			_, _ = w.Write([]byte(`{"files":[{"id":"auth-gemini","auth_index":"auth-gemini","name":"auth-gemini.json","provider":"gemini","type":"gemini"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	handler := newTestHandler(t, upstream.URL, true)
	create := httptest.NewRequest(http.MethodPost, "/v0/management/account-pools", strings.NewReader(`{"id":"codex-pool","name":"Codex","provider":"codex","enabled":true}`))
	create.Header.Set("Authorization", "Bearer management-key")
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	members := httptest.NewRequest(http.MethodPut, "/v0/management/account-pools/codex-pool/members", strings.NewReader(`{"items":[{"authId":"auth-gemini","enabled":true}]}`))
	members.Header.Set("Authorization", "Bearer management-key")
	members.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, members)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "non-Codex") {
		t.Fatalf("non-Codex member response = %d %s", response.Code, response.Body.String())
	}
}

func TestAccountPoolMutationPublishesSnapshotAndRecordsActiveVersion(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, true)
	request := httptest.NewRequest(http.MethodPost, "/v0/management/account-pools", strings.NewReader(`{"id":"engineering","name":"Engineering","provider":"codex","enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer management-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Status struct {
			ExclusiveReady bool  `json:"exclusiveReady"`
			AppliedVersion int64 `json:"appliedVersion"`
		} `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Status.ExclusiveReady || payload.Status.AppliedVersion == 0 {
		t.Fatalf("status = %#v", payload.Status)
	}
	if gotPath != "/v0/management/plugins/cpa-account-config-manager/account-pools/policy" || gotAuth != "Bearer management-key" {
		t.Fatalf("publication = path %q auth %q", gotPath, gotAuth)
	}
	if _, ok := gotBody["policy"]; !ok {
		t.Fatalf("publication body = %#v", gotBody)
	}
}
