//go:build !windows

package supervisor

import (
	"os"
	"syscall"
)

type processGroup struct{}

func newProcessGroup() (*processGroup, error) {
	return &processGroup{}, nil
}

func (g *processGroup) Assign(process *os.Process) error {
	return nil
}

func (g *processGroup) Terminate() error {
	return nil
}

func (g *processGroup) Close() error {
	return nil
}

// IsolateProcessGroup makes the manager and its children addressable as one
// lifecycle group for updater recovery on Unix systems.
func IsolateProcessGroup() error {
	return syscall.Setpgid(0, 0)
}
