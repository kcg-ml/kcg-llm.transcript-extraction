// Package durablejob runs one model call per unit and keeps every finished
// unit on disk, so a killed run resumes where it stopped.
//
// The problem this solves: a run of 3963 model calls takes hours, and a shell
// that prints one line per call says nothing for minutes at a time. A reader
// cannot tell a slow call from a dead process.
//
// Three things stay on disk:
//
//	out/jobs/<job>/done/<unit id>.txt    the answer of one unit, written once
//	out/jobs/<job>/failed/<unit id>.json the error of one unit out of retries
//	out/jobs/<job>/state.jsonl           one line per finished unit
//
// A unit is done when its answer file exists. Nothing else is trusted, so a
// kill at any moment loses at most the calls that were in flight.
//
// The progress line goes to the terminal every poll interval and to
// logs/<job>.progress.log, so a reader can `tail -f` it:
//
//	[12:03:44] 243/3963  6.1%  ok=240 fail=3  rate=1.9/s  eta=32m  inflight=8  last=rel-00042-v1 4.2s
package durablejob

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// JobUnit is one call: its identity, the prompt file it reads, and the image
// it passes to the model, if any.
type JobUnit struct {
	UnitId         string
	PromptFilePath string
	ImageFilePath  string
}

// JobSettings holds the options of one run.
type JobSettings struct {
	JobName           string
	WorkerCount       int
	PollSeconds       int
	ModelName         string
	ProviderName      string
	TimeoutSeconds    int
	RetryCount        int
	RetrySleepSeconds int
	UnitLimit         int
	PrintStatusOnly   bool
	RetryFailedOnly   bool
}

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
	jobFolderPath := filepath.Join(projectFolderPath, "out", "jobs", jobName)
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
func (store *JobStore) Counts(unitList []JobUnit) (int, int) {
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

// PiCallResult is the outcome of one model call.
type PiCallResult struct {
	ExitCode    int
	StdoutText  string
	StderrText  string
	SecondCount float64
}

// CallPi runs one pi call and returns its outcome.
//
// An image path is passed as an `@<path>` argument, which pi turns into an
// image attachment for a model that accepts image input. A call that outlives
// its timeout is killed, because a child that holds the pipe open would
// otherwise keep the call alive past its timeout.
func CallPi(projectFolderPath string, promptText string, settings JobSettings,
	imageFilePath string) PiCallResult {
	commandArgumentList := []string{"--print", "--no-tools", "--no-session",
		"--mode", "text", "--model", settings.ModelName}
	if settings.ProviderName != "" {
		commandArgumentList = append(commandArgumentList, "--provider", settings.ProviderName)
	}
	if imageFilePath != "" {
		commandArgumentList = append(commandArgumentList, "@"+imageFilePath)
	}
	piBinaryName := os.Getenv("PI_BIN")
	if piBinaryName == "" {
		piBinaryName = "pi"
	}
	startedAt := time.Now()
	timeoutDuration := time.Duration(settings.TimeoutSeconds) * time.Second
	contextHandle, cancelHandle := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancelHandle()

	commandHandle := exec.CommandContext(contextHandle, piBinaryName, commandArgumentList...)
	commandHandle.Dir = projectFolderPath
	commandHandle.Stdin = strings.NewReader(promptText)
	stdoutBuffer := bytes.Buffer{}
	stderrBuffer := bytes.Buffer{}
	commandHandle.Stdout = &stdoutBuffer
	commandHandle.Stderr = &stderrBuffer
	runError := commandHandle.Run()
	elapsedSeconds := time.Since(startedAt).Seconds()

	if contextHandle.Err() == context.DeadlineExceeded {
		return PiCallResult{
			ExitCode:    124,
			StdoutText:  "",
			StderrText:  fmt.Sprintf("timeout after %ds, process killed", settings.TimeoutSeconds),
			SecondCount: elapsedSeconds,
		}
	}
	exitCode := 0
	if runError != nil {
		if exitError, isExitError := runError.(*exec.ExitError); isExitError {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 127
			stderrBuffer.WriteString(runError.Error())
		}
	}
	return PiCallResult{
		ExitCode:    exitCode,
		StdoutText:  stdoutBuffer.String(),
		StderrText:  stderrBuffer.String(),
		SecondCount: elapsedSeconds,
	}
}

// ErrorPrefixList names the first-line shapes pi writes when it fails. pi
// writes a startup error to stdout and still exits 0, so the answer text is
// checked before it is saved.
var ErrorPrefixList = []string{"Error:", "error:", "Error ", "Cannot ", "Unable to "}

// AnswerErrorOf returns the error line when pi answered with an error instead
// of a result.
func AnswerErrorOf(answerText string) string {
	strippedText := strings.TrimSpace(answerText)
	if strippedText == "" {
		return "empty answer"
	}
	firstLine := strippedText
	if newlineIndex := strings.Index(firstLine, "\n"); newlineIndex >= 0 {
		firstLine = firstLine[:newlineIndex]
	}
	firstLine = strings.TrimSpace(firstLine)
	for _, errorPrefix := range ErrorPrefixList {
		if strings.HasPrefix(firstLine, errorPrefix) {
			return cutText(firstLine, 500)
		}
	}
	return ""
}

// PositionLabelPattern matches a vote label such as `M3 of 10`.
var PositionLabelPattern = regexp.MustCompile(`(?m)^\s*` + "`?" + `(M[0-9]+ of [0-9]+)` + "`?" + `\s+[YNU]\b`)

// MessageKeyPattern matches a vote line keyed by a message key.
var MessageKeyPattern = regexp.MustCompile(`(?m)^\s*` + "`?" + `([0-9]{6}-[0-9]{6})` + "`?" + `\s+[YNU]\b`)

// PromptPositionLabelPattern matches the label line a prompt gives a message.
var PromptPositionLabelPattern = regexp.MustCompile(`(?m)^### (M[0-9]+ of [0-9]+)`)

// PromptMessageKeyPattern matches the key line a prompt gives a message.
var PromptMessageKeyPattern = regexp.MustCompile(`(?m)^### ([0-9]{6}-[0-9]{6})`)

// answerLabelListOf returns the vote labels an answer holds, in order.
func answerLabelListOf(answerText string) []string {
	labelList := PositionLabelPattern.FindAllStringSubmatch(answerText, -1)
	if len(labelList) > 0 {
		collectedList := make([]string, 0, len(labelList))
		for _, match := range labelList {
			collectedList = append(collectedList, match[1])
		}
		return collectedList
	}
	keyList := MessageKeyPattern.FindAllStringSubmatch(answerText, -1)
	collectedList := make([]string, 0, len(keyList))
	for _, match := range keyList {
		collectedList = append(collectedList, match[1])
	}
	return collectedList
}

// promptLabelListOf returns the labels a prompt gives its messages, in order.
func promptLabelListOf(promptText string) []string {
	labelList := PromptPositionLabelPattern.FindAllStringSubmatch(promptText, -1)
	if len(labelList) > 0 {
		collectedList := make([]string, 0, len(labelList))
		for _, match := range labelList {
			collectedList = append(collectedList, match[1])
		}
		return collectedList
	}
	keyList := PromptMessageKeyPattern.FindAllStringSubmatch(promptText, -1)
	collectedList := make([]string, 0, len(keyList))
	for _, match := range keyList {
		collectedList = append(collectedList, match[1])
	}
	return collectedList
}

// AnswerWrongnessOf returns a reason when the answer votes on a label its
// prompt does not hold.
//
// A model that invents labels answers a window it was never given, and the
// vote then lands on a message that does not exist. The count check catches the
// same defect from the other side: 1 `###` line must yield 1 vote line.
func AnswerWrongnessOf(promptText string, answerText string) string {
	expectedLabelList := promptLabelListOf(promptText)
	if len(expectedLabelList) == 0 {
		return ""
	}
	expectedLabelSet := make(map[string]bool, len(expectedLabelList))
	for _, label := range expectedLabelList {
		expectedLabelSet[label] = true
	}
	gotLabelList := answerLabelListOf(answerText)
	unknownLabelList := make([]string, 0, 4)
	for _, label := range gotLabelList {
		if !expectedLabelSet[label] {
			unknownLabelList = append(unknownLabelList, label)
		}
	}
	if len(unknownLabelList) > 0 {
		shownLabelList := unknownLabelList
		if len(shownLabelList) > 5 {
			shownLabelList = shownLabelList[:5]
		}
		return fmt.Sprintf("answer votes on %d key(s) not in the prompt: %s",
			len(unknownLabelList), strings.Join(shownLabelList, ", "))
	}
	if len(gotLabelList) != len(expectedLabelList) {
		return fmt.Sprintf("answer has %d vote lines, the prompt has %d messages",
			len(gotLabelList), len(expectedLabelList))
	}
	return ""
}

// ShrinkImage returns the path of a copy of an image whose long side is at
// most maxSide pixels.
//
// The provider `zai` refuses a photo whose content filter fires on the
// full-size pixels with HTTP 400 `code 1301`. The same photo at 200 px passes,
// so a refused photo is retried once at 200 px. The copy goes to
// `out/jobs/shrunk/`, never into the export folder.
func ShrinkImage(projectFolderPath string, imageFilePath string, maxSide int) string {
	stemName := strings.TrimSuffix(filepath.Base(imageFilePath),
		filepath.Ext(imageFilePath))
	shrunkFolderPath := filepath.Join(projectFolderPath, "out", "jobs", "shrunk")
	if createError := os.MkdirAll(shrunkFolderPath, 0o755); createError != nil {
		return ""
	}
	shrunkFilePath := filepath.Join(shrunkFolderPath,
		fmt.Sprintf("%s-%d.jpg", stemName, maxSide))
	if _, statError := os.Stat(shrunkFilePath); statError == nil {
		return shrunkFilePath
	}
	shrinkCommand := exec.Command("sips", "-Z", fmt.Sprintf("%d", maxSide),
		imageFilePath, "--out", shrunkFilePath)
	if runError := shrinkCommand.Run(); runError != nil {
		return ""
	}
	if _, statError := os.Stat(shrunkFilePath); statError != nil {
		return ""
	}
	return shrunkFilePath
}

// RunCounters holds the live counts of one run.
type RunCounters struct {
	lockHandle       sync.Mutex
	okCount          int
	failCount        int
	inFlightCount    int
	lastUnitId       string
	lastSecondCount  float64
	totalSecondCount float64
}

// addOk records a finished unit.
func (counters *RunCounters) addOk(unitId string, secondCount float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.okCount++
	counters.lastUnitId = unitId
	counters.lastSecondCount = secondCount
	counters.totalSecondCount += secondCount
}

// addFail records a failed unit.
func (counters *RunCounters) addFail(unitId string, secondCount float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.failCount++
	counters.lastUnitId = unitId
	counters.lastSecondCount = secondCount
}

// addInFlight changes the count of calls in flight.
func (counters *RunCounters) addInFlight(deltaCount int) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.inFlightCount += deltaCount
}

