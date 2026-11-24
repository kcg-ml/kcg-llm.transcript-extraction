package unit
// JobSettings holds the options of one run.
type JobSettings struct {
	JobName           string
	WorkerCount       int
	PollSeconds       int
	ModelName         string
	ProviderName      string
	TimeoutSeconds    int
	RetryCount        int
	RetrySleepSeconds int
	UnitLimit         int
	PrintStatusOnly   bool
	RetryFailedOnly   bool
}
