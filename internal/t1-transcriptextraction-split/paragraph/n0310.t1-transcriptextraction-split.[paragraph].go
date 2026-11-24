package paragraph

import (
	"regexp"
	"strings"
)
// SeparatorPattern matches a message that is only a divider the speaker uses.
var SeparatorPattern = regexp.MustCompile(`^[\s\-—–_=*~.]{1,12}$`)

// CodeFencePattern matches the line that opens or closes a code fence.
var CodeFencePattern = regexp.MustCompile("^```")

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
