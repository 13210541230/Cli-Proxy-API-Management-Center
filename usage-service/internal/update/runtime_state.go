package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const suiteRuntimeStateSchema = 1
const suiteRuntimeStateFilename = ".suite-runtime.json"

type ProcessIdentity struct {
	PID            int    `json:"pid"`
	ExecutablePath string `json:"executablePath"`
}

type SuiteRuntimeState struct {
	Schema                  int             `json:"schema"`
	CPA                     ProcessIdentity `json:"cpa"`
	Manager                 ProcessIdentity `json:"manager"`
	CPAArguments            []string        `json:"cpaArguments"`
	ManagerArguments        []string        `json:"managerArguments"`
	CPAWorkingDirectory     string          `json:"cpaWorkingDirectory"`
	ManagerWorkingDirectory string          `json:"managerWorkingDirectory"`
	ConfigPath              string          `json:"configPath"`
	UpdatedAtMS             int64           `json:"updatedAtMs"`
}

func SuiteRuntimeStatePath(root string) string {
	return filepath.Join(root, suiteRuntimeStateFilename)
}

func NewSuiteRuntimeState(root, configPath string, cpaPID, managerPID int) (SuiteRuntimeState, error) {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return SuiteRuntimeState{}, fmt.Errorf("resolve suite directory: %w", err)
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, "config.yaml")
	}
	configPath, err = filepath.Abs(filepath.Clean(configPath))
	if err != nil {
		return SuiteRuntimeState{}, fmt.Errorf("resolve CPA config path: %w", err)
	}
	cpaName, managerName := "cli-proxy-api", "cpa-manager"
	if runtime.GOOS == "windows" {
		cpaName += ".exe"
		managerName += ".exe"
	}
	return SuiteRuntimeState{
		Schema: suiteRuntimeStateSchema,
		CPA: ProcessIdentity{
			PID:            cpaPID,
			ExecutablePath: filepath.Join(root, cpaName),
		},
		Manager: ProcessIdentity{
			PID:            managerPID,
			ExecutablePath: filepath.Join(root, managerName),
		},
		CPAArguments:            []string{"--config", configPath},
		ManagerArguments:        []string{"--no-start-cpa"},
		CPAWorkingDirectory:     root,
		ManagerWorkingDirectory: root,
		ConfigPath:              configPath,
		UpdatedAtMS:             time.Now().UnixMilli(),
	}, nil
}

func WriteSuiteRuntimeState(root string, state SuiteRuntimeState) error {
	path, err := filepath.Abs(SuiteRuntimeStatePath(root))
	if err != nil {
		return fmt.Errorf("resolve suite runtime state path: %w", err)
	}
	if state.Schema == 0 {
		state.Schema = suiteRuntimeStateSchema
	}
	if state.Schema != suiteRuntimeStateSchema {
		return fmt.Errorf("unsupported suite runtime state schema %d", state.Schema)
	}
	stateRoot := filepath.Dir(path)
	if err := validateRuntimeExecutablePaths(stateRoot, &state); err != nil {
		return err
	}
	if state.CPA.PID < 0 || state.Manager.PID < 0 {
		return errors.New("suite runtime process IDs cannot be negative")
	}
	if strings.TrimSpace(state.ConfigPath) == "" {
		return errors.New("suite runtime config path is empty")
	}
	state.ConfigPath, err = filepath.Abs(filepath.Clean(state.ConfigPath))
	if err != nil {
		return fmt.Errorf("resolve suite runtime config path: %w", err)
	}
	if len(state.CPAArguments) == 0 {
		state.CPAArguments = []string{"--config", state.ConfigPath}
	}
	if len(state.ManagerArguments) == 0 {
		state.ManagerArguments = []string{"--no-start-cpa"}
	}
	if strings.TrimSpace(state.CPAWorkingDirectory) == "" {
		state.CPAWorkingDirectory = stateRoot
	}
	if strings.TrimSpace(state.ManagerWorkingDirectory) == "" {
		state.ManagerWorkingDirectory = stateRoot
	}
	if state.UpdatedAtMS == 0 {
		state.UpdatedAtMS = time.Now().UnixMilli()
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode suite runtime state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create suite runtime state directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write suite runtime state: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit suite runtime state: %w", err)
	}
	return nil
}

