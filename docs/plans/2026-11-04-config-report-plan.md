# Configuration reports Implementation Plan

**Goal:** Explain configuration pruning and optionally enforce deployment intent.
**Architecture:** Instrument the existing resolver and prune stages; retain dependency edges as value-free diagnostics. Reuse ResolveOption for assertions.
**Tech Stack:** Go, YAML, standard testing.

1. Add config/report_test.go covering causal removal, final state, null, dangling refs, deterministic reports and partial failure. Run go test ./config to observe missing API failure.
2. Add config/report.go public report types, collector and intent validation. Integrate config/loader.go and config/prune.go without changing legacy entry signatures. Sort diagnostic inputs and sanitize malformed interpolation references in config/interpolate.go.
3. Add intent tests covering required, optional, excluded, duplicates, conflicts and unknown IDs; implement WithDeploymentIntent in config/env_context.go.
4. Run go test ./config, race tests, downstream compile/tests and git diff --check. Document API usage and review the final diff.
