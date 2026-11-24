package ideaprompt

import "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"

// WindowKindOf10Messages is the window kind phase 1 reads.
//
// The constant is taken from the summary group, not copied, so a rename of the
// window kind reaches this group and the 2 groups cannot disagree.
const WindowKindOf10Messages = window.WindowKindOf10Messages

// IdeaWindowTableColumnNames are the columns of data/idea_windows.csv.
//
// The table holds one row per 10-message window, with the prompt path the job
// reads and the message keys that bound the window, so a reader can find the
// chat text behind any extracted item.
var IdeaWindowTableColumnNames = []string{"idea_window_id", "window_kind",
	"month", "first_message_key", "last_message_key", "message_count",
	"prompt_path", "prompt_chars"}