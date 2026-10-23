# Align forced compaction with pre-send history budget

Observed: assembled prompt 10175 tokens, session history 4645, maxPromptTokens 10000, summary keepRecentTokens 20000. The guard requested compaction without a target, so the summarizer correctly saw no need to shrink and the turn failed.

Fix: pass a per-request history budget after subtracting assembled prompt overhead, leaving headroom; clamp a request-local copy of summary retention/output budgets, preserving concurrency and manual compaction defaults. Verify the rebuilt prompt before sending, and report an explicit fixed-overhead error when history compaction cannot help. Do not drop system instructions or bypass the prompt guard.

Local overlay inherits production budgets from config.base.yaml: maxPromptTokens 1000000, contextWindow 1048576 with triggerRatio 0.92 (threshold 964689), keepRecentTokens 20000, summary reserveTokens 16384. Temporary 32k/24k/6k/3k overrides have been removed. Validate the merged configuration without starting external services.

Tests: reproduce default-20k retention no-op with oversized assembled prompt and fixed system overhead, verify bounded send and task preservation, check budget propagation through pipeline/token-limit, concurrency isolation, and existing overflow/compaction smoke scenarios.
