package store

import "fmt"

// ValidateJobSettings checks every option before the first call, so a bad
// value stops the run at once instead of after hours of work.
//
// The message names the flag the operator typed, because the operator fixes
// the value by retyping that flag.
func ValidateJobSettings(settings JobSettings) error {
	if settings.JobName == "" {
		return fmt.Errorf("--job is empty; give one of the job names")
	}
	if settings.WorkerCount < 1 {
		return fmt.Errorf("--jobs is %d; it must be at least 1",
			settings.WorkerCount)
	}
	if settings.TimeoutSeconds <= 0 {
		return fmt.Errorf("--timeout is %d; it must be more than 0 seconds",
			settings.TimeoutSeconds)
	}
	if settings.RetryCount < 0 {
		return fmt.Errorf("--retries is %d; it must be 0 or more",
			settings.RetryCount)
	}
	if settings.RetrySleepSeconds < 0 {
		return fmt.Errorf("--retry-sleep is %d; it must be 0 or more seconds",
			settings.RetrySleepSeconds)
	}
	if settings.PollSeconds <= 0 {
		return fmt.Errorf("--poll is %d; it must be more than 0 seconds",
			settings.PollSeconds)
	}
	if settings.ModelName == "" {
		return fmt.Errorf("--model is empty; give the model name")
	}
	if settings.ProviderName == "" {
		return fmt.Errorf("--provider is empty; a bare model name matches several providers, so give provider.model")
	}
	if settings.UnitLimit < 0 {
		return fmt.Errorf("--limit is %d; it must be 0 or more", settings.UnitLimit)
	}
	if settings.StallSeconds <= 0 {
		return fmt.Errorf("--stall is %d; it must be more than 0 seconds",
			settings.StallSeconds)
	}
	return nil
}