package ideaprompt

import "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/window"

// WindowKindOf10Messages is the window kind phase 1 reads.
//
// The constant is taken from the summary group, not copied, so a rename of the
// window kind reaches this group and the 2 groups cannot disagree.
const WindowKindOf10Messages = window.WindowKindOf10Messages

// IdeaWindowTableColumnNames are the columns of data/idea_windows.csv.
//
// The table holds one row per 10-message window, with the prompt path the job
// reads and the message keys that bound the window, so a reader can find the
// chat text behind any extracted item.
var IdeaWindowTableColumnNames = []string{"idea_window_id", "window_kind",
	"month", "first_message_key", "last_message_key", "message_count",
	"prompt_path", "prompt_chars"}
// SummaryFieldColumn is one field of a summary answer and the column of
// `data/summaries.csv` that holds it.
type SummaryFieldColumn struct {
	// FieldName is the field name the summary prompt asked for, e.g.
	// `PAPERS`.
	FieldName string

	// ColumnName is the column of `data/summaries.csv` that holds it, e.g.
	// `papers`.
	ColumnName string
}

// SummaryFieldColumnList maps every field of a summary answer to its column,
// in the order the summary prompt asks for them.
//
// The summary table holds all 14 fields, so this stage reads the 5 it needs
// from the same row and the other 9 stay available to a later phase.
var SummaryFieldColumnList = []SummaryFieldColumn{
	{FieldName: "TOPICS", ColumnName: "topics"},
	{FieldName: "PAPERS", ColumnName: "papers"},
	{FieldName: "TALKS", ColumnName: "talks"},
	{FieldName: "CODE", ColumnName: "code"},
	{FieldName: "METHODS", ColumnName: "methods"},
	{FieldName: "MODELS", ColumnName: "models"},
	{FieldName: "EXPERIMENTS", ColumnName: "experiments"},
	{FieldName: "RESULTS", ColumnName: "results"},
	{FieldName: "REASONS", ColumnName: "reasons"},
	{FieldName: "CAUSAL", ColumnName: "causal"},
	{FieldName: "CLAIMS", ColumnName: "claims"},
	{FieldName: "OPEN", ColumnName: "open"},
	{FieldName: "SUGGESTIONS", ColumnName: "suggestions"},
	{FieldName: "QUOTES", ColumnName: "quotes"},
}
