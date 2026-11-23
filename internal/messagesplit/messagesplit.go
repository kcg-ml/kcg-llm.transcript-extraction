// Package messagesplit splits every message into parts in 3 modes and writes
// the part tables.
//
// The 3 modes answer the question "what is the unit the index should point
// at":
//
//	unbroken   one part per message; a message over 100 words is chunked
//	sentence   one part per sentence
//	paragraph  one part per paragraph, and a code fence stays whole
//
// Every part also carries a `source_type` that says how the text came into the
// chat, because a pasted abstract and a typed remark are not the same
// evidence.
package messagesplit

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// SplitModeList names the 3 split modes, in the order the tables are written.
var SplitModeList = []string{"unbroken", "sentence", "paragraph"}

// UnbrokenMaxWordCount is the word count above which a message is chunked even
// in `unbroken` mode, because one 4000-character message is not one indexable
// unit.
const UnbrokenMaxWordCount = 100

// SeparatorPattern matches a message that is only a divider the speaker uses.
var SeparatorPattern = regexp.MustCompile(`^[\s\-—–_=*~.]{1,12}$`)

// SentenceEndPattern matches the end of a sentence, with the closing quote or
// bracket that follows it.
var SentenceEndPattern = regexp.MustCompile(`([.!?])["'”’)\]]*\s+`)

// CodeFencePattern matches the line that opens or closes a code fence.
var CodeFencePattern = regexp.MustCompile("^```")

// PartTableColumnNames are the columns of every data/parts_<mode>.csv.
var PartTableColumnNames = []string{
	"part_id", "message_key", "message_id", "date", "month", "from",
	"part_index", "part_count", "split_mode", "source_type", "text",
	"char_len", "word_len", "url_count", "arxiv_count", "has_code",
	"is_duplicate", "duplicate_of",
}

// UrlCountPattern counts the URLs of a part.
var UrlCountPattern = regexp.MustCompile(`https?://`)

// ArxivCountPattern counts the arXiv identifiers of a part.
var ArxivCountPattern = regexp.MustCompile(`arxiv\.org|[0-9]{4}\.[0-9]{4,5}`)

// IndentedCodePattern matches a line that starts with 4 spaces, which the
// speakers use for a code block.
var IndentedCodePattern = regexp.MustCompile(`(?m)^\s{4}\S`)

// SourceTypeOf names how a message text came into the chat.
func SourceTypeOf(messageRow map[string]string) string {
	text := messageRow["text"]
	strippedText := strings.TrimSpace(text)
	if strippedText == "" {
		if messageRow["has_file"] == "1" || messageRow["media_type"] != "" {
			return "media_only"
		}
		return "empty"
	}
	if SeparatorPattern.MatchString(strippedText) {
		return "separator"
	}
	if messageRow["is_forwarded"] == "1" {
		return "forward"
	}
	wordCount := textrule.WordCount(strippedText)
	urlCount := 0
	fmt.Sscanf(messageRow["url_count"], "%d", &urlCount)
	if urlCount > 0 && wordCount <= 12 {
		return "link_only"
	}
	if messageRow["reply_to_message_id"] != "" {
		return "quote"
	}
	if wordCount >= 120 {
		return "paste"
	}
	return "typed"
}

// ParagraphListOf returns the paragraphs of a text, and a code fence stays in
// one paragraph.
func ParagraphListOf(text string) []string {
	paragraphList := make([]string, 0, 8)
	currentLineList := make([]string, 0, 8)
	inCodeFence := false
	for _, line := range strings.Split(text, "\n") {
		if CodeFencePattern.MatchString(strings.TrimSpace(line)) {
			inCodeFence = !inCodeFence
			currentLineList = append(currentLineList, line)
			if !inCodeFence {
				paragraphList = append(paragraphList, strings.Join(currentLineList, "\n"))
				currentLineList = make([]string, 0, 8)
			}
			continue
		}
		if inCodeFence {
			currentLineList = append(currentLineList, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(currentLineList) > 0 {
				paragraphList = append(paragraphList, strings.Join(currentLineList, "\n"))
				currentLineList = make([]string, 0, 8)
			}
			continue
		}
		currentLineList = append(currentLineList, line)
	}
	if len(currentLineList) > 0 {
		paragraphList = append(paragraphList, strings.Join(currentLineList, "\n"))
	}
	trimmedParagraphList := make([]string, 0, len(paragraphList))
	for _, paragraph := range paragraphList {
		trimmedParagraph := strings.TrimSpace(paragraph)
		if trimmedParagraph != "" {
			trimmedParagraphList = append(trimmedParagraphList, trimmedParagraph)
		}
	}
	return trimmedParagraphList
}

// SentenceListOf returns the sentences of a text, and a code block stays whole.
func SentenceListOf(text string) []string {
	sentenceList := make([]string, 0, 16)
	for _, paragraph := range ParagraphListOf(text) {
		if strings.HasPrefix(paragraph, "```") {
			sentenceList = append(sentenceList, paragraph)
			continue
		}
		flatParagraph := strings.Join(strings.Fields(paragraph), " ")
		startIndex := 0
		for _, match := range SentenceEndPattern.FindAllStringIndex(flatParagraph, -1) {
			piece := strings.TrimSpace(flatParagraph[startIndex:match[1]])
			if piece != "" {
				sentenceList = append(sentenceList, piece)
			}
			startIndex = match[1]
		}
		tailPiece := strings.TrimSpace(flatParagraph[startIndex:])
		if tailPiece != "" {
			sentenceList = append(sentenceList, tailPiece)
		}
	}
	return sentenceList
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
		if textrule.WordCount(text) <= UnbrokenMaxWordCount {
			return []string{text}
		}
		return wordChunkListOf(text, UnbrokenMaxWordCount)
	case "sentence":
		sentenceList := SentenceListOf(text)
		if len(sentenceList) == 0 {
			return []string{text}
		}
		return sentenceList
	case "paragraph":
		paragraphList := ParagraphListOf(text)
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
	messageTable, readError := csvtable.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}

	manifestTable := csvtable.NewTable([]string{"split_mode", "part_count",
		"message_count", "unbroken_max_words"})

	for _, splitMode := range SplitModeList {
		partTable := csvtable.NewTable(PartTableColumnNames)
		for _, messageRow := range messageTable.DataRows {
			partTextList := PartTextListOf(messageRow, splitMode)
			sourceType := SourceTypeOf(messageRow)
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
					"word_len":     fmt.Sprintf("%d", textrule.WordCount(partText)),
					"url_count":    fmt.Sprintf("%d", len(UrlCountPattern.FindAllString(partText, -1))),
					"arxiv_count":  fmt.Sprintf("%d", len(ArxivCountPattern.FindAllString(partText, -1))),
					"has_code":     boolText(strings.Contains(partText, "```") || IndentedCodePattern.MatchString(partText)),
					"is_duplicate": boolText(messageRow["duplicate_of"] != ""),
					"duplicate_of": messageRow["duplicate_of"],
				})
			}
		}
		partTablePath := filepath.Join(dataFolderPath, "parts_"+splitMode+".csv")
		if writeError := partTable.WriteTable(partTablePath); writeError != nil {
			return writeError
		}
		fmt.Printf("wrote %6d parts  %s\n", partTable.RowCount(),
			csvtable.RelativePath(projectFolderPath, partTablePath))
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

// boolText writes 1 for true and 0 for false, the shape the tables use.
func boolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
