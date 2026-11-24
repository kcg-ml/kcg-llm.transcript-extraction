package table

import (
	"encoding/json"
)
// photoExportMessage is the part of one image-export message this stage reads.
type photoExportMessage struct {
	Id               int64           `json:"id"`
	Date             string          `json:"date"`
	From             string          `json:"from"`
	Text             json.RawMessage `json:"text"`
	Photo            string          `json:"photo"`
	PhotoFileSize    json.Number     `json:"photo_file_size"`
	Width            json.Number     `json:"width"`
	Height           json.Number     `json:"height"`
	ReplyToMessageId int64           `json:"reply_to_message_id"`
	ForwardedFrom    string          `json:"forwarded_from"`
}
