// Package referenceextraction walks every message and pulls out every
// reference it carries.
//
// A reference is one thing a message points at. The 14 kinds are named in
// ReferenceKindList. The extractor is rule based and runs on every message, so
// the index covers the whole chat; the model stage reads the sentences the
// rules cannot classify.
//
// Every reference row carries the sentence it came from, so a reader can check
// the extraction without opening the chat.
package referenceextraction

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/csvtable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// ReferenceKindList names the 14 kinds of reference the extractor writes.
var ReferenceKindList = []string{
	"paper", "talk", "video", "code", "dataset", "model", "method",
	"experiment", "result", "reason", "causal", "claim", "suggestion", "link",
}

// ReferenceTableColumnNames are the columns of data/references.csv.
var ReferenceTableColumnNames = []string{
	"ref_id", "message_key", "message_id", "date", "month", "from",
	"ref_kind", "ref_value", "ref_label", "sentence", "context",
	"source_type", "is_duplicate", "duplicate_of",
}

// TitleLinePattern matches a title line of a pasted paper blurb: 3 or more
// capitalised words that do not form a sentence.
var TitleLinePattern = regexp.MustCompile(`^[A-Z][\w'’\-:]+(?:\s+(?:[A-Z][\w'’\-:]+|of|for|and|the|in|on|to|a|an))*$`)

// AuthorLinePattern matches the line of comma-separated names below a title.
var AuthorLinePattern = regexp.MustCompile(`^[A-Z][\w'’\-]+(?:\s+[A-Z][\w'’\-]+)+(?:,\s*[A-Z][\w'’\-]+(?:\s+[A-Z][\w'’\-]+)*)+$`)

// MethodTermList names the techniques the 2 speakers name.
var MethodTermList = []string{
	"muon", "lora", "qlora", "svd", "pca", "pruning", "sparsity", "sparse",
	"distillation", "quantization", "int8", "fp8", "fp4", "bf16", "fp16",
	"flash attention", "attention", "self-attention", "moe", "mixture of experts",
	"rope", "alibi", "gqa", "mqa", "kv cache", "speculative decoding",
	"gradient checkpointing", "adamw", "adam", "sgd", "lion", "shampoo",
	"muP", "mu-transfer", "weight decay", "warmup", "cosine schedule",
	"lottery ticket", "linear probing", "fine-tuning", "finetune", "rlhf",
	"dpo", "ppo", "grpo", "sft", "distillation", "pruning", "orthogonalization", "spectral",
	"eigendecomposition", "low-rank", "rank reduction", "batch norm",
	"layer norm", "rmsnorm", "residual", "skip connection",
	"embedding tying", "vocabulary", "tokenizer", "bpe", "sentencepiece",
	"beam search", "sampling", "temperature", "top-k", "top-p",
	"nanogpt", "nanochat", "modded-nanogpt", "transformer", "mamba", "ssm",
	"diffusion", "vae", "gan", "contrastive", "embedding", "retrieval",
	"rag", "chain of thought", "cot", "test-time compute", "scaling law",
}

// DatasetTermList names the datasets and benchmarks the 2 speakers name.
var DatasetTermList = []string{
	"c4", "openwebtext", "the pile", "pile", "redpajama", "dolma", "fineweb",
	"wikitext", "imagenet", "cifar", "mnist", "mmlu", "hellaswag", "arc",
	"gsm8k", "humaneval", "swe-bench", "lambada", "ptb", "enwik8",
	"common crawl", "laion", "shakespeare", "tinystories", "alpaca",
}

// ModelTermList names the models and checkpoints the 2 speakers name.
var ModelTermList = []string{
	"gpt-2", "gpt2", "gpt-3", "gpt-4", "gpt-4o", "gpt-5", "chatgpt",
	"llama", "llama-2", "llama-3", "llama-4", "mistral", "mixtral", "qwen",
	"qwen2", "qwen3", "deepseek", "gemma", "phi-3", "phi-4", "olmo",
	"pythia", "falcon", "yi", "kimi", "glm", "claude", "gemini", "grok",
	"stable diffusion", "sdxl", "flux", "whisper", "clip", "vit", "resnet",
	"bert", "t5", "nanogpt", "nanochat", "modded-nanogpt",
}

