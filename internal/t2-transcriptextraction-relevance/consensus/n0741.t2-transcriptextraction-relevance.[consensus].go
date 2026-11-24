package consensus

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/table"
)
// voteOfMessage is one vote of one message, with the window it came from.
type voteOfMessage struct {
	VoteIndex  int
	VoteLetter string
	ReasonText string
}

// VoteTableColumnNames are the columns of data/relevance_votes.csv.
var VoteTableColumnNames = []string{"message_key", "rel_window_id", "vote_index",
	"vote", "reason", "date", "month", "from", "word_len"}

// ConsensusTableColumnNames are the columns of data/relevance.csv.
var ConsensusTableColumnNames = []string{"message_key", "date", "month", "from",
	"word_len", "votes_y", "votes_n", "votes_u", "votes_total", "verdict",
	"agreement", "reasons", "text_head"}

// VoteLinePattern matches one vote line of an answer. The label is
// `M<position> of <total>` for a relevance prompt and a message key for an
// older prompt, so an answer written before the label change still counts.
var VoteLinePattern = regexp.MustCompile(`^\s*` + "`?" + `(M[0-9]+ of [0-9]+|[0-9]{6}-[0-9]{6})` + "`?" + `\s*[|:]?\s*([YNUynu])\b\s*(.*)$`)

// PromptLabelPattern matches the label line a prompt gives a message.
var PromptLabelPattern = regexp.MustCompile(`(?m)^### (M[0-9]+ of [0-9]+) \|`)

// PromptKeyPattern matches the key line a prompt gives a message.
var PromptKeyPattern = regexp.MustCompile(`(?m)^### ([0-9]{6}-[0-9]{6})`)

// PromptKeyMapOf returns the map from a prompt label to a message key.
//
// The prompt labels its messages `M<position> of <total>` and holds no message
// key, so the keys come from the `message_keys` column of the window table. A
// prompt that still holds message keys is read from the prompt.
func PromptKeyMapOf(promptText string, windowRow map[string]string) map[string]string {
	keyList := PromptKeyPattern.FindAllStringSubmatch(promptText, -1)
	if len(keyList) > 0 {
		labelList := PromptLabelPattern.FindAllStringSubmatch(promptText, -1)
		keyMap := make(map[string]string, len(keyList))
		if len(labelList) == len(keyList) {
			for index, labelMatch := range labelList {
				keyMap[labelMatch[1]] = keyList[index][1]
			}
			return keyMap
		}
		for _, keyMatch := range keyList {
			keyMap[keyMatch[1]] = keyMatch[1]
		}
		return keyMap
	}
	messageKeyList := strings.Fields(windowRow["message_keys"])
	totalCount := len(messageKeyList)
	keyMap := make(map[string]string, totalCount)
	for positionIndex, messageKey := range messageKeyList {
		keyMap[fmt.Sprintf("M%d of %d", positionIndex+1, totalCount)] = messageKey
	}
	return keyMap
}

// ParseVoteMap returns the vote and reason of every message an answer votes on.
//
// A model that repeats a label gives the same answer twice; the first line
// wins, because the repeat is the model echoing its own output.
func ParseVoteMap(answerText string, keyMap map[string]string) map[string]VoteRecord {
	voteMap := make(map[string]VoteRecord, len(keyMap))
	for _, line := range strings.Split(answerText, "\n") {
		match := VoteLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		messageKey, isKnownLabel := keyMap[match[1]]
		if !isKnownLabel {
			continue
		}
		if _, isAlreadyVoted := voteMap[messageKey]; isAlreadyVoted {
			continue
		}
		voteMap[messageKey] = VoteRecord{
			VoteLetter: strings.ToUpper(match[2]),
			ReasonText: strings.TrimSpace(match[3]),
		}
	}
	return voteMap
}

// VerdictOf returns the verdict and the agreement of one message.
//
// A message sits in 3 windows, and a message near the end of the chat sits in a
// 4th or 5th because the last windows wrap to the first messages. The rule
// reads the share of `Y` votes, so it holds for any count of votes.
func VerdictOf(voteLetterList []string) (string, string) {
	yesCount := 0
	noCount := 0
	unsureCount := 0
	for _, voteLetter := range voteLetterList {
		switch voteLetter {
		case "Y":
			yesCount++
		case "N":
			noCount++
		case "U":
			unsureCount++
		}
	}
	totalCount := yesCount + noCount + unsureCount
	if totalCount < 3 {
		return "UNSURE", "incomplete"
	}
	if yesCount == totalCount {
		return "RELEVANT", "unanimous"
	}
	if noCount == totalCount {
		return "NOT_RELEVANT", "unanimous"
	}
	if yesCount*2 > totalCount {
		if unsureCount == 0 {
			return "RELEVANT", "majority"
		}
		return "RELEVANT", "majority-unsure"
	}
	if noCount*2 > totalCount {
		if yesCount == 0 {
			return "NOT_RELEVANT", "majority"
		}
		return "NOT_RELEVANT", "minority"
	}
	if yesCount > 0 && noCount > 0 {
		return "UNSURE", "split"
	}
	return "UNSURE", "unsure-majority"
}

// answerFilePathOf returns the answer file of one relevance window, from the
// durable job folder first and the flat answer folder second.
func answerFilePathOf(projectFolderPath string, relWindowId string) string {
	candidatePathList := []string{
		filepath.Join(projectFolderPath, "model-answer", "job",
			"relevance-window-vote", "done", relWindowId+".txt"),
		filepath.Join(projectFolderPath, "model-answer", relWindowId+".txt"),
	}
	for _, candidatePath := range candidatePathList {
		if _, statError := os.Stat(candidatePath); statError == nil {
			return candidatePath
		}
	}
	return ""
}

