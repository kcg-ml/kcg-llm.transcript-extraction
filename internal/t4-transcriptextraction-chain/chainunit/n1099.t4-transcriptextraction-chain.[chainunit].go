package chainunit

// The job names. A job name is the folder under `model-answer/job/` that
// holds one answer file per unit, and it is the value of `--job`. Each name
// states the object the answer describes and the stage that makes it, so a
// reader knows what an answer file holds without opening it.
const (
	// JobNameMessageTranslation answers with the English text of 1 message
	// that holds Cyrillic.
	JobNameMessageTranslation = "message-translation"

	// JobNameSummaryWindowOf10Messages answers with the 14-field summary of
	// 10 consecutive messages.
	JobNameSummaryWindowOf10Messages = "summary-window-of-10-messages"

	// JobNameSummaryWindowOf100Messages answers with the 14-field summary of
	// 100 consecutive messages.
	JobNameSummaryWindowOf100Messages = "summary-window-of-100-messages"

	// JobNameSummaryWindowOfOneMonth answers with the 14-field summary of 1
	// calendar month.
	JobNameSummaryWindowOfOneMonth = "summary-window-of-one-month"

	// JobNameSummaryWindowOfWholeChat answers with the 14-field summary of
	// the whole chat, rolled up from the months.
	JobNameSummaryWindowOfWholeChat = "summary-window-of-whole-chat"

	// JobNameRelevanceWindowVote answers with 1 vote on whether a window of
	// messages is relevant to machine learning.
	JobNameRelevanceWindowVote = "relevance-window-vote"

	// JobNamePhotoContent answers with what 1 photo shows, the text inside
	// it, its topic, its relevance, the numbers it states, and its terms.
	JobNamePhotoContent = "photo-content"

	// JobNameIdeaExtraction answers with the papers, talks, repositories,
	// methods, and models that 10 consecutive messages name, each with the
	// message id it came from.
	JobNameIdeaExtraction = "idea-extraction"
)

// JobNameList names the jobs the runner accepts, in the order a reader runs
// them.
var JobNameList = []string{
	JobNameMessageTranslation,
	JobNameSummaryWindowOf10Messages,
	JobNameSummaryWindowOf100Messages,
	JobNameSummaryWindowOfOneMonth,
	JobNameSummaryWindowOfWholeChat,
	JobNameRelevanceWindowVote,
	JobNamePhotoContent,
	JobNameIdeaExtraction,
}