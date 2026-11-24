package window

import (
	"fmt"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"
)
// WindowSize is the message count of one relevance window.
const WindowSize = 10

// VoteOffsetList is the start offset of each vote, in messages.
var VoteOffsetList = []int{0, 5, 10}

// MaxWordCount is the word count above which a message gets its own window.
const MaxWordCount = 150

// RelevanceWindowTableColumnNames are the columns of
// data/relevance_windows.csv.
var RelevanceWindowTableColumnNames = []string{"rel_window_id", "vote_index",
	"start_index", "end_index", "first_message_key", "last_message_key",
	"message_keys", "message_count", "char_count", "prompt_path", "prompt_chars"}

// RelevanceWindow is one window of one vote.
type RelevanceWindow struct {
	RelWindowId    string
	VoteIndex      int
	MessageRowList []map[string]string
	IsSolo         bool
}

// BuildWindowList returns the overlapping windows, and the long messages as
// windows of their own.
func BuildWindowList(messageRowList []map[string]string) []RelevanceWindow {
	windowList := make([]RelevanceWindow, 0, len(messageRowList))
	longMessageRowList := make([]map[string]string, 0, 64)
	longMessageKeySet := make(map[string]bool, 64)
	shortMessageRowList := make([]map[string]string, 0, len(messageRowList))
	for _, messageRow := range messageRowList {
		if window.MessageWordCountOf(messageRow) > MaxWordCount {
			longMessageRowList = append(longMessageRowList, messageRow)
			longMessageKeySet[messageRow["message_key"]] = true
		} else {
			shortMessageRowList = append(shortMessageRowList, messageRow)
		}
	}

	for voteIndex := range VoteOffsetList {
		for _, messageRow := range longMessageRowList {
			windowList = append(windowList, RelevanceWindow{
				RelWindowId:    fmt.Sprintf("relevance-window-vote-of-one-long-message-%s-v%d", messageRow["message_key"], voteIndex),
				VoteIndex:      voteIndex,
				MessageRowList: []map[string]string{messageRow},
				IsSolo:         true,
			})
		}
	}

	shortCount := len(shortMessageRowList)
	for voteIndex, offset := range VoteOffsetList {
		for startIndex := 0; startIndex < shortCount; startIndex += WindowSize {
			chunkRowList := sliceWithin(shortMessageRowList, startIndex+offset, WindowSize)
			if len(chunkRowList) == 0 {
				// The offset runs past the last message, so the window wraps
				// to the first messages. Without the wrap the first 10
				// messages sit in 1 window only and never get 3 votes.
				chunkRowList = sliceWithin(shortMessageRowList, 0,
					startIndex+offset+WindowSize-shortCount)
			}
			if len(chunkRowList) == 0 {
				break
			}
			windowList = append(windowList, RelevanceWindow{
				RelWindowId:    fmt.Sprintf("relevance-window-vote-%05d-v%d", startIndex/WindowSize, voteIndex),
				VoteIndex:      voteIndex,
				MessageRowList: chunkRowList,
				IsSolo:         false,
			})
		}
	}
	return windowList
}

// sliceWithin returns at most count rows of a list, starting at startIndex.
func sliceWithin(messageRowList []map[string]string, startIndex int, count int) []map[string]string {
	if startIndex >= len(messageRowList) || count <= 0 {
		return nil
	}
	endIndex := startIndex + count
	if endIndex > len(messageRowList) {
		endIndex = len(messageRowList)
	}
	return messageRowList[startIndex:endIndex]
}