// WordBoundaryLeft and WordBoundaryRight mark the 2 ends of a whole word.
//
// A pattern written as `\b(потому|поэтому|значит)\b` matches inside another
// word, because a Cyrillic letter is not a word character to a regular
// expression written without Unicode classes: `значит` then matches inside
// `значительно`. These 2 fragments name the character classes by hand, so the
// match is a whole word in every alphabet.
const (
	WordBoundaryLeft  = `(?:^|[^\p{L}\p{N}_])`
	WordBoundaryRight = `(?:[^\p{L}\p{N}_]|$)`
)

// TalkPattern matches a talk, a conference, a meetup, or a slide deck.
var TalkPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(data ?fest(?:\s+\p{L}+)?(?:\s+\d+)?|conference|meetup|workshop|конференц\p{L}*|митап|воркшоп|доклад|выступлени\p{L}*|слайды)` + WordBoundaryRight)

// ResultPattern matches a sentence that states a number with a unit or a
// comparison.
var ResultPattern = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?\s*(?:%|x|×|times|billion|million|b\b|m\b|gb|mb|tb|tokens?|params?|parameters?|layers?|heads?|dim|dimensions?|seconds?|minutes?|hours?|days?|t/s|tokens/s|fps|ms)\b|\b\d+x\b|\bspeedup\b|\b\d+\s*(?:to|-)\s*\d+x\b)`)

// ExperimentPattern matches a sentence that reports a measurement or a run.
var ExperimentPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(experiment|benchmark|ablation|measured|we ran|i ran|i tested|tested|evaluated|evaluation|results?|speedup|accuracy|perplexity|loss|throughput|tokens per second|t/s|baseline|compare|comparison|эксперимент|бенчмарк|замер|измерил|проверил|результат|точность|лосс|ускорение|сравнени)` + WordBoundaryRight)

// ReasonPattern matches a sentence that gives a cause or a justification.
var ReasonPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(because|since|therefore|thus|hence|so that|the reason|which means|that is why|потому|поэтому|значит|причина|следовательно)` + WordBoundaryRight)

