//go:build windows

package update

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func processMatchesExecutable(pid int, expectedPath string) (bool, error) {
	if pid <= 0 || strings.TrimSpace(expectedPath) == "" {
		return true, nil
	}
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, fmt.Errorf("inspect process %d executable: %w", pid, err)
	}
	row, err := csv.NewReader(strings.NewReader(string(output))).Read()
	if err != nil {
		return false, nil
	}
	if len(row) < 2 || row[1] != strconv.Itoa(pid) {
		return false, nil
	}
	actual, err := powershellExecutablePath(pid)
	if err != nil {
		return false, err
	}
	if actual == "" {
		return false, nil
	}
	expected, err := filepath.Abs(filepath.Clean(expectedPath))
	if err != nil {
		return false, fmt.Errorf("resolve expected process path: %w", err)
	}
	actual, err = filepath.Abs(filepath.Clean(actual))
	if err != nil {
		return false, fmt.Errorf("resolve actual process path: %w", err)
	}
	return strings.EqualFold(actual, expected), nil
}

func powershellExecutablePath(pid int) (string, error) {
	script := fmt.Sprintf("$p=Get-Process -Id %d -ErrorAction SilentlyContinue; if ($null -ne $p) { $p.Path }", pid)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("inspect process %d executable path: %w", pid, err)
	}
	return strings.TrimSpace(string(output)), nil
}
