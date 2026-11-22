#!/usr/bin/env python3
"""Stage 5: run the pi agent over the window prompts and save every answer.

Reads  : data/windows.csv, prompts/<window_id>.txt
Writes : out/<window_id>.txt        the model answer, one file per window
         out/<window_id>.json       the same answer with its metadata
         logs/run-<stamp>.log       one line per call, with its exit code
         out/run-state.csv          which windows are done, failed, or pending

The runner is resumable: a window whose answer file exists is skipped unless
`--force` is given. A failed call is retried `--retries` times.

A photo window is a row of data/photos.csv, not data/windows.csv: its prompt
is prompts/photo-<message_key>.txt and its answer is out/photo-<message_key>.txt.
A photo window passes its image to pi as an `@<path>` argument, so the model
must accept image input. `--model` and `--provider` choose that model.

Usage:
  python3 bin/tg_run_pi.py --kind w10 --limit 5          # smoke test
  python3 bin/tg_run_pi.py --kind photo --jobs 4         # every photo
  python3 bin/tg_run_pi.py --kind rel --jobs 8           # every relevance window
  python3 bin/tg_run_pi.py --kind w10                    # all 10-message windows
  python3 bin/tg_run_pi.py --kind month --model <name>   # month windows
  python3 bin/tg_run_pi.py --all --jobs 4                # every window, 4 at a time
"""

import argparse
import concurrent.futures
import csv
import datetime
import json
import os
import subprocess
import sys
import time

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DATA_DIR = os.path.join(PROJECT_ROOT, "data")
PROMPT_DIR = os.path.join(PROJECT_ROOT, "prompts")
OUT_DIR = os.path.join(PROJECT_ROOT, "out")
LOG_DIR = os.path.join(PROJECT_ROOT, "logs")

PI_BIN = os.environ.get("PI_BIN", "pi")
DEFAULT_MODEL = os.environ.get("PI_MODEL", "deepseek-v4.1-flash-1017")
DEFAULT_PROVIDER = os.environ.get("PI_PROVIDER", "")


def read_windows():
    with open(os.path.join(DATA_DIR, "windows.csv"), encoding="utf-8") as f:
        return list(csv.DictReader(f))


def read_relevance_windows():
    """Return the relevance windows as windows, so the runner has one shape."""
    path = os.path.join(DATA_DIR, "relevance_windows.csv")
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    windows = []
    for row in rows:
        windows.append({
            "window_id": row["rel_window_id"],
            "window_kind": "rel",
            "month": "",
            "first_message_key": row["first_message_key"],
            "last_message_key": row["last_message_key"],
            "message_count": row["message_count"],
            "prompt_path": row["prompt_path"],
        })
    return windows


def read_photos():
    """Return the photo rows as windows, so the runner has one input shape."""
    path = os.path.join(DATA_DIR, "photos.csv")
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    windows = []
    for row in rows:
        windows.append({
            "window_id": row["photo_key"],
            "window_kind": "photo",
            "month": row["month"],
            "first_message_key": row["message_key"],
            "last_message_key": row["message_key"],
            "message_count": "1",
            "prompt_path": row["prompt_path"],
            "image_path": row["photo_path"],
        })
    return windows


def select_windows(windows, args):
    chosen = []
    for window in windows:
        if args.all:
            chosen.append(window)
        elif args.kind and window["window_kind"] == args.kind:
            chosen.append(window)
        elif args.window and window["window_id"] == args.window:
            chosen.append(window)
    if args.month:
        chosen = [w for w in chosen if w["month"] == args.month]
    return chosen


def answer_path(window_id):
    return os.path.join(OUT_DIR, window_id + ".txt")


def call_pi(prompt, model, provider, timeout, image_path=""):
    """Run one pi call and return (exit_code, stdout, stderr, seconds).

    An `image_path` is passed as an `@<path>` argument, which pi turns into an
    image attachment for a model that accepts image input.
    """
    command = [PI_BIN, "--print", "--no-tools", "--no-session", "--mode", "text",
               "--model", model]
    if provider:
        command += ["--provider", provider]
    if image_path:
        command += ["@" + image_path]
    started = time.time()
    try:
        result = subprocess.run(command, input=prompt, capture_output=True,
                                text=True, timeout=timeout, cwd=PROJECT_ROOT)
        return result.returncode, result.stdout, result.stderr, time.time() - started
    except subprocess.TimeoutExpired:
        return 124, "", "timeout after %ss" % timeout, time.time() - started


