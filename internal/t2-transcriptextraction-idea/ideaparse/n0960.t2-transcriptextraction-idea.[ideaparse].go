package ideaparse

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-idea/ideaprompt"
)

// IdeaItemSeparatorText separates the entries of one answer line.
const IdeaItemSeparatorText = ";;"

// JobNameIdeaExtraction is the job that answers the phase 1 prompts.
//
// The name is written here rather than read from `chainunit`, because a t2
// project never imports a t4 project. The chainunit group holds the same name
// in `JobNameIdeaExtraction`, and a rename must change both.
const JobNameIdeaExtraction = "idea-extraction"

// IdeaAnswerLinePattern matches one `FIELD: value` line of a model answer.
var IdeaAnswerLinePattern = regexp.MustCompile(`^\s*\**([A-Z]+)\**\s*:\s*(.*)$`)

// IdeaMessageKeyPattern matches a message key inside one entry.
//
// The key is `<telegram message id:06d>-<yymmdd>`, so it is 6 digits, a dash,
// and 6 digits. An entry that ends with the key it came from is the shape the
// prompt asks for.
var IdeaMessageKeyPattern = regexp.MustCompile(`([0-9]{6}-[0-9]{6})`)

// IdeaAnswerFieldMap returns the 5 field values of one model answer.
func IdeaAnswerFieldMap(answerText string) map[string]string {
	fieldValueMap := make(map[string]string, len(ideaprompt.IdeaFieldList))
	for _, fieldName := range ideaprompt.IdeaFieldList {
		fieldValueMap[fieldName] = ""
	}
	for _, lineText := range strings.Split(answerText, "\n") {
		match := IdeaAnswerLinePattern.FindStringSubmatch(lineText)
		if match == nil {
			continue
		}
		if _, isKnownField := fieldValueMap[match[1]]; isKnownField {
			fieldValueMap[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return fieldValueMap
}

// IdeaItemTextListOf returns the entries of one answer line.
//
// The word `none` is the answer for a line with no entry, so it yields no
// entry rather than an entry named `none`.
func IdeaItemTextListOf(fieldValueText string) []string {
	trimmedText := strings.TrimSpace(fieldValueText)
	if trimmedText == "" || strings.EqualFold(trimmedText, "none") {
		return nil
	}
	rawList := strings.Split(trimmedText, IdeaItemSeparatorText)
	itemTextList := make([]string, 0, len(rawList))
	for _, rawText := range rawList {
		itemText := strings.TrimSpace(rawText)
		if itemText == "" || strings.EqualFold(itemText, "none") {
			continue
		}
		itemTextList = append(itemTextList, itemText)
	}
	return itemTextList
}

// MessageKeyOfItem returns the message key an entry names, or an empty text.
//
// The last key of an entry is the one the prompt asks for, because the entry
// is `<name> | <what it is> | <message id>`. A paper title can hold no key, so
// the last match is the trailing id.
func MessageKeyOfItem(itemText string) string {
	keyList := IdeaMessageKeyPattern.FindAllString(itemText, -1)
	if len(keyList) == 0 {
		return ""
	}
	return keyList[len(keyList)-1]
}

// ItemHeadTextOf returns the name part of one entry, which is the text before
// the first `|`.
func ItemHeadTextOf(itemText string) string {
	pipeIndex := strings.Index(itemText, "|")
	if pipeIndex < 0 {
		return pattern.CutRuneText(itemText, 120)
	}
	return pattern.CutRuneText(strings.TrimSpace(itemText[:pipeIndex]), 120)
}

// Run reads every answer of the idea job and writes data/idea_items.csv.
//
// A window with no answer is skipped, so the table holds what was extracted
// and not what was planned. A reader who wants the missing windows compares
// data/idea_windows.csv with the answer folder.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	windowTable, windowError := table.ReadTable(
		filepath.Join(dataFolderPath, "idea_windows.csv"))
	if windowError != nil {
		return windowError
	}
	answerFolderPath := filepath.Join(projectFolderPath, "model-answer", "job",
		JobNameIdeaExtraction, "done")

	itemTable := table.NewTable(ideaprompt.IdeaItemTableColumnNames)
	answeredCount := 0
	for _, windowRow := range windowTable.DataRows {
		answerFilePath := filepath.Join(answerFolderPath, windowRow["idea_window_id"]+".txt")
		answerBytes, readError := os.ReadFile(answerFilePath)
		if readError != nil {
			continue
		}
		answeredCount++
		fieldValueMap := IdeaAnswerFieldMap(string(answerBytes))
		for _, fieldName := range ideaprompt.IdeaFieldList {
			itemTextList := IdeaItemTextListOf(fieldValueMap[fieldName])
			for itemIndex, itemText := range itemTextList {
				itemTable.AppendRow(map[string]string{
					"window_id":         windowRow["idea_window_id"],
					"window_kind":       windowRow["window_kind"],
					"month":             windowRow["month"],
					"first_message_key": windowRow["first_message_key"],
					"last_message_key":  windowRow["last_message_key"],
					"field":             fieldName,
					"item_index":        fmt.Sprintf("%d", itemIndex),
					"item":              itemText,
					"item_head":         ItemHeadTextOf(itemText),
					"message_key":       MessageKeyOfItem(itemText),
				})
			}
		}
	}
	if answeredCount == 0 {
		return fmt.Errorf("no answer in %s; run the job first", answerFolderPath)
	}
	return itemTable.WriteTable(filepath.Join(dataFolderPath, "idea_items.csv"))
}