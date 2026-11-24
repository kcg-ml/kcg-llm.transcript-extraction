package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"
)
// PromptCharBudget caps one prompt, so a call fits a 32k-token context with
// room for the answer. One token is about 3 characters of mixed English and
// Russian.
const PromptCharBudget = 60000

// SummaryFieldList names the 14 fields of one summary answer, in the order the
// model writes them.
var SummaryFieldList = []string{"TOPICS", "PAPERS", "TALKS", "CODE", "METHODS",
	"MODELS", "EXPERIMENTS", "RESULTS", "REASONS", "CAUSAL", "CLAIMS", "OPEN",
	"SUGGESTIONS", "QUOTES"}

// SummaryAnswerFormat is the answer shape the prompt asks for.
const SummaryAnswerFormat = `Answer in exactly these 14 lines, one value per line, no other text:

TOPICS: 3 to 8 short topic names, comma separated
PAPERS: one entry per paper, as ` + "`<arxiv id or title> | <what the message says about it>`" + `, separated by ` + "` ;; `" + `
TALKS: one entry per talk, conference, or video, as ` + "`<name or URL> | <what it is>`" + `, separated by ` + "` ;; `" + `
CODE: one entry per repository, checkpoint, or dataset, as ` + "`<name or URL> | <what it is>`" + `, separated by ` + "` ;; `" + `
METHODS: one entry per named method or technique, as ` + "`<name> | <what it does here>`" + `, separated by ` + "` ;; `" + `
MODELS: one entry per model or checkpoint named, comma separated
EXPERIMENTS: one entry per measurement or run, as ` + "`<what was measured> | <result>`" + `, separated by ` + "` ;; `" + `
RESULTS: one entry per stated number, size, or speedup, as ` + "`<what it measures> = <value>`" + `, separated by ` + "` ;; `" + `
REASONS: one entry per stated cause or justification, separated by ` + "` ;; `" + `
CAUSAL: one entry per cause-and-effect pair the messages state, as ` + "`<cause> -> <effect> | <the message id>`" + `, separated by ` + "` ;; `" + `
CLAIMS: one entry per belief, hypothesis, or thesis stated, separated by ` + "` ;; `" + `
OPEN: one entry per question the speakers left open, separated by ` + "` ;; `" + `
SUGGESTIONS: one entry per piece of advice one speaker gives the other, as ` + "`<the advice> | <the condition it depends on, or none>`" + `, separated by ` + "` ;; `" + `
QUOTES: one entry per sentence worth quoting verbatim, with its message id, as ` + "`<message id> | <the sentence>`" + `, separated by ` + "` ;; `" + `

Write ` + "`none`" + ` for a line with no entry. Do not invent a paper, a number, or a
name that is not in the messages. Keep the speaker's terms; do not translate a
Russian term into English. A message that is only ` + "`--`" + ` or a link carries no
content; skip it.
`

// referenceListOfMessage returns the references of one message, grouped by
// message key.
func referenceListOfMessage(referenceTable *table.Table) map[string][]map[string]string {
	referenceListByMessage := make(map[string][]map[string]string, len(referenceTable.DataRows))
	for _, referenceRow := range referenceTable.DataRows {
		messageKey := referenceRow["message_key"]
		referenceListByMessage[messageKey] = append(referenceListByMessage[messageKey], referenceRow)
	}
	return referenceListByMessage
}

// namedReferenceKindSet names the reference kinds the prompt repeats under a
// message, because they are the ones a reader wants to see beside the text.
var namedReferenceKindSet = map[string]bool{
	"paper": true, "code": true, "link": true, "model": true, "video": true,
	"talk": true, "dataset": true,
}

// renderMessageBlock returns the prompt block of one message.
func renderMessageBlock(messageRow map[string]string, referenceRowList []map[string]string,
	englishTextByMessage map[string]string) string {
	lineList := []string{fmt.Sprintf("### %s | %s | %s", messageRow["message_key"],
		messageRow["date"], messageRow["from"])}
	if messageRow["reply_to_message_id"] != "" {
		lineList = append(lineList, fmt.Sprintf("(reply to %s)", messageRow["reply_to_message_id"]))
	}
	if messageRow["forwarded_from"] != "" {
		lineList = append(lineList, fmt.Sprintf("(forwarded from %s)", messageRow["forwarded_from"]))
	}
	// A Cyrillic message is read through its English text, so the model reads
	// one language. A message with no translation keeps its own text.
	messageText := messageRow["text"]
	if englishText, hasEnglish := englishTextByMessage[messageRow["message_key"]]; hasEnglish {
		messageText = englishText
	}
	lineList = append(lineList, messageText)
	foundList := make([]string, 0, 8)
	for _, referenceRow := range referenceRowList {
		if namedReferenceKindSet[referenceRow["ref_kind"]] {
			foundList = append(foundList, fmt.Sprintf("%s=%s", referenceRow["ref_kind"],
				referenceRow["ref_value"]))
		}
	}
	if len(foundList) > 0 {
		if len(foundList) > 20 {
			foundList = foundList[:20]
		}
		lineList = append(lineList, fmt.Sprintf("(links found: %s)", strings.Join(foundList, "; ")))
	}
	return strings.Join(lineList, "\n")
}

