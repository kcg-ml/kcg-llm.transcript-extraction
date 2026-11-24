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

// TestTrickleIsKilledAsStall proves a call that writes under the floor rate is
// killed at the grace period and recorded as a stall, not a timeout.
func TestTrickleIsKilledAsStall(t *testing.T) {
	// 1 character every 2 seconds is 0.5 chars/s, under the 10 chars/s floor.
	writeFakePi(t, "for i in $(seq 1 200); do printf 'x'; sleep 2; done\n")
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

// TestNoGrowthIsKilledAsStall proves a call whose output stops is killed at
// the stall bound.
func TestNoGrowthIsKilledAsStall(t *testing.T) {
	writeFakePi(t, "sleep 300\n")
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
