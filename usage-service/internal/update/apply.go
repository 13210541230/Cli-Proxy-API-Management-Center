package update

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// managerExitGrantTimeout is how long the updater waits for the old manager
// process to exit before treating it as stuck. The manager's shutdown path
// (stop CPA + graceful HTTP shutdown) can take tens of seconds under load,
// so the previous 30s budget was too tight and caused false rollbacks that
// left the deployment stopped.
const managerExitGrantTimeout = 90 * time.Second

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
	CPAConfigPath           string
	CPAWorkingDirectory     string
	ManagerHealthURL        string
	CPAHealthURL            string
	ManagerPID              int
	CPAPID                  int
	PreviousCPAWasRunning   bool
	RestoreOnFailure        bool
	StartCPA                bool
	ManagerStartCPA         bool
	SuppressManagerCPAStart bool
	RecordRuntimeOnStart    bool
	StopRuntimeBeforeApply  bool
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
		"--cpa-config", options.CPAConfigPath,
		"--cpa-working-directory", options.CPAWorkingDirectory,
		"--manager-health-url", options.ManagerHealthURL,
		"--cpa-health-url", options.CPAHealthURL,
		"--manager-pid", strconv.Itoa(options.ManagerPID),
		"--cpa-pid", strconv.Itoa(options.CPAPID),
		"--previous-cpa-running", strconv.FormatBool(options.PreviousCPAWasRunning),
		"--restore-on-failure", strconv.FormatBool(options.RestoreOnFailure),
		"--start-cpa", strconv.FormatBool(options.StartCPA),
		"--manager-start-cpa", strconv.FormatBool(options.ManagerStartCPA),
		"--suppress-manager-cpa-start", strconv.FormatBool(options.SuppressManagerCPAStart),
		"--record-runtime-on-start", strconv.FormatBool(options.RecordRuntimeOnStart),
		"--stop-runtime-before-apply", strconv.FormatBool(options.StopRuntimeBeforeApply),
		"--owns-transaction-lock", strconv.FormatBool(options.OwnsTransactionLock),
	)
	cmd.Dir = filepath.Dir(launchPath)
	// Keep the detached updater log beside the suite's other runtime logs.
	var helperLog *os.File
	logDir := filepath.Join(filepath.Dir(options.ManagerExecutablePath), "logs", "update")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create updater helper log directory: %w", err)
	}
	if options.TransactionID == "" {
		return nil, errors.New("update helper transaction ID is empty")
	}
	var logFileErr error
	helperLog, logFileErr = os.OpenFile(filepath.Join(logDir, "update-helper.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if logFileErr != nil {
		return nil, fmt.Errorf("create updater helper log: %w", logFileErr)
	}
	cmd.Stdout = helperLog
	cmd.Stderr = helperLog
	if err := cmd.Start(); err != nil {
		if helperLog != nil {
			if closeErr := helperLog.Close(); closeErr != nil {
				log.Printf("close updater helper log after start failure: %v", closeErr)
			}
		}
		return nil, fmt.Errorf("start update helper: %w", err)
	}
	if helperLog != nil {
		if closeErr := helperLog.Close(); closeErr != nil {
			log.Printf("close updater helper log after start: %v", closeErr)
		}
	}
	if err := cmd.Process.Release(); err != nil {
		terminateProcess(cmd)
		return nil, fmt.Errorf("release update helper process: %w", err)
	}
	return cmd, nil
}

