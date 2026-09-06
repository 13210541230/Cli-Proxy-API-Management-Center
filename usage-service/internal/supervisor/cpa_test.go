package supervisor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestHasHealthyLocalCPA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	if !HasHealthyLocalCPA(context.Background(), server.URL) {
		t.Fatal("HasHealthyLocalCPA() = false, want true")
	}
	if HasHealthyLocalCPA(context.Background(), "https://example.com") {
		t.Fatal("HasHealthyLocalCPA() accepted a non-local URL")
	}
}

func TestControllerDoesNotStartOverExternalProcess(t *testing.T) {
	controller := New()
	controller.Configure(Config{Enabled: true, CPAExecutablePath: os.Args[0]})
	controller.MarkExternal()

	status, err := controller.Start()
	if err == nil {
		t.Fatal("Start() error = nil")
	}
	if !status.External || !status.Running || status.Managed {
		t.Fatalf("external status = %#v", status)
	}
}

func TestControllerDetectsHealthyLocalCPABeforeStarting(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer health.Close()

	controller := New()
	controller.Configure(Config{
		Enabled:           true,
		CPAExecutablePath: os.Args[0],
		HealthURL:         health.URL,
	})
	status, err := controller.Start()
	if err == nil {
		t.Fatal("Start() error = nil")
	}
	if !status.External || !status.Running || status.Managed {
		t.Fatalf("detected external status = %#v", status)
	}
}

func TestControllersArbitrateCrossProcessStarts(t *testing.T) {
	t.Setenv("SUPERVISOR_HELPER", "1")
	config := Config{
		Enabled:           true,
		CPAExecutablePath: os.Args[0],
		WorkingDirectory:  t.TempDir(),
		Arguments:         []string{"-test.run=TestSupervisorHelperProcess", "--", "arbitration"},
	}
	first := New()
	if err := first.Configure(config); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	if _, err := first.Start(); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = first.Stop()
		_ = first.WaitStopped(2 * time.Second)
	})

	second := New()
	if err := second.Configure(config); err != nil {
		t.Fatalf("second Configure() error = %v", err)
	}
	status, err := second.Start()
	if err == nil {
		t.Fatal("second Start() error = nil, want cross-process arbitration")
	}
	if status.Managed || status.Running || status.State != StateFailed {
		t.Fatalf("second status = %#v, want failed non-running controller", status)
	}
}

func TestControllerStartsAndStopsManagedProcess(t *testing.T) {
	t.Setenv("SUPERVISOR_HELPER", "1")

	controller := New()
	controller.Configure(Config{
		Enabled:           true,
		CPAExecutablePath: os.Args[0],
		WorkingDirectory:  t.TempDir(),
		Arguments:         []string{"-test.run=TestSupervisorHelperProcess", "--", "helper"},
	})

	started, err := controller.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !started.Running || !started.Managed || started.PID == 0 {
		t.Fatalf("start status = %#v", started)
	}
	if started.Health != HealthUnknown || started.Healthy {
		t.Fatalf("initial health status = %#v", started)
	}
	if started.ExecutablePath != mustAbs(t, os.Args[0]) {
		t.Fatalf("executable path = %q", started.ExecutablePath)
	}
	if err := controller.Configure(Config{
		Enabled:           true,
		CPAExecutablePath: os.Args[0],
		WorkingDirectory:  t.TempDir(),
		Arguments:         []string{"-test.run=TestSupervisorHelperProcess", "--", "changed"},
	}); err == nil {
		t.Fatal("Configure() while running returned nil")
	}

	stopping, err := controller.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopping.Running || stopping.State != StateStopping {
		t.Fatalf("stop status = %#v", stopping)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := controller.Status()
		if !status.Running && status.State == StateStopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process did not stop: %#v", controller.Status())
}

func TestControllerTracksCPAHealth(t *testing.T) {
	var healthy atomic.Bool
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer health.Close()

	t.Setenv("SUPERVISOR_HELPER", "1")
	controller := New()
	if err := controller.Configure(Config{
		Enabled:           true,
		CPAExecutablePath: os.Args[0],
		WorkingDirectory:  t.TempDir(),
		Arguments:         []string{"-test.run=TestSupervisorHelperProcess", "--", "health"},
		HealthURL:         health.URL,
	}); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if _, err := controller.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	healthy.Store(true)
	waitForHealthState(t, controller, HealthHealthy)

	healthy.Store(false)
	waitForHealthState(t, controller, HealthUnhealthy)

	if _, err := controller.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := controller.WaitStopped(2 * time.Second); err != nil {
		t.Fatalf("WaitStopped() error = %v", err)
	}
}

func waitForHealthState(t *testing.T, controller *Controller, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := controller.Status().Health; got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("health state = %q, want %q; status = %#v", controller.Status().Health, want, controller.Status())
}

func TestControllerRejectsMissingExecutable(t *testing.T) {
	controller := New()
	controller.Configure(Config{
		Enabled:           true,
		CPAExecutablePath: filepath.Join(t.TempDir(), "missing-cpa"),
	})

	status, err := controller.Start()
	if err == nil {
		t.Fatal("Start() error = nil")
	}
	if status.State != StateFailed || status.LastError == "" {
		t.Fatalf("failure status = %#v", status)
	}
}

func TestControllerRejectsDisabledRuntime(t *testing.T) {
	controller := New()
	status, err := controller.Start()
	if err == nil {
		t.Fatal("Start() error = nil")
	}
	if status.State != StateStopped || status.Enabled {
		t.Fatalf("disabled status = %#v", status)
	}
}

func TestSupervisorHelperProcess(t *testing.T) {
	if os.Getenv("SUPERVISOR_HELPER") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	return resolved
}
