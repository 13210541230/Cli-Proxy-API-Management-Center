package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type StageStatus struct {
	State         string            `json:"state"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	TransactionID string            `json:"transactionId,omitempty"`
	Manifest      Manifest          `json:"manifest"`
	Asset         Asset             `json:"asset"`
	ArchivePath   string            `json:"archivePath,omitempty"`
	StagingPath   string            `json:"stagingPath,omitempty"`
	Backups       []PersistedBackup `json:"backups,omitempty"`
	ManagerPath   string            `json:"managerPath,omitempty"`
	CPAPath       string            `json:"cpaPath,omitempty"`
	StartedAtMS   int64             `json:"startedAtMs"`
	CompletedAtMS int64             `json:"completedAtMs,omitempty"`
	Error         string            `json:"error,omitempty"`
}

const (
	StageIdle        = "idle"
	StageDownloading = "downloading"
	StageReady       = "ready"
	StageApplying    = "applying"
	StageFailed      = "failed"
)

type Stager struct {
	client     *Client
	statusPath string
	mu         sync.Mutex
	job        StageStatus
	stageLock  *transactionLock
	applyLock  *transactionLock
}

func NewStager(client *Client, statusPaths ...string) *Stager {
	if client == nil {
		client = NewClient()
	}
	statusPath := ""
	if len(statusPaths) > 0 {
		statusPath = statusPaths[0]
	}
	stager := &Stager{client: client, statusPath: statusPath, job: StageStatus{State: StageIdle}}
	stager.loadPersisted()
	return stager
}

func (s *Stager) Status() StageStatus {
	s.mu.Lock()
	status := s.job
	s.mu.Unlock()
	if persisted, ok, err := ReadPersistedStatus(s.statusPath); err == nil && ok && isTerminalStage(persisted.State) {
		var stageLock, applyLock *transactionLock
		s.mu.Lock()
		if persisted.CompletedAtMS >= s.job.CompletedAtMS {
			s.job.State = persisted.State
			s.job.TransactionID = persisted.TransactionID
			s.job.OS = persisted.OS
			s.job.Arch = persisted.Arch
			s.job.Manifest.CPAVersion = persisted.CPAVersion
			s.job.Manifest.ManagerVersion = persisted.ManagerVersion
			s.job.Backups = append([]PersistedBackup(nil), persisted.Backups...)
			s.job.Error = persisted.Error
			s.job.CompletedAtMS = persisted.CompletedAtMS
			status = s.job
			stageLock = s.stageLock
			s.stageLock = nil
			applyLock = s.applyLock
			s.applyLock = nil
		}
		s.mu.Unlock()
		if stageLock != nil {
			_ = stageLock.Release()
		}
		if applyLock != nil {
			_ = applyLock.Release()
		}
	}
	return status
}

func (s *Stager) BeginApply() (StageStatus, error) {
	s.mu.Lock()
	if s.job.State != StageReady {
		s.mu.Unlock()
		return StageStatus{}, errors.New("no verified update is ready to apply")
	}
	transactionID := s.job.TransactionID
	stageLock := s.stageLock
	stageLockOwned := stageLock != nil
	s.mu.Unlock()

	if stageLock == nil {
		var acquireErr error
		stageLock, acquireErr = acquireTransactionLock(s.statusPath, transactionID, 0)
		if acquireErr != nil {
			return StageStatus{}, acquireErr
		}
	}

	s.mu.Lock()
	if s.job.State != StageReady || s.job.TransactionID != transactionID {
		s.mu.Unlock()
		if !stageLockOwned {
			_ = stageLock.Release()
		}
		return StageStatus{}, errors.New("the staged update changed before apply")
	}
	s.stageLock = nil
	s.applyLock = stageLock
	s.job.State = StageApplying
	s.job.CompletedAtMS = 0
	s.job.Error = ""
	result := s.job
	s.mu.Unlock()
	if err := s.persist(result); err != nil {
		s.mu.Lock()
		s.job.State = StageFailed
		s.job.CompletedAtMS = time.Now().UnixMilli()
		s.job.Error = fmt.Sprintf("persist applying update: %v", err)
		result = s.job
		applyLock := s.applyLock
		s.applyLock = nil
		s.mu.Unlock()
		_ = applyLock.Release()
		return StageStatus{}, err
	}
	return result, nil
}

func (s *Stager) Fail(err error) error {
	s.mu.Lock()
	s.job.State = StageFailed
	s.job.CompletedAtMS = time.Now().UnixMilli()
	if err != nil {
		s.job.Error = err.Error()
	}
	result := s.job
	stageLock := s.stageLock
	s.stageLock = nil
	applyLock := s.applyLock
	s.applyLock = nil
	s.mu.Unlock()
	persistErr := s.persist(result)
	if releaseErr := stageLock.Release(); persistErr == nil {
		persistErr = releaseErr
	}
	if releaseErr := applyLock.Release(); persistErr == nil {
		persistErr = releaseErr
	}
	return persistErr
}

func (s *Stager) TransferApplyLock(pid int, transactionID, executablePath string) error {
	s.mu.Lock()
	if s.job.State != StageApplying || s.job.TransactionID != transactionID || s.applyLock == nil {
		s.mu.Unlock()
		return errors.New("update apply lock is not transferable")
	}
	applyLock := s.applyLock
	running, err := processIsRunning(pid)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !running {
		s.mu.Unlock()
		return errors.New("update helper exited before lock transfer")
	}
	if matches, matchErr := processMatchesExecutable(pid, executablePath); matchErr != nil {
		s.mu.Unlock()
		return matchErr
	} else if !matches {
		s.mu.Unlock()
		return errors.New("update helper executable identity mismatch")
	}
	if err := applyLock.Transfer(pid, transactionID, executablePath); err != nil {
		s.mu.Unlock()
		return err
	}
	s.applyLock = nil
	s.mu.Unlock()
	return nil
}

func (s *Stager) ReleaseApplyLock() error {
	s.mu.Lock()
	applyLock := s.applyLock
	s.applyLock = nil
	s.mu.Unlock()
	return applyLock.Release()
}

func (s *Stager) Stage(ctx context.Context, goos, goarch string) (StageStatus, error) {
	transactionID := newTransactionID()
	stageLock, err := acquireTransactionLock(s.statusPath, transactionID, 0)
	if err != nil {
		return StageStatus{}, err
	}

	keepLock := false
	defer func() {
		if !keepLock {
			_ = stageLock.Release()
		}
	}()

	s.mu.Lock()
	if s.job.State == StageReady || s.stageLock != nil || s.applyLock != nil {
		s.mu.Unlock()
		return StageStatus{}, errors.New("an update is already staged or being applied")
	}
	persisted, persistedOK, persistedErr := ReadPersistedStatus(s.statusPath)
	if persistedErr != nil && !os.IsNotExist(persistedErr) {
		s.mu.Unlock()
		return StageStatus{}, persistedErr
	}
	if persistedOK && (persisted.State == StageDownloading || persisted.State == StageReady || persisted.State == StageApplying) {
		if persisted.StagingPath != "" {
			if cleanupErr := removeStagingPath(persisted.StagingPath); cleanupErr != nil {
				s.mu.Unlock()
				return StageStatus{}, cleanupErr
			}
		}
	}
	startedAt := time.Now().UnixMilli()
	s.job = StageStatus{State: StageDownloading, OS: goos, Arch: goarch, TransactionID: transactionID, StartedAtMS: startedAt}
	result := s.job
	s.mu.Unlock()
	if err := s.persist(result); err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, fmt.Errorf("persist downloading update: %w", err))
	}

	manifest, err := s.client.CheckLatest(ctx)
	if err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, err)
	}
	asset, err := manifest.AssetFor(goos, goarch)
	if err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, err)
	}
	root, err := os.MkdirTemp("", "cpa-manager-update-")
	if err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, fmt.Errorf("create update staging directory: %w", err))
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(root)
		}
	}()
	archivePath := filepath.Join(root, asset.Name)
	if err := s.client.DownloadAsset(ctx, asset, archivePath); err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, err)
	}
	extractPath := filepath.Join(root, "extracted")
	if err := ExtractArchive(archivePath, extractPath); err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, err)
	}
	files, err := LocateBundle(extractPath)
	if err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, err)
	}
	result = StageStatus{
		State:         StageReady,
		OS:            goos,
		Arch:          goarch,
		TransactionID: transactionID,
		Manifest:      manifest,
		Asset:         asset,
		ArchivePath:   archivePath,
		StagingPath:   extractPath,
		ManagerPath:   files.ManagerPath,
		CPAPath:       files.CPAPath,
		StartedAtMS:   startedAt,
		CompletedAtMS: time.Now().UnixMilli(),
	}
	s.mu.Lock()
	s.job = result
	s.mu.Unlock()
	if err := s.persist(result); err != nil {
		return s.failStage(goos, goarch, startedAt, transactionID, fmt.Errorf("persist ready update: %w", err))
	}
	s.mu.Lock()
	s.stageLock = stageLock
	s.mu.Unlock()
	keepStaging = true
	keepLock = true
	return result, nil
}

func (s *Stager) failStage(goos, goarch string, startedAt int64, transactionID string, err error) (StageStatus, error) {
	result := StageStatus{State: StageFailed, OS: goos, Arch: goarch, TransactionID: transactionID, StartedAtMS: startedAt, CompletedAtMS: time.Now().UnixMilli(), Error: err.Error()}
	s.mu.Lock()
	s.job = result
	stageLock := s.stageLock
	s.stageLock = nil
	applyLock := s.applyLock
	s.applyLock = nil
	s.mu.Unlock()
	if persistErr := s.persist(result); persistErr != nil {
		return result, errors.Join(err, fmt.Errorf("persist failed update: %w", persistErr))
	}
	if releaseErr := stageLock.Release(); releaseErr != nil {
		return result, errors.Join(err, fmt.Errorf("release update lock: %w", releaseErr))
	}
	if releaseErr := applyLock.Release(); releaseErr != nil {
		return result, errors.Join(err, fmt.Errorf("release update lock: %w", releaseErr))
	}
	return result, err
}

func (s *Stager) StatusPath() string {
	return s.statusPath
}

func (s *Stager) loadPersisted() {
	status, ok, err := ReadPersistedStatus(s.statusPath)
	if err != nil || !ok {
		return
	}
	if status.State == StageSucceeded && (len(status.Backups) > 0 || status.StagingPath != "") {
		cleanupLock, lockErr := acquireTransactionLock(s.statusPath, "recovery-cleanup-"+newTransactionID(), 0)
		if lockErr != nil {
			log.Printf("defer completed update artifact cleanup while another updater is active: %v", lockErr)
		} else {
			changed := false
			if len(status.Backups) > 0 {
				if cleanupErr := cleanupPersistedBackups(status.Backups); cleanupErr != nil {
					log.Printf("completed update backup cleanup failed: %v", cleanupErr)
				} else {
					status.Backups = nil
					changed = true
				}
			}
			if status.StagingPath != "" {
				if cleanupErr := removeStagingPath(status.StagingPath); cleanupErr != nil {
					log.Printf("completed update staging cleanup failed: %v", cleanupErr)
				} else {
					status.StagingPath = ""
					changed = true
				}
			}
			if changed {
				if err := WritePersistedStatusOwned(s.statusPath, status); err != nil {
					log.Printf("completed update cleanup status persist failed: %v", err)
				}
			}
			if err := cleanupLock.Release(); err != nil {
				log.Printf("release completed update cleanup lock: %v", err)
			}
		}
	}
	if status.State == StageDownloading || status.State == StageReady || status.State == StageApplying {
		recoveryLock, lockErr := acquireTransactionLock(s.statusPath, "recovery", 0)
		if lockErr != nil {
			log.Printf("defer stale update recovery while another updater is active: %v", lockErr)
		} else {
			if status.State == StageApplying && len(status.Backups) > 0 {
				if rollbackErr := rollbackPersistedBackups(status.Backups); rollbackErr != nil {
					log.Printf("stale update rollback failed: %v", rollbackErr)
				} else {
					status.State = StageFailed
					status.Error = "interrupted update was rolled back before CPA-Manager restarted"
					status.CompletedAtMS = time.Now().UnixMilli()
					status.Backups = nil
				}
			} else {
				status.State = StageFailed
				status.Error = "update did not complete before CPA-Manager restarted"
				status.CompletedAtMS = time.Now().UnixMilli()
			}
			if status.State == StageFailed && status.StagingPath != "" {
				if err := removeStagingPath(status.StagingPath); err != nil {
					log.Printf("stale update staging cleanup failed: %v", err)
				} else {
					status.StagingPath = ""
				}
			}
			if status.State == StageFailed && status.CompletedAtMS != 0 {
				if err := WritePersistedStatusOwned(s.statusPath, status); err != nil {
					log.Printf("update recovery status persist failed: %v", err)
				}
			}
			if err := recoveryLock.Release(); err != nil {
				log.Printf("release update recovery lock: %v", err)
			}
		}
	}
	s.job = StageStatus{
		State:         status.State,
		OS:            status.OS,
		Arch:          status.Arch,
		TransactionID: status.TransactionID,
		Backups:       append([]PersistedBackup(nil), status.Backups...),
		StartedAtMS:   status.StartedAtMS,
		CompletedAtMS: status.CompletedAtMS,
		Error:         status.Error,
		Manifest: Manifest{
			CPAVersion:     status.CPAVersion,
			ManagerVersion: status.ManagerVersion,
		},
	}
}

func isTerminalStage(state string) bool {
	return state == StageFailed || state == StageSucceeded || state == StageRolledBack
}

func (s *Stager) persist(status StageStatus) error {
	return WritePersistedStatusOwned(s.statusPath, PersistedStatus{
		State:          status.State,
		OS:             status.OS,
		Arch:           status.Arch,
		TransactionID:  status.TransactionID,
		StagingPath:    status.StagingPath,
		Backups:        append([]PersistedBackup(nil), status.Backups...),
		CPAVersion:     status.Manifest.CPAVersion,
		ManagerVersion: status.Manifest.ManagerVersion,
		Error:          status.Error,
		StartedAtMS:    status.StartedAtMS,
		CompletedAtMS:  status.CompletedAtMS,
	})
}

func newTransactionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func removeStagingPath(path string) error {
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	tempDir, err := filepath.Abs(os.TempDir())
	if err != nil {
		return err
	}
	root := filepath.Dir(path)
	if filepath.Dir(root) != tempDir || !strings.HasPrefix(filepath.Base(root), "cpa-manager-update-") {
		return fmt.Errorf("refusing to remove staging path outside managed temp directory")
	}
	return os.RemoveAll(root)
}

func CurrentTarget() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}
