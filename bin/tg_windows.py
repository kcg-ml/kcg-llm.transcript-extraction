#!/usr/bin/env python3
"""Stage 4: build the summary windows and the prompts the model reads.

Reads  : data/messages.csv, data/references.csv
Writes : data/windows.csv          one row per window (10, 100, month)
         prompts/<window_id>.txt   one prompt file per window
         prompts/manifest.csv      window_id -> prompt path, part count

A window is a run of messages the model summarises in one call:

  w10    10 consecutive messages, counted over the whole chat
  w100   100 consecutive messages
  month  every message of one calendar month
  meta   the summary of summaries: one window per level, built in stage 6

The prompt holds the message text with its id, the references the rules found,
and the output format the model must follow.
"""

import csv
import os
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")

# A prompt is capped so that one call fits a 32k-token context with room for
# the answer. 1 token is about 3 characters of mixed English and Russian.
PROMPT_CHAR_BUDGET = 60000

WINDOW_FIELDS = ["window_id", "window_kind", "window_index", "month",
                 "first_message_key", "last_message_key", "message_count",
                 "char_count", "ref_count", "prompt_path", "prompt_chars",
                 "prompt_parts"]

SUMMARY_FORMAT = """\
Answer in exactly these 14 lines, one value per line, no other text:

TOPICS: 3 to 8 short topic names, comma separated
PAPERS: one entry per paper, as `<arxiv id or title> | <what the message says about it>`, separated by ` ;; `
TALKS: one entry per talk, conference, or video, as `<name or URL> | <what it is>`, separated by ` ;; `
CODE: one entry per repository, checkpoint, or dataset, as `<name or URL> | <what it is>`, separated by ` ;; `
METHODS: one entry per named method or technique, as `<name> | <what it does here>`, separated by ` ;; `
MODELS: one entry per model or checkpoint named, comma separated
EXPERIMENTS: one entry per measurement or run, as `<what was measured> | <result>`, separated by ` ;; `
RESULTS: one entry per stated number, size, or speedup, as `<what it measures> = <value>`, separated by ` ;; `
REASONS: one entry per stated cause or justification, separated by ` ;; `
CAUSAL: one entry per cause-and-effect pair the messages state, as `<cause> -> <effect> | <the message id>`, separated by ` ;; `
CLAIMS: one entry per belief, hypothesis, or thesis stated, separated by ` ;; `
OPEN: one entry per question the speakers left open, separated by ` ;; `
SUGGESTIONS: one entry per piece of advice one speaker gives the other, as `<the advice> | <the condition it depends on, or none>`, separated by ` ;; `
QUOTES: one entry per sentence worth quoting verbatim, with its message id, as `<message id> | <the sentence>`, separated by ` ;; `

Write `none` for a line with no entry. Do not invent a paper, a number, or a
name that is not in the messages. Keep the speaker's terms; do not translate a
Russian term into English. A message that is only `--` or a link carries no
content; skip it.
"""


def read_csv(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def build_windows(messages, size, kind):
    windows = []
    for start in range(0, len(messages), size):
        chunk = messages[start:start + size]
        windows.append({
            "window_id": "%s-%05d" % (kind, start // size),
            "window_kind": kind,
            "window_index": start // size,
            "month": chunk[0]["month"] if kind != "month" else chunk[0]["month"],
            "messages": chunk,
        })
    return windows


def build_month_windows(messages):
    by_month = {}
    for msg in messages:
        by_month.setdefault(msg["month"], []).append(msg)
    windows = []
    for index, month in enumerate(sorted(by_month)):
        windows.append({
            "window_id": "month-%s" % month,
            "window_kind": "month",
            "window_index": index,
            "month": month,
            "messages": by_month[month],
        })
    return windows


def refs_by_message(references):
    out = {}
    for ref in references:
        out.setdefault(ref["message_key"], []).append(ref)
    return out


def render_message(msg, refs):
    lines = ["### %s | %s | %s" % (msg["message_key"], msg["date"], msg["from"])]
    if msg["reply_to_message_id"]:
        lines.append("(reply to %s)" % msg["reply_to_message_id"])
    if msg["forwarded_from"]:
        lines.append("(forwarded from %s)" % msg["forwarded_from"])
    lines.append(msg["text"])
    if refs:
        found = []
        for ref in refs:
            if ref["ref_kind"] in ("paper", "code", "link", "model", "video",
                                   "talk", "dataset"):
                found.append("%s=%s" % (ref["ref_kind"], ref["ref_value"]))
        if found:
            lines.append("(links found: %s)" % "; ".join(found[:20]))
    return "\n".join(lines)


def render_prompt(window, refs_index):
    header = [
        "You read a Telegram chat between 2 speakers: `Synth` and `Vlad [russia][ml]`.",
        "The chat is about LLM training, sparsity, pruning, and model architecture.",
        "The messages below are one window of the chat, in time order.",
        "",
        "Window: %s" % window["window_id"],
        "Messages: %d, from %s to %s" % (
            len(window["messages"]),
            window["messages"][0]["message_key"],
            window["messages"][-1]["message_key"]),
        "",
        "Summarise this window for an index. Name every paper, talk, video,",
        "repository, dataset, method, model, experiment, result, reason, claim,",
        "and suggestion the messages carry. A suggestion is advice one speaker",
        "gives the other: `you should do X`, or `if you do X, then Y`. A causal",
        "explanation is a cause and its effect: `X causes Y`, `Y happens because",
        "X`, `if X then Y`, `X leads to Y`. A message that is only `--` or a link",
        "carries no content; skip it.",
        "",
        SUMMARY_FORMAT,
        "",
        "----- MESSAGES -----",
    ]
    body = []
    used = sum(len(line) + 1 for line in header)
    for msg in window["messages"]:
        block = render_message(msg, refs_index.get(msg["message_key"], []))
        if used + len(block) > PROMPT_CHAR_BUDGET:
            body.append("(window truncated at %d of %d messages to fit the context)"
                        % (len(body), len(window["messages"])))
            break
        body.append(block)
        used += len(block) + 2
    return "\n".join(header + body)


def main():
    os.makedirs(PROMPT_DIR, exist_ok=True)
    messages = read_csv("messages.csv")
    references = read_csv("references.csv")
    refs_index = refs_by_message(references)

    windows = []
    windows += build_windows(messages, 10, "w10")
    windows += build_windows(messages, 100, "w100")
    windows += build_month_windows(messages)

    rows = []
    for window in windows:
        prompt = render_prompt(window, refs_index)
        path = os.path.join(PROMPT_DIR, window["window_id"] + ".txt")
        with open(path, "w", encoding="utf-8") as f:
            f.write(prompt)
        ref_count = sum(len(refs_index.get(m["message_key"], []))
                        for m in window["messages"])
        rows.append({
            "window_id": window["window_id"],
            "window_kind": window["window_kind"],
            "window_index": window["window_index"],
            "month": window["month"],
            "first_message_key": window["messages"][0]["message_key"],
            "last_message_key": window["messages"][-1]["message_key"],
            "message_count": len(window["messages"]),
            "char_count": sum(int(m["char_len"]) for m in window["messages"]),
            "ref_count": ref_count,
            "prompt_path": os.path.relpath(path, PROJECT_ROOT),
            "prompt_chars": len(prompt),
            "prompt_parts": 1,
        })

    with open(os.path.join(DATA_DIR, "windows.csv"), "w", newline="",
              encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=WINDOW_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d windows  data/windows.csv" % len(rows))
    print("wrote %6d prompts  prompts/" % len(rows))


if __name__ == "__main__":
    main()