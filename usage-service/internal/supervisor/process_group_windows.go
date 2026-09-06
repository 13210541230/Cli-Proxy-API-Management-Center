//go:build windows

package supervisor

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGroup is a Windows Job Object. KILL_ON_JOB_CLOSE ensures that a CPA
// child tree cannot outlive the supervisor that owns it.
type processGroup struct {
	handle windows.Handle
}

func newProcessGroup() (*processGroup, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create CPA job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("configure CPA job object: %w", err)
	}
	return &processGroup{handle: handle}, nil
}

func (g *processGroup) Assign(process *os.Process) error {
	if g == nil || g.handle == 0 || process == nil {
		return fmt.Errorf("CPA job object or process is unavailable")
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open CPA process for job assignment: %w", err)
	}
	defer func() { _ = windows.CloseHandle(processHandle) }()
	if err := windows.AssignProcessToJobObject(g.handle, processHandle); err != nil {
		return fmt.Errorf("assign CPA process to job object: %w", err)
	}
	return nil
}

func (g *processGroup) Terminate() error {
	if g == nil || g.handle == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(g.handle, 1); err != nil {
		return fmt.Errorf("terminate CPA job object: %w", err)
	}
	return nil
}

func (g *processGroup) Close() error {
	if g == nil || g.handle == 0 {
		return nil
	}
	handle := g.handle
	g.handle = 0
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close CPA job object: %w", err)
	}
	return nil
}

// IsolateProcessGroup is retained for the manager process itself. CPA child
// lifecycle is controlled by processGroup above.
func IsolateProcessGroup() error {
	return nil
}
