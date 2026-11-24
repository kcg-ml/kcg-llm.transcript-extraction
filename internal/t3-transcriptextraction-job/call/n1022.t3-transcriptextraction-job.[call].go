package call

// The 2 ways a call is killed for stalling.
const (
	// StallKindNoGrowth is a call whose output stopped growing.
	StallKindNoGrowth = "no-growth"

	// StallKindTrickle is a call that kept writing but wrote too slowly to
	// finish its answer.
	StallKindTrickle = "trickle"
)

// The trickle rule.
//
// The no-growth rule misses a provider that writes 1 character every 2
// seconds: the growth clock keeps moving, so the call is never killed as a
// stall, and it holds its worker until the timeout and is then recorded as a
// timeout. The measured output rate of a healthy call is 24 to 63 chars per
// second, so a call under 10 chars per second after 30 seconds cannot finish
// its answer inside any timeout this runner sets.
const (
	// TrickleCharRatePerSecond is the output rate below which a call is too
	// slow to finish.
	TrickleCharRatePerSecond = 10.0

	// TrickleGraceSeconds is how long a call may run before the rate is
	// judged. A healthy call writes its first characters well inside this
	// window, so the rule never fires on a slow start.
	TrickleGraceSeconds = 30.0
)

// StallRecord says why a call was killed for stalling.
type StallRecord struct {
	// KindText is StallKindNoGrowth or StallKindTrickle.
	KindText string

	// SecondCount is the seconds the output stood still, or the seconds the
	// call had run when the trickle rule fired.
	SecondCount float64

	// CharRate is the output rate of the call when the trickle rule fired,
	// and 0 for a no-growth stall.
	CharRate float64
}