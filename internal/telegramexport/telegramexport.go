// Package telegramexport reads a Telegram chat export and writes the base
// message table and its 6 side tables.
//
// The export is the folder a Telegram desktop export writes: a `result.json`
// file with a `messages` array. One array element is one message, and its
// `text` field is either a string or an array of strings and objects.
package telegramexport

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

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

// exportFile is the part of result.json this stage reads.
type exportFile struct {
	Name     string          `json:"name"`
	Messages []ExportMessage `json:"messages"`
}

// ExportMessage is one message of the export. A field the export leaves out
// keeps its zero value, so an absent `reply_to_message_id` is 0.
type ExportMessage struct {
	Id               int64           `json:"id"`
	Date             string          `json:"date"`
	DateUnixtime     string          `json:"date_unixtime"`
	From             string          `json:"from"`
	FromId           string          `json:"from_id"`
	Text             json.RawMessage `json:"text"`
	TextEntities     []ExportEntity  `json:"text_entities"`
	Edited           string          `json:"edited"`
	ForwardedFrom    string          `json:"forwarded_from"`
	ForwardedFromId  string          `json:"forwarded_from_id"`
	ReplyToMessageId int64           `json:"reply_to_message_id"`
	MediaType        string          `json:"media_type"`
	File             string          `json:"file"`
	FileName         string          `json:"file_name"`
	FileSize         json.Number     `json:"file_size"`
	MimeType         string          `json:"mime_type"`
	DurationSeconds  json.Number     `json:"duration_seconds"`
	Width            json.Number     `json:"width"`
	Height           json.Number     `json:"height"`
	Photo            string          `json:"photo"`
	PhotoFileSize    json.Number     `json:"photo_file_size"`
}

// ExportEntity is one text entity of a message, for example a link or a code
// block.
type ExportEntity struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Href string `json:"href"`
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
	hostName := textrule.UrlHostName(urlText)
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

// keywordHitsOf returns the keywords of a list that a text holds.
func keywordHitsOf(text string, keywordList []string) []string {
	lowerText := strings.ToLower(text)
	hits := make([]string, 0, len(keywordList))
	for _, keyword := range keywordList {
		if strings.Contains(lowerText, keyword) {
			hits = append(hits, keyword)
		}
	}
	return hits
}

