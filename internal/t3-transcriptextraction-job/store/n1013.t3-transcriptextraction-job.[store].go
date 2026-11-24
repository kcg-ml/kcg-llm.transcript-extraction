package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LiveStatusFileName is the file the running process writes its live counts
// to, so `--status` can name the calls in flight.
//
// The counts live in the memory of the running process, and `--status` is a
// second process that reads files only. The runner therefore writes the
// counts to this file on a short tick, and the status reader picks them up.
const LiveStatusFileName = "live.status"

// LiveStatusRecord is the live count of one running job.
type LiveStatusRecord struct {
	// OkCount is how many units the run has answered.
	OkCount int

	// InFlightCount is how many calls the run has open right now.
	InFlightCount int

	// LastUnitId names the unit that finished most recently.
	LastUnitId string

	// LastSecondCount is how long that unit took.
	LastSecondCount float64
}

// LiveStatusFilePath returns the path of the live status file of one job.
func (store *JobStore) LiveStatusFilePath() string {
	return filepath.Join(store.JobFolderPath, LiveStatusFileName)
}

// WriteLiveStatus writes the live counts of the running job.
//
// A write failure is ignored: the live file is an observation aid, and a run
// must never stop because a reader cannot see it.
func (store *JobStore) WriteLiveStatus(okCount int, inFlightCount int,
	lastUnitId string, lastSecondCount float64) {
	statusText := fmt.Sprintf("ok=%d\ninflight=%d\nlast_unit=%s\nlast_seconds=%.1f\n",
		okCount, inFlightCount, lastUnitId, lastSecondCount)
	_ = os.WriteFile(store.LiveStatusFilePath(), []byte(statusText), 0o644)
}

// ReadLiveStatus returns the live counts of the running job.
//
// The second return value is false when no run has written the file, which is
// what a finished run leaves behind after it removes the file.
func (store *JobStore) ReadLiveStatus() (LiveStatusRecord, bool) {
	statusBytes, readError := os.ReadFile(store.LiveStatusFilePath())
	if readError != nil {
		return LiveStatusRecord{}, false
	}
	statusRecord := LiveStatusRecord{}
	for _, lineText := range strings.Split(string(statusBytes), "\n") {
		lineText = strings.TrimSpace(lineText)
		keyText, valueText, hasSeparator := strings.Cut(lineText, "=")
		if !hasSeparator {
			continue
		}
		switch keyText {
		case "ok":
			statusRecord.OkCount, _ = strconv.Atoi(valueText)
		case "inflight":
			statusRecord.InFlightCount, _ = strconv.Atoi(valueText)
		case "last_unit":
			statusRecord.LastUnitId = valueText
		case "last_seconds":
			statusRecord.LastSecondCount, _ = strconv.ParseFloat(valueText, 64)
		}
	}
	return statusRecord, true
}

// RemoveLiveStatus removes the live status file, so a finished run leaves no
// stale count behind.
func (store *JobStore) RemoveLiveStatus() {
	_ = os.Remove(store.LiveStatusFilePath())
}