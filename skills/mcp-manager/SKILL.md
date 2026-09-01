---
name: mcp-manager
description: 维护 AgentKit 的 MCP 动态工具配置（mcp.json）。在添加/编辑 mcpServers、配置 env/url/prefix/allowTools、排查 MCP 工具命名或修改后需重启 agent 时使用。
---

# MCP 配置指南

## 运行态约定

MCP 配置在**进程启动时**由 `mcp.default` 读取；运行中不支持热更新。改完 `mcp.json` 后**请用户重启 agent**，不要声称工具已刷新。

## 文件布局

顶层 `mcpServers`，格式与 Cursor 一致。默认查找顺序：

- `local:mcp.json` → `.agentkit/mcp.json`
- `global:mcp.json` → `~/.agentkit/mcp.json`

先命中者赢（按 server 名去重）。部分 preset 可能叠加其他路径，以 `mcp.default.config.files` 为准。路径经 workspace 解析，可用 `local:` / `global:` 前缀。

## 工作流

1. 用 read / edit / write 修改 `mcp.json`（或配置里列出的路径）。
2. 请用户**重启 agent 进程**。
3. 重启后调用 `github__search` 等 `<prefix><原始工具名>` 格式的动态工具。

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
    },
    "agenthub": {
      "transport": "streamable-http",
      "url": "env:AGENTHUB_MCP_URL",
      "headers": {
        "X-agenthub-apikey": "env:AGENTHUB_API_KEY"
      }
    }
  }
}
```

## 字段说明

| 字段 | 说明 |
|---|---|
| `command` / `args` | stdio 子进程 MCP server |
| `env` | 环境变量；值可为 `env:NAME`，经 credentials 解析 |
| `url` | HTTP/SSE 远程端点；值可为 `env:NAME` |
| `type` | `sse`、`http` / `streamable` / `streamable-http`；留空时先 streamable 再 SSE |
| `transport` | `type` 的别名 |
| `headers` | 每次请求都带上的静态 header；值可为 `env:NAME` |
| `prefix` | 工具名前缀，默认 `<serverName>__` |
| `allowTools` / `denyTools` | 原始 MCP 工具名白/黑名单 |
| `bind` | 从 `context` 透传到 MCP server（`header` / `meta` / `env`）；见 `docs/guides/tools.zh.md` |
| `timeoutSeconds` | 单次调用墙钟超时 |

每个 server 须提供 `command`（stdio）或 `url`（远程）之一。

## 动态工具命名

`<prefix><原始MCP工具名>`，例如 `github__search`。模型看到的是各 MCP server 的原生 input schema。

## 注意

- 密钥用 `env:VAR_NAME` 引用，不写明文 secret。
- 单个 server 连不上时跳过并 `slog.Warn`，不影响其他 server。
- 多租户下 MCP 客户端池按 `(租户键, server 名)` 分槽。
