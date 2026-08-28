# Tool Runtime Deps Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make single-tool plugins return `agentkit.Tool`, multi-tool plugins return `agentkit.ToolPack`, and dynamic tool plugins return `agentkit.ToolProvider`.

**Architecture:** Keep tool implementations simple and move source aggregation into `tools/runtime`. `RuntimeDeps` exposes separate dependency fields for single tools, tool packs, and dynamic providers; the runtime flattens static tools into one name-indexed map and keeps dynamic providers on the runtime path.

**Tech Stack:** Go, pluginkit config/dependency injection, existing runtime tools tests.

---

### Task 1: Runtime Deps Shape

**Files:**
- Modify: `runtime/tools/runtime.go`
- Test: `runtime/tools/runtime_test.go`

**Steps:**
1. Add a failing test showing `RuntimeDeps.Tools` accepts `[]agentkit.Tool` directly.
2. Add `ToolPacks []agentkit.ToolPack` to `RuntimeDeps`.
3. Flatten `deps.Tools` first, then `deps.ToolPacks`, preserving duplicate-name checks.
4. Update existing runtime tests to use `Tools` for single stubs and `ToolPacks` where a pack is being tested.

### Task 2: Plugin Constructor Return Types

**Files:**
- Modify single-tool plugins under `plugins/tool/*`
- Keep multi-tool plugins returning `agentkit.ToolPack`: `plugins/tool/fs/fs_workspace.go`, `plugins/tool/fs/fs_memory.go`
- Keep dynamic plugins returning `agentkit.ToolProvider`: `plugins/tool/mcp/mcp.go`

**Steps:**
1. Change single-tool constructors from `(agentkit.ToolPack, error)` to `(agentkit.Tool, error)`.
2. Return `tool, nil` instead of `agentkit.Pack(tool), nil`.
3. Leave multi-tool builders and filters based on `ToolPack`.

### Task 3: Config And Docs

**Files:**
- Modify: `config.base.yaml`
- Modify: `docs/go-agent-harness-architecture.zh.md`
- Modify: `docs/plugin-catalog.zh.md`
- Modify: `examples/skills/agentkit-plugin-config/references/*.md`
- Modify: `examples/skills/agentkit-plugin-config/assets/tool-plugin.go`

**Steps:**
1. Move single-tool plugin ids under `deps.tools`.
2. Move multi-tool plugin ids under `deps.toolPacks`.
3. Keep MCP under `deps.dynamicTools`.
4. Update plugin docs so `tool/*` no longer universally means `ToolPack`; return type depends on whether the plugin exposes one static tool, multiple static tools, or dynamic tools.

### Task 4: Verification

**Commands:**
- `go test ./runtime/tools`
- `go test ./...`

**Expected Result:** Runtime tests cover single-tool and tool-pack aggregation, and the full Go test suite passes.
