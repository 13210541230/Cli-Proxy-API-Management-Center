package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	managerconfig "github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/update"
)

func runAutomaticUpdate(checkOnly bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve updater executable: %w", err)
	}
	root, err := filepath.Abs(filepath.Dir(executable))
	if err != nil {
		return fmt.Errorf("resolve suite directory: %w", err)
	}
	installed, err := update.ReadSuiteVersion(root)
	if err != nil {
		return err
	}
	client := update.NewClient()
	manifest, err := client.CheckLatest(context.Background())
	if err != nil {
		return fmt.Errorf("check latest suite release: %w", err)
	}
	available, err := update.HasNewerSuiteVersion(manifest, installed)
	if err != nil {
		return err
	}
	if !available {
		fmt.Printf("CLIProxyAPI Suite is up to date (CPA %s / CPA-Manager %s).\n", installed.CPAVersion, installed.ManagerVersion)
		return nil
	}
	fmt.Printf("Update available: CPA %s / CPA-Manager %s (current %s / %s).\n", manifest.CPAVersion, manifest.ManagerVersion, installed.CPAVersion, installed.ManagerVersion)
	if checkOnly {
		return nil
	}

	runtimeState, exists, err := update.ReadSuiteRuntimeState(root)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("automatic update requires suite process state; stop manually started processes and start the suite with start.bat or start.sh first")
	}
	if _, err := os.Stat(runtimeState.ConfigPath); err != nil {
		return fmt.Errorf("configured CPA file is unavailable: %w", err)
	}
	managerRunning, err := update.RuntimeProcessRunning(runtimeState.Manager)
	if err != nil {
		return err
	}
	cpaRunning, err := update.RuntimeProcessRunning(runtimeState.CPA)
	if err != nil {
		return err
	}
	if !managerRunning || !cpaRunning {
		return errors.New("automatic update requires both processes to be running from start.bat or start.sh; start the suite, then run cpa-updater again")
	}

	managerSettings, err := loadManagerSettings()
	if err != nil {
		return err
	}
	statusPath := update.StatusPath(managerSettings.DBPath)
	stager := update.NewStager(client, statusPath)
	goos, goarch := update.CurrentTarget()
	staged, err := stager.Stage(context.Background(), goos, goarch)
	if err != nil {
		return fmt.Errorf("download and verify update package: %w", err)
	}
	stagedBundle, err := update.LocateBundle(staged.StagingPath)
	if err != nil {
		_ = stager.Fail(err)
		return err
	}
	stagedVersion, err := update.ReadSuiteVersion(stagedBundle.RootPath)
	if err != nil {
		_ = stager.Fail(err)
		return err
	}
	if stagedVersion.CPAVersion != staged.Manifest.CPAVersion || stagedVersion.ManagerVersion != staged.Manifest.ManagerVersion {
		err := errors.New("downloaded suite version metadata does not match the release manifest")
		_ = stager.Fail(err)
		return err
	}
	staged, err = stager.BeginApply()
	if err != nil {
		return fmt.Errorf("begin suite update: %w", err)
	}

	managerName, cpaName, updaterName := "cpa-manager", "cli-proxy-api", "cpa-updater"
	if filepath.Ext(executable) != "" {
		managerName += filepath.Ext(executable)
		cpaName += filepath.Ext(executable)
		updaterName += filepath.Ext(executable)
	}
	managerPath := filepath.Join(root, managerName)
	cpaPath := filepath.Join(root, cpaName)
	updaterPath := filepath.Join(root, updaterName)
	managerHealthURL, cpaHealthURL, err := resolveHealthURLs(managerSettings, runtimeState.ConfigPath)
	if err != nil {
		_ = stager.Fail(err)
		return fmt.Errorf("resolve suite health endpoints: %w", err)
	}
	options := update.ApplyOptions{
		StagingPath:             staged.StagingPath,
		ResultPath:              statusPath,
		OS:                      staged.OS,
		Arch:                    staged.Arch,
		TransactionID:           staged.TransactionID,
		CPAVersion:              staged.Manifest.CPAVersion,
		ManagerVersion:          staged.Manifest.ManagerVersion,
		ManagerExecutablePath:   managerPath,
		ManagerWorkingDirectory: runtimeState.ManagerWorkingDirectory,
		CPAExecutablePath:       cpaPath,
		UpdaterExecutablePath:   updaterPath,
		ManagerArguments:        append([]string(nil), runtimeState.ManagerArguments...),
		CPAArguments:            append([]string(nil), runtimeState.CPAArguments...),
		CPAConfigPath:           runtimeState.ConfigPath,
		CPAWorkingDirectory:     runtimeState.CPAWorkingDirectory,
		ManagerHealthURL:        managerHealthURL,
		CPAHealthURL:            cpaHealthURL,
		ManagerPID:              runtimeState.Manager.PID,
		CPAPID:                  runtimeState.CPA.PID,
		PreviousCPAWasRunning:   runtimeState.CPA.PID > 0,
		RestoreOnFailure:        true,
		StartCPA:                true,
		SuppressManagerCPAStart: true,
		RecordRuntimeOnStart:    true,
		StopRuntimeBeforeApply:  true,
		OwnsTransactionLock:     true,
	}
	helper, err := update.StartHelper(updaterPath, options)
	if err != nil {
		_ = stager.Fail(err)
		return fmt.Errorf("start staged updater: %w", err)
	}
	if err := stager.TransferApplyLock(helper.Process.Pid, staged.TransactionID, helper.Path); err != nil {
		_ = update.TerminateProcessByPID(helper.Process.Pid, helper.Path)
		_ = stager.Fail(err)
		return fmt.Errorf("transfer update transaction to helper: %w", err)
	}
	fmt.Println("Automatic update started. CPA-Manager and CLIProxyAPI will restart after replacement.")
	return nil
}

