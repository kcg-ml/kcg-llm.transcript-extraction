package answer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
// ErrorPrefixList names the first-line shapes pi writes when it fails. pi
// writes a startup error to stdout and still exits 0, so the answer text is
// checked before it is saved.
var ErrorPrefixList = []string{"Error:", "error:", "Error ", "Cannot ", "Unable to "}

// AnswerErrorOf returns the error line when pi answered with an error instead
// of a result.
func AnswerErrorOf(answerText string) string {
	strippedText := strings.TrimSpace(answerText)
	if strippedText == "" {
		return "empty answer"
	}
	firstLine := strippedText
	if newlineIndex := strings.Index(firstLine, "\n"); newlineIndex >= 0 {
		firstLine = firstLine[:newlineIndex]
	}
	firstLine = strings.TrimSpace(firstLine)
	for _, errorPrefix := range ErrorPrefixList {
		if strings.HasPrefix(firstLine, errorPrefix) {
			return pattern.CutRuneText(firstLine, 500)
		}
	}
	return ""
}

// PositionLabelPattern matches a vote label such as `M3 of 10`.
var PositionLabelPattern = regexp.MustCompile(`(?m)^\s*` + "`?" + `(M[0-9]+ of [0-9]+)` + "`?" + `\s+[YNU]\b`)

// MessageKeyPattern matches a vote line keyed by a message key.
var MessageKeyPattern = regexp.MustCompile(`(?m)^\s*` + "`?" + `([0-9]{6}-[0-9]{6})` + "`?" + `\s+[YNU]\b`)

// PromptPositionLabelPattern matches the label line a prompt gives a message.
var PromptPositionLabelPattern = regexp.MustCompile(`(?m)^### (M[0-9]+ of [0-9]+)`)

// PromptMessageKeyPattern matches the key line a prompt gives a message.
var PromptMessageKeyPattern = regexp.MustCompile(`(?m)^### ([0-9]{6}-[0-9]{6})`)

// answerLabelListOf returns the vote labels an answer holds, in order.
func answerLabelListOf(answerText string) []string {
	labelList := PositionLabelPattern.FindAllStringSubmatch(answerText, -1)
	if len(labelList) > 0 {
		collectedList := make([]string, 0, len(labelList))
		for _, match := range labelList {
			collectedList = append(collectedList, match[1])
		}
		return collectedList
	}
	keyList := MessageKeyPattern.FindAllStringSubmatch(answerText, -1)
	collectedList := make([]string, 0, len(keyList))
	for _, match := range keyList {
		collectedList = append(collectedList, match[1])
	}
	return collectedList
}

// promptLabelListOf returns the labels a prompt gives its messages, in order.
func promptLabelListOf(promptText string) []string {
	labelList := PromptPositionLabelPattern.FindAllStringSubmatch(promptText, -1)
	if len(labelList) > 0 {
		collectedList := make([]string, 0, len(labelList))
		for _, match := range labelList {
			collectedList = append(collectedList, match[1])
		}
		return collectedList
	}
	keyList := PromptMessageKeyPattern.FindAllStringSubmatch(promptText, -1)
	collectedList := make([]string, 0, len(keyList))
	for _, match := range keyList {
		collectedList = append(collectedList, match[1])
	}
	return collectedList
}

// RelevancePromptMarker is a line only a relevance prompt holds, so the answer
// check knows which job it reads.
const RelevancePromptMarker = "Your job is 1 decision per message"

// TranslationPromptMarker is the line that opens the message of a translation
// prompt, so the answer check knows which job it reads.
const TranslationPromptMarker = "----- MESSAGE -----"

// CyrillicLetterPattern matches 1 Cyrillic letter.
var CyrillicLetterPattern = regexp.MustCompile(`\p{Cyrillic}`)

// translationWrongnessOf returns a reason when a translation answer is the
// source text, or when it still holds a Russian sentence.
//
// The model sometimes answers with the message it was given, and sometimes
// translates the first sentence and stops. Both answers are useless to the
// later prompts, so the unit is retried instead of saved.
func translationWrongnessOf(promptText string, answerText string) string {
	sourceText := ""
	if markerIndex := strings.Index(promptText, TranslationPromptMarker); markerIndex >= 0 {
		afterMarker := promptText[markerIndex+len(TranslationPromptMarker):]
		if textIndex := strings.Index(afterMarker, "text:\n"); textIndex >= 0 {
			sourceText = afterMarker[textIndex+len("text:\n"):]
			if endIndex := strings.Index(sourceText, "----- END MESSAGE -----"); endIndex >= 0 {
				sourceText = sourceText[:endIndex]
			}
			sourceText = strings.TrimSpace(sourceText)
		}
	}
	trimmedAnswer := strings.TrimSpace(answerText)
	if sourceText != "" && trimmedAnswer == sourceText {
		// Some messages hold nothing to translate: a 2-letter fragment, a URL
		// whose path is Russian, or a name. The answer is the source, and the
		// source is what the later prompts carry.
		if holdsNothingToTranslate(sourceText) {
			return ""
		}
		return "answer repeats the source text"
	}
	answerCyrillicCount := len(CyrillicLetterPattern.FindAllString(trimmedAnswer, -1))
	if answerCyrillicCount == 0 {
		return ""
	}
	sourceCyrillicCount := len(CyrillicLetterPattern.FindAllString(sourceText, -1))
	// A URL path or a quoted Russian word keeps a few Cyrillic letters. A
	// sentence left untranslated keeps most of them.
	if sourceCyrillicCount > 0 && answerCyrillicCount*2 >= sourceCyrillicCount {
		return fmt.Sprintf("answer keeps %d of %d Cyrillic letters, so a Russian sentence is untranslated",
			answerCyrillicCount, sourceCyrillicCount)
	}
	return ""
}

