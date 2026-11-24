package kind

import (
	"regexp"
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
