#!/usr/bin/env python3
"""Decide the relevance of a long message with rules, not with a model.

Why this exists: a message longer than 150 words gets its own window, so the
window holds 1 message and 1 decision. The model then answers that window by
inventing the next message keys, because a prompt with 1 `###` line reads as
the start of a sequence. The answer is wrong in a way no prompt wording fixed:
9 of 3961 windows failed on it, and 66 of the first 250 answers voted on keys
that do not exist.

A long message also carries the most material of any message, so a rule pass
over it is more reliable than 1 model call: the rules read the whole body and
name what they found.

Reads  : data/messages.csv, data/references.csv, data/links.csv
Writes : out/jobs/rel/done/rel-solo-<message_key>-v<vote>.txt
         each file holds 1 line: `M1 of 1 <Y|N|U> <reason>`

The 3 votes of a solo window are the 3 rules below, so the consensus stage
reads a solo message the same way it reads a model answer:

  vote 0  the reference rule   a reference of a relevant kind is present
  vote 1  the link rule        a paper, repository, or talk link is present
  vote 2  the topic rule       a machine-learning term is present

A message that 3 rules mark `Y` is relevant with no doubt. A message that 1
rule marks `Y` and 2 mark `N` is probably a long personal story that mentions
1 term in passing, and the consensus stage reports it as `minority`.
"""

import argparse
import csv
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
DONE_DIR = os.path.join(PROJECT_ROOT, "out", "jobs", "rel", "done")

MAX_WORDS = 150
VOTES = 3

# A reference of one of these kinds makes a message relevant on its own.
# `claim`, `reason`, and `causal` are left out: the rule extractor finds them
# in any argument, including a long message about politics, so they say
# nothing about the subject of the chat.
RELEVANT_REF_KINDS = {
    "paper", "talk", "video", "code", "dataset", "model",
    "method", "experiment", "result", "suggestion",
}

# A link of one of these hosts is a paper, a repository, or a talk.
RELEVANT_LINK_HOSTS = (
    "arxiv.org", "openreview.net", "aclanthology.org", "papers.nips.cc",
    "proceedings.mlr.press", "github.com", "huggingface.co", "kaggle.com",
    "youtube.com", "youtu.be", "rutube.ru", "slideslive.com", "icml.cc",
    "neurips.cc", "iclr.cc", "semanticscholar.org", "paperswithcode.com",
    "openai.com", "deepmind.google", "anthropic.com", "transformer-circuits.pub",
)

# A term of the subject, matched as a whole word and case-insensitively.
TOPIC_TERMS = (
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
    "grokking", "sparsification", "pruning", "distillation", "quantization",
    "overfitting", "regularization", "batch norm", "layernorm", "residual",
    "softmax", "logits", "entropy", "kl divergence", "cross entropy",
    "learning rate", "scheduler", "warmup", "epoch", "batch size",
    "sparse autoencoder", "circuit", "features", "interpretability",
    "mechanistic", "scaling law", "compute", "tokens per second",
    # Russian terms of the same subject, because the chat is bilingual.
    "обучение", "модель", "модели", "веса", "слой", "слои", "токен",
    "токены", "разреженность", "прунинг", "квантование", "градиент",
    "нейросеть", "нейросети", "трансформер", "датасет", "бенчмарк",
    "гроккинг", "эмбеддинг", "архитектура", "параметр", "параметры",
)

TOPIC_RE = re.compile(r"\b(" + "|".join(re.escape(t) for t in TOPIC_TERMS) + r")\b",
                      re.IGNORECASE)


def read_csv(name):
    path = os.path.join(DATA_DIR, name)
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return list(csv.DictReader(f))


def host_of(url):
    match = re.match(r"https?://([^/]+)/", url + "/")
    return match.group(1).lower() if match else ""


def vote_reference(refs):
    """Vote 0: does a reference of a relevant kind sit in this message?"""
    kinds = sorted({r["ref_kind"] for r in refs if r["ref_kind"] in RELEVANT_REF_KINDS})
    if kinds:
        return "Y", "references: " + ", ".join(kinds[:4])
    if refs:
        return "N", "only link references, no subject reference"
    return "N", "no reference found"


def vote_link(links):
    """Vote 1: does a paper, repository, or talk link sit in this message?"""
    hosts = sorted({host_of(l["url"]) for l in links if host_of(l["url"])})
    relevant = [h for h in hosts if any(h.endswith(r) for r in RELEVANT_LINK_HOSTS)]
    if relevant:
        return "Y", "links to " + ", ".join(relevant[:3])
    # A repository or a paper page whose own path names a subject term is a
    # subject reference even when the host is not in the list above.
    for link in links:
        if TOPIC_RE.search(link["url"]):
            return "Y", "link names a subject term: " + link["url"][:80]
    if hosts:
        return "N", "links only to " + ", ".join(hosts[:3])
    return "N", "no link"


def vote_topic(text):
    """Vote 2: does a term of the subject sit in this message?"""
    found = []
    seen = set()
    for match in TOPIC_RE.finditer(text):
        term = match.group(1).lower()
        if term not in seen:
            seen.add(term)
            found.append(term)
    if len(found) >= 2:
        return "Y", "terms: " + ", ".join(found[:4])
    if found:
        return "N", "1 term only: " + found[0]
    return "N", "no subject term"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--force", action="store_true",
                        help="overwrite an answer that already exists")
    args = parser.parse_args()

    messages = read_csv("messages.csv")
    references = read_csv("references.csv")
    links = read_csv("links.csv")

    refs_by_key = {}
    for ref in references:
        refs_by_key.setdefault(ref["message_key"], []).append(ref)
    links_by_key = {}
    for link in links:
        links_by_key.setdefault(link["message_key"], []).append(link)

    long_messages = [m for m in messages if int(m["word_len"]) > MAX_WORDS]
    os.makedirs(DONE_DIR, exist_ok=True)

    written = 0
    skipped = 0
    tally = {"Y": 0, "N": 0}
    for msg in long_messages:
        key = msg["message_key"]
        refs = refs_by_key.get(key, [])
        msg_links = links_by_key.get(key, [])
        votes = [
            vote_reference(refs),
            vote_link(msg_links),
            vote_topic(msg["text"]),
        ]
        for index, (vote, reason) in enumerate(votes):
            path = os.path.join(DONE_DIR, "rel-solo-%s-v%d.txt" % (key, index))
            if os.path.exists(path) and not args.force:
                skipped += 1
                continue
            # A solo window holds 1 message, so its label is `M1 of 1`. The
            # answer uses the same label form as a model answer, so the
            # consensus stage reads both the same way.
            with open(path, "w", encoding="utf-8") as f:
                f.write("M1 of 1 %s %s\n" % (vote, reason))
            written += 1
            tally[vote] += 1

    print("long messages %d, wrote %d answers, kept %d, Y %d N %d"
          % (len(long_messages), written, skipped, tally["Y"], tally["N"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())