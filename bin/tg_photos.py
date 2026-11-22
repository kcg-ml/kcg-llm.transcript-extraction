#!/usr/bin/env python3
"""Stage 7: read every photo of the export and index what it shows.

Reads  : extract.telegram-log.vlad-images/result.json   (the export with photos)
         extract.telegram-log.vlad-images/photos/*.jpg
Writes : data/photos.csv            one row per photo message, with its file
         prompts/photo-<key>.txt    one prompt per photo
         out/photo-<key>.txt        the model answer: what the photo shows
         data/photo_index.csv       the photo index, joined to the message index

The export that carries photos is a second export of the same chat. Its
`result.json` may still be growing while the download runs, so this script
refuses to read a file that does not parse, and says so.

A photo message has empty text in most cases, so the photo is the only content
the chat carries at that point. The model answer names 4 things:

  what the photo shows          a chart, a slide, a screenshot, a photo
  the text inside the photo     a title, an axis label, a table cell
  the topic it belongs to       an LLM or ML topic, or `not ml`
  the relevance                 1 of high, low, none, with the reason

The relevance answer is what lets a reader drop the 4 photos of a dinner and
keep the 40 photos of a loss curve.
"""

import argparse
import csv
import json
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
IMAGE_EXPORT = os.path.join(PROJECT_ROOT, "extract.telegram-log.vlad-images")
SOURCE_JSON = os.path.join(IMAGE_EXPORT, "result.json")
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")

PHOTO_FIELDS = ["photo_key", "message_key", "message_id", "date", "month", "from",
                "photo_path", "photo_file_size", "width", "height",
                "caption", "reply_to_message_id", "forwarded_from",
                "prompt_path", "answer_path"]

PHOTO_PROMPT = """\
You read one photo that a speaker sent in a Telegram chat about LLM training,
sparsity, pruning, and model architecture. The 2 speakers are `Synth` and
`Vlad [russia][ml]`.

The message that carried the photo:
  id: {message_key}
  date: {date}
  from: {sender}
  caption: {caption}
  reply to: {reply_to}

Answer in exactly these 6 lines, one value per line, no other text:

SHOWS: what the photo shows, in 1 sentence (a chart, a slide, a screenshot of code, a table, a photo of a room)
TEXT: every readable string in the photo that matters: the title, the axis labels, the legend, the code line, the table header. Separate them with ` | `. Write `none` if the photo holds no readable text.
TOPIC: the LLM or machine-learning topic the photo belongs to, in 3 to 8 words. Write `not ml` if the photo is not about machine learning.
RELEVANCE: one of `high`, `low`, `none`, then ` - ` and the reason
NUMBERS: every number the photo states, as `<what it measures> = <value>`, separated by ` ;; `. Write `none` if the photo states no number.
TERMS: the method, model, or paper names the photo shows, comma separated. Write `none` if it names none.

Do not guess a number or a name you cannot read. If the photo is unreadable,
write `unreadable` on the SHOWS line and `none` on the other lines.
"""


def load_export():
    """Read the photo export, and say why it fails if it does not parse.

    A Telegram export writes `result.json` while the download runs, so the file
    can end in the middle of a message. `salvage` keeps every message object
    that is complete and drops the last partial one.
    """
    try:
        with open(SOURCE_JSON, encoding="utf-8") as f:
            return json.load(f), False
    except json.JSONDecodeError as error:
        print("cannot read %s as written: %s"
              % (os.path.relpath(SOURCE_JSON, PROJECT_ROOT), error), file=sys.stderr)
    with open(SOURCE_JSON, encoding="utf-8") as f:
        data = f.read()
    starts = [m.start() for m in re.finditer(r'\n  \{\n   "id": ', data)]
    if not starts:
        print("the export holds no message object; is the download running?",
              file=sys.stderr)
        return None, False
    body = data[:starts[-1]].rstrip()
    if body.endswith(","):
        body = body[:-1]
    try:
        export = json.loads(body + "\n ]\n}\n")
    except json.JSONDecodeError as error:
        print("cannot salvage the export either: %s" % error, file=sys.stderr)
        return None, False
    print("the export is incomplete: kept %d complete messages and dropped the last "
          "partial one. Run this stage again when the download finishes."
          % len(export["messages"]), file=sys.stderr)
    return export, True


def message_text(msg):
    raw = msg.get("text")
    if raw is None:
        return ""
    if isinstance(raw, str):
        return raw
    return "".join(p if isinstance(p, str) else p.get("text", "") for p in raw)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--force", action="store_true",
                        help="rewrite the prompt files even if they exist")
    parser.add_argument("--strict", action="store_true",
                        help="stop when the export does not parse, instead of "
                             "keeping the complete messages")
    args = parser.parse_args()

    export, salvaged = load_export()
    if export is None:
        return 1
    if salvaged and args.strict:
        print("stopped: the export is incomplete and --strict was given", file=sys.stderr)
        return 1
    os.makedirs(PROMPT_DIR, exist_ok=True)

    rows = []
    missing = []
    for msg in export["messages"]:
        photo = msg.get("photo")
        if not photo:
            continue
        date = msg.get("date", "")
        yymmdd = date[2:4] + date[5:7] + date[8:10] if len(date) >= 10 else "000000"
        message_key = "%06d-%s" % (msg["id"], yymmdd)
        photo_key = "photo-" + message_key
        photo_path = os.path.join(IMAGE_EXPORT, photo)
        if not os.path.exists(photo_path):
            missing.append(photo)
            continue
        prompt_path = os.path.join(PROMPT_DIR, photo_key + ".txt")
        prompt = PHOTO_PROMPT.format(
            message_key=message_key,
            date=date,
            sender=msg.get("from", ""),
            caption=message_text(msg).strip() or "none",
            reply_to=msg.get("reply_to_message_id", "none"),
        )
        if args.force or not os.path.exists(prompt_path):
            with open(prompt_path, "w", encoding="utf-8") as f:
                f.write(prompt)
        rows.append({
            "photo_key": photo_key,
            "message_key": message_key,
            "message_id": msg["id"],
            "date": date,
            "month": date[:7],
            "from": msg.get("from", ""),
            "photo_path": os.path.relpath(photo_path, PROJECT_ROOT),
            "photo_file_size": msg.get("photo_file_size", ""),
            "width": msg.get("width", ""),
            "height": msg.get("height", ""),
            "caption": message_text(msg).strip()[:500],
            "reply_to_message_id": msg.get("reply_to_message_id", ""),
            "forwarded_from": msg.get("forwarded_from", ""),
            "prompt_path": os.path.relpath(prompt_path, PROJECT_ROOT),
            "answer_path": "out/%s.txt" % photo_key,
        })

    path = os.path.join(DATA_DIR, "photos.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=PHOTO_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d photo rows  %s%s"
          % (len(rows), os.path.relpath(path, PROJECT_ROOT),
             "  (from a salvaged, incomplete export)" if salvaged else ""))
    if missing:
        print("%d photos are listed in result.json but not on disk yet, for example %s"
              % (len(missing), missing[0]), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())