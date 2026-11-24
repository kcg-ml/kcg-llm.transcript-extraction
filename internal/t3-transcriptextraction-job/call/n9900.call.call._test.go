package call

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
)

// jobSettingsForTest returns settings a test can pass to CallPi.
func jobSettingsForTest() store.JobSettings {
	return store.JobSettings{
		JobName:        "relevance-window-vote",
		WorkerCount:    1,
		ModelName:      "test-model",
		ProviderName:   "test-provider",
		TimeoutSeconds: 120,
		StallSeconds:   90,
		PollSeconds:    30,
	}
}

// writeFakePi writes a fake `pi` that runs the given shell body, and points
// PI_BIN at it.
func writeFakePi(t *testing.T, bodyText string) {
	t.Helper()
	folderPath := t.TempDir()
	piPath := filepath.Join(folderPath, "pi")
	if writeError := os.WriteFile(piPath, []byte("#!/bin/bash\n"+bodyText), 0o755); writeError != nil {
		t.Fatalf("cannot write the fake pi: %v", writeError)
	}
	t.Setenv("PI_BIN", piPath)
}

// TestLateWholeAnswerSurvives proves a call that thinks for a long time and
// then writes its whole answer in one write is not killed.
//
// The provider this pipeline uses does not stream: a measured 181224-char
// prompt answered in 330s with its first byte at 328s. A rule that judged the
// call before its first byte would kill every such call.
func TestLateWholeAnswerSurvives(t *testing.T) {
	// 60s of silence, then the whole answer at once.
	writeFakePi(t, "sleep 60\nprintf 'PAPERS: none\\nTALKS: none\\nCODE: none\\nMETHODS: none\\nMODELS: none\\n'\n")
	settings := jobSettingsForTest()
	settings.TimeoutSeconds = 200
	settings.StallSeconds = 90
	callResult := CallPi(t.TempDir(), "prompt", settings, "")
	if callResult.IsStall() {
		t.Fatalf("a late whole answer was killed as a stall: %s", callResult.StderrText)
	}
	if callResult.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", callResult.ExitCode, callResult.StderrText)
	}
	t.Logf("survived: %.1fs, %d chars", callResult.SecondCount, callResult.CharCount)
}

// TestTrickleIsKilledAsStall proves a call that writes under the floor rate
// after its first byte is killed at the grace period and recorded as a stall,
// not a timeout.
func TestTrickleIsKilledAsStall(t *testing.T) {
	// A first byte at once, then 1 character every 2 seconds: 0.5 chars/s,
	// under the 10 chars/s floor, measured from the first byte.
	writeFakePi(t, "printf 'x'\nfor i in $(seq 1 200); do printf 'x'; sleep 2; done\n")
	settings := jobSettingsForTest()
	settings.TimeoutSeconds = 120
	settings.StallSeconds = 90
	startedAt := time.Now()
	callResult := CallPi(t.TempDir(), "prompt", settings, "")
	elapsedSeconds := time.Since(startedAt).Seconds()
	if !callResult.IsStall() {
		t.Fatalf("expected a stall, got exit=%d stderr=%q",
			callResult.ExitCode, callResult.StderrText)
	}
	if callResult.ExitCode != 125 {
		t.Fatalf("expected exit 125, got %d", callResult.ExitCode)
	}
	if elapsedSeconds > 45 {
		t.Fatalf("expected the kill at the %.0fs grace, took %.0fs",
			TrickleGraceSeconds, elapsedSeconds)
	}
	t.Logf("killed at %.1fs: %s", elapsedSeconds, callResult.StderrText)
}

// TestNoGrowthIsKilledAsStall proves a call whose output stops after it has
// started is killed at the stall bound.
func TestNoGrowthIsKilledAsStall(t *testing.T) {
	// One byte, then silence: the call has started, so the no-growth rule
	// applies to it.
	writeFakePi(t, "printf 'x'\nsleep 300\n")
	settings := jobSettingsForTest()
	settings.TimeoutSeconds = 120
	settings.StallSeconds = 3
	callResult := CallPi(t.TempDir(), "prompt", settings, "")
	if !callResult.IsStall() || callResult.ExitCode != 125 {
		t.Fatalf("expected a stall with exit 125, got exit=%d stall=%v",
			callResult.ExitCode, callResult.IsStall())
	}
	t.Logf("killed: %s", callResult.StderrText)
}

// TestSilentCallIsBoundedByTimeout proves a call that never writes is bounded
// by the timeout and not killed as a stall.
//
// The provider holds the whole answer and writes it at the end, so a call
// with no output yet may be thinking. Only the timeout bounds it.
func TestSilentCallIsBoundedByTimeout(t *testing.T) {
	writeFakePi(t, "sleep 300\n")
	settings := jobSettingsForTest()
	settings.TimeoutSeconds = 5
	settings.StallSeconds = 3
	callResult := CallPi(t.TempDir(), "prompt", settings, "")
	if callResult.IsStall() {
		t.Fatalf("a silent call was killed as a stall: %s", callResult.StderrText)
	}
	if callResult.ExitCode != 124 {
		t.Fatalf("expected the timeout exit 124, got %d", callResult.ExitCode)
	}
	t.Logf("bounded by the timeout: %s", callResult.StderrText)
}
