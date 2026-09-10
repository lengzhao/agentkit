# Presets

**English** | **[中文](README.zh.md)**

L1 overlays on top of L0 `config.base.yaml`. `-config` accepts comma-separated files merged in order; later files override earlier ones.

```sh
# Single file
go run ./cmd/agent -config presets/coding.yaml "your task"

# Chained (autonomous + headless worker)
go run ./cmd/agent -config presets/autonomous.yaml,presets/worker.yaml "one-shot job"
```

## Index

| File | Scenario | Notes |
|---|---|---|
| [coding.yaml](coding.yaml) | Project coding | `scope: local`; sessions under `.agentkit/sessions` |
| [coding-smoke.yaml](coding-smoke.yaml) | Smoke without API key | Scripted LLM; validates assembly and tools |
| [autonomous.yaml](autonomous.yaml) | Autonomous run | Budgets, todo/finish, auto-allow + policy allowlist |
| [autonomous-smoke.yaml](autonomous-smoke.yaml) | Autonomous smoke | Scripted LLM; turn-continue / todo / finish |
| [worker.yaml](worker.yaml) | Headless one-shot | No stdin; CI / cron friendly; **chain with autonomous** |
| [daemon.yaml](daemon.yaml) | Fixed-interval daemon | `platform/timer`; **chain with autonomous** |
| [cron.yaml](cron.yaml) | Cron daemon | `schedule/cron` + `tool/schedule`; worker only starts tasks |
| [subagent-smoke.yaml](subagent-smoke.yaml) | Sub-agent smoke | Scripted LLM; L0 already mounts subagent |
| [web.yaml](web.yaml) | Web tools | Search, fetch, ask user |
| [web-smoke.yaml](web-smoke.yaml) | Web smoke | Scripted web; **chain with web** |
| [openapi-smoke.yaml](openapi-smoke.yaml) | OpenAPI tools smoke | Scripted LLM + fixture api.json; **HTTP mock: testing/openapitest** |
| [multi-tenant.yaml](multi-tenant.yaml) | Multi-tenant IM | Per-chat dirs; tools under `work/`; **bring your own platform** |
| [p1-context.yaml](p1-context.yaml) | P1 snippet | Copy into custom overlay; not a full stack |

## Integrations

| File | Scenario | Notes |
|---|---|---|
| [slack.yaml](slack.yaml) | Slack bot | Socket Mode + coding; set bot/app tokens |
| [feishu.yaml](feishu.yaml) | Feishu / Lark bot | Event subscription + coding |
| [chat-api.yaml](chat-api.yaml) | HTTP debug API | Chat-style HTTP console |
| [langfuse.yaml](langfuse.yaml) | Langfuse | Trace export overlay |
| [multiplex.yaml](multiplex.yaml) | Multi-IM | `platform/multiplex`; chain with coding; remove platforms without credentials |
| [acp.yaml](acp.yaml) | ACP server | Expose AgentKit over stdio ACP (editors / clients) |
| [acp-remote.yaml](acp-remote.yaml) | ACP remote agent | Run external ACP agent (e.g. Cursor CLI); see plugin-catalog |

Common override snippets (models, prompts, schedule paths, `once: true`) are in [config.example.yaml](../config.example.yaml).

## Commands

```sh
export OPENAI_API_KEY=sk-...

# Interactive REPL + project scope
go run ./cmd/agent -config presets/coding.yaml

# Autonomous (first message on stdin, exit when done)
go run ./cmd/agent -config presets/autonomous.yaml "organize docs and finish"

# Headless batch (safe with /dev/null stdin)
go run ./cmd/agent -config presets/autonomous.yaml,presets/worker.yaml "run tests and report"

# Daemon: fixed interval
go run ./cmd/agent -config presets/autonomous.yaml,presets/daemon.yaml

# Daemon: calendar cron + agent scheduling
go run ./cmd/agent -config presets/autonomous.yaml,presets/cron.yaml

# Smoke without API key
go run ./cmd/agent -config presets/coding-smoke.yaml "list dir and read README"
```

## Task modes: prompt vs script

Each `platform/worker` task uses **one of**:

```yaml
tasks:
  - prompt: "Patrol the workspace; finish when done."
  - id: weekday-morning
    cron: "0 9 * * 1-5"
    prompt: "Weekday morning: run tests, check dirty tree, summarize, finish."
  - id: nightly
    cron: "0 3 * * *"
    script: presets/scripts/nightly.sh
```

For `script` tasks, wire `workspace` and `shell` under `platform.default.deps`; see [cron.yaml](cron.yaml).
