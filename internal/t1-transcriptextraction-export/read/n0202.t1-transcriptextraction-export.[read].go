package read

import (
	"encoding/json"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
// ExportEntity is one text entity of a message, for example a link or a code
// block.
type ExportEntity struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Href string `json:"href"`
}

// MessageTableColumnNames are the columns of data/messages.csv, in order.
var MessageTableColumnNames = []string{
	"message_key", "message_id", "date", "date_unixtime", "month", "from",
	"from_id", "text", "char_len", "word_len", "line_count", "is_empty",
	"is_edited", "is_forwarded", "forwarded_from", "reply_to_message_id",
	"media_type", "has_file", "url_count", "arxiv_count", "doi_count",
	"arxiv_ids", "doi_ids", "entity_types", "has_code", "has_blockquote",
	"method_hits", "reason_hits", "experiment_hits", "claim_hits",
	"text_hash", "duplicate_of", "chat_name",
}

// MethodKeywordList names the words that mark a message as carrying a method.
var MethodKeywordList = []string{
	"method", "approach", "algorithm", "technique", "procedure", "pipeline",
	"метод", "подход", "алгоритм", "способ", "техника",
}

// ReasonKeywordList names the words that mark a message as carrying a reason.
var ReasonKeywordList = []string{
	"because", "since", "therefore", "thus", "hence", "so that", "reason",
	"why", "потому", "поэтому", "значит", "причина", "следовательно",
}

// ExperimentKeywordList names the words that mark a message as carrying an
// experiment or a measurement.
var ExperimentKeywordList = []string{
	"experiment", "benchmark", "ablation", "measured", "we ran", "i ran",
	"tested", "evaluated", "results", "speedup", "accuracy", "loss",
	"эксперимент", "бенчмарк", "замер", "измерил", "проверил", "результат",
	"точность", "лосс", "ускорение",
}

// ClaimKeywordList names the words that mark a message as carrying a claim.
var ClaimKeywordList = []string{
	"i think", "i believe", "in my view", "hypothesis", "idea", "claim",
	"думаю", "считаю", "гипотеза", "идея", "утвержд",
}

// MessageTextOf returns the message text as one string, whatever shape the
// `text` field has.
func MessageTextOf(message ExportMessage) string {
	if len(message.Text) == 0 {
		return ""
	}
	plainText := ""
	if json.Unmarshal(message.Text, &plainText) == nil {
		return plainText
	}
	pieceList := make([]json.RawMessage, 0, 4)
	if json.Unmarshal(message.Text, &pieceList) != nil {
		return ""
	}
	textBuilder := strings.Builder{}
	for _, piece := range pieceList {
		pieceText := ""
		if json.Unmarshal(piece, &pieceText) == nil {
			textBuilder.WriteString(pieceText)
			continue
		}
		pieceObject := struct {
			Text string `json:"text"`
		}{}
		if json.Unmarshal(piece, &pieceObject) == nil {
			textBuilder.WriteString(pieceObject.Text)
		}
	}
	return textBuilder.String()
}

// UrlKindOf names the kind of resource a URL points at.
func UrlKindOf(urlText string) string {
	hostName := pattern.UrlHostName(urlText)
	if strings.Contains(hostName, "arxiv.org") {
		return "paper"
	}
	if strings.HasPrefix(hostName, "doi.org") {
		return "paper"
	}
	if hostName == "openreview.net" || strings.Contains(hostName, "proceedings.") ||
		strings.Contains(hostName, "aclanthology") {
		return "paper"
	}
	switch hostName {
	case "github.com", "gitlab.com", "huggingface.co", "codeberg.org":
		return "code"
	case "x.com", "twitter.com":
		return "social"
	case "t.me", "telegram.me":
		return "telegram"
	case "youtube.com", "m.youtube.com", "youtu.be", "rutube.ru", "vimeo.com":
		return "video"
	case "en.wikipedia.org", "ru.wikipedia.org", "wikipedia.org":
		return "wiki"
	case "reddit.com", "news.ycombinator.com", "lobste.rs":
		return "forum"
	case "colab.research.google.com":
		return "notebook"
	}
	if strings.Contains(hostName, "substack.com") || strings.Contains(hostName, "kagi.com") ||
		strings.Contains(hostName, "medium.com") {
		return "review"
	}
	if isAddressHostName(hostName) {
		return "host"
	}
	return "web"
}

// isAddressHostName reports whether a host is a bare address, with an optional
// port.
func isAddressHostName(hostName string) bool {
	if hostName == "" {
		return false
	}
	for _, character := range hostName {
		isDigit := character >= '0' && character <= '9'
		if !isDigit && character != '.' && character != ':' {
			return false
		}
	}
	return true
}

// SentenceAround returns the sentence of a text that holds the span
// [spanStart, spanEnd).
func SentenceAround(text string, spanStart int, spanEnd int) string {
	leftIndex := -1
	for _, separator := range []string{".", "\n", "!", "?"} {
		if foundIndex := strings.LastIndex(text[:spanStart], separator); foundIndex > leftIndex {
			leftIndex = foundIndex
		}
	}
	rightIndex := len(text)
	for _, separator := range []string{".", "\n", "!", "?"} {
		if foundIndex := strings.Index(text[spanEnd:], separator); foundIndex >= 0 {
			candidateIndex := spanEnd + foundIndex
			if candidateIndex < rightIndex {
				rightIndex = candidateIndex
			}
		}
	}
	if rightIndex < len(text) {
		rightIndex++
	}
	return strings.TrimSpace(text[leftIndex+1 : rightIndex])
}
