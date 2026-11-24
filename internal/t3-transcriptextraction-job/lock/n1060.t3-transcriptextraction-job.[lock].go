package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// JobRunLock is the exclusive claim of one process on one job.
//
// Two runs of one job would write the same answer files, so the second run
// must refuse to start. The claim is a file created with O_EXCL, which the
// kernel makes atomic, so two processes that start at the same instant cannot
// both win.
type JobRunLock struct {
	LockFilePath string
	HolderPid    int
}

// lockRecord is the text of the lock file: the pid, then the start time.
type lockRecord struct {
	Pid       int
	StartedAt string
}

// AcquireJobRunLock takes the lock of one job.
//
// A lock whose holder is no longer alive is stale, because a killed run never
// removes its file. A stale lock is reclaimed and the reclaim is reported, so
// a reader knows the previous run died without cleaning up.
func AcquireJobRunLock(jobFolderPath string) (*JobRunLock, error) {
	lockFilePath := filepath.Join(jobFolderPath, "run.lock")
	runLock := &JobRunLock{LockFilePath: lockFilePath, HolderPid: os.Getpid()}

	createError := runLock.createLockFile()
	if createError == nil {
		return runLock, nil
	}
	if !os.IsExist(createError) {
		return nil, fmt.Errorf("cannot create %s: %w", lockFilePath, createError)
	}

	holderPid, holderStartedAt, readError := readLockRecord(lockFilePath)
	if readError != nil {
		// The file exists but says nothing usable, so no live run owns it.
		// Remove it and take the lock, and say so.
		fmt.Printf("warning: %s held no readable pid (%v); reclaiming it\n",
			lockFilePath, readError)
		os.Remove(lockFilePath)
		if retryError := runLock.createLockFile(); retryError != nil {
			return nil, fmt.Errorf("cannot create %s after reclaiming it: %w",
				lockFilePath, retryError)
		}
		return runLock, nil
	}

	if processIsAlive(holderPid) {
		return nil, fmt.Errorf(
			"job is already running as pid %d since %s; stop it first, or wait for it to finish",
			holderPid, holderStartedAt)
	}

	fmt.Printf("warning: %s named pid %d since %s, which is no longer running; reclaiming it\n",
		lockFilePath, holderPid, holderStartedAt)
	os.Remove(lockFilePath)
	if retryError := runLock.createLockFile(); retryError != nil {
		return nil, fmt.Errorf("cannot create %s after reclaiming it: %w",
			lockFilePath, retryError)
	}
	return runLock, nil
}

// createLockFile writes the lock file and fails when another process holds it.
func (runLock *JobRunLock) createLockFile() error {
	fileHandle, openError := os.OpenFile(runLock.LockFilePath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if openError != nil {
		return openError
	}
	defer fileHandle.Close()
	recordText := fmt.Sprintf("pid=%d\nstarted_at=%s\n",
		runLock.HolderPid, time.Now().Format("2006-01-02T15:04:05"))
	_, writeError := fileHandle.WriteString(recordText)
	return writeError
}

// Release removes the lock file, and removes it only when this process holds it.
func (runLock *JobRunLock) Release() {
	holderPid, _, readError := readLockRecord(runLock.LockFilePath)
	if readError == nil && holderPid != runLock.HolderPid {
		return
	}
	os.Remove(runLock.LockFilePath)
}

// readLockRecord reads the pid and the start time of the lock file.
func readLockRecord(lockFilePath string) (int, string, error) {
	lockBytes, readError := os.ReadFile(lockFilePath)
	if readError != nil {
		return 0, "", readError
	}
	holderPid := 0
	startedAtText := "an unknown time"
	for _, lineText := range strings.Split(string(lockBytes), "\n") {
		lineText = strings.TrimSpace(lineText)
		if strings.HasPrefix(lineText, "pid=") {
			parsedPid, parseError := strconv.Atoi(strings.TrimPrefix(lineText, "pid="))
			if parseError != nil {
				return 0, "", fmt.Errorf("pid line %q is not a number", lineText)
			}
			holderPid = parsedPid
		}
		if strings.HasPrefix(lineText, "started_at=") {
			startedAtText = strings.TrimPrefix(lineText, "started_at=")
		}
	}
	if holderPid <= 0 {
		return 0, "", fmt.Errorf("no pid line in %s", lockFilePath)
	}
	return holderPid, startedAtText, nil
}

// processIsAlive reports whether a pid names a running process.
//
// Signal 0 is delivered to no process; it only checks that the pid exists and
// that this user may signal it. EPERM means the process exists and belongs to
// another user, which still counts as alive.
func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	processHandle, findError := os.FindProcess(pid)
	if findError != nil {
		return false
	}
	signalError := processHandle.Signal(syscall.Signal(0))
	if signalError == nil {
		return true
	}
	return signalError == syscall.EPERM
}