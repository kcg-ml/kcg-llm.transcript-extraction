package pattern

import (
	"regexp"
	"strings"
)
// UrlPattern matches one URL. A URL ends at whitespace or at a character that
// closes a sentence.
var UrlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)

// UrlTrailingCharacters are stripped from the end of a matched URL, because a
// sentence puts a full stop or a closing bracket right after the URL.
const UrlTrailingCharacters = ".,;:!?)]}>»\"'"

// ArxivUrlPattern matches an arXiv identifier inside an arXiv URL.
var ArxivUrlPattern = regexp.MustCompile(`arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?`)

// ArxivBarePattern matches an arXiv identifier written as a bare number.
var ArxivBarePattern = regexp.MustCompile(`(?:^|[^\w./])([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?(?:[^\w/]|$)`)

// DoiUrlPattern matches a DOI inside a doi.org URL.
var DoiUrlPattern = regexp.MustCompile(`doi\.org/(10\.[^\s<>"'` + "`" + `]+)`)

// DoiBarePattern matches a DOI written without its URL.
var DoiBarePattern = regexp.MustCompile(`(10\.[0-9]{4,9}/[^\s<>"'` + "`" + `]+)`)

// CleanUrl removes the sentence characters that a URL match picks up.
func CleanUrl(urlText string) string {
	return strings.TrimRight(urlText, UrlTrailingCharacters)
}

// UrlHostName returns the lower-case host of a URL, without the scheme.
func UrlHostName(urlText string) string {
	withoutScheme := strings.TrimPrefix(strings.TrimPrefix(urlText, "https://"), "http://")
	hostName := withoutScheme
	if slashIndex := strings.Index(hostName, "/"); slashIndex >= 0 {
		hostName = hostName[:slashIndex]
	}
	if queryIndex := strings.Index(hostName, "?"); queryIndex >= 0 {
		hostName = hostName[:queryIndex]
	}
	return strings.ToLower(hostName)
}

// WordCount returns the number of whitespace-separated words of a text.
func WordCount(text string) int {
	return len(strings.Fields(text))
}

// MessageKeyOf returns `<message id:06d>-<yymmdd>`, the key of one message.
func MessageKeyOf(messageId int64, dateText string) string {
	yearMonthDay := "000000"
	if len(dateText) >= 10 {
		yearMonthDay = dateText[2:4] + dateText[5:7] + dateText[8:10]
	}
	return padMessageId(messageId) + "-" + yearMonthDay
}

// padMessageId writes the message id with 6 digits, so a key sorts by id.
func padMessageId(messageId int64) string {
	digits := "000000"
	text := ""
	for messageId > 0 {
		text = string(rune('0'+messageId%10)) + text
		messageId /= 10
	}
	if len(text) >= len(digits) {
		return text
	}
	return digits[:len(digits)-len(text)] + text
}

// MonthOf returns the `yyyy-mm` month of a Telegram date text.
func MonthOf(dateText string) string {
	if len(dateText) < 7 {
		return ""
	}
	return dateText[:7]
}

// CutRuneText returns the first maxRuneCount characters of a text, counted as
// characters and not as bytes.
//
// A byte cut splits a Cyrillic or Chinese character in half and writes a file
// that is not valid UTF-8, so every cut goes through this function.
func CutRuneText(text string, maxRuneCount int) string {
	runeList := []rune(text)
	if len(runeList) <= maxRuneCount {
		return text
	}
	return string(runeList[:maxRuneCount])
}

// BoolText writes 1 for true and 0 for false, the shape the tables use.

func BoolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
