package source

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/paragraph"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
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
	if paragraph.SeparatorPattern.MatchString(strippedText) {
		return "separator"
	}
	if messageRow["is_forwarded"] == "1" {
		return "forward"
	}
	wordCount := pattern.WordCount(strippedText)
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
