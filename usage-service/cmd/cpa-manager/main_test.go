package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/supervisor"
	"github.com/seakee/cpa-manager/usage-service/internal/update"
)

func TestMaybeRecoverInterruptedUpdateKeepsManagerAvailableWhileHelperRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.sqlite")
	statusPath := update.StatusPath(dbPath)
	if err := update.WritePersistedStatus(statusPath, update.PersistedStatus{
		State:         update.StageApplying,
		TransactionID: "active-update",
	}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	lockPath := statusPath + ".lock"
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf(`{"pid":%d,"transactionId":"active-update"}`, os.Getpid())), 0o600); err != nil {
		t.Fatalf("write active update lock: %v", err)
	}
	path := statusPath
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(lockPath)
	})

	handled, err := maybeRecoverInterruptedUpdate(dbPath, false)
	if err != nil {
		t.Fatalf("maybeRecoverInterruptedUpdate() error = %v", err)
	}
	if handled {
		t.Fatal("maybeRecoverInterruptedUpdate() deferred manager startup while updater was active")
	}
}

func TestRuntimeHealthBaseURLPrefersConfiguredEndpoint(t *testing.T) {
	if got := runtimeHealthBaseURL("http://127.0.0.1:9000", "http://127.0.0.1:8317"); got != "http://127.0.0.1:9000" {
		t.Fatalf("runtimeHealthBaseURL() = %q, want configured endpoint", got)
	}
	if got := runtimeHealthBaseURL("", "http://127.0.0.1:9000"); got != "http://127.0.0.1:9000" {
		t.Fatalf("runtimeHealthBaseURL() = %q, want setup endpoint", got)
	}
	if got := runtimeHealthBaseURL("", ""); got != "http://127.0.0.1:8317" {
		t.Fatalf("runtimeHealthBaseURL() = %q, want default endpoint", got)
	}
	if got := runtimeHealthBaseURL("https://remote.example:8317", ""); got != "http://127.0.0.1:8317" {
		t.Fatalf("runtimeHealthBaseURL(remote) = %q, want default local endpoint", got)
	}
}

func TestRuntimeHealthBaseURLSupportsConfiguredLocalAliases(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{name: "localhost", url: "http://localhost:9000", want: "http://localhost:9000"},
		{name: "loopback", url: "127.0.0.1:9000", want: "http://127.0.0.1:9000"},
		{name: "wildcard IPv4", url: "0.0.0.0:9000", want: "http://127.0.0.1:9000"},
		{name: "wildcard IPv6", url: "[::]:9000", want: "http://127.0.0.1:9000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeHealthBaseURL(tc.url, ""); got != tc.want {
				t.Fatalf("runtimeHealthBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigureLocalCPAAdoptsHealthyExternalWhenAutoStartDisabled(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer httpServer.Close()

	controller := supervisor.New()
	configureLocalCPA(controller, supervisor.Config{
		Enabled:           true,
		CPAExecutablePath: "cli-proxy-api",
		AutoStart:         false,
	}, httpServer.URL, false, "test")

	status := controller.Status()
	if !status.External || !status.Running || status.Managed {
		t.Fatalf("status = %#v, want an adopted external process", status)
	}
}
