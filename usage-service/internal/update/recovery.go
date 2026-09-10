package update

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RecoveryOptions struct {
	StatusPath              string
	ManagerExecutablePath   string
	ManagerWorkingDirectory string
	ManagerArguments        []string
	ManagerPID              int
}

// RecoverInterruptedUpdate restores the files recorded by an applying
// transaction after the detached updater stopped before recording a terminal
// state. The caller must invoke this before starting the normal manager.
func RecoverInterruptedUpdate(options RecoveryOptions) error {
	if strings.TrimSpace(options.StatusPath) == "" {
		return errors.New("recovery status path is empty")
	}
	status, ok, err := ReadPersistedStatus(options.StatusPath)
	if err != nil {
		return err
	}
	if !ok || status.State != StageApplying || len(status.Backups) == 0 {
		return nil
	}
	lock, err := acquireTransactionLock(options.StatusPath, "recovery-"+newTransactionID(), 0)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	status, ok, err = ReadPersistedStatus(options.StatusPath)
	if err != nil {
		return err
	}
	if !ok || status.State != StageApplying || len(status.Backups) == 0 {
		return nil
	}
	if options.ManagerPID > 0 {
		if err := waitForProcessExit(options.ManagerPID, options.ManagerExecutablePath, transactionLockWait); err != nil {
			if terminateErr := terminateProcessByPID(options.ManagerPID, options.ManagerExecutablePath); terminateErr != nil {
				return fmt.Errorf("wait for interrupted CPA-Manager: %w; terminate failed: %v", err, terminateErr)
			}
		}
	}

	skipPath, _ := os.Executable()
	if err := rollbackPersistedBackupsExcept(status.Backups, skipPath); err != nil {
		status.State = StageFailed
		status.Error = fmt.Sprintf("interrupted update rollback failed: %v", err)
		status.CompletedAtMS = nowUnixMilli()
		if persistErr := WritePersistedStatusOwned(options.StatusPath, status); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return err
	}
	if status.StagingPath != "" {
		if err := removeStagingPath(status.StagingPath); err != nil {
			return fmt.Errorf("remove interrupted update staging: %w", err)
		}
	}
	status.Backups = nil
	status.StagingPath = ""
	status.Error = "interrupted update was rolled back before CPA-Manager restarted"
	status.CompletedAtMS = nowUnixMilli()
	if strings.TrimSpace(options.ManagerExecutablePath) == "" {
		restartErr := errors.New("CPA-Manager executable path is empty after rollback")
		status.State = StageFailed
		status.Error = restartErr.Error()
		if persistErr := WritePersistedStatusOwned(options.StatusPath, status); persistErr != nil {
			return errors.Join(restartErr, persistErr)
		}
		return restartErr
	}
	managerCmd := exec.Command(options.ManagerExecutablePath, options.ManagerArguments...)
	configureProcessGroup(managerCmd)
	managerCmd.Dir = options.ManagerWorkingDirectory
	if strings.TrimSpace(managerCmd.Dir) == "" {
		managerCmd.Dir = filepath.Dir(options.ManagerExecutablePath)
	}
	if err := managerCmd.Start(); err != nil {
		status.State = StageFailed
		status.Error = fmt.Sprintf("restart CPA-Manager after recovery: %v", err)
		if persistErr := WritePersistedStatusOwned(options.StatusPath, status); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return fmt.Errorf("restart CPA-Manager after recovery: %w", err)
	}
	if err := managerCmd.Process.Release(); err != nil {
		status.State = StageFailed
		status.Error = fmt.Sprintf("release recovered CPA-Manager process: %v", err)
		if persistErr := WritePersistedStatusOwned(options.StatusPath, status); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return fmt.Errorf("release recovered CPA-Manager process: %w", err)
	}
	status.State = StageRolledBack
	return WritePersistedStatusOwned(options.StatusPath, status)
}

func nowUnixMilli() int64 {
	return time.Now().UnixMilli()
}

// WatchAndResolvePendingStatus runs in the background of a manager that
// started while an update helper still held the transaction lock. It polls the
// persisted status until the helper exits (or records a terminal state) and
// then resolves a dangling applying transaction so the UI observes a terminal
// result instead of staying stuck on "applying". It never rolls back files of
// an active helper; when the helper dies without a result it marks the
// transaction failed and cleans up the staging directory, leaving any
// half-replaced executables to be corrected by the next update or a later
// startup recovery.
func WatchAndResolvePendingStatus(statusPath string, stop <-chan struct{}) {
	if strings.TrimSpace(statusPath) == "" {
		return
	}
	deadline := time.Now().Add(transactionLockWait + 30*time.Second)
	for {
		status, ok, err := ReadPersistedStatus(statusPath)
		if err != nil || !ok {
			return
		}
		if isTerminalStage(status.State) {
			return
		}
		active, err := TransactionActive(statusPath)
		if err != nil {
			if !time.Now().Before(deadline) {
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		if active {
			if !time.Now().Before(deadline) {
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		break
	}

	// The helper is gone and no terminal state was recorded. Resolve the
	// dangling transaction: roll back any recorded backups, clean up staging,
	// and mark the transaction failed so subsequent polls return a terminal
	// result. Rollback errors are logged but do not abort resolution; files are
	// revalidated by the next startup or update.
	status, ok, err := ReadPersistedStatus(statusPath)
	if err != nil || !ok {
		return
	}
	if status.State == StageApplying && len(status.Backups) > 0 {
		if rollbackErr := rollbackPersistedBackups(status.Backups); rollbackErr != nil {
			log.Printf("resolve pending update: rollback failed: %v", rollbackErr)
		} else {
			status.Backups = nil
		}
	}
	if status.StagingPath != "" {
		if err := removeStagingPath(status.StagingPath); err != nil {
			log.Printf("resolve pending update: staging cleanup failed: %v", err)
		} else {
			status.StagingPath = ""
		}
	}
	status.State = StageFailed
	status.Error = "update helper exited without a result; transaction resolved after restart"
	status.CompletedAtMS = nowUnixMilli()
	if err := WritePersistedStatus(statusPath, status); err != nil {
		log.Printf("resolve pending update: persist failed: %v", err)
	}
}
