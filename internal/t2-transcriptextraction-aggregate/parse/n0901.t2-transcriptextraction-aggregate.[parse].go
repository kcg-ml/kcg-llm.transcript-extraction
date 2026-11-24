package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/prompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// SummaryItemRecord is one item of one summary field.
type SummaryItemRecord struct {
	WindowId     string
	WindowKind   string
	Month        string
	FieldName    string
	ItemIndex    int
	ItemText     string
	ItemHeadText string
}

// ItemSeparatorText separates the items of one summary field.
const ItemSeparatorText = ";;"

// AnswerFieldLinePattern matches one `FIELD: value` line of a model answer.
var AnswerFieldLinePattern = regexp.MustCompile(`^\s*\**([A-Z]+)\**\s*:\s*(.*)$`)

// SummaryTableColumnNames are the columns of data/summaries.csv and
// data/summary_rollup.csv.
var SummaryTableColumnNames = []string{"window_id", "window_kind", "month",
	"first_message_key", "last_message_key", "message_count", "answer_chars",
	"topics", "papers", "talks", "code", "methods", "models", "experiments",
	"results", "reasons", "causal", "claims", "open", "suggestions", "quotes"}

// SummaryItemTableColumnNames are the columns of data/summary_items.csv.
var SummaryItemTableColumnNames = []string{"window_id", "window_kind", "month",
	"field", "item_index", "item", "item_head"}

// ParseAnswer returns the 14 field values of one model answer.
func ParseAnswer(answerText string) map[string]string {
	fieldValueOfName := make(map[string]string, len(prompt.SummaryFieldList))
	for _, fieldName := range prompt.SummaryFieldList {
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

// SplitItemList returns the items of one summary field value.
func SplitItemList(fieldValue string) []string {
	trimmedValue := strings.TrimSpace(fieldValue)
	if trimmedValue == "" {
		return nil
	}
	lowerValue := strings.ToLower(trimmedValue)
	if lowerValue == "none" || lowerValue == "n/a" || lowerValue == "-" {
		return nil
	}
	itemList := make([]string, 0, 8)
	for _, rawItem := range strings.Split(trimmedValue, ItemSeparatorText) {
		itemText := strings.Trim(rawItem, " .")
		if itemText == "" || strings.ToLower(itemText) == "none" {
			continue
		}
		itemList = append(itemList, itemText)
	}
	return itemList
}

// ItemHeadOf returns the name part of an item, before its `|` explanation.
func ItemHeadOf(itemText string) string {
	headText := strings.TrimSpace(strings.SplitN(itemText, "|", 2)[0])
	if len(headText) > 120 {
		headText = headText[:120]
	}
	return headText
}

// LoadSummaryRecordList reads the answers of the w10, w100, and month windows.
func LoadSummaryRecordList(projectFolderPath string) ([]SummaryRecord, []SummaryItemRecord, error) {
	windowTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"))
	if readError != nil {
		return nil, nil, readError
	}
	windowIdList := make([]string, 0, len(windowTable.DataRows))
	windowRowOfId := make(map[string]map[string]string, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		windowId := windowRow["window_id"]
		if windowRow["window_kind"] == window.WindowKindOfWholeChat {
			continue
		}
		windowIdList = append(windowIdList, windowId)
		windowRowOfId[windowId] = windowRow
	}
	sort.Strings(windowIdList)

	summaryRecordList := make([]SummaryRecord, 0, len(windowIdList))
	itemRecordList := make([]SummaryItemRecord, 0, len(windowIdList)*8)
	for _, windowId := range windowIdList {
		answerText, answerError := ReadAnswerText(projectFolderPath, windowId)
		if answerError != nil {
			continue
		}
		windowRow := windowRowOfId[windowId]
		fieldValueOfName := ParseAnswer(answerText)
		messageCount, _ := strconv.Atoi(windowRow["message_count"])
		summaryRecordList = append(summaryRecordList, SummaryRecord{
			WindowId:         windowId,
			WindowKind:       windowRow["window_kind"],
			Month:            windowRow["month"],
			FirstMessageKey:  windowRow["first_message_key"],
			LastMessageKey:   windowRow["last_message_key"],
			MessageCount:     messageCount,
			AnswerCharCount:  len(answerText),
			FieldValueOfName: fieldValueOfName,
		})
		for _, fieldName := range prompt.SummaryFieldList {
			for itemIndex, itemText := range SplitItemList(fieldValueOfName[fieldName]) {
				itemRecordList = append(itemRecordList, SummaryItemRecord{
					WindowId:     windowId,
					WindowKind:   windowRow["window_kind"],
					Month:        windowRow["month"],
					FieldName:    strings.ToLower(fieldName),
					ItemIndex:    itemIndex,
					ItemText:     itemText,
					ItemHeadText: ItemHeadOf(itemText),
				})
			}
		}
	}
	return summaryRecordList, itemRecordList, nil
}

// ReadAnswerText returns the answer of one window, from the durable job folder
// first and from the flat answer folder second.
func ReadAnswerText(projectFolderPath string, windowId string) (string, error) {
	candidatePathList := []string{
		filepath.Join(projectFolderPath, "model-answer", "job",
			jobNameOfWindowId(windowId), "done", windowId+".txt"),
		filepath.Join(projectFolderPath, "model-answer", windowId+".txt"),
	}
	for _, candidatePath := range candidatePathList {
		answerBytes, readError := os.ReadFile(candidatePath)
		if readError == nil {
			return string(answerBytes), nil
		}
	}
	return "", fmt.Errorf("no answer for window %s", windowId)
}

// jobNameOfWindowId returns the durable job that answers a window kind.
//
// The 3 rollup levels all answer with the whole-chat job, because each one
// rolls the summaries below it into the one summary of the chat.
func jobNameOfWindowId(windowId string) string {
	switch {
	case strings.HasPrefix(windowId, window.WindowKindOf10Messages+"-"):
		return window.WindowKindOf10Messages
	case strings.HasPrefix(windowId, window.WindowKindOf100Messages+"-"):
		return window.WindowKindOf100Messages
	case strings.HasPrefix(windowId, window.WindowKindOfOneMonth+"-"):
		return window.WindowKindOfOneMonth
	case strings.HasPrefix(windowId, window.WindowKindOfWholeChat+"-"):
		return window.WindowKindOfWholeChat
	}
	return window.WindowKindOf10Messages
}

// SummaryRowOf returns the table row of one summary record.
func SummaryRowOf(record SummaryRecord) map[string]string {
	row := map[string]string{
		"window_id":         record.WindowId,
		"window_kind":       record.WindowKind,
		"month":             record.Month,
		"first_message_key": record.FirstMessageKey,
		"last_message_key":  record.LastMessageKey,
		"message_count":     strconv.Itoa(record.MessageCount),
		"answer_chars":      strconv.Itoa(record.AnswerCharCount),
	}
	for _, fieldName := range prompt.SummaryFieldList {
		row[strings.ToLower(fieldName)] = record.FieldValueOfName[fieldName]
	}
	return row
}

// writeTable writes one table into the data folder and prints its row count.
func WriteTableFile(projectFolderPath string, tableName string, tableObject *table.Table) error {
	tablePath := filepath.Join(projectFolderPath, "data", tableName)
	if writeError := tableObject.WriteTable(tablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d rows  %s\n", tableObject.RowCount(),
		table.RelativePath(projectFolderPath, tablePath))
	return nil
}
