#!/usr/bin/env python3
"""Stage 1: read the Telegram export result.json and write the base message table.

Reads  : extract.telegram-log.vlad/result.json
Writes : data/messages.csv, data/links.csv, data/entities.csv, data/mentions.csv,
         data/replies.csv, data/forwarded.csv, data/media.csv

Every row of data/messages.csv is one Telegram message. The message text is kept
unbroken in the column `text`; the split into parts happens in stage 2.
"""

import csv
import hashlib
import json
import os
import re
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SOURCE_JSON = os.path.join(PROJECT_ROOT, "extract.telegram-log.vlad", "result.json")
DATA_DIR = os.path.join(PROJECT_ROOT, "data")

# A URL ends at whitespace or at a character that closes a sentence.
URL_RE = re.compile(r"https?://[^\s<>\"'`]+")
URL_TRAILING = ".,;:!?)]}>»\"'"

# arXiv identifiers appear as arxiv.org/abs/XXXX.XXXXX, arxiv.org/pdf/..., or as
# the bare identifier in a sentence ("see 2512.05117").
ARXIV_URL_RE = re.compile(r"arxiv\.org/(?:abs|pdf)/([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?")
ARXIV_BARE_RE = re.compile(r"(?<![\w./])([0-9]{4}\.[0-9]{4,5})(v[0-9]+)?(?![\w/])")
DOI_URL_RE = re.compile(r"doi\.org/(10\.[^\s<>\"'`]+)")

# Labels the two speakers use in front of a reference. The label tells the
# reference kind, so the extractor does not have to guess from the URL alone.
LABEL_RE = re.compile(
    r"(?i)\b(статья|статьи|paper|papers|препринт|preprint|pdf|arxiv|doi|"
    r"код|code|repo|repository|github|hf|huggingface|blog|пост|post|"
    r"ревью|review|видео|video|слайды|slides|тред|thread|твит|tweet)\b"
    r"\s*[:：]?\s*$"
)

# Words that mark a sentence as carrying a method, a reason, or an experiment.
# The list is deliberately short: stage 3 lets the model read the sentence.
METHOD_WORDS = (
    "method", "approach", "algorithm", "technique", "procedure", "pipeline",
    "метод", "подход", "алгоритм", "способ", "техника",
)
REASON_WORDS = (
    "because", "since", "therefore", "thus", "hence", "so that", "reason",
    "why", "потому", "поэтому", "значит", "причина", "следовательно",
)
EXPERIMENT_WORDS = (
    "experiment", "benchmark", "ablation", "measured", "we ran", "i ran",
    "tested", "evaluated", "results", "speedup", "accuracy", "loss",
    "эксперимент", "бенчмарк", "замер", "измерил", "проверил", "результат",
    "точность", "лосс", "ускорение",
)
CLAIM_WORDS = (
    "i think", "i believe", "in my view", "hypothesis", "idea", "claim",
    "думаю", "считаю", "гипотеза", "идея", "утвержд",
)


def load_messages():
    with open(SOURCE_JSON, encoding="utf-8") as f:
        return json.load(f)


def message_text(msg):
    """Return the message text as one string, whatever shape `text` has."""
    raw = msg.get("text")
    if raw is None:
        return ""
    if isinstance(raw, str):
        return raw
    out = []
    for piece in raw:
        if isinstance(piece, str):
            out.append(piece)
        elif isinstance(piece, dict):
            out.append(piece.get("text", ""))
    return "".join(out)


def clean_url(url):
    while url and url[-1] in URL_TRAILING:
        url = url[:-1]
    return url


def url_host(url):
    rest = re.sub(r"^https?://", "", url)
    host = rest.split("/")[0].split("?")[0]
    return host.lower()


