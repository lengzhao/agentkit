---
name: agentkit-config
description: 维护已部署 AgentKit 进程的运行时配置（config.yaml、preset、workspace、llm、tools、mcp.json、api.json、agents、skills、.env）。在修改上述配置、解释 L0/L1 覆盖与 workspace 路径、或需提醒用户重启后生效时使用。
---

# AgentKit 配置指南

## 运行态约定

本 Skill 面向**已编译、正在运行的 AgentKit 进程**。配置在**进程启动时**加载并装配实例图；运行中**不支持热更新**。

改完任何配置后，必须**请用户重启 agent 进程**，新配置才会生效。不要声称「已生效」或尝试调用尚不可用的工具，直到用户确认已重启。

可改的运行时文件（路径经 workspace 解析）：

| 类型 | 典型路径 |
|---|---|
| L1 override | `config.yaml`（工作目录或启动时指定） |
| MCP | 默认仅 `global:mcp.json` → `~/.agentkit/mcp.json`；`enableLocal: true` 后另加载 `local:mcp.json` → `.agentkit/mcp.json` |
| OpenAPI | `local:api.json`、`api/*.json` |
| 子 agent | `local:agents/*.md`、`global:agents/*.md` |
| Skills | `local:skills/`、`global:skills/`、`local:../skills/` |
| 密钥 | `local:.env` 或进程环境变量 |

Preset（`-config presets/...`）在**启动参数**里指定，改 preset 同样需要改启动命令并重启。

## 配置模型

两层 YAML 在启动时合并，由 `pluginkit/build` 构造实例图，root 为 `runner.default`：

| 层 | 来源 | 说明 |
|---|---|---|
| L0 | 随二进制发布的 `config.base.yaml` | 默认实例图；实例 id 以 `.default` 结尾 |
| L1 | `config.yaml` 或 `-config` 路径 | 用户 override；同 id 与 L0 **深合并**（`use` 变更时整颗替换） |

合并规则：

1. 先 L0，再按顺序合并 L1 overlay；`-config` 逗号分隔多文件时，**后面的覆盖前面的**。
2. **实例级**：L1 与 L0 同 id 且 `use` 不变时，`config` / `deps` 递归深合并；`use` 变更则整颗节点替换。
3. **字段级**（深合并时）：标量覆盖；列表整体覆盖；`key+: [...]` 追加；`key-: [...]` 按值删减列表元素；`key: null` 删除 map 键。同一 overlay 内顺序：覆盖 → `+` → `-`。
4. 节点可 `extends: other.instance.id` 继承另一实例（YAML 层展开，需无环）。

## 推荐工作流

1. **确认目标**：要改模型、工具、MCP、OpenAPI、prompt、子 agent 还是 workspace？
2. **定位文件**：按下方各节找到对应磁盘路径；需要改 YAML 实例时，先弄清当前生效的 `config.yaml` / preset 里写了什么。
3. **写盘**：用 read / edit / write 修改配置文件。
4. **请用户重启**：明确告知改了哪些文件、重启后预期变化；不要自行假设已生效。
5. **重启后验证**：请用户发一条探测消息，或检查预期工具是否出现在工具列表中。

## 常用实例 id

改 `config.yaml` 时按 id 整颗替换对应实例：

| 实例 id | 作用 | 典型 override |
|---|---|---|
| `workspace.default` | global/local 根与 scope | `scope: local` |
| `llm.default` | 模型与 API 端点 | `model`、`baseUrl`、`apiKeyRef` |
| `agent.assistant.default` | 主 agent | `model`、`maxSteps`、`retry` |
| `tools.default` | 工具集与策略 | 增删 `deps.tools`、挂 `dynamicTools` |
| `prompt.default` | system prompt 拼装 | 调整 `deps.sections` |
| `skills.default` | skill 目录 | `dirs` 叠加路径 |
| `sessionStore.default` | 会话持久化 | `dir`、`maxLoadedEvents` |
| `runner.default` | 并发与 session 语义 | `sessionScope`、`maxConcurrentTurns`、`inject`、`defaultTimezone` |
| `platform.default` | 入口形态 | `platform/cli`、`platform/worker` 等 |
| `credentials.default` | 密钥解析 | `.env` 文件列表 |
| `mcp.default` | MCP 配置文件列表 | `files` 路径；`enableLocal` 开启 local |
| `openapi.default` | OpenAPI 索引文件列表 | `files` 路径 |