// snapshot returns the counts under the lock.
func (counters *RunCounters) snapshot() (int, int, string, float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	return counters.okCount, counters.inFlightCount, counters.lastUnitId,
		counters.lastSecondCount
}

// formatEtaText writes a duration as `12m03s` or `2h05m`.
func formatEtaText(secondCount float64) string {
	if secondCount <= 0 {
		return "--"
	}
	minuteCount := int(secondCount) / 60
	if minuteCount < 60 {
		return fmt.Sprintf("%dm%02ds", minuteCount, int(secondCount)%60)
	}
	return fmt.Sprintf("%dh%02dm", minuteCount/60, minuteCount%60)
}

// progressLineOf returns the one-line progress report.
func progressLineOf(store *JobStore, unitList []JobUnit, counters *RunCounters,
	startedAt time.Time) string {
	totalCount := len(unitList)
	doneCount, failedCount := store.Counts(unitList)
	elapsedSeconds := time.Since(startedAt).Seconds()
	if elapsedSeconds < 0.001 {
		elapsedSeconds = 0.001
	}
	okCount, inFlightCount, lastUnitId, lastSecondCount := counters.snapshot()
	ratePerSecond := 0.0
	if okCount > 0 {
		ratePerSecond = float64(okCount) / elapsedSeconds
	}
	remainingCount := totalCount - doneCount
	etaSecondCount := 0.0
	if ratePerSecond > 0 {
		etaSecondCount = float64(remainingCount) / ratePerSecond
	}
	lastText := "-"
	if lastUnitId != "" {
		lastText = fmt.Sprintf("%s %.1fs", lastUnitId, lastSecondCount)
	}
	percentValue := 0.0
	if totalCount > 0 {
		percentValue = 100.0 * float64(doneCount) / float64(totalCount)
	}
	return fmt.Sprintf("[%s] %d/%d  %5.1f%%  ok=%d fail=%d  rate=%.2f/s  eta=%s  inflight=%d  last=%s",
		time.Now().Format("15:04:05"), doneCount, totalCount, percentValue,
		okCount, failedCount, ratePerSecond, formatEtaText(etaSecondCount),
		inFlightCount, lastText)
}

