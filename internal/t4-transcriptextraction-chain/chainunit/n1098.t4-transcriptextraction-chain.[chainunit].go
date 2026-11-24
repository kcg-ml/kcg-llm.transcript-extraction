package chainunit

// The timeout of one call, per job.
//
// A flat timeout is wrong in both directions: 600s is 16 times the worst
// message-translation call, so a hung translation holds a worker for 10
// minutes, and it is under the worst whole-chat call, so a slow rollup is
// killed while it works. Each bound below is 4 times the p95 of the seconds
// measured in `model-answer/job/<job>/state.jsonl`, with a floor of 60s.
//
// A bound of 1 to 4 seconds is not possible with this model. The measured
// output rate is 24 to 63 chars per second, and a 10-message summary needs
// about 1900 chars, so a 1 to 4 second call yields 24 to 250 chars, which is
// a truncated answer. The bound therefore limits a runaway call, not a normal
// one.
const (
	// TimeoutSecondsMessageTranslation is 4 x 36.4s, the p95 of 284 calls.
	TimeoutSecondsMessageTranslation = 145

	// TimeoutSecondsSummaryWindowOf10Messages is 4 x 120.9s, the p95 of 1042
	// calls.
	TimeoutSecondsSummaryWindowOf10Messages = 483

	// TimeoutSecondsSummaryWindowOf100Messages is 4 x 301.5s, the p95 of 105
	// calls.
	TimeoutSecondsSummaryWindowOf100Messages = 1206

	// TimeoutSecondsSummaryWindowOfOneMonth is 4 x 475.3s, the p95 of 13
	// calls.
	TimeoutSecondsSummaryWindowOfOneMonth = 1901

	// TimeoutSecondsSummaryWindowOfWholeChat is 4 x 503.9s, the p95 of 29
	// calls.
	TimeoutSecondsSummaryWindowOfWholeChat = 2015

	// TimeoutSecondsRelevanceWindowVote is 4 x 53.0s, the p95 of 2867 calls.
	TimeoutSecondsRelevanceWindowVote = 212

	// TimeoutSecondsPhotoContent is 4 x 43.3s, the p95 of 797 calls.
	TimeoutSecondsPhotoContent = 173

	// TimeoutSecondsIdeaExtraction is 4 x 121s, the p95 of the
	// summary-window-of-10-messages calls.
	//
	// This job has no measured calls yet, so the bound is taken from the job
	// that reads the same 10 messages and writes a longer answer. The phase 1
	// answer is 5 lines against that job's 14, so the bound is generous. A
	// measured p95 replaces it after the first full run.
	TimeoutSecondsIdeaExtraction = 483
)

// TimeoutSecondsOf returns the timeout of one job, or 0 for an unknown job.
func TimeoutSecondsOf(jobName string) int {
	switch jobName {
	case JobNameMessageTranslation:
		return TimeoutSecondsMessageTranslation
	case JobNameSummaryWindowOf10Messages:
		return TimeoutSecondsSummaryWindowOf10Messages
	case JobNameSummaryWindowOf100Messages:
		return TimeoutSecondsSummaryWindowOf100Messages
	case JobNameSummaryWindowOfOneMonth:
		return TimeoutSecondsSummaryWindowOfOneMonth
	case JobNameSummaryWindowOfWholeChat:
		return TimeoutSecondsSummaryWindowOfWholeChat
	case JobNameRelevanceWindowVote:
		return TimeoutSecondsRelevanceWindowVote
	case JobNamePhotoContent:
		return TimeoutSecondsPhotoContent
	case JobNameIdeaExtraction:
		return TimeoutSecondsIdeaExtraction
	}
	return 0
}