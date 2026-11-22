#!/usr/bin/env python3
"""Stage 3: walk every message and pull out every reference it carries.

Reads  : data/messages.csv, data/links.csv
Writes : data/references.csv

A reference is one thing a message points at. The extractor names 11 kinds:

  paper       an arXiv id, a DOI, a proceedings or OpenReview URL, or a line
              that names a title and a list of authors
  talk        a conference, meetup, or talk, e.g. `Data Fest Siberia 6`
  video       a YouTube, Rutube, or Vimeo URL
  code        a GitHub, GitLab, Hugging Face, or Codeberg URL
  dataset     a dataset or benchmark name, e.g. `C4`, `MMLU`, `OpenWebText`
  model       a checkpoint or model name, e.g. `nanogpt`, `llama-3`, `qwen3`
  method      a named technique, e.g. `muon`, `LoRA`, `SVD`, `pruning`
  experiment  a sentence that reports a measurement, a run, or a result
  result      a sentence that states a number, a speedup, or a size
  reason      a sentence that gives a cause or a justification
  causal      a sentence that states a cause and its effect, with both parts
  claim       a sentence that states a belief, a hypothesis, or a thesis
  suggestion  a sentence that gives advice: `you should do X`, `if you do X, then Y`
  link        any other URL

The extractor is rule based and runs on every message, so the index covers the
whole chat. Stage 4 sends the same messages to the model, which reads the
sentences the rules cannot classify.

Every reference row carries the sentence it came from, so a reader can check
the extraction without opening the chat.
"""

import csv
import os
import re

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")

URL_RE = re.compile(r"https?://[^\s<>\"'`]+")
URL_TRAILING = ".,;:!?)]}>»\"'"
ARXIV_URL_RE = re.compile(r"arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?")
ARXIV_BARE_RE = re.compile(r"(?<![\w./])([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?(?![\w/])")
DOI_RE = re.compile(r"(10\.[0-9]{4,9}/[^\s<>\"'`]+)")

# A title line in a pasted paper blurb: a line of 3 or more capitalised words
# that is not a sentence, followed by a line of comma-separated names.
TITLE_LINE_RE = re.compile(r"^[A-Z][\w'’\-:]+(?:\s+(?:[A-Z][\w'’\-:]+|of|for|and|the|in|on|to|a|an))*$")
AUTHOR_LINE_RE = re.compile(r"^[A-Z][\w'’\-]+(?:\s+[A-Z][\w'’\-]+)+(?:,\s*[A-Z][\w'’\-]+(?:\s+[A-Z][\w'’\-]+)*)+$")

METHOD_TERMS = [
    "muon", "lora", "qlora", "svd", "pca", "pruning", "sparsity", "sparse",
    "distillation", "quantization", "int8", "fp8", "fp4", "bf16", "fp16",
    "flash attention", "attention", "self-attention", "moe", "mixture of experts",
    "rope", "alibi", "gqa", "mqa", "kv cache", "speculative decoding",
    "gradient checkpointing", "adamw", "adam", "sgd", "lion", "shampoo",
    "muP", "mu-transfer", "weight decay", "warmup", "cosine schedule",
    "lottery ticket", "linear probing", "fine-tuning", "finetune", "rlhf",
    "dpo", "ppo", "grpo", "sft", "distillation", "pruning", "orthogonalization",
    "spectral", "eigendecomposition", "low-rank", "rank reduction",
    "batch norm", "layer norm", "rmsnorm", "residual", "skip connection",
    "embedding tying", "vocabulary", "tokenizer", "bpe", "sentencepiece",
    "beam search", "sampling", "temperature", "top-k", "top-p",
    "nanogpt", "nanochat", "modded-nanogpt", "transformer", "mamba", "ssm",
    "diffusion", "vae", "gan", "contrastive", "embedding", "retrieval",
    "rag", "chain of thought", "cot", "test-time compute", "scaling law",
]

DATASET_TERMS = [
    "c4", "openwebtext", "the pile", "pile", "redpajama", "dolma", "fineweb",
    "wikitext", "imagenet", "cifar", "mnist", "mmlu", "hellaswag", "arc",
    "gsm8k", "humaneval", "swe-bench", "lambada", "ptb", "enwik8",
    "common crawl", "laion", "shakespeare", "tinystories", "alpaca",
]

TALK_RE = re.compile(
    r"(?i)\b(data ?fest(?:\s+\w+)?(?:\s+\d+)?|conference|meetup|workshop|"
    r"конференц\w*|митап|воркшоп|доклад|выступлени\w*|слайды)\b"
)

