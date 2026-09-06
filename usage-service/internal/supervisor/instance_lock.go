package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const instanceLockStaleAfter = 30 * time.Second

var errCPAInstanceBusy = errors.New("another CPA-Manager instance is starting or managing this CLIProxyAPI")

type cpaInstanceLock struct {
	path string
	file *os.File
}

type cpaInstanceLockOwner struct {
	PID            int    `json:"pid"`
	ExecutablePath string `json:"executablePath,omitempty"`
	StartedAtMS    int64  `json:"startedAtMs"`
}

func acquireCPAInstanceLock(executablePath string) (*cpaInstanceLock, error) {
	executablePath, err := filepath.Abs(filepath.Clean(executablePath))
	if err != nil {
		return nil, fmt.Errorf("resolve CPA instance lock executable: %w", err)
	}
	lockPath := executablePath + ".cpa-manager.lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create CPA instance lock directory: %w", err)
	}
	for {
		file, createErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if createErr == nil {
			owner, marshalErr := json.Marshal(cpaInstanceLockOwner{
				PID:            os.Getpid(),
				ExecutablePath: executablePath,
				StartedAtMS:    time.Now().UnixMilli(),
			})
			if marshalErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("encode CPA instance lock owner: %w", marshalErr)
			}
			if _, writeErr := file.Write(owner); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("write CPA instance lock owner: %w", writeErr)
			}
			if syncErr := file.Sync(); syncErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("sync CPA instance lock: %w", syncErr)
			}
			return &cpaInstanceLock{path: lockPath, file: file}, nil
		}
		if !os.IsExist(createErr) {
			return nil, fmt.Errorf("create CPA instance lock: %w", createErr)
		}

		owner, ownerOK, readErr := readCPAInstanceLockOwner(lockPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return nil, readErr
		}
		if ownerOK {
			running, runningErr := processIsRunning(owner.PID)
			if runningErr != nil {
				return nil, runningErr
			}
			if running {
				if owner.ExecutablePath == "" {
					return nil, errCPAInstanceBusy
				}
				matches, matchErr := processMatchesExecutable(owner.PID, owner.ExecutablePath)
				if matchErr != nil {
					return nil, matchErr
				}
				if matches {
					return nil, errCPAInstanceBusy
				}
			}
		} else if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) < instanceLockStaleAfter {
			return nil, errCPAInstanceBusy
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("stat CPA instance lock: %w", statErr)
		}
		if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("remove stale CPA instance lock: %w", removeErr)
		}
	}
}

func (l *cpaInstanceLock) Release() error {
	if l == nil {
		return nil
	}
	var releaseErr error
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			releaseErr = err
		}
		l.file = nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) && releaseErr == nil {
		releaseErr = err
	}
	return releaseErr
}

func readCPAInstanceLockOwner(path string) (cpaInstanceLockOwner, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return cpaInstanceLockOwner{}, false, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return cpaInstanceLockOwner{}, false, fmt.Errorf("read CPA instance lock: %w", err)
	}
	var owner cpaInstanceLockOwner
	if err := json.Unmarshal(data, &owner); err != nil || owner.PID <= 0 {
		return cpaInstanceLockOwner{}, false, nil
	}
	return owner, true, nil
}

func instanceLockError(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
