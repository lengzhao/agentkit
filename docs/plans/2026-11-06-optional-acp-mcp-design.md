# Independent ACP delegation and runtime MCP adapters

## Scope

The approved scope is two independent optional plugins. No Buddy/NEX/TaskHub,
health probing, client installation, default listeners, or role-file conventions.

* `subagent/acp` constructs named ACP backends from explicit trusted config and
  delegates synchronously through the existing child execution mechanism. It
  requires workspace/filesystem/session storage/telemetry, not an MCP provider.
  Approval remains parent-owned; auto-approval is rejected. Remote runtimes are
  owned by the plugin and shut down with it. Role discovery stays in the host.
* `mcp/tool-runtime` adapts any ToolRuntime to MCP tool descriptors and calls.
  A transport-neutral `runtime/mcpbridge` library exposes the reusable adapter.
  It has no ACP, workspace, listener, authentication, or host identity dependency.
  The caller supplies trusted request context, controls lifecycle and catalog
  refresh, and handles turn-abort errors. Existing ACP Session MCP reuses it.

Registration is opt-in through imports (or the existing all-plugins bundle),
activation through instance configuration. Neither plugin imports the other.
Core interfaces and existing presets remain unchanged.

## Alternatives

Copying ACPD packages would drag in product policies. Adding another ACP engine
would duplicate isolation and permission handling. Reuse existing delegation,
and extract only protocol mapping from the existing Session MCP implementation.

## Validation

Tests cover construction without the other plugin, input validation, independent
child history, parent identity/cancellation/approval, timeout, shutdown, MCP schema
and argument mapping, runtime policy/error/cancellation behavior, and existing
Session MCP disclosure/scope regressions. Run targeted tests, race tests, vet,
and the repository suite where practical. Existing user changes stay untouched.
