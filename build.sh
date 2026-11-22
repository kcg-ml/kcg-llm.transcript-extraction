#!/usr/bin/env bash
# Build and check the transcript-extraction pipeline.
#
# The pipeline is Python 3 with no third-party package, so a build is 3 checks:
# every script compiles, every script answers --help, and the CSV tables that
# exist parse with the column count their header states.
#
# Usage:
#   ./build.sh              run the checks
#   ./build.sh --clean      remove the generated folders first
#   ./build.sh --rules      run the checks, then the rule stages (no model calls)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON="${PYTHON:-python3}"
BIN="$ROOT/bin"
GENERATED=(data out prompts logs)

clean() {
  for folder in "${GENERATED[@]}"; do
    if [ -d "$ROOT/$folder" ]; then
      rm -rf "$ROOT/$folder"
      echo "removed $folder/"
    fi
  done
}

check_compile() {
  echo "== compile =="
  "$PYTHON" -m compileall -q "$BIN" >/dev/null
  echo "ok: every script in bin/ compiles"
}

check_help() {
  echo "== --help =="
  for script in "$BIN"/*.py; do
    if ! "$PYTHON" "$script" --help >/dev/null 2>&1; then
      echo "FAIL: $script does not answer --help" >&2
      exit 1
    fi
  done
  echo "ok: every script in bin/ answers --help"
}

check_csv() {
  echo "== csv =="
  if [ ! -d "$ROOT/data" ]; then
    echo "skip: data/ does not exist yet; run ./build.sh --rules to make it"
    return 0
  fi
  "$PYTHON" - "$ROOT/data" <<'PY'
import csv, os, sys
folder = sys.argv[1]
bad = 0
count = 0
for name in sorted(os.listdir(folder)):
    if not name.endswith(".csv"):
        continue
    path = os.path.join(folder, name)
    with open(path, encoding="utf-8") as f:
        reader = csv.reader(f)
        header = next(reader, None)
        if header is None:
            print("FAIL: %s is empty" % name, file=sys.stderr)
            bad += 1
            continue
        width = len(header)
        rows = 0
        for number, row in enumerate(reader, 2):
            rows += 1
            if len(row) != width:
                print("FAIL: %s line %d has %d fields, header has %d"
                      % (name, number, len(row), width), file=sys.stderr)
                bad += 1
                break
    count += 1
    print("ok: %-28s %6d rows, %2d columns" % (name, rows, width))
print("%d csv files checked, %d bad" % (count, bad))
sys.exit(1 if bad else 0)
PY
}

main() {
  if [ "${1:-}" = "--clean" ]; then
    clean
    shift
  fi
  check_compile
  check_help
  check_csv
  if [ "${1:-}" = "--rules" ]; then
    echo "== rule stages =="
    "$PYTHON" "$BIN/tg_pipeline.py" --stage rules
  fi
  echo "build ok"
}

main "$@"