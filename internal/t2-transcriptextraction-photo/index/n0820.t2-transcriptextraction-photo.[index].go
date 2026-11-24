package index

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/index"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// PhotoAnswerFieldList names the 6 fields of one photo answer.
var PhotoAnswerFieldList = []string{"SHOWS", "TEXT", "TOPIC", "RELEVANCE",
	"NUMBERS", "TERMS"}

// PhotoIndexTableColumnNames are the columns of data/photo_index.csv.
var PhotoIndexTableColumnNames = []string{"photo_key", "message_key", "date",
	"month", "from", "photo_path", "shows", "text_in_photo", "topic", "relevance",
	"relevance_reason", "numbers", "terms", "answer_chars"}

// AnswerFieldLinePattern matches one `FIELD: value` line of a photo answer.
var AnswerFieldLinePattern = regexp.MustCompile(`^\s*\**([A-Z]+)\**\s*:\s*(.*)$`)

// ParsePhotoAnswer returns the 6 field values of one photo answer.
func ParsePhotoAnswer(answerText string) map[string]string {
	fieldValueOfName := make(map[string]string, len(PhotoAnswerFieldList))
	for _, fieldName := range PhotoAnswerFieldList {
		fieldValueOfName[fieldName] = ""
	}
	for _, line := range strings.Split(answerText, "\n") {
		match := AnswerFieldLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if _, isKnownField := fieldValueOfName[match[1]]; isKnownField {
			fieldValueOfName[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return fieldValueOfName
}

// photoAnswerFilePathList returns the paths a photo answer may sit at, the
// durable job folder first and the flat answer folder second.
func photoAnswerFilePathList(projectFolderPath string, photoRow map[string]string) []string {
	return []string{
		filepath.Join(projectFolderPath, "model-answer", "job", "photo-content",
			"done", photoRow["photo_key"]+".txt"),
		filepath.Join(projectFolderPath, photoRow["answer_path"]),
	}
}

// appendPhotoRowsToIndex replaces the photo rows of data/index.csv with the
// given rows.
func appendPhotoRowsToIndex(projectFolderPath string, photoIndexRowList []map[string]string) error {
	indexTablePath := filepath.Join(projectFolderPath, "data", "index.csv")
	existingTable, readError := table.ReadTableIfPresent(indexTablePath,
		index.IndexTableColumnNames)
	if readError != nil {
		return readError
	}
	keptRowList := make([]map[string]string, 0, len(existingTable.DataRows))
	for _, indexRow := range existingTable.DataRows {
		if indexRow["window_kind"] == "photo-content" {
			continue
		}
		keptRowList = append(keptRowList, indexRow)
	}
	indexTable := table.NewTable(index.IndexTableColumnNames)
	for _, indexRow := range keptRowList {
		indexTable.AppendRow(indexRow)
	}
	for _, photoIndexRow := range photoIndexRowList {
		indexTable.AppendRow(map[string]string{
			"index_id":          fmt.Sprintf("P%06d", indexTable.RowCount()+1),
			"window_id":         photoIndexRow["photo_key"],
			"window_kind":       "photo-content",
			"month":             photoIndexRow["month"],
			"first_message_key": photoIndexRow["message_key"],
			"last_message_key":  photoIndexRow["message_key"],
			"field":             "photo",
			"item": fmt.Sprintf("%s | %s | %s", photoIndexRow["shows"],
				photoIndexRow["topic"], photoIndexRow["terms"]),
			"item_head": pattern.CutRuneText(photoIndexRow["topic"], 120),
		})
	}
	if writeError := indexTable.WriteTable(indexTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d index rows  data/index.csv (%d of them photo rows)\n",
		indexTable.RowCount(), len(photoIndexRowList))
	return nil
}

// Run writes data/photo_index.csv and appends its rows to data/index.csv.
func Run(projectFolderPath string) error {
	photoTablePath := filepath.Join(projectFolderPath, "data", "photos.csv")
	if _, statError := os.Stat(photoTablePath); statError != nil {
		return fmt.Errorf("no data/photos.csv; run the photos stage first")
	}
	photoTable, readError := table.ReadTable(photoTablePath)
	if readError != nil {
		return readError
	}

	photoIndexRowList := make([]map[string]string, 0, len(photoTable.DataRows))
	pendingCount := 0
	for _, photoRow := range photoTable.DataRows {
		answerText := ""
		for _, answerFilePath := range photoAnswerFilePathList(projectFolderPath, photoRow) {
			answerBytes, answerError := os.ReadFile(answerFilePath)
			if answerError == nil {
				answerText = string(answerBytes)
				break
			}
		}
		if answerText == "" {
			pendingCount++
			continue
		}
		fieldValueOfName := ParsePhotoAnswer(answerText)
		relevanceText := fieldValueOfName["RELEVANCE"]
		relevanceReasonText := ""
		if separatorIndex := strings.Index(relevanceText, " - "); separatorIndex >= 0 {
			relevanceReasonText = relevanceText[separatorIndex+3:]
			relevanceText = relevanceText[:separatorIndex]
		}
		photoIndexRowList = append(photoIndexRowList, map[string]string{
			"photo_key":        photoRow["photo_key"],
			"message_key":      photoRow["message_key"],
			"date":             photoRow["date"],
			"month":            photoRow["month"],
			"from":             photoRow["from"],
			"photo_path":       photoRow["photo_path"],
			"shows":            fieldValueOfName["SHOWS"],
			"text_in_photo":    fieldValueOfName["TEXT"],
			"topic":            fieldValueOfName["TOPIC"],
			"relevance":        strings.ToLower(strings.TrimSpace(relevanceText)),
			"relevance_reason": strings.TrimSpace(relevanceReasonText),
			"numbers":          fieldValueOfName["NUMBERS"],
			"terms":            fieldValueOfName["TERMS"],
			"answer_chars":     fmt.Sprintf("%d", len(answerText)),
		})
	}

	photoIndexTable := table.NewTable(PhotoIndexTableColumnNames)
	for _, photoIndexRow := range photoIndexRowList {
		photoIndexTable.AppendRow(photoIndexRow)
	}
	photoIndexTablePath := filepath.Join(projectFolderPath, "data", "photo_index.csv")
	if writeError := photoIndexTable.WriteTable(photoIndexTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d photo index rows  %s\n", photoIndexTable.RowCount(),
		table.RelativePath(projectFolderPath, photoIndexTablePath))

	if appendError := appendPhotoRowsToIndex(projectFolderPath, photoIndexRowList); appendError != nil {
		return appendError
	}
	if pendingCount > 0 {
		fmt.Fprintf(os.Stderr, "%d photos have no answer yet in out/jobs/photo/done/\n",
			pendingCount)
	}
	return nil
}
