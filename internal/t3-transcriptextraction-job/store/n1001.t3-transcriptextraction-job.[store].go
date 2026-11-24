package store

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

	// StallSeconds is how long a call may go without growing its output
	// before the runner kills it. A call that hangs holds its worker for the
	// whole timeout otherwise, so a stall is caught far sooner.
	StallSeconds int

	// ProbeOnly makes one call on the first pending unit and prints its
	// seconds, chars, and chars per second without saving an answer. It
	// checks a provider before a long run.
	ProbeOnly bool
}