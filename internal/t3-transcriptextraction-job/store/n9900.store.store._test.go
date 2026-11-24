package store

import (
	"os"
	"strings"
	"testing"
)

// validSettings returns settings that pass validation.
func validSettings() JobSettings {
	return JobSettings{
		JobName:        "relevance-window-vote",
		WorkerCount:    1,
		ModelName:      "test-model",
		ProviderName:   "test-provider",
		TimeoutSeconds: 120,
		RetryCount:     2,
		PollSeconds:    30,
		StallSeconds:   90,
	}
}

// TestEveryBadFlagIsRefused proves each bad settings value stops the run with
// a message naming its flag.
func TestEveryBadFlagIsRefused(t *testing.T) {
	caseList := []struct {
		flagText string
		mutate   func(*JobSettings)
	}{
		{"--jobs", func(s *JobSettings) { s.WorkerCount = 0 }},
		{"--timeout", func(s *JobSettings) { s.TimeoutSeconds = -1 }},
		{"--retries", func(s *JobSettings) { s.RetryCount = -1 }},
		{"--poll", func(s *JobSettings) { s.PollSeconds = 0 }},
		{"--stall", func(s *JobSettings) { s.StallSeconds = 0 }},
		{"--model", func(s *JobSettings) { s.ModelName = "" }},
		{"--provider", func(s *JobSettings) { s.ProviderName = "" }},
	}
	for _, testCase := range caseList {
		settings := validSettings()
		testCase.mutate(&settings)
		validateError := ValidateJobSettings(settings)
		if validateError == nil {
			t.Fatalf("%s: a bad value was accepted", testCase.flagText)
		}
		if !strings.Contains(validateError.Error(), testCase.flagText) {
			t.Fatalf("%s: the message does not name the flag: %v",
				testCase.flagText, validateError)
		}
		t.Logf("%s -> %v", testCase.flagText, validateError)
	}
}

// TestGoodSettingsAccepted proves the valid settings pass.
func TestGoodSettingsAccepted(t *testing.T) {
	if validateError := ValidateJobSettings(validSettings()); validateError != nil {
		t.Fatalf("valid settings were refused: %v", validateError)
	}
}

// TestFailureWritesErrorLogLine proves a failure appends one line to
// logs/<job>.errors.log with the time, unit, attempt, exit, seconds, chars,
// and reason.
func TestFailureWritesErrorLogLine(t *testing.T) {
	projectFolderPath := t.TempDir()
	jobStore, storeError := NewJobStore(projectFolderPath, "relevance-window-vote")
	if storeError != nil {
		t.Fatalf("cannot make the store: %v", storeError)
	}
	jobStore.WriteFailed("unit-1", "stall: no output for 3s after 0 chars",
		map[string]any{"code": 125, "attempt": 1, "seconds": 3.0, "chars": 0})
	logBytes, readError := os.ReadFile(jobStore.ErrorLogFilePath)
	if readError != nil {
		t.Fatalf("no error log was written: %v", readError)
	}
	logText := strings.TrimSpace(string(logBytes))
	for _, wantedText := range []string{"unit=unit-1", "attempt=1", "exit=125",
		"seconds=3", "chars=0", "reason=stall: no output for 3s after 0 chars"} {
		if !strings.Contains(logText, wantedText) {
			t.Fatalf("the log line does not hold %q: %s", wantedText, logText)
		}
	}
	t.Logf("log line: %s", logText)
}

// TestFailureReasonIsCut proves a long reason is cut to 300 characters.
func TestFailureReasonIsCut(t *testing.T) {
	projectFolderPath := t.TempDir()
	jobStore, storeError := NewJobStore(projectFolderPath, "relevance-window-vote")
	if storeError != nil {
		t.Fatalf("cannot make the store: %v", storeError)
	}
	longReasonText := strings.Repeat("x", 900)
	jobStore.WriteFailed("unit-2", longReasonText, map[string]any{"code": 1, "attempt": 1})
	logBytes, _ := os.ReadFile(jobStore.ErrorLogFilePath)
	logText := string(logBytes)
	reasonIndex := strings.Index(logText, "reason=")
	reasonText := strings.TrimSpace(logText[reasonIndex+len("reason="):])
	if len(reasonText) > 300 {
		t.Fatalf("the reason is %d characters, over the 300 limit", len(reasonText))
	}
	t.Logf("reason cut to %d characters", len(reasonText))
}
