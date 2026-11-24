#!/usr/bin/env bash
# Build and check the transcript-extraction pipeline.
#
# The pipeline is Go with no third-party module, so a build is 4 checks: every
# package compiles, `go vet` finds no defect, every subcommand answers --help,
# and every CSV table in data/ has the column count its header states.
#
# Usage:
#   ./build.sh              run the checks
#   ./build.sh --clean      remove the generated folders first
#   ./build.sh --rules      run the checks, then the rule stages (no model calls)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_FOLDER="$ROOT/.build"
PIPELINE_BINARY="$BUILD_FOLDER/transcriptextraction"
CSVCHECK_BINARY="$BUILD_FOLDER/csvcheck"
GENERATED=(data model-answer prompts logs)
SUBCOMMAND_LIST=(extract split references windows relevance-windows
  relevance-solo relevance-consensus photos photo-index aggregate job pipeline)

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
  mkdir -p "$BUILD_FOLDER"
  (cd "$ROOT" && go build -o "$PIPELINE_BINARY" ./cmd/command)
  (cd "$ROOT" && go build -o "$CSVCHECK_BINARY" ./cmd/csvcheck)
  echo "ok: every package compiles"
}

check_vet() {
  echo "== vet =="
  (cd "$ROOT" && go vet ./...)
  echo "ok: go vet finds no defect"
}

check_help() {
  echo "== --help =="
  if ! "$PIPELINE_BINARY" --help >/dev/null 2>&1; then
    echo "FAIL: transcriptextraction does not answer --help" >&2
    exit 1
  fi
  for subcommand in "${SUBCOMMAND_LIST[@]}"; do
    if ! "$PIPELINE_BINARY" "$subcommand" --help >/dev/null 2>&1; then
      echo "FAIL: subcommand $subcommand does not answer --help" >&2
      exit 1
    fi
  done
  echo "ok: every subcommand answers --help"
}

check_csv() {
  echo "== csv =="
  if [ ! -d "$ROOT/data" ]; then
    echo "skip: data/ does not exist yet; run ./build.sh --rules to make it"
    return 0
  fi
  "$CSVCHECK_BINARY" "$ROOT/data"
}

main() {
  if [ "${1:-}" = "--clean" ]; then
    clean
    shift
  fi
  check_compile
  check_vet
  check_help
  check_csv
  if [ "${1:-}" = "--rules" ]; then
    echo "== rule stages =="
    "$PIPELINE_BINARY" pipeline --stage rules
  fi
  echo "build ok"
}

main "$@"