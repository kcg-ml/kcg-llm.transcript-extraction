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
//
// The id must not follow a digit, or a 7-digit id the model invented such as
// `1000017-260714` matches on its last 6 digits and yields the key
// `000017-260714`, which names no message.
var IdeaMessageKeyPattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]{6}-[0-9]{6})(?:[^0-9]|$)`)

// IdeaBareMessageIdPattern matches a Telegram message id with no date part.
//
// The model sometimes writes `593111` where the prompt asks for
// `593111-251120`. The date part is the message's own date, so the id is
// resolved against the window rather than dropped: an entry with no id
// cannot be checked, and the id is the point of the table.
var IdeaBareMessageIdPattern = regexp.MustCompile(`(?:^|[^0-9-])([0-9]{6})(?:[^0-9-]|$)`)

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
//
// A bare Telegram id is resolved against the window's messages, because the
// model sometimes writes `593111` where the prompt asks for `593111-251120`.
func MessageKeyOfItem(itemText string, messageKeyByMessageId map[string]string) string {
	keyMatchList := IdeaMessageKeyPattern.FindAllStringSubmatch(itemText, -1)
	if len(keyMatchList) > 0 {
		return keyMatchList[len(keyMatchList)-1][1]
	}
	bareMatchList := IdeaBareMessageIdPattern.FindAllStringSubmatch(itemText, -1)
	for matchIndex := len(bareMatchList) - 1; matchIndex >= 0; matchIndex-- {
		if messageKey, keyFound := messageKeyByMessageId[bareMatchList[matchIndex][1]]; keyFound {
			return messageKey
		}
	}
	return ""
}

// MessageKeyOfItemInWindow returns the message key an entry names, and falls
// back to the window's first message key when the entry names none.
//
// The model sometimes lists an item with no trailing id, or writes `unknown`
// where the prompt asks for one, because the item came from the window's
// summary and no single message states it. The item still came from that
// window, so the window's first message key is the honest attribution and
// every row keeps a key that resolves in `data/messages.csv`.
func MessageKeyOfItemInWindow(itemText string, windowRow map[string]string,
	messageKeyByMessageId map[string]string) string {
	messageKey := MessageKeyOfItem(itemText, messageKeyByMessageId)
	if messageKey != "" {
		return messageKey
	}
	return windowRow["first_message_key"]
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

// messageKeyByMessageIdOf returns the message key of every message, by the
// bare Telegram message id.
func messageKeyByMessageIdOf(projectFolderPath string) (map[string]string, error) {
	messageTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "messages.csv"))
	if readError != nil {
		return nil, readError
	}
	messageKeyByMessageId := make(map[string]string, len(messageTable.DataRows))
	for _, messageRow := range messageTable.DataRows {
		messageKeyByMessageId[messageRow["message_id"]] = messageRow["message_key"]
	}
	return messageKeyByMessageId, nil
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

	// A bare message id is resolved against the whole chat, so the id the
	// model wrote as `593111` becomes the key `593111-251120`.
	messageKeyByMessageId, messageError := messageKeyByMessageIdOf(projectFolderPath)
	if messageError != nil {
		return messageError
	}

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
					"message_key":       MessageKeyOfItemInWindow(itemText, windowRow, messageKeyByMessageId),
				})
			}
		}
	}
	if answeredCount == 0 {
		return fmt.Errorf("no answer in %s; run the job first", answerFolderPath)
	}
	return itemTable.WriteTable(filepath.Join(dataFolderPath, "idea_items.csv"))
}