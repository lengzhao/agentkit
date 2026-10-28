# Session cwd tool implementation plan

**Goal:** Let an agent set or clear its own session cwd with a single required `path` argument.

**Architecture:** Register `tool/session-cwd` as a ToolProvider, mounted through `deps.dynamicTools`. Each listing builds an immutable description from that request's execution cwd. Calls derive the session from the call context and reuse sessbind.CwdBinding/ValidateCwd. Empty path resets only the session override. Changes apply next turn; this tool does not alter execution snapshots or history. If no shared cwd is bound, describe consumer defaults explicitly rather than inventing a single cwd for differently configured tools.

**Tech Stack:** Go, existing typed tool builder, ToolProvider and session binding stores.

1. Add failing tests in plugins/tool/sessioncwd/sessioncwd_test.go for per-context descriptions, schema validation, set/reset, invalid paths, session isolation and next-turn activation.
2. Implement plugins/tool/sessioncwd/sessioncwd.go using the existing binding and workspace capabilities; reject missing path rather than interpreting omitted/null as reset.
3. Ensure the generic MCP bridge carries the immutable cwd snapshot and renders request-local descriptions without changing its shared registry; cover two independent HTTP clients. Generate plugin imports and config catalog; document local loop and ACP provider wiring.
4. Run focused tests, full tests, race checks, plugin dependency and generated catalog checks.
