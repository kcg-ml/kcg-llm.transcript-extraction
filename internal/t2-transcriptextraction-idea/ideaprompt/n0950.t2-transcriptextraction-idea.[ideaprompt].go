package ideaprompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
)

// IdeaFieldList names the 5 fields phase 1 extracts, in the order the model
// writes them.
//
// Phase 1 takes the fields that name a thing a reader can look up: a paper, a
// talk, a repository, a method, a model. The later phases take the fields that
// need the whole argument: an experiment, a result, a reason, a cause, a
// claim, an open question, a suggestion, a quote.
var IdeaFieldList = []string{"PAPERS", "TALKS", "CODE", "METHODS", "MODELS"}

// IdeaItemTableColumnNames are the columns of data/idea_items.csv.
//
// Every row keeps the message id beside the item, so a reader can check any
// line of the index against the chat.
var IdeaItemTableColumnNames = []string{"window_id", "window_kind", "month",
	"first_message_key", "last_message_key", "field", "item_index", "item",
	"item_head", "message_key"}

// IdeaAnswerFormat is the answer shape the prompt asks for.
//
// Every line asks for the message id of the item, because an item with no
// message id cannot be checked against the chat, and the whole point of the
// index is that a reader can check it.
const IdeaAnswerFormat = `Answer in exactly these 5 lines, one value per line, no other text:

PAPERS: one entry per paper, as ` + "`<arxiv id or DOI or title> | <what the message says about it> | <message id>`" + `, separated by ` + "` ;; `" + `
TALKS: one entry per talk, conference, meetup, or video, as ` + "`<name or URL> | <what it is> | <message id>`" + `, separated by ` + "` ;; `" + `
CODE: one entry per repository, checkpoint, or dataset, as ` + "`<name or URL> | <what it is> | <message id>`" + `, separated by ` + "` ;; `" + `
METHODS: one entry per named method or technique, as ` + "`<name as the speaker names it> | <what it does here> | <message id>`" + `, separated by ` + "` ;; `" + `
MODELS: one entry per model or checkpoint name, as ` + "`<name> | <what it is here> | <message id>`" + `, separated by ` + "` ;; `" + `

Write ` + "`none`" + ` for a line with no entry. Do not invent a paper, a talk, a
repository, a method, or a model that is not in the messages. Keep the
speaker's terms; do not translate a Russian term into English. A message that
is only ` + "`--`" + ` or a link carries no content; skip it.`

// IdeaPromptCharBudget caps one prompt, so a call fits a 32k-token context
// with room for the answer.
const IdeaPromptCharBudget = 60000

// IdeaPromptHeaderLineList is the fixed part of every phase 1 prompt.
var IdeaPromptHeaderLineList = []string{
	"You read a Telegram chat between 2 speakers: `Synth` and `Vlad [russia][ml]`.",
	"The chat is about LLM training, sparsity, pruning, and model architecture.",
	"The messages below are one window of the chat, in time order.",
	"",
	"Extract the named things this window carries, for a searchable index.",
	"Name every paper, talk, video, repository, dataset, method, and model.",
	"An entry that names no message id cannot be checked, so every entry ends",
	"with the message id it came from.",
	"",
	IdeaAnswerFormat,
	"",
	"----- MESSAGES -----",
}

// IdeaPromptTextOf returns the prompt text of one window.
//
// A Cyrillic message is read through its English text, so the model reads one
// language. A message with no translation keeps its own text.
func IdeaPromptTextOf(windowRow map[string]string, messageRowList []map[string]string,
	englishTextByMessage map[string]string) string {
	headerLineList := make([]string, 0, len(IdeaPromptHeaderLineList)+6)
	headerLineList = append(headerLineList, IdeaPromptHeaderLineList[0:4]...)
	headerLineList = append(headerLineList,
		fmt.Sprintf("Window: %s", windowRow["window_id"]),
		fmt.Sprintf("Messages: %d, from %s to %s", len(messageRowList),
			windowRow["first_message_key"], windowRow["last_message_key"]),
		"")
	headerLineList = append(headerLineList, IdeaPromptHeaderLineList[4:]...)

	usedCharCount := 0
	for _, headerLine := range headerLineList {
		usedCharCount += len([]rune(headerLine)) + 1
	}
	bodyLineList := make([]string, 0, len(messageRowList))
	for _, messageRow := range messageRowList {
		blockText := renderMessageBlock(messageRow, englishTextByMessage)
		if usedCharCount+len([]rune(blockText)) > IdeaPromptCharBudget {
			bodyLineList = append(bodyLineList, fmt.Sprintf(
				"(window truncated at %d of %d messages to fit the context)",
				len(bodyLineList), len(messageRowList)))
			break
		}
		bodyLineList = append(bodyLineList, blockText)
		usedCharCount += len([]rune(blockText)) + 2
	}
	return strings.Join(append(headerLineList, bodyLineList...), "\n")
}

