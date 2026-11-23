// Package jobunit reads the unit list of one durable job from the data tables.
//
// A job is named after the table its units come from, so a reader can tell
// which stage a job answers:
//
//	w10, w100, month, meta   one window of data/windows.csv
//	rel                      one window of data/relevance_windows.csv
//	photo                    one row of data/photos.csv, with its image
package jobunit

import (
	"fmt"
	"path/filepath"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/durablejob"
)

// JobNameList names the jobs the runner accepts, in the order a reader runs
// them.
var JobNameList = []string{"w10", "w100", "month", "meta", "rel", "photo"}

// IsKnownJobName reports whether a job name is one the runner accepts.
func IsKnownJobName(jobName string) bool {
	for _, knownJobName := range JobNameList {
		if knownJobName == jobName {
			return true
		}
	}
	return false
}

// unitListOfWindowKind returns one unit per window of one kind.
func unitListOfWindowKind(projectFolderPath string, windowKind string) ([]durablejob.JobUnit, error) {
	windowTable, readError := csvtable.ReadTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]durablejob.JobUnit, 0, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		if windowRow["window_kind"] != windowKind {
			continue
		}
		unitList = append(unitList, durablejob.JobUnit{
			UnitId:         windowRow["window_id"],
			PromptFilePath: windowRow["prompt_path"],
		})
	}
	return unitList, nil
}

// unitListOfRelevance returns one unit per relevance window.
func unitListOfRelevance(projectFolderPath string) ([]durablejob.JobUnit, error) {
	windowTable, readError := csvtable.ReadTable(
		filepath.Join(projectFolderPath, "data", "relevance_windows.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]durablejob.JobUnit, 0, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		unitList = append(unitList, durablejob.JobUnit{
			UnitId:         windowRow["rel_window_id"],
			PromptFilePath: windowRow["prompt_path"],
		})
	}
	return unitList, nil
}

// unitListOfPhoto returns one unit per photo, with the image it passes to the
// model.
func unitListOfPhoto(projectFolderPath string) ([]durablejob.JobUnit, error) {
	photoTable, readError := csvtable.ReadTable(
		filepath.Join(projectFolderPath, "data", "photos.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]durablejob.JobUnit, 0, len(photoTable.DataRows))
	for _, photoRow := range photoTable.DataRows {
		unitList = append(unitList, durablejob.JobUnit{
			UnitId:         photoRow["photo_key"],
			PromptFilePath: photoRow["prompt_path"],
			ImageFilePath:  photoRow["photo_path"],
		})
	}
	return unitList, nil
}

// UnitListOf returns the unit list of one job.
func UnitListOf(projectFolderPath string, jobName string) ([]durablejob.JobUnit, error) {
	switch jobName {
	case "w10", "w100", "month", "meta":
		return unitListOfWindowKind(projectFolderPath, jobName)
	case "rel":
		return unitListOfRelevance(projectFolderPath)
	case "photo":
		return unitListOfPhoto(projectFolderPath)
	}
	return nil, fmt.Errorf("unknown job %q; the jobs are %v", jobName, JobNameList)
}
