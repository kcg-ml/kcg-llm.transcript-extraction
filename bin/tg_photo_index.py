#!/usr/bin/env python3
"""Stage 8: join the photo answers into the index.

Reads  : data/photos.csv, out/photo-<key>.txt
Writes : data/photo_index.csv   one row per photo, with its 6 parsed fields
         data/index.csv          the photo rows are appended, field `photo`

A photo row enters the index with the same shape as a summary row, so a reader
searches one file for both a sentence and a picture.

The append is idempotent: a second run replaces the earlier photo rows instead
of adding them twice.
"""

import csv
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
OUT_DIR = os.path.join(PROJECT_ROOT, "out")

PHOTO_FIELDS = ["SHOWS", "TEXT", "TOPIC", "RELEVANCE", "NUMBERS", "TERMS"]
INDEX_FIELDS = ["photo_key", "message_key", "date", "month", "from", "photo_path",
                "shows", "text_in_photo", "topic", "relevance", "relevance_reason",
                "numbers", "terms", "answer_chars"]

# The columns of data/index.csv, which bin/tg_aggregate.py writes.
CHAT_INDEX_FIELDS = ["index_id", "window_id", "window_kind", "month",
                     "first_message_key", "last_message_key", "field", "item",
                     "item_head"]


def append_to_index(rows):
    """Replace the photo rows of data/index.csv with `rows`."""
    path = os.path.join(DATA_DIR, "index.csv")
    kept = []
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            kept = [r for r in csv.DictReader(f) if r["window_kind"] != "photo"]
    for row in rows:
        kept.append({
            "index_id": "P%06d" % (len(kept) + 1),
            "window_id": row["photo_key"],
            "window_kind": "photo",
            "month": row["month"],
            "first_message_key": row["message_key"],
            "last_message_key": row["message_key"],
            "field": "photo",
            "item": "%s | %s | %s" % (row["shows"], row["topic"], row["terms"]),
            "item_head": row["topic"][:120],
        })
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=CHAT_INDEX_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(kept)
    print("wrote %6d index rows  data/index.csv (%d of them photo rows)"
          % (len(kept), len(rows)))


def parse_answer(text):
    out = {field: "" for field in PHOTO_FIELDS}
    for line in text.split("\n"):
        match = re.match(r"^\s*\**([A-Z]+)\**\s*:\s*(.*)$", line)
        if match and match.group(1) in out:
            out[match.group(1)] = match.group(2).strip()
    return out


def main():
    path = os.path.join(DATA_DIR, "photos.csv")
    if not os.path.exists(path):
        print("no data/photos.csv; run bin/tg_photos.py first", file=sys.stderr)
        return 1
    with open(path, encoding="utf-8") as f:
        photos = list(csv.DictReader(f))

    rows = []
    pending = 0
    for photo in photos:
        # A photo answer comes from the durable job `photo`, which writes
        # out/jobs/photo/done/<photo_key>.txt. A single run of
        # bin/tg_run_pi.py writes out/photo-<key>.txt instead, so read both.
        candidates = [os.path.join(OUT_DIR, "jobs", "photo", "done",
                                   photo["photo_key"] + ".txt"),
                      os.path.join(PROJECT_ROOT, photo["answer_path"])]
        answer_path = next((p for p in candidates if os.path.exists(p)), "")
        if not answer_path:
            pending += 1
            continue
        with open(answer_path, encoding="utf-8") as f:
            answer = f.read()
        parsed = parse_answer(answer)
        relevance = parsed["RELEVANCE"]
        reason = ""
        if " - " in relevance:
            relevance, reason = relevance.split(" - ", 1)
        rows.append({
            "photo_key": photo["photo_key"],
            "message_key": photo["message_key"],
            "date": photo["date"],
            "month": photo["month"],
            "from": photo["from"],
            "photo_path": photo["photo_path"],
            "shows": parsed["SHOWS"],
            "text_in_photo": parsed["TEXT"],
            "topic": parsed["TOPIC"],
            "relevance": relevance.strip().lower(),
            "relevance_reason": reason.strip(),
            "numbers": parsed["NUMBERS"],
            "terms": parsed["TERMS"],
            "answer_chars": len(answer),
        })

    out_path = os.path.join(DATA_DIR, "photo_index.csv")
    with open(out_path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=INDEX_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d photo index rows  %s"
          % (len(rows), os.path.relpath(out_path, PROJECT_ROOT)))
    append_to_index(rows)
    if pending:
        print("%d photos have no answer yet in out/jobs/photo/done/" % pending,
              file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())