def url_kind(url):
    """Name the kind of resource a URL points at."""
    host = url_host(url)
    low = url.lower()
    if "arxiv.org" in host:
        return "paper"
    if host.startswith("doi.org"):
        return "paper"
    if host in ("openreview.net",) or "proceedings." in host or "aclanthology" in host:
        return "paper"
    if host in ("github.com", "gitlab.com", "huggingface.co", "codeberg.org"):
        return "code"
    if host in ("x.com", "twitter.com"):
        return "social"
    if host in ("t.me", "telegram.me"):
        return "telegram"
    if host in ("youtube.com", "m.youtube.com", "youtu.be", "rutube.ru", "vimeo.com"):
        return "video"
    if host in ("en.wikipedia.org", "ru.wikipedia.org", "wikipedia.org"):
        return "wiki"
    if host in ("reddit.com", "news.ycombinator.com", "lobste.rs"):
        return "forum"
    if "substack.com" in host or "kagi.com" in host or "medium.com" in host:
        return "review"
    if host in ("colab.research.google.com",):
        return "notebook"
    if re.match(r"^[0-9.]+(:[0-9]+)?$", host):
        return "host"
    return "web"


def label_before(text, pos):
    """Return the reference label that sits just before `pos`, or ''."""
    window = text[max(0, pos - 40):pos]
    match = LABEL_RE.search(window)
    return match.group(1).lower() if match else ""


def sentence_around(text, start, end):
    """Return the sentence of `text` that contains the span [start, end)."""
    left = max(text.rfind(".", 0, start), text.rfind("\n", 0, start),
               text.rfind("!", 0, start), text.rfind("?", 0, start))
    right_candidates = [p for p in (text.find(".", end), text.find("\n", end),
                                    text.find("!", end), text.find("?", end)) if p != -1]
    right = min(right_candidates) if right_candidates else len(text)
    return text[left + 1:right + 1].strip()


def keyword_hits(text, words):
    low = text.lower()
    return [w for w in words if w in low]


