// Package relevancewindow builds the overlapping relevance windows and their
// prompts, and decides the relevance of a long message with rules.
//
// One window sees 10 messages and no context, so a short message like
// `because it shows speedup for even dense matrix` looks unrelated. The same
// message inside a window that starts 5 messages earlier reads as part of an
// argument. So every message sits in 3 windows, offset by 0, 5, and 10
// messages, and the consensus stage turns the 3 votes into 1 verdict.
//
// A message longer than MaxWordCount gets its own single-message window in all
// 3 votes, because a long message crowds out its neighbours and its own answer
// is the only one that matters.
package relevancewindow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summarywindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// WindowSize is the message count of one relevance window.
const WindowSize = 10

// VoteOffsetList is the start offset of each vote, in messages.
var VoteOffsetList = []int{0, 5, 10}

// MaxWordCount is the word count above which a message gets its own window.
const MaxWordCount = 150

// PromptCharBudget caps one relevance prompt.
const PromptCharBudget = 24000

// RelevanceWindowTableColumnNames are the columns of
// data/relevance_windows.csv.
var RelevanceWindowTableColumnNames = []string{"rel_window_id", "vote_index",
	"start_index", "end_index", "first_message_key", "last_message_key",
	"message_keys", "message_count", "char_count", "prompt_path", "prompt_chars"}

// RelevancePromptText is the instruction block every relevance prompt starts
// with.
const RelevancePromptText = `You read a Telegram chat between 2 speakers: ` + "`Synth`" + ` and ` + "`Vlad [russia][ml]`" + `.
The chat is about LLM training, sparsity, pruning, model architecture, and the
machine-learning research around them.

Your job is 1 decision per message: is this message relevant to that subject?

Mark ` + "`Y`" + ` when the message carries any of:
  a paper, a talk, a video, a repository, a dataset, a model, a checkpoint
  a method, a technique, an architecture, an optimizer, a kernel
  an experiment, a measurement, a number, a result, a benchmark
  a reason, a causal explanation, a claim about how a model works
  a suggestion about what to try, or what to do if a condition holds

Mark ` + "`N`" + ` when the message carries none of those:
  a greeting, a thanks, a joke, a separator such as ` + "`--`" + `, a bare link with no
  comment, small talk about people, money, travel, food, or politics

Mark ` + "`U`" + ` when the message is too short or too ambiguous to decide, or when it
only makes sense with a message outside this window.

Answer with exactly 1 line per ` + "`###`" + ` message, in the order the messages
appear, and no other text. Each line is:

  M<position> of <total> <Y|N|U> <3 to 12 words of reason>

Each ` + "`###`" + ` line starts with ` + "`M<position> of <total>`" + `. Write 1 line for each
` + "`###`" + ` line, and start the line with that label, for example ` + "`M3 of 10`" + `. The
label is the only key you write: never write a message id, and never write a
line for a label that has no ` + "`###`" + ` line below.

The count of lines you write equals the count of ` + "`###`" + ` lines below. When 1
` + "`###`" + ` line is below, you write 1 line and stop. Do not add a header, do not
add a summary, and do not write a line for a message that has no ` + "`###`" + ` line.
A message long enough to fill the window still gets 1 line.

A message body may hold several paragraphs, a list, or a quoted block. The
whole body is 1 message, so it gets 1 line. Never split a body into 1 line
per paragraph.

Never continue the key sequence past the last ` + "`###`" + ` line. A key that is not
written on a ` + "`###`" + ` line is not a message, so a line for it is a wrong answer.
The last ` + "`###`" + ` line of this prompt is the last message of this prompt.

----- MESSAGES -----
`

// RelevanceWindow is one window of one vote.
type RelevanceWindow struct {
	RelWindowId    string
	VoteIndex      int
	MessageRowList []map[string]string
	IsSolo         bool
}

