# Optional ACP and MCP Plugins Implementation Plan

**Goal:** Deliver two independently usable optional plugins in AgentKit.

**Architecture:** Reuse ACP remote agents and child delegation. Extract MCP
protocol mapping into runtime/mcpbridge; retain scope/transport in Session MCP.

**Tech Stack:** Go, pluginkit, existing ACP SDK and mcp-go.

1. Add failing tests for `runtime/mcpbridge` and `plugins/mcp/toolruntime`, run
   them, then implement descriptors/calls and registration.
2. Refactor `plugins/acp/sessionmcptools/runtime.go` to use the bridge. Run its
   existing scope, dynamic catalog, cancellation and policy tests.
3. Add failing tests for `plugins/subagent/acp`, then implement config validation,
   owned ACP runtimes, synchronous delegation, and shutdown forwarding.
4. Add a real subprocess ACP integration test, plus isolation/cancellation and
   independently imported plugin tests. Regenerate `plugins/all.go`.
5. Document opt-in use and host responsibilities, run targeted/race/vet/full
   verification and inspect the final diff. Do not commit unrelated changes.
