package part

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/paragraph"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/sentence"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/source"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// SplitModeList names the 3 split modes, in the order the tables are written.
var SplitModeList = []string{"unbroken", "sentence", "paragraph"}

// UnbrokenMaxWordCount is the word count above which a message is chunked even
// in `unbroken` mode, because one 4000-character message is not one indexable
// unit.
const UnbrokenMaxWordCount = 100

// PartTableColumnNames are the columns of every data/parts_<mode>.csv.
var PartTableColumnNames = []string{
	"part_id", "message_key", "message_id", "date", "month", "from",
	"part_index", "part_count", "split_mode", "source_type", "text",
	"char_len", "word_len", "url_count", "arxiv_count", "has_code",
	"is_duplicate", "duplicate_of",
}

// wordChunkListOf breaks a text into chunks of at most maxWordCount words.
func wordChunkListOf(text string, maxWordCount int) []string {
	wordList := strings.Fields(text)
	chunkList := make([]string, 0, len(wordList)/maxWordCount+1)
	for startIndex := 0; startIndex < len(wordList); startIndex += maxWordCount {
		endIndex := startIndex + maxWordCount
		if endIndex > len(wordList) {
			endIndex = len(wordList)
		}
		chunkList = append(chunkList, strings.Join(wordList[startIndex:endIndex], " "))
	}
	return chunkList
}

// PartTextListOf returns the part texts of one message in one split mode.
func PartTextListOf(messageRow map[string]string, splitMode string) []string {
	text := messageRow["text"]
	if strings.TrimSpace(text) == "" {
		return nil
	}
	switch splitMode {
	case "unbroken":
		if pattern.WordCount(text) <= UnbrokenMaxWordCount {
			return []string{text}
		}
		return wordChunkListOf(text, UnbrokenMaxWordCount)
	case "sentence":
		sentenceList := sentence.SentenceListOf(text)
		if len(sentenceList) == 0 {
			return []string{text}
		}
		return sentenceList
	case "paragraph":
		paragraphList := paragraph.ParagraphListOf(text)
		if len(paragraphList) == 0 {
			return []string{text}
		}
		return paragraphList
	}
	return nil
}

// Run writes data/parts_<mode>.csv for each split mode and
// data/parts_manifest.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}

	manifestTable := table.NewTable([]string{"split_mode", "part_count",
		"message_count", "unbroken_max_words"})

	for _, splitMode := range SplitModeList {
		partTable := table.NewTable(PartTableColumnNames)
		for _, messageRow := range messageTable.DataRows {
			partTextList := PartTextListOf(messageRow, splitMode)
			sourceType := source.SourceTypeOf(messageRow)
			for partIndex, partText := range partTextList {
				partTable.AppendRow(map[string]string{
					"part_id":      fmt.Sprintf("%s-P%02d", messageRow["message_key"], partIndex),
					"message_key":  messageRow["message_key"],
					"message_id":   messageRow["message_id"],
					"date":         messageRow["date"],
					"month":        messageRow["month"],
					"from":         messageRow["from"],
					"part_index":   fmt.Sprintf("%d", partIndex),
					"part_count":   fmt.Sprintf("%d", len(partTextList)),
					"split_mode":   splitMode,
					"source_type":  sourceType,
					"text":         partText,
					"char_len":     fmt.Sprintf("%d", utf8.RuneCountInString(partText)),
					"word_len":     fmt.Sprintf("%d", pattern.WordCount(partText)),
					"url_count":    fmt.Sprintf("%d", len(source.UrlCountPattern.FindAllString(partText, -1))),
					"arxiv_count":  fmt.Sprintf("%d", len(source.ArxivCountPattern.FindAllString(partText, -1))),
					"has_code":     pattern.BoolText(strings.Contains(partText, "```") || source.IndentedCodePattern.MatchString(partText)),
					"is_duplicate": pattern.BoolText(messageRow["duplicate_of"] != ""),
					"duplicate_of": messageRow["duplicate_of"],
				})
			}
		}
		partTablePath := filepath.Join(dataFolderPath, "parts_"+splitMode+".csv")
		if writeError := partTable.WriteTable(partTablePath); writeError != nil {
			return writeError
		}
		fmt.Printf("wrote %6d parts  %s\n", partTable.RowCount(),
			table.RelativePath(projectFolderPath, partTablePath))
		manifestTable.AppendRow(map[string]string{
			"split_mode":         splitMode,
			"part_count":         fmt.Sprintf("%d", partTable.RowCount()),
			"message_count":      fmt.Sprintf("%d", messageTable.RowCount()),
			"unbroken_max_words": fmt.Sprintf("%d", UnbrokenMaxWordCount),
		})
	}

	manifestTablePath := filepath.Join(dataFolderPath, "parts_manifest.csv")
	return manifestTable.WriteTable(manifestTablePath)
}
