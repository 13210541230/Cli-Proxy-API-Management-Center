package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ApplyOptions struct {
	StagingPath             string
	ResultPath              string
	OS                      string
	Arch                    string
	TransactionID           string
	CPAVersion              string
	ManagerVersion          string
	ManagerExecutablePath   string
	ManagerWorkingDirectory string
	CPAExecutablePath       string
	UpdaterExecutablePath   string
	ManagerArguments        []string
	CPAArguments            []string
	CPAWorkingDirectory     string
	ManagerHealthURL        string
	CPAHealthURL            string
	ManagerPID              int
	PreviousCPAWasRunning   bool
	RestoreOnFailure        bool
	StartCPA                bool
	ManagerStartCPA         bool
	OwnsTransactionLock     bool
}

func StartHelper(helperPath string, options ApplyOptions) (*exec.Cmd, error) {
	if strings.TrimSpace(helperPath) == "" {
		return nil, errors.New("update helper path is empty")
	}
	if _, err := os.Stat(helperPath); err != nil {
		return nil, fmt.Errorf("stat update helper: %w", err)
	}
	managerArgs, err := json.Marshal(options.ManagerArguments)
	if err != nil {
		return nil, fmt.Errorf("encode manager arguments: %w", err)
	}
	cpaArgs, err := json.Marshal(options.CPAArguments)
	if err != nil {
		return nil, fmt.Errorf("encode CPA arguments: %w", err)
	}
	files, err := LocateBundle(options.StagingPath)
	if err != nil {
		return nil, fmt.Errorf("locate staged update helper: %w", err)
	}
	launchPath := files.UpdaterPath
	if strings.TrimSpace(launchPath) == "" {
		return nil, errors.New("staged update helper path is empty")
	}
	if strings.TrimSpace(options.UpdaterExecutablePath) == "" {
		options.UpdaterExecutablePath = helperPath
	}
	cmd := exec.Command(launchPath,
		"--staging-path", options.StagingPath,
		"--result-path", options.ResultPath,
		"--os", options.OS,
		"--arch", options.Arch,
		"--transaction-id", options.TransactionID,
		"--cpa-version", options.CPAVersion,
		"--manager-version", options.ManagerVersion,
		"--manager-path", options.ManagerExecutablePath,
		"--manager-working-directory", options.ManagerWorkingDirectory,
		"--cpa-path", options.CPAExecutablePath,
		"--updater-path", options.UpdaterExecutablePath,
		"--manager-args", string(managerArgs),
		"--cpa-args", string(cpaArgs),
		"--cpa-working-directory", options.CPAWorkingDirectory,
		"--manager-health-url", options.ManagerHealthURL,
		"--cpa-health-url", options.CPAHealthURL,
		"--manager-pid", strconv.Itoa(options.ManagerPID),
		"--previous-cpa-running", strconv.FormatBool(options.PreviousCPAWasRunning),
		"--restore-on-failure", strconv.FormatBool(options.RestoreOnFailure),
		"--start-cpa", strconv.FormatBool(options.StartCPA),
		"--manager-start-cpa", strconv.FormatBool(options.ManagerStartCPA),
		"--owns-transaction-lock", strconv.FormatBool(options.OwnsTransactionLock),
	)
	cmd.Dir = filepath.Dir(launchPath)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start update helper: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return nil, fmt.Errorf("release update helper process: %w", err)
	}
	return cmd, nil
}

