package prompt

import (
	"fmt"
	"regexp"
	"strings"
)

// TranslationPromptTemplate is the instruction block of one translation
// prompt. The 2 speakers write in Russian and in English, and the rest of the
// pipeline reads English only, so a Cyrillic message is translated once and
// the English text is what every later prompt carries.
const TranslationPromptTemplate = `You translate one message of a Telegram chat about LLM training,
sparsity, pruning, and model architecture. The 2 speakers are ` + "`Synth`" + ` and
` + "`Vlad [russia][ml]`" + `.

Rules:
- Translate every Russian sentence into English. No Russian sentence may stay.
- A message that mixes English and Russian keeps its English sentences as they
  are and translates only the Russian ones.
- Keep every number, unit, and percentage exactly.
- Keep every paper title, arXiv id, DOI, repository name, model name, method
  name, and URL exactly as written. A name is not translated.
- Keep the speaker's tone. Do not summarise, do not shorten, do not explain.
- Keep the line breaks of the original.

Answer with the translation only, no preamble, no quotes, no field label.
Do not repeat the source text. Do not answer with the source text.

----- MESSAGE -----
id: %s
date: %s
from: %s
text:
%s
----- END MESSAGE -----
`

// RenderPrompt returns the prompt text of one message.
func RenderPrompt(messageKey string, dateText string, fromName string, text string) string {
	return fmt.Sprintf(TranslationPromptTemplate, messageKey, dateText, fromName, text)
}

// SourceTextOf returns the message text a translation prompt carries.
func SourceTextOf(promptText string) string {
	markerIndex := strings.Index(promptText, "----- MESSAGE -----")
	if markerIndex < 0 {
		return ""
	}
	afterMarker := promptText[markerIndex+len("----- MESSAGE -----"):]
	textIndex := strings.Index(afterMarker, "text:\n")
	if textIndex < 0 {
		return ""
	}
	sourceText := afterMarker[textIndex+len("text:\n"):]
	if endIndex := strings.Index(sourceText, "----- END MESSAGE -----"); endIndex >= 0 {
		sourceText = sourceText[:endIndex]
	}
	return strings.TrimSpace(sourceText)
}

// EnglishOnlyTextOf returns the part of a message that is already English.
//
// A speaker sometimes writes a sentence in English and then writes the same
// sentence again in Russian. The English half already carries the meaning, so
// the Russian half is dropped and the English half is the translation. The
// function returns an empty text when the message holds no such pair.
func EnglishOnlyTextOf(sourceText string) string {
	paragraphList := strings.Split(sourceText, "\n")
	englishLineList := make([]string, 0, len(paragraphList))
	russianLineCount := 0
	for _, lineText := range paragraphList {
		trimmedLine := strings.TrimSpace(lineText)
		if trimmedLine == "" {
			continue
		}
		if CyrillicLetterPattern.MatchString(trimmedLine) {
			russianLineCount++
			continue
		}
		englishLineList = append(englishLineList, lineText)
	}
	// Both halves must be present, or the message is not a pair.
	if russianLineCount == 0 || len(englishLineList) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(englishLineList, "\n"))
}

// CyrillicLetterPattern matches 1 Cyrillic letter.
var CyrillicLetterPattern = regexp.MustCompile(`\p{Cyrillic}`)

// AnswerTextOf returns the translation an answer carries.
//
// The model sometimes wraps the translation in a field label or in a code
// fence even when the prompt forbids it, so both are removed here. A model
// that answers with an error line returns an empty text, and the caller
// retries the unit.
func AnswerTextOf(answerText string) string {
	lineList := strings.Split(strings.ReplaceAll(answerText, "\r\n", "\n"), "\n")
	// Drop a leading code fence and a leading field label.
	for len(lineList) > 0 {
		firstLine := strings.TrimSpace(lineList[0])
		if firstLine == "" || strings.HasPrefix(firstLine, "```") {
			lineList = lineList[1:]
			continue
		}
		lowerFirstLine := strings.ToLower(firstLine)
		if strings.HasPrefix(lowerFirstLine, "translation:") ||
			strings.HasPrefix(lowerFirstLine, "english:") ||
			strings.HasPrefix(lowerFirstLine, "text_en:") {
			lineList[0] = strings.TrimSpace(firstLine[strings.Index(firstLine, ":")+1:])
			if lineList[0] == "" {
				lineList = lineList[1:]
			}
			continue
		}
		break
	}
	// Drop a trailing code fence.
	for len(lineList) > 0 {
		lastLine := strings.TrimSpace(lineList[len(lineList)-1])
		if lastLine == "" || strings.HasPrefix(lastLine, "```") {
			lineList = lineList[:len(lineList)-1]
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lineList, "\n"))
}