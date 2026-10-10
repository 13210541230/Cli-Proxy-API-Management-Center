package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuiteRuntimeStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "custom config.yaml")
	state, err := NewSuiteRuntimeState(root, configPath, 12, 34)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSuiteRuntimeState(root, state); err != nil {
		t.Fatal(err)
	}

	got, exists, err := ReadSuiteRuntimeState(root)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("runtime state not found")
	}
	if got.ConfigPath != state.ConfigPath || got.CPA.PID != 12 || got.Manager.PID != 34 {
		t.Fatalf("runtime state = %#v, want %#v", got, state)
	}
	if len(got.CPAArguments) != 2 || got.CPAArguments[0] != "--config" || got.CPAArguments[1] != state.ConfigPath {
		t.Fatalf("CPA arguments = %#v", got.CPAArguments)
	}
	if len(got.ManagerArguments) != 1 || got.ManagerArguments[0] != "--no-start-cpa" {
		t.Fatalf("manager arguments = %#v", got.ManagerArguments)
	}
	if got.CPAWorkingDirectory != root || got.ManagerWorkingDirectory != root {
		t.Fatalf("working directories = CPA:%q Manager:%q", got.CPAWorkingDirectory, got.ManagerWorkingDirectory)
	}
}

func TestSuiteRuntimeStateRejectsExecutableOutsideSuite(t *testing.T) {
	root := t.TempDir()
	state, err := NewSuiteRuntimeState(root, filepath.Join(root, "config.yaml"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	state.CPA.ExecutablePath = filepath.Join(t.TempDir(), "unrelated.exe")
	if err := WriteSuiteRuntimeState(root, state); err == nil {
		t.Fatal("expected external executable path to be rejected")
	}
}

func TestRuntimeProcessRunningChecksPIDExecutableIdentity(t *testing.T) {
	currentExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	running, err := RuntimeProcessRunning(ProcessIdentity{PID: os.Getpid(), ExecutablePath: currentExecutable})
	if err != nil || !running {
		t.Fatalf("RuntimeProcessRunning(current process) = (%v, %v), want (true, nil)", running, err)
	}

	running, err = RuntimeProcessRunning(ProcessIdentity{PID: os.Getpid(), ExecutablePath: filepath.Join(t.TempDir(), "different-executable")})
	if err == nil || running {
		t.Fatalf("RuntimeProcessRunning() = (%v, %v), want identity mismatch", running, err)
	}
}

func TestEnsureSuiteStoppedAllowsMissingRuntimeState(t *testing.T) {
	if err := EnsureSuiteStopped(t.TempDir()); err != nil {
		t.Fatalf("EnsureSuiteStopped() error = %v", err)
	}
}

func TestReadSuiteRuntimeStateRejectsUnknownSchema(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"schema":99,"cpa":{"pid":0},"manager":{"pid":0},"configPath":"config.yaml"}`)
	if err := os.WriteFile(SuiteRuntimeStatePath(root), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSuiteRuntimeState(root); err == nil {
		t.Fatal("expected unknown runtime schema to be rejected")
	}
}
