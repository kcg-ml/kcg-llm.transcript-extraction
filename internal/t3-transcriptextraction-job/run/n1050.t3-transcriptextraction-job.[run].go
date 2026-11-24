package run

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/answer"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/call"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/lock"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// runUnitRecovering runs one unit and turns a panic into a failure file.
//
// One unit that panics must not kill the run, because the other units are
// still answerable. The panic text is kept, so the cause is not lost. The
// recovery itself must not panic, so it touches no value that the unit body
// may have left broken.
func runUnitRecovering(projectFolderPath string, jobStore *store.JobStore,
	jobUnit store.JobUnit, settings store.JobSettings, counters *RunCounters) {
	defer func() {
		panicValue := recover()
		if panicValue == nil {
			return
		}
		panicText := fmt.Sprintf("panic: %v", panicValue)
		jobStore.WriteFailed(jobUnit.UnitId, panicText,
			map[string]any{"code": -1, "attempt": 0, "stack": string(debug.Stack())})
		if counters != nil {
			counters.AddFail(jobUnit.UnitId, 0)
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", jobUnit.UnitId, panicText)
	}()
	runUnit(projectFolderPath, jobStore, jobUnit, settings, counters)
}

// runUnit makes the calls of one unit until it answers or runs out of retries.
func runUnit(projectFolderPath string, jobStore *store.JobStore, jobUnit store.JobUnit,
	settings store.JobSettings, counters *RunCounters) {
	if jobStore.IsDone(jobUnit.UnitId) {
		return
	}
	promptBytes, readError := os.ReadFile(filepath.Join(projectFolderPath, jobUnit.PromptFilePath))
	if readError != nil {
		jobStore.WriteFailed(jobUnit.UnitId, fmt.Sprintf("cannot read prompt %s: %v",
			jobUnit.PromptFilePath, readError), map[string]any{"code": 0, "attempt": 0})
		counters.AddFail(jobUnit.UnitId, 0)
		return
	}
	promptText := string(promptBytes)
	imageFilePath := jobUnit.ImageFilePath
	if imageFilePath != "" {
		imageFilePath = filepath.Join(projectFolderPath, imageFilePath)
	}

	attemptCount := 0
	for {
		attemptCount++
		counters.AddInFlight(1)
		callResult := call.CallPi(projectFolderPath, promptText, settings, imageFilePath)
		counters.AddInFlight(-1)

		answerError := answer.AnswerErrorOf(callResult.StdoutText)
		if answerError == "" {
			answerError = answer.AnswerWrongnessOf(promptText, callResult.StdoutText)
		}
		if imageFilePath != "" &&
			strings.Contains(callResult.StdoutText+callResult.StderrText, "1301") {
			// The provider refused the pixels, not the prompt. Retry once at
			// 200 px, which the same photo passes.
			shrunkFilePath := call.ShrinkImage(projectFolderPath, imageFilePath, 200)
			if shrunkFilePath != "" {
				callResult = call.CallPi(projectFolderPath, promptText, settings, shrunkFilePath)
				answerError = answer.AnswerErrorOf(callResult.StdoutText)
				if answerError == "" {
					answerError = answer.AnswerWrongnessOf(promptText, callResult.StdoutText)
				}
			}
		}

		if callResult.ExitCode == 0 && strings.TrimSpace(callResult.StdoutText) != "" &&
			answerError == "" {
			jobStore.WriteDone(jobUnit.UnitId, callResult.StdoutText, map[string]any{
				"model":   settings.ModelName,
				"seconds": RoundToTenth(callResult.SecondCount),
				"attempt": attemptCount,
				"chars":   len(callResult.StdoutText),
			})
			jobStore.ClearFailed(jobUnit.UnitId)
			counters.AddOk(jobUnit.UnitId, callResult.SecondCount)
			return
		}

		if attemptCount > settings.RetryCount {
			// A stall is named as a stall, so a reader can tell a hung
			// provider from a slow one without reading the seconds.
			// A non-zero exit is named by its stderr, because the reason
			// the provider gave is the one the operator can act on; the
			// empty answer is only a symptom of it.
			reasonText := answerError
			if callResult.IsStall() {
				reasonText = fmt.Sprintf("stall: no output for %.0fs after %d chars",
					callResult.StallSeconds, callResult.CharCount)
			} else if callResult.ExitCode != 0 {
				stderrText := pattern.CutRuneText(
					strings.TrimSpace(callResult.StderrText), 1000)
				if stderrText != "" {
					reasonText = fmt.Sprintf("exit %d: %s", callResult.ExitCode, stderrText)
				} else {
					reasonText = fmt.Sprintf("exit %d: no stderr", callResult.ExitCode)
				}
			} else if reasonText == "" {
				reasonText = "no reason recorded"
			}
			jobStore.WriteFailed(jobUnit.UnitId, reasonText, map[string]any{
				"code":    callResult.ExitCode,
				"attempt": attemptCount,
				"seconds": RoundToTenth(callResult.SecondCount),
				"chars":   callResult.CharCount,
				"stall":   callResult.IsStall(),
			})
			counters.AddFail(jobUnit.UnitId, callResult.SecondCount)
			return
		}
		time.Sleep(time.Duration(settings.RetrySleepSeconds) * time.Second)
	}
}
// Run executes a job and returns the process exit code.
func Run(projectFolderPath string, settings store.JobSettings, unitList []store.JobUnit) int {
	if settingsError := store.ValidateJobSettings(settings); settingsError != nil {
		fmt.Fprintf(os.Stderr, "%v\n", settingsError)
		return 1
	}
	if settings.UnitLimit > 0 && settings.UnitLimit < len(unitList) {
		unitList = unitList[:settings.UnitLimit]
	}
	jobStore, storeError := store.NewJobStore(projectFolderPath, settings.JobName)
	if storeError != nil {
		fmt.Fprintf(os.Stderr, "%v\n", storeError)
		return 1
	}

	// A status read, a retry-failed clear, and a probe change nothing that a
	// second run would collide with, so they take no lock. Every other run
	// does.
	if !settings.PrintStatusOnly && !settings.RetryFailedOnly && !settings.ProbeOnly {
		runLock, lockError := lock.AcquireJobRunLock(jobStore.JobFolderPath)
		if lockError != nil {
			fmt.Fprintf(os.Stderr, "%v\n", lockError)
			return 3
		}
		defer runLock.Release()
	}
	logFolderPath := filepath.Join(projectFolderPath, "logs")
	if createError := os.MkdirAll(logFolderPath, 0o755); createError != nil {
		fmt.Fprintf(os.Stderr, "cannot create %s: %v\n", logFolderPath, createError)
		return 1
	}
	progressFilePath := filepath.Join(logFolderPath, settings.JobName+".progress.log")

	if settings.RetryFailedOnly {
		clearedCount := 0
		for _, jobUnit := range unitList {
			if jobStore.IsFailed(jobUnit.UnitId) {
				jobStore.ClearFailed(jobUnit.UnitId)
				clearedCount++
			}
		}
		fmt.Printf("cleared %d failed markers for job %s\n", clearedCount, settings.JobName)
		return 0
	}

	counters := &RunCounters{}
	if settings.PrintStatusOnly {
		statusReport := BuildJobStatusReport(projectFolderPath,
			settings.JobName, unitList)
		fmt.Println(statusReport.StatusLineOf())
		fmt.Println(statusReport.HolderLineOf())
		return 0
	}

	if settings.ProbeOnly {
		return runProbe(projectFolderPath, jobStore, unitList, settings)
	}

	pendingUnitList := make([]store.JobUnit, 0, len(unitList))
	for _, jobUnit := range unitList {
		if !jobStore.IsDone(jobUnit.UnitId) {
			pendingUnitList = append(pendingUnitList, jobUnit)
		}
	}
	fmt.Printf("job %s: %d units, %d pending, %d workers, model %s\n",
		settings.JobName, len(unitList), len(pendingUnitList), settings.WorkerCount,
		settings.ModelName)
	fmt.Printf("timeout %ds per call, stall %ds, retries %d\n",
		settings.TimeoutSeconds, settings.StallSeconds, settings.RetryCount)
	fmt.Printf("progress log: %s\n", table.RelativePath(projectFolderPath, progressFilePath))
	if len(pendingUnitList) == 0 {
		fmt.Println("nothing to do")
		return 0
	}

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
			line := ProgressLineOf(jobStore, unitList, counters)
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
	unitChannel := make(chan store.JobUnit)
	workerGroup := sync.WaitGroup{}
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for jobUnit := range unitChannel {
				runUnitRecovering(projectFolderPath, jobStore, jobUnit, settings, counters)
			}
		}()
	}
	for _, jobUnit := range pendingUnitList {
		unitChannel <- jobUnit
	}
	close(unitChannel)
	workerGroup.Wait()

	close(stopChannel)
	<-progressDoneChannel

	finalLine := ProgressLineOf(jobStore, unitList, counters)
	fmt.Println(finalLine)
	if progressFileHandle, openError := os.OpenFile(progressFilePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); openError == nil {
		progressFileHandle.WriteString(finalLine + "  DONE\n")
		progressFileHandle.Close()
	}
	_, failedCount := jobStore.Counts(unitList)
	if failedCount > 0 {
		printFailureSummary(jobStore, unitList)
		return 2
	}
	return 0
}

