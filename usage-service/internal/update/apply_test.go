package update

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessMatchesExecutable(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := processMatchesExecutable(os.Getpid(), current)
	if err != nil {
		t.Fatalf("processMatchesExecutable() error = %v", err)
	}
	if !matches {
		t.Fatalf("processMatchesExecutable(%d, %q) = false, want true", os.Getpid(), current)
	}
	matches, err = processMatchesExecutable(os.Getpid(), filepath.Join(t.TempDir(), "other"))
	if err != nil {
		t.Fatalf("processMatchesExecutable() mismatch error = %v", err)
	}
	if matches {
		t.Fatal("processMatchesExecutable() accepted a different path")
	}
}

func TestIsCurrentExecutable(t *testing.T) {
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !isCurrentExecutable(current) {
		t.Fatalf("isCurrentExecutable(%q) = false", current)
	}
	if isCurrentExecutable(filepath.Join(t.TempDir(), "other")) {
		t.Fatal("isCurrentExecutable() accepted a different path")
	}
}

func TestApplyReplacesBundleStartsProcessesAndPersistsResult(t *testing.T) {
	if os.Getenv("UPDATE_APPLY_HELPER") == "1" {
		t.Skip("helper environment must only be used by child processes")
	}

	root := t.TempDir()
	stagingRoot, err := os.MkdirTemp("", "cpa-manager-update-")
	if err != nil {
		t.Fatalf("create managed staging root: %v", err)
	}
	staging := filepath.Join(stagingRoot, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("create staging: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	for _, name := range []string{"cli-proxy-api", "cpa-manager", "cpa-updater"} {
		name += suffix
		if err := copyTestBinary(self, filepath.Join(staging, name)); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
	}
	managerPath := filepath.Join(root, "installed-manager"+suffix)
	cpaPath := filepath.Join(root, "installed-cpa"+suffix)
	updaterPath := filepath.Join(root, "installed-updater"+suffix)
	for _, target := range []string{managerPath, cpaPath, updaterPath} {
		if err := copyTestBinary(self, target); err != nil {
			t.Fatalf("install %s: %v", target, err)
		}
	}

	managerAddr := freeLocalAddress(t)
	cpaAddr := freeLocalAddress(t)
	t.Setenv("UPDATE_APPLY_HELPER", "1")
	t.Setenv("UPDATE_APPLY_MANAGER_ADDR", managerAddr)
	t.Setenv("UPDATE_APPLY_CPA_ADDR", cpaAddr)
	resultPath := filepath.Join(t.TempDir(), "update-result.json")
	err = Apply(ApplyOptions{
		StagingPath:             staging,
		ResultPath:              resultPath,
		OS:                      runtime.GOOS,
		Arch:                    runtime.GOARCH,
		CPAVersion:              "7.2.146",
		ManagerVersion:          "7.2.146",
		ManagerExecutablePath:   managerPath,
		ManagerWorkingDirectory: root,
		CPAExecutablePath:       cpaPath,
		UpdaterExecutablePath:   updaterPath,
		ManagerArguments:        []string{"-test.run=TestApplyManagerHelperProcess"},
		CPAArguments:            []string{"-test.run=TestApplyCPAHelperProcess"},
		CPAWorkingDirectory:     root,
		ManagerHealthURL:        "http://" + managerAddr + "/health",
		CPAHealthURL:            "http://" + cpaAddr + "/healthz",
		ManagerPID:              0,
		StartCPA:                true,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	status, ok, err := ReadPersistedStatus(resultPath)
	if err != nil || !ok {
		t.Fatalf("result = %#v, %v, %v", status, ok, err)
	}
	if status.State != StageSucceeded {
		t.Fatalf("result state = %q, want %q", status.State, StageSucceeded)
	}
	if status.OS != runtime.GOOS || status.Arch != runtime.GOARCH {
		t.Fatalf("result target = %#v", status)
	}
	if status.CPAVersion != "7.2.146" || status.ManagerVersion != "7.2.146" {
		t.Fatalf("result versions = %#v", status)
	}
	time.Sleep(10500 * time.Millisecond)
}

func TestStartProcessesPassesManagerStartCPA(t *testing.T) {
	t.Setenv("UPDATE_APPLY_HELPER", "1")
	managerAddr := freeLocalAddress(t)
	t.Setenv("UPDATE_APPLY_MANAGER_ADDR", managerAddr)

	_, managerCmd, err := startProcesses(ApplyOptions{
		ManagerExecutablePath:   os.Args[0],
		ManagerWorkingDirectory: t.TempDir(),
		ManagerArguments:        []string{"-test.run=TestApplyManagerHelperProcess"},
		ManagerStartCPA:         true,
	})
	if err != nil {
		t.Fatalf("startProcesses() error = %v", err)
	}
	t.Cleanup(func() { terminateProcess(managerCmd) })
	if !containsArgument(managerCmd.Args, "--start-cpa") {
		t.Fatalf("manager args = %#v, want --start-cpa", managerCmd.Args)
	}
}

func TestApplyManagerHelperProcess(t *testing.T) {
	if os.Getenv("UPDATE_APPLY_HELPER") != "1" {
		return
	}
	serveApplyHealth(t, os.Getenv("UPDATE_APPLY_MANAGER_ADDR"), "/health")
}

func TestApplyCPAHelperProcess(t *testing.T) {
	if os.Getenv("UPDATE_APPLY_HELPER") != "1" {
		return
	}
	serveApplyHealth(t, os.Getenv("UPDATE_APPLY_CPA_ADDR"), "/healthz")
}

func serveApplyHealth(t *testing.T, address, path string) {
	if strings.TrimSpace(address) == "" {
		t.Fatal("helper address is empty")
	}
	server := &http.Server{
		Addr: address,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if path == "/health" {
				_, _ = io.WriteString(w, `{"ok":true,"service":"cpa-manager"}`)
				return
			}
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		}),
	}
	go func() { _ = server.ListenAndServe() }()
	time.Sleep(10 * time.Second)
	_ = server.Close()
}

func copyTestBinary(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func containsArgument(arguments []string, want string) bool {
	for _, argument := range arguments {
		if argument == want {
			return true
		}
	}
	return false
}

func freeLocalAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate local address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close local listener: %v", err)
	}
	return address
}
