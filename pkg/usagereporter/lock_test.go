package usagereporter

import "testing"

func TestAcquireRunLockBlocksConcurrentRun(t *testing.T) {
	lockPath := t.TempDir() + "/state.json"

	release, err := AcquireRunLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireRunLock(first) error: %v", err)
	}
	defer release()

	secondRelease, err := AcquireRunLock(lockPath)
	if err == nil {
		if secondRelease != nil {
			secondRelease()
		}
		t.Fatal("AcquireRunLock(second) expected error when lock already held")
	}
	if err != ErrReporterAlreadyRunning {
		t.Fatalf("AcquireRunLock(second) error = %v, want %v", err, ErrReporterAlreadyRunning)
	}
}
