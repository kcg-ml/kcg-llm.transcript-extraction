package sentence

import (
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
// SentenceSpan is one sentence of a message, with its byte span.
type SentenceSpan struct {
	StartIndex int
	EndIndex   int
	Text       string
}

// SentenceSpanListOf returns the sentences of a text, split at a full stop, an
// exclamation mark, a question mark, or a line break.
func SentenceSpanListOf(text string) []SentenceSpan {
	sentenceSpanList := make([]SentenceSpan, 0, 16)
	startIndex := 0
	// A run of `.`, `!`, `?`, and line breaks ends 1 sentence, so `...` does
	// not make 3 sentences of which 2 are empty.
	for position := 0; position < len(text); position++ {
		character := text[position]
		if character != '.' && character != '!' && character != '?' && character != '\n' {
			continue
		}
		for position+1 < len(text) && isSentenceEndCharacter(text[position+1]) {
			position++
		}
		endIndex := position + 1
		piece := strings.TrimSpace(text[startIndex:endIndex])
		if piece != "" {
			sentenceSpanList = append(sentenceSpanList, SentenceSpan{
				StartIndex: startIndex, EndIndex: endIndex, Text: piece})
		}
		startIndex = endIndex
	}
	tailPiece := strings.TrimSpace(text[startIndex:])
	if tailPiece != "" {
		sentenceSpanList = append(sentenceSpanList, SentenceSpan{
			StartIndex: startIndex, EndIndex: len(text), Text: tailPiece})
	}
	return sentenceSpanList
}

// isSentenceEndCharacter reports whether a character ends a sentence.
func isSentenceEndCharacter(character byte) bool {
	return character == '.' || character == '!' || character == '?' || character == '\n'
}

// SentenceAround returns the sentence of a text that holds a byte position.
func SentenceAround(text string, position int) string {
	for _, sentenceSpan := range SentenceSpanListOf(text) {
		if sentenceSpan.StartIndex <= position && position < sentenceSpan.EndIndex {
			return sentenceSpan.Text
		}
	}
	return ""
}

// UrlKindOf names the kind of reference a URL is.
func UrlKindOf(urlText string) string {
	hostName := pattern.UrlHostName(urlText)
	if strings.Contains(hostName, "arxiv.org") || strings.HasPrefix(hostName, "doi.org") {
		return "paper"
	}
	if strings.Contains(hostName, "openreview.net") || strings.Contains(hostName, "proceedings.") ||
		strings.Contains(hostName, "aclanthology") {
		return "paper"
	}
	switch hostName {
	case "github.com", "gitlab.com", "huggingface.co", "codeberg.org":
		return "code"
	case "youtube.com", "m.youtube.com", "youtu.be", "rutube.ru", "vimeo.com":
		return "video"
	}
	return "link"
}