def run_window(window, args, log_handle):
    window_id = window["window_id"]
    target = answer_path(window_id)
    if os.path.exists(target) and not args.force:
        return window_id, "skip", 0.0

    with open(os.path.join(PROJECT_ROOT, window["prompt_path"]), encoding="utf-8") as f:
        prompt = f.read()

    attempt = 0
    while True:
        attempt += 1
        code, stdout, stderr, seconds = call_pi(prompt, args.model, args.provider,
                                                args.timeout,
                                                window.get("image_path", ""))
        if code == 0 and stdout.strip():
            with open(target, "w", encoding="utf-8") as f:
                f.write(stdout)
            with open(os.path.join(OUT_DIR, window_id + ".json"), "w",
                      encoding="utf-8") as f:
                json.dump({
                    "window_id": window_id,
                    "window_kind": window["window_kind"],
                    "month": window["month"],
                    "first_message_key": window["first_message_key"],
                    "last_message_key": window["last_message_key"],
                    "message_count": int(window["message_count"]),
                    "model": args.model,
                    "provider": args.provider,
                    "prompt_chars": len(prompt),
                    "answer_chars": len(stdout),
                    "seconds": round(seconds, 2),
                    "attempt": attempt,
                    "finished_at": datetime.datetime.now().isoformat(timespec="seconds"),
                    "answer": stdout,
                }, f, ensure_ascii=False, indent=1)
            log_handle.write("%s OK %s attempt=%d %.1fs %d chars\n"
                             % (datetime.datetime.now().isoformat(timespec="seconds"),
                                window_id, attempt, seconds, len(stdout)))
            log_handle.flush()
            return window_id, "ok", seconds
        if attempt > args.retries:
            log_handle.write("%s FAIL %s code=%d attempt=%d %.1fs stderr=%s\n"
                             % (datetime.datetime.now().isoformat(timespec="seconds"),
                                window_id, code, attempt, seconds,
                                stderr.strip().replace("\n", " ")[:300]))
            log_handle.flush()
            return window_id, "fail:%d" % code, seconds
        time.sleep(args.retry_sleep)


def write_state(windows, results):
    path = os.path.join(OUT_DIR, "run-state.csv")
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f, quoting=csv.QUOTE_ALL)
        writer.writerow(["window_id", "window_kind", "month", "message_count",
                         "state", "answer_path"])
        for window in windows:
            window_id = window["window_id"]
            state = results.get(window_id, "pending")
            if state == "pending" and os.path.exists(answer_path(window_id)):
                state = "done-earlier"
            writer.writerow([window_id, window["window_kind"], window["month"],
                             window["message_count"], state,
                             os.path.relpath(answer_path(window_id), PROJECT_ROOT)])
    print("wrote state  %s" % os.path.relpath(path, PROJECT_ROOT))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kind", choices=["w10", "w100", "month", "meta", "photo",
                                           "rel"])
    parser.add_argument("--window")
    parser.add_argument("--month")
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--jobs", type=int, default=1)
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument("--provider", default=DEFAULT_PROVIDER)
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--retries", type=int, default=2)
    parser.add_argument("--retry-sleep", type=int, default=5)
    parser.add_argument("--force", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    if not (args.all or args.kind or args.window):
        parser.error("give --kind, --window, or --all")
    if args.kind == "photo" and args.model == DEFAULT_MODEL:
        parser.error("--kind photo needs --model naming a model that accepts image "
                     "input; the default %s reads text only" % DEFAULT_MODEL)

    os.makedirs(OUT_DIR, exist_ok=True)
    os.makedirs(LOG_DIR, exist_ok=True)
    if args.kind == "photo" or args.window and args.window.startswith("photo-"):
        windows = select_windows(read_photos(), args)
    elif args.kind == "rel":
        windows = select_windows(read_relevance_windows(), args)
    else:
        windows = select_windows(read_windows(), args)
    if args.limit:
        windows = windows[:args.limit]
    if not windows:
        print("no window matched")
        return 1

    stamp = datetime.datetime.now().strftime("%y%m%d-%H%M%S")
    log_path = os.path.join(LOG_DIR, "run-%s.log" % stamp)
    print("%d windows, model %s, jobs %d, log %s"
          % (len(windows), args.model, args.jobs, os.path.relpath(log_path, PROJECT_ROOT)))
    if args.dry_run:
        for window in windows[:10]:
            print("  would run %s (%s messages)"
                  % (window["window_id"], window["message_count"]))
        return 0

    results = {}
    started = time.time()
    with open(log_path, "a", encoding="utf-8") as log_handle:
        log_handle.write("# run %s model=%s windows=%d\n"
                         % (stamp, args.model, len(windows)))
        if args.jobs <= 1:
            for index, window in enumerate(windows, 1):
                window_id, state, seconds = run_window(window, args, log_handle)
                results[window_id] = state
                print("[%d/%d] %-16s %-6s %5.1fs"
                      % (index, len(windows), window_id, state, seconds))
        else:
            with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
                futures = {pool.submit(run_window, w, args, log_handle): w
                           for w in windows}
                for index, future in enumerate(
                        concurrent.futures.as_completed(futures), 1):
                    window_id, state, seconds = future.result()
                    results[window_id] = state
                    print("[%d/%d] %-16s %-6s %5.1fs"
                          % (index, len(windows), window_id, state, seconds))

    ok = sum(1 for v in results.values() if v == "ok")
    skipped = sum(1 for v in results.values() if v == "skip")
    failed = len(results) - ok - skipped
    print("done in %.0fs: ok=%d skip=%d fail=%d"
          % (time.time() - started, ok, skipped, failed))
    if args.kind not in ("photo", "rel"):
        write_state(read_windows(), results)
    return 0 if failed == 0 else 2


if __name__ == "__main__":
    sys.exit(main())