package rollup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/parse"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/prompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)

// The window id prefixes of the 3 rollup levels. A rollup window is named
// after the level it rolls up, so a reader knows which summaries it covers.
const (
	// RollupWindowIdPrefixOf100Messages names a rollup of 10-message windows.
	RollupWindowIdPrefixOf100Messages = "summary-window-of-whole-chat-from-100-messages-"

	// RollupWindowIdPrefixOfMonths names a rollup of 100-message windows.
	RollupWindowIdPrefixOfMonths = "summary-window-of-whole-chat-from-months-"

	// RollupWindowIdPrefixOfChat names the one summary of the whole chat.
	RollupWindowIdPrefixOfChat = "summary-window-of-whole-chat-"
)
// rollupGroupOfWindowId returns the rollup window a summary belongs to.
func rollupGroupOfWindowId(record parse.SummaryRecord) string {
	switch {
	case strings.HasPrefix(record.WindowId, window.WindowKindOf10Messages+"-"):
		windowIndex, parseError := strconv.Atoi(strings.TrimPrefix(record.WindowId,
			window.WindowKindOf10Messages+"-"))
		if parseError != nil {
			return ""
		}
		return fmt.Sprintf("%s%05d", RollupWindowIdPrefixOf100Messages, windowIndex/10)
	case strings.HasPrefix(record.WindowId, window.WindowKindOf100Messages+"-"):
		return RollupWindowIdPrefixOfMonths + record.Month
	case strings.HasPrefix(record.WindowId, window.WindowKindOfOneMonth+"-"):
		return RollupWindowIdPrefixOfChat + "00000"
	}
	return ""
}

// rollupInstructionLineList is the instruction block of a rollup prompt.
var rollupInstructionLineList = []string{
	"You merge several window summaries of one Telegram chat into one summary.",
	"The chat is between `Synth` and `Vlad [russia][ml]`, about LLM training,",
	"sparsity, pruning, and model architecture.",
	"",
	"Merge the summaries below. Drop a duplicate. Keep every distinct paper,",
	"talk, repository, dataset, method, model, experiment, result, reason,",
	"claim, suggestion, and causal explanation. Keep the speaker's terms.",
	"Never drop a number. A suggestion is advice one speaker gives the",
	"other: `you should do X`, or `if you do X, then Y`. A causal",
	"explanation is a cause and its effect: `X causes Y`, `Y happens",
	"because X`, `X leads to Y`.",
	"",
	"Answer in exactly these 14 lines, one value per line, no other text:",
	"TOPICS: 3 to 10 short topic names, comma separated",
	"PAPERS: `<arxiv id or title> | <what the chat says about it>`, separated by ` ;; `",
	"TALKS: `<name or URL> | <what it is>`, separated by ` ;; `",
	"CODE: `<repository, checkpoint, or dataset> | <what it is>`, separated by ` ;; `",
	"METHODS: `<name> | <what it does here>`, separated by ` ;; `",
	"MODELS: model or checkpoint names, comma separated",
	"EXPERIMENTS: `<what was measured> | <result>`, separated by ` ;; `",
	"RESULTS: `<what it measures> = <value>`, separated by ` ;; `",
	"REASONS: one entry per stated cause, separated by ` ;; `",
	"CAUSAL: `<cause> -> <effect> | <the message id>`, separated by ` ;; `",
	"CLAIMS: one entry per belief or hypothesis, separated by ` ;; `",
	"OPEN: one entry per open question, separated by ` ;; `",
	"SUGGESTIONS: `<the advice> | <the condition it depends on, or none>`, separated by ` ;; `",
	"QUOTES: `<message id> | <the sentence>`, separated by ` ;; `",
	"",
	"Write `none` for a line with no entry.",
	"",
	"----- SUMMARIES -----",
	"",
}

// BuildRollupPromptList writes one prompt per rollup window and returns the
// window rows of those prompts.
func BuildRollupPromptList(projectFolderPath string, recordList []parse.SummaryRecord) ([]map[string]string, error) {
	recordOfId := make(map[string]parse.SummaryRecord, len(recordList))
	childIdListOfGroup := make(map[string][]string, len(recordList))
	for _, record := range recordList {
		recordOfId[record.WindowId] = record
		groupName := rollupGroupOfWindowId(record)
		if groupName == "" {
			continue
		}
		childIdListOfGroup[groupName] = append(childIdListOfGroup[groupName], record.WindowId)
	}
	groupNameList := make([]string, 0, len(childIdListOfGroup))
	for groupName := range childIdListOfGroup {
		groupNameList = append(groupNameList, groupName)
	}
	sort.Strings(groupNameList)

	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if createError := os.MkdirAll(promptFolderPath, 0o755); createError != nil {
		return nil, fmt.Errorf("cannot create %s: %w", promptFolderPath, createError)
	}

	windowRowList := make([]map[string]string, 0, len(groupNameList))
	for _, groupName := range groupNameList {
		childIdList := childIdListOfGroup[groupName]
		sort.Strings(childIdList)
		blockList := make([]string, 0, len(childIdList))
		for _, childId := range childIdList {
			childRecord := recordOfId[childId]
			blockLineList := []string{fmt.Sprintf("### %s (%d messages, %s)",
				childId, childRecord.MessageCount, childRecord.Month)}
			for _, fieldName := range prompt.SummaryFieldList {
				fieldValue := childRecord.FieldValueOfName[fieldName]
				if fieldValue != "" {
					blockLineList = append(blockLineList, fmt.Sprintf("%s: %s", fieldName, fieldValue))
				}
			}
			blockList = append(blockList, strings.Join(blockLineList, "\n"))
		}
		promptText := strings.Join(append(append([]string{}, rollupInstructionLineList...),
			blockList...), "\n")
		promptPath := filepath.Join(promptFolderPath, groupName+".txt")
		if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
			return nil, fmt.Errorf("cannot write %s: %w", promptPath, writeError)
		}

		firstRecord := recordOfId[childIdList[0]]
		lastRecord := recordOfId[childIdList[len(childIdList)-1]]
		messageCount := 0
		charCount := 0
		for _, childId := range childIdList {
			childRecord := recordOfId[childId]
			messageCount += childRecord.MessageCount
			for _, fieldName := range prompt.SummaryFieldList {
				charCount += len(childRecord.FieldValueOfName[fieldName])
			}
		}
		windowRowList = append(windowRowList, map[string]string{
			"window_id":         groupName,
			"window_kind":       window.WindowKindOfWholeChat,
			"window_index":      "0",
			"month":             firstRecord.Month,
			"first_message_key": firstRecord.FirstMessageKey,
			"last_message_key":  lastRecord.LastMessageKey,
			"message_count":     strconv.Itoa(messageCount),
			"char_count":        strconv.Itoa(charCount),
			"ref_count":         "0",
			"prompt_path":       table.RelativePath(projectFolderPath, promptPath),
			"prompt_chars":      strconv.Itoa(utf8.RuneCountInString(promptText)),
			"prompt_parts":      strconv.Itoa(len(childIdList)),
		})
	}
	return windowRowList, nil
}
