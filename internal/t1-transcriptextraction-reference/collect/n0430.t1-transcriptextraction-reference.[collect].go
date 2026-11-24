package collect

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-reference/kind"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-reference/refpattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-reference/sentence"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// referenceCollector accumulates the reference rows of one run.
type referenceCollector struct {
	table      *table.Table
	rowCounter int
}

// add appends one reference row, cutting the long columns to the length the
// table states.
func (collector *referenceCollector) add(messageRow map[string]string, referenceKind string,
	referenceValue string, referenceLabel string, sentenceText string, contextText string) {
	collector.rowCounter++
	collector.table.AppendRow(map[string]string{
		"ref_id":       fmt.Sprintf("R%06d", collector.rowCounter),
		"message_key":  messageRow["message_key"],
		"message_id":   messageRow["message_id"],
		"date":         messageRow["date"],
		"month":        messageRow["month"],
		"from":         messageRow["from"],
		"ref_kind":     referenceKind,
		"ref_value":    pattern.CutRuneText(referenceValue, 300),
		"ref_label":    referenceLabel,
		"sentence":     pattern.CutRuneText(sentenceText, 1000),
		"context":      pattern.CutRuneText(contextText, 600),
		"source_type":  messageRow["source_type"],
		"is_duplicate": pattern.BoolText(messageRow["duplicate_of"] != ""),
		"duplicate_of": messageRow["duplicate_of"],
	})
}

// Run writes data/references.csv from data/messages.csv and
// data/parts_paragraph.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	paragraphTable, paragraphError := table.ReadTable(filepath.Join(dataFolderPath, "parts_paragraph.csv"))
	if paragraphError != nil {
		return paragraphError
	}
	sourceTypeOfMessage := make(map[string]string, len(paragraphTable.DataRows))
	for _, partRow := range paragraphTable.DataRows {
		sourceTypeOfMessage[partRow["message_key"]] = partRow["source_type"]
	}

	collector := &referenceCollector{table: table.NewTable(kind.ReferenceTableColumnNames)}

	for _, messageRow := range messageTable.DataRows {
		text := messageRow["text"]
		if strings.TrimSpace(text) == "" {
			continue
		}
		messageRow["source_type"] = sourceTypeOfMessage[messageRow["message_key"]]
		contextText := pattern.CutRuneText(text, 600)
		sentenceSpanList := sentence.SentenceSpanListOf(text)

		seenUrlSet := make(map[string]bool, 4)
		for _, match := range pattern.UrlPattern.FindAllStringIndex(text, -1) {
			urlText := pattern.CleanUrl(text[match[0]:match[1]])
			if urlText == "" || seenUrlSet[urlText] {
				continue
			}
			seenUrlSet[urlText] = true
			collector.add(messageRow, sentence.UrlKindOf(urlText), urlText, "",
				sentence.SentenceAround(text, match[0]), contextText)
		}

		arxivIdSet := make(map[string]bool, 2)
		for _, match := range pattern.ArxivUrlPattern.FindAllStringSubmatch(text, -1) {
			arxivIdSet[match[1]] = true
		}
		for _, match := range pattern.ArxivBarePattern.FindAllStringSubmatch(text, -1) {
			arxivIdSet[match[1]] = true
		}
		for _, arxivId := range sortedKeys(arxivIdSet) {
			collector.add(messageRow, "paper", "arxiv:"+arxivId, "arxiv", "", contextText)
		}

		for _, match := range pattern.DoiBarePattern.FindAllStringSubmatch(text, -1) {
			collector.add(messageRow, "paper", "doi:"+match[1], "doi", "", contextText)
		}

		lineList := strings.Split(text, "\n")
		for lineIndex := 0; lineIndex+1 < len(lineList); lineIndex++ {
			titleLine := strings.TrimSpace(lineList[lineIndex])
			authorLine := strings.TrimSpace(lineList[lineIndex+1])
			titleWordCount := pattern.WordCount(titleLine)
			if titleWordCount >= 3 && titleWordCount <= 25 &&
				kind.TitleLinePattern.MatchString(titleLine) && kind.AuthorLinePattern.MatchString(authorLine) {
				collector.add(messageRow, "paper", titleLine, "title", authorLine, contextText)
			}
		}

		lowerText := strings.ToLower(text)
		for _, term := range kind.MethodTermList {
			if refpattern.TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "method", term, "", "", contextText)
			}
		}
		for _, term := range kind.ModelTermList {
			if refpattern.TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "model", term, "", "", contextText)
			}
		}
		for _, term := range kind.DatasetTermList {
			if refpattern.TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "dataset", term, "", "", contextText)
			}
		}
		for _, talkSpan := range refpattern.TalkSpanListOf(lowerText) {
			collector.add(messageRow, "talk", text[talkSpan[0]:talkSpan[1]], "",
				sentence.SentenceAround(text, talkSpan[0]), contextText)
		}

		for _, sentenceSpan := range sentenceSpanList {
			piece := sentenceSpan.Text
			wordCount := pattern.WordCount(piece)
			if wordCount < 4 {
				continue
			}
			if refpattern.ExperimentPattern.MatchString(piece) {
				collector.add(messageRow, "experiment", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
			if refpattern.ReasonPattern.MatchString(piece) {
				collector.add(messageRow, "reason", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
			if refpattern.ClaimPattern.MatchString(piece) {
				collector.add(messageRow, "claim", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
			if refpattern.ResultPattern.MatchString(piece) {
				collector.add(messageRow, "result", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
			if refpattern.SuggestionPattern.MatchString(piece) {
				collector.add(messageRow, "suggestion", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
			if wordCount >= 6 && refpattern.CausalPattern.MatchString(piece) {
				collector.add(messageRow, "causal", pattern.CutRuneText(piece, 200), "", piece, contextText)
			}
		}
	}

	referenceTablePath := filepath.Join(dataFolderPath, "references.csv")
	if writeError := collector.table.WriteTable(referenceTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d references  %s\n", collector.table.RowCount(),
		table.RelativePath(projectFolderPath, referenceTablePath))
	return nil
}
// sortedKeys returns the keys of a set in sorted order.
func sortedKeys(valueSet map[string]bool) []string {
	keyList := make([]string, 0, len(valueSet))
	for key := range valueSet {
		keyList = append(keyList, key)
	}
	sort.Strings(keyList)
	return keyList
}