type managerSettings struct {
	DBPath   string
	HTTPAddr string
}

func loadManagerSettings() (managerSettings, error) {
	cfg, err := managerconfig.Load()
	if err != nil {
		return managerSettings{}, fmt.Errorf("load CPA-Manager configuration: %w", err)
	}
	return managerSettings{DBPath: cfg.DBPath, HTTPAddr: cfg.HTTPAddr}, nil
}

func resolveHealthURLs(settings managerSettings, cpaConfigPath string) (string, string, error) {
	managerURL := localHealthEndpoint(settings.HTTPAddr, "/health", "http://127.0.0.1:18317/health")
	cpaURL, err := cpaHealthURLFromConfig(cpaConfigPath)
	if err != nil {
		return "", "", err
	}
	return managerURL, cpaURL, nil
}

func cpaHealthURLFromConfig(configPath string) (string, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("open CLIProxyAPI config: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("close CLIProxyAPI config after endpoint detection: %v", closeErr)
		}
	}()
	var host string
	port := 8317
	tlsEnabled := false
	inTLS := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		rawLine := strings.TrimPrefix(scanner.Text(), "\ufeff")
		if rawLine == "" || strings.TrimSpace(rawLine) == "" {
			continue
		}
		if rawLine[0] == ' ' || rawLine[0] == '\t' {
			if inTLS {
				key, value, ok := strings.Cut(strings.TrimSpace(rawLine), ":")
				if ok && strings.TrimSpace(key) == "enable" {
					parsed, parseErr := strconv.ParseBool(strings.Trim(strings.TrimSpace(strings.SplitN(value, "#", 2)[0]), "\"'"))
					if parseErr != nil {
						return "", fmt.Errorf("invalid CLIProxyAPI TLS enable setting: %w", parseErr)
					}
					tlsEnabled = parsed
				}
			}
			continue
		}
		inTLS = false
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
		switch key {
		case "tls":
			inTLS = true
		case "host":
			host = strings.Trim(value, "\"'")
		case "port":
			parsed, parseErr := strconv.Atoi(strings.Trim(value, "\"'"))
			if parseErr != nil || parsed < 1 || parsed > 65535 {
				return "", fmt.Errorf("invalid CLIProxyAPI port %q", value)
			}
			port = parsed
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read CLIProxyAPI config: %w", err)
	}
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "*":
		host = "127.0.0.1"
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/healthz", nil
}

func localHealthEndpoint(address, path, fallback string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return fallback
	}
	if strings.Contains(address, "://") {
		parsed, err := url.Parse(address)
		if err != nil || parsed.Host == "" {
			return fallback
		}
		parsed.Path = path
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fallback
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + path
}
