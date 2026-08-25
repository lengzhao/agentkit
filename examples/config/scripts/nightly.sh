#!/usr/bin/env bash
set -euo pipefail

echo "nightly: checking build and tests"
go test ./...
