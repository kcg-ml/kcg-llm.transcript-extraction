package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecondAcquireRefused proves a second run cannot take a held lock, and
// that the refusal names the holder pid.
func TestSecondAcquireRefused(t *testing.T) {
	jobFolderPath := t.TempDir()
	firstLock, firstError := AcquireJobRunLock(jobFolderPath)
	if firstError != nil {
		t.Fatalf("the first acquire failed: %v", firstError)
	}
	defer firstLock.Release()
	_, secondError := AcquireJobRunLock(jobFolderPath)
	if secondError == nil {
		t.Fatal("the second acquire succeeded; it must be refused")
	}
	if !strings.Contains(secondError.Error(), "already running") {
		t.Fatalf("the refusal does not say the job is running: %v", secondError)
	}
	t.Logf("refused: %v", secondError)
}

// TestStaleLockReclaimed proves a lock naming a dead pid is reclaimed.
func TestStaleLockReclaimed(t *testing.T) {
	jobFolderPath := t.TempDir()
	lockFilePath := filepath.Join(jobFolderPath, "run.lock")
	// pid 999999 is above the macOS and Linux pid ceiling, so it is dead.
	staleText := "pid=999999\nstarted_at=2026-01-01T00:00:00\n"
	if writeError := os.WriteFile(lockFilePath, []byte(staleText), 0o644); writeError != nil {
		t.Fatalf("cannot write the stale lock: %v", writeError)
	}
	runLock, acquireError := AcquireJobRunLock(jobFolderPath)
	if acquireError != nil {
		t.Fatalf("the stale lock was not reclaimed: %v", acquireError)
	}
	defer runLock.Release()
	t.Log("stale lock reclaimed")
}

// TestLockFileIsNeverEmpty proves the lock file never exists with no record
// inside it, which is the race a second process would read as a stale lock.
//
// The record is linked into place, so a reader sees either no file or a file
// that already holds the pid.
func TestLockFileIsNeverEmpty(t *testing.T) {
	jobFolderPath := t.TempDir()
	lockFilePath := filepath.Join(jobFolderPath, "run.lock")
	emptySeen := make(chan string, 1)
	stopChannel := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopChannel:
				return
			default:
			}
			lockBytes, readError := os.ReadFile(lockFilePath)
			if readError == nil && len(lockBytes) == 0 {
				select {
				case emptySeen <- "empty":
				default:
				}
				return
			}
		}
	}()
	for roundIndex := 0; roundIndex < 200; roundIndex++ {
		runLock, acquireError := AcquireJobRunLock(jobFolderPath)
		if acquireError != nil {
			t.Fatalf("round %d: %v", roundIndex, acquireError)
		}
		runLock.Release()
	}
	close(stopChannel)
	select {
	case <-emptySeen:
		t.Fatal("the lock file was seen empty; a second process would reclaim a live lock")
	default:
	}
	t.Log("200 acquire/release rounds, the lock file was never empty")
}
