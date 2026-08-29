#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TIER="${1:-unit}"

case "$TIER" in
  unit)
    echo "== unit + smoke (default go test) =="
    go test ./...
    ;;
  integration)
    echo "== integration presets =="
    go test -tags=integration ./integration/...
    ;;
  smoke)
    echo "== smoke only =="
    go test -run '^TestSmoke' ./testing/smoke/...
    ;;
  all)
    echo "== unit + smoke =="
    go test ./...
    echo "== integration =="
    go test -tags=integration ./integration/...
    ;;
  *)
    echo "usage: $0 [unit|integration|smoke|all]" >&2
    exit 1
    ;;
esac
