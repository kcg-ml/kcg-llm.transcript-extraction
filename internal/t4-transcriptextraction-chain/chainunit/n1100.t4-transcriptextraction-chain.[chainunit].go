package chainunit

import (
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"fmt"
	"path/filepath"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
)
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
func unitListOfWindowKind(projectFolderPath string, windowKind string) ([]store.JobUnit, error) {	windowTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]store.JobUnit, 0, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		if windowRow["window_kind"] != windowKind {
			continue
		}
		unitList = append(unitList, store.JobUnit{
			UnitId:         windowRow["window_id"],
			PromptFilePath: windowRow["prompt_path"],
		})
	}
	return unitList, nil
}

// unitListOfRelevance returns one unit per relevance window.
func unitListOfRelevance(projectFolderPath string) ([]store.JobUnit, error) {
	windowTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "relevance_windows.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]store.JobUnit, 0, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		unitList = append(unitList, store.JobUnit{
			UnitId:         windowRow["rel_window_id"],
			PromptFilePath: windowRow["prompt_path"],
		})
	}
	return unitList, nil
}

// unitListOfPhoto returns one unit per photo, with the image it passes to the
// model.
func unitListOfPhoto(projectFolderPath string) ([]store.JobUnit, error) {
	photoTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "photos.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]store.JobUnit, 0, len(photoTable.DataRows))
	for _, photoRow := range photoTable.DataRows {
		unitList = append(unitList, store.JobUnit{
			UnitId:         photoRow["photo_key"],
			PromptFilePath: photoRow["prompt_path"],
			ImageFilePath:  photoRow["photo_path"],
		})
	}
	return unitList, nil
}

// unitListOfIdea returns one unit per 10-message window of the idea
// extraction.
func unitListOfIdea(projectFolderPath string) ([]store.JobUnit, error) {
	ideaWindowTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "idea_windows.csv"))
	if readError != nil {
		return nil, readError
	}
	unitList := make([]store.JobUnit, 0, len(ideaWindowTable.DataRows))
	for _, ideaWindowRow := range ideaWindowTable.DataRows {
		unitList = append(unitList, store.JobUnit{
			UnitId:         ideaWindowRow["idea_window_id"],
			PromptFilePath: ideaWindowRow["prompt_path"],
		})
	}
	return unitList, nil
}

// unitListOfTranslation returns one unit per message that holds Cyrillic
// text, so the English text of that message can be read by every later
// prompt.
func unitListOfTranslation(projectFolderPath string) ([]store.JobUnit, error) {
	cyrillicRowList, cyrillicError := translatetable.CyrillicMessageRowList(projectFolderPath)
	if cyrillicError != nil {
		return nil, cyrillicError
	}
	unitList := make([]store.JobUnit, 0, len(cyrillicRowList))
	for _, messageRow := range cyrillicRowList {
		unitList = append(unitList, store.JobUnit{
			UnitId:         messageRow["message_key"],
			PromptFilePath: translatetable.PromptFilePathOf(messageRow["message_key"]),
		})
	}
	return unitList, nil
}

// UnitListOf returns the unit list of one job.
func UnitListOf(projectFolderPath string, jobName string) ([]store.JobUnit, error) {
	switch jobName {
	case JobNameMessageTranslation:
		return unitListOfTranslation(projectFolderPath)
	case JobNameSummaryWindowOf10Messages, JobNameSummaryWindowOf100Messages,
		JobNameSummaryWindowOfOneMonth, JobNameSummaryWindowOfWholeChat:
		return unitListOfWindowKind(projectFolderPath, jobName)
	case JobNameRelevanceWindowVote:
		return unitListOfRelevance(projectFolderPath)
	case JobNamePhotoContent:
		return unitListOfPhoto(projectFolderPath)
	case JobNameIdeaExtraction:
		return unitListOfIdea(projectFolderPath)
	}
	return nil, fmt.Errorf("unknown job %q; the jobs are %v", jobName, JobNameList)
}