// runUnit makes the calls of one unit until it answers or runs out of retries.
func runUnit(projectFolderPath string, store *JobStore, unit JobUnit,
	settings JobSettings, counters *RunCounters) {
	if store.IsDone(unit.UnitId) {
		return
	}
	promptBytes, readError := os.ReadFile(filepath.Join(projectFolderPath, unit.PromptFilePath))
	if readError != nil {
		store.WriteFailed(unit.UnitId, fmt.Sprintf("cannot read prompt %s: %v",
			unit.PromptFilePath, readError), map[string]any{"code": 0, "attempt": 0})
		counters.addFail(unit.UnitId, 0)
		return
	}
	promptText := string(promptBytes)
	imageFilePath := unit.ImageFilePath
	if imageFilePath != "" {
		imageFilePath = filepath.Join(projectFolderPath, imageFilePath)
	}

	attemptCount := 0
	for {
		attemptCount++
		counters.addInFlight(1)
		callResult := CallPi(projectFolderPath, promptText, settings, imageFilePath)
		counters.addInFlight(-1)

		answerError := AnswerErrorOf(callResult.StdoutText)
		if answerError == "" {
			answerError = AnswerWrongnessOf(promptText, callResult.StdoutText)
		}
		if imageFilePath != "" &&
			strings.Contains(callResult.StdoutText+callResult.StderrText, "1301") {
			// The provider refused the pixels, not the prompt. Retry once at
			// 200 px, which the same photo passes.
			shrunkFilePath := ShrinkImage(projectFolderPath, imageFilePath, 200)
			if shrunkFilePath != "" {
				callResult = CallPi(projectFolderPath, promptText, settings, shrunkFilePath)
				answerError = AnswerErrorOf(callResult.StdoutText)
				if answerError == "" {
					answerError = AnswerWrongnessOf(promptText, callResult.StdoutText)
				}
			}
		}

		if callResult.ExitCode == 0 && strings.TrimSpace(callResult.StdoutText) != "" &&
			answerError == "" {
			store.WriteDone(unit.UnitId, callResult.StdoutText, map[string]any{
				"model":   settings.ModelName,
				"seconds": roundToTenth(callResult.SecondCount),
				"attempt": attemptCount,
				"chars":   len(callResult.StdoutText),
			})
			store.ClearFailed(unit.UnitId)
			counters.addOk(unit.UnitId, callResult.SecondCount)
			return
		}

		if attemptCount > settings.RetryCount {
			reasonText := answerError
			if reasonText == "" {
				reasonText = cutText(strings.TrimSpace(callResult.StderrText), 1000)
			}
			store.WriteFailed(unit.UnitId, reasonText, map[string]any{
				"code":    callResult.ExitCode,
				"attempt": attemptCount,
				"seconds": roundToTenth(callResult.SecondCount),
			})
			counters.addFail(unit.UnitId, callResult.SecondCount)
			return
		}
		time.Sleep(time.Duration(settings.RetrySleepSeconds) * time.Second)
	}
}

