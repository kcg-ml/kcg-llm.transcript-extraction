package vote

import (
	"regexp"
	"sort"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/pattern"
)
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
			return VoteResult{"Y", "link names a subject term: " + pattern.CutRuneText(linkRow["url"], 80)}
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