// BuildWindowList returns the overlapping windows, and the long messages as
// windows of their own.
func BuildWindowList(messageRowList []map[string]string) []RelevanceWindow {
	windowList := make([]RelevanceWindow, 0, len(messageRowList))
	longMessageRowList := make([]map[string]string, 0, 64)
	longMessageKeySet := make(map[string]bool, 64)
	shortMessageRowList := make([]map[string]string, 0, len(messageRowList))
	for _, messageRow := range messageRowList {
		if summarywindow.MessageWordCountOf(messageRow) > MaxWordCount {
			longMessageRowList = append(longMessageRowList, messageRow)
			longMessageKeySet[messageRow["message_key"]] = true
		} else {
			shortMessageRowList = append(shortMessageRowList, messageRow)
		}
	}

	for voteIndex := range VoteOffsetList {
		for _, messageRow := range longMessageRowList {
			windowList = append(windowList, RelevanceWindow{
				RelWindowId:    fmt.Sprintf("rel-solo-%s-v%d", messageRow["message_key"], voteIndex),
				VoteIndex:      voteIndex,
				MessageRowList: []map[string]string{messageRow},
				IsSolo:         true,
			})
		}
	}

	shortCount := len(shortMessageRowList)
	for voteIndex, offset := range VoteOffsetList {
		for startIndex := 0; startIndex < shortCount; startIndex += WindowSize {
			chunkRowList := sliceWithin(shortMessageRowList, startIndex+offset, WindowSize)
			if len(chunkRowList) == 0 {
				// The offset runs past the last message, so the window wraps
				// to the first messages. Without the wrap the first 10
				// messages sit in 1 window only and never get 3 votes.
				chunkRowList = sliceWithin(shortMessageRowList, 0,
					startIndex+offset+WindowSize-shortCount)
			}
			if len(chunkRowList) == 0 {
				break
			}
			windowList = append(windowList, RelevanceWindow{
				RelWindowId:    fmt.Sprintf("rel-%05d-v%d", startIndex/WindowSize, voteIndex),
				VoteIndex:      voteIndex,
				MessageRowList: chunkRowList,
				IsSolo:         false,
			})
		}
	}
	return windowList
}

// sliceWithin returns at most count rows of a list, starting at startIndex.
func sliceWithin(messageRowList []map[string]string, startIndex int, count int) []map[string]string {
	if startIndex >= len(messageRowList) || count <= 0 {
		return nil
	}
	endIndex := startIndex + count
	if endIndex > len(messageRowList) {
		endIndex = len(messageRowList)
	}
	return messageRowList[startIndex:endIndex]
}

// RenderPrompt returns the prompt text of one relevance window.
func RenderPrompt(window RelevanceWindow) string {
	lineList := []string{RelevancePromptText}
	usedCharCount := len(RelevancePromptText)
	totalCount := len(window.MessageRowList)
	for positionIndex, messageRow := range window.MessageRowList {
		// The position tells the model how many messages the window holds and
		// that the keys are not consecutive, so it stops inventing the keys a
		// gap in the id sequence would hold.
		block := fmt.Sprintf("### M%d of %d | %s | %s\n%s\n", positionIndex+1,
			totalCount, messageRow["date"], messageRow["from"], messageRow["text"])
		if usedCharCount+len(block) > PromptCharBudget {
			break
		}
		lineList = append(lineList, block)
		usedCharCount += len(block)
	}
	return strings.Join(lineList, "\n")
}

