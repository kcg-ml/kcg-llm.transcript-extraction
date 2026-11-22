#!/usr/bin/env python3
"""Stage 6: turn the model answers into the summary tables and the index.

Reads  : out/<window_id>.txt, data/windows.csv, data/references.csv
Writes : data/summaries.csv        one row per window, 8 parsed fields
         data/summary_items.csv    one row per (window, field, item)
         data/summary_rollup.csv   the summary of summaries, per level
         data/index_terms.csv      every term with the windows and messages it hits
         data/index_messages.csv   one row per message, with its terms and refs
         data/index.csv            the flat index a reader searches

The rollup runs in 3 steps:

  10-message windows  ->  100-message window   (10 answers into 1)
  100-message windows ->  month                (3 to 35 answers into 1)
  month windows       ->  whole chat           (13 answers into 1)

The rollup text is written to prompts/meta-*.txt and sent to the model by
`bin/tg_run_pi.py --kind meta`, so the summary of summaries is a model
answer, not a string join.
"""

import collections
import csv
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
OUT_DIR = os.path.join(PROJECT_ROOT, "out")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")

FIELDS = ["TOPICS", "PAPERS", "TALKS", "CODE", "METHODS", "MODELS",
          "EXPERIMENTS", "RESULTS", "REASONS", "CAUSAL", "CLAIMS", "OPEN",
          "SUGGESTIONS", "QUOTES"]
ITEM_SEPARATOR = ";;"

SUMMARY_FIELDS = ["window_id", "window_kind", "month", "first_message_key",
                  "last_message_key", "message_count", "answer_chars"] + \
                 [f.lower() for f in FIELDS]
ITEM_FIELDS = ["window_id", "window_kind", "month", "field", "item_index",
               "item", "item_head"]


def read_csv(path):
    with open(path, encoding="utf-8") as f:
        return list(csv.DictReader(f))


def parse_answer(text):
    """Return the 8 fields of one model answer."""
    out = {field: "" for field in FIELDS}
    for line in text.split("\n"):
        match = re.match(r"^\s*\**([A-Z]+)\**\s*:\s*(.*)$", line)
        if not match:
            continue
        name = match.group(1)
        if name in out:
            out[name] = match.group(2).strip()
    return out


def split_items(value):
    if not value or value.strip().lower() in ("none", "n/a", "-"):
        return []
    items = [p.strip(" .") for p in value.split(ITEM_SEPARATOR)]
    return [p for p in items if p and p.lower() != "none"]


def item_head(item):
    """Return the name part of an item, before its `|` explanation."""
    head = item.split("|")[0].strip()
    return head[:120]


def load_summaries():
    windows = {w["window_id"]: w for w in read_csv(os.path.join(DATA_DIR, "windows.csv"))}
    summaries = []
    items = []
    for window_id in sorted(windows):
        path = os.path.join(OUT_DIR, window_id + ".txt")
        if not os.path.exists(path):
            continue
        with open(path, encoding="utf-8") as f:
            answer = f.read()
        parsed = parse_answer(answer)
        window = windows[window_id]
        row = {
            "window_id": window_id,
            "window_kind": window["window_kind"],
            "month": window["month"],
            "first_message_key": window["first_message_key"],
            "last_message_key": window["last_message_key"],
            "message_count": window["message_count"],
            "answer_chars": len(answer),
        }
        for field in FIELDS:
            row[field.lower()] = parsed[field]
            for index, item in enumerate(split_items(parsed[field])):
                items.append({
                    "window_id": window_id,
                    "window_kind": window["window_kind"],
                    "month": window["month"],
                    "field": field.lower(),
                    "item_index": index,
                    "item": item,
                    "item_head": item_head(item),
                })
        summaries.append(row)
    return summaries, items


def write_csv(name, fieldnames, rows):
    path = os.path.join(DATA_DIR, name)
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d rows  %s" % (len(rows), os.path.relpath(path, PROJECT_ROOT)))


