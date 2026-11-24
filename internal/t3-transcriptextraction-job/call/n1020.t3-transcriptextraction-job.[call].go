package call

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/unit"
)
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
func CallPi(projectFolderPath string, promptText string, settings unit.JobSettings,
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