func Apply(options ApplyOptions) error {
	if strings.TrimSpace(options.StagingPath) == "" {
		return errors.New("update staging path is empty")
	}
	if strings.TrimSpace(options.ManagerExecutablePath) == "" || strings.TrimSpace(options.CPAExecutablePath) == "" {
		return errors.New("manager and CPA executable paths are required")
	}
	backups := make([]backupFile, 0, 3)
	startedAt := time.Now().UnixMilli()
	resultRecorded := false
	preserveApplyingOnExit := false
	persistApplying := func() error {
		return WritePersistedStatusOwned(options.ResultPath, PersistedStatus{
			State:          StageApplying,
			OS:             options.OS,
			Arch:           options.Arch,
			TransactionID:  options.TransactionID,
			CPAVersion:     options.CPAVersion,
			ManagerVersion: options.ManagerVersion,
			StagingPath:    options.StagingPath,
			Backups:        persistedBackups(backups),
			StartedAtMS:    startedAt,
		})
	}
	recordResult := func(state string, resultErr error) error {
		if err := WritePersistedStatusOwned(options.ResultPath, PersistedStatus{
			State:          state,
			OS:             options.OS,
			Arch:           options.Arch,
			TransactionID:  options.TransactionID,
			CPAVersion:     options.CPAVersion,
			ManagerVersion: options.ManagerVersion,
			StagingPath:    options.StagingPath,
			Backups:        persistedBackups(backups),
			Error:          errorString(resultErr),
			StartedAtMS:    startedAt,
			CompletedAtMS:  time.Now().UnixMilli(),
		}); err == nil {
			resultRecorded = true
			return nil
		} else {
			log.Printf("update status persist failed: %v", err)
			return err
		}
	}
	defer func() {
		if !resultRecorded && !preserveApplyingOnExit {
			_ = recordResult(StageFailed, errors.New("update helper exited before recording a result"))
		}
		if err := removeStagingPath(options.StagingPath); err != nil {
			log.Printf("update staging cleanup failed: %v", err)
		}
	}()
	var applyLock *transactionLock
	var err error
	if options.OwnsTransactionLock {
		applyLock, err = adoptTransactionLock(options.ResultPath, options.TransactionID)
	} else {
		applyLock, err = acquireTransactionLock(options.ResultPath, options.TransactionID, transactionLockWait)
	}
	if err != nil {
		return err
	}
	defer func() {
		if err := applyLock.Release(); err != nil {
			log.Printf("release update apply lock: %v", err)
		}
	}()
	rollback := func() error {
		return rollbackPersistedBackups(persistedBackups(backups))
	}
	files, err := LocateBundle(options.StagingPath)
	if err != nil {
		return recoverApplyFailure(options, rollback, recordResult, err)
	}
	if options.ManagerPID > 0 {
		if err := waitForProcessExit(options.ManagerPID, options.ManagerExecutablePath, 30*time.Second); err != nil {
			return recoverApplyFailure(options, rollback, recordResult, err)
		}
	}
	type replacement struct {
		source string
		target string
		label  string
	}
	replacements := []replacement{
		{source: files.CPAPath, target: options.CPAExecutablePath, label: "CLIProxyAPI"},
		{source: files.ManagerPath, target: options.ManagerExecutablePath, label: "CPA-Manager"},
	}
	// The installed helper relays to the staged helper before Apply runs, so the
	// staged process can replace the installed updater image safely.
	if strings.TrimSpace(options.UpdaterExecutablePath) != "" && !isCurrentExecutable(options.UpdaterExecutablePath) {
		replacements = append(replacements, replacement{source: files.UpdaterPath, target: options.UpdaterExecutablePath, label: "cpa-updater"})
	}
	for _, item := range replacements {
		planned, planErr := planBackup(item.target)
		if planErr != nil {
			return recoverApplyFailure(options, rollback, recordResult, fmt.Errorf("plan %s replacement: %w", item.label, planErr))
		}
		backups = append(backups, planned)
	}
	if err := persistApplying(); err != nil {
		return recoverApplyFailure(options, rollback, recordResult, fmt.Errorf("persist replacement plan: %w", err))
	}
	for index, item := range replacements {
		if err := replaceFileWithBackup(item.source, &backups[index]); err != nil {
			return restartAfterRollback(options, rollback, recordResult, fmt.Errorf("replace %s: %w", item.label, err))
		}
		if err := persistApplying(); err != nil {
			return restartAfterRollback(options, rollback, recordResult, fmt.Errorf("persist %s replacement metadata: %w", item.label, err))
		}
	}

	cpaCmd, managerCmd, err := startProcesses(options)
	if err != nil {
		return restartAfterRollback(options, rollback, recordResult, fmt.Errorf("start updated processes: %w", err))
	}
	if err := waitForProcessesHealthy(options, managerCmd, cpaCmd); err != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		return restartAfterRollback(options, rollback, recordResult, fmt.Errorf("updated CPA-Manager health check failed: %w", err))
	}
	// Commit the successful health-checked state before deleting backups. If the
	// updater exits in the cleanup window, the next manager can safely retry the
	// idempotent cleanup without treating the update as interrupted.
	preserveApplyingOnExit = true
	if err := recordResult(StageSucceeded, nil); err != nil {
		return fmt.Errorf("persist successful update status: %w", err)
	}
	preserveApplyingOnExit = false
	cleanupErr := cleanupBackups(backups)
	if cleanupErr == nil {
		backups = nil
		if err := recordResult(StageSucceeded, nil); err != nil {
			log.Printf("persist completed update cleanup status failed: %v", err)
		}
	} else if err := recordResult(StageSucceeded, fmt.Errorf("cleanup update backups: %w", cleanupErr)); err != nil {
		log.Printf("persist completed update cleanup warning failed: %v", err)
	}
	_ = managerCmd.Process.Release()
	if cpaCmd != nil {
		_ = cpaCmd.Process.Release()
	}
	return nil
}