# A sentence that states a number with a unit or a comparison.
RESULT_RE = re.compile(
    r"(?i)(\d+(?:[.,]\d+)?\s*(?:%|x|×|times|billion|million|b\b|m\b|gb|mb|"
    r"tb|tokens?|params?|parameters?|layers?|heads?|dim|dimensions?|"
    r"seconds?|minutes?|hours?|days?|t/s|tokens/s|fps|ms)\b|"
    r"\b\d+x\b|\bspeedup\b|\b\d+\s*(?:to|-)\s*\d+x\b)"
)

MODEL_TERMS = [
    "gpt-2", "gpt2", "gpt-3", "gpt-4", "gpt-4o", "gpt-5", "chatgpt",
    "llama", "llama-2", "llama-3", "llama-4", "mistral", "mixtral", "qwen",
    "qwen2", "qwen3", "deepseek", "gemma", "phi-3", "phi-4", "olmo",
    "pythia", "falcon", "yi", "kimi", "glm", "claude", "gemini", "grok",
    "stable diffusion", "sdxl", "flux", "whisper", "clip", "vit", "resnet",
    "bert", "t5", "nanogpt", "nanochat", "modded-nanogpt",
]

EXPERIMENT_RE = re.compile(
    r"(?i)\b(experiment|benchmark|ablation|measured|we ran|i ran|i tested|"
    r"tested|evaluated|evaluation|results?|speedup|accuracy|perplexity|"
    r"loss|throughput|tokens per second|t/s|baseline|compare|comparison|"
    r"эксперимент|бенчмарк|замер|измерил|проверил|результат|точность|"
    r"лосс|ускорение|сравнени)\b"
)
REASON_RE = re.compile(
    r"(?i)\b(because|since|therefore|thus|hence|so that|the reason|"
    r"which means|that is why|потому|поэтому|значит|причина|следовательно)\b"
)
# A causal sentence names a cause and an effect. The patterns below are the
# shapes the 2 speakers use; the extractor keeps the whole sentence so the
# reader can see both parts.
CAUSAL_RE = re.compile(
    r"(?i)("
    r"\b(?:causes?|caused by|leads? to|led to|results? in|resulted in|"
    r"gives? rise to|due to|owing to|thanks to|driven by|"
    r"because of|as a result|consequently|therefore|thus|hence|"
    r"so that|which means|that is why|this is why|the reason is)\b"
    r"|\bbecause\b"
    r"|\bif\b[^.]{3,80}\bthen\b"
    r"|\b(?:приводит|привело|вызывает|из-за|вследствие|поэтому|"
    r"потому что|следовательно|значит)\b"
    r")"
)

SUGGESTION_RE = re.compile(
    r"(?i)(\byou (?:should|must|need to|have to|can|could|might want to|ought to)\b|"
    r"\bif you (?:do|use|try|take|want|go|set|run|add|remove|switch)\b|"
    r"\bi (?:would|'d) (?:suggest|recommend|try|do)\b|"
    r"\b(?:try|consider|make sure|be sure to|worth trying|better to)\b|"
    r"\b(?:advise|recommend|suggest(?:ion)?)\b|"
    r"\b(?:советую|попробуй|стоит|лучше|надо|нужно|если (?:ты|вы))\b)"
)

CLAIM_RE = re.compile(
    r"(?i)\b(i think|i believe|i suspect|in my view|my hypothesis|hypothesis|"
    r"i claim|it seems|it appears|probably|likely|i'd say|думаю|считаю|"
    r"гипотеза|идея|утвержда|кажется|вероятно)\b"
)

REFERENCE_FIELDS = [
    "ref_id", "message_key", "message_id", "date", "month", "from",
    "ref_kind", "ref_value", "ref_label", "sentence", "context",
    "source_type", "is_duplicate", "duplicate_of",
]


def clean_url(url):
    while url and url[-1] in URL_TRAILING:
        url = url[:-1]
    return url


def url_kind(url):
    low = url.lower()
    host = re.sub(r"^https?://", "", low).split("/")[0]
    if "arxiv.org" in host or host.startswith("doi.org"):
        return "paper"
    if "openreview.net" in host or "proceedings." in host or "aclanthology" in host:
        return "paper"
    if host in ("github.com", "gitlab.com", "huggingface.co", "codeberg.org"):
        return "code"
    if host in ("youtube.com", "m.youtube.com", "youtu.be", "rutube.ru", "vimeo.com"):
        return "video"
    return "link"


def sentence_around(text, start, end):
    """Return the sentence of `text` that contains the span [start, end)."""
    for s_start, s_end, piece in sentences_of(text):
        if s_start <= start < s_end:
            return piece
    return ""


def sentences_of(text):
    """Return the (start, end, sentence) triples of `text`."""
    out = []
    start = 0
    for match in re.finditer(r"[.!?\n]+", text):
        piece = text[start:match.end()].strip()
        if piece:
            out.append((start, match.end(), piece))
        start = match.end()
    tail = text[start:].strip()
    if tail:
        out.append((start, len(text), tail))
    return out


