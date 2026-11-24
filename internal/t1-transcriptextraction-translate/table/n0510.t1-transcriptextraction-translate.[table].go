package table

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/prompt"
)

// TranslationTableColumnNames names the columns of data/translations.csv.
var TranslationTableColumnNames = []string{"message_key", "message_id", "date",
	"from", "text_en", "source_char_len", "english_char_len"}

// CyrillicPattern matches 1 Cyrillic letter. A message that holds one is
// translated, and a message that holds none keeps its own text.
var CyrillicPattern = regexp.MustCompile(`\p{Cyrillic}`)

// HasCyrillicText reports whether a text holds a Cyrillic letter.
func HasCyrillicText(text string) bool {
	return CyrillicPattern.MatchString(text)
}

// PromptFilePathOf returns the prompt file of one message.
func PromptFilePathOf(messageKey string) string {
	return filepath.Join("prompts", "message-translation-"+messageKey+".txt")
}

// AnswerFilePathOf returns the answer file of one message.
//
// The job name is written here rather than read from `chainunit`, because a
// t1 project never imports a t4 project.
func AnswerFilePathOf(projectFolderPath string, messageKey string) string {
	return filepath.Join(projectFolderPath, "model-answer", "job",
		"message-translation", "done", messageKey+".txt")
}

// CyrillicMessageRowList returns the message rows that hold Cyrillic text.
func CyrillicMessageRowList(projectFolderPath string) ([]map[string]string, error) {
	messageTable, readError := table.ReadTable(
		filepath.Join(projectFolderPath, "data", "messages.csv"))
	if readError != nil {
		return nil, readError
	}
	rowList := make([]map[string]string, 0, 256)
	for _, messageRow := range messageTable.DataRows {
		if HasCyrillicText(messageRow["text"]) {
			rowList = append(rowList, messageRow)
		}
	}
	return rowList, nil
}

// WritePromptFileList writes one prompt file per Cyrillic message and returns
// the number written.
func WritePromptFileList(projectFolderPath string) (int, error) {
	rowList, readError := CyrillicMessageRowList(projectFolderPath)
	if readError != nil {
		return 0, readError
	}
	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if makeError := os.MkdirAll(promptFolderPath, 0o755); makeError != nil {
		return 0, makeError
	}
	for _, messageRow := range rowList {
		promptText := prompt.RenderPrompt(messageRow["message_key"],
			messageRow["date"], messageRow["from"], messageRow["text"])
		writeError := os.WriteFile(
			filepath.Join(projectFolderPath, PromptFilePathOf(messageRow["message_key"])),
			[]byte(promptText), 0o644)
		if writeError != nil {
			return 0, writeError
		}
	}
	return len(rowList), nil
}

// BuildTranslationTableList reads the answer of every Cyrillic message and
// returns the rows of data/translations.csv. A message whose answer is missing
// is left out, so a later run of the job fills it in.
func BuildTranslationTableList(projectFolderPath string) ([]map[string]string, int, error) {
	rowList, readError := CyrillicMessageRowList(projectFolderPath)
	if readError != nil {
		return nil, 0, readError
	}
	translationRowList := make([]map[string]string, 0, len(rowList))
	missingCount := 0
	for _, messageRow := range rowList {
		answerBytes, answerError := os.ReadFile(
			AnswerFilePathOf(projectFolderPath, messageRow["message_key"]))
		if answerError != nil {
			missingCount++
			continue
		}
		englishText := prompt.AnswerTextOf(string(answerBytes))
		if englishText == "" {
			missingCount++
			continue
		}
		// A message that holds the same sentence in English and in Russian
		// needs no model answer: the English half is the translation.
		if englishOnlyText := prompt.EnglishOnlyTextOf(messageRow["text"]); englishOnlyText != "" {
			englishText = englishOnlyText
		}
		translationRowList = append(translationRowList, map[string]string{
			"message_key":       messageRow["message_key"],
			"message_id":        messageRow["message_id"],
			"date":              messageRow["date"],
			"from":              messageRow["from"],
			"text_en":           englishText,
			"source_char_len":   messageRow["char_len"],
			"english_char_len":  fmt.Sprintf("%d", utf8.RuneCountInString(englishText)),
		})
	}
	return translationRowList, missingCount, nil
}

// Run writes the translation prompts and data/translations.csv.
func Run(projectFolderPath string) error {
	promptCount, writeError := WritePromptFileList(projectFolderPath)
	if writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d translation prompts  prompts/translate-*.txt\n", promptCount)
	translationRowList, missingCount, buildError := BuildTranslationTableList(projectFolderPath)
	if buildError != nil {
		return buildError
	}
	translationTable := table.NewTable(TranslationTableColumnNames)
	for _, translationRow := range translationRowList {
		translationTable.AppendRow(translationRow)
	}
	writeTableError := translationTable.WriteTable(
		filepath.Join(projectFolderPath, "data", "translations.csv"))
	if writeTableError != nil {
		return writeTableError
	}
	fmt.Printf("wrote %6d translations  data/translations.csv (%d Cyrillic messages still unanswered)\n",
		translationTable.RowCount(), missingCount)
	return nil
}

// EnglishTextByMessage returns the English text of every translated message,
// keyed by message key.
func EnglishTextByMessage(projectFolderPath string) (map[string]string, error) {
	translationTable, readError := table.ReadTableIfPresent(
		filepath.Join(projectFolderPath, "data", "translations.csv"),
		TranslationTableColumnNames)
	if readError != nil {
		return nil, readError
	}
	englishTextByMessage := make(map[string]string, translationTable.RowCount())
	for _, translationRow := range translationTable.DataRows {
		englishText := translationRow["text_en"]
		if strings.TrimSpace(englishText) == "" {
			continue
		}
		englishTextByMessage[translationRow["message_key"]] = englishText
	}
	return englishTextByMessage, nil
}