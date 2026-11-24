package solo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/vote"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/window"
)
// VoteCount is the number of votes a solo window writes.
const VoteCount = 3

// Run writes one answer file per solo vote of every long message.
func Run(projectFolderPath string, forceRewrite bool) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	referenceTable, referenceError := table.ReadTable(
		filepath.Join(dataFolderPath, "references.csv"))
	if referenceError != nil {
		return referenceError
	}
	linkTable, linkError := table.ReadTable(filepath.Join(dataFolderPath, "links.csv"))
	if linkError != nil {
		return linkError
	}

	referenceRowListOfMessage := make(map[string][]map[string]string, len(referenceTable.DataRows))
	for _, referenceRow := range referenceTable.DataRows {
		messageKey := referenceRow["message_key"]
		referenceRowListOfMessage[messageKey] = append(referenceRowListOfMessage[messageKey], referenceRow)
	}
	linkRowListOfMessage := make(map[string][]map[string]string, len(linkTable.DataRows))
	for _, linkRow := range linkTable.DataRows {
		messageKey := linkRow["message_key"]
		linkRowListOfMessage[messageKey] = append(linkRowListOfMessage[messageKey], linkRow)
	}

	doneFolderPath := filepath.Join(projectFolderPath, "model-answer", "job",
		"relevance-window-vote", "done")
	if createError := os.MkdirAll(doneFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", doneFolderPath, createError)
	}

	longMessageCount := 0
	writtenCount := 0
	keptCount := 0
	yesCount := 0
	noCount := 0
	for _, messageRow := range messageTable.DataRows {
		wordCount, _ := strconv.Atoi(messageRow["word_len"])
		if wordCount <= window.MaxWordCount {
			continue
		}
		longMessageCount++
		messageKey := messageRow["message_key"]
		voteResultList := []vote.VoteResult{
			vote.VoteOnReference(referenceRowListOfMessage[messageKey]),
			vote.VoteOnLink(linkRowListOfMessage[messageKey]),
			vote.VoteOnTopic(messageRow["text"]),
		}
		for voteIndex, voteResult := range voteResultList {
			answerFilePath := filepath.Join(doneFolderPath,
				fmt.Sprintf("relevance-window-vote-of-one-long-message-%s-v%d.txt", messageKey, voteIndex))
			if _, statError := os.Stat(answerFilePath); statError == nil && !forceRewrite {
				keptCount++
				continue
			}
			// A solo window holds 1 message, so its label is `M1 of 1`. The
			// answer uses the same label form as a model answer, so the
			// consensus stage reads both the same way.
			answerText := fmt.Sprintf("M1 of 1 %s %s\n", voteResult.VoteLetter,
				voteResult.ReasonText)
			if writeError := os.WriteFile(answerFilePath, []byte(answerText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", answerFilePath, writeError)
			}
			writtenCount++
			if voteResult.VoteLetter == "Y" {
				yesCount++
			} else {
				noCount++
			}
		}
	}

	fmt.Printf("long messages %d, wrote %d answers, kept %d, Y %d N %d\n",
		longMessageCount, writtenCount, keptCount, yesCount, noCount)
	return nil
}
