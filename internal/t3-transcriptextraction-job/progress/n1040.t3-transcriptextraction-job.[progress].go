package progress

import (
	"fmt"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/unit"
)
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

// AddOk records a finished unit.
func (counters *RunCounters) AddOk(unitId string, secondCount float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.okCount++
	counters.lastUnitId = unitId
	counters.lastSecondCount = secondCount
	counters.totalSecondCount += secondCount
}

// AddFail records a failed unit.
func (counters *RunCounters) AddFail(unitId string, secondCount float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.failCount++
	counters.lastUnitId = unitId
	counters.lastSecondCount = secondCount
}

// AddInFlight changes the count of calls in flight.
func (counters *RunCounters) AddInFlight(deltaCount int) {
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

// ProgressLineOf returns the one-line progress report.
func ProgressLineOf(store *store.JobStore, unitList []unit.JobUnit, counters *RunCounters,
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

// RoundToTenth rounds a second count to 1 decimal place.
func RoundToTenth(secondCount float64) float64 {
	return float64(int(secondCount*10+0.5)) / 10
}
