package window

// The window kinds. A window kind is the value of the `window_kind` column of
// `data/windows.csv`, the prefix of a `window_id`, and the name of the durable
// job that answers the window. Each value states how many messages the window
// holds, so a reader knows what a summary covers without opening the table.
const (
	// WindowKindOf10Messages is a window of 10 consecutive messages.
	WindowKindOf10Messages = "summary-window-of-10-messages"

	// WindowKindOf100Messages is a window of 100 consecutive messages.
	WindowKindOf100Messages = "summary-window-of-100-messages"

	// WindowKindOfOneMonth is a window of 1 calendar month.
	WindowKindOfOneMonth = "summary-window-of-one-month"

	// WindowKindOfWholeChat is the summary of the whole chat, rolled up from
	// the months. It is the only kind that no message window answers.
	WindowKindOfWholeChat = "summary-window-of-whole-chat"
)