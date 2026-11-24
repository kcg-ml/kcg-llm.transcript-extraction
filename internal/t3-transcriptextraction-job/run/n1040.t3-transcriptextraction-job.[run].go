package run

import (
	"fmt"
	"sync"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
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

	// finishTimeList holds when the last answers finished, newest last, so
	// the rate covers a sliding window instead of the whole run. A rate over
	// the whole run hides a stall: 1000 answers in 10 minutes still read as
	// 1.7/s during the 5 minutes the provider has been silent.
	finishTimeList []time.Time
}

// rateWindowSeconds is the span the progress rate covers.
const rateWindowSeconds = 60.0

// AddOk records a finished unit.
func (counters *RunCounters) AddOk(unitId string, secondCount float64) {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.okCount++
	counters.lastUnitId = unitId
	counters.lastSecondCount = secondCount
	counters.totalSecondCount += secondCount
	counters.finishTimeList = append(counters.finishTimeList, time.Now())
	counters.dropOldFinishTimes()
}

// dropOldFinishTimes removes the finish times outside the rate window.
//
// The caller holds the lock. The list is kept in order, so the stale times
// are always at the front and the cut is one slice operation.
func (counters *RunCounters) dropOldFinishTimes() {
	windowStartTime := time.Now().Add(-time.Duration(rateWindowSeconds * float64(time.Second)))
	cutIndex := 0
	for cutIndex < len(counters.finishTimeList) &&
		counters.finishTimeList[cutIndex].Before(windowStartTime) {
		cutIndex++
	}
	if cutIndex > 0 {
		counters.finishTimeList = counters.finishTimeList[cutIndex:]
	}
}

// RecentRatePerSecond returns the answers per second over the rate window.
func (counters *RunCounters) RecentRatePerSecond() float64 {
	counters.lockHandle.Lock()
	defer counters.lockHandle.Unlock()
	counters.dropOldFinishTimes()
	if len(counters.finishTimeList) == 0 {
		return 0
	}
	// The span runs from the oldest answer in the window to now, so a run
	// that has just started reports its true rate instead of dividing by a
	// window it has not filled.
	spanSeconds := time.Since(counters.finishTimeList[0]).Seconds()
	if spanSeconds < 1 {
		spanSeconds = 1
	}
	return float64(len(counters.finishTimeList)) / spanSeconds
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

// FormatEtaText writes a duration as `12m03s` or `2h05m`.
func FormatEtaText(secondCount float64) string {
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
//
// The rate and the ETA cover the last 60 seconds, so a stalled run shows its
// rate falling to 0 and its ETA growing, which is what a reader needs to see.
// The failed count is on the line, because a run that answers 200 units and
// fails 200 is not half done.
func ProgressLineOf(store *store.JobStore, unitList []store.JobUnit,
	counters *RunCounters) string {
	totalCount := len(unitList)
	doneCount, failedCount := store.Counts(unitList)
	okCount, inFlightCount, lastUnitId, lastSecondCount := counters.snapshot()
	ratePerSecond := counters.RecentRatePerSecond()
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
		okCount, failedCount, ratePerSecond, FormatEtaText(etaSecondCount),
		inFlightCount, lastText)
}

// RoundToTenth rounds a second count to 1 decimal place.
func RoundToTenth(secondCount float64) float64 {
	return float64(int(secondCount*10+0.5)) / 10
}
