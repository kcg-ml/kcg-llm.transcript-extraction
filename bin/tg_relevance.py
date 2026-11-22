#!/usr/bin/env python3
"""Stage 9: mark every message relevant or not, with 3 overlapping votes.

Reads  : data/messages.csv, data/references.csv
Writes : data/relevance_windows.csv   the windows and their overlap
         prompts/rel-<window_id>.txt   one prompt per window
         out/rel-<window_id>.txt       the model answer: one line per message
         data/relevance_votes.csv      one row per (message, window) vote
         data/relevance.csv            the consensus of the 3 votes per message

Why 3 overlapping windows: one window sees 10 messages and no context, so a
short message like `because it shows speedup for even dense matrix` looks
unrelated. The same message inside a window that starts 5 messages earlier
reads as part of an argument. A message that 3 windows mark `Y` is relevant
with no doubt; a message that 2 mark `Y` and 1 marks `N` is relevant with a
dissent; a message that 1 marks `Y` and 2 mark `N` is probably noise.

The 3 windows are offset by 0, 5, and 10 messages over a 20-message span, so
every message sits in the middle of at least 1 window:

  window A  messages  i .. i+9
  window B  messages  i+5 .. i+14
  window C  messages  i+10 .. i+19

A message longer than `--max-words` gets its own single-message window in all
3 votes, because a long message crowds out its neighbours and its own answer
is the only one that matters.

The vote line format is one line per message, so a reader can diff 2 answers:

  <message_key> <Y|N|U> <3 to 12 words of reason>

`U` means unsure, and it is a real answer: a window that cannot tell says so
instead of guessing.
"""

import argparse
import csv
import os
import sys

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")

WINDOW_SIZE = 10
OFFSETS = (0, 5, 10)
MAX_WORDS = 150
PROMPT_CHAR_BUDGET = 24000

WINDOW_FIELDS = ["rel_window_id", "vote_index", "start_index", "end_index",
                 "first_message_key", "last_message_key", "message_count",
                 "char_count", "prompt_path", "prompt_chars"]

VOTE_FIELDS = ["message_key", "rel_window_id", "vote_index", "vote", "reason",
               "date", "month", "from", "word_len"]

CONSENSUS_FIELDS = ["message_key", "date", "month", "from", "word_len",
                    "votes_y", "votes_n", "votes_u", "votes_total",
                    "verdict", "agreement", "reasons", "text_head"]

RELEVANCE_PROMPT = """\
You read a Telegram chat between 2 speakers: `Synth` and `Vlad [russia][ml]`.
The chat is about LLM training, sparsity, pruning, model architecture, and the
machine-learning research around them.

Your job is 1 decision per message: is this message relevant to that subject?

Mark `Y` when the message carries any of:
  a paper, a talk, a video, a repository, a dataset, a model, a checkpoint
  a method, a technique, an architecture, an optimizer, a kernel
  an experiment, a measurement, a number, a result, a benchmark
  a reason, a causal explanation, a claim about how a model works
  a suggestion about what to try, or what to do if a condition holds

Mark `N` when the message carries none of those:
  a greeting, a thanks, a joke, a separator such as `--`, a bare link with no
  comment, small talk about people, money, travel, food, or politics

Mark `U` when the message is too short or too ambiguous to decide, or when it
only makes sense with a message outside this window.

Answer with exactly 1 line per `###` message, in the order the messages
appear, and no other text. Each line is:

  <message key> <Y|N|U> <3 to 12 words of reason>

The message key is the 13-character id at the start of the `###` line, for
example `593142-251115`. Copy the key from the `###` line; never make one up.

The count of lines you write equals the count of `###` lines below. When 1
`###` line is below, you write 1 line and stop. Do not add a header, do not
add a summary, and do not write a line for a message that has no `###` line.
A message long enough to fill the window still gets 1 line.

A message body may hold several paragraphs, a list, or a quoted block. The
whole body is 1 message, so it gets 1 line. Never split a body into 1 line
per paragraph.

Never continue the key sequence past the last `###` line. A key that is not
written on a `###` line is not a message, so a line for it is a wrong answer.
The last `###` line of this prompt is the last message of this prompt.

----- MESSAGES -----
"""


def read_csv(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def build_windows(messages):
    """Return the overlapping windows, and the long messages as own windows."""
    windows = []
    long_messages = [m for m in messages if int(m["word_len"]) > MAX_WORDS]
    long_keys = {m["message_key"] for m in long_messages}

    # A long message is its own window in every vote, so it never crowds out
    # its neighbours and its answer never depends on a neighbour.
    for vote_index in range(len(OFFSETS)):
        for msg in long_messages:
            windows.append({
                "rel_window_id": "rel-solo-%s-v%d" % (msg["message_key"], vote_index),
                "vote_index": vote_index,
                "messages": [msg],
                "solo": True,
            })

    short = [m for m in messages if m["message_key"] not in long_keys]
    step = WINDOW_SIZE
    for vote_index, offset in enumerate(OFFSETS):
        start = 0
        while start < len(short):
            chunk = short[start + offset:start + offset + WINDOW_SIZE]
            if not chunk:
                break
            windows.append({
                "rel_window_id": "rel-%05d-v%d" % (start // step, vote_index),
                "vote_index": vote_index,
                "messages": chunk,
                "solo": False,
            })
            start += step
    return windows


def render_prompt(window):
    lines = [RELEVANCE_PROMPT]
    used = len(RELEVANCE_PROMPT)
    for msg in window["messages"]:
        block = "### %s | %s | %s\n%s\n" % (
            msg["message_key"], msg["date"], msg["from"], msg["text"])
        if used + len(block) > PROMPT_CHAR_BUDGET:
            break
        lines.append(block)
        used += len(block)
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    os.makedirs(PROMPT_DIR, exist_ok=True)
    messages = read_csv("messages.csv")
    position = {m["message_key"]: i for i, m in enumerate(messages)}
    windows = build_windows(messages)

    rows = []
    for window in windows:
        prompt = render_prompt(window)
        path = os.path.join(PROMPT_DIR, window["rel_window_id"] + ".txt")
        if args.force or not os.path.exists(path):
            with open(path, "w", encoding="utf-8") as f:
                f.write(prompt)
        rows.append({
            "rel_window_id": window["rel_window_id"],
            "vote_index": window["vote_index"],
            "start_index": position[window["messages"][0]["message_key"]],
            "end_index": position[window["messages"][-1]["message_key"]],
            "first_message_key": window["messages"][0]["message_key"],
            "last_message_key": window["messages"][-1]["message_key"],
            "message_count": len(window["messages"]),
            "char_count": sum(int(m["char_len"]) for m in window["messages"]),
            "prompt_path": os.path.relpath(path, PROJECT_ROOT),
            "prompt_chars": len(prompt),
        })

    path = os.path.join(DATA_DIR, "relevance_windows.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=WINDOW_FIELDS, quoting=csv.QUOTE_ALL)
        writer.writeheader()
        writer.writerows(rows)
    print("wrote %6d relevance windows  %s"
          % (len(rows), os.path.relpath(path, PROJECT_ROOT)))
    print("wrote %6d prompts  prompts/rel-*.txt" % len(rows))
    return 0


if __name__ == "__main__":
    sys.exit(main())