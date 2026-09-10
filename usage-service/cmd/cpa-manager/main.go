package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/httpapi"
	"github.com/seakee/cpa-manager/usage-service/internal/mail"
	"github.com/seakee/cpa-manager/usage-service/internal/rollup"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/supervisor"
	"github.com/seakee/cpa-manager/usage-service/internal/update"
)

func main() {
	if err := supervisor.IsolateProcessGroup(); err != nil {
		log.Printf("isolate manager process group: %v", err)
	}
	startCPAOnLaunch := flag.Bool("start-cpa", false, "start CLIProxyAPI during manager startup")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Printf("load config: %v", err)
		return
	}
	if handled, err := maybeRecoverInterruptedUpdate(cfg.DBPath, *startCPAOnLaunch); err != nil {
		log.Printf("start interrupted update recovery: %v", err)
		return
	} else if handled {
		return
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Printf("open sqlite: %v", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close sqlite: %v", err)
		}
	}()

	// Build alert config: env defaults overridden by DB-persisted config
	alertCfg := buildAlertConfig(db, cfg)
	sender := mail.NewSender(mail.Config{
		Host:     alertCfg.SMTPHost,
		Port:     alertCfg.SMTPPort,
		Username: alertCfg.SMTPUsername,
		Password: alertCfg.SMTPPassword,
		From:     alertCfg.SMTPFrom,
		FromName: alertCfg.SMTPFromName,
	})
	runtimeController := supervisor.New()
	if savedConfig, ok, err := db.LoadManagerConfig(context.Background()); err != nil {
		log.Printf("load local runtime config: %v", err)
	} else if ok {
		runtimeCfg := savedConfig.LocalRuntime
		if !savedConfig.LocalRuntimeConfigured() {
			if defaultCfg, defaultOK := supervisor.DefaultConfig(); defaultOK {
				runtimeCfg = store.LocalRuntimeConfig{
					Enabled:           defaultCfg.Enabled,
					CPAExecutablePath: defaultCfg.CPAExecutablePath,
					WorkingDirectory:  defaultCfg.WorkingDirectory,
					Arguments:         append([]string(nil), defaultCfg.Arguments...),
					AutoStart:         defaultCfg.AutoStart,
					HealthURL:         defaultCfg.HealthURL,
				}
			}
		}
		healthBaseURL := runtimeCfg.HealthURL
		if healthBaseURL == "" {
			healthBaseURL = savedConfig.CPAConnection.CPABaseURL
		}
		if healthBaseURL == "" {
			healthBaseURL = cfg.CPAUpstreamURL
		}
		if healthBaseURL == "" {
			if setup, setupOK, setupErr := db.LoadSetup(context.Background()); setupErr == nil && setupOK {
				healthBaseURL = setup.CPAUpstreamURL
			}
		}
		healthBaseURL = runtimeHealthBaseURL(healthBaseURL, "")
		configureLocalCPA(runtimeController, supervisor.Config{
			Enabled:           runtimeCfg.Enabled,
			CPAExecutablePath: runtimeCfg.CPAExecutablePath,
			WorkingDirectory:  runtimeCfg.WorkingDirectory,
			Arguments:         runtimeCfg.Arguments,
			AutoStart:         runtimeCfg.AutoStart,
			HealthURL:         runtimeCfg.HealthURL,
		}, healthBaseURL, *startCPAOnLaunch, "auto-start")
	} else if runtimeCfg, ok := supervisor.DefaultConfig(); ok {
		setupURL := ""
		if setup, setupOK, setupErr := db.LoadSetup(context.Background()); setupErr == nil && setupOK {
			setupURL = setup.CPAUpstreamURL
		}
		healthBaseURL := runtimeHealthBaseURL(cfg.CPAUpstreamURL, setupURL)
		configureLocalCPA(runtimeController, runtimeCfg, healthBaseURL, false, "auto-start adjacent")
	}

	manager := collector.NewManager(cfg, db, sender, collector.AlertConfig{
		Enabled:           alertCfg.AlertEnabled,
		ThresholdCents:    alertCfg.ThresholdCents,
		CheckInterval:     time.Duration(alertCfg.CheckIntervalMS) * time.Millisecond,
		PoolAlertEnabled:  alertCfg.PoolCheckEnabled,
		PoolCheckInterval: time.Duration(alertCfg.PoolCheckInterval) * time.Minute,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Rollup catches up in event-id batches so historical rebuilds do not block the HTTP listener.
	rollupWorker := rollup.NewWorker(db, rollup.Config{BatchSize: 1000})
	rollupWorker.Start(ctx)

	if cfg.CPAUpstreamURL != "" && cfg.ManagementKey != "" {
		manager.Start(ctx, collector.RuntimeConfig{
			CPAUpstreamURL: cfg.CPAUpstreamURL,
			ManagementKey:  cfg.ManagementKey,
			CollectorMode:  cfg.CollectorMode,
			Queue:          cfg.Queue,
			PopSide:        cfg.PopSide,
			BatchSize:      cfg.BatchSize,
			PollInterval:   cfg.PollInterval,
			TLSSkipVerify:  cfg.TLSSkipVerify,
		})
	} else if managerCfg, ok, err := db.LoadManagerConfig(ctx); err == nil && ok &&
		managerCfg.CPAConnection.CPABaseURL != "" && managerCfg.CPAConnection.ManagementKey != "" {
		if managerCollectorEnabled(managerCfg) {
			manager.Start(ctx, runtimeConfigFromManagerConfig(managerCfg, cfg))
		}
	} else if setup, ok, err := db.LoadSetup(ctx); err == nil && ok {
		manager.Start(ctx, collector.RuntimeConfig{
			CPAUpstreamURL: setup.CPAUpstreamURL,
			ManagementKey:  setup.ManagementKey,
			CollectorMode:  cfg.CollectorMode,
			Queue:          setup.Queue,
			PopSide:        setup.PopSide,
			BatchSize:      cfg.BatchSize,
			PollInterval:   cfg.PollInterval,
			TLSSkipVerify:  cfg.TLSSkipVerify,
		})
	} else if err != nil {
		log.Printf("load setup: %v", err)
	}

	managerServer := httpapi.New(cfg, db, manager, runtimeController)
	managerServer.SetShutdown(stop)
	if executable, executableErr := os.Executable(); executableErr == nil {
		managerServer.SetProcessIdentity(executable, persistentArguments(os.Args[1:]))
	} else {
		log.Printf("resolve manager executable path: %v", executableErr)
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           managerServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("cpa-manager listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
			stop()
		}
	}()

	// Periodic cleanup of old usage events
	if cfg.RetentionDays > 0 {
		cleanupInterval := 24 * time.Hour
		log.Printf("cleanup: purging usage_events older than %d days", cfg.RetentionDays)
		go func() {
			ticker := time.NewTicker(cleanupInterval)
			defer ticker.Stop()
			for {
				cleanupCutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays).UnixMilli()
				if n, err := db.PurgeEventsBefore(ctx, cleanupCutoff); err != nil {
					log.Printf("cleanup: purge error: %v", err)
				} else if n > 0 {
					log.Printf("cleanup: purged %d old events", n)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}

	<-ctx.Done()
	if _, err := runtimeController.Stop(); err != nil {
		log.Printf("stop CLIProxyAPI: %v", err)
	} else if err := runtimeController.WaitStopped(10 * time.Second); err != nil {
		log.Printf("wait for CLIProxyAPI shutdown: %v", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager.Stop()
	rollupWorker.Stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func maybeRecoverInterruptedUpdate(dbPath string, startCPA bool) (bool, error) {
	statusPath, err := filepath.Abs(update.StatusPath(dbPath))
	if err != nil {
		return false, err
	}
	status, ok, err := update.ReadPersistedStatus(statusPath)
	if err != nil || !ok || status.State != update.StageApplying {
		return false, err
	}
	active, err := update.TransactionActive(statusPath)
	if err != nil {
		return true, err
	}
	if active {
		// The detached updater holds the transaction lock while it restarts this
		// manager. Keep the new manager available for the updater's health check;
		// recovery is deferred until the helper records a terminal status. If the
		// helper dies without recording a result, a background watcher resolves
		// the dangling applying transaction so the UI observes a terminal state.
		log.Printf("an update helper is still active; manager startup continues")
		go update.WatchAndResolvePendingStatus(statusPath, nil)
		return false, nil
	}
	if len(status.Backups) == 0 {
		return false, nil
	}
	managerPath, err := os.Executable()
	if err != nil {
		return false, err
	}
	managerPath, err = filepath.Abs(managerPath)
	if err != nil {
		return false, err
	}
	updaterName := "cpa-updater"
	if filepath.Ext(managerPath) != "" {
		updaterName += filepath.Ext(managerPath)
	}
	updaterPath := filepath.Join(filepath.Dir(managerPath), updaterName)
	if _, err := os.Stat(updaterPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	managerArgs := []string{}
	if startCPA {
		managerArgs = append(managerArgs, "--start-cpa")
	}
	encodedArgs, err := json.Marshal(managerArgs)
	if err != nil {
		return false, err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return false, err
	}
	command := exec.Command(updaterPath,
		"--recover-status", statusPath,
		"--manager-path", managerPath,
		"--manager-pid", strconv.Itoa(os.Getpid()),
		"--manager-args", string(encodedArgs),
		"--manager-working-directory", workingDirectory,
	)
	command.Dir = filepath.Dir(updaterPath)
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start recovery helper: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return false, fmt.Errorf("release recovery helper: %w", err)
	}
	log.Printf("interrupted update recovery helper started")
	return true, nil
}

func runtimeHealthBaseURL(configuredURL, setupURL string) string {
	for _, candidate := range []string{configuredURL, setupURL} {
		if normalized, ok := supervisor.LocalHealthURL(candidate); ok {
			return normalized
		}
	}
	return "http://127.0.0.1:8317"
}

func configureLocalCPA(controller *supervisor.Controller, cfg supervisor.Config, healthBaseURL string, forceStart bool, startLabel string) {
	if normalized, ok := supervisor.LocalHealthURL(cfg.HealthURL); ok {
		cfg.HealthURL = normalized
	} else if normalized, ok := supervisor.LocalHealthURL(healthBaseURL); ok {
		cfg.HealthURL = normalized
	} else {
		cfg.HealthURL = "http://127.0.0.1:8317"
	}
	if err := controller.Configure(cfg); err != nil {
		log.Printf("%s CLIProxyAPI configuration rejected: %v", startLabel, err)
		return
	}
	if !cfg.Enabled {
		return
	}
	if supervisor.HasHealthyLocalCPA(context.Background(), cfg.HealthURL) {
		controller.MarkExternal()
		log.Printf("CLIProxyAPI is already running outside CPA-Manager; automatic start skipped")
		return
	}
	if !cfg.AutoStart && !forceStart {
		return
	}
	status, err := controller.Start()
	if err != nil {
		log.Printf("%s CLIProxyAPI failed: %v", startLabel, err)
		return
	}
	log.Printf("CLIProxyAPI started with pid %d", status.PID)
}

func persistentArguments(arguments []string) []string {
	filtered := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if argument == "--start-cpa" {
			continue
		}
		filtered = append(filtered, argument)
	}
	return filtered
}

func runtimeConfigFromManagerConfig(managerCfg store.ManagerConfig, base config.Config) collector.RuntimeConfig {
	pollInterval := time.Duration(managerCfg.Collector.PollIntervalMS) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = base.PollInterval
	}
	batchSize := managerCfg.Collector.BatchSize
	if batchSize <= 0 {
		batchSize = base.BatchSize
	}
	return collector.RuntimeConfig{
		CPAUpstreamURL: managerCfg.CPAConnection.CPABaseURL,
		ManagementKey:  managerCfg.CPAConnection.ManagementKey,
		CollectorMode:  valueOr(managerCfg.Collector.CollectorMode, base.CollectorMode),
		Queue:          valueOr(managerCfg.Collector.Queue, base.Queue),
		PopSide:        valueOr(managerCfg.Collector.PopSide, base.PopSide),
		BatchSize:      batchSize,
		PollInterval:   pollInterval,
		TLSSkipVerify:  managerCfg.Collector.TLSSkipVerify,
	}
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func managerCollectorEnabled(managerCfg store.ManagerConfig) bool {
	return managerCfg.Collector.Enabled == nil || *managerCfg.Collector.Enabled
}

// buildAlertConfig merges env-based config with DB-persisted alert config.
// DB values take precedence when present.
func buildAlertConfig(s *store.Store, cfg config.Config) store.AlertConfigStored {
	result := store.AlertConfigStored{
		SMTPHost:          cfg.SMTPHost,
		SMTPPort:          cfg.SMTPPort,
		SMTPUsername:      cfg.SMTPUsername,
		SMTPPassword:      cfg.SMTPPassword,
		SMTPFrom:          cfg.SMTPFrom,
		SMTPFromName:      cfg.SMTPFromName,
		AlertEnabled:      cfg.AlertEnabled,
		ThresholdCents:    cfg.AlertThresholdCents,
		CheckIntervalMS:   int(cfg.AlertCheckInterval / time.Millisecond),
		PoolCheckEnabled:  false,
		PoolCheckInterval: 5,
	}

	dbCfg, ok, err := s.LoadAlertConfigWithPassword(context.Background())
	if err != nil {
		log.Printf("alert: load DB config failed: %v, using env defaults", err)
		return result
	}
	if !ok {
		return result
	}

	// DB values override env defaults (non-zero fields only for optional fields)
	if dbCfg.SMTPHost != "" {
		result.SMTPHost = dbCfg.SMTPHost
	}
	if dbCfg.SMTPPort > 0 {
		result.SMTPPort = dbCfg.SMTPPort
	}
	if dbCfg.SMTPUsername != "" {
		result.SMTPUsername = dbCfg.SMTPUsername
	}
	if dbCfg.SMTPPassword != "" {
		result.SMTPPassword = dbCfg.SMTPPassword
	}
	if dbCfg.SMTPFrom != "" {
		result.SMTPFrom = dbCfg.SMTPFrom
	}
	if dbCfg.SMTPFromName != "" {
		result.SMTPFromName = dbCfg.SMTPFromName
	}
	if dbCfg.ThresholdCents > 0 {
		result.ThresholdCents = dbCfg.ThresholdCents
	}
	if dbCfg.CheckIntervalMS > 0 {
		result.CheckIntervalMS = dbCfg.CheckIntervalMS
	}
	if dbCfg.PoolCheckInterval > 0 {
		result.PoolCheckInterval = dbCfg.PoolCheckInterval
	}

	// These are DB-only — always use DB value when present
	result.AlertEnabled = dbCfg.AlertEnabled
	result.PoolCheckEnabled = dbCfg.PoolCheckEnabled

	return result
}
