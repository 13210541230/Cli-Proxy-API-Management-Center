package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/seakee/cpa-manager/usage-service/internal/update"
)

func main() {
	if err := update.IsolateProcessGroup(); err != nil {
		log.Printf("isolate updater process group: %v", err)
	}
	var options update.ApplyOptions
	flag.StringVar(&options.StagingPath, "staging-path", "", "extracted update bundle directory")
	flag.StringVar(&options.ResultPath, "result-path", "", "persistent update result sidecar")
	flag.StringVar(&options.OS, "os", "", "target operating system")
	flag.StringVar(&options.Arch, "arch", "", "target architecture")
	flag.StringVar(&options.TransactionID, "transaction-id", "", "update transaction identifier")
	flag.StringVar(&options.CPAVersion, "cpa-version", "", "target CLIProxyAPI version")
	flag.StringVar(&options.ManagerVersion, "manager-version", "", "target CPA-Manager version")
	flag.StringVar(&options.ManagerExecutablePath, "manager-path", "", "installed CPA-Manager executable")
	flag.StringVar(&options.CPAExecutablePath, "cpa-path", "", "installed CLIProxyAPI executable")
	flag.StringVar(&options.UpdaterExecutablePath, "updater-path", "", "installed cpa-updater executable")
	managerArgs := flag.String("manager-args", "[]", "JSON array of CPA-Manager arguments")
	cpaArgs := flag.String("cpa-args", "[]", "JSON array of CLIProxyAPI arguments")
	flag.StringVar(&options.ManagerWorkingDirectory, "manager-working-directory", "", "CPA-Manager working directory")
	flag.StringVar(&options.CPAWorkingDirectory, "cpa-working-directory", "", "CLIProxyAPI working directory")
	flag.StringVar(&options.ManagerHealthURL, "manager-health-url", "", "CPA-Manager health URL")
	flag.StringVar(&options.CPAHealthURL, "cpa-health-url", "", "CLIProxyAPI health URL")
	recoverStatus := flag.String("recover-status", "", "recover an interrupted update from a persisted status sidecar")
	managerPID := flag.Int("manager-pid", 0, "PID of the running CPA-Manager process")
	previousCPAWasRunning := flag.Bool("previous-cpa-running", false, "restore CPA when an update fails")
	restoreOnFailure := flag.Bool("restore-on-failure", false, "restore stopped processes when an update fails")
	startCPA := flag.Bool("start-cpa", false, "start CLIProxyAPI before restarting CPA-Manager")
	managerStartCPA := flag.Bool("manager-start-cpa", false, "ask the restarted CPA-Manager to start CLIProxyAPI")
	ownsTransactionLock := flag.Bool("owns-transaction-lock", false, "use the transaction lock transferred by CPA-Manager")
	runStaged := flag.Bool("run-staged", false, "run the staged updater instead of relaying to it")
	flag.Parse()
	options.ManagerPID = *managerPID
	if strings.TrimSpace(*recoverStatus) != "" {
		var recoveryArgs []string
		if err := json.Unmarshal([]byte(*managerArgs), &recoveryArgs); err != nil {
			log.Printf("decode recovery manager arguments: %v", err)
			os.Exit(1)
		}
		err := update.RecoverInterruptedUpdate(update.RecoveryOptions{
			StatusPath:              *recoverStatus,
			ManagerExecutablePath:   options.ManagerExecutablePath,
			ManagerWorkingDirectory: options.ManagerWorkingDirectory,
			ManagerArguments:        recoveryArgs,
			ManagerPID:              options.ManagerPID,
		})
		if err != nil {
			log.Printf("recover interrupted update: %v", err)
			os.Exit(1)
		}
		return
	}
	options.PreviousCPAWasRunning = *previousCPAWasRunning
	options.RestoreOnFailure = *restoreOnFailure
	options.StartCPA = *startCPA
	options.ManagerStartCPA = *managerStartCPA
	options.OwnsTransactionLock = *ownsTransactionLock
	recordFailure := func(err error) {
		if err == nil || strings.TrimSpace(options.ResultPath) == "" {
			return
		}
		if persistErr := update.WritePersistedStatusOwned(options.ResultPath, update.PersistedStatus{
			State:          update.StageFailed,
			OS:             options.OS,
			Arch:           options.Arch,
			TransactionID:  options.TransactionID,
			CPAVersion:     options.CPAVersion,
			ManagerVersion: options.ManagerVersion,
			Error:          err.Error(),
		}); persistErr != nil {
			log.Printf("persist update failure status: %v", persistErr)
		}
	}

	if err := json.Unmarshal([]byte(*managerArgs), &options.ManagerArguments); err != nil {
		recordFailure(err)
		log.Printf("decode manager arguments: %v", err)
		os.Exit(1)
	}
	if err := json.Unmarshal([]byte(*cpaArgs), &options.CPAArguments); err != nil {
		recordFailure(err)
		log.Printf("decode CPA arguments: %v", err)
		os.Exit(1)
	}
	if strings.TrimSpace(options.CPAWorkingDirectory) == "" {
		options.CPAWorkingDirectory = "."
	}
	if !*runStaged {
		relayed, err := relayToStagedUpdater(options)
		if err != nil {
			recordFailure(err)
			log.Printf("start staged updater: %v", err)
			os.Exit(1)
		}
		if relayed {
			return
		}
	}
	if err := update.Apply(options); err != nil {
		log.Printf("apply update: %v", err)
		os.Exit(1)
	}
	fmt.Println("update applied")
}

func relayToStagedUpdater(options update.ApplyOptions) (bool, error) {
	files, err := update.LocateBundle(options.StagingPath)
	if err != nil {
		return false, err
	}
	current, err := os.Executable()
	if err != nil {
		return false, err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return false, err
	}
	staged, err := filepath.Abs(files.UpdaterPath)
	if err != nil {
		return false, err
	}
	if samePath(current, staged) {
		return false, nil
	}
	arguments := append([]string(nil), os.Args[1:]...)
	arguments = append(arguments, "--run-staged=true")
	command := exec.Command(files.UpdaterPath, arguments...)
	command.Dir = filepath.Dir(files.UpdaterPath)
	if err := command.Start(); err != nil {
		return false, err
	}
	if err := command.Process.Release(); err != nil {
		return false, err
	}
	return true, nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
