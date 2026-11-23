// Package summaryaggregation turns the model answers into the summary tables,
// the rollup prompts, and the flat index a reader searches.
//
// The rollup runs in 3 steps:
//
//	10-message windows  ->  100-message window   (10 answers into 1)
//	100-message windows ->  month                (3 to 35 answers into 1)
//	month windows       ->  whole chat           (13 answers into 1)
//
// The rollup text goes to prompts/meta-*.txt and is answered by the model
// through the durable job `meta`, so the summary of summaries is a model
// answer, not a string join.
package summaryaggregation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summarywindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

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

// IndexTableColumnNames are the columns of data/index.csv.
var IndexTableColumnNames = []string{"index_id", "window_id", "window_kind",
	"month", "first_message_key", "last_message_key", "field", "item",
	"item_head"}

// IndexMessageTableColumnNames are the columns of data/index_messages.csv.
var IndexMessageTableColumnNames = []string{"message_key", "date", "month",
	"from", "word_len", "url_count", "arxiv_ids", "ref_count", "ref_kinds",
	"w10_window", "is_duplicate", "text_head"}

// IndexTermTableColumnNames are the columns of data/index_terms.csv.
var IndexTermTableColumnNames = []string{"term", "hit_count", "fields", "months",
	"month_count", "windows", "window_count"}

