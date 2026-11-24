package index

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/parse"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/rollup"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
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

// buildIndexTableList writes the 3 index tables and returns the flat index
// table, so the photo stage can append its rows.
func buildIndexTableList(projectFolderPath string, recordList []parse.SummaryRecord,
	itemRecordList []parse.SummaryItemRecord) (*table.Table, error) {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return nil, readError
	}
	referenceTable, referenceError := table.ReadTable(
		filepath.Join(dataFolderPath, "references.csv"))
	if referenceError != nil {
		return nil, referenceError
	}
	referenceListOfMessage := make(map[string][]map[string]string, len(referenceTable.DataRows))
	for _, referenceRow := range referenceTable.DataRows {
		messageKey := referenceRow["message_key"]
		referenceListOfMessage[messageKey] = append(referenceListOfMessage[messageKey], referenceRow)
	}

	w10WindowOfMessage := make(map[string]string, len(messageTable.DataRows))
	for _, record := range recordList {
		if record.WindowKind != window.WindowKindOf10Messages {
			continue
		}
		for _, messageRow := range messageTable.DataRows {
			messageKey := messageRow["message_key"]
			if messageKey >= record.FirstMessageKey && messageKey <= record.LastMessageKey {
				w10WindowOfMessage[messageKey] = record.WindowId
			}
		}
	}

	indexMessageTable := table.NewTable(IndexMessageTableColumnNames)
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
		textHead := strings.ReplaceAll(pattern.CutRuneText(messageRow["text"], 200), "\n", " ")
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
			"is_duplicate": pattern.BoolText(messageRow["duplicate_of"] != ""),
			"text_head":    textHead,
		})
	}
	if writeError := parse.WriteTableFile(projectFolderPath, "index_messages.csv", indexMessageTable); writeError != nil {
		return nil, writeError
	}

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
	indexTermTable := table.NewTable(IndexTermTableColumnNames)
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
	if writeError := parse.WriteTableFile(projectFolderPath, "index_terms.csv", indexTermTable); writeError != nil {
		return nil, writeError
	}

	recordOfId := make(map[string]parse.SummaryRecord, len(recordList))
	for _, record := range recordList {
		recordOfId[record.WindowId] = record
	}
	indexTable := table.NewTable(IndexTableColumnNames)
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
	if writeError := parse.WriteTableFile(projectFolderPath, "index.csv", indexTable); writeError != nil {
		return nil, writeError
	}
	return indexTable, nil
}

	// The w10 window that holds a message, found by the key range of the
	// window. A key sorts by message id, so a range compare is exact.
	// Every term, with the windows and the months it hits.
	// The flat index: one row per (window, field, item), with the message
	// range of the window.
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

// Run writes the summary tables, the rollup prompts, and the index tables.
func Run(projectFolderPath string) error {
	recordList, itemRecordList, loadError := parse.LoadSummaryRecordList(projectFolderPath)
	if loadError != nil {
		return loadError
	}
	if len(recordList) == 0 {
		return fmt.Errorf("no model answers in model-answer/; run the job runner first")
	}

	summaryTable := table.NewTable(parse.SummaryTableColumnNames)
	for _, record := range recordList {
		summaryTable.AppendRow(parse.SummaryRowOf(record))
	}
	if writeError := parse.WriteTableFile(projectFolderPath, "summaries.csv", summaryTable); writeError != nil {
		return writeError
	}

	itemTable := table.NewTable(parse.SummaryItemTableColumnNames)
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
	if writeError := parse.WriteTableFile(projectFolderPath, "summary_items.csv", itemTable); writeError != nil {
		return writeError
	}

	windowRowList, rollupError := rollup.BuildRollupPromptList(projectFolderPath, recordList)
	if rollupError != nil {
		return rollupError
	}
	addedCount, appendError := table.AppendRowsToTable(
		filepath.Join(projectFolderPath, "data", "windows.csv"), windowRowList)
	if appendError != nil {
		return appendError
	}
	fmt.Printf("wrote %6d rollup prompts  prompts/%s*.txt (%d new windows)\n",
		len(windowRowList), window.WindowKindOfWholeChat+"-", addedCount)
	if addedCount > 0 {
		fmt.Printf("run the rollups with: transcriptextraction job --job %s\n",
			window.WindowKindOfWholeChat)
	}

	rollupRecordList := make([]parse.SummaryRecord, 0, len(windowRowList))
	for _, windowRow := range windowRowList {
		windowId := windowRow["window_id"]
		answerText, answerError := parse.ReadAnswerText(projectFolderPath, windowId)
		if answerError != nil {
			continue
		}
		messageCount, _ := strconv.Atoi(windowRow["message_count"])
		rollupRecordList = append(rollupRecordList, parse.SummaryRecord{
			WindowId:         windowId,
			WindowKind:       window.WindowKindOfWholeChat,
			Month:            windowRow["month"],
			FirstMessageKey:  windowRow["first_message_key"],
			LastMessageKey:   windowRow["last_message_key"],
			MessageCount:     messageCount,
			AnswerCharCount:  len(answerText),
			FieldValueOfName: parse.ParseAnswer(answerText),
		})
	}
	if len(rollupRecordList) > 0 {
		rollupTable := table.NewTable(parse.SummaryTableColumnNames)
		for _, record := range rollupRecordList {
			rollupTable.AppendRow(parse.SummaryRowOf(record))
		}
		if writeError := parse.WriteTableFile(projectFolderPath, "summary_rollup.csv", rollupTable); writeError != nil {
			return writeError
		}
	}

	_, indexError := buildIndexTableList(projectFolderPath, recordList, itemRecordList)
	return indexError
}
