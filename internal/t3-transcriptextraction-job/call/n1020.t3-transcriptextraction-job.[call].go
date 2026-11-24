package call

import (
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

)
// PiCallResult is the outcome of one model call.
type PiCallResult struct {
	ExitCode    int
	StdoutText  string
	StderrText  string
	SecondCount float64

	// StallSeconds is how long the output stood still when the call was
	// killed for a stall, and 0 when the call was not killed for one.
	StallSeconds float64

	// CharCount is the number of characters the call produced.
	CharCount int
}

// IsStall reports whether the call was killed because its output stopped.
func (callResult PiCallResult) IsStall() bool {
	return callResult.StallSeconds > 0
}

// CallPi runs one pi call and returns its outcome.
//
// An image path is passed as an `@<path>` argument, which pi turns into an
// image attachment for a model that accepts image input. A call that outlives
// its timeout is killed, because a child that holds the pipe open would
// otherwise keep the call alive past its timeout. A call whose output stops
// growing for stallSeconds is killed sooner, because a hung provider would
// otherwise hold its worker for the whole timeout. A call that keeps writing
// but writes under TrickleCharRatePerSecond is killed at
// TrickleGraceSeconds, because a provider that trickles 1 character every 2
// seconds moves the growth clock and would otherwise hold its worker for the
// whole timeout and then be recorded as a timeout rather than a stall.
func CallPi(projectFolderPath string, promptText string, settings store.JobSettings,
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
	stdoutWatch := NewStallWatchBuffer()
	stderrBuffer := bytes.Buffer{}
	commandHandle.Stdout = stdoutWatch
	commandHandle.Stderr = &stderrBuffer

	// The child is a shell script that runs the real process as its own
	// child. Killing only the child leaves the grandchild holding the stdout
	// pipe, and Wait then blocks forever on that pipe. The child therefore
	// gets its own process group, and a kill signals the whole group.
	commandHandle.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	commandHandle.Cancel = func() error {
		if commandHandle.Process == nil {
			return nil
		}
		return syscall.Kill(-commandHandle.Process.Pid, syscall.SIGKILL)
	}

	if startError := commandHandle.Start(); startError != nil {
		return PiCallResult{
			ExitCode:    127,
			StderrText:  startError.Error(),
			SecondCount: time.Since(startedAt).Seconds(),
		}
	}

	// The watcher kills the child when its output stops growing. The wait
	// goroutine then returns, so the watcher must stop before the buffers
	// are read.
	stallSeconds := float64(settings.StallSeconds)
	stallChannel := make(chan StallRecord, 1)
	watcherStopChannel := make(chan struct{})
	watcherDoneChannel := make(chan struct{})
	go func() {
		defer close(watcherDoneChannel)
		if stallSeconds <= 0 {
			return
		}
		tickerHandle := time.NewTicker(500 * time.Millisecond)
		defer tickerHandle.Stop()
		for {
			select {
			case <-watcherStopChannel:
				return
			case <-tickerHandle.C:
				// The provider this pipeline uses does not stream: it holds
				// the whole answer and writes it at the end, so the time
				// before the first byte is the provider thinking and not the
				// call stalling. A measured 181224-char prompt answered in
				// 330s with its first byte at 328s. Neither rule may judge
				// the call before that byte arrives, or a healthy call is
				// killed for being slow to start.
				if !stdoutWatch.HasFirstByte() {
					continue
				}
				stoodStillSeconds := stdoutWatch.SecondsSinceGrowth()
				if stoodStillSeconds >= stallSeconds {
					stallChannel <- StallRecord{KindText: StallKindNoGrowth,
						SecondCount: stoodStillSeconds}
					cancelHandle()
					return
				}
				// The trickle rule. A call that has written for the grace
				// period and still writes under the floor rate cannot finish
				// its answer, so it is killed as a stall and not as a
				// timeout. The clock starts at the first byte, so a call
				// that writes its whole answer in one late write is not
				// judged on the time it spent thinking.
				writingSeconds := stdoutWatch.SecondsSinceFirstByte()
				if writingSeconds >= TrickleGraceSeconds &&
					stdoutWatch.CharRatePerSecond() < TrickleCharRatePerSecond {
					stallChannel <- StallRecord{KindText: StallKindTrickle,
						SecondCount: writingSeconds,
						CharRate:    stdoutWatch.CharRatePerSecond()}
					cancelHandle()
					return
				}
			}
		}
	}()

	waitError := commandHandle.Wait()
	close(watcherStopChannel)
	<-watcherDoneChannel
	elapsedSeconds := time.Since(startedAt).Seconds()
	stdoutText := stdoutWatch.Text()

	stallRecord := StallRecord{}
	select {
	case stallRecord = <-stallChannel:
	default:
	}
	if stallRecord.SecondCount > 0 {
		stallReasonText := fmt.Sprintf("stall: no output for %.0fs, process killed",
			stallRecord.SecondCount)
		if stallRecord.KindText == StallKindTrickle {
			stallReasonText = fmt.Sprintf(
				"stall: trickled %.2f chars/s for %.0fs, under the %.0f chars/s floor, process killed",
				stallRecord.CharRate, stallRecord.SecondCount, TrickleCharRatePerSecond)
		}
		return PiCallResult{
			ExitCode:     125,
			StdoutText:   stdoutText,
			StderrText:   stallReasonText,
			SecondCount:  elapsedSeconds,
			StallSeconds: stallRecord.SecondCount,
			CharCount:    len(stdoutText),
		}
	}

	if contextHandle.Err() == context.DeadlineExceeded {
		return PiCallResult{
			ExitCode:    124,
			StdoutText:  stdoutText,
			StderrText:  fmt.Sprintf("timeout after %ds, process killed", settings.TimeoutSeconds),
			SecondCount: elapsedSeconds,
			CharCount:   len(stdoutText),
		}
	}
	exitCode := 0
	if waitError != nil {
		if exitError, isExitError := waitError.(*exec.ExitError); isExitError {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 127
			stderrBuffer.WriteString(waitError.Error())
		}
	}
	return PiCallResult{
		ExitCode:    exitCode,
		StdoutText:  stdoutText,
		StderrText:  stderrBuffer.String(),
		SecondCount: elapsedSeconds,
		CharCount:   len(stdoutText),
	}
}

// ShrinkImage returns the path of a copy of an image whose long side is at
// most maxSide pixels.
//
// The provider `zai` refuses a photo whose content filter fires on the
// full-size pixels with HTTP 400 `code 1301`. The same photo at 200 px passes,
// so a refused photo is retried once at 200 px. The copy goes to
// `model-answer/shrunk-photo/`, never into the export folder.
func ShrinkImage(projectFolderPath string, imageFilePath string, maxSide int) string {
	stemName := strings.TrimSuffix(filepath.Base(imageFilePath),
		filepath.Ext(imageFilePath))
	shrunkFolderPath := filepath.Join(projectFolderPath, "model-answer", "shrunk-photo")
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