// Run writes data/relevance_windows.csv and one prompt file per window.
func Run(projectFolderPath string, forceRewrite bool) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	promptFolderPath := filepath.Join(projectFolderPath, "prompts")
	if createError := os.MkdirAll(promptFolderPath, 0o755); createError != nil {
		return fmt.Errorf("cannot create %s: %w", promptFolderPath, createError)
	}
	messageTable, readError := csvtable.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	positionOfMessageKey := make(map[string]int, len(messageTable.DataRows))
	for messageIndex, messageRow := range messageTable.DataRows {
		positionOfMessageKey[messageRow["message_key"]] = messageIndex
	}

	windowTable := csvtable.NewTable(RelevanceWindowTableColumnNames)
	for _, window := range BuildWindowList(messageTable.DataRows) {
		promptText := RenderPrompt(window)
		promptPath := filepath.Join(promptFolderPath, window.RelWindowId+".txt")
		if forceRewrite {
			if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
			}
		} else if _, statError := os.Stat(promptPath); statError != nil {
			if writeError := os.WriteFile(promptPath, []byte(promptText), 0o644); writeError != nil {
				return fmt.Errorf("cannot write %s: %w", promptPath, writeError)
			}
		}
		// The char count is the sum of the `char_len` column, which counts
		// runes, not bytes: a Cyrillic message holds more bytes than runes.
		messageKeyList := make([]string, 0, len(window.MessageRowList))
		charCount := 0
		for _, messageRow := range window.MessageRowList {
			messageKeyList = append(messageKeyList, messageRow["message_key"])
			messageCharCount, parseError := strconv.Atoi(messageRow["char_len"])
			if parseError != nil {
				return fmt.Errorf("message %s holds char_len %q: %w",
					messageRow["message_key"], messageRow["char_len"], parseError)
			}
			charCount += messageCharCount
		}
		windowTable.AppendRow(map[string]string{
			"rel_window_id":     window.RelWindowId,
			"vote_index":        fmt.Sprintf("%d", window.VoteIndex),
			"start_index":       fmt.Sprintf("%d", positionOfMessageKey[window.MessageRowList[0]["message_key"]]),
			"end_index":         fmt.Sprintf("%d", positionOfMessageKey[window.MessageRowList[len(window.MessageRowList)-1]["message_key"]]),
			"first_message_key": window.MessageRowList[0]["message_key"],
			"last_message_key":  window.MessageRowList[len(window.MessageRowList)-1]["message_key"],
			"message_keys":      strings.Join(messageKeyList, " "),
			"message_count":     fmt.Sprintf("%d", len(window.MessageRowList)),
			"char_count":        fmt.Sprintf("%d", charCount),
			"prompt_path":       csvtable.RelativePath(projectFolderPath, promptPath),
			"prompt_chars":      fmt.Sprintf("%d", utf8.RuneCountInString(promptText)),
		})
	}

	windowTablePath := filepath.Join(dataFolderPath, "relevance_windows.csv")
	if writeError := windowTable.WriteTable(windowTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d relevance windows  %s\n", windowTable.RowCount(),
		csvtable.RelativePath(projectFolderPath, windowTablePath))
	fmt.Printf("wrote %6d prompts  prompts/rel-*.txt\n", windowTable.RowCount())
	return nil
}

// RelevantReferenceKindSet names the reference kinds that make a message
// relevant on their own. `claim`, `reason`, and `causal` are left out: the rule
// extractor finds them in any argument, including a long message about
// politics, so they say nothing about the subject of the chat.
var RelevantReferenceKindSet = map[string]bool{
	"paper": true, "talk": true, "video": true, "code": true, "dataset": true,
	"model": true, "method": true, "experiment": true, "result": true,
	"suggestion": true,
}

// RelevantLinkHostList names the hosts whose link is a paper, a repository, or
// a talk.
var RelevantLinkHostList = []string{
	"arxiv.org", "openreview.net", "aclanthology.org", "papers.nips.cc",
	"proceedings.mlr.press", "github.com", "huggingface.co", "kaggle.com",
	"youtube.com", "youtu.be", "rutube.ru", "slideslive.com", "icml.cc",
	"neurips.cc", "iclr.cc", "semanticscholar.org", "paperswithcode.com",
	"openai.com", "deepmind.google", "anthropic.com", "transformer-circuits.pub",
}

// TopicTermList names the terms of the subject, in English and in Russian,
// because the chat is bilingual.
var TopicTermList = []string{
	"llm", "gpt", "transformer", "attention", "sparsity", "sparse",
	"pruning", "prune", "quantization", "quantize", "distillation", "lora",
	"embedding", "gradient", "optimizer", "muon", "adam", "sgd", "backprop",
	"layer", "neuron", "weight", "weights", "checkpoint", "tokenizer",
	"token", "tokens", "dataset", "benchmark", "eval", "evaluation",
	"loss", "perplexity", "grokking", "superposition", "autoencoder",
	"sae", "kernel", "matmul", "flops", "gpu", "h100", "a100", "vram",
	"inference", "training", "finetune", "fine-tune", "pretrain", "nanogpt",
	"mixture of experts", "moe", "routing", "router", "activation",
	"architecture", "parameter", "parameters", "context window", "kv cache",
	"sparsification", "overfitting", "regularization", "batch norm",
	"layernorm", "residual", "softmax", "logits", "entropy", "kl divergence",
	"cross entropy", "learning rate", "scheduler", "warmup", "epoch",
	"batch size", "sparse autoencoder", "circuit", "features",
	"interpretability", "mechanistic", "scaling law", "compute",
	"tokens per second",
	"обучение", "модель", "модели", "веса", "слой", "слои", "токен",
	"токены", "разреженность", "прунинг", "квантование", "градиент",
	"нейросеть", "нейросети", "трансформер", "датасет", "бенчмарк",
	"гроккинг", "эмбеддинг", "архитектура", "параметр", "параметры",
}

// TopicTermPattern matches a subject term as a whole word.
var TopicTermPattern = regexp.MustCompile(`(?i)(?:^|[^\w])` + buildTopicAlternation() + `(?:[^\w]|$)`)

