#!/usr/bin/env python3
"""Stage 2: split every message into parts and write the part tables.

Reads  : data/messages.csv
Writes : data/parts_<mode>.csv for each mode in MODES
         data/parts_manifest.csv   one row per (message, mode)

Three split modes answer the operator's question "what is the unit the index
should point at":

  unbroken   one part per message. The part text is the whole message.
  sentence   one part per sentence. A sentence ends at . ! ? or a blank line.
  paragraph  one part per paragraph. A paragraph is a run of lines between
             blank lines; a code block is always its own paragraph.

Every part gets the id `<message_key>-P<part_index>`, for example
`760238-260219-P00`. The message key is `<message_id:06d>-<yymmdd>`.

A part also carries a `source_type` that says how the text came into the chat,
because a pasted abstract and a typed remark are not the same evidence:

  typed        the speaker typed it in this message
  paste        the message is a long block that looks copied from elsewhere
  forward      the message is a Telegram forward
  quote        the message is a reply that quotes the earlier message
  link_only    the message holds a URL and little else
  media_only   the message holds no text, only a file, photo, or voice note
  separator    the message is a dash or rule the speaker uses as a divider
"""

import csv
import os
import re

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")

MODES = ("unbroken", "sentence", "paragraph")

# A message longer than this many words is split even in `unbroken` mode,
# because a single 4000-character message is not one indexable unit.
UNBROKEN_MAX_WORDS = 100

SEPARATOR_RE = re.compile(r"^[\s\-—–_=*~.]{1,12}$")
SENTENCE_END_RE = re.compile(r"(?<=[.!?])[\"'”’)\]]*\s+")
CODE_FENCE_RE = re.compile(r"^```")

PART_FIELDS = [
    "part_id", "message_key", "message_id", "date", "month", "from",
    "part_index", "part_count", "split_mode", "source_type", "text",
    "char_len", "word_len", "url_count", "arxiv_count", "has_code",
    "is_duplicate", "duplicate_of",
]


def read_messages():
    with open(os.path.join(DATA_DIR, "messages.csv"), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def source_type(msg):
    """Name how the message text came into the chat."""
    text = msg["text"]
    stripped = text.strip()
    if not stripped:
        return "media_only" if msg["has_file"] == "1" or msg["media_type"] else "empty"
    if SEPARATOR_RE.match(stripped):
        return "separator"
    if msg["is_forwarded"] == "1":
        return "forward"
    words = len(stripped.split())
    urls = int(msg["url_count"] or 0)
    if urls and words <= 12:
        return "link_only"
    if msg["reply_to_message_id"]:
        return "quote"
    # A long message with few sentence breaks is pasted, not typed.
    if words >= 120:
        return "paste"
    return "typed"


def split_paragraphs(text):
    """Return the paragraphs of `text`; a code fence stays in one paragraph."""
    paragraphs = []
    current = []
    in_code = False
    for line in text.split("\n"):
        if CODE_FENCE_RE.match(line.strip()):
            in_code = not in_code
            current.append(line)
            if not in_code:
                paragraphs.append("\n".join(current))
                current = []
            continue
        if in_code:
            current.append(line)
            continue
        if line.strip() == "":
            if current:
                paragraphs.append("\n".join(current))
                current = []
            continue
        current.append(line)
    if current:
        paragraphs.append("\n".join(current))
    return [p.strip() for p in paragraphs if p.strip()]


def split_sentences(text):
    """Return the sentences of `text`, keeping code blocks whole."""
    sentences = []
    for paragraph in split_paragraphs(text):
        if paragraph.startswith("```"):
            sentences.append(paragraph)
            continue
        flat = " ".join(paragraph.split())
        start = 0
        for match in SENTENCE_END_RE.finditer(flat):
            piece = flat[start:match.end()].strip()
            if piece:
                sentences.append(piece)
            start = match.end()
        tail = flat[start:].strip()
        if tail:
            sentences.append(tail)
    return sentences


def split_words(text, max_words):
    """Break `text` into chunks of at most `max_words` words."""
    words = text.split()
    chunks = []
    for start in range(0, len(words), max_words):
        chunks.append(" ".join(words[start:start + max_words]))
    return chunks


def parts_for_mode(msg, mode):
    text = msg["text"]
    if not text.strip():
        return []
    if mode == "unbroken":
        if len(text.split()) <= UNBROKEN_MAX_WORDS:
            return [text]
        return split_words(text, UNBROKEN_MAX_WORDS)
    if mode == "sentence":
        pieces = split_sentences(text)
        return pieces or [text]
    if mode == "paragraph":
        pieces = split_paragraphs(text)
        return pieces or [text]
    raise ValueError("unknown split mode: %s" % mode)


def main():
    messages = read_messages()
    manifest = []
    for mode in MODES:
        rows = []
        for msg in messages:
            pieces = parts_for_mode(msg, mode)
            kind = source_type(msg)
            for index, piece in enumerate(pieces):
                rows.append({
                    "part_id": "%s-P%02d" % (msg["message_key"], index),
                    "message_key": msg["message_key"],
                    "message_id": msg["message_id"],
                    "date": msg["date"],
                    "month": msg["month"],
                    "from": msg["from"],
                    "part_index": index,
                    "part_count": len(pieces),
                    "split_mode": mode,
                    "source_type": kind,
                    "text": piece,
                    "char_len": len(piece),
                    "word_len": len(piece.split()),
                    "url_count": len(re.findall(r"https?://", piece)),
                    "arxiv_count": len(re.findall(r"arxiv\.org|[0-9]{4}\.[0-9]{4,5}", piece)),
                    "has_code": 1 if "```" in piece or re.search(r"^\s{4}\S", piece, re.M) else 0,
                    "is_duplicate": 1 if msg["duplicate_of"] else 0,
                    "duplicate_of": msg["duplicate_of"],
                })
        path = os.path.join(DATA_DIR, "parts_%s.csv" % mode)
        with open(path, "w", newline="", encoding="utf-8") as f:
            writer = csv.DictWriter(f, fieldnames=PART_FIELDS, quoting=csv.QUOTE_ALL)
            writer.writeheader()
            writer.writerows(rows)
        manifest.append((mode, len(rows)))
        print("wrote %6d parts  %s" % (len(rows), os.path.relpath(path, PROJECT_ROOT)))

    with open(os.path.join(DATA_DIR, "parts_manifest.csv"), "w", newline="",
              encoding="utf-8") as f:
        writer = csv.writer(f, quoting=csv.QUOTE_ALL)
        writer.writerow(["split_mode", "part_count", "message_count",
                         "unbroken_max_words"])
        for mode, count in manifest:
            writer.writerow([mode, count, len(messages), UNBROKEN_MAX_WORDS])


if __name__ == "__main__":
    main()