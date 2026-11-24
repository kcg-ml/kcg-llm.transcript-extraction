package read

import (
	"encoding/json"
)
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