// buildTopicAlternation joins the subject terms into one alternation, longest
// first, so `sparse autoencoder` wins over `sparse`.
func buildTopicAlternation() string {
	termList := append([]string{}, TopicTermList...)
	sort.SliceStable(termList, func(leftIndex int, rightIndex int) bool {
		return len(termList[leftIndex]) > len(termList[rightIndex])
	})
	quotedList := make([]string, 0, len(termList))
	for _, term := range termList {
		quotedList = append(quotedList, regexp.QuoteMeta(term))
	}
	return "(" + strings.Join(quotedList, "|") + ")"
}

// VoteResult is one vote on one message: the letter and its reason.
type VoteResult struct {
	VoteLetter string
	ReasonText string
}

// VoteOnReference decides a long message by the references it carries.
func VoteOnReference(referenceRowList []map[string]string) VoteResult {
	kindSet := make(map[string]bool, len(referenceRowList))
	for _, referenceRow := range referenceRowList {
		if RelevantReferenceKindSet[referenceRow["ref_kind"]] {
			kindSet[referenceRow["ref_kind"]] = true
		}
	}
	if len(kindSet) > 0 {
		kindList := make([]string, 0, len(kindSet))
		for kind := range kindSet {
			kindList = append(kindList, kind)
		}
		sort.Strings(kindList)
		if len(kindList) > 4 {
			kindList = kindList[:4]
		}
		return VoteResult{"Y", "references: " + strings.Join(kindList, ", ")}
	}
	if len(referenceRowList) > 0 {
		return VoteResult{"N", "only link references, no subject reference"}
	}
	return VoteResult{"N", "no reference found"}
}

// VoteOnLink decides a long message by the links it carries.
func VoteOnLink(linkRowList []map[string]string) VoteResult {
	hostSet := make(map[string]bool, len(linkRowList))
	for _, linkRow := range linkRowList {
		hostName := strings.ToLower(linkRow["host"])
		if hostName != "" {
			hostSet[hostName] = true
		}
	}
	relevantHostList := make([]string, 0, len(hostSet))
	for hostName := range hostSet {
		for _, relevantHost := range RelevantLinkHostList {
			if strings.HasSuffix(hostName, relevantHost) {
				relevantHostList = append(relevantHostList, hostName)
				break
			}
		}
	}
	if len(relevantHostList) > 0 {
		sort.Strings(relevantHostList)
		if len(relevantHostList) > 3 {
			relevantHostList = relevantHostList[:3]
		}
		return VoteResult{"Y", "links to " + strings.Join(relevantHostList, ", ")}
	}
	// A repository or a paper page whose own path names a subject term is a
	// subject reference even when its host is not in the list above.
	for _, linkRow := range linkRowList {
		if TopicTermPattern.MatchString(linkRow["url"]) {
			return VoteResult{"Y", "link names a subject term: " + cutText(linkRow["url"], 80)}
		}
	}
	if len(hostSet) > 0 {
		hostList := make([]string, 0, len(hostSet))
		for hostName := range hostSet {
			hostList = append(hostList, hostName)
		}
		sort.Strings(hostList)
		if len(hostList) > 3 {
			hostList = hostList[:3]
		}
		return VoteResult{"N", "links only to " + strings.Join(hostList, ", ")}
	}
	return VoteResult{"N", "no link"}
}

// VoteOnTopic decides a long message by the subject terms it carries.
func VoteOnTopic(text string) VoteResult {
	foundTermList := make([]string, 0, 8)
	seenTermSet := make(map[string]bool, 8)
	for _, match := range TopicTermPattern.FindAllStringSubmatch(text, -1) {
		term := strings.ToLower(match[1])
		if seenTermSet[term] {
			continue
		}
		seenTermSet[term] = true
		foundTermList = append(foundTermList, term)
	}
	if len(foundTermList) >= 2 {
		shownTermList := foundTermList
		if len(shownTermList) > 4 {
			shownTermList = shownTermList[:4]
		}
		return VoteResult{"Y", "terms: " + strings.Join(shownTermList, ", ")}
	}
	if len(foundTermList) == 1 {
		return VoteResult{"N", "1 term only: " + foundTermList[0]}
	}
	return VoteResult{"N", "no subject term"}
}

// cutText returns the first maxLength characters of a text.
func cutText(text string, maxLength int) string {
	return textrule.CutRuneText(text, maxLength)
}
