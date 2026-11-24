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
// The stall bound of one call, per job.
//
// A stall bound answers a different question from the timeout: the timeout
// asks whether the call has taken too long in total, and the stall bound asks
// whether the call has stopped producing. The provider this pipeline uses
// does not stream: it holds the whole answer and writes it at the end, and a
// measured 181224-char prompt answered in 315.0s with its first byte at
// 313.0s. So the time before the first byte is the provider thinking, and a
// bound under that time kills a healthy call.
//
// Each bound below is the larger of 90s and 15% above the slowest measured
// call of its job, so a healthy call never reaches it. Each bound is also
// under the job's timeout, so a call that stops producing is killed as a
// stall and not as a timeout, and the failure says which.
const (
	// StallSecondsMessageTranslation is above the slowest of 284 calls,
	// 46.9s, and under the 145s timeout.
	StallSecondsMessageTranslation = 90

	// StallSecondsSummaryWindowOf10Messages is 15% above the slowest of
	// 1042 calls, 209.3s, and under the 483s timeout.
	StallSecondsSummaryWindowOf10Messages = 241

	// StallSecondsSummaryWindowOf100Messages is 15% above the slowest of
	// 105 calls, 380.1s, and under the 1206s timeout.
	StallSecondsSummaryWindowOf100Messages = 438

	// StallSecondsSummaryWindowOfOneMonth is 15% above the slowest of 13
	// calls, 475.3s, and under the 1901s timeout.
	StallSecondsSummaryWindowOfOneMonth = 547

	// StallSecondsSummaryWindowOfWholeChat is 15% above the slowest of 29
	// calls, 547.6s, and under the 2015s timeout.
	StallSecondsSummaryWindowOfWholeChat = 630

	// StallSecondsRelevanceWindowVote is 205s, between the slowest of 2862
	// calls, 200.7s, and the 212s timeout.
	//
	// The margin is narrow because this job's slowest healthy call is close
	// to its timeout: the timeout is 4 x the 53.0s p95, and 3 calls of 2862
	// ran past 190s. A bound of 15% above the slowest call would be 231s,
	// which is above the timeout, so the timeout would kill first and the
	// stall rule would never fire. 205s keeps both rules live, and the 4.3s
	// of margin under the slowest call is the price of that.
	StallSecondsRelevanceWindowVote = 205

	// StallSecondsPhotoContent is 15% above the slowest of 797 calls,
	// 84.0s, and under the 173s timeout.
	StallSecondsPhotoContent = 97

	// StallSecondsIdeaExtraction is the 90s floor. This job has 5 measured
	// calls, the slowest 8.3s, so no measured call comes near the floor.
	StallSecondsIdeaExtraction = 90
)

// StallSecondsOf returns the stall bound of one job, or 0 for an unknown job.
func StallSecondsOf(jobName string) int {
	switch jobName {
	case JobNameMessageTranslation:
		return StallSecondsMessageTranslation
	case JobNameSummaryWindowOf10Messages:
		return StallSecondsSummaryWindowOf10Messages
	case JobNameSummaryWindowOf100Messages:
		return StallSecondsSummaryWindowOf100Messages
	case JobNameSummaryWindowOfOneMonth:
		return StallSecondsSummaryWindowOfOneMonth
	case JobNameSummaryWindowOfWholeChat:
		return StallSecondsSummaryWindowOfWholeChat
	case JobNameRelevanceWindowVote:
		return StallSecondsRelevanceWindowVote
	case JobNamePhotoContent:
		return StallSecondsPhotoContent
	case JobNameIdeaExtraction:
		return StallSecondsIdeaExtraction
	}
	return 0
}