// RenderPrompt returns the prompt text of one window.
func RenderPrompt(window window.Window, referenceListByMessage map[string][]map[string]string,
	englishTextByMessage map[string]string) string {
	headerLineList := []string{
		"You read a Telegram chat between 2 speakers: `Synth` and `Vlad [russia][ml]`.",
		"The chat is about LLM training, sparsity, pruning, and model architecture.",
		"The messages below are one window of the chat, in time order.",
		"",
		fmt.Sprintf("Window: %s", window.WindowId),
		fmt.Sprintf("Messages: %d, from %s to %s", len(window.MessageRowList),
			window.MessageRowList[0]["message_key"],
			window.MessageRowList[len(window.MessageRowList)-1]["message_key"]),
		"",
		"Summarise this window for an index. Name every paper, talk, video,",
		"repository, dataset, method, model, experiment, result, reason, claim,",
		"and suggestion the messages carry. A suggestion is advice one speaker",
		"gives the other: `you should do X`, or `if you do X, then Y`. A causal",
		"explanation is a cause and its effect: `X causes Y`, `Y happens because",
		"X`, `if X then Y`, `X leads to Y`. A message that is only `--` or a link",
		"carries no content; skip it.",
		"",
		SummaryAnswerFormat,
		"",
		"----- MESSAGES -----",
	}
	// The budget counts runes, not bytes, so a Cyrillic month fits as many
	// messages as the same month written in Latin text.
	usedCharCount := 0
	for _, headerLine := range headerLineList {
		usedCharCount += utf8.RuneCountInString(headerLine) + 1
	}
	bodyLineList := make([]string, 0, len(window.MessageRowList))
	for _, messageRow := range window.MessageRowList {
		block := renderMessageBlock(messageRow, referenceListByMessage[messageRow["message_key"]],
			englishTextByMessage)
		if usedCharCount+utf8.RuneCountInString(block) > PromptCharBudget {
			bodyLineList = append(bodyLineList, fmt.Sprintf(
				"(window truncated at %d of %d messages to fit the context)",
				len(bodyLineList), len(window.MessageRowList)))
			break
		}
		bodyLineList = append(bodyLineList, block)
		usedCharCount += utf8.RuneCountInString(block) + 2
	}
	return strings.Join(append(headerLineList, bodyLineList...), "\n")
}

// Run writes data/windows.csv and one prompt file per window.
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
	referenceTable, referenceError := table.ReadTable(filepath.Join(dataFolderPath, "references.csv"))
	if referenceError != nil {
		return referenceError
	}
	referenceListByMessage := referenceListOfMessage(referenceTable)
	englishTextByMessage, translationError := translatetable.EnglishTextByMessage(projectFolderPath)
	if translationError != nil {
		return translationError
	}

	windowList := make([]window.Window, 0, 1200)
	windowList = append(windowList, window.WindowListOfSize(messageTable.DataRows, 10,
		window.WindowKindOf10Messages)...)
	windowList = append(windowList, window.WindowListOfSize(messageTable.DataRows, 100,
		window.WindowKindOf100Messages)...)
	windowList = append(windowList, window.MonthWindowList(messageTable.DataRows)...)

	windowTable := table.NewTable(window.WindowTableColumnNames)
	for _, window := range windowList {
		promptText := RenderPrompt(window, referenceListByMessage, englishTextByMessage)
		promptPath := filepath.Join(promptFolderPath, window.WindowId+".txt")
		if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
			return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
		}
		// The char count is the sum of the `char_len` column, which counts
		// runes, not bytes: a Cyrillic message holds more bytes than runes.
		charCount := 0
		referenceCount := 0
		for _, messageRow := range window.MessageRowList {
			messageCharCount, parseError := strconv.Atoi(messageRow["char_len"])
			if parseError != nil {
				return fmt.Errorf("message %s holds char_len %q: %w",
					messageRow["message_key"], messageRow["char_len"], parseError)
			}
			charCount += messageCharCount
			referenceCount += len(referenceListByMessage[messageRow["message_key"]])
		}
		windowTable.AppendRow(map[string]string{
			"window_id":         window.WindowId,
			"window_kind":       window.WindowKind,
			"window_index":      fmt.Sprintf("%d", window.WindowIndex),
			"month":             window.Month,
			"first_message_key": window.MessageRowList[0]["message_key"],
			"last_message_key":  window.MessageRowList[len(window.MessageRowList)-1]["message_key"],
			"message_count":     fmt.Sprintf("%d", len(window.MessageRowList)),
			"char_count":        fmt.Sprintf("%d", charCount),
			"ref_count":         fmt.Sprintf("%d", referenceCount),
			"prompt_path":       table.RelativePath(projectFolderPath, promptPath),
			"prompt_chars":      fmt.Sprintf("%d", utf8.RuneCountInString(promptText)),
			"prompt_parts":      "1",
		})
	}

	windowTablePath := filepath.Join(dataFolderPath, "windows.csv")
	if writeError := windowTable.WriteTable(windowTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d windows  data/windows.csv\n", windowTable.RowCount())
	fmt.Printf("wrote %6d prompts  prompts/\n", windowTable.RowCount())
	return nil
}
