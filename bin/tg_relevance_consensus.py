#!/usr/bin/env python3
"""Stage 10: turn the 3 relevance votes per message into 1 verdict.

Reads  : data/relevance_windows.csv, out/jobs/rel/done/<window_id>.txt,
         data/messages.csv
Writes : data/relevance_votes.csv   one row per (message, vote)
         data/relevance.csv         one row per message, with the consensus

The consensus rule, applied to the votes of the 3 windows that hold a message:

  3 Y                 relevant, agreement `unanimous`
  2 Y, 1 N or 1 U     relevant, agreement `majority`
  2 Y, 1 U            relevant, agreement `majority-unsure`
  1 Y, 2 N            not relevant, agreement `minority` (1 window saw it)
  1 Y, 1 N, 1 U       unsure, agreement `split`
  0 Y                 not relevant, agreement `unanimous` or `majority`
  fewer than 3 votes  unsure, agreement `incomplete`

A message that only 1 of 3 windows marked `Y` is kept in the table with its
verdict, so a reader can widen the net by filtering on `votes_y >= 1` instead
of trusting the consensus.
"""

import collections
import csv
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
# The durable runner writes 1 answer file per unit, and that file is the
# record of a finished unit. The older flat `out/<id>.txt` is read as a
# fallback, so an answer from an earlier run still counts.
JOB_DONE_DIR = os.path.join(PROJECT_ROOT, "out", "jobs", "rel", "done")
OUT_DIR = os.path.join(PROJECT_ROOT, "out")

# A vote line starts with the label the prompt gave its message, which is
# `M<position> of <total>` for a relevance prompt and a message id for an
# older prompt. Both forms are read, so an answer written before the label
# change still counts.
VOTE_LINE_RE = re.compile(
    r"^\s*`?(M[0-9]+ of [0-9]+|[0-9]{6}-[0-9]{6})`?\s*[|:]?\s*([YNUynu])\b\s*(.*)$")

LABEL_RE = re.compile(r"^### (M[0-9]+ of [0-9]+) \|", re.M)
KEY_RE = re.compile(r"^### ([0-9]{6}-[0-9]{6})", re.M)

VOTE_FIELDS = ["message_key", "rel_window_id", "vote_index", "vote", "reason",
               "date", "month", "from", "word_len"]

CONSENSUS_FIELDS = ["message_key", "date", "month", "from", "word_len",
                    "votes_y", "votes_n", "votes_u", "votes_total",
                    "verdict", "agreement", "reasons", "text_head"]


def read_csv(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def prompt_key_map(prompt, window):
    """Return {label: message_key} for the messages of one window.

    The prompt labels its messages `M<position> of <total>` and holds no
    message id, so the keys come from the `message_keys` column of the window
    table. A prompt that still holds message ids is read from the prompt.
    """
    keys = KEY_RE.findall(prompt)
    if keys:
        labels = LABEL_RE.findall(prompt)
        if labels and len(labels) == len(keys):
            return dict(zip(labels, keys))
        return {key: key for key in keys}
    keys = window.get("message_keys", "").split()
    total = len(keys)
    return {"M%d of %d" % (i, total): key for i, key in enumerate(keys, 1)}


def parse_votes(text, key_map):
    """Return {message_key: (vote, reason)} for the votes of one answer."""
    votes = {}
    for line in text.split("\n"):
        match = VOTE_LINE_RE.match(line)
        if not match:
            continue
        label = match.group(1)
        key = key_map.get(label)
        if key is None:
            continue
        vote = match.group(2).upper()
        reason = match.group(3).strip()
        # A model that repeats a label gives the same answer twice; the first
        # line wins, because the repeat is the model echoing its own output.
        if key not in votes:
            votes[key] = (vote, reason)
    return votes


def verdict_of(votes):
    """Return (verdict, agreement) for the votes of one message.

    A message sits in 3 windows, and a message near the end of the chat sits
    in a 4th or 5th because the last windows wrap to the first messages. The
    rule reads the share of `Y` votes, so it holds for any count of votes.
    """
    counts = collections.Counter(votes)
    y, n, u = counts["Y"], counts["N"], counts["U"]
    total = y + n + u
    if total < 3:
        return "UNSURE", "incomplete"
    if y == total:
        return "RELEVANT", "unanimous"
    if n == total:
        return "NOT_RELEVANT", "unanimous"
    if y * 2 > total:
        return "RELEVANT", "majority" if u == 0 else "majority-unsure"
    if n * 2 > total:
        return "NOT_RELEVANT", "majority" if y == 0 else "minority"
    if y and n:
        return "UNSURE", "split"
    return "UNSURE", "unsure-majority"


def main():
    windows = read_csv("relevance_windows.csv")
    messages = {m["message_key"]: m for m in read_csv("messages.csv")}

    vote_rows = []
    per_message = collections.defaultdict(list)
    missing_windows = 0

    for window in windows:
        path = os.path.join(PROJECT_ROOT, window["prompt_path"])
        answer_path = os.path.join(JOB_DONE_DIR, window["rel_window_id"] + ".txt")
        if not os.path.exists(answer_path):
            answer_path = os.path.join(OUT_DIR, window["rel_window_id"] + ".txt")
        if not os.path.exists(answer_path):
            missing_windows += 1
            continue
        with open(path, encoding="utf-8") as f:
            prompt = f.read()
        key_map = prompt_key_map(prompt, window)
        with open(answer_path, encoding="utf-8") as f:
            answer = f.read()
        votes = parse_votes(answer, key_map)
        for key, (vote, reason) in votes.items():
            msg = messages.get(key, {})
            vote_rows.append({
                "message_key": key,
                "rel_window_id": window["rel_window_id"],
                "vote_index": window["vote_index"],
                "vote": vote,
                "reason": reason[:300],
                "date": msg.get("date", ""),
                "month": msg.get("month", ""),
                "from": msg.get("from", ""),
                "word_len": msg.get("word_len", ""),
            })
            per_message[key].append((window["vote_index"], vote, reason))

    path = os.path.join(DATA_DIR, "relevance_votes.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=VOTE_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(vote_rows)
    print("wrote %6d vote rows  %s" % (len(vote_rows), os.path.relpath(path, PROJECT_ROOT)))

    consensus = []
    for key, msg in messages.items():
        votes = [v for _, v, _ in per_message.get(key, [])]
        verdict, agreement = verdict_of(votes)
        counts = collections.Counter(votes)
        reasons = " ;; ".join("%s:%s" % (v, r[:120]) for _, v, r in
                              sorted(per_message.get(key, [])))
        consensus.append({
            "message_key": key,
            "date": msg["date"],
            "month": msg["month"],
            "from": msg["from"],
            "word_len": msg["word_len"],
            "votes_y": counts["Y"],
            "votes_n": counts["N"],
            "votes_u": counts["U"],
            "votes_total": len(votes),
            "verdict": verdict,
            "agreement": agreement,
            "reasons": reasons,
            "text_head": msg["text"][:200].replace("\n", " "),
        })

    path = os.path.join(DATA_DIR, "relevance.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=CONSENSUS_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(consensus)
    print("wrote %6d consensus rows  %s"
          % (len(consensus), os.path.relpath(path, PROJECT_ROOT)))

    tally = collections.Counter(r["verdict"] for r in consensus)
    print("verdicts: %s" % dict(tally))
    print("agreement: %s" % dict(collections.Counter(r["agreement"] for r in consensus)))
    if missing_windows:
        print("%d relevance windows have no answer yet in out/" % missing_windows,
              file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())