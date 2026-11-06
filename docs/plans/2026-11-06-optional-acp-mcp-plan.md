# ACP Composition and MCP Adapter Implementation Plan

**Goal:** Provide optional ACP delegation and MCP bridging without plugin-to-plugin imports.

**Architecture:** Inject existing ACP Agents into the backend-neutral loop-agent
spawner. Share MCP protocol mapping through runtime/mcpbridge.

**Tech Stack:** Go, pluginkit, existing ACP SDK and mcp-go.

1. Retain `runtime/mcpbridge` and `plugins/mcp/toolruntime`; reuse the bridge from
   `plugins/acp/sessionmcptools/runtime.go`.
2. Use `subagent/loop-agent` with `deps.agents`; do not add an ACP-specific spawner
   or move the existing ACP backend. Retain opt-in fresh child histories.
3. Test fresh/legacy session behavior and a real ACP subprocess injected through
   a graph without MCP. Retain existing generic delegation permission/cancel tests.
4. Document separate opt-in wiring and host lifecycle responsibilities.
5. Regenerate plugin imports and configuration catalog; run `./scripts/test.sh unit`
   and integration checks, then update the existing PR with the final scope.