func Apply(options ApplyOptions) (applyErr error) {
	backups := make([]backupFile, 0, 3)
	startedAt := time.Now().UnixMilli()
	resultRecorded := false
	preserveApplyingOnExit := false
	// recordResult is captured by the terminal-status fallback defer below, so
	// it must be assigned before any validation return can fire the defer.
	recordResult := func(state string, resultErr error) error {
		status := PersistedStatus{
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
		}
		err := WritePersistedStatusOwned(options.ResultPath, status)
		if err == nil {
			resultRecorded = true
			return nil
		}
		log.Printf("update status persist failed (owned): %v", err)
		// The owning transaction may already be gone (helper terminated or
		// identity check failed). Fall back to an unowned write so the
		// terminal state and the real failure reason are never lost.
		if fallbackErr := WritePersistedStatus(options.ResultPath, status); fallbackErr == nil {
			resultRecorded = true
			return nil
		}
		return err
	}
	// Register the terminal-status fallback before any validation return so
	// every failure path, including early validation, leaves a real failure
	// reason in the sidecar.
	defer func() {
		if !resultRecorded && !preserveApplyingOnExit {
			if applyErr == nil {
				applyErr = errors.New("update helper exited before recording a result")
			}
			if recordResult == nil {
				return
			}
			if err := recordResult(StageFailed, applyErr); err != nil {
				log.Printf("update status persist failed: %v", err)
			}
		}
		if err := removeStagingPath(options.StagingPath); err != nil {
			log.Printf("update staging cleanup failed: %v", err)
		}
	}()
	if strings.TrimSpace(options.StagingPath) == "" {
		applyErr = errors.New("update staging path is empty")
		return
	}
	if strings.TrimSpace(options.ManagerExecutablePath) == "" || strings.TrimSpace(options.CPAExecutablePath) == "" {
		applyErr = errors.New("manager and CPA executable paths are required")
		return
	}
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
	var applyLock *transactionLock
	var err error
	if options.OwnsTransactionLock {
		applyLock, err = adoptTransactionLock(options.ResultPath, options.TransactionID)
	} else {
		applyLock, err = acquireTransactionLock(options.ResultPath, options.TransactionID, transactionLockWait)
	}
	if err != nil {
		applyErr = err
		return
	}
	defer func() {
		if err := applyLock.Release(); err != nil {
			log.Printf("release update apply lock: %v", err)
		}
	}()
	rollback := func() error {
		return rollbackPersistedBackups(persistedBackups(backups))
	}
	log.Printf("update apply: transaction %s -> CPA %s / CPA-Manager %s", options.TransactionID, options.CPAVersion, options.ManagerVersion)
	files, err := LocateBundle(options.StagingPath)
	if err != nil {
		if options.StopRuntimeBeforeApply {
			applyErr = err
			recordResult(StageFailed, err)
		} else {
			applyErr = recoverApplyFailure(options, rollback, recordResult, err)
		}
		return
	}
	if options.StopRuntimeBeforeApply {
		if err := stopRuntimeBeforeApply(options); err != nil {
			if options.RestoreOnFailure && runtimeProcessesStopped(options) {
				applyErr = restartAfterRollback(options, rollback, recordResult, fmt.Errorf("stop suite processes: %w", err))
			} else {
				applyErr = err
				recordResult(StageFailed, err)
			}
			return
		}
	} else if options.ManagerPID > 0 {
		if err := waitForProcessExit(options.ManagerPID, options.ManagerExecutablePath, managerExitGrantTimeout); err != nil {
			applyErr = recoverApplyFailure(options, rollback, recordResult, err)
			return
		}
	}
	type replacement struct {
		source           string
		target           string
		label            string
		directory        bool
		preserveUserData bool
	}
	root := filepath.Dir(options.ManagerExecutablePath)
	replacements := []replacement{
		{source: files.CPAPath, target: options.CPAExecutablePath, label: "CLIProxyAPI"},
		{source: files.ManagerPath, target: options.ManagerExecutablePath, label: "CPA-Manager"},
	}
	if files.VersionPath != "" {
		replacements = append(replacements, replacement{source: files.VersionPath, target: filepath.Join(root, suiteVersionFilename), label: "suite version metadata"})
	}
	if files.PluginsPath != "" {
		replacements = append(replacements, replacement{source: files.PluginsPath, target: filepath.Join(root, "plugins"), label: "plugins", directory: true, preserveUserData: true})
	}
	if files.StaticPath != "" {
		replacements = append(replacements, replacement{source: files.StaticPath, target: filepath.Join(root, "static"), label: "static assets", directory: true})
	}
	for name, source := range files.Files {
		replacements = append(replacements, replacement{source: source, target: filepath.Join(root, name), label: name})
	}
	// The installed helper relays to the staged helper before Apply runs, so the
	// staged process can replace the installed updater image safely.
	if strings.TrimSpace(options.UpdaterExecutablePath) != "" && !isCurrentExecutable(options.UpdaterExecutablePath) {
		replacements = append(replacements, replacement{source: files.UpdaterPath, target: options.UpdaterExecutablePath, label: "cpa-updater"})
	}
	for _, item := range replacements {
		planned, planErr := planBackup(item.target)
		if planErr != nil {
			applyErr = recoverApplyFailure(options, rollback, recordResult, fmt.Errorf("plan %s replacement: %w", item.label, planErr))
			return
		}
		backups = append(backups, planned)
	}
	if err := persistApplying(); err != nil {
		applyErr = recoverApplyFailure(options, rollback, recordResult, fmt.Errorf("persist replacement plan: %w", err))
		return
	}
	for index, item := range replacements {
		var replaceErr error
		if item.directory {
			replaceErr = replaceDirectoryWithBackup(item.source, &backups[index], item.preserveUserData)
		} else {
			replaceErr = replaceFileWithBackup(item.source, &backups[index])
		}
		if replaceErr != nil {
			applyErr = restartAfterRollback(options, rollback, recordResult, fmt.Errorf("replace %s: %w", item.label, replaceErr))
			return
		}
		if err := persistApplying(); err != nil {
			applyErr = restartAfterRollback(options, rollback, recordResult, fmt.Errorf("persist %s replacement metadata: %w", item.label, err))
			return
		}
	}
	log.Printf("update apply: replaced %d executables", len(replacements))

	cpaCmd, managerCmd, err := startProcesses(options)
	if err != nil {
		applyErr = restartAfterRollback(options, rollback, recordResult, fmt.Errorf("start updated processes: %w", err))
		return
	}
	log.Printf("update apply: started CPA-Manager pid %d (previous manager %d)", managerCmd.Process.Pid, options.ManagerPID)
	if err := waitForProcessesHealthy(options, managerCmd, cpaCmd); err != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		applyErr = restartAfterRollback(options, rollback, recordResult, fmt.Errorf("updated CPA-Manager health check failed: %w", err))
		return
	}
	log.Printf("update apply: health checks passed; committing update result")
	// Commit the successful health-checked state before deleting backups. If the
	// updater exits in the cleanup window, the next manager can safely retry the
	// idempotent cleanup without treating the update as interrupted.
	preserveApplyingOnExit = true
	if err := recordResult(StageSucceeded, nil); err != nil {
		applyErr = fmt.Errorf("persist successful update status: %w", err)
		return
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
	log.Printf("update apply failure recovery started: %v", cause)
	if rollbackErr := rollback(); rollbackErr != nil {
		failure := fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
		log.Printf("update apply failure: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	if !options.RestoreOnFailure {
		log.Printf("update apply failure (no restore): %v", cause)
		recordResult(StageFailed, cause)
		return cause
	}

	managerExited := options.ManagerPID == 0
	if !managerExited {
		managerExited = waitForProcessExit(options.ManagerPID, options.ManagerExecutablePath, managerExitGrantTimeout) == nil
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
		log.Printf("update apply failure: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}

	cpaCmd, managerCmd, restartErr := startProcesses(options)
	if restartErr != nil {
		failure := fmt.Errorf("%w; rollback restart failed: %v", cause, restartErr)
		log.Printf("update apply failure: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	log.Printf("update apply failure: rollback restart started CPA-Manager pid %d", managerCmd.Process.Pid)
	if healthErr := waitForProcessesHealthy(options, managerCmd, cpaCmd); healthErr != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		failure := fmt.Errorf("%w; rollback health check failed: %v", cause, healthErr)
		log.Printf("update apply failure: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	log.Printf("update apply failure: rollback health checks passed")
	_ = managerCmd.Process.Release()
	if cpaCmd != nil {
		_ = cpaCmd.Process.Release()
	}
	recordResult(StageRolledBack, cause)
	return cause
}

func stopRuntimeBeforeApply(options ApplyOptions) error {
	root := filepath.Dir(options.ManagerExecutablePath)
	state, exists, err := ReadSuiteRuntimeState(root)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("suite process state is missing; start the suite with start.bat or start.sh before automatic updates")
	}
	if options.ManagerPID > 0 && state.Manager.PID != options.ManagerPID {
		return errors.New("CPA-Manager PID changed before update")
	}
	if options.CPAPID > 0 && state.CPA.PID != options.CPAPID {
		return errors.New("CLIProxyAPI PID changed before update")
	}
	managerRunning, err := RuntimeProcessRunning(state.Manager)
	if err != nil {
		return err
	}
	cpaRunning, err := RuntimeProcessRunning(state.CPA)
	if err != nil {
		return err
	}
	if managerRunning {
		if err := terminateProcessByPID(state.Manager.PID, options.ManagerExecutablePath); err != nil {
			return fmt.Errorf("stop CPA-Manager: %w", err)
		}
	}
	if cpaRunning {
		if err := terminateProcessByPID(state.CPA.PID, options.CPAExecutablePath); err != nil {
			return fmt.Errorf("stop CLIProxyAPI: %w", err)
		}
	}
	state.Manager.PID = 0
	state.CPA.PID = 0
	state.UpdatedAtMS = time.Now().UnixMilli()
	if err := WriteSuiteRuntimeState(root, state); err != nil {
		return fmt.Errorf("persist stopped suite process state: %w", err)
	}
	return nil
}

func runtimeProcessesStopped(options ApplyOptions) bool {
	root := filepath.Dir(options.ManagerExecutablePath)
	state, exists, err := ReadSuiteRuntimeState(root)
	if err != nil || !exists {
		return false
	}
	managerRunning, managerErr := RuntimeProcessRunning(state.Manager)
	cpaRunning, cpaErr := RuntimeProcessRunning(state.CPA)
	return managerErr == nil && cpaErr == nil && !managerRunning && !cpaRunning
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
	} else if options.SuppressManagerCPAStart && !hasArgument(managerArguments, "--no-start-cpa") {
		managerArguments = append(managerArguments, "--no-start-cpa")
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
	if options.RecordRuntimeOnStart {
		configPath := strings.TrimSpace(options.CPAConfigPath)
		if configPath == "" {
			configPath = configuredCPAPath(options.CPAArguments, filepath.Dir(options.CPAExecutablePath))
		}
		cpaPID := 0
		if cpaCmd != nil && cpaCmd.Process != nil {
			cpaPID = cpaCmd.Process.Pid
		}
		state, stateErr := NewSuiteRuntimeState(filepath.Dir(options.ManagerExecutablePath), configPath, cpaPID, managerCmd.Process.Pid)
		if stateErr == nil {
			state.CPAArguments = append([]string(nil), options.CPAArguments...)
			state.ManagerArguments = append([]string(nil), managerArguments...)
			state.CPAWorkingDirectory = options.CPAWorkingDirectory
			if state.CPAWorkingDirectory == "" {
				state.CPAWorkingDirectory = filepath.Dir(options.CPAExecutablePath)
			}
			state.ManagerWorkingDirectory = managerCmd.Dir
			stateErr = WriteSuiteRuntimeState(filepath.Dir(options.ManagerExecutablePath), state)
		}
		if stateErr != nil {
			terminateProcess(managerCmd)
			terminateProcess(cpaCmd)
			return nil, nil, fmt.Errorf("record started suite processes: %w", stateErr)
		}
	}
	return cpaCmd, managerCmd, nil
}

func hasArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}

func configuredCPAPath(arguments []string, root string) string {
	for index, argument := range arguments {
		if argument == "--config" && index+1 < len(arguments) {
			return arguments[index+1]
		}
		if strings.HasPrefix(argument, "--config=") {
			return strings.TrimPrefix(argument, "--config=")
		}
	}
	return filepath.Join(root, "config.yaml")
}

func restartAfterRollback(options ApplyOptions, rollback func() error, recordResult func(string, error) error, cause error) error {
	log.Printf("update apply rollback started: %v", cause)
	if rollbackErr := rollback(); rollbackErr != nil {
		failure := fmt.Errorf("%w; rollback failed: %v", cause, rollbackErr)
		log.Printf("update apply rollback failed: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	cpaCmd, managerCmd, restartErr := startProcesses(options)
	if restartErr != nil {
		failure := fmt.Errorf("%w; rollback restart failed: %v", cause, restartErr)
		log.Printf("update apply rollback restart failed: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	log.Printf("update apply rollback restarted CPA-Manager pid %d", managerCmd.Process.Pid)
	if healthErr := waitForProcessesHealthy(options, managerCmd, cpaCmd); healthErr != nil {
		terminateProcess(managerCmd)
		terminateProcess(cpaCmd)
		failure := fmt.Errorf("%w; rollback health check failed: %v", cause, healthErr)
		log.Printf("update apply rollback health failed: %v", failure)
		recordResult(StageFailed, failure)
		return failure
	}
	log.Printf("update apply rollback health checks passed")
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
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return backupFile{target: target}, nil
	} else if err != nil {
		return backupFile{}, fmt.Errorf("stat current update target: %w", err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	backup := target + ".update-backup-" + suffix
	if info.IsDir() {
		digest, digestErr := directorySHA256(target)
		if digestErr != nil {
			return backupFile{}, fmt.Errorf("hash current update directory: %w", digestErr)
		}
		return backupFile{target: target, backup: backup, sha256: digest}, nil
	}
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

func replaceDirectoryWithBackup(source string, item *backupFile, preserveUserData bool) error {
	if item == nil || item.target == "" {
		return errors.New("replacement directory target is empty")
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("staged replacement directory is unavailable: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(item.target), 0o755); err != nil {
		return fmt.Errorf("create replacement directory parent: %w", err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	temporary := item.target + ".update-new-" + suffix
	_ = os.RemoveAll(temporary)
	if err := copyDirectory(source, temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	if preserveUserData {
		if err := preserveDirectoryUserData(item.target, temporary); err != nil {
			_ = os.RemoveAll(temporary)
			return err
		}
	}
	if item.backup == "" {
		if _, err := os.Stat(item.target); err == nil {
			_ = os.RemoveAll(temporary)
			return errors.New("replacement directory appeared after planning")
		} else if !os.IsNotExist(err) {
			_ = os.RemoveAll(temporary)
			return fmt.Errorf("recheck current update directory: %w", err)
		}
		return waitRename(temporary, item.target)
	}
	if _, err := os.Stat(item.backup); err == nil {
		_ = os.RemoveAll(temporary)
		return errors.New("replacement directory backup path already exists")
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(temporary)
		return fmt.Errorf("check replacement directory backup: %w", err)
	}
	if err := waitRename(item.target, item.backup); err != nil {
		_ = os.RemoveAll(temporary)
		return fmt.Errorf("backup current update directory: %w", err)
	}
	if err := waitRename(temporary, item.target); err != nil {
		_ = os.Rename(item.backup, item.target)
		return fmt.Errorf("install updated directory: %w", err)
	}
	return nil
}

func preserveDirectoryUserData(source, target string) error {
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if !isPreservedPluginDataPath(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to preserve plugin symlink: %s", path)
		}
		destination := filepath.Join(target, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return copyFile(path, destination)
	})
}

func isPreservedPluginDataPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch strings.ToLower(part) {
		case "data", "auths", "cache", "logs":
			return true
		}
	}
	base := strings.ToLower(filepath.Base(path))
	if base == "settings.json" || base == "config.json" {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".toml", ".ini", ".db", ".db-wal", ".db-shm", ".sqlite", ".sqlite-wal", ".sqlite-shm", ".sqlite3", ".sqlite3-wal", ".sqlite3-shm", ".wal", ".shm", ".log", ".key", ".pem", ".token", ".env":
		return true
	default:
		return false
	}
}

func copyDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := target
		if relative != "." {
			destination = filepath.Join(target, relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink in update directory: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported update directory entry: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return copyFile(path, destination)
	})
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
	if err := waitForHealthy(options.ManagerHealthURL, 90*time.Second); err != nil {
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
		if err := waitForHealthy(options.CPAHealthURL, 90*time.Second); err != nil {
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

func pathSHA256(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return directorySHA256(path)
	}
	return fileSHA256(path)
}

func directorySHA256(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot hash update directory symlink: %s", path)
		}
		if _, err := io.WriteString(hash, filepath.ToSlash(relative)+"\x00"+info.Mode().String()+"\x00"); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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
				if actual, hashErr := pathSHA256(target); hashErr == nil && strings.EqualFold(actual, item.SHA256) {
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
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
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

func TerminateProcessByPID(pid int, executablePath string) error {
	return terminateProcessByPID(pid, executablePath)
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
	client := healthHTTPClient(endpoint)
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("create manager health request: %w", err)
		}
		response, err := client.Do(request)
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

func healthHTTPClient(endpoint string) *http.Client {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return http.DefaultClient
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return http.DefaultClient
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultClient
	}
	transport = transport.Clone()
	// Local CPA TLS commonly uses a self-signed certificate; the probe stays on loopback.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	if httpTransport, ok := http.DefaultTransport.(*http.Transport); ok && httpTransport.TLSClientConfig != nil {
		transport.TLSClientConfig = httpTransport.TLSClientConfig.Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	return &http.Client{Transport: transport}
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
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
	} else if err := signalProcessGroup(process); err != nil {
		return fmt.Errorf("terminate process group %d: %w", pid, err)
	}
	if err := waitForProcessExit(pid, expectedPath, 5*time.Second); err == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run(); err != nil {
			if waitErr := waitForProcessExit(pid, expectedPath, 0); waitErr != nil {
				return fmt.Errorf("force terminate process tree %d: %w", pid, err)
			}
		}
	} else if err := killProcessGroup(process); err != nil {
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