// roundToTenth rounds a second count to 1 decimal place.
func roundToTenth(secondCount float64) float64 {
	return float64(int(secondCount*10+0.5)) / 10
}

// cutText returns the first maxLength characters of a text.
func cutText(text string, maxLength int) string {
	return textrule.CutRuneText(text, maxLength)
}

// Run executes a job and returns the process exit code.
func Run(projectFolderPath string, settings JobSettings, unitList []JobUnit) int {
	if settings.UnitLimit > 0 && settings.UnitLimit < len(unitList) {
		unitList = unitList[:settings.UnitLimit]
	}
	store, storeError := NewJobStore(projectFolderPath, settings.JobName)
	if storeError != nil {
		fmt.Fprintf(os.Stderr, "%v\n", storeError)
		return 1
	}
	logFolderPath := filepath.Join(projectFolderPath, "logs")
	if createError := os.MkdirAll(logFolderPath, 0o755); createError != nil {
		fmt.Fprintf(os.Stderr, "cannot create %s: %v\n", logFolderPath, createError)
		return 1
	}
	progressFilePath := filepath.Join(logFolderPath, settings.JobName+".progress.log")

	if settings.RetryFailedOnly {
		clearedCount := 0
		for _, unit := range unitList {
			if store.IsFailed(unit.UnitId) {
				store.ClearFailed(unit.UnitId)
				clearedCount++
			}
		}
		fmt.Printf("cleared %d failed markers for job %s\n", clearedCount, settings.JobName)
		return 0
	}

	counters := &RunCounters{}
	if settings.PrintStatusOnly {
		fmt.Println(progressLineOf(store, unitList, counters, time.Now()))
		return 0
	}

	pendingUnitList := make([]JobUnit, 0, len(unitList))
	for _, unit := range unitList {
		if !store.IsDone(unit.UnitId) {
			pendingUnitList = append(pendingUnitList, unit)
		}
	}
	fmt.Printf("job %s: %d units, %d pending, %d workers, model %s\n",
		settings.JobName, len(unitList), len(pendingUnitList), settings.WorkerCount,
		settings.ModelName)
	fmt.Printf("progress log: %s\n", csvtable.RelativePath(projectFolderPath, progressFilePath))
	if len(pendingUnitList) == 0 {
		fmt.Println("nothing to do")
		return 0
	}

	startedAt := time.Now()
	stopChannel := make(chan struct{})
	progressDoneChannel := make(chan struct{})
	go func() {
		defer close(progressDoneChannel)
		progressFileHandle, openError := os.OpenFile(progressFilePath,
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openError != nil {
			return
		}
		defer progressFileHandle.Close()
		pollDuration := time.Duration(settings.PollSeconds) * time.Second
		if pollDuration <= 0 {
			pollDuration = 30 * time.Second
		}
		tickerHandle := time.NewTicker(pollDuration)
		defer tickerHandle.Stop()
		for {
			line := progressLineOf(store, unitList, counters, startedAt)
			fmt.Println(line)
			progressFileHandle.WriteString(line + "\n")
			select {
			case <-stopChannel:
				return
			case <-tickerHandle.C:
			}
		}
	}()

	workerCount := settings.WorkerCount
	if workerCount < 1 {
		workerCount = 1
	}
	unitChannel := make(chan JobUnit)
	workerGroup := sync.WaitGroup{}
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for unit := range unitChannel {
				runUnit(projectFolderPath, store, unit, settings, counters)
			}
		}()
	}
	for _, unit := range pendingUnitList {
		unitChannel <- unit
	}
	close(unitChannel)
	workerGroup.Wait()

	close(stopChannel)
	<-progressDoneChannel

	finalLine := progressLineOf(store, unitList, counters, startedAt)
	fmt.Println(finalLine)
	if progressFileHandle, openError := os.OpenFile(progressFilePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); openError == nil {
		progressFileHandle.WriteString(finalLine + "  DONE\n")
		progressFileHandle.Close()
	}
	_, failedCount := store.Counts(unitList)
	if failedCount > 0 {
		return 2
	}
	return 0
}
