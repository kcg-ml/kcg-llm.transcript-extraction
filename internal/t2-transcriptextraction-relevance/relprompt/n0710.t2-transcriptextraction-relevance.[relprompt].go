package relprompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/window"
)
// PromptCharBudget caps one relevance prompt.
const PromptCharBudget = 24000

// RelevancePromptText is the instruction block every relevance prompt starts
// with.
const RelevancePromptText = `You read a Telegram chat between 2 speakers: ` + "`Synth`" + ` and ` + "`Vlad [russia][ml]`" + `.
The chat is about LLM training, sparsity, pruning, model architecture, and the
machine-learning research around them.

Your job is 1 decision per message: is this message relevant to that subject?

Mark ` + "`Y`" + ` when the message carries any of:
  a paper, a talk, a video, a repository, a dataset, a model, a checkpoint
  a method, a technique, an architecture, an optimizer, a kernel
  an experiment, a measurement, a number, a result, a benchmark
  a reason, a causal explanation, a claim about how a model works
  a suggestion about what to try, or what to do if a condition holds

Mark ` + "`N`" + ` when the message carries none of those:
  a greeting, a thanks, a joke, a separator such as ` + "`--`" + `, a bare link with no
  comment, small talk about people, money, travel, food, or politics

Mark ` + "`U`" + ` when the message is too short or too ambiguous to decide, or when it
only makes sense with a message outside this window.

Answer with exactly 1 line per ` + "`###`" + ` message, in the order the messages
appear, and no other text. Each line is:

  M<position> of <total> <Y|N|U> <3 to 12 words of reason>

Each ` + "`###`" + ` line starts with ` + "`M<position> of <total>`" + `. Write 1 line for each
` + "`###`" + ` line, and start the line with that label, for example ` + "`M3 of 10`" + `. The
label is the only key you write: never write a message id, and never write a
line for a label that has no ` + "`###`" + ` line below.

The count of lines you write equals the count of ` + "`###`" + ` lines below. When 1
` + "`###`" + ` line is below, you write 1 line and stop. Do not add a header, do not
add a summary, and do not write a line for a message that has no ` + "`###`" + ` line.
A message long enough to fill the window still gets 1 line.

A message body may hold several paragraphs, a list, or a quoted block. The
whole body is 1 message, so it gets 1 line. Never split a body into 1 line
per paragraph.

Never continue the key sequence past the last ` + "`###`" + ` line. A key that is not
written on a ` + "`###`" + ` line is not a message, so a line for it is a wrong answer.
The last ` + "`###`" + ` line of this prompt is the last message of this prompt.

----- MESSAGES -----
`

// RenderPrompt returns the prompt text of one relevance window.
func RenderPrompt(window window.RelevanceWindow, englishTextByMessage map[string]string) string {
	lineList := []string{RelevancePromptText}
	usedCharCount := len(RelevancePromptText)
	totalCount := len(window.MessageRowList)
	for positionIndex, messageRow := range window.MessageRowList {
		// The position tells the model how many messages the window holds and
		// that the keys are not consecutive, so it stops inventing the keys a
		// gap in the id sequence would hold.
		// A Cyrillic message is read through its English text, so the model
		// reads one language. A message with no translation keeps its own text.
		messageText := messageRow["text"]
		if englishText, hasEnglish := englishTextByMessage[messageRow["message_key"]]; hasEnglish {
			messageText = englishText
		}
		block := fmt.Sprintf("### M%d of %d | %s | %s\n%s\n", positionIndex+1,
			totalCount, messageRow["date"], messageRow["from"], messageText)
		if usedCharCount+len(block) > PromptCharBudget {
			break
		}
		lineList = append(lineList, block)
		usedCharCount += len(block)
	}
	return strings.Join(lineList, "\n")
}

// Run writes data/relevance_windows.csv and one prompt file per window.
func Run(projectFolderPath string, forceRewrite bool) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if createError := os.MkdirAll(promptFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", promptFolderPath, createError)
	}
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	englishTextByMessage, translationError := translatetable.EnglishTextByMessage(projectFolderPath)
	if translationError != nil {
		return translationError
	}
	positionOfMessageKey := make(map[string]int, len(messageTable.DataRows))
	for messageIndex, messageRow := range messageTable.DataRows {
		positionOfMessageKey[messageRow["message_key"]] = messageIndex
	}

	windowTable := table.NewTable(window.RelevanceWindowTableColumnNames)
	for _, window := range window.BuildWindowList(messageTable.DataRows) {
		promptText := RenderPrompt(window, englishTextByMessage)
		promptPath := filepath.Join(promptFolderPath, window.RelWindowId+".txt")
		if forceRewrite {
			if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
			}
		} else if _, statError := os.Stat(promptPath); statError != nil {
			if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
			}
		}
		// The char count is the sum of the `char_len` column, which counts
		// runes, not bytes: a Cyrillic message holds more bytes than runes.
		messageKeyList := make([]string, 0, len(window.MessageRowList))
		charCount := 0
		for _, messageRow := range window.MessageRowList {
			messageKeyList = append(messageKeyList, messageRow["message_key"])
			messageCharCount, parseError := strconv.Atoi(messageRow["char_len"])
			if parseError != nil {
				return fmt.Errorf("message %s holds char_len %q: %w",
					messageRow["message_key"], messageRow["char_len"], parseError)
			}
			charCount += messageCharCount
		}
		windowTable.AppendRow(map[string]string{
			"rel_window_id":     window.RelWindowId,
			"vote_index":        fmt.Sprintf("%d", window.VoteIndex),
			"start_index":       fmt.Sprintf("%d", positionOfMessageKey[window.MessageRowList[0]["message_key"]]),
			"end_index":         fmt.Sprintf("%d", positionOfMessageKey[window.MessageRowList[len(window.MessageRowList)-1]["message_key"]]),
			"first_message_key": window.MessageRowList[0]["message_key"],
			"last_message_key":  window.MessageRowList[len(window.MessageRowList)-1]["message_key"],
			"message_keys":      strings.Join(messageKeyList, " "),
			"message_count":     fmt.Sprintf("%d", len(window.MessageRowList)),
			"char_count":        fmt.Sprintf("%d", charCount),
			"prompt_path":       table.RelativePath(projectFolderPath, promptPath),
			"prompt_chars":      fmt.Sprintf("%d", utf8.RuneCountInString(promptText)),
		})
	}

	windowTablePath := filepath.Join(dataFolderPath, "relevance_windows.csv")
	if writeError := windowTable.WriteTable(windowTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d relevance windows  %s\n", windowTable.RowCount(),
		table.RelativePath(projectFolderPath, windowTablePath))
	fmt.Printf("wrote %6d prompts  prompts/relevance-window-vote-*.txt\n", windowTable.RowCount())
	return nil
}
