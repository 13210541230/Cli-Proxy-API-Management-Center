package update

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestApplyKeepsRealFailureReason verifies that an Apply failure that happens
// before any replacement records the real failure reason in the sidecar
// instead of the generic "helper exited" fallback text.
func TestApplyKeepsRealFailureReason(t *testing.T) {
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "status.update.json")
	options := ApplyOptions{
		StagingPath:   filepath.Join(dir, "staging"),
		ResultPath:    resultPath,
		TransactionID: "txn-real-error",
		// An empty manager path makes Apply fail immediately at validation,
		// before any process or file is touched.
	}
	err := Apply(options)
	if err == nil {
		t.Fatal("Apply should fail when manager executable path is empty")
	}
	if !strings.Contains(err.Error(), "manager and CPA executable paths are required") {
		t.Fatalf("Apply error should carry the real reason, got: %v", err)
	}
	status, ok, readErr := ReadPersistedStatus(resultPath)
	if readErr != nil || !ok {
		t.Fatalf("sidecar should be persisted: ok=%v err=%v", ok, readErr)
	}
	if status.State != StageFailed {
		t.Fatalf("sidecar state should be failed, got %q", status.State)
	}
	if !strings.Contains(status.Error, "manager and CPA executable paths are required") {
		t.Fatalf("sidecar should carry the real error, got %q", status.Error)
	}
}

// TestWatchAndResolvePendingStatusResolvesDeadHelper simulates a manager that
// started while an applying transaction belonged to a helper which has since
// died. The background watcher must resolve the dangling transaction to failed
// and clean up the staging directory.
func TestWatchAndResolvePendingStatusResolvesDeadHelper(t *testing.T) {
	dir := t.TempDir()
	// Prepare a staging file so removeStagingPath has something to clean. The
	// staging root must live directly under os.TempDir() with the managed
	// prefix for removeStagingPath's safety validation to accept it.
	stagingRoot := filepath.Join(os.TempDir(), "cpa-manager-update-test-watcher")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stagingRoot) })
	staging := filepath.Join(stagingRoot, "extracted")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(dir, "status.update.json")
	transactionID := "txn-watcher"
	if err := WritePersistedStatus(statusPath, PersistedStatus{
		State:         StageApplying,
		OS:            "windows",
		Arch:          "amd64",
		TransactionID: transactionID,
		StagingPath:   staging,
		Backups:       nil,
	}); err != nil {
		t.Fatal(err)
	}
	// Create a transaction lock owned by a PID that is guaranteed not to be
	// running so TransactionActive reports false.
	deadOwner := transactionLockOwner{PID: 268435455, TransactionID: transactionID}
	if err := writeLockOwner(updateLockPath(statusPath), deadOwner); err != nil {
		t.Fatal(err)
	}

	WatchAndResolvePendingStatus(statusPath, nil)

	status, ok, err := ReadPersistedStatus(statusPath)
	if err != nil || !ok {
		t.Fatalf("sidecar should remain: ok=%v err=%v", ok, err)
	}
	if !isTerminalStage(status.State) {
		t.Fatalf("watcher should resolve to a terminal state, got %q", status.State)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging directory should be cleaned up, stat err = %v", err)
	}
}

// TestWatchAndResolvePendingStatusWaitsWhileHelperActive verifies the watcher
// does not resolve a transaction while its owning helper process is alive.
func TestWatchAndResolvePendingStatusWaitsWhileHelperActive(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "status.update.json")
	transactionID := "txn-watcher-active"

	// Spawn a long-running child to act as the still-alive helper owner.
	helper := exec.Command(os.Args[0], "-test.run=TestHelperProcessStub")
	helper.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = helper.Process.Kill() }()

	if err := WritePersistedStatus(statusPath, PersistedStatus{
		State:         StageApplying,
		OS:            "windows",
		Arch:          "amd64",
		TransactionID: transactionID,
		StagingPath:   "",
	}); err != nil {
		t.Fatal(err)
	}
	helperExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLockOwner(updateLockPath(statusPath), transactionLockOwner{
		PID:            helper.Process.Pid,
		TransactionID:  transactionID,
		ExecutablePath: helperExecutable,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		WatchAndResolvePendingStatus(statusPath, ctx.Done())
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("watcher returned while the helper was still alive")
	case <-time.After(700 * time.Millisecond):
	}

	status, ok, err := ReadPersistedStatus(statusPath)
	if err != nil || !ok {
		t.Fatalf("sidecar should remain readable: ok=%v err=%v", ok, err)
	}
	if status.State != StageApplying {
		t.Fatalf("state should still be applying while helper is alive, got %q", status.State)
	}
}

// HelperProcessStub is used as a long-running child helper for
// TestWatchAndResolvePendingStatusWaitsWhileHelperActive.
func TestHelperProcessStub(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	time.Sleep(10 * time.Second)
}

func writeLockOwner(path string, owner transactionLockOwner) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}
