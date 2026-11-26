package ideaconcept

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)

// IdeaConceptTableColumnNames are the columns of data/concepts.csv.
//
// One row is one concept the chat names, and the row counts how often the
// chat returns to it. A concept the chat names once is a passing mention; a
// concept named 128 times is what the chat is about, and `hit_count` is what
// separates the 2.
var IdeaConceptTableColumnNames = []string{"concept", "kind", "hit_count",
	"fields", "months", "window_count", "message_keys"}

// IdeaConceptFieldList are the idea-extraction fields a concept comes from.
//
// The 3 fields name the things the chat works with: a method, a model, and a
// repository or dataset. PAPERS and TALKS are left out because they are the
// subject of their own tables, `data/papers.csv` and the index.
var IdeaConceptFieldList = []string{"METHODS", "MODELS", "CODE"}

// IdeaConceptKindSeparatorText separates the field names of one concept.
const IdeaConceptKindSeparatorText = "+"

// IdeaConceptKeySeparatorText separates the message keys of one concept.
const IdeaConceptKeySeparatorText = " "

// IdeaConceptMessageKeyLimit is the number of message keys one row keeps.
//
// A concept named 128 times would otherwise carry 128 keys in one cell and
// make the table unreadable. The count stays exact in `hit_count`, and the
// keys are the first ones the chat used, so a reader can still check the row.
const IdeaConceptMessageKeyLimit = 20

// IdeaConceptRow is the accumulated state of one concept while the table is
// built.
type IdeaConceptRow struct {
	// ConceptText is the concept name as the chat wrote it, lowercased.
	ConceptText string

	// FieldSet holds the idea-extraction fields that named the concept.
	FieldSet map[string]bool

	// HitCount is the number of items that named the concept.
	HitCount int

	// MonthSet holds the months the concept was named in.
	MonthSet map[string]bool

	// WindowSet holds the windows that named the concept.
	WindowSet map[string]bool

	// MessageKeyList holds the message keys, in the order first seen.
	MessageKeyList []string

	// MessageKeySet holds the keys already in MessageKeyList.
	MessageKeySet map[string]bool
}

// ConceptTextOf returns the merge key of one item head.
//
// The head is the name the chat used, so 2 spellings of one name stay 2
// concepts. The text is lowercased and its spaces are collapsed, because the
// chat writes `Modded-NanoGPT` and `modded-nanogpt` for one repository.
func ConceptTextOf(itemHeadText string) string {
	return strings.ToLower(strings.Join(strings.Fields(itemHeadText), " "))
}

// ConceptRowOfItemList returns one row per concept, sorted by hit count and
// then by name.
func ConceptRowOfItemList(itemRowList []map[string]string) []map[string]string {
	conceptRowByText := make(map[string]*IdeaConceptRow, len(itemRowList))
	for _, itemRow := range itemRowList {
		conceptText := ConceptTextOf(itemRow["item_head"])
		if conceptText == "" {
			continue
		}
		conceptRow, rowFound := conceptRowByText[conceptText]
		if !rowFound {
			conceptRow = &IdeaConceptRow{
				ConceptText:   conceptText,
				FieldSet:      make(map[string]bool, 3),
				MonthSet:      make(map[string]bool, 12),
				WindowSet:     make(map[string]bool, 8),
				MessageKeySet: make(map[string]bool, 8),
			}
			conceptRowByText[conceptText] = conceptRow
		}
		conceptRow.HitCount++
		conceptRow.FieldSet[itemRow["field"]] = true
		conceptRow.MonthSet[itemRow["month"]] = true
		conceptRow.WindowSet[itemRow["window_id"]] = true
		messageKey := itemRow["message_key"]
		if messageKey != "" && !conceptRow.MessageKeySet[messageKey] {
			conceptRow.MessageKeySet[messageKey] = true
			conceptRow.MessageKeyList = append(conceptRow.MessageKeyList, messageKey)
		}
	}

	conceptRowList := make([]*IdeaConceptRow, 0, len(conceptRowByText))
	for _, conceptRow := range conceptRowByText {
		conceptRowList = append(conceptRowList, conceptRow)
	}
	sort.Slice(conceptRowList, func(leftIndex, rightIndex int) bool {
		leftRow := conceptRowList[leftIndex]
		rightRow := conceptRowList[rightIndex]
		if leftRow.HitCount != rightRow.HitCount {
			return leftRow.HitCount > rightRow.HitCount
		}
		return leftRow.ConceptText < rightRow.ConceptText
	})

	rowList := make([]map[string]string, 0, len(conceptRowList))
	for _, conceptRow := range conceptRowList {
		keptKeyList := conceptRow.MessageKeyList
		if len(keptKeyList) > IdeaConceptMessageKeyLimit {
			keptKeyList = keptKeyList[:IdeaConceptMessageKeyLimit]
		}
		rowList = append(rowList, map[string]string{
			"concept":      conceptRow.ConceptText,
			"kind":         FieldTextOf(conceptRow.FieldSet),
			"hit_count":    strconv.Itoa(conceptRow.HitCount),
			"fields":       strconv.Itoa(len(conceptRow.FieldSet)),
			"months":       strconv.Itoa(len(conceptRow.MonthSet)),
			"window_count": strconv.Itoa(len(conceptRow.WindowSet)),
			"message_keys": strings.Join(keptKeyList, IdeaConceptKeySeparatorText),
		})
	}
	return rowList
}

// FieldTextOf returns the field names of one concept in the order the
// concept list states them, so 2 rows with the same fields read the same.
func FieldTextOf(fieldSet map[string]bool) string {
	fieldNameList := make([]string, 0, len(fieldSet))
	for _, fieldName := range IdeaConceptFieldList {
		if fieldSet[fieldName] {
			fieldNameList = append(fieldNameList, fieldName)
		}
	}
	return strings.Join(fieldNameList, IdeaConceptKindSeparatorText)
}

// Run writes data/concepts.csv from data/idea_items.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	itemTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "idea_items.csv"))
	if readError != nil {
		return fmt.Errorf("read idea_items.csv: %w", readError)
	}

	fieldSet := make(map[string]bool, len(IdeaConceptFieldList))
	for _, fieldName := range IdeaConceptFieldList {
		fieldSet[fieldName] = true
	}
	itemRowList := make([]map[string]string, 0, itemTable.RowCount())
	for _, itemRow := range itemTable.DataRows {
		if fieldSet[itemRow["field"]] {
			itemRowList = append(itemRowList, itemRow)
		}
	}
	if len(itemRowList) == 0 {
		return fmt.Errorf("no %s row in idea_items.csv; run the idea-extraction job first",
			pattern.CutRuneText(strings.Join(IdeaConceptFieldList, ", "), 60))
	}

	conceptTable := table.NewTable(IdeaConceptTableColumnNames)
	for _, conceptRow := range ConceptRowOfItemList(itemRowList) {
		conceptTable.AppendRow(conceptRow)
	}
	return conceptTable.WriteTable(filepath.Join(dataFolderPath, "concepts.csv"))
}