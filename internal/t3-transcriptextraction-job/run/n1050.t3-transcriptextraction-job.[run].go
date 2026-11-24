package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/answer"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/call"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/progress"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/unit"
)
// runUnit makes the calls of one unit until it answers or runs out of retries.
func runUnit(projectFolderPath string, store *store.JobStore, unit unit.JobUnit,
	settings unit.JobSettings, counters *progress.RunCounters) {
	if store.IsDone(unit.UnitId) {
		return
	}
	promptBytes, readError := os.ReadFile(filepath.Join(projectFolderPath, unit.PromptFilePath))
	if readError != nil {
		store.WriteFailed(unit.UnitId, fmt.Sprintf("cannot read prompt %s: %v",
			unit.PromptFilePath, readError), map[string]any{"code": 0, "attempt": 0})
		counters.AddFail(unit.UnitId, 0)
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
			store.WriteDone(unit.UnitId, callResult.StdoutText, map[string]any{
				"model":   settings.ModelName,
				"seconds": progress.RoundToTenth(callResult.SecondCount),
				"attempt": attemptCount,
				"chars":   len(callResult.StdoutText),
			})
			store.ClearFailed(unit.UnitId)
			counters.AddOk(unit.UnitId, callResult.SecondCount)
			return
		}

		if attemptCount > settings.RetryCount {
			reasonText := answerError
			if reasonText == "" {
				reasonText = pattern.CutRuneText(strings.TrimSpace(callResult.StderrText), 1000)
			}
			store.WriteFailed(unit.UnitId, reasonText, map[string]any{
				"code":    callResult.ExitCode,
				"attempt": attemptCount,
				"seconds": progress.RoundToTenth(callResult.SecondCount),
			})
			counters.AddFail(unit.UnitId, callResult.SecondCount)
			return
		}
		time.Sleep(time.Duration(settings.RetrySleepSeconds) * time.Second)
	}
}
// Run executes a job and returns the process exit code.
func Run(projectFolderPath string, settings unit.JobSettings, unitList []unit.JobUnit) int {
	if settings.UnitLimit > 0 && settings.UnitLimit < len(unitList) {
		unitList = unitList[:settings.UnitLimit]
	}
	store, storeError := store.NewJobStore(projectFolderPath, settings.JobName)
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

	counters := &progress.RunCounters{}
	if settings.PrintStatusOnly {
		fmt.Println(progress.ProgressLineOf(store, unitList, counters, time.Now()))
		return 0
	}

	pendingUnitList := make([]unit.JobUnit, 0, len(unitList))
	for _, unit := range unitList {
		if !store.IsDone(unit.UnitId) {
			pendingUnitList = append(pendingUnitList, unit)
		}
	}
	fmt.Printf("job %s: %d units, %d pending, %d workers, model %s\n",
		settings.JobName, len(unitList), len(pendingUnitList), settings.WorkerCount,
		settings.ModelName)
	fmt.Printf("progress log: %s\n", table.RelativePath(projectFolderPath, progressFilePath))
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
			line := progress.ProgressLineOf(store, unitList, counters, startedAt)
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
	unitChannel := make(chan unit.JobUnit)
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

	finalLine := progress.ProgressLineOf(store, unitList, counters, startedAt)
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