// ParseAnswer returns the 14 field values of one model answer.
func ParseAnswer(answerText string) map[string]string {
	fieldValueOfName := make(map[string]string, len(summarywindow.SummaryFieldList))
	for _, fieldName := range summarywindow.SummaryFieldList {
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

// summaryRecord is one parsed window summary.
type summaryRecord struct {
	WindowId         string
	WindowKind       string
	Month            string
	FirstMessageKey  string
	LastMessageKey   string
	MessageCount     int
	AnswerCharCount  int
	FieldValueOfName map[string]string
}

// itemRecord is one item of one summary field.
type itemRecord struct {
	WindowId     string
	WindowKind   string
	Month        string
	FieldName    string
	ItemIndex    int
	ItemText     string
	ItemHeadText string
}

// loadSummaryRecordList reads the answers of the w10, w100, and month windows.
func loadSummaryRecordList(projectFolderPath string) ([]summaryRecord, []itemRecord, error) {
	windowTable, readError := csvtable.ReadTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"))
	if readError != nil {
		return nil, nil, readError
	}
	windowIdList := make([]string, 0, len(windowTable.DataRows))
	windowRowOfId := make(map[string]map[string]string, len(windowTable.DataRows))
	for _, windowRow := range windowTable.DataRows {
		windowId := windowRow["window_id"]
		if windowRow["window_kind"] == "meta" {
			continue
		}
		windowIdList = append(windowIdList, windowId)
		windowRowOfId[windowId] = windowRow
	}
	sort.Strings(windowIdList)

	summaryRecordList := make([]summaryRecord, 0, len(windowIdList))
	itemRecordList := make([]itemRecord, 0, len(windowIdList)*8)
	for _, windowId := range windowIdList {
		answerText, answerError := readAnswerText(projectFolderPath, windowId)
		if answerError != nil {
			continue
		}
		windowRow := windowRowOfId[windowId]
		fieldValueOfName := ParseAnswer(answerText)
		messageCount, _ := strconv.Atoi(windowRow["message_count"])
		summaryRecordList = append(summaryRecordList, summaryRecord{
			WindowId:         windowId,
			WindowKind:       windowRow["window_kind"],
			Month:            windowRow["month"],
			FirstMessageKey:  windowRow["first_message_key"],
			LastMessageKey:   windowRow["last_message_key"],
			MessageCount:     messageCount,
			AnswerCharCount:  len(answerText),
			FieldValueOfName: fieldValueOfName,
		})
		for _, fieldName := range summarywindow.SummaryFieldList {
			for itemIndex, itemText := range SplitItemList(fieldValueOfName[fieldName]) {
				itemRecordList = append(itemRecordList, itemRecord{
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

// readAnswerText returns the answer of one window, from the durable job folder
// first and from the flat out/ folder second.
func readAnswerText(projectFolderPath string, windowId string) (string, error) {
	candidatePathList := []string{
		filepath.Join(projectFolderPath, "out", "jobs", jobNameOfWindowId(windowId), "done", windowId+".txt"),
		filepath.Join(projectFolderPath, "out", windowId+".txt"),
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
func jobNameOfWindowId(windowId string) string {
	switch {
	case strings.HasPrefix(windowId, "w10-"):
		return "w10"
	case strings.HasPrefix(windowId, "w100-"):
		return "w100"
	case strings.HasPrefix(windowId, "month-"):
		return "month"
	case strings.HasPrefix(windowId, "meta-"):
		return "meta"
	}
	return "w10"
}

// summaryRowOf returns the table row of one summary record.
func summaryRowOf(record summaryRecord) map[string]string {
	row := map[string]string{
		"window_id":         record.WindowId,
		"window_kind":       record.WindowKind,
		"month":             record.Month,
		"first_message_key": record.FirstMessageKey,
		"last_message_key":  record.LastMessageKey,
		"message_count":     strconv.Itoa(record.MessageCount),
		"answer_chars":      strconv.Itoa(record.AnswerCharCount),
	}
	for _, fieldName := range summarywindow.SummaryFieldList {
		row[strings.ToLower(fieldName)] = record.FieldValueOfName[fieldName]
	}
	return row
}

// writeTable writes one table into the data folder and prints its row count.
func writeTable(projectFolderPath string, tableName string, table *csvtable.Table) error {
	tablePath := filepath.Join(projectFolderPath, "data", tableName)
	if writeError := table.WriteTable(tablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d rows  %s\n", table.RowCount(),
		csvtable.RelativePath(projectFolderPath, tablePath))
	return nil
}

// rollupGroupOfWindowId returns the rollup window a summary belongs to.
func rollupGroupOfWindowId(record summaryRecord) string {
	switch {
	case strings.HasPrefix(record.WindowId, "w10-"):
		windowIndex, parseError := strconv.Atoi(strings.TrimPrefix(record.WindowId, "w10-"))
		if parseError != nil {
			return ""
		}
		return fmt.Sprintf("meta-w100-%05d", windowIndex/10)
	case strings.HasPrefix(record.WindowId, "w100-"):
		return "meta-month-" + record.Month
	case strings.HasPrefix(record.WindowId, "month-"):
		return "meta-chat-00000"
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

// buildRollupPromptList writes one prompt per rollup window and returns the
// window rows of those prompts.
func buildRollupPromptList(projectFolderPath string, recordList []summaryRecord) ([]map[string]string, error) {
	recordOfId := make(map[string]summaryRecord, len(recordList))
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
			for _, fieldName := range summarywindow.SummaryFieldList {
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
			for _, fieldName := range summarywindow.SummaryFieldList {
				charCount += len(childRecord.FieldValueOfName[fieldName])
			}
		}
		windowRowList = append(windowRowList, map[string]string{
			"window_id":         groupName,
			"window_kind":       "meta",
			"window_index":      "0",
			"month":             firstRecord.Month,
			"first_message_key": firstRecord.FirstMessageKey,
			"last_message_key":  lastRecord.LastMessageKey,
			"message_count":     strconv.Itoa(messageCount),
			"char_count":        strconv.Itoa(charCount),
			"ref_count":         "0",
			"prompt_path":       csvtable.RelativePath(projectFolderPath, promptPath),
			"prompt_chars":      strconv.Itoa(utf8.RuneCountInString(promptText)),
			"prompt_parts":      strconv.Itoa(len(childIdList)),
		})
	}
	return windowRowList, nil
}

// buildIndexTableList writes the 3 index tables and returns the flat index
// table, so the photo stage can append its rows.
func buildIndexTableList(projectFolderPath string, recordList []summaryRecord,
	itemRecordList []itemRecord) (*csvtable.Table, error) {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := csvtable.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return nil, readError
	}
	referenceTable, referenceError := csvtable.ReadTable(
		filepath.Join(dataFolderPath, "references.csv"))
	if referenceError != nil {
		return nil, referenceError
	}
	referenceListOfMessage := make(map[string][]map[string]string, len(referenceTable.DataRows))
	for _, referenceRow := range referenceTable.DataRows {
		messageKey := referenceRow["message_key"]
		referenceListOfMessage[messageKey] = append(referenceListOfMessage[messageKey], referenceRow)
	}

	// The w10 window that holds a message, found by the key range of the
	// window. A key sorts by message id, so a range compare is exact.
	w10WindowOfMessage := make(map[string]string, len(messageTable.DataRows))
	for _, record := range recordList {
		if record.WindowKind != "w10" {
			continue
		}
		for _, messageRow := range messageTable.DataRows {
			messageKey := messageRow["message_key"]
			if messageKey >= record.FirstMessageKey && messageKey <= record.LastMessageKey {
				w10WindowOfMessage[messageKey] = record.WindowId
			}
		}
	}

	indexMessageTable := csvtable.NewTable(IndexMessageTableColumnNames)
	for _, messageRow := range messageTable.DataRows {
		messageKey := messageRow["message_key"]
		referenceRowList := referenceListOfMessage[messageKey]
		kindCountOfName := make(map[string]int, 8)
		for _, referenceRow := range referenceRowList {
			kindCountOfName[referenceRow["ref_kind"]]++
		}
		kindNameList := make([]string, 0, len(kindCountOfName))
		for kindName := range kindCountOfName {
			kindNameList = append(kindNameList, kindName)
		}
		sort.Strings(kindNameList)
		kindTextList := make([]string, 0, len(kindNameList))
		for _, kindName := range kindNameList {
			kindTextList = append(kindTextList, fmt.Sprintf("%s=%d", kindName, kindCountOfName[kindName]))
		}
		textHead := strings.ReplaceAll(cutText(messageRow["text"], 200), "\n", " ")
		indexMessageTable.AppendRow(map[string]string{
			"message_key":  messageKey,
			"date":         messageRow["date"],
			"month":        messageRow["month"],
			"from":         messageRow["from"],
			"word_len":     messageRow["word_len"],
			"url_count":    messageRow["url_count"],
			"arxiv_ids":    messageRow["arxiv_ids"],
			"ref_count":    strconv.Itoa(len(referenceRowList)),
			"ref_kinds":    strings.Join(kindTextList, " "),
			"w10_window":   w10WindowOfMessage[messageKey],
			"is_duplicate": boolText(messageRow["duplicate_of"] != ""),
			"text_head":    textHead,
		})
	}
	if writeError := writeTable(projectFolderPath, "index_messages.csv", indexMessageTable); writeError != nil {
		return nil, writeError
	}

	// Every term, with the windows and the months it hits.
	termEntryOfName := make(map[string]*termEntry, len(itemRecordList))
	for _, itemRecord := range itemRecordList {
		termName := strings.ToLower(itemRecord.ItemHeadText)
		if termName == "" {
			continue
		}
		entry, isKnown := termEntryOfName[termName]
		if !isKnown {
			entry = &termEntry{
				windowSet: make(map[string]bool, 8),
				monthSet:  make(map[string]bool, 8),
				fieldSet:  make(map[string]bool, 4),
			}
			termEntryOfName[termName] = entry
		}
		entry.windowSet[itemRecord.WindowId] = true
		entry.monthSet[itemRecord.Month] = true
		entry.fieldSet[itemRecord.FieldName] = true
		entry.hitCount++
	}
	termNameList := make([]string, 0, len(termEntryOfName))
	for termName := range termEntryOfName {
		termNameList = append(termNameList, termName)
	}
	sort.Slice(termNameList, func(leftIndex int, rightIndex int) bool {
		leftCount := termEntryOfName[termNameList[leftIndex]].hitCount
		rightCount := termEntryOfName[termNameList[rightIndex]].hitCount
		if leftCount != rightCount {
			return leftCount > rightCount
		}
		return termNameList[leftIndex] < termNameList[rightIndex]
	})
	indexTermTable := csvtable.NewTable(IndexTermTableColumnNames)
	for _, termName := range termNameList {
		entry := termEntryOfName[termName]
		windowNameList := sortedSetKeys(entry.windowSet)
		shownWindowList := windowNameList
		if len(shownWindowList) > 40 {
			shownWindowList = shownWindowList[:40]
		}
		indexTermTable.AppendRow(map[string]string{
			"term":         termName,
			"hit_count":    strconv.Itoa(entry.hitCount),
			"fields":       strings.Join(sortedSetKeys(entry.fieldSet), " "),
			"months":       strings.Join(sortedSetKeys(entry.monthSet), " "),
			"month_count":  strconv.Itoa(len(entry.monthSet)),
			"windows":      strings.Join(shownWindowList, " "),
			"window_count": strconv.Itoa(len(entry.windowSet)),
		})
	}
	if writeError := writeTable(projectFolderPath, "index_terms.csv", indexTermTable); writeError != nil {
		return nil, writeError
	}

	// The flat index: one row per (window, field, item), with the message
	// range of the window.
	recordOfId := make(map[string]summaryRecord, len(recordList))
	for _, record := range recordList {
		recordOfId[record.WindowId] = record
	}
	indexTable := csvtable.NewTable(IndexTableColumnNames)
	for _, itemRecord := range itemRecordList {
		record := recordOfId[itemRecord.WindowId]
		indexTable.AppendRow(map[string]string{
			"index_id":          fmt.Sprintf("I%06d", indexTable.RowCount()+1),
			"window_id":         itemRecord.WindowId,
			"window_kind":       itemRecord.WindowKind,
			"month":             itemRecord.Month,
			"first_message_key": record.FirstMessageKey,
			"last_message_key":  record.LastMessageKey,
			"field":             itemRecord.FieldName,
			"item":              itemRecord.ItemText,
			"item_head":         itemRecord.ItemHeadText,
		})
	}
	if writeError := writeTable(projectFolderPath, "index.csv", indexTable); writeError != nil {
		return nil, writeError
	}
	return indexTable, nil
}

// termEntry holds the windows, months, and fields one term hits.
type termEntry struct {
	windowSet map[string]bool
	monthSet  map[string]bool
	fieldSet  map[string]bool
	hitCount  int
}

// sortedSetKeys returns the keys of a set in sorted order.
func sortedSetKeys(valueSet map[string]bool) []string {
	keyList := make([]string, 0, len(valueSet))
	for key := range valueSet {
		keyList = append(keyList, key)
	}
	sort.Strings(keyList)
	return keyList
}

// cutText returns the first maxLength characters of a text.
func cutText(text string, maxLength int) string {
	return textrule.CutRuneText(text, maxLength)
}

// boolText writes 1 for true and 0 for false, the shape the tables use.
func boolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// Run writes the summary tables, the rollup prompts, and the index tables.
func Run(projectFolderPath string) error {
	recordList, itemRecordList, loadError := loadSummaryRecordList(projectFolderPath)
	if loadError != nil {
		return loadError
	}
	if len(recordList) == 0 {
		return fmt.Errorf("no model answers in out/; run the job runner first")
	}

	summaryTable := csvtable.NewTable(SummaryTableColumnNames)
	for _, record := range recordList {
		summaryTable.AppendRow(summaryRowOf(record))
	}
	if writeError := writeTable(projectFolderPath, "summaries.csv", summaryTable); writeError != nil {
		return writeError
	}

	itemTable := csvtable.NewTable(SummaryItemTableColumnNames)
	for _, itemRecord := range itemRecordList {
		itemTable.AppendRow(map[string]string{
			"window_id":   itemRecord.WindowId,
			"window_kind": itemRecord.WindowKind,
			"month":       itemRecord.Month,
			"field":       itemRecord.FieldName,
			"item_index":  strconv.Itoa(itemRecord.ItemIndex),
			"item":        itemRecord.ItemText,
			"item_head":   itemRecord.ItemHeadText,
		})
	}
	if writeError := writeTable(projectFolderPath, "summary_items.csv", itemTable); writeError != nil {
		return writeError
	}

	windowRowList, rollupError := buildRollupPromptList(projectFolderPath, recordList)
	if rollupError != nil {
		return rollupError
	}
	addedCount, appendError := csvtable.AppendRowsToTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"), windowRowList)
	if appendError != nil {
		return appendError
	}
	fmt.Printf("wrote %6d rollup prompts  prompts/meta-*.txt (%d new windows)\n",
		len(windowRowList), addedCount)
	if addedCount > 0 {
		fmt.Println("run the rollups with: transcriptextraction job --job meta")
	}

	rollupRecordList := make([]summaryRecord, 0, len(windowRowList))
	for _, windowRow := range windowRowList {
		windowId := windowRow["window_id"]
		answerText, answerError := readAnswerText(projectFolderPath, windowId)
		if answerError != nil {
			continue
		}
		messageCount, _ := strconv.Atoi(windowRow["message_count"])
		rollupRecordList = append(rollupRecordList, summaryRecord{
			WindowId:         windowId,
			WindowKind:       "meta",
			Month:            windowRow["month"],
			FirstMessageKey:  windowRow["first_message_key"],
			LastMessageKey:   windowRow["last_message_key"],
			MessageCount:     messageCount,
			AnswerCharCount:  len(answerText),
			FieldValueOfName: ParseAnswer(answerText),
		})
	}
	if len(rollupRecordList) > 0 {
		rollupTable := csvtable.NewTable(SummaryTableColumnNames)
		for _, record := range rollupRecordList {
			rollupTable.AppendRow(summaryRowOf(record))
		}
		if writeError := writeTable(projectFolderPath, "summary_rollup.csv", rollupTable); writeError != nil {
			return writeError
		}
	}

	_, indexError := buildIndexTableList(projectFolderPath, recordList, itemRecordList)
	return indexError
}
