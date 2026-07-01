package usagereporter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrReporterAlreadyRunning = errors.New("reporter already running")

const runLockStaleAfter = 30 * time.Minute

func AcquireRunLock(statePath string) (func() error, error) {
	lockPath := strings.TrimSpace(statePath)
	if lockPath == "" {
		return nil, fmt.Errorf("state path is required for reporter lock")
	}
	lockPath += ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		handle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = handle.WriteString(strconv.FormatInt(time.Now().Unix(), 10))
			_, _ = handle.WriteString("\n")
			_, _ = handle.WriteString(strconv.Itoa(os.Getpid()))
			if closeErr := handle.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, closeErr
			}
			return func() error {
				return os.Remove(lockPath)
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		stale, staleErr := isStaleRunLock(lockPath)
		if staleErr != nil || !stale || attempt == 1 {
			return nil, ErrReporterAlreadyRunning
		}
		if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, ErrReporterAlreadyRunning
		}
	}

	return nil, ErrReporterAlreadyRunning
}

func isStaleRunLock(lockPath string) (bool, error) {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false, err
	}
	return time.Since(info.ModTime()) > runLockStaleAfter, nil
}
