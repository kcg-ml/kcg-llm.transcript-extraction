// Package photoextraction reads every photo of the image export and writes one
// prompt per photo.
//
// A photo message carries no text, so the photo is the only content the chat
// carries at that point. The model answer names what the photo shows, the text
// inside it, its topic, its relevance to machine learning, the numbers it
// states, and the terms it shows.
//
// The image export is a second export of the same chat. Its `result.json` may
// still be growing while a download runs, so this stage refuses to read a file
// that does not parse, and says so.
package photoextraction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/telegramexport"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// PhotoTableColumnNames are the columns of data/photos.csv.
var PhotoTableColumnNames = []string{"photo_key", "message_key", "message_id",
	"date", "month", "from", "photo_path", "photo_file_size", "width", "height",
	"caption", "reply_to_message_id", "forwarded_from", "prompt_path",
	"answer_path"}

// PhotoPromptTemplate is the instruction block of one photo prompt. The
// language line matters: a prompt that does not name the answer language gets
// Chinese or Russian field labels, and the photo index then parses no field.
const PhotoPromptTemplate = `You read one photo that a speaker sent in a Telegram chat about LLM training,
sparsity, pruning, and model architecture. The 2 speakers are ` + "`Synth`" + ` and
` + "`Vlad [russia][ml]`" + `.

The message that carried the photo:
  id: %s
  date: %s
  from: %s
  caption: %s
  reply to: %s

Answer in exactly these 6 lines, one value per line, no other text. Write
every value in English, even when the photo holds Russian or Chinese text:

SHOWS: what the photo shows, in 1 sentence (a chart, a slide, a screenshot of code, a table, a photo of a room)
TEXT: every readable string in the photo that matters: the title, the axis labels, the legend, the code line, the table header. Separate them with ` + "` | `" + `. Write ` + "`none`" + ` if the photo holds no readable text.
TOPIC: the LLM or machine-learning topic the photo belongs to, in 3 to 8 words. Write ` + "`not ml`" + ` if the photo is not about machine learning.
RELEVANCE: one of ` + "`high`" + `, ` + "`low`" + `, ` + "`none`" + `, then ` + "` - `" + ` and the reason
NUMBERS: every number the photo states, as ` + "`<what it measures> = <value>`" + `, separated by ` + "` ;; `" + `. Write ` + "`none`" + ` if the photo states no number.
TERMS: the method, model, or paper names the photo shows, comma separated. Write ` + "`none`" + ` if it names none.

Do not guess a number or a name you cannot read. If the photo is unreadable,
write ` + "`unreadable`" + ` on the SHOWS line and ` + "`none`" + ` on the other lines.
`

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

// photoExportFile is the part of the image-export result.json this stage reads.
type photoExportFile struct {
	Messages []photoExportMessage `json:"messages"`
}

// MessageStartPattern finds the start of one message object in a partial
// export file.
var MessageStartPattern = regexp.MustCompile(`\n  \{\n   "id": `)

// LoadExport reads the image export. When the file does not parse, it keeps
// every complete message and drops the last partial one, and reports that the
// export is incomplete.
func LoadExport(sourceFilePath string) (*photoExportFile, bool, error) {
	fileBytes, readError := os.ReadFile(sourceFilePath)
	if readError != nil {
		return nil, false, fmt.Errorf("cannot read %s: %w", sourceFilePath, readError)
	}
	export := photoExportFile{}
	if parseError := json.Unmarshal(fileBytes, &export); parseError == nil {
		return &export, false, nil
	} else {
		fmt.Fprintf(os.Stderr, "cannot read %s as written: %v\n", sourceFilePath, parseError)
	}
	startIndexList := MessageStartPattern.FindAllIndex(fileBytes, -1)
	if len(startIndexList) == 0 {
		return nil, false, fmt.Errorf(
			"the export holds no message object; is the download running?")
	}
	bodyBytes := []byte(strings.TrimRight(string(fileBytes[:startIndexList[len(startIndexList)-1][0]]), " \n\t"))
	if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] == ',' {
		bodyBytes = bodyBytes[:len(bodyBytes)-1]
	}
	salvagedText := string(bodyBytes) + "\n ]\n}\n"
	salvagedExport := photoExportFile{}
	if salvageError := json.Unmarshal([]byte(salvagedText), &salvagedExport); salvageError != nil {
		return nil, false, fmt.Errorf("cannot salvage the export either: %w", salvageError)
	}
	fmt.Fprintf(os.Stderr, "the export is incomplete: kept %d complete messages and dropped "+
		"the last partial one. Run this stage again when the download finishes.\n",
		len(salvagedExport.Messages))
	return &salvagedExport, true, nil
}

