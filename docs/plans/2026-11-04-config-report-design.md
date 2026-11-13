# Configuration reports and deployment assertions

Preserve existing resolution semantics and expose ResolveYAMLWithReport with the same overlays/options model. Reports contain sorted merged/resolved IDs, per-instance final states and diagnostics, including original dependency edges for causal traversal. Return partial reports on resolution failure. Never include interpolated values or malformed raw interpolation text.

DeploymentIntent is supplied with WithDeploymentIntent. Validate duplicate/conflicting/unknown IDs against the merged graph, apply excluded before environment resolution, then validate required against the reachable output. Optional IDs remain observable without failing. Existing APIs remain unchanged when no intent is supplied. Dangling references are diagnostics, not new errors for legacy callers.

Tests cover direct and cascading removal, explicit null, reachability, dangling dependencies, deterministic JSON, failures, intent conflicts and exclusions, and existing config fixtures.