func ReadSuiteRuntimeState(root string) (SuiteRuntimeState, bool, error) {
	path := SuiteRuntimeStatePath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SuiteRuntimeState{}, false, nil
	}
	if err != nil {
		return SuiteRuntimeState{}, false, fmt.Errorf("read suite runtime state: %w", err)
	}
	var state SuiteRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return SuiteRuntimeState{}, false, fmt.Errorf("decode suite runtime state: %w", err)
	}
	if state.Schema != suiteRuntimeStateSchema {
		return SuiteRuntimeState{}, false, fmt.Errorf("unsupported suite runtime state schema %d", state.Schema)
	}
	if err := validateRuntimeExecutablePaths(root, &state); err != nil {
		return SuiteRuntimeState{}, false, err
	}
	if strings.TrimSpace(state.ConfigPath) == "" || state.CPA.PID < 0 || state.Manager.PID < 0 {
		return SuiteRuntimeState{}, false, errors.New("suite runtime state is incomplete")
	}
	if !filepath.IsAbs(state.ConfigPath) {
		state.ConfigPath = filepath.Join(root, state.ConfigPath)
	}
	state.ConfigPath, err = filepath.Abs(filepath.Clean(state.ConfigPath))
	if err != nil {
		return SuiteRuntimeState{}, false, fmt.Errorf("resolve suite runtime config path: %w", err)
	}
	if len(state.CPAArguments) == 0 {
		state.CPAArguments = []string{"--config", state.ConfigPath}
	}
	if len(state.ManagerArguments) == 0 {
		state.ManagerArguments = []string{"--no-start-cpa"}
	}
	if strings.TrimSpace(state.CPAWorkingDirectory) == "" {
		state.CPAWorkingDirectory = filepath.Dir(path)
	}
	if strings.TrimSpace(state.ManagerWorkingDirectory) == "" {
		state.ManagerWorkingDirectory = filepath.Dir(path)
	}
	return state, true, nil
}

func RuntimeProcessRunning(identity ProcessIdentity) (bool, error) {
	if identity.PID <= 0 {
		return false, nil
	}
	if strings.TrimSpace(identity.ExecutablePath) == "" {
		return false, errors.New("runtime process executable path is empty")
	}
	running, err := processIsRunning(identity.PID)
	if err != nil || !running {
		return running, err
	}
	matches, err := processMatchesExecutable(identity.PID, identity.ExecutablePath)
	if err != nil {
		return false, err
	}
	if !matches {
		return false, fmt.Errorf("refusing to manage process %d: executable identity mismatch", identity.PID)
	}
	return true, nil
}

func validateRuntimeExecutablePaths(root string, state *SuiteRuntimeState) error {
	if state == nil {
		return errors.New("suite runtime state is nil")
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return fmt.Errorf("resolve suite directory: %w", err)
	}
	cpaName, managerName := "cli-proxy-api", "cpa-manager"
	if runtime.GOOS == "windows" {
		cpaName += ".exe"
		managerName += ".exe"
	}
	for _, expected := range []struct {
		identity *ProcessIdentity
		name     string
		label    string
	}{
		{identity: &state.CPA, name: cpaName, label: "CLIProxyAPI"},
		{identity: &state.Manager, name: managerName, label: "CPA-Manager"},
	} {
		expectedPath := filepath.Join(root, expected.name)
		if expected.identity.ExecutablePath == "" {
			expected.identity.ExecutablePath = expectedPath
			continue
		}
		actualPath, pathErr := filepath.Abs(filepath.Clean(expected.identity.ExecutablePath))
		if pathErr != nil {
			return fmt.Errorf("resolve %s executable path: %w", expected.label, pathErr)
		}
		if !samePath(actualPath, expectedPath) {
			return fmt.Errorf("refusing to manage %s outside suite directory", expected.label)
		}
		expected.identity.ExecutablePath = expectedPath
	}
	return nil
}

func EnsureSuiteStopped(root string) error {
	state, exists, err := ReadSuiteRuntimeState(root)
	if err != nil || !exists {
		return err
	}
	managerRunning, err := RuntimeProcessRunning(state.Manager)
	if err != nil {
		return err
	}
	cpaRunning, err := RuntimeProcessRunning(state.CPA)
	if err != nil {
		return err
	}
	if managerRunning || cpaRunning {
		return errors.New("CLIProxyAPI suite is already running; use stop.bat or stop.sh first")
	}
	return nil
}

func StopSuite(root string) error {
	state, exists, err := ReadSuiteRuntimeState(root)
	if err != nil || !exists {
		return err
	}
	// Validate both identities before terminating either process so stale or
	// corrupted PID state can never stop an unrelated process.
	managerRunning, err := RuntimeProcessRunning(state.Manager)
	if err != nil {
		return err
	}
	cpaRunning, err := RuntimeProcessRunning(state.CPA)
	if err != nil {
		return err
	}
	if managerRunning {
		if err := terminateProcessByPID(state.Manager.PID, state.Manager.ExecutablePath); err != nil {
			return fmt.Errorf("stop CPA-Manager: %w", err)
		}
	}
	if cpaRunning {
		if err := terminateProcessByPID(state.CPA.PID, state.CPA.ExecutablePath); err != nil {
			return fmt.Errorf("stop CLIProxyAPI: %w", err)
		}
	}
	state.Manager.PID = 0
	state.CPA.PID = 0
	state.UpdatedAtMS = time.Now().UnixMilli()
	if err := WriteSuiteRuntimeState(root, state); err != nil {
		return fmt.Errorf("clear stopped suite process IDs: %w", err)
	}
	return nil
}
