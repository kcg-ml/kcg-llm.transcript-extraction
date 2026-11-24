package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
// JobStore is the on-disk record of one job.
type JobStore struct {
	JobName          string
	JobFolderPath    string
	DoneFolderPath   string
	FailedFolderPath string
	StateFilePath    string
	ErrorLogFilePath string
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
		ErrorLogFilePath: filepath.Join(projectFolderPath, "logs", jobName+".errors.log"),
	}
	for _, folderPath := range []string{store.DoneFolderPath, store.FailedFolderPath,
		filepath.Dir(store.ErrorLogFilePath)} {
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

// WriteFailed writes the failure file, the state line, and the error log line.
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
	store.appendErrorLogLine(unitId, errorText, stateFields)
	return nil
}

// appendErrorLogLine adds one line to `logs/<job>.errors.log`.
//
// The failure file holds the whole record, but a reader who wants every
// failure of a run reads one file, so each failure also writes one line here.
func (store *JobStore) appendErrorLogLine(unitId string, errorText string,
	stateFields map[string]any) {
	reasonText := strings.ReplaceAll(errorText, "\n", " ")
	reasonText = pattern.CutRuneText(reasonText, 300)
	logLine := fmt.Sprintf("%s  unit=%s  attempt=%s  exit=%s  seconds=%s  chars=%s  reason=%s\n",
		time.Now().Format("2006-01-02T15:04:05"), unitId,
		fieldTextOf(stateFields, "attempt"), fieldTextOf(stateFields, "code"),
		fieldTextOf(stateFields, "seconds"), fieldTextOf(stateFields, "chars"),
		reasonText)
	store.stateLock.Lock()
	defer store.stateLock.Unlock()
	fileHandle, openError := os.OpenFile(store.ErrorLogFilePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openError != nil {
		return
	}
	defer fileHandle.Close()
	fileHandle.WriteString(logLine)
}

// fieldTextOf writes one state field as text, or `-` when it is absent.
func fieldTextOf(stateFields map[string]any, fieldName string) string {
	fieldValue, fieldFound := stateFields[fieldName]
	if !fieldFound {
		return "-"
	}
	return fmt.Sprintf("%v", fieldValue)
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

// FailureReasonOf returns the reason text of one failed unit, shortened to the
// part before the first newline so it fits on one summary line.
func (store *JobStore) FailureReasonOf(unitId string) string {
	failedBytes, readError := os.ReadFile(store.FailedFilePath(unitId))
	if readError != nil {
		return "unreadable failure file"
	}
	failureRecord := map[string]any{}
	if decodeError := json.Unmarshal(failedBytes, &failureRecord); decodeError != nil {
		return "unreadable failure record"
	}
	reasonValue, reasonFound := failureRecord["error"]
	if !reasonFound {
		return "no reason recorded"
	}
	reasonText := fmt.Sprintf("%v", reasonValue)
	if newlineIndex := strings.Index(reasonText, "\n"); newlineIndex >= 0 {
		reasonText = reasonText[:newlineIndex]
	}
	return pattern.CutRuneText(reasonText, 120)
}

// Counts returns the done and failed counts of a unit list.
func (store *JobStore) Counts(unitList []JobUnit) (int, int) {
	doneCount := 0
	failedCount := 0
	for _, jobUnit := range unitList {
		if store.IsDone(jobUnit.UnitId) {
			doneCount++
		}
		if store.IsFailed(jobUnit.UnitId) {
			failedCount++
		}
	}
	return doneCount, failedCount
}
