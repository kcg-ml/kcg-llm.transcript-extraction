package subcommand

// ExitCodeError carries the exit code a subcommand wants the process to end
// with.
//
// A job ends with a code that says why it stopped: 2 when units failed and 3
// when another process holds the lock. A plain error loses that code, because
// the command turns every error into exit 1, and a caller that scripts the
// runner cannot tell a lock refusal from a failure.
type ExitCodeError struct {
	// Code is the exit code the process must end with.
	Code int

	// ReasonText says what happened, in one line.
	ReasonText string
}

// Error returns the reason text.
func (exitCodeError *ExitCodeError) Error() string {
	return exitCodeError.ReasonText
}

// ExitCodeOf returns the exit code an error asks for, or 1 for a plain error.
func ExitCodeOf(runError error) int {
	if exitCodeError, isExitCodeError := runError.(*ExitCodeError); isExitCodeError {
		return exitCodeError.Code
	}
	return 1
}
