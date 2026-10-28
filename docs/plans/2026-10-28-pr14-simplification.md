# PR 14 simplification implementation plan

Goal: retain scheduler reliability and safe ACP connection reclamation while removing duplicated execution and unnecessary extension points.

Architecture: shell-bash implements cap/shell.Executor using its existing execution path. The MCP bridge uses explicit refresh and symmetric context header binding. Cron retries registry reads without delaying unrelated jobs after a failed fire. Connection leases remain internal; cleanup uses the lease already held by a turn.

1. Add regression tests for shell Executor request directories, exit status and cancellation; MCP context round trips and unknown-tool behavior; cron failure isolation; channel-scoped config job visibility. Run tests before implementing fixes.
2. Replace standalone shellexec with shell-bash Executor capability; update plugin wiring and generated catalog. Preserve separate scheduled execution settings through another shell-bash instance.
3. Complete outbound MCP header binding; remove configurable transport/listen address and call-time refresh. Retain header mappings for existing host compatibility.
4. Replace cron stage maps/test-only API with one registry-read backoff; retain prompt prefix override but remove unnecessary helper. Restore channel filtering for config jobs.
5. Internalize lease/statistics types, remove unused reuse diagnostics/context capture and pinned strategy. Retain configurable TTL/none policies. Release turn subprocess before releasing its existing lease; simplify stop signaling without closing the operation channel.
6. Run affected race tests, full tests, vet, plugin import and generated catalog checks. Review diff and report any existing failures separately.

## Verification

- Regression tests failed before the fixes for missing shell Executor capability, missing outbound MCP context, implicit tool refresh, fire-induced global delay, global config-job visibility, stopped-bridge cleanup blocking, and early reclamation after pool Close.
- `go test -race -p 4 -count=1 ./...` passed. The first unrestricted run exposed an intermittent existing loop cancellation timeout; its targeted test passed 10 repetitions, and the final full run passed.
- `go vet ./...`, plugin import checks, generated config catalog checks, and `git diff --check` passed.
- Original PR configuration matched its golden files. Regenerated 18 snapshots with the expected nine-line addition for the scheduled shell-bash instance and dependency.
- Read-only independent review found no actionable regressions. The shell constructor keeps its original signature; custom MCP header bindings and the schedule route compatibility fallback remain supported.
