package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
)

// TestPanicIsRecovered proves a panicking unit writes a failure file and the
// recovery itself does not panic.
//
// The counters are nil, which is what a panic leaves behind when it fires
// before the counters are made. The recovery must survive that.
func TestPanicIsRecovered(t *testing.T) {
	projectFolderPath := t.TempDir()
	jobStore, storeError := store.NewJobStore(projectFolderPath, "relevance-window-vote")
	if storeError != nil {
		t.Fatalf("cannot make the store: %v", storeError)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("the recovery panicked: %v", recovered)
		}
	}()
	// A nil counters makes runUnit panic, which is the case under test.
	runUnitRecovering(projectFolderPath, jobStore,
		store.JobUnit{UnitId: "unit-1", PromptFilePath: "no-such-prompt.txt"},
		store.JobSettings{JobName: "relevance-window-vote"}, nil)
	failedFolderPath := filepath.Join(projectFolderPath, "model-answer", "job",
		"relevance-window-vote", "failed")
	entries, readError := os.ReadDir(failedFolderPath)
	if readError != nil || len(entries) == 0 {
		t.Fatalf("no failure file was written: %v", readError)
	}
	failedBytes, _ := os.ReadFile(filepath.Join(failedFolderPath, entries[0].Name()))
	if !containsText(string(failedBytes), "panic") {
		t.Fatalf("the failure file does not name the panic: %s", failedBytes)
	}
	t.Logf("failure file: %s", entries[0].Name())
}

// containsText reports whether text holds subText.
func containsText(text string, subText string) bool {
	return len(text) >= len(subText) && indexOfText(text, subText) >= 0
}

// indexOfText returns the index of subText in text, or -1.
func indexOfText(text string, subText string) int {
	for index := 0; index+len(subText) <= len(text); index++ {
		if text[index:index+len(subText)] == subText {
			return index
		}
	}
	return -1
}