def main():
    with open(os.path.join(DATA_DIR, "messages.csv"), encoding="utf-8") as f:
        messages = list(csv.DictReader(f))
    with open(os.path.join(DATA_DIR, "parts_paragraph.csv"), encoding="utf-8") as f:
        source_types = {row["message_key"]: row["source_type"]
                        for row in csv.DictReader(f)}

    rows = []
    counter = 0

    def add(msg, kind, value, label, sentence, context):
        nonlocal counter
        counter += 1
        rows.append({
            "ref_id": "R%06d" % counter,
            "message_key": msg["message_key"],
            "message_id": msg["message_id"],
            "date": msg["date"],
            "month": msg["month"],
            "from": msg["from"],
            "ref_kind": kind,
            "ref_value": value[:300],
            "ref_label": label,
            "sentence": sentence[:1000],
            "context": context[:600],
            "source_type": source_types.get(msg["message_key"], ""),
            "is_duplicate": 1 if msg["duplicate_of"] else 0,
            "duplicate_of": msg["duplicate_of"],
        })

    for msg in messages:
        text = msg["text"]
        if not text.strip():
            continue
        sentences = sentences_of(text)

        # 1. Every URL, with the kind its host implies.
        seen_urls = set()
        for match in URL_RE.finditer(text):
            url = clean_url(match.group(0))
            if not url or url in seen_urls:
                continue
            seen_urls.add(url)
            sentence = ""
            for start, end, piece in sentences:
                if start <= match.start() < end:
                    sentence = piece
                    break
            add(msg, url_kind(url), url, "", sentence, text[:600])

        # 2. Every arXiv id, whether it came as a URL or as a bare number.
        arxiv_ids = set()
        for match in ARXIV_URL_RE.finditer(text):
            arxiv_ids.add(match.group(1))
        for match in ARXIV_BARE_RE.finditer(text):
            arxiv_ids.add(match.group(1))
        for arxiv_id in sorted(arxiv_ids):
            add(msg, "paper", "arxiv:" + arxiv_id, "arxiv", "", text[:600])

        # 3. Every DOI.
        for match in DOI_RE.finditer(text):
            add(msg, "paper", "doi:" + match.group(1), "doi", "", text[:600])

        # 4. A pasted paper blurb: a title line followed by an author line.
        lines = [line.strip() for line in text.split("\n")]
        for index in range(len(lines) - 1):
            title = lines[index]
            authors = lines[index + 1]
            if (3 <= len(title.split()) <= 25 and TITLE_LINE_RE.match(title)
                    and AUTHOR_LINE_RE.match(authors)):
                add(msg, "paper", title, "title", authors, text[:600])

        # 5. Named talks, conferences, datasets, methods, and models.
        low = text.lower()
        for term in METHOD_TERMS:
            if re.search(r"(?<![\w-])" + re.escape(term) + r"(?![\w-])", low):
                add(msg, "method", term, "", "", text[:600])
        for term in MODEL_TERMS:
            if re.search(r"(?<![\w-])" + re.escape(term) + r"(?![\w-])", low):
                add(msg, "model", term, "", "", text[:600])
        for term in DATASET_TERMS:
            if re.search(r"(?<![\w-])" + re.escape(term) + r"(?![\w-])", low):
                add(msg, "dataset", term, "", "", text[:600])
        for match in TALK_RE.finditer(text):
            add(msg, "talk", match.group(1), "", sentence_around(text, match.start(),
                                                                 match.end()), text[:600])

        # 6. Sentences that report an experiment, give a reason, or state a claim.
        for start, end, piece in sentences:
            if len(piece.split()) < 4:
                continue
            if EXPERIMENT_RE.search(piece):
                add(msg, "experiment", piece[:200], "", piece, text[:600])
            if REASON_RE.search(piece):
                add(msg, "reason", piece[:200], "", piece, text[:600])
            if CLAIM_RE.search(piece):
                add(msg, "claim", piece[:200], "", piece, text[:600])
            if RESULT_RE.search(piece):
                add(msg, "result", piece[:200], "", piece, text[:600])
            if SUGGESTION_RE.search(piece):
                add(msg, "suggestion", piece[:200], "", piece, text[:600])
            if CAUSAL_RE.search(piece) and len(piece.split()) >= 6:
                add(msg, "causal", piece[:200], "", piece, text[:600])

    path = os.path.join(DATA_DIR, "references.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=REFERENCE_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d references  %s" % (len(rows), os.path.relpath(path, PROJECT_ROOT)))


if __name__ == "__main__":
    main()