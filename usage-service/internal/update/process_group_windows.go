//go:build windows

package update

import (
	"os"
	"os/exec"
)

func IsolateProcessGroup() error {
	return nil
}

func configureProcessGroup(cmd *exec.Cmd) {
}

func signalProcessGroup(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func killProcessGroup(process *os.Process) error {
	return process.Kill()
}