## Workspace 路径

`workspace/default` 定义两个根：

- **global**：`~/.agentkit`（跨项目共享）
- **local**：`<cwd>/.agentkit`（项目/租户私有）

通过 `scope` 选择默认根（常见 preset 用 `local`）。

| 写法 | 含义 |
|---|---|
| `sessions` | 相对当前 scope 默认根 |
| `global:skills` | `~/.agentkit/skills` |
| `local:skills` | `<cwd>/.agentkit/skills` |
| `local:../skills` | `<cwd>/skills`（随仓库提交） |
| `..` | 项目根 |
| `work` | scope 根下的 `work/` 子目录 |

文件工具与 shell 的读写根由 `tool.fs-workspace.default.config.root` / `tool.shell-bash.default.config.workDir` 决定（常见 coding 场景为 `..` 即项目根；多租户为 `work` 临时产物目录）。`AGENTS.md`、`memory.md` 等租户级文件落在 local 根；`root: work` 时通过 `tenantFiles` 仍可由工具读写。

Skills 目录叠加：`dirs: [local:../skills, local:skills, global:skills]`，先命中者优先。

## 最小 override 示例

### IM 多用户 inject

```yaml
runner.default:
  config:
    inject:
      - sender_id
      - sender_name
      - platform
      - chat_id
      - timestamp
      - task_id
    defaultTimezone: Asia/Shanghai
```

内置项：`sender_id`、`sender_name`、`sender_email`、`platform`、`chat_id`、`timestamp`、`task_id`、`trace_id`、`language`、`custom.*`，或任意 `MessageEvent.Metadata` 键名。

### 切换模型

```yaml
llm.default:
  use: llm/openai-compatible
  config:
    model: gpt-5.4
    baseUrl: https://api.openai.com/v1
    apiKeyRef: env:OPENAI_API_KEY
  deps:
    credentials: credentials.default
```

### 项目 coding

```yaml
workspace.default:
  use: workspace/default
  config:
    scope: local

tool.fs-workspace.default:
  use: tool/fs-workspace
  config:
    root: ..
    maxBytes: 1048576
  deps:
    workspace: workspace.default

tool.shell-bash.default:
  use: tool/shell-bash
  config:
    workDir: ..
    timeoutSeconds: 60
  deps:
    workspace: workspace.default
```

### 自定义 system prompt

```yaml
prompt.default:
  use: prompt/assembler/default
  deps:
    sections:
      - prompt.custom.default
      - prompt.agents-md.default
      - prompt.skills.default
      - prompt.subagents.default

prompt.custom.default:
  use: prompt/section/static
  config:
    name: custom-instructions
    content: |
      你是一名资深 Go 工程师。
      - 优先最小化 diff
      - 修改后运行测试再声称完成
```

## MCP 动态工具

YAML 实例 `mcp.default` 在启动时读取磁盘上的 `mcp.json` 并注册动态工具。

**文件布局**：顶层 `mcpServers`，格式与 Cursor 一致。默认只加载 `global:mcp.json`（`~/.agentkit/mcp.json`）。设 `enableLocal: true` 后另加载 `local:mcp.json`（`.agentkit/mcp.json`）。

先命中者赢（按 server 名去重）。部分部署可能配置了额外路径，以 `mcp.default.config.files` 为准。

