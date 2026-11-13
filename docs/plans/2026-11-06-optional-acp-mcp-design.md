# Optional ACP delegation composition and MCP runtime bridge

## Architecture

ACP delegation composes existing plugins through dependency injection:
`agent/acp-remote` supplies an `agentkit.Agent` to `subagent/loop-agent` through
`deps.agents`. The spawner is backend-neutral; it neither constructs nor imports
an ACP plugin. No new ACP-specific spawner or runtime relocation is needed.

An opt-in `freshSession` setting gives every delegation independent history,
including delegations started from the same parent snapshot. The existing default
session ID behavior and synchronous/asynchronous delegation contracts remain.
The host owns ACP configuration, event persistence, approvals and shutdown.

The `runtime/mcpbridge` library adapts any ToolRuntime. It has no
ACP, workspace, listener, authentication or host-identity dependency. The caller
supplies trusted context and owns lifecycle/catalog refresh. Existing ACP Session
MCP reuses the mapping while retaining its scoped execution semantics.

No new plugin kinds are introduced. Existing presets and core interfaces stay
unchanged; neither integration needs the other enabled. No Buddy/NEX/TaskHub,
health probing, client installation, or role-file conventions are introduced.

## Validation

Verify fresh/legacy behavior from the same parent snapshot, real ACP delegation
through a configuration graph without MCP, standalone MCP library use,
schema/argument/error mapping, and existing approval/cancellation/scope tests.
Run `./scripts/test.sh unit`: import isolation, catalog freshness, full vet and
uncached race tests. Generate the config catalog when registered kinds change.