// Run writes data/relevance_votes.csv and data/relevance.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	windowTable, readError := table.ReadTable(
		filepath.Join(dataFolderPath, "relevance_windows.csv"))
	if readError != nil {
		return readError
	}
	messageTable, messageError := table.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if messageError != nil {
		return messageError
	}
	messageRowOfKey := make(map[string]map[string]string, len(messageTable.DataRows))
	for _, messageRow := range messageTable.DataRows {
		messageRowOfKey[messageRow["message_key"]] = messageRow
	}

	voteTable := table.NewTable(VoteTableColumnNames)
	voteListOfMessage := make(map[string][]voteOfMessage, len(messageTable.DataRows))
	missingWindowCount := 0

	for _, windowRow := range windowTable.DataRows {
		answerFilePath := answerFilePathOf(projectFolderPath, windowRow["rel_window_id"])
		if answerFilePath == "" {
			missingWindowCount++
			continue
		}
		promptBytes, promptError := os.ReadFile(filepath.Join(projectFolderPath, windowRow["prompt_path"]))
		if promptError != nil {
			return fmt.Errorf("cannot read prompt %s: %w", windowRow["prompt_path"], promptError)
		}
		answerBytes, answerError := os.ReadFile(answerFilePath)
		if answerError != nil {
			return fmt.Errorf("cannot read answer %s: %w", answerFilePath, answerError)
		}
		keyMap := PromptKeyMapOf(string(promptBytes), windowRow)
		voteMap := ParseVoteMap(string(answerBytes), keyMap)
		voteIndex, _ := strconv.Atoi(windowRow["vote_index"])
		messageKeyList := make([]string, 0, len(voteMap))
		for messageKey := range voteMap {
			messageKeyList = append(messageKeyList, messageKey)
		}
		sort.Strings(messageKeyList)
		for _, messageKey := range messageKeyList {
			vote := voteMap[messageKey]
			messageRow := messageRowOfKey[messageKey]
			voteTable.AppendRow(map[string]string{
				"message_key":   messageKey,
				"rel_window_id": windowRow["rel_window_id"],
				"vote_index":    windowRow["vote_index"],
				"vote":          vote.VoteLetter,
				"reason":        pattern.CutRuneText(vote.ReasonText, 300),
				"date":          messageRow["date"],
				"month":         messageRow["month"],
				"from":          messageRow["from"],
				"word_len":      messageRow["word_len"],
			})
			voteListOfMessage[messageKey] = append(voteListOfMessage[messageKey],
				voteOfMessage{VoteIndex: voteIndex, VoteLetter: vote.VoteLetter,
					ReasonText: vote.ReasonText})
		}
	}

	voteTablePath := filepath.Join(dataFolderPath, "relevance_votes.csv")
	if writeError := voteTable.WriteTable(voteTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d vote rows  %s\n", voteTable.RowCount(),
		table.RelativePath(projectFolderPath, voteTablePath))

	consensusTable := table.NewTable(ConsensusTableColumnNames)
	verdictCountOfName := make(map[string]int, 3)
	agreementCountOfName := make(map[string]int, 8)
	for _, messageRow := range messageTable.DataRows {
		messageKey := messageRow["message_key"]
		messageVoteList := voteListOfMessage[messageKey]
		sort.SliceStable(messageVoteList, func(leftIndex int, rightIndex int) bool {
			return messageVoteList[leftIndex].VoteIndex < messageVoteList[rightIndex].VoteIndex
		})
		voteLetterList := make([]string, 0, len(messageVoteList))
		reasonTextList := make([]string, 0, len(messageVoteList))
		yesCount := 0
		noCount := 0
		unsureCount := 0
		for _, vote := range messageVoteList {
			voteLetterList = append(voteLetterList, vote.VoteLetter)
			reasonTextList = append(reasonTextList,
				fmt.Sprintf("%s:%s", vote.VoteLetter, pattern.CutRuneText(vote.ReasonText, 120)))
			switch vote.VoteLetter {
			case "Y":
				yesCount++
			case "N":
				noCount++
			case "U":
				unsureCount++
			}
		}
		verdictName, agreementName := VerdictOf(voteLetterList)
		verdictCountOfName[verdictName]++
		agreementCountOfName[agreementName]++
		consensusTable.AppendRow(map[string]string{
			"message_key": messageKey,
			"date":        messageRow["date"],
			"month":       messageRow["month"],
			"from":        messageRow["from"],
			"word_len":    messageRow["word_len"],
			"votes_y":     strconv.Itoa(yesCount),
			"votes_n":     strconv.Itoa(noCount),
			"votes_u":     strconv.Itoa(unsureCount),
			"votes_total": strconv.Itoa(len(voteLetterList)),
			"verdict":     verdictName,
			"agreement":   agreementName,
			"reasons":     strings.Join(reasonTextList, " ;; "),
			"text_head":   strings.ReplaceAll(pattern.CutRuneText(messageRow["text"], 200), "\n", " "),
		})
	}

	consensusTablePath := filepath.Join(dataFolderPath, "relevance.csv")
	if writeError := consensusTable.WriteTable(consensusTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d consensus rows  %s\n", consensusTable.RowCount(),
		table.RelativePath(projectFolderPath, consensusTablePath))
	fmt.Printf("verdicts: %v\n", verdictCountOfName)
	fmt.Printf("agreement: %v\n", agreementCountOfName)
	if missingWindowCount > 0 {
		fmt.Fprintf(os.Stderr, "%d relevance windows have no answer yet in out/\n",
			missingWindowCount)
	}
	return nil
}
