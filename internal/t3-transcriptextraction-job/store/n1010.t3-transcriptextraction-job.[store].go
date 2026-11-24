package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/unit"
)
// JobStore is the on-disk record of one job.
type JobStore struct {
	JobName          string
	JobFolderPath    string
	DoneFolderPath   string
	FailedFolderPath string
	StateFilePath    string
	stateLock        sync.Mutex
}

// NewJobStore returns the store of a job and creates its folders.
func NewJobStore(projectFolderPath string, jobName string) (*JobStore, error) {
	jobFolderPath := filepath.Join(projectFolderPath, "model-answer", "job", jobName)
	store := &JobStore{
		JobName:          jobName,
		JobFolderPath:    jobFolderPath,
		DoneFolderPath:   filepath.Join(jobFolderPath, "done"),
		FailedFolderPath: filepath.Join(jobFolderPath, "failed"),
		StateFilePath:    filepath.Join(jobFolderPath, "state.jsonl"),
	}
	for _, folderPath := range []string{store.DoneFolderPath, store.FailedFolderPath} {
		if createError := os.MkdirAll(folderPath, 0o755); createError != nil {
			return nil, fmt.Errorf("cannot create %s: %w", folderPath, createError)
		}
	}
	return store, nil
}

// DoneFilePath returns the answer file of one unit.
func (store *JobStore) DoneFilePath(unitId string) string {
	return filepath.Join(store.DoneFolderPath, unitId+".txt")
}

// FailedFilePath returns the failure file of one unit.
func (store *JobStore) FailedFilePath(unitId string) string {
	return filepath.Join(store.FailedFolderPath, unitId+".json")
}

// IsDone reports whether a unit has an answer file.
func (store *JobStore) IsDone(unitId string) bool {
	_, statError := os.Stat(store.DoneFilePath(unitId))
	return statError == nil
}

// IsFailed reports whether a unit has a failure file.
func (store *JobStore) IsFailed(unitId string) bool {
	_, statError := os.Stat(store.FailedFilePath(unitId))
	return statError == nil
}

// WriteDone writes the answer, then the state line. The answer file is the
// truth, so it is written first and renamed into place.
func (store *JobStore) WriteDone(unitId string, answerText string, stateFields map[string]any) error {
	answerFilePath := store.DoneFilePath(unitId)
	temporaryPath := answerFilePath + ".tmp"
	if writeError := os.WriteFile(temporaryPath, []byte(answerText), 0o644); writeError != nil {
		return fmt.Errorf("cannot write %s: %w", temporaryPath, writeError)
	}
	if renameError := os.Rename(temporaryPath, answerFilePath); renameError != nil {
		return fmt.Errorf("cannot rename %s: %w", temporaryPath, renameError)
	}
	store.appendStateLine(unitId, "ok", stateFields)
	return nil
}

// WriteFailed writes the failure file and the state line.
func (store *JobStore) WriteFailed(unitId string, errorText string, stateFields map[string]any) error {
	failureRecord := map[string]any{"unit_id": unitId, "error": errorText}
	for fieldName, fieldValue := range stateFields {
		failureRecord[fieldName] = fieldValue
	}
	encodedRecord, encodeError := json.MarshalIndent(failureRecord, "", " ")
	if encodeError != nil {
		return encodeError
	}
	failedFilePath := store.FailedFilePath(unitId)
	if writeError := os.WriteFile(failedFilePath, encodedRecord, 0o644); writeError != nil {
		return fmt.Errorf("cannot write %s: %w", failedFilePath, writeError)
	}
	store.appendStateLine(unitId, "fail", stateFields)
	return nil
}

// appendStateLine adds one line to state.jsonl.
func (store *JobStore) appendStateLine(unitId string, stateName string, stateFields map[string]any) {
	stateRecord := map[string]any{
		"unit_id": unitId,
		"state":   stateName,
		"at":      time.Now().Format("2006-01-02T15:04:05"),
	}
	for fieldName, fieldValue := range stateFields {
		stateRecord[fieldName] = fieldValue
	}
	encodedRecord, encodeError := json.Marshal(stateRecord)
	if encodeError != nil {
		return
	}
	store.stateLock.Lock()
	defer store.stateLock.Unlock()
	fileHandle, openError := os.OpenFile(store.StateFilePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openError != nil {
		return
	}
	defer fileHandle.Close()
	fileHandle.Write(append(encodedRecord, '\n'))
}

// ClearFailed removes the failure file of one unit, so the next run retries it.
func (store *JobStore) ClearFailed(unitId string) {
	os.Remove(store.FailedFilePath(unitId))
}

// Counts returns the done and failed counts of a unit list.
func (store *JobStore) Counts(unitList []unit.JobUnit) (int, int) {
	doneCount := 0
	failedCount := 0
	for _, unit := range unitList {
		if store.IsDone(unit.UnitId) {
			doneCount++
		}
		if store.IsFailed(unit.UnitId) {
			failedCount++
		}
	}
	return doneCount, failedCount
}