// textHashOf returns the first 16 hex characters of the SHA-1 of a text.
func textHashOf(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// sideTables holds the 6 tables that sit beside data/messages.csv.
type sideTables struct {
	linkTable    *csvtable.Table
	entityTable  *csvtable.Table
	mentionTable *csvtable.Table
	replyTable   *csvtable.Table
	forwardTable *csvtable.Table
	mediaTable   *csvtable.Table
}

// newSideTables returns the 6 side tables with their columns.
func newSideTables() *sideTables {
	return &sideTables{
		linkTable: csvtable.NewTable([]string{"message_key", "message_id", "date",
			"from", "url", "host", "url_kind", "label", "sentence"}),
		entityTable: csvtable.NewTable([]string{"message_key", "entity_type",
			"entity_text", "entity_href"}),
		mentionTable: csvtable.NewTable([]string{"message_key", "message_id", "date",
			"from", "mention"}),
		replyTable: csvtable.NewTable([]string{"message_key", "message_id", "date",
			"from", "reply_to_message_id"}),
		forwardTable: csvtable.NewTable([]string{"message_key", "message_id", "date",
			"from", "forwarded_from", "forwarded_from_id"}),
		mediaTable: csvtable.NewTable([]string{"message_key", "message_id", "date",
			"from", "media_type", "file", "file_name", "file_size", "mime_type",
			"duration_seconds", "width", "height"}),
	}
}

// writeAll writes the 6 side tables into the data folder.
func (tables *sideTables) writeAll(dataFolderPath string, projectFolderPath string) error {
	namedTables := []struct {
		fileName string
		table    *csvtable.Table
	}{
		{"links.csv", tables.linkTable},
		{"entities.csv", tables.entityTable},
		{"mentions.csv", tables.mentionTable},
		{"replies.csv", tables.replyTable},
		{"forwarded.csv", tables.forwardTable},
		{"media.csv", tables.mediaTable},
	}
	for _, namedTable := range namedTables {
		filePath := filepath.Join(dataFolderPath, namedTable.fileName)
		if writeError := namedTable.table.WriteTable(filePath); writeError != nil {
			return writeError
		}
		fmt.Printf("wrote %6d rows  %s\n", namedTable.table.RowCount(),
			csvtable.RelativePath(projectFolderPath, filePath))
	}
	return nil
}

// Run reads the text export and writes data/messages.csv and the 6 side
// tables. exportFolderPath is the folder that holds result.json.
func Run(projectFolderPath string, exportFolderPath string) error {
	sourcePath := filepath.Join(exportFolderPath, "result.json")
	fileBytes, readError := os.ReadFile(sourcePath)
	if readError != nil {
		return fmt.Errorf("cannot read %s: %w", sourcePath, readError)
	}
	export := exportFile{}
	if parseError := json.Unmarshal(fileBytes, &export); parseError != nil {
		return fmt.Errorf("cannot parse %s: %w", sourcePath, parseError)
	}

	dataFolderPath := filepath.Join(projectFolderPath, "data")
	if createError := os.MkdirAll(dataFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", dataFolderPath, createError)
	}

	messageTable := csvtable.NewTable(MessageTableColumnNames)
	tables := newSideTables()
	firstKeyOfTextHash := make(map[string]string, len(export.Messages))

	for _, message := range export.Messages {
		text := MessageTextOf(message)
		messageKey := textrule.MessageKeyOf(message.Id, message.Date)

		textHash := textHashOf(text)
		duplicateOf := ""
		if text != "" {
			if firstKey, found := firstKeyOfTextHash[textHash]; found {
				duplicateOf = firstKey
			} else {
				firstKeyOfTextHash[textHash] = messageKey
			}
		}

		entityTypeSet := make(map[string]bool, len(message.TextEntities))
		for _, entity := range message.TextEntities {
			entityTypeSet[entity.Type] = true
		}
		entityTypeList := make([]string, 0, len(entityTypeSet))
		for entityType := range entityTypeSet {
			entityTypeList = append(entityTypeList, entityType)
		}
		sort.Strings(entityTypeList)

		urlList := make([]string, 0, 4)
		for _, match := range textrule.UrlPattern.FindAllStringIndex(text, -1) {
			urlText := textrule.CleanUrl(text[match[0]:match[1]])
			if urlText == "" {
				continue
			}
			urlList = append(urlList, urlText)
			tables.linkTable.AppendRow(map[string]string{
				"message_key": messageKey,
				"message_id":  fmt.Sprintf("%d", message.Id),
				"date":        message.Date,
				"from":        message.From,
				"url":         urlText,
				"host":        textrule.UrlHostName(urlText),
				"url_kind":    UrlKindOf(urlText),
				"label":       labelBefore(text, match[0]),
				"sentence":    SentenceAround(text, match[0], match[1]),
			})
		}

		arxivIdList := make([]string, 0, 2)
		for _, match := range textrule.ArxivUrlPattern.FindAllStringSubmatch(text, -1) {
			arxivIdList = append(arxivIdList, match[1])
		}
		for _, match := range textrule.ArxivBarePattern.FindAllStringSubmatch(text, -1) {
			if !containsText(arxivIdList, match[1]) {
				arxivIdList = append(arxivIdList, match[1])
			}
		}
		doiIdList := make([]string, 0, 2)
		for _, match := range textrule.DoiUrlPattern.FindAllStringSubmatch(text, -1) {
			doiIdList = append(doiIdList, match[1])
		}

		for _, entity := range message.TextEntities {
			entityText := textrule.CutRuneText(entity.Text, 500)
			tables.entityTable.AppendRow(map[string]string{
				"message_key": messageKey,
				"entity_type": entity.Type,
				"entity_text": entityText,
				"entity_href": entity.Href,
			})
			if entity.Type == "mention" {
				tables.mentionTable.AppendRow(map[string]string{
					"message_key": messageKey,
					"message_id":  fmt.Sprintf("%d", message.Id),
					"date":        message.Date,
					"from":        message.From,
					"mention":     entity.Text,
				})
			}
		}

		if message.ReplyToMessageId != 0 {
			tables.replyTable.AppendRow(map[string]string{
				"message_key":         messageKey,
				"message_id":          fmt.Sprintf("%d", message.Id),
				"date":                message.Date,
				"from":                message.From,
				"reply_to_message_id": fmt.Sprintf("%d", message.ReplyToMessageId),
			})
		}

		if message.ForwardedFrom != "" {
			tables.forwardTable.AppendRow(map[string]string{
				"message_key":       messageKey,
				"message_id":        fmt.Sprintf("%d", message.Id),
				"date":              message.Date,
				"from":              message.From,
				"forwarded_from":    message.ForwardedFrom,
				"forwarded_from_id": message.ForwardedFromId,
			})
		}

		if message.MediaType != "" || message.File != "" || message.Photo != "" {
			tables.mediaTable.AppendRow(map[string]string{
				"message_key":      messageKey,
				"message_id":       fmt.Sprintf("%d", message.Id),
				"date":             message.Date,
				"from":             message.From,
				"media_type":       message.MediaType,
				"file":             message.File,
				"file_name":        message.FileName,
				"file_size":        message.FileSize.String(),
				"mime_type":        message.MimeType,
				"duration_seconds": message.DurationSeconds.String(),
				"width":            message.Width.String(),
				"height":           message.Height.String(),
			})
		}

		lineCount := 0
		if text != "" {
			lineCount = strings.Count(text, "\n") + 1
		}
		replyToMessageIdText := ""
		if message.ReplyToMessageId != 0 {
			replyToMessageIdText = fmt.Sprintf("%d", message.ReplyToMessageId)
		}
		messageTable.AppendRow(map[string]string{
			"message_key":         messageKey,
			"message_id":          fmt.Sprintf("%d", message.Id),
			"date":                message.Date,
			"date_unixtime":       message.DateUnixtime,
			"month":               textrule.MonthOf(message.Date),
			"from":                message.From,
			"from_id":             message.FromId,
			"text":                text,
			"char_len":            fmt.Sprintf("%d", utf8.RuneCountInString(text)),
			"word_len":            fmt.Sprintf("%d", textrule.WordCount(text)),
			"line_count":          fmt.Sprintf("%d", lineCount),
			"is_empty":            boolText(strings.TrimSpace(text) == ""),
			"is_edited":           boolText(message.Edited != ""),
			"is_forwarded":        boolText(message.ForwardedFrom != ""),
			"forwarded_from":      message.ForwardedFrom,
			"reply_to_message_id": replyToMessageIdText,
			"media_type":          message.MediaType,
			"has_file":            boolText(message.File != ""),
			"url_count":           fmt.Sprintf("%d", len(urlList)),
			"arxiv_count":         fmt.Sprintf("%d", len(arxivIdList)),
			"doi_count":           fmt.Sprintf("%d", len(doiIdList)),
			"arxiv_ids":           strings.Join(arxivIdList, " "),
			"doi_ids":             strings.Join(doiIdList, " "),
			"entity_types":        strings.Join(entityTypeList, " "),
			"has_code":            boolText(entityTypeSet["code"] || entityTypeSet["pre"]),
			"has_blockquote":      boolText(entityTypeSet["blockquote"]),
			"method_hits":         strings.Join(keywordHitsOf(text, MethodKeywordList), " "),
			"reason_hits":         strings.Join(keywordHitsOf(text, ReasonKeywordList), " "),
			"experiment_hits":     strings.Join(keywordHitsOf(text, ExperimentKeywordList), " "),
			"claim_hits":          strings.Join(keywordHitsOf(text, ClaimKeywordList), " "),
			"text_hash":           textHash,
			"duplicate_of":        duplicateOf,
			"chat_name":           export.Name,
		})
	}

	messageTablePath := filepath.Join(dataFolderPath, "messages.csv")
	if writeError := messageTable.WriteTable(messageTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d rows  %s\n", messageTable.RowCount(),
		csvtable.RelativePath(projectFolderPath, messageTablePath))
	return tables.writeAll(dataFolderPath, projectFolderPath)
}

// labelBefore returns the reference label that sits just before a position,
// for example `paper:` or `код:`.
//
// The label must end the 40-character window, and a word character must not
// sit in front of it: `for papers` yields `papers`, while `OpenReview:` yields
// nothing because `review` starts inside the word `OpenReview`.
func labelBefore(text string, position int) string {
	windowStart := position - 40
	if windowStart < 0 {
		windowStart = 0
	}
	window := strings.ToLower(text[windowStart:position])
	bestLabel := ""
	bestIndex := -1
	for _, labelMatch := range ReferenceLabelPattern.FindAllStringSubmatchIndex(window, -1) {
		if labelMatch[0] <= bestIndex {
			continue
		}
		bestLabel = window[labelMatch[2]:labelMatch[3]]
		bestIndex = labelMatch[0]
	}
	return bestLabel
}

// referenceLabelList names the labels the 2 speakers write in front of a
// reference.
var referenceLabelList = []string{
	"статья", "статьи", "paper", "papers", "препринт", "preprint", "pdf",
	"arxiv", "doi", "код", "code", "repo", "repository", "github", "hf",
	"huggingface", "blog", "пост", "post", "ревью", "review", "видео", "video",
	"слайды", "slides", "тред", "thread", "твит", "tweet",
}

// ReferenceLabelPattern matches a reference label that ends the text it reads,
// with an optional colon after it. The left boundary is a Unicode word
// boundary, so a label that starts inside a longer word does not match.
//
// The right boundary is `\s*[:：]?\s*$` and nothing else. Go's `\b` is
// ASCII-only, so it does not end a Cyrillic label: `статья:` would never match.
var ReferenceLabelPattern = regexp.MustCompile(
	`(?:^|[^\p{L}\p{N}_])(` + strings.Join(referenceLabelList, "|") + `)` +
		`\s*[:：]?\s*$`)

// boolText writes 1 for true and 0 for false, the shape the tables use.
func boolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// containsText reports whether a list holds a text.
func containsText(textList []string, text string) bool {
	for _, listText := range textList {
		if listText == text {
			return true
		}
	}
	return false
}
