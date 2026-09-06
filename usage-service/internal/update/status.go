package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PersistedStatus contains only the update lifecycle information needed after
// CPA-Manager is restarted. It intentionally excludes executable paths and
// download URLs.
type PersistedBackup struct {
	Target string `json:"target"`
	Backup string `json:"backup,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type PersistedStatus struct {
	State          string            `json:"state"`
	OS             string            `json:"os,omitempty"`
	Arch           string            `json:"arch,omitempty"`
	TransactionID  string            `json:"transactionId,omitempty"`
	StagingPath    string            `json:"stagingPath,omitempty"`
	CPAVersion     string            `json:"cpaVersion,omitempty"`
	ManagerVersion string            `json:"managerVersion,omitempty"`
	Backups        []PersistedBackup `json:"backups,omitempty"`
	Error          string            `json:"error,omitempty"`
	StartedAtMS    int64             `json:"startedAtMs,omitempty"`
	CompletedAtMS  int64             `json:"completedAtMs,omitempty"`
}

const (
	StageSucceeded  = "succeeded"
	StageRolledBack = "rolled_back"
)

type transactionLock struct {
	path        string
	file        *os.File
	transferred bool
}

type transactionLockOwner struct {
	PID            int    `json:"pid"`
	TransactionID  string `json:"transactionId"`
	ExecutablePath string `json:"executablePath,omitempty"`
}

const transactionLockWait = 30 * time.Second

func acquireTransactionLock(statusPath, transactionID string, wait time.Duration) (*transactionLock, error) {
	if strings.TrimSpace(statusPath) == "" {
		return nil, nil
	}
	lockPath := updateLockPath(statusPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create update lock directory: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			executablePath, _ := os.Executable()
			owner, marshalErr := json.Marshal(transactionLockOwner{PID: os.Getpid(), TransactionID: transactionID, ExecutablePath: executablePath})
			if marshalErr == nil {
				if _, writeErr := file.Write(owner); writeErr == nil {
					if syncErr := file.Sync(); syncErr == nil {
						return &transactionLock{path: lockPath, file: file}, nil
					}
				}
			}
			_ = file.Close()
			_ = os.Remove(lockPath)
			return nil, errors.New("write update lock owner")
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create update lock: %w", err)
		}

		owner, ownerOK, ownerErr := readTransactionLockOwner(lockPath)
		if ownerErr != nil && !os.IsNotExist(ownerErr) {
			return nil, ownerErr
		}
		stale := false
		if !ownerOK {
			if info, statErr := os.Stat(lockPath); statErr == nil {
				stale = time.Since(info.ModTime()) > transactionLockWait
			} else if os.IsNotExist(statErr) {
				continue
			} else {
				return nil, fmt.Errorf("stat update lock: %w", statErr)
			}
		} else {
			running, runningErr := processIsRunning(owner.PID)
			if runningErr != nil {
				return nil, runningErr
			}
			stale = !running
			if running && owner.ExecutablePath != "" {
				matches, matchErr := processMatchesExecutable(owner.PID, owner.ExecutablePath)
				if matchErr != nil {
					return nil, matchErr
				}
				stale = !matches
			}
		}
		if stale {
			if reclaimErr := reclaimStaleTransactionLock(lockPath); reclaimErr != nil {
				if os.IsNotExist(reclaimErr) {
					continue
				}
				return nil, reclaimErr
			}
			continue
		}
		if wait <= 0 || !time.Now().Before(deadline) {
			if ownerOK && owner.TransactionID != "" {
				return nil, fmt.Errorf("update transaction %s is already active", owner.TransactionID)
			}
			return nil, errors.New("another update operation is already active")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func readTransactionLockOwner(path string) (transactionLockOwner, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return transactionLockOwner{}, false, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return transactionLockOwner{}, false, fmt.Errorf("read update lock: %w", err)
	}
	var owner transactionLockOwner
	if err := json.Unmarshal(data, &owner); err != nil || owner.PID <= 0 {
		return transactionLockOwner{}, false, nil
	}
	return owner, true, nil
}

func (l *transactionLock) Transfer(pid int, transactionID, executablePath string) error {
	if l == nil {
		return errors.New("update transaction lock is unavailable")
	}
	if pid <= 0 || strings.TrimSpace(transactionID) == "" {
		return errors.New("update transaction lock transfer identity is invalid")
	}
	if l.file == nil {
		return errors.New("update transaction lock is already closed")
	}
	if strings.TrimSpace(executablePath) == "" {
		executablePath, _ = os.Executable()
	}
	owner, err := json.Marshal(transactionLockOwner{PID: pid, TransactionID: transactionID, ExecutablePath: executablePath})
	if err != nil {
		return fmt.Errorf("encode transferred update lock owner: %w", err)
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek update lock: %w", err)
	}
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate update lock: %w", err)
	}
	if _, err := l.file.Write(owner); err != nil {
		return fmt.Errorf("write transferred update lock owner: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync transferred update lock owner: %w", err)
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close transferred update lock: %w", err)
	}
	l.file = nil
	l.transferred = true
	return nil
}

func adoptTransactionLock(statusPath, transactionID string) (*transactionLock, error) {
	if strings.TrimSpace(statusPath) == "" {
		return nil, nil
	}
	lockPath := updateLockPath(statusPath)
	deadline := time.Now().Add(transactionLockWait)
	for {
		owner, ok, err := readTransactionLockOwner(lockPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read transferred update lock: %w", err)
		}
		if ok && owner.PID == os.Getpid() && owner.TransactionID == transactionID {
			if owner.ExecutablePath != "" {
				matches, matchErr := processMatchesExecutable(owner.PID, owner.ExecutablePath)
				if matchErr != nil {
					return nil, matchErr
				}
				if !matches {
					return nil, errors.New("transferred update lock executable identity mismatch")
				}
			}
			return &transactionLock{path: lockPath}, nil
		}
		if !time.Now().Before(deadline) {
			return nil, errors.New("transferred update lock was not assigned to this updater")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *transactionLock) Release() error {
	if l == nil || l.transferred {
		return nil
	}
	var releaseErr error
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			releaseErr = err
		}
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) && releaseErr == nil {
		releaseErr = err
	}
	return releaseErr
}

func updateLockPath(statusPath string) string {
	if strings.TrimSpace(statusPath) == "" {
		return ""
	}
	return statusPath + ".lock"
}

// StatusPath returns the sidecar path used to retain the last update result.
func StatusPath(dbPath string) string {
	if strings.TrimSpace(dbPath) == "" {
		return ""
	}
	return dbPath + ".update.json"
}

func WritePersistedStatus(path string, status PersistedStatus) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create update status directory: %w", err)
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update status: %w", err)
	}
	data = append(data, '\n')
	temporary := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write update status: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit update status: %w", err)
	}
	return nil
}

func WritePersistedStatusOwned(path string, status PersistedStatus) error {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(status.TransactionID) == "" {
		return WritePersistedStatus(path, status)
	}
	if err := verifyTransactionLockOwner(path, status.TransactionID); err != nil {
		return err
	}
	writeLock, err := acquireTransactionLock(path+".status-write", status.TransactionID, transactionLockWait)
	if err != nil {
		return err
	}
	defer func() { _ = writeLock.Release() }()
	if err := verifyTransactionLockOwner(path, status.TransactionID); err != nil {
		return err
	}

	existing, ok, err := ReadPersistedStatus(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if ok && existing.TransactionID != "" && existing.TransactionID != status.TransactionID {
		if existing.State == StageDownloading || existing.State == StageReady || existing.State == StageApplying || isTerminalStage(status.State) {
			return fmt.Errorf("update transaction %s no longer owns status file", status.TransactionID)
		}
	}
	return WritePersistedStatus(path, status)
}

func verifyTransactionLockOwner(statusPath, transactionID string) error {
	lockPath := updateLockPath(statusPath)
	owner, ok, err := readTransactionLockOwner(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			status, statusOK, statusErr := ReadPersistedStatus(statusPath)
			if statusErr == nil && statusOK && status.TransactionID == transactionID && status.State == StageApplying {
				return nil
			}
			if statusErr != nil && !os.IsNotExist(statusErr) {
				return fmt.Errorf("read persisted update status: %w", statusErr)
			}
			return fmt.Errorf("update transaction lock is missing for %s", transactionID)
		}
		return fmt.Errorf("read update transaction lock: %w", err)
	}
	if !ok {
		return errors.New("update transaction lock owner is invalid")
	}
	if owner.PID != os.Getpid() {
		return fmt.Errorf("update transaction %s is owned by process %d", owner.TransactionID, owner.PID)
	}
	if owner.TransactionID != transactionID && owner.TransactionID != "recovery" && !strings.HasPrefix(owner.TransactionID, "recovery-") {
		return fmt.Errorf("update transaction %s no longer owns status file", transactionID)
	}
	if owner.ExecutablePath != "" {
		matches, matchErr := processMatchesExecutable(owner.PID, owner.ExecutablePath)
		if matchErr != nil {
			return matchErr
		}
		if !matches {
			return errors.New("update transaction lock executable identity mismatch")
		}
	}
	return nil
}

func reclaimStaleTransactionLock(lockPath string) error {
	reclaimPath := fmt.Sprintf("%s.reclaim-%d", lockPath, time.Now().UnixNano())
	if err := os.Rename(lockPath, reclaimPath); err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return fmt.Errorf("reclaim stale update lock: %w", err)
	}
	if err := os.Remove(reclaimPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove reclaimed update lock: %w", err)
	}
	return nil
}

// TransactionActive reports whether the persisted transaction lock is owned by
// a live process. It is used during manager startup to avoid racing a
// detached updater.
func TransactionActive(statusPath string) (bool, error) {
	if strings.TrimSpace(statusPath) == "" {
		return false, nil
	}
	owner, ok, err := readTransactionLockOwner(updateLockPath(statusPath))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !ok {
		return false, nil
	}
	running, err := processIsRunning(owner.PID)
	if err != nil || !running {
		return false, err
	}
	if owner.ExecutablePath == "" {
		return true, nil
	}
	matches, err := processMatchesExecutable(owner.PID, owner.ExecutablePath)
	if err != nil {
		return false, err
	}
	return matches, nil
}

func ReadPersistedStatus(path string) (PersistedStatus, bool, error) {
	if strings.TrimSpace(path) == "" {
		return PersistedStatus{}, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PersistedStatus{}, false, nil
	}
	if err != nil {
		return PersistedStatus{}, false, err
	}
	var status PersistedStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return PersistedStatus{}, false, err
	}
	if strings.TrimSpace(status.State) == "" {
		return PersistedStatus{}, false, errors.New("update status has no state")
	}
	return status, true, nil
}