// runProbe makes one call on the first pending unit and prints its rate.
//
// It saves no answer, so a probe never changes what the job holds. It answers
// the 2 questions a long run asks before it starts: does the provider accept
// this model, and how many characters per second does it produce.
func runProbe(projectFolderPath string, jobStore *store.JobStore,
	unitList []store.JobUnit, settings store.JobSettings) int {
	probeUnit := store.JobUnit{}
	probeFound := false
	for _, jobUnit := range unitList {
		if !jobStore.IsDone(jobUnit.UnitId) {
			probeUnit = jobUnit
			probeFound = true
			break
		}
	}
	if !probeFound {
		fmt.Println("nothing to probe: every unit has an answer")
		return 0
	}
	promptBytes, readError := os.ReadFile(filepath.Join(projectFolderPath,
		probeUnit.PromptFilePath))
	if readError != nil {
		fmt.Fprintf(os.Stderr, "cannot read prompt %s: %v\n",
			probeUnit.PromptFilePath, readError)
		return 1
	}
	imageFilePath := probeUnit.ImageFilePath
	if imageFilePath != "" {
		imageFilePath = filepath.Join(projectFolderPath, imageFilePath)
	}
	fmt.Printf("probe %s with model %s\n", probeUnit.UnitId, settings.ModelName)
	callResult := call.CallPi(projectFolderPath, string(promptBytes), settings, imageFilePath)
	charPerSecond := 0.0
	if callResult.SecondCount > 0 {
		charPerSecond = float64(callResult.CharCount) / callResult.SecondCount
	}
	fmt.Printf("exit=%d  seconds=%.1f  chars=%d  chars/s=%.1f\n",
		callResult.ExitCode, callResult.SecondCount, callResult.CharCount, charPerSecond)
	if callResult.IsStall() {
		fmt.Printf("stall: no output for %.0fs\n", callResult.StallSeconds)
	}
	if callResult.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "stderr: %s\n",
			pattern.CutRuneText(strings.TrimSpace(callResult.StderrText), 500))
		return 2
	}
	answerError := answer.AnswerErrorOf(callResult.StdoutText)
	if answerError != "" {
		fmt.Fprintf(os.Stderr, "answer rejected: %s\n", answerError)
		return 2
	}
	fmt.Println("answer accepted; no answer was saved")
	return 0
}