// CausalPattern matches a sentence that names a cause and its effect.
var CausalPattern = regexp.MustCompile(`(?i)(` + WordBoundaryLeft + `(?:causes?|caused by|leads? to|led to|results? in|resulted in|gives? rise to|due to|owing to|thanks to|driven by|because of|as a result|consequently|therefore|thus|hence|so that|which means|that is why|this is why|the reason is)` + WordBoundaryRight + `|` + WordBoundaryLeft + `because` + WordBoundaryRight + `|` + WordBoundaryLeft + `if` + WordBoundaryRight + `[^.]{3,80}` + WordBoundaryLeft + `then` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:приводит|привело|вызывает|из-за|вследствие|поэтому|потому что|следовательно|значит)` + WordBoundaryRight + `)`)

// SuggestionPattern matches a sentence that gives advice.
var SuggestionPattern = regexp.MustCompile(`(?i)(` + WordBoundaryLeft + `you (?:should|must|need to|have to|can|could|might want to|ought to)` + WordBoundaryRight + `|` + WordBoundaryLeft + `if you (?:do|use|try|take|want|go|set|run|add|remove|switch)` + WordBoundaryRight + `|` + WordBoundaryLeft + `i (?:would|'d) (?:suggest|recommend|try|do)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:try|consider|make sure|be sure to|worth trying|better to)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:advise|recommend|suggest(?:ion)?)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:советую|попробуй|стоит|лучше|надо|нужно|если (?:ты|вы))` + WordBoundaryRight + `)`)

// ClaimPattern matches a sentence that states a belief or a hypothesis.
var ClaimPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(i think|i believe|i suspect|in my view|my hypothesis|hypothesis|i claim|it seems|it appears|probably|likely|i'd say|думаю|считаю|гипотеза|идея|утвержда|кажется|вероятно)` + WordBoundaryRight)

// SentenceSpan is one sentence of a message, with its byte span.
type SentenceSpan struct {
	StartIndex int
	EndIndex   int
	Text       string
}

// SentenceSpanListOf returns the sentences of a text, split at a full stop, an
// exclamation mark, a question mark, or a line break.
func SentenceSpanListOf(text string) []SentenceSpan {
	sentenceSpanList := make([]SentenceSpan, 0, 16)
	startIndex := 0
	// A run of `.`, `!`, `?`, and line breaks ends 1 sentence, so `...` does
	// not make 3 sentences of which 2 are empty.
	for position := 0; position < len(text); position++ {
		character := text[position]
		if character != '.' && character != '!' && character != '?' && character != '\n' {
			continue
		}
		for position+1 < len(text) && isSentenceEndCharacter(text[position+1]) {
			position++
		}
		endIndex := position + 1
		piece := strings.TrimSpace(text[startIndex:endIndex])
		if piece != "" {
			sentenceSpanList = append(sentenceSpanList, SentenceSpan{
				StartIndex: startIndex, EndIndex: endIndex, Text: piece})
		}
		startIndex = endIndex
	}
	tailPiece := strings.TrimSpace(text[startIndex:])
	if tailPiece != "" {
		sentenceSpanList = append(sentenceSpanList, SentenceSpan{
			StartIndex: startIndex, EndIndex: len(text), Text: tailPiece})
	}
	return sentenceSpanList
}

// isSentenceEndCharacter reports whether a character ends a sentence.
func isSentenceEndCharacter(character byte) bool {
	return character == '.' || character == '!' || character == '?' || character == '\n'
}

// SentenceAround returns the sentence of a text that holds a byte position.
func SentenceAround(text string, position int) string {
	for _, sentenceSpan := range SentenceSpanListOf(text) {
		if sentenceSpan.StartIndex <= position && position < sentenceSpan.EndIndex {
			return sentenceSpan.Text
		}
	}
	return ""
}

// UrlKindOf names the kind of reference a URL is.
func UrlKindOf(urlText string) string {
	hostName := textrule.UrlHostName(urlText)
	if strings.Contains(hostName, "arxiv.org") || strings.HasPrefix(hostName, "doi.org") {
		return "paper"
	}
	if strings.Contains(hostName, "openreview.net") || strings.Contains(hostName, "proceedings.") ||
		strings.Contains(hostName, "aclanthology") {
		return "paper"
	}
	switch hostName {
	case "github.com", "gitlab.com", "huggingface.co", "codeberg.org":
		return "code"
	case "youtube.com", "m.youtube.com", "youtu.be", "rutube.ru", "vimeo.com":
		return "video"
	}
	return "link"
}

// TermPatternOf returns a pattern that matches a term as a whole word.
//
// The 2 ends are named by character class, not by `\b`, so the match is a whole
// word in Cyrillic and Chinese text too. A hyphen belongs to the word, so
// `nanogpt` does not match inside `modded-nanogpt`.
func TermPatternOf(term string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_-])` + regexp.QuoteMeta(term) +
		`(?:[^\p{L}\p{N}_-]|$)`)
}

// talkSpanListOf returns the span of every talk name in a lower-case text.
//
// The right boundary of TalkPattern is a character that belongs to the next
// word, so a plain FindAll drops the talk that follows another talk in the
// same sentence: `Data Fest 11 conference` yields `data fest 11` and loses
// `conference`. This function scans on from the end of the matched name, so
// the next scan starts at the character the boundary consumed.
func talkSpanListOf(lowerText string) [][2]int {
	talkSpanList := make([][2]int, 0, 4)
	scanStart := 0
	for scanStart < len(lowerText) {
		match := TalkPattern.FindStringSubmatchIndex(lowerText[scanStart:])
		if match == nil {
			break
		}
		talkSpanList = append(talkSpanList, [2]int{
			scanStart + match[2], scanStart + match[3]})
		scanStart += match[3]
	}
	return talkSpanList
}

// referenceCollector accumulates the reference rows of one run.
type referenceCollector struct {
	table      *csvtable.Table
	rowCounter int
}

// add appends one reference row, cutting the long columns to the length the
// table states.
func (collector *referenceCollector) add(messageRow map[string]string, referenceKind string,
	referenceValue string, referenceLabel string, sentenceText string, contextText string) {
	collector.rowCounter++
	collector.table.AppendRow(map[string]string{
		"ref_id":       fmt.Sprintf("R%06d", collector.rowCounter),
		"message_key":  messageRow["message_key"],
		"message_id":   messageRow["message_id"],
		"date":         messageRow["date"],
		"month":        messageRow["month"],
		"from":         messageRow["from"],
		"ref_kind":     referenceKind,
		"ref_value":    cutText(referenceValue, 300),
		"ref_label":    referenceLabel,
		"sentence":     cutText(sentenceText, 1000),
		"context":      cutText(contextText, 600),
		"source_type":  messageRow["source_type"],
		"is_duplicate": boolText(messageRow["duplicate_of"] != ""),
		"duplicate_of": messageRow["duplicate_of"],
	})
}

// Run writes data/references.csv from data/messages.csv and
// data/parts_paragraph.csv.
func Run(projectFolderPath string) error {
	dataFolderPath := filepath.Join(projectFolderPath, "data")
	messageTable, readError := csvtable.ReadTable(filepath.Join(dataFolderPath, "messages.csv"))
	if readError != nil {
		return readError
	}
	paragraphTable, paragraphError := csvtable.ReadTable(filepath.Join(dataFolderPath, "parts_paragraph.csv"))
	if paragraphError != nil {
		return paragraphError
	}
	sourceTypeOfMessage := make(map[string]string, len(paragraphTable.DataRows))
	for _, partRow := range paragraphTable.DataRows {
		sourceTypeOfMessage[partRow["message_key"]] = partRow["source_type"]
	}

	collector := &referenceCollector{table: csvtable.NewTable(ReferenceTableColumnNames)}

	for _, messageRow := range messageTable.DataRows {
		text := messageRow["text"]
		if strings.TrimSpace(text) == "" {
			continue
		}
		messageRow["source_type"] = sourceTypeOfMessage[messageRow["message_key"]]
		contextText := cutText(text, 600)
		sentenceSpanList := SentenceSpanListOf(text)

		// 1. Every URL, with the kind its host implies.
		seenUrlSet := make(map[string]bool, 4)
		for _, match := range textrule.UrlPattern.FindAllStringIndex(text, -1) {
			urlText := textrule.CleanUrl(text[match[0]:match[1]])
			if urlText == "" || seenUrlSet[urlText] {
				continue
			}
			seenUrlSet[urlText] = true
			collector.add(messageRow, UrlKindOf(urlText), urlText, "",
				SentenceAround(text, match[0]), contextText)
		}

		// 2. Every arXiv identifier, whether it came as a URL or as a number.
		arxivIdSet := make(map[string]bool, 2)
		for _, match := range textrule.ArxivUrlPattern.FindAllStringSubmatch(text, -1) {
			arxivIdSet[match[1]] = true
		}
		for _, match := range textrule.ArxivBarePattern.FindAllStringSubmatch(text, -1) {
			arxivIdSet[match[1]] = true
		}
		for _, arxivId := range sortedKeys(arxivIdSet) {
			collector.add(messageRow, "paper", "arxiv:"+arxivId, "arxiv", "", contextText)
		}

		// 3. Every DOI.
		for _, match := range textrule.DoiBarePattern.FindAllStringSubmatch(text, -1) {
			collector.add(messageRow, "paper", "doi:"+match[1], "doi", "", contextText)
		}

		// 4. A pasted paper blurb: a title line followed by an author line.
		lineList := strings.Split(text, "\n")
		for lineIndex := 0; lineIndex+1 < len(lineList); lineIndex++ {
			titleLine := strings.TrimSpace(lineList[lineIndex])
			authorLine := strings.TrimSpace(lineList[lineIndex+1])
			titleWordCount := textrule.WordCount(titleLine)
			if titleWordCount >= 3 && titleWordCount <= 25 &&
				TitleLinePattern.MatchString(titleLine) && AuthorLinePattern.MatchString(authorLine) {
				collector.add(messageRow, "paper", titleLine, "title", authorLine, contextText)
			}
		}

		// 5. Named methods, models, datasets, and talks.
		//
		// A term is matched against the lower-case text, and the row keeps the
		// term as the list spells it. The term itself is lower-cased too, so
		// `muP` matches the text `mup`: the pattern is case-insensitive, but the
		// text it reads is already lower-case.
		lowerText := strings.ToLower(text)
		for _, term := range MethodTermList {
			if TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "method", term, "", "", contextText)
			}
		}
		for _, term := range ModelTermList {
			if TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "model", term, "", "", contextText)
			}
		}
		for _, term := range DatasetTermList {
			if TermPatternOf(strings.ToLower(term)).MatchString(lowerText) {
				collector.add(messageRow, "dataset", term, "", "", contextText)
			}
		}
		// A talk name is read from the lower-case text, so the row keeps the
		// name as the speaker wrote it: the span is taken from `text`.
		for _, talkSpan := range talkSpanListOf(lowerText) {
			collector.add(messageRow, "talk", text[talkSpan[0]:talkSpan[1]], "",
				SentenceAround(text, talkSpan[0]), contextText)
		}

		// 6. Sentences that report an experiment, give a reason, or state a
		// claim, a result, a suggestion, or a cause and its effect.
		for _, sentenceSpan := range sentenceSpanList {
			piece := sentenceSpan.Text
			wordCount := textrule.WordCount(piece)
			if wordCount < 4 {
				continue
			}
			if ExperimentPattern.MatchString(piece) {
				collector.add(messageRow, "experiment", cutText(piece, 200), "", piece, contextText)
			}
			if ReasonPattern.MatchString(piece) {
				collector.add(messageRow, "reason", cutText(piece, 200), "", piece, contextText)
			}
			if ClaimPattern.MatchString(piece) {
				collector.add(messageRow, "claim", cutText(piece, 200), "", piece, contextText)
			}
			if ResultPattern.MatchString(piece) {
				collector.add(messageRow, "result", cutText(piece, 200), "", piece, contextText)
			}
			if SuggestionPattern.MatchString(piece) {
				collector.add(messageRow, "suggestion", cutText(piece, 200), "", piece, contextText)
			}
			if wordCount >= 6 && CausalPattern.MatchString(piece) {
				collector.add(messageRow, "causal", cutText(piece, 200), "", piece, contextText)
			}
		}
	}

	referenceTablePath := filepath.Join(dataFolderPath, "references.csv")
	if writeError := collector.table.WriteTable(referenceTablePath); writeError != nil {
		return writeError
	}
	fmt.Printf("wrote %6d references  %s\n", collector.table.RowCount(),
		csvtable.RelativePath(projectFolderPath, referenceTablePath))
	return nil
}

// cutText returns the first maxLength characters of a text.
func cutText(text string, maxLength int) string {
	return textrule.CutRuneText(text, maxLength)
}

// sortedKeys returns the keys of a set in sorted order.
func sortedKeys(valueSet map[string]bool) []string {
	keyList := make([]string, 0, len(valueSet))
	for key := range valueSet {
		keyList = append(keyList, key)
	}
	sort.Strings(keyList)
	return keyList
}

// boolText writes 1 for true and 0 for false, the shape the tables use.
func boolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