func recoverApplyFailure(options ApplyOptions, rollback func() error, recordResult func(string, error) error, cause error) error {
	if rollbackErr := rollback(); rollbackErr != nil {
		failure := fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
		recordResult(StageFailed, failure)
		return failure
	}
	if !options.RestoreOnFailure {
		recordResult(StageFailed, cause)
		return cause
	}

	managerExited := options.ManagerPID == 0
	if !managerExited {
		managerExited = waitForProcessExit(options.ManagerPID, options.ManagerExecutablePath, 30*time.Second) == nil
	}
	if !managerExited {
		if terminateErr := terminateProcessByPID(options.ManagerPID, options.ManagerExecutablePath); terminateErr == nil {
			managerExited = true
		} else {
			log.Printf("terminate old CPA-Manager during recovery: %v", terminateErr)
		}
	}
	if !managerExited {
		failure := fmt.Errorf("%w; CPA-Manager did not exit for rollback", cause)
		recordResult(StageFailed, failure)
		return failure
	}

	cpaCmd, managerCmd, restartErr := startProcesses(options)
	if restartErr != nil {
		failure := fmt.Errorf("%w; rollback restart failed: %v", cause, restartErr)
		recordResult(StageFailed, failure)
		return failure
	}
	if healthErr := waitForProcessesHealthy(options, managerCmd, cpaCmd); healthErr != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		failure := fmt.Errorf("%w; rollback health check failed: %v", cause, healthErr)
		recordResult(StageFailed, failure)
		return failure
	}
	_ = managerCmd.Process.Release()
	if cpaCmd != nil {
		_ = cpaCmd.Process.Release()
	}
	recordResult(StageRolledBack, cause)
	return cause
}

func startCPAProcess(options ApplyOptions) (*exec.Cmd, error) {
	cmd := exec.Command(options.CPAExecutablePath, options.CPAArguments...)
	configureProcessGroup(cmd)
	cmd.Dir = options.CPAWorkingDirectory
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start CLIProxyAPI: %w", err)
	}
	return cmd, nil
}

func startProcesses(options ApplyOptions) (*exec.Cmd, *exec.Cmd, error) {
	var cpaCmd *exec.Cmd
	// ManagerStartCPA is the authoritative path when the manager owns CPA. Do
	// not also start a direct CPA child, or the update would create duplicates.
	if options.StartCPA && options.ManagerStartCPA {
		options.StartCPA = false
	}
	if options.StartCPA {
		var err error
		cpaCmd, err = startCPAProcess(options)
		if err != nil {
			return nil, nil, err
		}
	}
	managerArguments := append([]string(nil), options.ManagerArguments...)
	if options.ManagerStartCPA {
		managerArguments = append(managerArguments, "--start-cpa")
	}
	managerCmd := exec.Command(options.ManagerExecutablePath, managerArguments...)
	configureProcessGroup(managerCmd)
	managerCmd.Dir = options.ManagerWorkingDirectory
	if strings.TrimSpace(managerCmd.Dir) == "" {
		managerCmd.Dir = filepath.Dir(options.ManagerExecutablePath)
	}
	if err := managerCmd.Start(); err != nil {
		terminateProcess(cpaCmd)
		return nil, nil, fmt.Errorf("start CPA-Manager: %w", err)
	}
	return cpaCmd, managerCmd, nil
}

