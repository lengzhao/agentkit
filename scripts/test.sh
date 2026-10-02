#!/usr/bin/env bash
# Single source of truth for test tiers; CI (.github/workflows/test.yml) calls
# this script instead of inlining commands.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TIER="${1:-unit}"

case "$TIER" in
  unit)
    echo "== plugin import isolation =="
    go run ./scripts/check-plugin-imports
    echo "== go vet =="
    go vet ./...
    echo "== unit + smoke (race, no cache, explicit timeout) =="
    go test -race -count=1 -timeout=10m ./...
    ;;
  integration)
    echo "== integration presets =="
    go test -tags=integration -count=1 -timeout=15m ./integration/...
    ;;
  sysint)
    # Real-binary isolation tests (bubblewrap). In CI (CI=true) a missing or
    # unusable bwrap fails the run instead of skipping, so the security
    # coverage cannot silently vanish; locally it skips with a message.
    echo "== sysint (real bwrap) =="
    SYSINT_REQUIRED="${CI:+1}" go test -tags=sysint -count=1 -timeout=10m ./runtime/sandbox/...
    ;;
  coverage)
    # Collection only, no threshold gate yet — observe trends first.
    echo "== coverage =="
    go test -count=1 -timeout=10m -coverpkg=./... -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -1
    ;;
  smoke)
    echo "== smoke only =="
    go test -race -count=1 -run '^TestSmoke' ./testing/smoke/...
    ;;
  all)
    "$0" unit
    "$0" integration
    ;;
  *)
    echo "usage: $0 [unit|integration|sysint|coverage|smoke|all]" >&2
    exit 1
    ;;
esac
