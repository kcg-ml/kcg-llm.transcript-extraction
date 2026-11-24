package window

import (
	"fmt"
	"sort"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
// WindowTableColumnNames are the columns of data/windows.csv.
var WindowTableColumnNames = []string{"window_id", "window_kind", "window_index",
	"month", "first_message_key", "last_message_key", "message_count",
	"char_count", "ref_count", "prompt_path", "prompt_chars", "prompt_parts"}

// Window is one summary window: its identity and the messages it holds.
type Window struct {
	WindowId       string
	WindowKind     string
	WindowIndex    int
	Month          string
	MessageRowList []map[string]string
}

// WindowListOfSize returns consecutive windows of a fixed message count.
func WindowListOfSize(messageRowList []map[string]string, windowSize int, windowKind string) []Window {
	windowList := make([]Window, 0, len(messageRowList)/windowSize+1)
	for startIndex := 0; startIndex < len(messageRowList); startIndex += windowSize {
		endIndex := startIndex + windowSize
		if endIndex > len(messageRowList) {
			endIndex = len(messageRowList)
		}
		chunkRowList := messageRowList[startIndex:endIndex]
		windowList = append(windowList, Window{
			WindowId:       fmt.Sprintf("%s-%05d", windowKind, startIndex/windowSize),
			WindowKind:     windowKind,
			WindowIndex:    startIndex / windowSize,
			Month:          chunkRowList[0]["month"],
			MessageRowList: chunkRowList,
		})
	}
	return windowList
}

// MonthWindowList returns one window per calendar month, in month order.
func MonthWindowList(messageRowList []map[string]string) []Window {
	monthOfMessageList := make(map[string][]map[string]string, 16)
	for _, messageRow := range messageRowList {
		month := messageRow["month"]
		monthOfMessageList[month] = append(monthOfMessageList[month], messageRow)
	}
	monthList := make([]string, 0, len(monthOfMessageList))
	for month := range monthOfMessageList {
		monthList = append(monthList, month)
	}
	sort.Strings(monthList)
	windowList := make([]Window, 0, len(monthList))
	for monthIndex, month := range monthList {
		windowList = append(windowList, Window{
			WindowId:       "summary-window-of-one-month-" + month,
			WindowKind:     WindowKindOfOneMonth,
			WindowIndex:    monthIndex,
			Month:          month,
			MessageRowList: monthOfMessageList[month],
		})
	}
	return windowList
}

// MessageWordCountOf returns the word count column of a message row as a
// number, for a stage that filters on it.
func MessageWordCountOf(messageRow map[string]string) int {
	wordCount := 0
	fmt.Sscanf(messageRow["word_len"], "%d", &wordCount)
	return wordCount
}

// MonthOfMessageRow returns the month of a message row.
func MonthOfMessageRow(messageRow map[string]string) string {
	return pattern.MonthOf(messageRow["date"])
}
