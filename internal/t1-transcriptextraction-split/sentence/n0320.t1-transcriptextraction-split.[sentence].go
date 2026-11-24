package sentence

import (
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/paragraph"
)
// SentenceEndPattern matches the end of a sentence, with the closing quote or
// bracket that follows it.
var SentenceEndPattern = regexp.MustCompile(`([.!?])["'”’)\]]*\s+`)

// SentenceListOf returns the sentences of a text, and a code block stays whole.
func SentenceListOf(text string) []string {
	sentenceList := make([]string, 0, 16)
	for _, paragraph := range paragraph.ParagraphListOf(text) {
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
