package update

import (
	"errors"
	"fmt"
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
