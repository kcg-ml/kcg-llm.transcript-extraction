package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
)

// JobStatusReport is what a reader learns about a job without running it.
//
// The report is built from files alone, so it answers while another process
// holds the lock and runs the job. Live counters belong to that process and
// are not readable here, so the rate comes from the state file's timestamps.
type JobStatusReport struct {
	JobName             string
	TotalCount          int
	DoneCount           int
	FailedCount         int
	PendingCount        int
	RatePerSecond       float64
	EtaText             string
	LastAnswerAgeText   string
	StallCount          int
	InFlightCount       int
	HolderPid           int
	HolderStartedAtText string
	IsRunning           bool
}

// BuildJobStatusReport reads the files of one job and returns its status.
func BuildJobStatusReport(projectFolderPath string, jobName string,
	unitList []store.JobUnit) JobStatusReport {
	jobStore, storeError := store.NewJobStore(projectFolderPath, jobName)
	report := JobStatusReport{JobName: jobName, TotalCount: len(unitList)}
	if storeError != nil {
		return report
	}
	report.DoneCount, report.FailedCount = jobStore.Counts(unitList)
	report.PendingCount = report.TotalCount - report.DoneCount
	report.StallCount = jobStore.StallCount()
	report.HolderPid, report.HolderStartedAtText, report.IsRunning =
		readLockHolder(jobStore.JobFolderPath)

	report.RatePerSecond = jobStore.RecentRatePerSecond(60)
	if report.RatePerSecond > 0 {
		report.EtaText = FormatEtaText(float64(report.PendingCount) / report.RatePerSecond)
	} else {
		report.EtaText = "--"
	}
	report.LastAnswerAgeText = jobStore.LastAnswerAgeText()
	// The calls in flight live in the memory of the running process, so the
	// runner writes them to a live file and this reader picks them up. A
	// finished run removes the file, so a stale count is never shown.
	if liveStatusRecord, liveStatusFound := jobStore.ReadLiveStatus(); liveStatusFound {
		report.InFlightCount = liveStatusRecord.InFlightCount
	}
	return report
}

// StatusLineOf writes the report as one line, in the shape of the progress line.
func (report JobStatusReport) StatusLineOf() string {
	percentValue := 0.0
	if report.TotalCount > 0 {
		percentValue = 100.0 * float64(report.DoneCount) / float64(report.TotalCount)
	}
	return fmt.Sprintf(
		"[%s] %d/%d  %5.1f%%  ok=%d fail=%d pending=%d  rate=%.2f/s  eta=%s  inflight=%d  last=%s  stalls=%d",
		time.Now().Format("15:04:05"), report.DoneCount, report.TotalCount, percentValue,
		report.DoneCount, report.FailedCount, report.PendingCount,
		report.RatePerSecond, report.EtaText, report.InFlightCount,
		report.LastAnswerAgeText, report.StallCount)
}

// HolderLineOf writes who runs the job, or that nobody does.
func (report JobStatusReport) HolderLineOf() string {
	if !report.IsRunning {
		return "no process holds the lock"
	}
	return fmt.Sprintf("running as pid %d since %s", report.HolderPid,
		report.HolderStartedAtText)
}

// readLockHolder reads the pid and start time of the run lock.
func readLockHolder(jobFolderPath string) (int, string, bool) {
	lockFilePath := filepath.Join(jobFolderPath, "run.lock")
	lockBytes, readError := os.ReadFile(lockFilePath)
	if readError != nil {
		return 0, "", false
	}
	holderPid := 0
	startedAtText := "an unknown time"
	for _, lineText := range strings.Split(string(lockBytes), "\n") {
		lineText = strings.TrimSpace(lineText)
		if strings.HasPrefix(lineText, "pid=") {
			fmt.Sscanf(strings.TrimPrefix(lineText, "pid="), "%d", &holderPid)
		}
		if strings.HasPrefix(lineText, "started_at=") {
			startedAtText = strings.TrimPrefix(lineText, "started_at=")
		}
	}
	if holderPid <= 0 {
		return 0, "", false
	}
	return holderPid, startedAtText, true
}