// renderMessageBlock returns the prompt block of one message.
//
// The message key is on the `###` line, so the model can copy it into every
// entry it takes from this message.
func renderMessageBlock(messageRow map[string]string,
	englishTextByMessage map[string]string) string {
	lineList := []string{fmt.Sprintf("### %s | %s | %s", messageRow["message_key"],
		messageRow["date"], messageRow["from"])}
	if messageRow["reply_to_message_id"] != "" {
		lineList = append(lineList, fmt.Sprintf("(reply to %s)",
			messageRow["reply_to_message_id"]))
	}
	messageText := messageRow["text"]
	if englishText, hasEnglish := englishTextByMessage[messageRow["message_key"]]; hasEnglish {
		messageText = englishText
	}
	lineList = append(lineList, messageText)
	return strings.Join(lineList, "\n")
}

// IdeaPromptFilePathOf returns the prompt file of one idea window.
//
// The name carries the `idea-extraction-` prefix, so this stage never writes
// over the summary prompt of the same window. Both stages read the same 10
// messages, and a shared file name would let one stage destroy the other's
// prompt.
func IdeaPromptFilePathOf(ideaWindowId string) string {
	return filepath.Join("prompts", "idea-extraction-"+ideaWindowId+".txt")
}

// Run writes data/idea_windows.csv and one prompt file per 10-message window.
//
// Phase 1 reads the 10-message windows, because a window of 10 messages names
// a handful of things and the model can copy each message id correctly. A
// 100-message window names too many things for one answer to stay complete.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if createError := os.MkdirAll(promptFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", promptFolderPath, createError)
	}
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	windowTable, windowError := table.ReadTable(filepath.Join(dataFolderPath, "windows.csv"))
	if windowError != nil {
		return windowError
	}
	messageRowByKey := make(map[string]map[string]string, len(messageTable.DataRows))
	for _, messageRow := range messageTable.DataRows {
		messageRowByKey[messageRow["message_key"]] = messageRow
	}
	englishTextByMessage, translationError := translatetable.EnglishTextByMessage(projectFolderPath)
	if translationError != nil {
		return translationError
	}

	ideaWindowTable := table.NewTable(IdeaWindowTableColumnNames)
	for _, windowRow := range windowTable.DataRows {
		if windowRow["window_kind"] != WindowKindOf10Messages {
			continue
		}
		messageRowList := messageRowListOfWindow(windowRow, messageTable)
		if len(messageRowList) == 0 {
			continue
		}
		promptText := IdeaPromptTextOf(windowRow, messageRowList, englishTextByMessage)
		promptRelativePath := IdeaPromptFilePathOf(windowRow["window_id"])
		promptPath := filepath.Join(projectFolderPath, promptRelativePath)
		if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
			return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
		}
		ideaWindowTable.AppendRow(map[string]string{
			"idea_window_id":    windowRow["window_id"],
			"window_kind":       windowRow["window_kind"],
			"month":             windowRow["month"],
			"first_message_key": windowRow["first_message_key"],
			"last_message_key":  windowRow["last_message_key"],
			"message_count":     windowRow["message_count"],
			"prompt_path":       promptRelativePath,
			"prompt_chars":      fmt.Sprintf("%d", len([]rune(promptText))),
		})
	}
	return ideaWindowTable.WriteTable(filepath.Join(dataFolderPath, "idea_windows.csv"))
}

// messageRowListOfWindow returns the messages of one window, in time order.
//
// The window table names its first and last message key, and the message
// table holds every message in chat order, so the window is the slice of the
// message table between those 2 keys.
func messageRowListOfWindow(windowRow map[string]string,
	messageTable *table.Table) []map[string]string {
	firstMessageKey := windowRow["first_message_key"]
	lastMessageKey := windowRow["last_message_key"]
	messageRowList := make([]map[string]string, 0, 10)
	collecting := false
	for _, messageRow := range messageTable.DataRows {
		if messageRow["message_key"] == firstMessageKey {
			collecting = true
		}
		if !collecting {
			continue
		}
		messageRowList = append(messageRowList, messageRow)
		if messageRow["message_key"] == lastMessageKey {
			break
		}
	}
	return messageRowList
}
