package table

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-photo/photoprompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-export/read"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
)
// photoExportFile is the part of the image-export result.json this stage reads.
type photoExportFile struct {
	Messages []photoExportMessage `json:"messages"`
}

// PhotoTableColumnNames are the columns of data/photos.csv.
var PhotoTableColumnNames = []string{"photo_key", "message_key", "message_id",
	"date", "month", "from", "photo_path", "photo_file_size", "width", "height",
	"caption", "reply_to_message_id", "forwarded_from", "prompt_path",
	"answer_path"}

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

	englishTextByMessage, translationError := translatetable.EnglishTextByMessage(projectFolderPath)
	if translationError != nil {
		return translationError
	}
	photoTable := table.NewTable(PhotoTableColumnNames)
	missingFileNameList := make([]string, 0, 4)
	for _, message := range export.Messages {
		if message.Photo == "" {
			continue
		}
		messageKey := pattern.MessageKeyOf(message.Id, message.Date)
		photoKey := "photo-content-" + messageKey
		photoFilePath := filepath.Join(imageExportFolderPath, message.Photo)
		if _, statError := os.Stat(photoFilePath); statError != nil {
			missingFileNameList = append(missingFileNameList, message.Photo)
			continue
		}
		// The prompt names `none` so the model reads a word, not a gap. The
		// table keeps the empty caption, so a reader can tell a photo with no
		// caption from one whose caption is the word `none`.
		captionText := strings.TrimSpace(read.MessageTextOf(read.ExportMessage{
			Text: message.Text}))
		// A Cyrillic caption is read through its English text, so the model
		// reads one language.
		if englishText, hasEnglish := englishTextByMessage[messageKey]; hasEnglish {
			captionText = englishText
		}
		promptCaptionText := captionText
		if promptCaptionText == "" {
			promptCaptionText = "none"
		}
		replyToText := "none"
		if message.ReplyToMessageId != 0 {
			replyToText = fmt.Sprintf("%d", message.ReplyToMessageId)
		}
		promptText := fmt.Sprintf(photoprompt.PhotoPromptTemplate, messageKey, message.Date,
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
			"month":               pattern.MonthOf(message.Date),
			"from":                message.From,
			"photo_path":          table.RelativePath(projectFolderPath, photoFilePath),
			"photo_file_size":     message.PhotoFileSize.String(),
			"width":               message.Width.String(),
			"height":              message.Height.String(),
			"caption":             pattern.CutRuneText(captionText, 500),
			"reply_to_message_id": replyToMessageIdText,
			"forwarded_from":      message.ForwardedFrom,
			"prompt_path":         table.RelativePath(projectFolderPath, promptFilePath),
			"answer_path":         "model-answer/job/photo-content/done/" + photoKey + ".txt",
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
		table.RelativePath(projectFolderPath, photoTablePath), salvagedNote)
	if len(missingFileNameList) > 0 {
		fmt.Fprintf(os.Stderr, "%d photos are listed in result.json but not on disk yet, "+
			"for example %s\n", len(missingFileNameList), missingFileNameList[0])
	}
	return nil
}