func restartAfterRollback(options ApplyOptions, rollback func() error, recordResult func(string, error) error, cause error) error {
	if rollbackErr := rollback(); rollbackErr != nil {
		failure := fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
		recordResult(StageFailed, failure)
		return failure
	}
	cpaCmd, managerCmd, restartErr := startProcesses(options)
	if restartErr != nil {
		failure := fmt.Errorf("%w; rollback restart failed: %v", cause, restartErr)
		recordResult(StageFailed, failure)
		return failure
	}
	if healthErr := waitForProcessesHealthy(options, managerCmd, cpaCmd); healthErr != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		failure := fmt.Errorf("%w; rollback health check failed: %v", cause, healthErr)
		recordResult(StageFailed, failure)
		return failure
	}
	_ = managerCmd.Process.Release()
	if cpaCmd != nil {
		_ = cpaCmd.Process.Release()
	}
	recordResult(StageRolledBack, cause)
	return cause
}

type backupFile struct {
	target string
	backup string
	sha256 string
}

func persistedBackups(backups []backupFile) []PersistedBackup {
	if len(backups) == 0 {
		return nil
	}
	result := make([]PersistedBackup, 0, len(backups))
	for _, item := range backups {
		result = append(result, PersistedBackup{Target: item.target, Backup: item.backup, SHA256: item.sha256})
	}
	return result
}

