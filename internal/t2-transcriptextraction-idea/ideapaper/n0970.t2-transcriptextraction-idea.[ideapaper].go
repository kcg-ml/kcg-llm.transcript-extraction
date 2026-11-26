package ideapaper

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)

// IdeaPaperTableColumnNames are the columns of data/papers.csv.
//
// One row is one paper the chat names. `said` is what the chat says about it,
// which is the point of the table: the chat is the source, and a reader wants
// the speakers' view of a paper, not an abstract.
var IdeaPaperTableColumnNames = []string{"paper_key", "arxiv_id", "doi",
	"title", "said", "message_key", "window_id"}

// IdeaPaperFieldName is the idea-extraction field that holds the papers.
const IdeaPaperFieldName = "PAPERS"

// IdeaArxivIdPattern matches an arXiv id inside one paper entry.
//
// An arXiv id is 4 digits, a dot, and 4 or 5 digits. The chat writes it bare
// (`2501.16396`), after `arXiv:`, and inside a link (`arxiv.org/abs/2405.12250`),
// so the prefix is optional and the version suffix is dropped.
var IdeaArxivIdPattern = regexp.MustCompile(
	`(?:arxiv\.org/(?:abs|pdf)/|arxiv[: ]*)?\b([0-9]{4}\.[0-9]{4,5})(?:v[0-9]+)?\b`)

// IdeaDoiPattern matches a DOI inside one paper entry.
var IdeaDoiPattern = regexp.MustCompile(`\b(10\.[0-9]{4,9}/[^\s|,;)\]]+)`)

// IdeaUrlPattern matches a link inside one paper entry.
var IdeaUrlPattern = regexp.MustCompile(`https?://[^\s|,;)\]]+`)

// IdeaPaperSeparatorText separates the parts of one paper entry.
//
// The prompt asks for `<name> | <what it is> | <message id>`, so the first
// part is the name and the second is what the chat says about it.
const IdeaPaperSeparatorText = "|"

// PaperRowOfItem returns the papers.csv row of one PAPERS entry.
//
// The name part is the arXiv id when the entry leads with one, and otherwise
// the title the chat used. A paper with neither is still a row, because the
// chat named it and dropping it would lose a paper.
func PaperRowOfItem(itemRow map[string]string) map[string]string {
	itemText := itemRow["item"]
	partList := strings.Split(itemText, IdeaPaperSeparatorText)
	headText := strings.TrimSpace(partList[0])
	saidText := ""
	if len(partList) > 1 {
		saidText = strings.TrimSpace(partList[1])
	}

	arxivId := ""
	if arxivMatchList := IdeaArxivIdPattern.FindAllStringSubmatch(itemText, -1); len(arxivMatchList) > 0 {
		arxivId = arxivMatchList[0][1]
	}
	doiText := ""
	if doiMatchList := IdeaDoiPattern.FindAllStringSubmatch(itemText, -1); len(doiMatchList) > 0 {
		doiText = strings.TrimSuffix(doiMatchList[0][1], ".")
	}

	titleText := headText
	urlText := ""
	if urlMatchList := IdeaUrlPattern.FindAllString(itemText, -1); len(urlMatchList) > 0 {
		urlText = pattern.CleanUrl(urlMatchList[0])
	}
	// A head that is an id or a link names no title, so the title stays empty
	// rather than repeating the id.
	if arxivId != "" && strings.Contains(headText, arxivId) {
		titleText = ""
	}
	if strings.HasPrefix(headText, "http") {
		titleText = ""
	}
	// A head that is the DOI names no title either, whether it carries the
	// `doi:` prefix or not.
	if doiText != "" && strings.Contains(strings.ToLower(headText), strings.ToLower(doiText)) {
		titleText = ""
	}

	paperKey := arxivId
	if paperKey == "" {
		paperKey = doiText
	}
	if paperKey == "" {
		paperKey = urlText
	}
	if paperKey == "" {
		paperKey = strings.ToLower(pattern.CutRuneText(headText, 80))
	}

	return map[string]string{
		"paper_key":   paperKey,
		"arxiv_id":    arxivId,
		"doi":         doiText,
		"title":       pattern.CutRuneText(titleText, 200),
		"said":        pattern.CutRuneText(saidText, 400),
		"message_key": itemRow["message_key"],
		"window_id":   itemRow["window_id"],
	}
}

// PaperRowListOf merges the rows of one paper key into one row.
//
// The chat names the same paper in several windows, so the table would hold
// one row per mention. A reader wants one row per paper, with the first
// message that names it and the first thing said about it.
func PaperRowListOf(itemRowList []map[string]string) []map[string]string {
	rowByPaperKey := make(map[string]map[string]string, len(itemRowList))
	keyOrderList := make([]string, 0, len(itemRowList))
	for _, itemRow := range itemRowList {
		paperRow := PaperRowOfItem(itemRow)
		paperKey := paperRow["paper_key"]
		if paperKey == "" {
			continue
		}
		keptRow, keyFound := rowByPaperKey[paperKey]
		if !keyFound {
			rowByPaperKey[paperKey] = paperRow
			keyOrderList = append(keyOrderList, paperKey)
			continue
		}
		// A later mention fills a field the first mention left empty, so a
		// paper first named by title gains its arXiv id when a later window
		// gives one.
		for _, columnName := range IdeaPaperTableColumnNames {
			if keptRow[columnName] == "" && paperRow[columnName] != "" {
				keptRow[columnName] = paperRow[columnName]
			}
		}
	}
	sort.Strings(keyOrderList)
	paperRowList := make([]map[string]string, 0, len(keyOrderList))
	for _, paperKey := range keyOrderList {
		paperRowList = append(paperRowList, rowByPaperKey[paperKey])
	}
	return paperRowList
}

// Run writes data/papers.csv from data/idea_items.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	itemTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "idea_items.csv"))
	if readError != nil {
		return fmt.Errorf("read idea_items.csv: %w", readError)
	}

	itemRowList := make([]map[string]string, 0, itemTable.RowCount())
	for _, itemRow := range itemTable.DataRows {
		if itemRow["field"] == IdeaPaperFieldName {
			itemRowList = append(itemRowList, itemRow)
		}
	}
	if len(itemRowList) == 0 {
		return fmt.Errorf("no %s row in idea_items.csv; run the idea-extraction job first",
			IdeaPaperFieldName)
	}

	paperTable := table.NewTable(IdeaPaperTableColumnNames)
	for _, paperRow := range PaperRowListOf(itemRowList) {
		paperTable.AppendRow(paperRow)
	}
	return paperTable.WriteTable(filepath.Join(dataFolderPath, "papers.csv"))
}