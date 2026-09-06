//go:build !windows

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func processIsRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
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

func processMatchesExecutable(pid int, expectedPath string) (bool, error) {
	if pid <= 0 || strings.TrimSpace(expectedPath) == "" {
		return true, nil
	}
	expected, err := filepath.Abs(filepath.Clean(expectedPath))
	if err != nil {
		return false, fmt.Errorf("resolve expected process path: %w", err)
	}
	if runtime.GOOS == "linux" {
		actual, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read process %d executable: %w", pid, err)
		}
		actual, err = filepath.Abs(filepath.Clean(actual))
		if err != nil {
			return false, err
		}
		return actual == expected, nil
	}

	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect process %d executable: %w", pid, err)
	}
	command := strings.TrimSpace(string(output))
	if command == "" {
		return false, nil
	}
	actual := strings.Fields(command)[0]
	if !filepath.IsAbs(actual) {
		return false, nil
	}
	actual, err = filepath.Abs(filepath.Clean(actual))
	if err != nil {
		return false, err
	}
	return actual == expected, nil
}