def build_rollup_prompts(summaries):
    """Write one prompt per rollup window: 10x10 -> 100, 100s -> month, months -> chat."""
    by_id = {s["window_id"]: s for s in summaries}
    groups = collections.defaultdict(list)

    for window_id, summary in by_id.items():
        if window_id.startswith("w10-"):
            groups["meta-w100-%05d" % (int(window_id.split("-")[1]) // 10)].append(window_id)
    for window_id, summary in by_id.items():
        if window_id.startswith("w100-"):
            groups["meta-month-%s" % summary["month"]].append(window_id)
    for window_id, summary in by_id.items():
        if window_id.startswith("month-"):
            groups["meta-chat-00000"].append(window_id)

    rows = []
    for meta_id in sorted(groups):
        children = sorted(groups[meta_id])
        parts = []
        for child in children:
            summary = by_id[child]
            block = ["### %s (%s messages, %s)"
                     % (child, summary["message_count"], summary["month"])]
            for field in FIELDS:
                value = summary[field.lower()]
                if value:
                    block.append("%s: %s" % (field, value))
            parts.append("\n".join(block))
        prompt = "\n".join([
            "You merge several window summaries of one Telegram chat into one summary.",
            "The chat is between `Synth` and `Vlad [russia][ml]`, about LLM training,",
            "sparsity, pruning, and model architecture.",
            "",
            "Merge the summaries below. Drop a duplicate. Keep every distinct paper,",
            "talk, repository, dataset, method, model, experiment, result, reason,",
            "claim, suggestion, and causal explanation. Keep the speaker's terms.",
            "Never drop a number. A suggestion is advice one speaker gives the",
            "other: `you should do X`, or `if you do X, then Y`. A causal",
            "explanation is a cause and its effect: `X causes Y`, `Y happens",
            "because X`, `X leads to Y`.",
            "",
            "Answer in exactly these 14 lines, one value per line, no other text:",
            "TOPICS: 3 to 10 short topic names, comma separated",
            "PAPERS: `<arxiv id or title> | <what the chat says about it>`, separated by ` ;; `",
            "TALKS: `<name or URL> | <what it is>`, separated by ` ;; `",
            "CODE: `<repository, checkpoint, or dataset> | <what it is>`, separated by ` ;; `",
            "METHODS: `<name> | <what it does here>`, separated by ` ;; `",
            "MODELS: model or checkpoint names, comma separated",
            "EXPERIMENTS: `<what was measured> | <result>`, separated by ` ;; `",
            "RESULTS: `<what it measures> = <value>`, separated by ` ;; `",
            "REASONS: one entry per stated cause, separated by ` ;; `",
            "CAUSAL: `<cause> -> <effect> | <the message id>`, separated by ` ;; `",
            "CLAIMS: one entry per belief or hypothesis, separated by ` ;; `",
            "OPEN: one entry per open question, separated by ` ;; `",
            "SUGGESTIONS: `<the advice> | <the condition it depends on, or none>`, separated by ` ;; `",
            "QUOTES: `<message id> | <the sentence>`, separated by ` ;; `",
            "",
            "Write `none` for a line with no entry.",
            "",
            "----- SUMMARIES -----",
            "",
        ] + parts)
        path = os.path.join(PROMPT_DIR, meta_id + ".txt")
        with open(path, "w", encoding="utf-8") as f:
            f.write(prompt)
        rows.append({
            "window_id": meta_id,
            "window_kind": "meta",
            "window_index": 0,
            "month": by_id[children[0]]["month"],
            "first_message_key": by_id[children[0]]["first_message_key"],
            "last_message_key": by_id[children[-1]]["last_message_key"],
            "message_count": sum(int(by_id[c]["message_count"]) for c in children),
            "char_count": sum(len(by_id[c][f.lower()]) for c in children for f in FIELDS),
            "ref_count": 0,
            "prompt_path": os.path.relpath(path, PROJECT_ROOT),
            "prompt_chars": len(prompt),
            "prompt_parts": len(children),
        })
    return rows


def append_meta_windows(meta_rows):
    """Add the meta windows to data/windows.csv so the runner can find them."""
    path = os.path.join(DATA_DIR, "windows.csv")
    rows = read_csv(path)
    known = {r["window_id"] for r in rows}
    added = [r for r in meta_rows if r["window_id"] not in known]
    if not added:
        return 0
    with open(path, "a", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()), quoting=csv.QUOTE_ALL)
        writer.writerows(added)
    return len(added)


def build_index(summaries, items):
    """Write the 3 index tables."""
    # Every message, with the window that summarises it.
    messages = read_csv(os.path.join(DATA_DIR, "messages.csv"))
    references = read_csv(os.path.join(DATA_DIR, "references.csv"))
    refs_by_message = collections.defaultdict(list)
    for ref in references:
        refs_by_message[ref["message_key"]].append(ref)

    window_of_message = {}
    for summary in summaries:
        if summary["window_kind"] != "w10":
            continue
        first = summary["first_message_key"]
        last = summary["last_message_key"]
        for msg in messages:
            if first <= msg["message_key"] <= last:
                window_of_message[msg["message_key"]] = summary["window_id"]

    index_messages = []
    for msg in messages:
        refs = refs_by_message.get(msg["message_key"], [])
        kinds = collections.Counter(r["ref_kind"] for r in refs)
        index_messages.append({
            "message_key": msg["message_key"],
            "date": msg["date"],
            "month": msg["month"],
            "from": msg["from"],
            "word_len": msg["word_len"],
            "url_count": msg["url_count"],
            "arxiv_ids": msg["arxiv_ids"],
            "ref_count": len(refs),
            "ref_kinds": " ".join("%s=%d" % (k, v) for k, v in sorted(kinds.items())),
            "w10_window": window_of_message.get(msg["message_key"], ""),
            "is_duplicate": 1 if msg["duplicate_of"] else 0,
            "text_head": msg["text"][:200].replace("\n", " "),
        })
    write_csv("index_messages.csv", list(index_messages[0].keys()), index_messages)

    # Every term, with the windows and the months it hits.
    term_hits = collections.defaultdict(lambda: {"windows": set(), "months": set(),
                                                 "fields": set(), "count": 0})
    for item in items:
        key = item["item_head"].lower()
        if not key:
            continue
        entry = term_hits[key]
        entry["windows"].add(item["window_id"])
        entry["months"].add(item["month"])
        entry["fields"].add(item["field"])
        entry["count"] += 1
    index_terms = []
    for term in sorted(term_hits, key=lambda t: -term_hits[t]["count"]):
        entry = term_hits[term]
        index_terms.append({
            "term": term,
            "hit_count": entry["count"],
            "fields": " ".join(sorted(entry["fields"])),
            "months": " ".join(sorted(entry["months"])),
            "month_count": len(entry["months"]),
            "windows": " ".join(sorted(entry["windows"])[:40]),
            "window_count": len(entry["windows"]),
        })
    write_csv("index_terms.csv", list(index_terms[0].keys()), index_terms)

    # The flat index: one row per (window, field, item), with the message range.
    index_rows = []
    by_id = {s["window_id"]: s for s in summaries}
    for item in items:
        summary = by_id[item["window_id"]]
        index_rows.append({
            "index_id": "I%06d" % (len(index_rows) + 1),
            "window_id": item["window_id"],
            "window_kind": item["window_kind"],
            "month": item["month"],
            "first_message_key": summary["first_message_key"],
            "last_message_key": summary["last_message_key"],
            "field": item["field"],
            "item": item["item"],
            "item_head": item["item_head"],
        })
    write_csv("index.csv", list(index_rows[0].keys()), index_rows)


def main():
    summaries, items = load_summaries()
    if not summaries:
        print("no model answers in out/; run bin/tg_run_pi.py first", file=sys.stderr)
        return 1
    write_csv("summaries.csv", SUMMARY_FIELDS, summaries)
    write_csv("summary_items.csv", ITEM_FIELDS, items)

    meta_rows = build_rollup_prompts(summaries)
    added = append_meta_windows(meta_rows)
    print("wrote %6d rollup prompts  prompts/meta-*.txt (%d new windows)"
          % (len(meta_rows), added))
    # A rollup answer lands in out/jobs/meta/done/<meta_id>.txt, so the runner
    # finds its units in data/windows.csv. The rows above carry the 12 columns
    # of that file, so the runner can read them.
    if added:
        print("run the rollups with: python3 bin/tg_job.py --job meta")

    # The rollup answers, once they exist, are their own summary table.
    rollup = []
    for row in meta_rows:
        path = os.path.join(OUT_DIR, "jobs", "meta", "done",
                            row["window_id"] + ".txt")
        if not os.path.exists(path):
            path = os.path.join(OUT_DIR, row["window_id"] + ".txt")
        if not os.path.exists(path):
            continue
        with open(path, encoding="utf-8") as f:
            answer = f.read()
        parsed = parse_answer(answer)
        entry = {
            "window_id": row["window_id"],
            "window_kind": "meta",
            "month": row["month"],
            "first_message_key": row["first_message_key"],
            "last_message_key": row["last_message_key"],
            "message_count": row["message_count"],
            "answer_chars": len(answer),
        }
        for field in FIELDS:
            entry[field.lower()] = parsed[field]
        rollup.append(entry)
    if rollup:
        write_csv("summary_rollup.csv", SUMMARY_FIELDS, rollup)

    build_index(summaries, items)
    return 0


if __name__ == "__main__":
    sys.exit(main())