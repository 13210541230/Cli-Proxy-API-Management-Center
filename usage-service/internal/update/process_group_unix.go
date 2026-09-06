//go:build !windows

package update

import (
	"os"
	"os/exec"
	"syscall"
)

func IsolateProcessGroup() error {
	return syscall.Setpgid(0, 0)
}

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessGroup(process *os.Process) error {
	if err := syscall.Kill(-process.Pid, syscall.SIGTERM); err == nil {
		return nil
	}
	return process.Signal(syscall.SIGTERM)
}

func killProcessGroup(process *os.Process) error {
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return process.Kill()
}
