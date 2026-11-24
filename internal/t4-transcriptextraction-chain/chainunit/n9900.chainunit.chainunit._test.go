package chainunit

import "testing"

// TestStallBoundIsUnderTheTimeout proves the stall rule can fire before the
// timeout for every job.
//
// A stall bound at or above the timeout is dead code: the timeout kills the
// call first, and a hung call holds its worker for the whole per-job bound
// while the failure says timeout instead of stall.
func TestStallBoundIsUnderTheTimeout(t *testing.T) {
	for _, jobName := range JobNameList {
		timeoutSeconds := TimeoutSecondsOf(jobName)
		stallSeconds := StallSecondsOf(jobName)
		if stallSeconds <= 0 {
			t.Fatalf("%s has no stall bound", jobName)
		}
		if stallSeconds >= timeoutSeconds {
			t.Fatalf("%s: stall %ds is not under the timeout %ds, so the "+
				"stall rule can never fire", jobName, stallSeconds, timeoutSeconds)
		}
		t.Logf("%-38s timeout=%5ds stall=%5ds", jobName, timeoutSeconds, stallSeconds)
	}
}

// TestStallBoundIsAboveTheSlowestMeasuredCall proves a healthy call never
// reaches its stall bound.
//
// The measured seconds come from `model-answer/job/<job>/state.jsonl` and are
// recorded here, because the test must run without the data folder. The
// slowest call of each job is in the comment of its constant in n1098.
func TestStallBoundIsAboveTheSlowestMeasuredCall(t *testing.T) {
	slowestSecondsByJob := map[string]float64{
		JobNameMessageTranslation:         46.9,
		JobNameSummaryWindowOf10Messages:  209.3,
		JobNameSummaryWindowOf100Messages: 380.1,
		JobNameSummaryWindowOfOneMonth:    475.3,
		JobNameSummaryWindowOfWholeChat:   547.6,
		JobNameRelevanceWindowVote:        200.7,
		JobNamePhotoContent:               84.0,
		JobNameIdeaExtraction:             8.3,
	}
	for jobName, slowestSeconds := range slowestSecondsByJob {
		stallSeconds := float64(StallSecondsOf(jobName))
		if stallSeconds <= slowestSeconds {
			t.Fatalf("%s: stall %.0fs is not above the slowest measured call "+
				"%.1fs, so a healthy call is killed", jobName, stallSeconds, slowestSeconds)
		}
	}
}
