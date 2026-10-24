# Slash Instance Suffix Implementation Plan

**Goal:** Append plugin instance IDs only when slash command names or aliases conflict.

**Architecture:** Preserve CommandProvider compatibility using an optional instance ID interface and a provider wrapper at runner wiring. Resolve collisions centrally before registry registration; retain original names for authorization and log sanitization.

**Tech Stack:** Go, pluginkit build metadata, Go testing.

### Tasks
1. Add failing registry tests for duplicate instance commands, unique commands, aliases, order independence, generated collisions, filters, admin rules, and sanitization.
2. Add an instance-aware provider adapter and propagate build IDs in runner command wiring.
3. Resolve conflicting names/aliases with `_instanceID`, reject unresolved collisions, and expose ambiguous command alternatives.
4. Verify registry, runner, platform and integration tests; review diff and formatting.

### Accepted design
Unique names remain unchanged. All conflicting names receive instance ID suffixes; no registration-order winner. Names and aliases share one namespace. Original names remain valid policy selectors, and effective names can select individual instances. Missing IDs or collisions that cannot be resolved produce explicit errors instead of silently selecting an instance.
