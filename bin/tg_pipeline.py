#!/usr/bin/env python3
"""Run the whole extraction chain in order.

  bin/tg_extract.py      result.json  -> data/messages.csv and the side tables
  bin/tg_split.py        messages     -> data/parts_<mode>.csv
  bin/tg_references.py   messages     -> data/references.csv
  bin/tg_windows.py      messages     -> data/windows.csv, prompts/
  bin/tg_run_pi.py       prompts      -> out/<window_id>.txt
  bin/tg_aggregate.py    answers      -> data/summaries.csv, data/index*.csv
  bin/tg_photos.py       image export -> data/photos.csv, prompts/photo-*.txt
  bin/tg_run_pi.py --kind photo  -> out/photo-*.txt
  bin/tg_photo_index.py  photo answers -> data/photo_index.csv

Usage:
  python3 bin/tg_pipeline.py --stage all --jobs 4
  python3 bin/tg_pipeline.py --stage rules          # no model calls
  python3 bin/tg_pipeline.py --stage w10 --jobs 4   # one window kind
  python3 bin/tg_pipeline.py --stage photos --jobs 4 --vision-model <name> \
      --vision-provider <name>
"""

import argparse
import os
import subprocess
import sys
import time

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCRIPTS = os.path.join(PROJECT_ROOT, "scripts")
PYTHON = sys.executable


def run(script, extra=None):
    command = [PYTHON, os.path.join(SCRIPTS, script)] + (extra or [])
    shown = [os.path.relpath(c, PROJECT_ROOT) if os.path.isabs(c) and
             c.startswith(PROJECT_ROOT) else c for c in command]
    print("\n$ %s" % " ".join(shown), flush=True)
    started = time.time()
    result = subprocess.run(command, cwd=PROJECT_ROOT)
    print("  exit=%d  %.1fs" % (result.returncode, time.time() - started), flush=True)
    return result.returncode


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stage", default="all",
                        choices=["all", "rules", "w10", "w100", "month", "meta",
                                 "aggregate", "photos"])
    parser.add_argument("--jobs", type=int, default=4)
    parser.add_argument("--model", default=None)
    parser.add_argument("--vision-model", default=None,
                        help="a model that accepts image input, for --stage photos")
    parser.add_argument("--vision-provider", default=None)
    parser.add_argument("--limit", type=int, default=0)
    args = parser.parse_args()

    model_args = ["--model", args.model] if args.model else []
    limit_args = ["--limit", str(args.limit)] if args.limit else []

    if args.stage in ("all", "rules"):
        for script in ("tg_extract.py", "tg_split.py", "tg_references.py",
                       "tg_windows.py"):
            if run(script) != 0:
                return 1

    if args.stage in ("all", "w10"):
        if run("tg_run_pi.py", ["--kind", "w10", "--jobs", str(args.jobs)]
               + model_args + limit_args) not in (0, 2):
            return 1

    if args.stage in ("all", "w100"):
        # The 100-message windows need the 10-message answers first, because the
        # rollup prompt reads them.
        if run("tg_aggregate.py") not in (0, 1):
            return 1
        if run("tg_run_pi.py", ["--kind", "w100", "--jobs", str(args.jobs)]
               + model_args + limit_args) not in (0, 2):
            return 1

    if args.stage in ("all", "month"):
        if run("tg_aggregate.py") not in (0, 1):
            return 1
        if run("tg_run_pi.py", ["--kind", "month", "--jobs", str(args.jobs)]
               + model_args + limit_args) not in (0, 2):
            return 1

    if args.stage in ("all", "meta"):
        if run("tg_aggregate.py") not in (0, 1):
            return 1
        if run("tg_run_pi.py", ["--kind", "meta", "--jobs", str(args.jobs)]
               + model_args + limit_args) not in (0, 2):
            return 1

    if args.stage in ("all", "photos"):
        if run("tg_photos.py") != 0:
            return 1
        if args.vision_model:
            photo_args = ["--kind", "photo", "--jobs", str(args.jobs),
                          "--model", args.vision_model]
            if args.vision_provider:
                photo_args += ["--provider", args.vision_provider]
            if run("tg_run_pi.py", photo_args + limit_args) not in (0, 2):
                return 1
            if run("tg_photo_index.py") != 0:
                return 1
        else:
            print("\nskipped the photo model calls: give --vision-model to run them",
                  flush=True)

    if args.stage in ("all", "aggregate"):
        if run("tg_aggregate.py") not in (0, 1):
            return 1
        # The photo rows of data/index.csv are lost when tg_aggregate.py rewrites
        # the file, so they are appended again here.
        run("tg_photo_index.py")

    print("\npipeline done")
    return 0


if __name__ == "__main__":
    sys.exit(main())