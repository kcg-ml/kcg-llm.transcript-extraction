package parse
// SummaryRecord is one parsed window summary.
type SummaryRecord struct {
	WindowId         string
	WindowKind       string
	Month            string
	FirstMessageKey  string
	LastMessageKey   string
	MessageCount     int
	AnswerCharCount  int
	FieldValueOfName map[string]string
}
