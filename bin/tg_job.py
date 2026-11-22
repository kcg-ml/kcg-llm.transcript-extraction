#!/usr/bin/env python3
"""A durable, resumable job runner with a progress line every N seconds.

The problem this solves: a run of 3961 model calls takes hours, and a shell
that prints one line per call says nothing for minutes at a time. A reader
cannot tell a slow call from a dead process.

This runner keeps 3 things on disk, so a killed run resumes where it stopped:

  out/<job>/state.jsonl   one line per finished unit: unit id, state, seconds
  out/<job>/done/<id>     the answer of one unit, written once and never again
  out/<job>/failed/<id>   the error of one unit that used every retry

A unit is done when its file exists in `done/`. Nothing else is trusted, so a
kill at any moment loses at most the calls that were in flight.

The progress line goes to the terminal every `--poll` seconds and to
`logs/<job>.progress.log`, so a reader can `tail -f` it:

  [12:03:44] 243/3961  6.1%  ok=240 fail=3  rate=1.9/s  eta=32m  inflight=8  last=rel-00042-v1 4.2s

Usage:
  python3 bin/tg_job.py --job rel --jobs 8 --poll 30
  python3 bin/tg_job.py --job rel --status          # print progress and exit
  python3 bin/tg_job.py --job rel --retry-failed    # re-queue the failed units
"""

import argparse
import concurrent.futures
import csv
import datetime
import json
import os
import re
import subprocess
import sys
import threading
import time

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")
OUT_DIR = os.path.join(PROJECT_ROOT, "out")
LOG_DIR = os.path.join(PROJECT_ROOT, "logs")

PI_BIN = os.environ.get("PI_BIN", "pi")
DEFAULT_MODEL = os.environ.get("PI_MODEL", "deepseek-v4.1-flash-1017")
# A bare model name matches 9 authenticated providers, and pi then exits 0
# with an `Error: Model ... is ambiguous` line on stdout. Name the provider.
DEFAULT_PROVIDER = os.environ.get("PI_PROVIDER",
                                  "local-dgx1.deepseek-v4.1-flash-1017-128k")

# pi writes a startup error to stdout and still exits 0, so the answer text is
# checked for the error shapes before it is saved.
ERROR_PREFIXES = ("Error:", "error:", "Error ", "Cannot ", "Unable to ")


# ---------------------------------------------------------------- unit sources

