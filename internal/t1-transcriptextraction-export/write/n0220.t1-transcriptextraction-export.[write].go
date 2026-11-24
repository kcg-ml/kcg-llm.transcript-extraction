package write

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-export/label"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-export/read"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
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
	linkTable    *table.Table
	entityTable  *table.Table
	mentionTable *table.Table
	replyTable   *table.Table
	forwardTable *table.Table
	mediaTable   *table.Table
}

// newSideTables returns the 6 side tables with their columns.
func newSideTables() *sideTables {
	return &sideTables{
		linkTable: table.NewTable([]string{"message_key", "message_id", "date",
			"from", "url", "host", "url_kind", "label", "sentence"}),
		entityTable: table.NewTable([]string{"message_key", "entity_type",
			"entity_text", "entity_href"}),
		mentionTable: table.NewTable([]string{"message_key", "message_id", "date",
			"from", "mention"}),
		replyTable: table.NewTable([]string{"message_key", "message_id", "date",
			"from", "reply_to_message_id"}),
		forwardTable: table.NewTable([]string{"message_key", "message_id", "date",
			"from", "forwarded_from", "forwarded_from_id"}),
		mediaTable: table.NewTable([]string{"message_key", "message_id", "date",
			"from", "media_type", "file", "file_name", "file_size", "mime_type",
			"duration_seconds", "width", "height"}),
	}
}

// writeAll writes the 6 side tables into the data folder.
func (tables *sideTables) writeAll(dataFolderPath string, projectFolderPath string) error {
	namedTables := []struct {
		fileName string
		table    *table.Table
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
			table.RelativePath(projectFolderPath, filePath))
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
	export := read.ExportFile{}
	if parseError := json.Unmarshal(fileBytes, &export); parseError != nil {
		return fmt.Errorf("cannot parse %s: %w", sourcePath, parseError)
	}

	dataFolderPath := filepath.Join(projectFolderPath, "data")
	if createError := os.MkdirAll(dataFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", dataFolderPath, createError)
	}

	messageTable := table.NewTable(read.MessageTableColumnNames)
	tables := newSideTables()
	firstKeyOfTextHash := make(map[string]string, len(export.Messages))

	for _, message := range export.Messages {
		text := read.MessageTextOf(message)
		messageKey := pattern.MessageKeyOf(message.Id, message.Date)

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
		for _, match := range pattern.UrlPattern.FindAllStringIndex(text, -1) {
			urlText := pattern.CleanUrl(text[match[0]:match[1]])
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
				"host":        pattern.UrlHostName(urlText),
				"url_kind":    read.UrlKindOf(urlText),
				"label":       label.LabelBefore(text, match[0]),
				"sentence":    read.SentenceAround(text, match[0], match[1]),
			})
		}

		arxivIdList := make([]string, 0, 2)
		for _, match := range pattern.ArxivUrlPattern.FindAllStringSubmatch(text, -1) {
			arxivIdList = append(arxivIdList, match[1])
		}
		for _, match := range pattern.ArxivBarePattern.FindAllStringSubmatch(text, -1) {
			if !containsText(arxivIdList, match[1]) {
				arxivIdList = append(arxivIdList, match[1])
			}
		}
		doiIdList := make([]string, 0, 2)
		for _, match := range pattern.DoiUrlPattern.FindAllStringSubmatch(text, -1) {
			doiIdList = append(doiIdList, match[1])
		}

		for _, entity := range message.TextEntities {
			entityText := pattern.CutRuneText(entity.Text, 500)
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
			"month":               pattern.MonthOf(message.Date),
			"from":                message.From,
			"from_id":             message.FromId,
			"text":                text,
			"char_len":            fmt.Sprintf("%d", utf8.RuneCountInString(text)),
			"word_len":            fmt.Sprintf("%d", pattern.WordCount(text)),
			"line_count":          fmt.Sprintf("%d", lineCount),
			"is_empty":            pattern.BoolText(strings.TrimSpace(text) == ""),
			"is_edited":           pattern.BoolText(message.Edited != ""),
			"is_forwarded":        pattern.BoolText(message.ForwardedFrom != ""),
			"forwarded_from":      message.ForwardedFrom,
			"reply_to_message_id": replyToMessageIdText,
			"media_type":          message.MediaType,
			"has_file":            pattern.BoolText(message.File != ""),
			"url_count":           fmt.Sprintf("%d", len(urlList)),
			"arxiv_count":         fmt.Sprintf("%d", len(arxivIdList)),
			"doi_count":           fmt.Sprintf("%d", len(doiIdList)),
			"arxiv_ids":           strings.Join(arxivIdList, " "),
			"doi_ids":             strings.Join(doiIdList, " "),
			"entity_types":        strings.Join(entityTypeList, " "),
			"has_code":            pattern.BoolText(entityTypeSet["code"] || entityTypeSet["pre"]),
			"has_blockquote":      pattern.BoolText(entityTypeSet["blockquote"]),
			"method_hits":         strings.Join(keywordHitsOf(text, read.MethodKeywordList), " "),
			"reason_hits":         strings.Join(keywordHitsOf(text, read.ReasonKeywordList), " "),
			"experiment_hits":     strings.Join(keywordHitsOf(text, read.ExperimentKeywordList), " "),
			"claim_hits":          strings.Join(keywordHitsOf(text, read.ClaimKeywordList), " "),
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
		table.RelativePath(projectFolderPath, messageTablePath))
	return tables.writeAll(dataFolderPath, projectFolderPath)
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