// Run writes data/photos.csv and one prompt per photo.
func Run(projectFolderPath string, imageExportFolderName string, forceRewrite bool,
	strictMode bool) error {
	imageExportFolderPath := filepath.Join(projectFolderPath, imageExportFolderName)
	sourceFilePath := filepath.Join(imageExportFolderPath, "result.json")
	export, isSalvaged, loadError := LoadExport(sourceFilePath)
	if loadError != nil {
		return loadError
	}
	if isSalvaged && strictMode {
		return fmt.Errorf("stopped: the export is incomplete and --strict was given")
	}

	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if createError := os.MkdirAll(promptFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", promptFolderPath, createError)
	}

	photoTable := csvtable.NewTable(PhotoTableColumnNames)
	missingFileNameList := make([]string, 0, 4)
	for _, message := range export.Messages {
		if message.Photo == "" {
			continue
		}
		messageKey := textrule.MessageKeyOf(message.Id, message.Date)
		photoKey := "photo-" + messageKey
		photoFilePath := filepath.Join(imageExportFolderPath, message.Photo)
		if _, statError := os.Stat(photoFilePath); statError != nil {
			missingFileNameList = append(missingFileNameList, message.Photo)
			continue
		}
		// The prompt names `none` so the model reads a word, not a gap. The
		// table keeps the empty caption, so a reader can tell a photo with no
		// caption from one whose caption is the word `none`.
		captionText := strings.TrimSpace(telegramexport.MessageTextOf(telegramexport.ExportMessage{
			Text: message.Text}))
		promptCaptionText := captionText
		if promptCaptionText == "" {
			promptCaptionText = "none"
		}
		replyToText := "none"
		if message.ReplyToMessageId != 0 {
			replyToText = fmt.Sprintf("%d", message.ReplyToMessageId)
		}
		promptText := fmt.Sprintf(PhotoPromptTemplate, messageKey, message.Date,
			message.From, promptCaptionText, replyToText)
		promptFilePath := filepath.Join(promptFolderPath, photoKey+".txt")
		if forceRewrite {
			if writeError := os.WriteFile(promptFilePath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptFilePath, writeError)
			}
		} else if _, statError := os.Stat(promptFilePath); statError != nil {
			if writeError := os.WriteFile(promptFilePath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptFilePath, writeError)
			}
		}
		replyToMessageIdText := ""
		if message.ReplyToMessageId != 0 {
			replyToMessageIdText = fmt.Sprintf("%d", message.ReplyToMessageId)
		}
		photoTable.AppendRow(map[string]string{
			"photo_key":           photoKey,
			"message_key":         messageKey,
			"message_id":          fmt.Sprintf("%d", message.Id),
			"date":                message.Date,
			"month":               textrule.MonthOf(message.Date),
			"from":                message.From,
			"photo_path":          csvtable.RelativePath(projectFolderPath, photoFilePath),
			"photo_file_size":     message.PhotoFileSize.String(),
			"width":               message.Width.String(),
			"height":              message.Height.String(),
			"caption":             cutText(captionText, 500),
			"reply_to_message_id": replyToMessageIdText,
			"forwarded_from":      message.ForwardedFrom,
			"prompt_path":         csvtable.RelativePath(projectFolderPath, promptFilePath),
			"answer_path":         "out/" + photoKey + ".txt",
		})
	}

	photoTablePath := filepath.Join(projectFolderPath, "data", "photos.csv")
	if writeError := photoTable.WriteTable(photoTablePath); writeError != nil {
		return writeError
	}
	salvagedNote := ""
	if isSalvaged {
		salvagedNote = "  (from a salvaged, incomplete export)"
	}
	fmt.Printf("wrote %6d photo rows  %s%s\n", photoTable.RowCount(),
		csvtable.RelativePath(projectFolderPath, photoTablePath), salvagedNote)
	if len(missingFileNameList) > 0 {
		fmt.Fprintf(os.Stderr, "%d photos are listed in result.json but not on disk yet, "+
			"for example %s\n", len(missingFileNameList), missingFileNameList[0])
	}
	return nil
}

// cutText returns the first maxLength characters of a text.
func cutText(text string, maxLength int) string {
	return textrule.CutRuneText(text, maxLength)
}