// printFailureSummary prints the failures of a run grouped by reason.
//
// A run that fails 200 units for 1 reason is 1 problem, so the summary counts
// reasons rather than units. The full record of each failure is in
// `logs/<job>.errors.log`.
func printFailureSummary(jobStore *store.JobStore, unitList []store.JobUnit) {
	reasonCountMap := map[string]int{}
	for _, jobUnit := range unitList {
		if !jobStore.IsFailed(jobUnit.UnitId) {
			continue
		}
		reasonText := jobStore.FailureReasonOf(jobUnit.UnitId)
		reasonCountMap[reasonText]++
	}
	if len(reasonCountMap) == 0 {
		return
	}
	fmt.Printf("failures by reason (%d reasons):\n", len(reasonCountMap))
	reasonList := make([]string, 0, len(reasonCountMap))
	for reasonText := range reasonCountMap {
		reasonList = append(reasonList, reasonText)
	}
	sort.Strings(reasonList)
	for _, reasonText := range reasonList {
		fmt.Printf("  %5d  %s\n", reasonCountMap[reasonText], reasonText)
	}
	fmt.Printf("full record: %s\n",
		table.RelativePath(filepath.Dir(filepath.Dir(jobStore.JobFolderPath)),
			jobStore.ErrorLogFilePath))
}