// UrlPattern matches 1 URL.
var UrlPattern = regexp.MustCompile(`https?://\S+`)

// holdsNothingToTranslate reports whether a message is a fragment, a URL, or a
// name, so no English sentence can replace it.
func holdsNothingToTranslate(sourceText string) bool {
	// A fragment of 3 letters or fewer carries no sentence.
	letterList := regexp.MustCompile(`\p{L}`).FindAllString(sourceText, -1)
	if len(letterList) <= 3 {
		return true
	}
	// A message whose only non-URL text is a name carries no sentence.
	withoutUrlText := strings.TrimSpace(UrlPattern.ReplaceAllString(sourceText, ""))
	if withoutUrlText == "" {
		return true
	}
	// A message of 1 line that names a thing and holds no sentence is a name
	// or a title, so no English sentence can replace it.
	if !strings.Contains(sourceText, "\n") && len(letterList) <= 40 &&
		!strings.ContainsAny(sourceText, ".!?") {
		return true
	}
	// A message whose Cyrillic letters all sit inside a bracket or a quote is
	// an English sentence that quotes a Russian example. A quote that opens on
	// one line and never closes holds no Russian example, so the depth is
	// counted per line and a line with an unclosed quote counts as prose.
	// A Cyrillic letter inside a URL path is part of the address, so the URL
	// is removed before the count.
	cyrillicOutsideQuoteCount := 0
	for _, lineText := range strings.Split(UrlPattern.ReplaceAllString(sourceText, ""), "\n") {
		lineDepthCount := 0
		lineBalanced := true
		for _, letterText := range lineText {
			switch letterText {
			case '[', '«':
				lineDepthCount++
			case ']', '»':
				if lineDepthCount > 0 {
					lineDepthCount--
				}
			case '"':
				lineDepthCount++
			}
			if lineDepthCount == 0 && CyrillicLetterPattern.MatchString(string(letterText)) {
				cyrillicOutsideQuoteCount++
			}
		}
		if lineDepthCount != 0 {
			lineBalanced = false
		}
		_ = lineBalanced
	}
	if cyrillicOutsideQuoteCount == 0 {
		return true
	}
	// A message whose Cyrillic letters are a small part of it is English prose
	// with a Russian name or a Russian gloss, and the English prose is already
	// the translation.
	cyrillicLetterCount := len(CyrillicLetterPattern.FindAllString(sourceText, -1))
	if len(letterList) > 0 && cyrillicLetterCount*8 <= len(letterList) {
		return true
	}
	return false
}

// AnswerWrongnessOf returns a reason when the answer votes on a label its
// prompt does not hold.
//
// A model that invents labels answers a window it was never given, and the
// vote then lands on a message that does not exist. The count check catches the
// same defect from the other side: 1 `###` line must yield 1 vote line.
func AnswerWrongnessOf(promptText string, answerText string) string {
	if strings.Contains(promptText, TranslationPromptMarker) {
		return translationWrongnessOf(promptText, answerText)
	}
	// Only a relevance prompt asks for 1 vote per message. A summary prompt
	// also heads each message with `###`, so the vote check must not read it.
	if !strings.Contains(promptText, RelevancePromptMarker) {
		return ""
	}
	expectedLabelList := promptLabelListOf(promptText)
	if len(expectedLabelList) == 0 {
		return ""
	}
	expectedLabelSet := make(map[string]bool, len(expectedLabelList))
	for _, label := range expectedLabelList {
		expectedLabelSet[label] = true
	}
	gotLabelList := answerLabelListOf(answerText)
	unknownLabelList := make([]string, 0, 4)
	for _, label := range gotLabelList {
		if !expectedLabelSet[label] {
			unknownLabelList = append(unknownLabelList, label)
		}
	}
	if len(unknownLabelList) > 0 {
		shownLabelList := unknownLabelList
		if len(shownLabelList) > 5 {
			shownLabelList = shownLabelList[:5]
		}
		return fmt.Sprintf("answer votes on %d key(s) not in the prompt: %s",
			len(unknownLabelList), strings.Join(shownLabelList, ", "))
	}
	if len(gotLabelList) != len(expectedLabelList) {
		return fmt.Sprintf("answer has %d vote lines, the prompt has %d messages",
			len(gotLabelList), len(expectedLabelList))
	}
	return ""
}
