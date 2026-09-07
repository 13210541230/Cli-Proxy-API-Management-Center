package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	StateStopped  = "stopped"
	StateRunning  = "running"
	StateFailed   = "failed"
	StateStarting = "starting"
	StateStopping = "stopping"
	StateExternal = "external"

	HealthUnknown   = "unknown"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

// LocalHealthURL normalizes a configured local CPA base URL and rejects
// remote endpoints. Wildcard bind addresses are probed through loopback.
func LocalHealthURL(baseURL string) (string, bool) {
	baseURL = strings.TrimSpace(baseURL)
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	case "0.0.0.0", "::":
		// A wildcard bind address is local configuration, not a remote host.
		parsed.Host = net.JoinHostPort("127.0.0.1", parsed.Port())
	default:
		return "", false
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

// HasHealthyLocalCPA reports whether a local CPA endpoint is already serving.
// It is used only during migration from manually started CPA.
func HasHealthyLocalCPA(ctx context.Context, baseURL string) bool {
	baseURL, ok := LocalHealthURL(baseURL)
	if !ok {
		return false
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/healthz"
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return false
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return false
	}
	return payload.Status == "ok"
}

// Config describes the locally managed CLIProxyAPI process.
type Config struct {
	Enabled           bool
	CPAExecutablePath string
	WorkingDirectory  string
	Arguments         []string
	AutoStart         bool
	// HealthURL is a runtime-only guard used to avoid starting a second local
	// CPA when one was started outside CPA-Manager.
	HealthURL string
}

// Status is the observable state of the locally managed process.
type Status struct {
	State            string   `json:"state"`
	Running          bool     `json:"running"`
	Managed          bool     `json:"managed"`
	PID              int      `json:"pid,omitempty"`
	ExecutablePath   string   `json:"executablePath,omitempty"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	Arguments        []string `json:"arguments,omitempty"`
	StartedAtMS      int64    `json:"startedAtMs,omitempty"`
	LastExitAtMS     int64    `json:"lastExitAtMs,omitempty"`
	LastError        string   `json:"lastError,omitempty"`
	Health           string   `json:"health"`
	Healthy          bool     `json:"healthy"`
	External         bool     `json:"external"`
	Enabled          bool     `json:"enabled"`
	AutoStart        bool     `json:"autoStart"`
}

// Controller owns only processes started through this instance. It never
// searches for or terminates an unrelated process with the same executable.
type Controller struct {
	mu           sync.Mutex
	config       Config
	cmd          *exec.Cmd
	state        string
	startedAtMS  int64
	lastExitMS   int64
	lastError    string
	health       string
	stopping     bool
	external     bool
	instanceLock *cpaInstanceLock
	processGroup *processGroup
}

func New() *Controller {
	return &Controller{state: StateStopped, health: HealthUnknown}
}

func (c *Controller) Configure(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.canConfigureLocked(cfg); err != nil {
		return err
	}
	c.config = cloneConfig(cfg)
	return nil
}

func (c *Controller) CanConfigure(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canConfigureLocked(cfg)
}

func (c *Controller) canConfigureLocked(cfg Config) error {
	if c.cmd != nil && c.cmd.ProcessState == nil && !sameProcessConfig(c.config, cfg) {
		return errors.New("stop CLIProxyAPI before changing its local runtime configuration")
	}
	return nil
}

// MarkExternal records that a CPA health endpoint is already available but
// the process was not started by this controller. It is never stopped or
// replaced automatically.
func (c *Controller) MarkExternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil {
		c.external = true
		c.state = StateExternal
		c.health = HealthHealthy
		c.lastError = ""
	}
}

// ClearExternal clears a previously detected unmanaged process after the
// caller has confirmed that the external process is no longer healthy.
func (c *Controller) ClearExternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil && c.external {
		c.external = false
		c.state = StateStopped
		c.health = HealthUnknown
	}
}

func (c *Controller) Config() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneConfig(c.config)
}

func (c *Controller) Start() (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil && c.cmd.ProcessState == nil {
		return c.statusLocked(), nil
	}
	if c.external {
		return c.statusLocked(), errors.New("an externally started CLIProxyAPI is already running; stop it manually before starting a managed process")
	}
	if !c.config.Enabled {
		return c.statusLocked(), errors.New("local CPA runtime is disabled")
	}
	executable, workingDirectory, err := ResolveConfig(c.config)
	if err != nil {
		c.state = StateFailed
		c.lastError = err.Error()
		return c.statusLocked(), err
	}
	instanceLock, err := acquireCPAInstanceLock(executable)
	if err != nil {
		c.state = StateFailed
		c.lastError = instanceLockError(err)
		return c.statusLocked(), err
	}
	if strings.TrimSpace(c.config.HealthURL) != "" && HasHealthyLocalCPA(context.Background(), c.config.HealthURL) {
		if releaseErr := instanceLock.Release(); releaseErr != nil {
			c.lastError = releaseErr.Error()
		}
		c.external = true
		c.state = StateExternal
		c.health = HealthHealthy
		c.lastError = ""
		return c.statusLocked(), errors.New("an externally started CLIProxyAPI is already running")
	}

	cmd := exec.Command(executable, c.config.Arguments...)
	cmd.Dir = workingDirectory
	if err := cmd.Start(); err != nil {
		_ = instanceLock.Release()
		wrapped := fmt.Errorf("start CLIProxyAPI: %w", err)
		c.state = StateFailed
		c.lastError = wrapped.Error()
		return c.statusLocked(), wrapped
	}
	processGroup, err := newProcessGroup()
	if err == nil {
		err = processGroup.Assign(cmd.Process)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = instanceLock.Release()
		if processGroup != nil {
			_ = processGroup.Close()
		}
		wrapped := fmt.Errorf("isolate CLIProxyAPI process tree: %w", err)
		c.state = StateFailed
		c.lastError = wrapped.Error()
		return c.statusLocked(), wrapped
	}

	c.instanceLock = instanceLock
	c.processGroup = processGroup
	c.cmd = cmd
	c.state = StateRunning
	c.health = HealthUnknown
	c.startedAtMS = time.Now().UnixMilli()
	c.lastError = ""
	c.stopping = false
	go c.wait(cmd)
	go c.monitorHealth(cmd)
	return c.statusLocked(), nil
}

func (c *Controller) Stop() (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.external {
		return c.statusLocked(), errors.New("CLIProxyAPI is running outside CPA-Manager and cannot be stopped automatically")
	}
	if c.cmd == nil || c.cmd.ProcessState != nil {
		return c.statusLocked(), nil
	}

	c.state = StateStopping
	c.stopping = true
	var stopErr error
	if runtime.GOOS == "windows" {
		if c.processGroup != nil {
			stopErr = c.processGroup.Terminate()
		} else {
			stopErr = c.cmd.Process.Kill()
		}
	} else {
		stopErr = c.cmd.Process.Signal(syscall.SIGTERM)
	}
	if stopErr != nil {
		wrapped := fmt.Errorf("stop CLIProxyAPI: %w", stopErr)
		c.state = StateFailed
		c.lastError = wrapped.Error()
		c.stopping = false
		return c.statusLocked(), wrapped
	}
	return c.statusLocked(), nil
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *Controller) WaitStopped(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status := c.Status()
		if !status.Running {
			return nil
		}
		if timeout > 0 && time.Now().After(deadline) {
			return errors.New("timed out waiting for CLIProxyAPI to stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *Controller) wait(cmd *exec.Cmd) {
	err := cmd.Wait()

	c.mu.Lock()
	if c.cmd != cmd {
		c.mu.Unlock()
		return
	}
	instanceLock := c.instanceLock
	c.instanceLock = nil
	processGroup := c.processGroup
	c.processGroup = nil
	c.cmd = nil
	c.lastExitMS = time.Now().UnixMilli()
	c.health = HealthUnknown
	if err != nil && !c.stopping {
		c.state = StateFailed
		c.lastError = err.Error()
	} else {
		c.state = StateStopped
		c.lastError = ""
	}
	c.stopping = false
	c.mu.Unlock()
	if releaseErr := instanceLock.Release(); releaseErr != nil {
		c.mu.Lock()
		if c.lastError == "" {
			c.lastError = fmt.Sprintf("release CPA instance lock: %v", releaseErr)
		}
		c.mu.Unlock()
	}
	if closeErr := processGroup.Close(); closeErr != nil {
		c.mu.Lock()
		if c.lastError == "" {
			c.lastError = closeErr.Error()
		}
		c.mu.Unlock()
	}
}

const (
	healthCheckError    = "CLIProxyAPI health check failed"
	healthCheckInterval = 1 * time.Second
	healthCheckDelay    = 100 * time.Millisecond
)

func (c *Controller) monitorHealth(cmd *exec.Cmd) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	firstCheck := time.NewTimer(healthCheckDelay)
	defer firstCheck.Stop()
	for {
		select {
		case <-firstCheck.C:
		case <-ticker.C:
		}
		c.mu.Lock()
		if c.cmd != cmd {
			c.mu.Unlock()
			return
		}
		healthURL := c.config.HealthURL
		c.mu.Unlock()

		healthy := healthURL != "" && HasHealthyLocalCPA(context.Background(), healthURL)

		c.mu.Lock()
		if c.cmd != cmd {
			c.mu.Unlock()
			return
		}
		if healthy {
			c.health = HealthHealthy
			if c.lastError == healthCheckError {
				c.lastError = ""
			}
		} else if healthURL != "" {
			c.health = HealthUnhealthy
			c.lastError = healthCheckError
		} else {
			c.health = HealthUnknown
		}
		c.mu.Unlock()
	}
}

func (c *Controller) statusLocked() Status {
	status := Status{
		State:     c.state,
		Enabled:   c.config.Enabled,
		AutoStart: c.config.AutoStart,
		Health:    c.health,
		Healthy:   c.health == HealthHealthy,
		External:  c.external,
	}
	if status.State == "" {
		status.State = StateStopped
	}
	status.ExecutablePath, status.WorkingDirectory, _ = ResolveConfig(c.config)
	status.Arguments = append([]string(nil), c.config.Arguments...)
	status.Managed = c.cmd != nil && c.cmd.ProcessState == nil
	status.Running = status.Managed || c.external
	if status.Managed {
		status.PID = c.cmd.Process.Pid
		status.StartedAtMS = c.startedAtMS
	}
	status.LastExitAtMS = c.lastExitMS
	status.LastError = c.lastError
	return status
}

// DefaultConfig enables supervision when the unified package places
// cli-proxy-api beside cpa-manager.
func DefaultConfig() (Config, bool) {
	baseDir, err := executableDirectory()
	if err != nil {
		return Config{}, false
	}
	name := "cli-proxy-api"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(baseDir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return Config{}, false
	}
	return Config{
		Enabled:           true,
		CPAExecutablePath: path,
		WorkingDirectory:  baseDir,
		AutoStart:         true,
	}, true
}

func ResolveConfig(cfg Config) (string, string, error) {
	baseDir, err := executableDirectory()
	if err != nil {
		return "", "", err
	}

	executable := strings.TrimSpace(cfg.CPAExecutablePath)
	if executable == "" {
		name := "cli-proxy-api"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		executable = filepath.Join(baseDir, name)
	} else if !filepath.IsAbs(executable) {
		executable = filepath.Join(baseDir, executable)
	}
	executable, err = filepath.Abs(filepath.Clean(executable))
	if err != nil {
		return "", "", fmt.Errorf("resolve CLIProxyAPI path: %w", err)
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", "", fmt.Errorf("stat CLIProxyAPI executable %s: %w", executable, err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("CLIProxyAPI executable path is a directory: %s", executable)
	}

	workingDirectory := strings.TrimSpace(cfg.WorkingDirectory)
	if workingDirectory == "" {
		workingDirectory = filepath.Dir(executable)
	} else if !filepath.IsAbs(workingDirectory) {
		workingDirectory = filepath.Join(baseDir, workingDirectory)
	}
	workingDirectory, err = filepath.Abs(filepath.Clean(workingDirectory))
	if err != nil {
		return "", "", fmt.Errorf("resolve CLIProxyAPI working directory: %w", err)
	}
	if info, err := os.Stat(workingDirectory); err != nil {
		return "", "", fmt.Errorf("stat CLIProxyAPI working directory %s: %w", workingDirectory, err)
	} else if !info.IsDir() {
		return "", "", fmt.Errorf("CLIProxyAPI working directory is not a directory: %s", workingDirectory)
	}
	return executable, workingDirectory, nil
}

func executableDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve CPA-Manager executable: %w", err)
	}
	return filepath.Dir(executable), nil
}

func sameProcessConfig(left, right Config) bool {
	if left.Enabled != right.Enabled ||
		left.CPAExecutablePath != right.CPAExecutablePath ||
		left.WorkingDirectory != right.WorkingDirectory ||
		left.AutoStart != right.AutoStart ||
		len(left.Arguments) != len(right.Arguments) {
		return false
	}
	for i := range left.Arguments {
		if left.Arguments[i] != right.Arguments[i] {
			return false
		}
	}
	return true
}

func cloneConfig(cfg Config) Config {
	cfg.Arguments = append([]string(nil), cfg.Arguments...)
	return cfg
}
