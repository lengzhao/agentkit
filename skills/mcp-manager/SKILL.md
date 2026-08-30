---
name: mcp-manager
description: MCP server 配置与动态工具维护约定。编辑 mcp.json 前先加载本 Skill。
---

# MCP 配置指南

## 文件布局

顶层 `mcpServers`，格式与 Cursor 等项目一致。

默认查找顺序（L0 `mcp.default`）：

- `local:mcp.json` → `.agentkit/mcp.json`（项目/租户私有）
- `global:mcp.json` → `~/.agentkit/mcp.json`（跨项目共享）

先命中的文件赢（按 server 名字去重）。部分 preset 或 L1 配置可能叠加 `.cursor/mcp.json` 等路径，以实际 `mcp.default.config.files` 为准。

路径经 workspace 解析，可用 `local:` / `global:` 前缀。

## 推荐工作流

1. 用 `read` / `edit` / `write` 修改 `mcp.json`（或配置里列出的路径）。
2. 请用户执行 **`/mcp -u`** 重读配置并刷新动态 MCP 工具（Agent 工具列表里没有 slash command）。
3. 查看状态时请用户执行 **`/mcp`**（显示已加载 server 与帮助）。
4. 追加单条 server 时，用户也可用 **`/mcp add <name> <json>`**（探活校验、写盘、失败回滚）。
5. 调用 `github__search` 等带前缀的动态 MCP 工具。

## Slash 命令

| 命令 | 作用 |
|---|---|
| `/mcp` | 查看当前已加载 server 与工具摘要、帮助 |
| `/mcp -u` | 重读 `mcp.json` 并重新发现动态工具 |
| `/mcp add <name> <json>` | 追加 server 条目（探活、写盘、失败回滚） |

改完 `mcp.json` 后**必须**请用户 `/mcp -u`，否则模型仍看到旧的工具列表。

## mcp.json 示例

```json
{
  "mcpServers": {
    "github": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "ghcr.io/example/github-mcp"],
      "env": { "GITHUB_TOKEN": "env:GITHUB_TOKEN" },
      "prefix": "github__",
      "allowTools": ["search", "get_file"],
      "denyTools": ["delete_repo"],
      "timeoutSeconds": 60
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
    },
    "remote": {
      "url": "http://127.0.0.1:8080/mcp",
      "type": "sse"
    }
  }
}
```

## 字段说明

| 字段 | 说明 |
|---|---|
| `command` / `args` | stdio 子进程 MCP server |
| `env` | 传给子进程的环境变量；值可为 `env:NAME`，经 credentials 解析 |
| `url` | HTTP/SSE 远程 MCP 端点 |
| `type` | `sse`、`http` / `streamable`；留空时先尝试 streamable 再 SSE |
| `prefix` | 模型可见工具名前缀，默认 `<serverName>__` |
| `allowTools` / `denyTools` | 原始 MCP 工具名白 / 黑名单（按 server 暴露名过滤） |
| `timeoutSeconds` | 单次 MCP 调用墙钟超时 |

每个 server 条目必须提供 **`command`**（stdio）或 **`url`**（远程）之一。

## 动态工具命名

`<prefix><原始MCP工具名>`，例如 server 名 `github`、prefix 默认 `github__`、MCP 工具 `search` → 模型侧 `github__search`。

模型看到的是各 MCP server 的原生 input schema（不是泛化 `mcp_call`）。

## 敏感信息

- 密钥、token 等用 `env:VAR_NAME` 引用，由 credentials 从环境变量或 `.env` 解析。
- 不要把明文 secret 写进 mcp.json。

## 注意

- server 配置与各 server 的工具列表**缓存在内存**；改 `mcp.json` 或重启 server 后必须 **`/mcp -u`**。
- 单个 server 连不上时跳过并 `slog.Warn`，不影响其他 server。
- 多租户下 MCP 客户端池按 `(租户键, server 名)` 分槽，同名 server 在不同租户互不干扰。
- 详细挂载与 preset 说明见 `docs/guides/tools.zh.md`；多租户见 `docs/guides/multi-tenant.zh.md`。
