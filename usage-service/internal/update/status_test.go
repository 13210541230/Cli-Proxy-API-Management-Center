package update

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPersistedStatusRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	want := PersistedStatus{
		State:          StageSucceeded,
		OS:             "windows",
		Arch:           "amd64",
		CPAVersion:     "7.2.146",
		ManagerVersion: "7.2.146",
		StartedAtMS:    10,
		CompletedAtMS:  20,
	}
	if err := WritePersistedStatus(path, want); err != nil {
		t.Fatalf("WritePersistedStatus() error = %v", err)
	}
	got, ok, err := ReadPersistedStatus(path)
	if err != nil || !ok {
		t.Fatalf("ReadPersistedStatus() = %#v, %v, %v", got, ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
}

func TestRecoverInterruptedUpdateRestoresPersistedBackups(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "update.json")
	target := filepath.Join(dir, "cpa-manager")
	backup := target + ".update-backup-1"
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	originalHash, err := fileSHA256(backup)
	if err != nil {
		t.Fatalf("hash backup: %v", err)
	}
	if err := WritePersistedStatus(statusPath, PersistedStatus{
		State:         StageApplying,
		TransactionID: "txn-1",
		Backups:       []PersistedBackup{{Target: target, Backup: backup, SHA256: originalHash}},
	}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	if err := RecoverInterruptedUpdate(RecoveryOptions{StatusPath: statusPath}); err == nil {
		t.Fatal("RecoverInterruptedUpdate() unexpectedly reported a successful recovery without a manager path")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read restored target: %v", err)
	}
	if string(content) != "old" {
		t.Fatalf("restored target = %q, want old", content)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists, stat error = %v", err)
	}
	status, ok, err := ReadPersistedStatus(statusPath)
	if err != nil || !ok {
		t.Fatalf("read recovered status = %#v, %v, %v", status, ok, err)
	}
	if status.State != StageFailed || status.Backups != nil {
		t.Fatalf("recovered status = %#v", status)
	}
}

func TestRecoverInterruptedUpdateRecordsRestartFailure(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "update.json")
	target := filepath.Join(dir, "cpa-manager")
	backup := target + ".update-backup-1"
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := WritePersistedStatus(statusPath, PersistedStatus{
		State:         StageApplying,
		TransactionID: "txn-restart-failure",
		Backups:       []PersistedBackup{{Target: target, Backup: backup}},
	}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	missingManager := filepath.Join(dir, "missing-cpa-manager")
	if err := RecoverInterruptedUpdate(RecoveryOptions{
		StatusPath:            statusPath,
		ManagerExecutablePath: missingManager,
	}); err == nil {
		t.Fatal("RecoverInterruptedUpdate() unexpectedly succeeded")
	}
	status, ok, readErr := ReadPersistedStatus(statusPath)
	if readErr != nil || !ok {
		t.Fatalf("read failed recovery status = %#v, %v, %v", status, ok, readErr)
	}
	if status.State != StageFailed || status.Error == "" {
		t.Fatalf("failed recovery status = %#v", status)
	}
}

func TestWritePersistedStatusOwnedRejectsStaleTerminalResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{State: StageFailed, TransactionID: "new-owner"}); err != nil {
		t.Fatalf("write initial status: %v", err)
	}
	staleLock, err := acquireTransactionLock(path, "stale-owner", 0)
	if err != nil {
		t.Fatalf("acquire stale owner lock: %v", err)
	}
	if err := WritePersistedStatusOwned(path, PersistedStatus{State: StageSucceeded, TransactionID: "stale-owner"}); err == nil {
		_ = staleLock.Release()
		t.Fatal("WritePersistedStatusOwned() accepted stale terminal result")
	}
	if err := staleLock.Release(); err != nil {
		t.Fatalf("release stale owner lock: %v", err)
	}
	replacementLock, err := acquireTransactionLock(path, "replacement-owner", 0)
	if err != nil {
		t.Fatalf("acquire replacement owner lock: %v", err)
	}
	if err := WritePersistedStatusOwned(path, PersistedStatus{State: StageDownloading, TransactionID: "replacement-owner"}); err != nil {
		_ = replacementLock.Release()
		t.Fatalf("WritePersistedStatusOwned() rejected a new transaction: %v", err)
	}
	if err := replacementLock.Release(); err != nil {
		t.Fatalf("release replacement owner lock: %v", err)
	}
}

func TestWritePersistedStatusOwnedAllowsMatchingApplyingRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{State: StageApplying, TransactionID: "txn-1"}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	if err := WritePersistedStatusOwned(path, PersistedStatus{State: StageFailed, TransactionID: "txn-1", Error: "helper failed before lock adoption"}); err != nil {
		t.Fatalf("WritePersistedStatusOwned() rejected matching recovery: %v", err)
	}
	status, ok, err := ReadPersistedStatus(path)
	if err != nil || !ok || status.State != StageFailed || status.TransactionID != "txn-1" {
		t.Fatalf("recovered status = %#v, %v, %v", status, ok, err)
	}
}

func TestStagerCleansStaleApplyingUpdate(t *testing.T) {
	root, err := os.MkdirTemp("", "cpa-manager-update-")
	if err != nil {
		t.Fatalf("create staging root: %v", err)
	}
	extracted := filepath.Join(root, "extracted")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		t.Fatalf("create extracted path: %v", err)
	}
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{State: StageApplying, StagingPath: extracted, TransactionID: "stale", StartedAtMS: 10}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	stager := NewStager(nil, path)
	status := stager.Status()
	if status.State != StageFailed || status.Error == "" {
		t.Fatalf("recovered status = %#v, want failed stale update", status)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("stale staging root still exists, stat error = %v", err)
	}
}