def main():
    os.makedirs(DATA_DIR, exist_ok=True)
    export = load_messages()
    messages = export["messages"]
    chat_name = export.get("name", "")

    seen_text_hash = {}
    rows = []
    link_rows = []
    entity_rows = []
    mention_rows = []
    reply_rows = []
    forward_rows = []
    media_rows = []

    for index, msg in enumerate(messages):
        text = message_text(msg)
        msg_id = msg.get("id")
        date = msg.get("date", "")
        yymmdd = date[2:4] + date[5:7] + date[8:10] if len(date) >= 10 else "000000"
        message_key = "%06d-%s" % (msg_id, yymmdd)

        text_hash = hashlib.sha1(text.encode("utf-8")).hexdigest()[:16]
        if text and text_hash in seen_text_hash:
            duplicate_of = seen_text_hash[text_hash]
        else:
            duplicate_of = ""
            if text:
                seen_text_hash[text_hash] = message_key

        words = text.split()
        entities = msg.get("text_entities") or []
        entity_types = sorted({e.get("type", "") for e in entities})

        urls = []
        for match in URL_RE.finditer(text):
            url = clean_url(match.group(0))
            if not url:
                continue
            urls.append(url)
            link_rows.append({
                "message_key": message_key,
                "message_id": msg_id,
                "date": date,
                "from": msg.get("from", ""),
                "url": url,
                "host": url_host(url),
                "url_kind": url_kind(url),
                "label": label_before(text, match.start()),
                "sentence": sentence_around(text, match.start(), match.end()),
            })

        arxiv_ids = []
        for match in ARXIV_URL_RE.finditer(text):
            arxiv_ids.append(match.group(1))
        for match in ARXIV_BARE_RE.finditer(text):
            if match.group(1) not in arxiv_ids:
                arxiv_ids.append(match.group(1))

        doi_ids = [m.group(1) for m in DOI_URL_RE.finditer(text)]

        for entity in entities:
            entity_rows.append({
                "message_key": message_key,
                "entity_type": entity.get("type", ""),
                "entity_text": entity.get("text", "")[:500],
                "entity_href": entity.get("href", ""),
            })
            if entity.get("type") == "mention":
                mention_rows.append({
                    "message_key": message_key,
                    "message_id": msg_id,
                    "date": date,
                    "from": msg.get("from", ""),
                    "mention": entity.get("text", ""),
                })

        if "reply_to_message_id" in msg:
            reply_rows.append({
                "message_key": message_key,
                "message_id": msg_id,
                "date": date,
                "from": msg.get("from", ""),
                "reply_to_message_id": msg["reply_to_message_id"],
            })

        if "forwarded_from" in msg:
            forward_rows.append({
                "message_key": message_key,
                "message_id": msg_id,
                "date": date,
                "from": msg.get("from", ""),
                "forwarded_from": msg.get("forwarded_from", ""),
                "forwarded_from_id": msg.get("forwarded_from_id", ""),
            })

        if msg.get("media_type") or msg.get("file") or msg.get("photo"):
            media_rows.append({
                "message_key": message_key,
                "message_id": msg_id,
                "date": date,
                "from": msg.get("from", ""),
                "media_type": msg.get("media_type", ""),
                "file": msg.get("file", ""),
                "file_name": msg.get("file_name", ""),
                "file_size": msg.get("file_size", ""),
                "mime_type": msg.get("mime_type", ""),
                "duration_seconds": msg.get("duration_seconds", ""),
                "width": msg.get("width", ""),
                "height": msg.get("height", ""),
            })

        rows.append({
            "message_key": message_key,
            "message_id": msg_id,
            "date": date,
            "date_unixtime": msg.get("date_unixtime", ""),
            "month": date[:7],
            "from": msg.get("from", ""),
            "from_id": msg.get("from_id", ""),
            "text": text,
            "char_len": len(text),
            "word_len": len(words),
            "line_count": text.count("\n") + 1 if text else 0,
            "is_empty": 1 if not text.strip() else 0,
            "is_edited": 1 if "edited" in msg else 0,
            "is_forwarded": 1 if "forwarded_from" in msg else 0,
            "forwarded_from": msg.get("forwarded_from", ""),
            "reply_to_message_id": msg.get("reply_to_message_id", ""),
            "media_type": msg.get("media_type", ""),
            "has_file": 1 if msg.get("file") else 0,
            "url_count": len(urls),
            "arxiv_count": len(arxiv_ids),
            "doi_count": len(doi_ids),
            "arxiv_ids": " ".join(arxiv_ids),
            "doi_ids": " ".join(doi_ids),
            "entity_types": " ".join(entity_types),
            "has_code": 1 if ("code" in entity_types or "pre" in entity_types) else 0,
            "has_blockquote": 1 if "blockquote" in entity_types else 0,
            "method_hits": " ".join(keyword_hits(text, METHOD_WORDS)),
            "reason_hits": " ".join(keyword_hits(text, REASON_WORDS)),
            "experiment_hits": " ".join(keyword_hits(text, EXPERIMENT_WORDS)),
            "claim_hits": " ".join(keyword_hits(text, CLAIM_WORDS)),
            "text_hash": text_hash,
            "duplicate_of": duplicate_of,
            "chat_name": chat_name,
        })

    def write_csv(path, fieldnames, records):
        with open(path, "w", newline="", encoding="utf-8") as f:
            writer = csv.DictWriter(f, fieldnames=fieldnames, quoting=csv.QUOTE_ALL)
            writer.writeheader()
            writer.writerows(records)
        print("wrote %6d rows  %s" % (len(records), os.path.relpath(path, PROJECT_ROOT)))

    write_csv(os.path.join(DATA_DIR, "messages.csv"), list(rows[0].keys()), rows)
    write_csv(os.path.join(DATA_DIR, "links.csv"),
              ["message_key", "message_id", "date", "from", "url", "host",
               "url_kind", "label", "sentence"], link_rows)
    write_csv(os.path.join(DATA_DIR, "entities.csv"),
              ["message_key", "entity_type", "entity_text", "entity_href"], entity_rows)
    write_csv(os.path.join(DATA_DIR, "mentions.csv"),
              ["message_key", "message_id", "date", "from", "mention"], mention_rows)
    write_csv(os.path.join(DATA_DIR, "replies.csv"),
              ["message_key", "message_id", "date", "from", "reply_to_message_id"], reply_rows)
    write_csv(os.path.join(DATA_DIR, "forwarded.csv"),
              ["message_key", "message_id", "date", "from", "forwarded_from",
               "forwarded_from_id"], forward_rows)
    write_csv(os.path.join(DATA_DIR, "media.csv"),
              ["message_key", "message_id", "date", "from", "media_type", "file",
               "file_name", "file_size", "mime_type", "duration_seconds",
               "width", "height"], media_rows)


if __name__ == "__main__":
    main()