#!/usr/bin/env python3
"""Stage 10: turn the 3 relevance votes per message into 1 verdict.

Reads  : data/relevance_windows.csv, out/rel-<window_id>.txt, data/messages.csv
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
OUT_DIR = os.path.join(PROJECT_ROOT, "out")

VOTE_LINE_RE = re.compile(r"^\s*`?([0-9]{6}-[0-9]{6})`?\s*[|:]?\s*([YNUynu])\b\s*(.*)$")

VOTE_FIELDS = ["message_key", "rel_window_id", "vote_index", "vote", "reason",
               "date", "month", "from", "word_len"]

CONSENSUS_FIELDS = ["message_key", "date", "month", "from", "word_len",
                    "votes_y", "votes_n", "votes_u", "votes_total",
                    "verdict", "agreement", "reasons", "text_head"]


def read_csv(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def parse_votes(text, allowed_keys):
    """Return the votes of one answer, keeping only the keys of its window."""
    votes = {}
    for line in text.split("\n"):
        match = VOTE_LINE_RE.match(line)
        if not match:
            continue
        key = match.group(1)
        if key not in allowed_keys:
            continue
        vote = match.group(2).upper()
        reason = match.group(3).strip()
        # A model that repeats a key gives the same answer twice; the first
        # line wins, because the repeat is the model echoing its own output.
        if key not in votes:
            votes[key] = (vote, reason)
    return votes


def verdict_of(votes):
    """Return (verdict, agreement) for the votes of one message."""
    counts = collections.Counter(votes)
    y, n, u = counts["Y"], counts["N"], counts["U"]
    total = y + n + u
    if total < 3:
        return "UNSURE", "incomplete"
    if y == 3:
        return "RELEVANT", "unanimous"
    if y == 2:
        return "RELEVANT", "majority" if n == 1 else "majority-unsure"
    if y == 1:
        if n == 2:
            return "NOT_RELEVANT", "minority"
        return "UNSURE", "split"
    if n >= 2:
        return "NOT_RELEVANT", "unanimous" if n == 3 else "majority"
    return "UNSURE", "unsure-majority"


def main():
    windows = read_csv("relevance_windows.csv")
    messages = {m["message_key"]: m for m in read_csv("messages.csv")}

    vote_rows = []
    per_message = collections.defaultdict(list)
    missing_windows = 0

    for window in windows:
        path = os.path.join(PROJECT_ROOT, window["prompt_path"])
        answer_path = os.path.join(OUT_DIR, window["rel_window_id"] + ".txt")
        if not os.path.exists(answer_path):
            missing_windows += 1
            continue
        # The keys a window may vote on are the keys of its prompt.
        with open(path, encoding="utf-8") as f:
            prompt = f.read()
        allowed = set(re.findall(r"^### ([0-9]{6}-[0-9]{6})", prompt, re.M))
        with open(answer_path, encoding="utf-8") as f:
            answer = f.read()
        votes = parse_votes(answer, allowed)
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