**工作流**：修改 `mcp.json` → **请用户重启 agent**。

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
    "remote": {
      "url": "http://127.0.0.1:8080/mcp",
      "type": "sse"
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `command` / `args` | stdio 子进程 MCP server |
| `env` | 环境变量；值可为 `env:NAME` |
| `url` | HTTP/SSE 远程端点 |
| `type` | `sse`、`http` / `streamable` |
| `prefix` | 工具名前缀，默认 `<serverName>__` |
| `allowTools` / `denyTools` | 原始 MCP 工具名白/黑名单 |
| `timeoutSeconds` | 单次调用墙钟超时 |

每个 server 须提供 `command`（stdio）或 `url`（远程）之一。动态工具命名：`<prefix><原始工具名>`，如 `github__search`。

## OpenAPI 动态工具

YAML 实例 `openapi.default` 在启动时读取 `api.json` 并注册 HTTP 动态工具。

**文件布局**：

- `api.json`：纯索引，顶层 `apis` 列出每个 API 的 wiring。
- `api/<name>.json`：独立 OpenAPI 3 文档；不含 auth/bind/baseUrl。
- 默认查找：`local:api.json` → `global:api.json`（先命中者赢）。

**工作流**：修改 `api.json` 或 `api/*.json` → **请用户重启 agent**。

```json
{
  "apis": {
    "petstore": {
      "path": "api/petstore.json",
      "baseUrl": "https://petstore.example.com",
      "prefix": "petstore__",
      "auth": { "type": "bearer", "token": "env:PETSTORE_TOKEN" },
      "bind": {
        "uid": { "from": "ctx:user_id", "in": "header", "name": "X-User-Id" }
      },
      "allowOperations": ["getPet"],
      "denyOperations": ["deletePet"],
      "timeoutSeconds": 30
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `path` | 指向 OpenAPI 文档 |
| `baseUrl` | API 根地址；优先于 spec 里的 `servers` |
| `prefix` | 工具名前缀，默认 `<name>__` |
| `auth` | `bearer` / `header` / `query` / `basic`；敏感值用 `env:NAME` |
| `allowOperations` / `denyOperations` | 按 `operationId` 白/黑名单 |
| `bind` | 从 context 注入参数，不暴露给模型 |

**bind 约定**：`from` 须 `ctx:` 前缀；`in` 为 `path`/`query`/`header`；ctx 值为空时不发起请求。动态工具命名：`<prefix><operationId>`，如 `petstore__getPet`。

## 子 Agent

定义文件放在 `local:agents/` 或 `global:agents/`。每个子 agent 一个 `agents/<name>.md`（frontmatter + 指令正文）。主 agent 通过 `delegate` 委派；子 agent 工具集为只读 fs + web。

新增或修改子 agent 定义后**请用户重启 agent**。

## 多租户

多租户部署使用 `workspace/tenant`：

- 每租户独立 local 根（默认 `~/.agentkit/tenants/<租户键>`）
- `AGENTS.md`、`memory.md` 等租户级文件在 local 根；文件工具与 shell 限定在 `work/` 临时产物（`tenantFiles` 例外）
- 共享资源（skills、mcp.json）放 `global:` 路径

## 密钥与敏感信息

- API Key 通过环境变量注入，配置中引用 `env:OPENAI_API_KEY` 等。
- 可从 `local:.env` 加载；改 `.env` 后同样**需要重启**。
- **不要把明文 secret 写进配置文件或提交到 git**。

## Preset 速查

改 preset 需修改进程启动参数（`-config`）并重启：

| preset | 场景 |
|---|---|
| `coding.yaml` | 项目目录交互 coding |
| `autonomous.yaml` | 自主长跑（预算、todo/finish） |
| `worker.yaml` | headless 一次性（需链 `autonomous`） |
| `daemon.yaml` | 固定间隔守护（需链 `autonomous`） |
| `cron.yaml` | 日历 cron + 自主排期（需链 `autonomous`） |
| `web.yaml` | 网络搜索 + 抓取 |
| `multi-tenant.yaml` | 多租户 IM 隔离 |

链式示例：`-config presets/autonomous.yaml,presets/worker.yaml`

## 注意

- 同 id **整颗替换**：改 `deps` 子列表时须写全，漏写会从图中消失。
- 所有配置（YAML、mcp.json、api.json、agents、skills、.env）改完后**必须重启进程**才生效。
- 重启前不要声称配置已生效，也不要调用依赖新配置的工具。
- 密钥用 `env:VAR_NAME` 引用，不写明文 secret。
