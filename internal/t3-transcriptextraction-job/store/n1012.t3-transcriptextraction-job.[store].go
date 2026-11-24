package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// stateTimeLayout is the time format of the `at` field of a state line.
const stateTimeLayout = "2006-01-02T15:04:05"

// stateLineRecord is one line of `state.jsonl`.
//
// Every field is a pointer-free type that matches the written JSON, because
// 1 field of the wrong type makes the whole line fail to decode and the line
// then looks absent.
type stateLineRecord struct {
	UnitId    string  `json:"unit_id"`
	State     string  `json:"state"`
	Seconds   float64 `json:"seconds"`
	Stall     bool    `json:"stall"`
	AtText    string  `json:"at"`
	CharCount int     `json:"chars"`
	Attempt   int     `json:"attempt"`
}

// RecentRatePerSecond returns the answers per second over the last windowSeconds.
//
// The rate uses the finished times of the answers in the window, so a run that
// stalled for the last 10 minutes shows a rate near 0 and an ETA that grows,
// which is what a reader needs to see. A rate over the whole run would hide
// the stall.
func (store *JobStore) RecentRatePerSecond(windowSeconds float64) float64 {
	stateRecordList := store.readStateRecords()
	if len(stateRecordList) == 0 {
		return 0
	}
	nowTime := time.Now()
	windowStartTime := nowTime.Add(-time.Duration(windowSeconds * float64(time.Second)))
	windowOkCount := 0
	earliestTime := nowTime
	for _, stateRecord := range stateRecordList {
		if stateRecord.State != "ok" {
			continue
		}
		finishedTime, parseError := time.ParseInLocation(stateTimeLayout, stateRecord.AtText, time.Local)
		if parseError != nil {
			continue
		}
		if finishedTime.Before(windowStartTime) {
			continue
		}
		windowOkCount++
		if finishedTime.Before(earliestTime) {
			earliestTime = finishedTime
		}
	}
	if windowOkCount == 0 {
		return 0
	}
	spanSeconds := nowTime.Sub(earliestTime).Seconds()
	if spanSeconds < 1 {
		spanSeconds = 1
	}
	return float64(windowOkCount) / spanSeconds
}

// LastAnswerAgeText returns how long ago the last answer was written, as
// `12s ago` or `4m10s ago`, or `-` when the job has no answer.
func (store *JobStore) LastAnswerAgeText() string {
	stateRecordList := store.readStateRecords()
	latestTime := time.Time{}
	for _, stateRecord := range stateRecordList {
		if stateRecord.State != "ok" {
			continue
		}
		finishedTime, parseError := time.ParseInLocation(stateTimeLayout, stateRecord.AtText, time.Local)
		if parseError != nil {
			continue
		}
		if finishedTime.After(latestTime) {
			latestTime = finishedTime
		}
	}
	if latestTime.IsZero() {
		return "-"
	}
	return formatAgeText(time.Since(latestTime).Seconds()) + " ago"
}

// StallCount returns how many state lines record a stall.
func (store *JobStore) StallCount() int {
	stallCount := 0
	for _, stateRecord := range store.readStateRecords() {
		if stateRecord.Stall {
			stallCount++
		}
	}
	return stallCount
}

// readStateRecords reads every line of `state.jsonl`.
//
// A line that does not decode is skipped, because a killed run can leave a
// half-written last line and the earlier lines are still good.
func (store *JobStore) readStateRecords() []stateLineRecord {
	stateFileHandle, openError := os.Open(store.StateFilePath)
	if openError != nil {
		return nil
	}
	defer stateFileHandle.Close()
	stateRecordList := make([]stateLineRecord, 0, 1024)
	scannerHandle := bufio.NewScanner(stateFileHandle)
	scannerHandle.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scannerHandle.Scan() {
		lineText := strings.TrimSpace(scannerHandle.Text())
		if lineText == "" {
			continue
		}
		stateRecord := stateLineRecord{}
		if decodeError := json.Unmarshal([]byte(lineText), &stateRecord); decodeError != nil {
			continue
		}
		stateRecordList = append(stateRecordList, stateRecord)
	}
	return stateRecordList
}

// formatAgeText writes a second count as `12s` or `4m10s`.
func formatAgeText(secondCount float64) string {
	wholeSecondCount := int(secondCount)
	if wholeSecondCount < 60 {
		return fmt.Sprintf("%ds", wholeSecondCount)
	}
	minuteCount := wholeSecondCount / 60
	if minuteCount < 60 {
		return fmt.Sprintf("%dm%02ds", minuteCount, wholeSecondCount%60)
	}
	return fmt.Sprintf("%dh%02dm", minuteCount/60, minuteCount%60)
}