func planBackup(target string) (backupFile, error) {
	target, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return backupFile{}, fmt.Errorf("resolve replacement target: %w", err)
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return backupFile{target: target}, nil
	} else if err != nil {
		return backupFile{}, fmt.Errorf("stat current executable: %w", err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	backup := target + ".update-backup-" + suffix
	originalSHA256, err := fileSHA256(target)
	if err != nil {
		return backupFile{}, fmt.Errorf("hash current executable: %w", err)
	}
	return backupFile{target: target, backup: backup, sha256: originalSHA256}, nil
}

func replaceFileWithBackup(source string, item *backupFile) error {
	if item == nil || item.target == "" {
		return errors.New("replacement target is empty")
	}
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("stat staged executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(item.target), 0o755); err != nil {
		return fmt.Errorf("create executable directory: %w", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	temporary := item.target + ".update-new-" + suffix
	_ = os.Remove(temporary)
	if err := copyFile(source, temporary); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if item.backup == "" {
		if _, err := os.Stat(item.target); err == nil {
			_ = os.Remove(temporary)
			return errors.New("replacement target appeared after planning")
		} else if !os.IsNotExist(err) {
			_ = os.Remove(temporary)
			return fmt.Errorf("recheck current executable: %w", err)
		}
		if err := waitRename(temporary, item.target); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("install replacement executable: %w", err)
		}
		return nil
	}
	if _, err := os.Stat(item.backup); err == nil {
		_ = os.Remove(temporary)
		return errors.New("replacement backup path already exists")
	} else if !os.IsNotExist(err) {
		_ = os.Remove(temporary)
		return fmt.Errorf("check replacement backup: %w", err)
	}
	if err := waitRename(item.target, item.backup); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err := waitRename(temporary, item.target); err != nil {
		_ = os.Rename(item.backup, item.target)
		return fmt.Errorf("install updated executable: %w", err)
	}
	return nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isCurrentExecutable(path string) bool {
	current, err := os.Executable()
	if err != nil {
		return false
	}
	current, err = filepath.Abs(filepath.Clean(current))
	if err != nil {
		return false
	}
	target, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	return strings.EqualFold(current, target)
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged executable: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat staged executable: %w", err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode()&0o777)
	if err != nil {
		return fmt.Errorf("create replacement executable: %w", err)
	}
	_, copyErr := output.ReadFrom(input)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy replacement executable: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close replacement executable: %w", closeErr)
	}
	return nil
}

func waitRename(source, target string) error {
	var lastErr error
	for attempt := 0; attempt < 300; attempt++ {
		if err := os.Rename(source, target); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if runtime.GOOS != "windows" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func waitForProcessesHealthy(options ApplyOptions, commands ...*exec.Cmd) error {
	if err := waitForHealthy(options.ManagerHealthURL, 30*time.Second); err != nil {
		return fmt.Errorf("CPA-Manager health check failed: %w", err)
	}
	if len(commands) > 0 && commands[0] != nil && strings.TrimSpace(options.ManagerExecutablePath) != "" {
		if matches, err := processMatchesExecutable(commands[0].Process.Pid, options.ManagerExecutablePath); err != nil {
			return fmt.Errorf("re-validate CPA-Manager process: %w", err)
		} else if !matches {
			return errors.New("CPA-Manager process changed during health check")
		}
	}
	if strings.TrimSpace(options.CPAHealthURL) != "" {
		if err := waitForHealthy(options.CPAHealthURL, 30*time.Second); err != nil {
			return fmt.Errorf("CLIProxyAPI health check failed: %w", err)
		}
		if len(commands) > 1 && commands[1] != nil && strings.TrimSpace(options.CPAExecutablePath) != "" {
			if matches, err := processMatchesExecutable(commands[1].Process.Pid, options.CPAExecutablePath); err != nil {
				return fmt.Errorf("identify CLIProxyAPI process: %w", err)
			} else if !matches {
				return errors.New("CLIProxyAPI health process identity mismatch")
			}
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rollbackPersistedBackups(backups []PersistedBackup) error {
	return rollbackPersistedBackupsExcept(backups, "")
}

func rollbackPersistedBackupsExcept(backups []PersistedBackup, skipPath string) error {
	var rollbackErr error
	if strings.TrimSpace(skipPath) != "" {
		skipPath, _ = filepath.Abs(filepath.Clean(skipPath))
	}
	for i := len(backups) - 1; i >= 0; i-- {
		item := backups[i]
		target, err := filepath.Abs(filepath.Clean(item.Target))
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("resolve persisted rollback target: %w", err))
			continue
		}
		if skipPath != "" && samePath(target, skipPath) {
			continue
		}
		if item.Backup == "" {
			if err := removeIfExists(target); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove replacement %s: %w", target, err))
			}
			continue
		}
		backup, err := filepath.Abs(filepath.Clean(item.Backup))
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("resolve persisted rollback backup: %w", err))
			continue
		}
		prefix := target + ".update-backup-"
		if runtime.GOOS == "windows" {
			if !strings.HasPrefix(strings.ToLower(backup), strings.ToLower(prefix)) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing rollback backup outside target path: %s", backup))
				continue
			}
		} else if !strings.HasPrefix(backup, prefix) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing rollback backup outside target path: %s", backup))
			continue
		}
		if _, statErr := os.Stat(backup); statErr != nil {
			if os.IsNotExist(statErr) && item.SHA256 != "" {
				if actual, hashErr := fileSHA256(target); hashErr == nil && strings.EqualFold(actual, item.SHA256) {
					continue
				}
			}
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("persisted rollback backup is missing: %s", backup))
			continue
		}
		if err := removeIfExists(target); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove replacement %s: %w", target, err))
			continue
		}
		if err := waitRename(backup, target); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore backup %s: %w", target, err))
		}
	}
	return rollbackErr
}

func cleanupBackups(backups []backupFile) error {
	var cleanupErr error
	for _, item := range backups {
		if item.backup == "" {
			continue
		}
		if err := removeIfExists(item.backup); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove update backup %s: %w", item.backup, err))
		}
	}
	return cleanupErr
}