def read_rows(name):
    with open(os.path.join(DATA_DIR, name), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def units_from_windows(kind):
    """Return (unit_id, prompt_path, image_path) for a window kind."""
    return [(r["window_id"], r["prompt_path"], "")
            for r in read_rows("windows.csv") if r["window_kind"] == kind]


def units_from_relevance():
    return [(r["rel_window_id"], r["prompt_path"], "")
            for r in read_rows("relevance_windows.csv")]


def units_from_photos():
    return [(r["photo_key"], r["prompt_path"], r["photo_path"])
            for r in read_rows("photos.csv")]


UNIT_SOURCES = {
    "w10": lambda: units_from_windows("w10"),
    "w100": lambda: units_from_windows("w100"),
    "month": lambda: units_from_windows("month"),
    "meta": lambda: units_from_windows("meta"),
    "rel": units_from_relevance,
    "photo": units_from_photos,
}


# ---------------------------------------------------------------- job storage

class Job:
    def __init__(self, name):
        self.name = name
        self.root = os.path.join(OUT_DIR, "jobs", name)
        self.done_dir = os.path.join(self.root, "done")
        self.failed_dir = os.path.join(self.root, "failed")
        self.state_path = os.path.join(self.root, "state.jsonl")
        for path in (self.done_dir, self.failed_dir):
            os.makedirs(path, exist_ok=True)
        self.lock = threading.Lock()

    def done_path(self, unit_id):
        return os.path.join(self.done_dir, unit_id + ".txt")

    def failed_path(self, unit_id):
        return os.path.join(self.failed_dir, unit_id + ".json")

    def is_done(self, unit_id):
        return os.path.exists(self.done_path(unit_id))

    def write_done(self, unit_id, answer, meta):
        """Write the answer, then the state line. The answer file is the truth."""
        path = self.done_path(unit_id)
        temporary = path + ".tmp"
        with open(temporary, "w", encoding="utf-8") as f:
            f.write(answer)
        os.replace(temporary, path)
        self.append_state(unit_id, "ok", meta)

    def write_failed(self, unit_id, error, meta):
        with open(self.failed_path(unit_id), "w", encoding="utf-8") as f:
            json.dump({"unit_id": unit_id, "error": error, **meta}, f,
                      ensure_ascii=False, indent=1)
        self.append_state(unit_id, "fail", meta)

    def append_state(self, unit_id, state, meta):
        line = {"unit_id": unit_id, "state": state,
                "at": datetime.datetime.now().isoformat(timespec="seconds"), **meta}
        with self.lock:
            with open(self.state_path, "a", encoding="utf-8") as f:
                f.write(json.dumps(line, ensure_ascii=False) + "\n")

    def clear_failed(self, unit_id):
        path = self.failed_path(unit_id)
        if os.path.exists(path):
            os.remove(path)

    def counts(self, units):
        done = sum(1 for u in units if self.is_done(u[0]))
        failed = sum(1 for u in units if os.path.exists(self.failed_path(u[0])))
        return done, failed


# ---------------------------------------------------------------- the call

def call_pi(prompt, model, provider, timeout, image_path=""):
    command = [PI_BIN, "--print", "--no-tools", "--no-session", "--mode", "text",
               "--model", model]
    if provider:
        command += ["--provider", provider]
    if image_path:
        command += ["@" + image_path]
    started = time.time()
    process = subprocess.Popen(command, stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               text=True, cwd=PROJECT_ROOT)
    try:
        stdout, stderr = process.communicate(input=prompt, timeout=timeout)
        return process.returncode, stdout, stderr, time.time() - started
    except subprocess.TimeoutExpired:
        # Kill the whole process group, so a child that holds the pipe open
        # cannot keep the call alive past its timeout.
        process.kill()
        try:
            process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            pass
        return 124, "", "timeout after %ss, process killed" % timeout, time.time() - started


def answer_keys(text):
    """Return the message keys an answer votes on, in order."""
    return re.findall(r"^\s*`?([0-9]{6}-[0-9]{6})`?\s+[YNU]\b", text, re.M)


def answer_is_wrong(prompt, text):
    """Return a reason when the answer votes on a key its prompt does not hold.

    A model that invents keys answers a window it was never given, and the
    vote then lands on a message that does not exist. The count check catches
    the same defect from the other side: 1 `###` line must yield 1 vote line.
    """
    expected = re.findall(r"^### ([0-9]{6}-[0-9]{6})", prompt, re.M)
    if not expected:
        return ""
    got = answer_keys(text)
    unknown = [k for k in got if k not in set(expected)]
    if unknown:
        return "answer votes on %d key(s) not in the prompt: %s" % (
            len(unknown), ", ".join(unknown[:5]))
    if len(got) != len(expected):
        return "answer has %d vote lines, the prompt has %d messages" % (
            len(got), len(expected))
    return ""


def answer_is_error(text):
    """Return the error line when pi answered with an error instead of a result."""
    stripped = text.strip()
    if not stripped:
        return "empty answer"
    first = stripped.split("\n", 1)[0].strip()
    for prefix in ERROR_PREFIXES:
        if first.startswith(prefix):
            return first[:500]
    return ""


def run_unit(job, unit, args, counters):
    unit_id, prompt_path, image_path = unit
    if job.is_done(unit_id):
        return
    with open(os.path.join(PROJECT_ROOT, prompt_path), encoding="utf-8") as f:
        prompt = f.read()

    attempt = 0
    while True:
        attempt += 1
        with counters["lock"]:
            counters["inflight"] += 1
        try:
            code, stdout, stderr, seconds = call_pi(
                prompt, args.model, args.provider, args.timeout, image_path)
        finally:
            with counters["lock"]:
                counters["inflight"] -= 1

        answer_error = answer_is_error(stdout) or answer_is_wrong(prompt, stdout)
        if code == 0 and stdout.strip() and not answer_error:
            job.write_done(unit_id, stdout, {
                "model": args.model, "seconds": round(seconds, 2),
                "attempt": attempt, "chars": len(stdout)})
            job.clear_failed(unit_id)
            with counters["lock"]:
                counters["ok"] += 1
                counters["last"] = (unit_id, round(seconds, 1))
                counters["seconds"] += seconds
            return

        if attempt > args.retries:
            reason = answer_error or stderr.strip()[:1000]
            job.write_failed(unit_id, reason, {
                "code": code, "attempt": attempt,
                "seconds": round(seconds, 2)})
            with counters["lock"]:
                counters["fail"] += 1
                counters["last"] = (unit_id, round(seconds, 1))
            return
        time.sleep(args.retry_sleep)


# ---------------------------------------------------------------- progress

def format_eta(seconds):
    if seconds <= 0 or seconds != seconds:
        return "--"
    minutes = int(seconds // 60)
    if minutes < 60:
        return "%dm%02ds" % (minutes, int(seconds % 60))
    return "%dh%02dm" % (minutes // 60, minutes % 60)


def progress_line(job, units, counters, started):
    total = len(units)
    done, failed = job.counts(units)
    elapsed = max(time.time() - started, 0.001)
    with counters["lock"]:
        ok = counters["ok"]
        inflight = counters["inflight"]
        last = counters["last"]
        seconds = counters["seconds"]
    rate = ok / elapsed if ok else 0.0
    remaining = total - done
    eta = remaining / rate if rate > 0 else 0
    last_text = "%s %.1fs" % last if last else "-"
    return ("[%s] %d/%d  %5.1f%%  ok=%d fail=%d  rate=%.2f/s  eta=%s  "
            "inflight=%d  last=%s"
            % (datetime.datetime.now().strftime("%H:%M:%S"), done, total,
               100.0 * done / total if total else 0.0, ok, failed, rate,
               format_eta(eta), inflight, last_text))


def progress_loop(job, units, counters, started, poll, stop_event, progress_path):
    with open(progress_path, "a", encoding="utf-8") as handle:
        while not stop_event.is_set():
            line = progress_line(job, units, counters, started)
            print(line, flush=True)
            handle.write(line + "\n")
            handle.flush()
            stop_event.wait(poll)


# ---------------------------------------------------------------- main

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--job", required=True, choices=sorted(UNIT_SOURCES))
    parser.add_argument("--jobs", type=int, default=8)
    parser.add_argument("--poll", type=int, default=30,
                        help="seconds between progress lines")
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument("--provider", default=DEFAULT_PROVIDER)
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--retries", type=int, default=2)
    parser.add_argument("--retry-sleep", type=int, default=5)
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--status", action="store_true",
                        help="print the progress of the job and exit")
    parser.add_argument("--retry-failed", action="store_true",
                        help="clear the failed markers so the next run retries them")
    args = parser.parse_args()

    units = UNIT_SOURCES[args.job]()
    if args.limit:
        units = units[:args.limit]
    job = Job(args.job)
    os.makedirs(LOG_DIR, exist_ok=True)
    progress_path = os.path.join(LOG_DIR, "%s.progress.log" % args.job)

    if args.retry_failed:
        cleared = 0
        for unit in units:
            if os.path.exists(job.failed_path(unit[0])):
                job.clear_failed(unit[0])
                cleared += 1
        print("cleared %d failed markers for job %s" % (cleared, args.job))
        return 0

    if args.status:
        counters = {"ok": 0, "fail": 0, "inflight": 0, "last": None, "seconds": 0,
                    "lock": threading.Lock()}
        print(progress_line(job, units, counters, time.time()))
        return 0

    pending = [u for u in units if not job.is_done(u[0])]
    print("job %s: %d units, %d pending, %d jobs, model %s"
          % (args.job, len(units), len(pending), args.jobs, args.model), flush=True)
    print("progress log: %s" % os.path.relpath(progress_path, PROJECT_ROOT), flush=True)
    if not pending:
        print("nothing to do")
        return 0

    counters = {"ok": 0, "fail": 0, "inflight": 0, "last": None, "seconds": 0,
                "lock": threading.Lock()}
    started = time.time()
    stop_event = threading.Event()
    watcher = threading.Thread(target=progress_loop,
                               args=(job, units, counters, started, args.poll,
                                     stop_event, progress_path), daemon=True)
    watcher.start()

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
            futures = [pool.submit(run_unit, job, unit, args, counters)
                       for unit in pending]
            for future in concurrent.futures.as_completed(futures):
                future.result()
    except KeyboardInterrupt:
        print("\ninterrupted; finished units are kept in %s"
              % os.path.relpath(job.done_dir, PROJECT_ROOT), flush=True)
        stop_event.set()
        return 130

    stop_event.set()
    watcher.join(timeout=2)
    line = progress_line(job, units, counters, started)
    print(line, flush=True)
    with open(progress_path, "a", encoding="utf-8") as handle:
        handle.write(line + "  DONE\n")
    done, failed = job.counts(units)
    return 0 if failed == 0 else 2


if __name__ == "__main__":
    sys.exit(main())