func TestStagerCleansCompletedUpdateArtifacts(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cpa-manager")
	backup := target + ".update-backup-test"
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	stagingRoot, err := os.MkdirTemp("", "cpa-manager-update-")
	if err != nil {
		t.Fatalf("create staging root: %v", err)
	}
	stagingPath := filepath.Join(stagingRoot, "extracted")
	if err := os.Mkdir(stagingPath, 0o700); err != nil {
		t.Fatalf("create staging path: %v", err)
	}
	statusPath := filepath.Join(dir, "update.json")
	if err := WritePersistedStatus(statusPath, PersistedStatus{
		State:         StageSucceeded,
		TransactionID: "completed-transaction",
		StagingPath:   stagingPath,
		Backups:       []PersistedBackup{{Target: target, Backup: backup}},
		CompletedAtMS: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("write completed status: %v", err)
	}

	stager := NewStager(nil, statusPath)
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup still exists, stat error = %v", err)
	}
	if _, err := os.Stat(stagingRoot); !os.IsNotExist(err) {
		t.Fatalf("staging root still exists, stat error = %v", err)
	}
	status, ok, err := ReadPersistedStatus(statusPath)
	if err != nil || !ok {
		t.Fatalf("read cleaned status = %#v, %v, %v", status, ok, err)
	}
	if len(status.Backups) != 0 || status.StagingPath != "" {
		t.Fatalf("cleaned status = %#v", status)
	}
	if stager.Status().State != StageSucceeded {
		t.Fatalf("stager state = %q, want succeeded", stager.Status().State)
	}
}

func TestStagerPreservesApplyingStatusWhileUpdaterLockIsActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{
		State:         StageApplying,
		TransactionID: "active-helper",
		StartedAtMS:   time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("write applying status: %v", err)
	}
	lock, err := acquireTransactionLock(path, "active-helper", 0)
	if err != nil {
		t.Fatalf("acquire active helper lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	stager := NewStager(nil, path)
	status := stager.Status()
	if status.State != StageApplying || status.TransactionID != "active-helper" {
		t.Fatalf("stager status = %#v, want active applying transaction", status)
	}
	persisted, ok, err := ReadPersistedStatus(path)
	if err != nil || !ok || persisted.State != StageApplying || persisted.TransactionID != "active-helper" {
		t.Fatalf("persisted status = %#v, %v, %v", persisted, ok, err)
	}
}

func TestStagerDoesNotRestoreReadyWithoutStagingPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{
		State:       StageReady,
		CPAVersion:  "7.2.146",
		StartedAtMS: 10,
	}); err != nil {
		t.Fatalf("WritePersistedStatus() error = %v", err)
	}
	stager := NewStager(nil, path)
	status := stager.Status()
	if status.State != StageFailed || status.Error == "" {
		t.Fatalf("recovered status = %#v", status)
	}
}

func TestTransactionLockTransferAndAdoption(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "update.json")
	lock, err := acquireTransactionLock(statusPath, "manager", 0)
	if err != nil {
		t.Fatalf("acquire manager lock: %v", err)
	}
	if err := lock.Transfer(os.Getpid(), "helper", ""); err != nil {
		t.Fatalf("transfer lock: %v", err)
	}
	adopted, err := adoptTransactionLock(statusPath, "helper")
	if err != nil {
		t.Fatalf("adopt transferred lock: %v", err)
	}
	if err := adopted.Release(); err != nil {
		t.Fatalf("release adopted lock: %v", err)
	}
}

func TestTransactionLockSerializesUpdateOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	first, err := acquireTransactionLock(path, "first", 0)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer func() { _ = first.Release() }()
	if _, err := acquireTransactionLock(path, "second", 0); err == nil {
		t.Fatal("acquireTransactionLock() accepted a concurrent owner")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first lock: %v", err)
	}
	second, err := acquireTransactionLock(path, "second", 0)
	if err != nil {
		t.Fatalf("acquire second lock after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second lock: %v", err)
	}
}

func TestStagerRefreshesTerminalHelperResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	if err := WritePersistedStatus(path, PersistedStatus{
		State:       StageApplying,
		CPAVersion:  "7.2.146",
		StartedAtMS: 10,
	}); err != nil {
		t.Fatalf("WritePersistedStatus() error = %v", err)
	}

	lock, err := acquireTransactionLock(path, "active-helper", 0)
	if err != nil {
		t.Fatalf("acquire active helper lock: %v", err)
	}
	stager := NewStager(nil, path)
	if status := stager.Status(); status.State != StageApplying || status.Error != "" {
		_ = lock.Release()
		t.Fatalf("initial status = %#v, want applying status preserved while helper may be active", status)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release active helper lock: %v", err)
	}
	if err := WritePersistedStatus(path, PersistedStatus{
		State:          StageSucceeded,
		OS:             "windows",
		Arch:           "amd64",
		CPAVersion:     "7.2.146",
		ManagerVersion: "7.2.146",
		StartedAtMS:    10,
		CompletedAtMS:  time.Now().Add(time.Second).UnixMilli(),
	}); err != nil {
		t.Fatalf("WritePersistedStatus() error = %v", err)
	}
	status := stager.Status()
	if status.State != StageSucceeded || status.OS != "windows" || status.Arch != "amd64" || status.Manifest.CPAVersion != "7.2.146" {
		t.Fatalf("refreshed status = %#v", status)
	}
}