func cleanupPersistedBackups(backups []PersistedBackup) error {
	validated := make([]backupFile, 0, len(backups))
	for _, item := range backups {
		if strings.TrimSpace(item.Backup) == "" {
			continue
		}
		target, err := filepath.Abs(filepath.Clean(item.Target))
		if err != nil {
			return fmt.Errorf("resolve persisted cleanup target: %w", err)
		}
		backup, err := filepath.Abs(filepath.Clean(item.Backup))
		if err != nil {
			return fmt.Errorf("resolve persisted cleanup backup: %w", err)
		}
		prefix := target + ".update-backup-"
		if runtime.GOOS == "windows" {
			if !strings.HasPrefix(strings.ToLower(backup), strings.ToLower(prefix)) {
				return fmt.Errorf("refusing cleanup backup outside target path: %s", backup)
			}
		} else if !strings.HasPrefix(backup, prefix) {
			return fmt.Errorf("refusing cleanup backup outside target path: %s", backup)
		}
		validated = append(validated, backupFile{target: target, backup: backup})
	}
	return cleanupBackups(validated)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func rollbackError(cause, rollbackErr error) error {
	if rollbackErr == nil {
		return cause
	}
	return fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
}

func TerminateStartedProcess(cmd *exec.Cmd) {
	terminateProcess(cmd)
}

func terminateProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// The manager may have started CPA as a child. Kill the process tree so
		// a failed update cannot leave the new child holding the old binary.
		taskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		if err := taskkill.Run(); err == nil {
			_, _ = cmd.Process.Wait()
			return
		}
	}
	_ = signalProcessGroup(cmd.Process)
	waitDone := make(chan error, 1)
	go func() {
		_, err := cmd.Process.Wait()
		waitDone <- err
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		_ = killProcessGroup(cmd.Process)
		<-waitDone
	}
}

func waitForHealthy(endpoint string, timeout time.Duration) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return errors.New("manager health URL is empty")
	}
	validateResponse := func(response *http.Response) error {
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("service returned %s", response.Status)
		}
		if response.Request != nil && response.Request.URL != nil && response.Request.URL.Path == "/health" {
			var payload struct {
				Service string `json:"service"`
			}
			if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
				return fmt.Errorf("decode manager health response: %w", err)
			}
			if payload.Service != "cpa-manager" {
				return fmt.Errorf("unexpected manager health service %q", payload.Service)
			}
			return nil
		}
		if response.Request != nil && response.Request.URL != nil && response.Request.URL.Path == "/healthz" {
			var payload struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
				return fmt.Errorf("decode CPA health response: %w", err)
			}
			if payload.Status != "ok" {
				return fmt.Errorf("unexpected CPA health status %q", payload.Status)
			}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("create manager health request: %w", err)
		}
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			lastErr = validateResponse(response)
			_ = response.Body.Close()
			if lastErr == nil {
				return nil
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func waitForProcessExit(pid int, expectedPath string, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	running, err := processIsRunning(pid)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	if matches, err := processMatchesExecutable(pid, expectedPath); err != nil {
		return err
	} else if !matches {
		return fmt.Errorf("process %d is not %s", pid, expectedPath)
	}
	deadline := time.Now().Add(timeout)
	for {
		running, err := processIsRunning(pid)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		if matches, err := processMatchesExecutable(pid, expectedPath); err != nil {
			return err
		} else if !matches {
			return fmt.Errorf("process %d changed while waiting to exit", pid)
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return fmt.Errorf("timed out waiting for process %d to exit", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func terminateProcessByPID(pid int, expectedPath string) error {
	if pid <= 0 {
		return nil
	}
	running, err := processIsRunning(pid)
	if err != nil || !running {
		return err
	}
	if matches, err := processMatchesExecutable(pid, expectedPath); err != nil {
		return err
	} else if !matches {
		return fmt.Errorf("refusing to terminate process %d: executable identity mismatch", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if runtime.GOOS == "windows" {
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run(); err != nil {
			return fmt.Errorf("terminate process tree %d: %w", pid, err)
		}
	} else if err := signalProcessGroup(process); err != nil {
		return fmt.Errorf("terminate process group %d: %w", pid, err)
	}
	if err := waitForProcessExit(pid, expectedPath, 5*time.Second); err == nil {
		return nil
	}
	if err := killProcessGroup(process); err != nil {
		return fmt.Errorf("kill process %d after graceful termination: %w", pid, err)
	}
	return waitForProcessExit(pid, expectedPath, 5*time.Second)
}

func processIsRunning(pid int) (bool, error) {
	if runtime.GOOS == "windows" {
		output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("probe process %d: %w", pid, err)
		}
		return strings.Contains(string(output), `"`+strconv.Itoa(pid)+`"`), nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, nil
	}
	return true, nil
}
