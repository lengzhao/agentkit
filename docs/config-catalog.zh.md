<!-- 由 scripts/gen-config-catalog 生成——请勿手工编辑。
     运行 `go run ./scripts/gen-config-catalog` 重新生成；CI 以 -check 校验新鲜度。 -->

# 插件配置目录

每个 kind 对应 `use: <kind>` 可引用的插件类型。config 表列出字段名、类型、默认值
（来自插件 Config 的 SetDefaults）与字段注释；deps 表列出依赖注入点。字段级校验规则
（Validate）在装配期生效，可用 `agent config validate` 在不构造实例的情况下检查配置。

## `agent/acp-remote`

- 返回类型：`agentkit.Agent`
- 源码：[`plugins/agent/acpremote/acpremote.go`](../plugins/agent/acpremote/acpremote.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `id` | `agentkit.AgentID` | `acp` | ID is the agent id referenced by loop.defaultAgent. |
| `command` | `[]string` | — | Command is the subprocess to spawn, e.g. ["agent", "acp"] (Cursor CLI) or ["npx", "-y", "@zed-industries/claude-code-acp@latest"]. |
| `env` | `map[string]string` | — | Env adds environment variables for the subprocess. |
| `cwd` | `string` | — | Cwd is the working directory for the ACP subprocess (cmd.Dir) and session/new. Empty uses workspace work/ (same default as tool/shell-bash). |
| `releaseSubprocessAfterTurn` | `bool` | — | ReleaseSubprocessAfterTurn kills the ACP process group after each RunTurn so Cursor child processes (language servers, worker-server) do not accumulate. |
| `autoApprove` | `bool` | — | AutoApprove automatically grants tool permission requests without prompting. |
| `authMethod` | `string` | — | AuthMethod is passed to authenticate when non-empty (e.g. "cursor_login"). |
| `clientName` | `string` | `agentkit` | ClientName is sent in initialize; defaults to "agentkit". |
| `clientVersion` | `string` | `0.1.0` | ClientVersion is sent in initialize; defaults to "0.1.0". |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `fs`（必填） | `filesystem.Service` | FS stores ACP session resume binds (tenant-relative acp/ dir). |
| `sessionStore` | `agentkit.SessionStore` |  |
| `sessionEvents` | `session.Conversation` |  |
| `sessionMcp` | `acp.SessionMCPProvider` | SessionMCP supplies harness MCP servers for session/new (not project mcp.json). |
| `telemetry`（必填） | `telemetry.Toolkit` |  |
| `sandbox` | `sandbox.Service` | Sandbox wraps the ACP subprocess command with the tenant sandbox view (e.g. sandbox/bwrap) at spawn time. Nil defaults to capsandbox.Disabled() (passthrough). |

## `agent/catalog-commands`

- 返回类型：`agentkit.CommandProvider`
- 源码：[`runtime/command/catalog_commands.go`](../runtime/command/catalog_commands.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `loop`（必填） | `agentkit.AgentCatalogLoop` |  |
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `workspace`（必填） | `workspace.Service` |  |
| `subagents` | `capsubagent.Spawner` | 可选：启用 `/model -g sub` 的子 Agent 名校验与总览列表 |
| `llm` | `agentkit.LLMProvider` | 可选（建议 `llm/router`）：提供模型目录，`/model` show 附 `available` 行、set 未知模型给非阻断警告 |

## `agent/chain`

- 返回类型：`agentkit.Agent`
- 源码：[`plugins/agent/chain/chain.go`](../plugins/agent/chain/chain.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `id` | `agentkit.AgentID` | — | ID is the chain agent id referenced by loop.defaultAgent or /agent use. |
| `nodes` | `[]agentkit.AgentID` | — | Nodes lists node agent ids (from deps.agents) executed in order for every turn. |
| `continueOnError` | `bool` | — | ContinueOnError keeps running later nodes after a node fails; default aborts the chain. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `agents`（必填） | `[]agentkit.Agent` |  |

## `agent/coding`

- 返回类型：`agentkit.Agent`
- 源码：[`runtime/agent/agent.go`](../runtime/agent/agent.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `id` | `agentkit.AgentID` | `coding` | ID is agent id, referenced by loop.defaultAgent. |
| `model` | `string` | — | Model is model name passed to the LLM provider. |
| `modalities` | `[]string` | — | Modalities overrides provider input modalities when set (e.g. vision subagent with modalities: [image]). |
| `retry` | `*agent.RetryConfig` | — | Retry is per-step retry for transient provider failures. |
| `maxSteps` | `*int` | — | MaxSteps caps model steps per turn (all segments). Omitted defaults to 200; explicit 0 disables the cap. |
| `maxPromptTokens` | `int` | — | MaxPromptTokens is the pre-send guard: when the assembled prompt (system prompt + hydrated history, inline media counted at real size) exceeds it, force-run compaction once before the LLM call; if it still exceeds, fail the step instead of sending a request the provider will reject. 0 disables. |
| `toolExecution` | `agentkit.ToolExecutionMode` | — | ToolExecution is how multiple tool calls in one assistant step run together. Omitted defaults to parallel (pi agent-loop default). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `llm`（必填） | `agentkit.LLMProvider` |  |
| `tools`（必填） | `agentkit.ToolRuntime` |  |
| `prompt`（必填） | `agentkit.PromptAssembler` |  |
| `policies` | `[]agentkit.Policy` |  |
| `hooks` | `agentkit.HookRuntime` |  |
| `compaction` | `[]compaction.Service` |  |
| `workspace` | `workspace.Service` |  |

## `approval/auto-allow`

- 返回类型：`agentkit.Approval`
- 源码：[`plugins/approval/autoallow.go`](../plugins/approval/autoallow.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `reason` | `string` | `auto-allowed: unattended run` | Reason is text recorded on each decision; defaults to "auto-allowed: unattended run". |

## `approval/auto-deny`

- 返回类型：`agentkit.Approval`

无 config 字段。

## `bootstrap/shell`

- 返回类型：`agentkit.AppInitializer`
- 源码：[`plugins/bootstrap/shell.go`](../plugins/bootstrap/shell.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `workDir` | `string` | `local:.` | WorkDir is the workspace-relative working directory. Default local:.. |
| `commands` | `[]string` | — | Commands run sequentially via bash -lc at app start. |
| `timeoutSeconds` | `int` | `60` | TimeoutSeconds bounds each command; 0 uses 60s. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |

## `commands/registry`

- 返回类型：`agentkit.Commands`
- 源码：[`runtime/command/registry.go`](../runtime/command/registry.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `allow` | `[]string` | — | Allow exposes only these command names; empty means all. |
| `deny` | `[]string` | — | Deny hides these command names; applied after Allow. |
| `admins` | `[]string` | — | Admins lists user IDs that may run admin-only slash commands. Matching is case-insensitive. When empty, admin restrictions are not enforced. |
| `adminOnly` | `[]string` | — | AdminOnly lists command names and aliases that require an admin user when Admins is non-empty. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionCommands` | `agentkit.CommandProvider` | SessionCommands pulls session lifecycle slash commands into the build graph. |

## `compaction/chain`

- 返回类型：`compaction.Chain`

无 config 字段。

## `compaction/pipeline`

- 返回类型：`compaction.Service`

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `chain`（必填） | `compaction.Chain` |  |
| `services`（必填） | `[]compaction.Service` |  |

## `compaction/prune-tool-results`

- 返回类型：`compaction.Service`
- 源码：[`runtime/compaction/prune_plugin.go`](../runtime/compaction/prune_plugin.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxToolResultBytes` | `int` | `8192` | MaxToolResultBytes is per-result truncation limit. |

## `compaction/summary`

- 返回类型：`compaction.Service`
- 源码：[`runtime/compaction/summary_plugin.go`](../runtime/compaction/summary_plugin.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `minMessages` | `int` | — | MinMessages is an optional pre-gate when Force is false. Zero disables it. |
| `keepRecentTokens` | `int` | `20000` | KeepRecentTokens is the recent context budget kept verbatim (Pi-compatible). |
| `reserveTokens` | `int` | `16384` | ReserveTokens caps summarization output size. |
| `keepRecent` | `int` | — | KeepRecent is deprecated; use keepRecentTokens. |
| `summaryModel` | `string` | — | SummaryModel is model used for the summary; defaults to the agent's model. |
| `maxInputTokens` | `int` | `400000` | MaxInputTokens bounds the summarization request input (oldest messages are dropped first, single messages truncated). Zero uses the default. |
| `summaryPrompt` | `string` | `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.` | SummaryPrompt overrides the built-in summarisation instruction. |
| `retry` | `*compaction.RetryConfig` | — |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `llm`（必填） | `agentkit.LLMProvider` |  |
| `sessionEvents`（必填） | `session.Compaction` |  |

## `compaction/token-limit`

- 返回类型：`compaction.Service`
- 源码：[`plugins/compaction/tokenlimit.go`](../plugins/compaction/tokenlimit.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxTokens` | `int` | — | MaxTokens is absolute trigger; takes precedence over ContextWindow. |
| `contextWindow` | `int` | — | ContextWindow is model context size; the trigger becomes ContextWindow × TriggerRatio. |
| `triggerRatio` | `float64` | `0.7` | TriggerRatio is fraction of ContextWindow that trips compaction, default 0.7 — leaving room for the reply plus the next tool result. |
| `charsPerToken` | `int` | `4` | CharsPerToken calibrates the fallback estimate used before the provider reports usage; default 4 (English prose), lower it for CJK. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `chain`（必填） | `compaction.Chain` |  |
| `services`（必填） | `[]compaction.Service` | Services run only once the threshold is crossed, with Force set. |

## `credentials/env`

- 返回类型：`credentials.Store`
- 源码：[`plugins/credentials/credentials.go`](../plugins/credentials/credentials.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `prefix` | `string` | — |  |
| `env` | `map[string]string` | — |  |
| `files` | `[]string` | — |  |
| `encryptedFile` | `string` | — |  |
| `secretsKey` | `string` | — | SecretsKey is the AES master key for encryptedFile (post-interpolation). |
| `processEnv` | `*bool` | — | ProcessEnv enables os.Getenv lookup (static default true; integrations default false). |
| `manifestFiles` | `[]string` | — | ManifestFiles lists workspace paths scanned for per-scope env: allowlists (integrations only). |
| `scopedEnv` | `map[string]map[string]string` | — | ScopedEnv preloads integration secrets per scope (L1 config); enc /env add overrides same key. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` | FS reads/writes dotenv and encrypted secrets files (scope prefixes allowed). |

## `credentials/integrations`

- 返回类型：`credentials.Store`
- 源码：[`plugins/credentials/credentials.go`](../plugins/credentials/credentials.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `prefix` | `string` | — |  |
| `env` | `map[string]string` | — |  |
| `files` | `[]string` | — |  |
| `encryptedFile` | `string` | — |  |
| `secretsKey` | `string` | — | SecretsKey is the AES master key for encryptedFile (post-interpolation). |
| `processEnv` | `*bool` | — | ProcessEnv enables os.Getenv lookup (static default true; integrations default false). |
| `manifestFiles` | `[]string` | — | ManifestFiles lists workspace paths scanned for per-scope env: allowlists (integrations only). |
| `scopedEnv` | `map[string]map[string]string` | — | ScopedEnv preloads integration secrets per scope (L1 config); enc /env add overrides same key. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` | FS reads/writes dotenv and encrypted secrets files (scope prefixes allowed). |

## `delivery/assistant`

- 返回类型：`delivery.Assistant`

无 config 字段。

## `filesystem/local`

- 返回类型：`filesystem.Service`
- 源码：[`runtime/filesystem/local.go`](../runtime/filesystem/local.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | `.` | Root is directory relative to the workspace root; may use global: or local: prefix. |
| `unrestricted` | `bool` | — | Unrestricted disables path confinement to Root (absolute paths and .. are allowed). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |

## `filesystem/sandbox`

- 返回类型：`filesystem.Service`
- 源码：[`runtime/filesystem/sandbox.go`](../runtime/filesystem/sandbox.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |
| `sandbox`（必填） | `sandbox.Service` |  |

## `hook/background-review`

- 返回类型：`agentkit.HookProvider`
- 源码：[`plugins/learning/background_review.go`](../plugins/learning/background_review.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `enabled` | `*bool` | — |  |
| `model` | `string` | — |  |
| `maxSteps` | `int` | — |  |
| `maxDigestMessages` | `int` | — |  |
| `skipSlashOnly` | `*bool` | — |  |
| `minTurnTokens` | `int` | — |  |
| `maxReviewsPerDay` | `int` | — |  |
| `minIdleSeconds` | `int` | — |  |
| `memoryNotifications` | `string` | — | off \| on \| verbose (Hermes display.memory_notifications) |
| `memoryNudgeInterval` | `*int` | — | MemoryNudgeInterval user turns between automatic memory reviews (Hermes memory.nudge_interval). 0 disables; default 10. |
| `sessionRecall` | `*bool` | — | SessionRecall when false, skip FTS prior-session block in the review digest (use tool/session-search instead). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `learning`（必填） | `learning.reviewLearning` |  |
| `memory`（必填） | `memory.Capture` |  |
| `llm`（必填） | `agentkit.LLMProvider` |  |
| `sessionIndex` | `sessionindex.Service` |  |
| `sender` | `delivery.Sender` |  |
| `delivery` | `delivery.Assistant` |  |

## `hook/before-step`

- 返回类型：`agentkit.HookProvider`
- 源码：[`plugins/hook/beforestep.go`](../plugins/hook/beforestep.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `contributeCommands` | `bool` | — | ContributeCommands registers /compact on the shared commands registry. Only one hook/before-step instance should enable this (typically the default agent). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `compaction`（必填） | `compaction.Service` | Compaction is typically compaction.pipeline.* (chain + inner services wired in config). |
| `sessionStore`（必填） | `agentkit.SessionStore` |  |

## `hook/session-index`

- 返回类型：`agentkit.HookProvider`
- 源码：[`plugins/hook/sessionindex.go`](../plugins/hook/sessionindex.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `index`（必填） | `sessionindex.Service` |  |

## `hook/turn-continue`

- 返回类型：`agentkit.HookProvider`
- 源码：[`plugins/hook/turncontinue.go`](../plugins/hook/turncontinue.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxContinuations` | `int` | — | MaxContinuations is the most segments this hook will ask for after the first. |
| `continuePrompt` | `string` | `Keep going on the task. Review the remaining work, do the next concrete step, and call finish when everything is done or you are blocked.` | ContinuePrompt is text injected to start another segment. |
| `requireFinish` | `*bool` | — | RequireFinish keeps going until tool/finish is called, even with no pending todos. |
| `requireTodosDone` | `*bool` | — | RequireTodosDone keeps going while todos are still pending. |
| `stallLimit` | `int` | `3` | StallLimit stops after this many repeats of the same tool call signature. |
| `noProgressLimit` | `int` | `3` | NoProgressLimit stops after this many consecutive continuations with no substantive progress — no new tool/call, todo/update, or run/finish event (default 3). Guards against idle loops when tool/finish is missing or the model keeps ending segments with text-only replies. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |

## `hooks/runtime`

- 返回类型：`agentkit.HookRuntime`
- 源码：[`runtime/hooks/runtime.go`](../runtime/hooks/runtime.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `providers` | `[]agentkit.HookProvider` |  |

## `learning/default`

- 返回类型：`*learning.Service`
- 源码：[`plugins/learning/service.go`](../plugins/learning/service.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `disabled` | `bool` | — |  |
| `sessionsDir` | `string` | `sessions` |  |
| `dreaming` | `dreaming.Config` | — |  |
| `workshop` | `workshop.Config` | — |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `fs`（必填） | `filesystem.Service` |  |
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `memory`（必填） | `memory.Service` |  |
| `engine`（必填） | `schedule.Engine` |  |

## `learning/dream-sweep`

- 返回类型：`schedule.Runtime`
- 源码：[`plugins/learning/dream_sweep.go`](../plugins/learning/dream_sweep.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `pollSeconds` | `int` | `60` |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `learning`（必填） | `learning.DreamSweepScheduler` |  |

## `llm/fallback`

- 返回类型：`agentkit.LLMProvider`
- 源码：[`runtime/llm/fallback.go`](../runtime/llm/fallback.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `models` | `[]string` | — | Models is the full ordered model chain when sharing one provider or pairing with deps.fallbacks. |
| `fallbackModels` | `[]string` | — | FallbackModels are tried after the request model (from agent config) on the shared provider. |
| `fallbackOn` | `string` | — | FallbackOn selects which errors trigger failover: retryable (default), quota, or any. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `provider` | `agentkit.LLMProvider` |  |
| `fallbacks` | `[]agentkit.LLMProvider` |  |

## `llm/openai-chat`

- 返回类型：`agentkit.LLMProvider`
- 源码：[`runtime/llm/openai.go`](../runtime/llm/openai.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `model` | `string` | `gpt-4o` | Model is model name. |
| `baseUrl` | `string` | `https://api.openai.com/v1` | BaseURL is API base URL, e.g. https://api.openai.com/v1. |
| `apiKey` | `string` | — | APIKey is inline key. Prefer APIKeyRef so the secret stays out of the config file. |
| `apiKeyRef` | `string` | — | APIKeyRef is credentials reference, e.g. env:OPENAI_API_KEY. |
| `api` | `string` | — | API is chat or responses. |
| `hostedTools` | `[]llm.HostedToolConfig` | — | HostedTools are OpenAI Responses API built-in tools (e.g. web_search) executed server-side. Requires api: responses. |
| `reasoning` | `*llm.OpenAIReasoningConfig` | — | Reasoning is reasoning effort and summary settings, for models that support them. |
| `retry` | `*llm.LLMRetryConfig` | — | Retry is provider-level retry, separate from the agent's per-step retry. |
| `timeoutSeconds` | `int` | `180` | TimeoutSeconds is max wait for the first model event on a stream (TTFB). 0 uses 180s. Later stream tokens are uncapped. HTTP connect/headers use ResponseHeaderTimeoutSeconds. |
| `responseHeaderTimeoutSeconds` | `int` | `60` | ResponseHeaderTimeoutSeconds caps connect + TLS + HTTP response headers. 0 uses 60s. Independent of timeoutSeconds (headers often return before the first model token). |
| `modalities` | `[]string` | — | Modalities lists supported user input modalities: text, image, audio. Empty defaults to text and image. Use [text] for models that reject vision input. Used as default for config.models[] entries that omit modalities. |
| `models` | `[]llm.ModelCatalogEntry` | — | Models lists routable model ids for llm/router (optional). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `llm/openai-compatible`

- 返回类型：`agentkit.LLMProvider`
- 源码：[`runtime/llm/openai.go`](../runtime/llm/openai.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `model` | `string` | `gpt-4o` | Model is model name. |
| `baseUrl` | `string` | `https://api.openai.com/v1` | BaseURL is API base URL, e.g. https://api.openai.com/v1. |
| `apiKey` | `string` | — | APIKey is inline key. Prefer APIKeyRef so the secret stays out of the config file. |
| `apiKeyRef` | `string` | — | APIKeyRef is credentials reference, e.g. env:OPENAI_API_KEY. |
| `api` | `string` | — | API is chat or responses. |
| `hostedTools` | `[]llm.HostedToolConfig` | — | HostedTools are OpenAI Responses API built-in tools (e.g. web_search) executed server-side. Requires api: responses. |
| `reasoning` | `*llm.OpenAIReasoningConfig` | — | Reasoning is reasoning effort and summary settings, for models that support them. |
| `retry` | `*llm.LLMRetryConfig` | — | Retry is provider-level retry, separate from the agent's per-step retry. |
| `timeoutSeconds` | `int` | `180` | TimeoutSeconds is max wait for the first model event on a stream (TTFB). 0 uses 180s. Later stream tokens are uncapped. HTTP connect/headers use ResponseHeaderTimeoutSeconds. |
| `responseHeaderTimeoutSeconds` | `int` | `60` | ResponseHeaderTimeoutSeconds caps connect + TLS + HTTP response headers. 0 uses 60s. Independent of timeoutSeconds (headers often return before the first model token). |
| `modalities` | `[]string` | — | Modalities lists supported user input modalities: text, image, audio. Empty defaults to text and image. Use [text] for models that reject vision input. Used as default for config.models[] entries that omit modalities. |
| `models` | `[]llm.ModelCatalogEntry` | — | Models lists routable model ids for llm/router (optional). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `llm/openai-responses`

- 返回类型：`agentkit.LLMProvider`
- 源码：[`runtime/llm/openai.go`](../runtime/llm/openai.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `model` | `string` | `gpt-4o` | Model is model name. |
| `baseUrl` | `string` | `https://api.openai.com/v1` | BaseURL is API base URL, e.g. https://api.openai.com/v1. |
| `apiKey` | `string` | — | APIKey is inline key. Prefer APIKeyRef so the secret stays out of the config file. |
| `apiKeyRef` | `string` | — | APIKeyRef is credentials reference, e.g. env:OPENAI_API_KEY. |
| `api` | `string` | — | API is chat or responses. |
| `hostedTools` | `[]llm.HostedToolConfig` | — | HostedTools are OpenAI Responses API built-in tools (e.g. web_search) executed server-side. Requires api: responses. |
| `reasoning` | `*llm.OpenAIReasoningConfig` | — | Reasoning is reasoning effort and summary settings, for models that support them. |
| `retry` | `*llm.LLMRetryConfig` | — | Retry is provider-level retry, separate from the agent's per-step retry. |
| `timeoutSeconds` | `int` | `180` | TimeoutSeconds is max wait for the first model event on a stream (TTFB). 0 uses 180s. Later stream tokens are uncapped. HTTP connect/headers use ResponseHeaderTimeoutSeconds. |
| `responseHeaderTimeoutSeconds` | `int` | `60` | ResponseHeaderTimeoutSeconds caps connect + TLS + HTTP response headers. 0 uses 60s. Independent of timeoutSeconds (headers often return before the first model token). |
| `modalities` | `[]string` | — | Modalities lists supported user input modalities: text, image, audio. Empty defaults to text and image. Use [text] for models that reject vision input. Used as default for config.models[] entries that omit modalities. |
| `models` | `[]llm.ModelCatalogEntry` | — | Models lists routable model ids for llm/router (optional). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `llm/router`

- 返回类型：`agentkit.LLMProvider`

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `protocols`（必填） | `[]agentkit.LLMProvider` |  |
| `default`（必填） | `agentkit.LLMProvider` |  |

## `llm/scripted`

- 返回类型：`agentkit.LLMProvider`
- 源码：[`runtime/llm/scripted.go`](../runtime/llm/scripted.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `model` | `string` | `scripted` | Model is name reported back to callers. |
| `steps` | `[]llm.ScriptedStep` | — | Steps are replies in order: text, tool calls, or both. |

## `loop/default`

- 返回类型：`agentkit.Loop`
- 源码：[`runtime/loop/loop.go`](../runtime/loop/loop.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `defaultAgent` | `agentkit.AgentID` | — | DefaultAgent is agent id used when the event names none; defaults to the single configured agent. |
| `followUpMode` | `agentkit.FollowUpMode` | `one-at-a-time` | FollowUpMode is how follow-up messages are drained after a turn ends. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `agents`（必填） | `[]agentkit.Agent` |  |
| `telemetry` | `telemetry.Exporter` |  |

## `memory/default`

- 返回类型：`*memory.Service`
- 源码：[`plugins/memory/service.go`](../plugins/memory/service.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `disabled` | `bool` | — |  |
| `charLimit` | `int` | — |  |
| `memoryRoot` | `string` | `.` |  |
| `memoryFile` | `string` | `memory.md` |  |
| `review` | `memory.ReviewConfig` | — |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |

## `platform/acp`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/acp/platform.go`](../runtime/platform/acp/platform.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `agentName` | `string` | — | AgentName is reported in initialize (default "agentkit"). |
| `agentVersion` | `string` | — | AgentVersion is reported in initialize (default "0.1.0"). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace` | `workspace.Service` |  |

## `platform/chat-api`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/chatapi/chatapi.go`](../runtime/platform/chatapi/chatapi.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `AgentRoutingConfig` | `common.AgentRoutingConfig` | — |  |
| `listenAddr` | `string` | — |  |
| `registerOnly` | `bool` | — | RegisterOnly mounts routes on http.DefaultServeMux and does not listen. Use with platform/http (or any plugin that serves DefaultServeMux). listenAddr "-" is an alias for registerOnly. |
| `path` | `string` | `/v1/` |  |
| `apiToken` | `string` | — |  |
| `userHeader` | `string` | `X-Chat-API-User` |  |
| `userNameHeader` | `string` | `X-Chat-API-User-Name` |  |
| `userEmailHeader` | `string` | `X-Chat-API-User-Email` |  |
| `channelHeader` | `string` | `X-Chat-API-Channel` |  |
| `metadataHeaders` | `[]string` | — | MetadataHeaders lists HTTP headers copied into MessageEvent.Metadata. |
| `corsOrigins` | `[]string` | — |  |
| `requestTimeout` | `string` | `30m0s` |  |
| `interactionTimeout` | `string` | `10m0s` |  |
| `interactive` | `*bool` | — | Interactive enables permission/ask_user prompts via SSE (default true when unset). |
| `busyPolicy` | `string` | `queue` |  |
| `maxRuns` | `int` | — |  |
| `maxUploadSize` | `int64` | — |  |
| `publicBaseUrl` | `string` | — |  |
| `debugUi` | `bool` | — |  |
| `sessionsDir` | `string` | — |  |
| `agents` | `[]string` | — | Agents lists selectable agent ids for debug UI and request validation. |
| `admins` | `[]string` | — | Admins lists user IDs allowed to upload/download files outside work/ (absolute paths, global:/local: scopes, and other workspace paths). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `agentkit.Commands` |  |
| `sessionStore` | `agentkit.SessionStore` |  |
| `workspace` | `workspace.Service` |  |
| `sessionIndex` | `sessionindex.Service` | SessionIndex optional: list conversations from the same SQLite index as tool/session-query. |
| `agents` | `[]agentkit.Agent` |  |

## `platform/cli`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/cli/cli.go`](../runtime/platform/cli/cli.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `prompt` | `string` | — | Prompt is first message; falls back to the positional command-line arguments. |
| `once` | `bool` | — | Once runs a single turn and exit instead of looping on stdin. |
| `defaultSessionId` | `string` | `cli:default` | DefaultSessionID overrides the stable delivery session key. When empty, CLI uses cli:default and resolves the active conversation via session/store. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `agentkit.Commands` |  |
| `sessionStore` | `agentkit.SessionStore` |  |

## `platform/feishu`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/feishu/agentkit.go`](../runtime/platform/feishu/agentkit.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `AgentRoutingConfig` | `common.AgentRoutingConfig` | — |  |
| `appId` | `string` | — |  |
| `appSecret` | `string` | — |  |
| `domain` | `string` | — |  |
| `allowFrom` | `string` | — |  |
| `allowChat` | `string` | — |  |
| `groupReplyAll` | `bool` | — |  |
| `shareSessionInChannel` | `bool` | — | deprecated: use runner.config.sessionScope |
| `threadIsolation` | `bool` | — |  |
| `replyInThread` | `*bool` | — |  |
| `reactionEmoji` | `string` | `OnIt` |  |
| `doneEmoji` | `string` | `CheckMark` |  |
| `cancelledEmoji` | `string` | `HEARTBROKEN` |  |
| `errorEmoji` | `string` | `CrossMark` |  |
| `groupOnly` | `bool` | — |  |
| `respondToAtEveryoneAndHere` | `bool` | — |  |
| `replyToTrigger` | `*bool` | — |  |
| `resolveMentions` | `bool` | — |  |
| `peerBots` | `map[string]string` | — |  |
| `showThinking` | `*bool` | — |  |
| `showToolProgress` | `*bool` | — |  |
| `asyncSubagentProgressCard` | `*bool` | — |  |
| `enableFeishuCard` | `*bool` | — |  |
| `encryptKey` | `string` | — |  |
| `port` | `string` | — |  |
| `callbackPath` | `string` | — |  |
| `unknownCardAction` | `string` | — | forward (default) \| ignore |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `agentkit.Commands` |  |
| `sessionStore` | `agentkit.SessionStore` |  |
| `workspace` | `workspace.Service` |  |

## `platform/http`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/http/httphost.go`](../runtime/platform/http/httphost.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `listenAddr` | `string` | `:8080` |  |

## `platform/lark`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/feishu/agentkit.go`](../runtime/platform/feishu/agentkit.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `AgentRoutingConfig` | `common.AgentRoutingConfig` | — |  |
| `appId` | `string` | — |  |
| `appSecret` | `string` | — |  |
| `domain` | `string` | — |  |
| `allowFrom` | `string` | — |  |
| `allowChat` | `string` | — |  |
| `groupReplyAll` | `bool` | — |  |
| `shareSessionInChannel` | `bool` | — | deprecated: use runner.config.sessionScope |
| `threadIsolation` | `bool` | — |  |
| `replyInThread` | `*bool` | — |  |
| `reactionEmoji` | `string` | `OnIt` |  |
| `doneEmoji` | `string` | `CheckMark` |  |
| `cancelledEmoji` | `string` | `HEARTBROKEN` |  |
| `errorEmoji` | `string` | `CrossMark` |  |
| `groupOnly` | `bool` | — |  |
| `respondToAtEveryoneAndHere` | `bool` | — |  |
| `replyToTrigger` | `*bool` | — |  |
| `resolveMentions` | `bool` | — |  |
| `peerBots` | `map[string]string` | — |  |
| `showThinking` | `*bool` | — |  |
| `showToolProgress` | `*bool` | — |  |
| `asyncSubagentProgressCard` | `*bool` | — |  |
| `enableFeishuCard` | `*bool` | — |  |
| `encryptKey` | `string` | — |  |
| `port` | `string` | — |  |
| `callbackPath` | `string` | — |  |
| `unknownCardAction` | `string` | — | forward (default) \| ignore |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `agentkit.Commands` |  |
| `sessionStore` | `agentkit.SessionStore` |  |
| `workspace` | `workspace.Service` |  |

## `platform/multiplex`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/multiplex/multiplex.go`](../runtime/platform/multiplex/multiplex.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `platforms`（必填） | `[]agentkit.Platform` |  |

## `platform/slack`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/slack/slack.go`](../runtime/platform/slack/slack.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `AgentRoutingConfig` | `common.AgentRoutingConfig` | — |  |
| `botToken` | `string` | — |  |
| `botTokenRef` | `string` | — |  |
| `appToken` | `string` | — |  |
| `appTokenRef` | `string` | — |  |
| `domain` | `string` | — | optional Slack Web API base URL override |
| `allowFrom` | `string` | — |  |
| `allowChannels` | `string` | — |  |
| `groupReplyAll` | `bool` | — |  |
| `doneEmoji` | `string` | — |  |
| `cancelledEmoji` | `string` | — |  |
| `errorEmoji` | `string` | — |  |
| `unknownCardAction` | `string` | — | forward (default) \| ignore |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `agentkit.Commands` |  |
| `sessionStore` | `agentkit.SessionStore` |  |
| `workspace` | `workspace.Service` |  |
| `credentials` | `credentials.Store` |  |

## `platform/timer`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/headless/timer.go`](../runtime/platform/headless/timer.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `everySeconds` | `int` | — | EverySeconds is the tick interval. Required. |
| `prompt` | `string` | — | Prompt is the task text sent on every tick. Required. |
| `immediate` | `*bool` | — | Immediate fires the first tick at startup instead of waiting a full interval. Defaults to true, so a restart does useful work right away. |
| `maxRuns` | `int` | — | MaxRuns bounds the number of ticks; 0 means run until shutdown. |
| `sessionMode` | `string` | — | SessionMode is fresh (default) or fixed. |
| `sessionId` | `string` | `timer` | SessionID is the id used in fixed mode, and the prefix in fresh mode. |
| `output` | `string` | — | Output is text (default) or json, one event object per line. |
| `stream` | `bool` | — | Stream echoes assistant deltas as they arrive. |

## `platform/worker`

- 返回类型：`agentkit.Platform`
- 源码：[`runtime/platform/headless/worker.go`](../runtime/platform/headless/worker.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `tasks` | `[]headless.TaskSpec` | — | Tasks each run as one turn at startup, in order. Positional command-line arguments override this list. |
| `prompt` | `string` | — | Prompt is the single-task shorthand, used when Tasks is empty. |
| `sessionMode` | `string` | — | SessionMode is fresh (default) or fixed. |
| `sessionId` | `string` | `worker` | SessionID is the id used in fixed mode, and the prefix in fresh mode. |
| `output` | `string` | — | Output is text (default) or json, one event object per line. |
| `stream` | `bool` | — | Stream echoes assistant deltas as they arrive. Off by default: an unattended run wants the result, not the typing. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace` | `workspace.Service` | Workspace resolves script paths. Required when any task uses script. |
| `shell` | `shell.Executor` | Shell runs script tasks. Required when any task uses script. |

## `policy/deny-dangerous-shell`

- 返回类型：`agentkit.Policy`

无 config 字段。

## `policy/path-denylist`

- 返回类型：`agentkit.Policy`
- 源码：[`plugins/policy/pathdenylist.go`](../plugins/policy/pathdenylist.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `deny` | `[]string` | `[.git/** **/.git/** .env **/.env **/.env.* .ssh/** **/.ssh/** **/id_rsa **/*.pem]` | Deny holds glob patterns matched against the path argument; ** spans directories. Empty falls back to DefaultDeniedPaths (.git, .env, .ssh, *.pem). |
| `tools` | `[]string` | — | Tools limits enforcement to these tool names; empty means every tool taking a path. |

## `policy/shell-allowlist`

- 返回类型：`agentkit.Policy`
- 源码：[`plugins/policy/shellallowlist.go`](../plugins/policy/shellallowlist.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `allow` | `[]string` | — | Allow holds command prefixes that may run, e.g. "go test", "git status". |
| `deny` | `[]string` | — | Deny holds command prefixes that never run; checked before Allow. |
| `strict` | `bool` | — | Strict denies anything outside Allow. Without it, unlisted commands fall through to ask, so the approval provider still gets a say. |
| `tool` | `string` | `bash` | Tool is shell tool name to guard; defaults to "bash". |

## `prompt/assembler/default`

- 返回类型：`agentkit.PromptAssembler`
- 源码：[`runtime/prompt/assembler.go`](../runtime/prompt/assembler.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sections` | `[]agentkit.SectionProvider` |  |

## `prompt/section/agents-md`

- 返回类型：`agentkit.SectionProvider`
- 源码：[`plugins/prompt/agentsmd.go`](../plugins/prompt/agentsmd.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | `.` | Root is directory to start the upward search from. |
| `filenames` | `[]string` | `[AGENTS.md AGENTS.MD CLAUDE.md]` | Filenames overrides the default instruction files to search in each directory. Example for Automon runtimes: ["Automon.md", "AUTOMON.md"]. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `fs`（必填） | `filesystem.Service` | FS reads candidate instruction files; wire an unrestricted filesystem/local instance (the upward walk passes absolute host paths). Non-local backends simply miss every candidate and inject nothing. |

## `prompt/section/memory`

- 返回类型：`agentkit.SectionProvider`
- 源码：[`plugins/prompt/memorymd.go`](../plugins/prompt/memorymd.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | — | Root is deprecated (ignored). Use memory.default PromptBody. |
| `filenames` | `[]string` | — | Filenames is deprecated (ignored). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `memory`（必填） | `memory.Reader` |  |

## `prompt/section/skills`

- 返回类型：`agentkit.SectionProvider`
- 源码：[`plugins/prompt/skills.go`](../plugins/prompt/skills.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `skills`（必填） | `skill.Registry` |  |

## `prompt/section/static`

- 返回类型：`agentkit.SectionProvider`
- 源码：[`plugins/prompt/static.go`](../plugins/prompt/static.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `name` | `string` | `static` | Name is section label, used for ordering and debugging. |
| `content` | `string` | — | Content is the prompt text. |

## `prompt/section/subagents`

- 返回类型：`agentkit.SectionProvider`
- 源码：[`plugins/prompt/subagents.go`](../plugins/prompt/subagents.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `subagent`（必填） | `subagent.Spawner` |  |
| `llm` | `agentkit.LLMProvider` |  |

## `runner`

- 返回类型：`agentkit.Runner`
- 源码：[`runtime/runner/runner.go`](../runtime/runner/runner.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `shutdownTimeoutSeconds` | `int` | — | ShutdownTimeoutSeconds bounds how long shutdown waits for in-flight turns to finish on normal exit (e.g. platform EOF). 0 waits indefinitely. SIGINT/SIGTERM use immediate abandon (see shutdownGraceSecondsOnSignal). |
| `shutdownGraceSecondsOnSignal` | `int` | — | ShutdownGraceSecondsOnSignal caps wait after SIGINT/SIGTERM once in-flight turns are cancelled. 0 abandons immediately (default). |
| `sessionScope` | `string` | — | SessionScope collapses platform delivery SessionIDs for Loop locking and history: "channel" (default), "thread", or "user". |
| `maxConcurrentTurns` | `int` | `64` | MaxConcurrentTurns caps how many turns run at once across distinct effective sessions. Defaults to 64. Ordering within one session is always preserved. |
| `inject` | `[]string` | — | Inject lists fields prepended to each inbound user message as [meta sender_id=... timestamp="..." task_id="..." ...]. Built-in tokens: sender_id, sender_name, sender_email, mentions, platform, chat_id, timestamp, task_id, trace_id, language, custom.*, or any Metadata key. L0 config.base.yaml defaults to sender_id, sender_name, timestamp; inject: [] disables. |
| `defaultTimezone` | `string` | — | DefaultTimezone is the fallback IANA timezone for inject timestamp when the platform does not implement UserTimezoneProvider. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `platform`（必填） | `agentkit.Platform` |  |
| `loop`（必填） | `agentkit.Loop` |  |
| `sessionStore` | `agentkit.SessionStore` |  |
| `workspace` | `workspace.Service` |  |
| `schedules` | `[]schedule.Runtime` |  |
| `init` | `[]agentkit.AppInitializer` |  |
| `telemetry` | `telemetry.Exporter` |  |
| `catalogCommands` | `agentkit.CommandProvider` | CatalogCommands pulls agent catalog slash commands (/agent, /acp) into the build graph without routing through commands/registry (which platform depends on). |

## `sandbox/bwrap`

- 返回类型：`sandbox.Service`
- 源码：[`runtime/sandbox/sandbox.go`](../runtime/sandbox/sandbox.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `mode` | `string` | `auto` |  |
| `failIfUnavailable` | `*bool` | — |  |
| `env` | `string` | — | Env marks the deployment environment: "prod"/"production" upgrades mode=auto to fail-closed bwrap, so a misconfig cannot silently run unsandboxed in production. Explicit config, no implicit env detection. |
| `roBinds` | `[]string` | — | RoBinds are extra read-only binds (public read-only paths shared by all tenants; source and target are the same path). Host absolute paths or workspace refs (e.g. "global:share_dir", resolved per call per tenant). |
| `rwBinds` | `[]string` | — | RwBinds are extra read-write binds (public writable paths shared by all tenants; beware concurrent write conflicts). Host absolute paths or workspace refs (missing dirs are created). |
| `hidePaths` | `[]string` | — | HidePaths are masked: directories become invisible and read-only, files are masked empty. Host absolute paths or workspace refs. Applied after all binds, so they can mask sensitive subpaths of system binds or the global root. Resolution fails closed (a failed mask means a leak). |
| `secretFiles` | `[]string` | `[secrets.enc.json]` | SecretFiles are sensitive files under the global root masked empty (relative to the global root), default ["secrets.enc.json"]. Applied after the global ro-bind and ro/rwBinds. |
| `homeRef` | `string` | — | HomeRef selects the in-sandbox HOME. Default "." (the writable tenant root, i.e. workspace "."). Values: a workspace ref ("global:dir", "local:path"), a host absolute path, or the literal "host" (pass the host user's home through, ro-bound — intended for single-tenant trusted setups). The resolved home must live inside the view (tenant root, global root or a configured bind); otherwise rendering fails closed instead of pointing HOME at an unmounted void. |
| `maps` | `[]sandbox.MapConfig` | — | Maps bind a host source onto a different in-sandbox destination, e.g. share the host's ~/.ssh or ~/.gitconfig into the tenant HOME. Applied after ro/rwBinds and before secretFiles/hidePaths (masks always win). |
| `systemBinds` | `[]string` | — | SystemBinds overrides the default minimal system read-only bind list; empty uses the built-in default (/usr /bin /lib /lib64 /opt plus DNS/cert files under /etc). |
| `tmpBase` | `string` | `/dev/shm/shellbwrap` | TmpBase is the base dir on a host tmpfs (default /dev/shm/shellbwrap): each tenant gets a subdir bound rw as in-sandbox /tmp. /dev/shm has its own size cap (usually half of RAM), preventing tmpfs writes from eating node memory. The explicit value "tmpfs" falls back to bwrap's own --tmpfs /tmp (no size limit, not recommended). Must be exclusive per runner instance on a shared host: startup removes leftover contents. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |

## `schedule/cron`

- 返回类型：`schedule.Runtime`
- 源码：[`plugins/schedule/cron.go`](../plugins/schedule/cron.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `jobs` | `[]schedule.CronJobSpec` | — | Jobs are reconciled into the registry on every start with source=config. |
| `sessionMode` | `string` | — | SessionMode is stateless (default), reuse, or fixed. |
| `sessionId` | `string` | `schedule` | SessionID is the id prefix used for inbound turns. |
| `pollSeconds` | `int` | `30` | PollSeconds is the idle backoff when no jobs are scheduled. Defaults to 30. |
| `missedGraceSeconds` | `int` | `300` | MissedGraceSeconds is how long a missed one-shot job is still fired. Older one-shots are marked stale instead of backfilled. Defaults to 300. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `schedule`（必填） | `schedule.Registry` |  |
| `workspace` | `workspace.Service` |  |
| `shell` | `shell.Executor` |  |
| `engine`（必填） | `schedule.Engine` |  |

## `schedule/engine`

- 返回类型：`schedule.Engine`

无 config 字段。

## `schedule/file`

- 返回类型：`schedule.Registry`
- 源码：[`plugins/schedule/file.go`](../plugins/schedule/file.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `path` | `string` | `capschedule.json` | Path is JSON file holding the jobs, resolved through the workspace. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |
| `engine`（必填） | `schedule.Engine` |  |

## `schedule/multi`

- 返回类型：`schedule.Registry`
- 源码：[`plugins/schedule/multi.go`](../plugins/schedule/multi.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `path` | `string` | `global:schedule.json` | Path is the shared schedule file, default global:schedule.json. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |
| `engine`（必填） | `schedule.Engine` |  |

## `session/commands`

- 返回类型：`agentkit.CommandProvider`
- 源码：[`runtime/session/sessstore/commands_plugin.go`](../runtime/session/sessstore/commands_plugin.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |

## `session/events`

- 返回类型：`session.Events`

无 config 字段。

## `session/jsonl`

- 返回类型：`agentkit.Session`
- 源码：[`runtime/session/sessstore/jsonl.go`](../runtime/session/sessstore/jsonl.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `path` | `string` | — | Path is the log file itself, not a directory. |
| `id` | `agentkit.SessionID` | — | ID is fixed session id. |
| `maxLoadedEvents` | `int` | — | MaxLoadedEvents limits non-compaction events kept in memory on load. Zero loads the full file. |

## `session/memory`

- 返回类型：`agentkit.Session`
- 源码：[`runtime/session/sessstore/memory.go`](../runtime/session/sessstore/memory.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `id` | `agentkit.SessionID` | — | ID is fixed session id. |
| `maxToolResultBytes` | `int` | — | MaxToolResultBytes caps tool result text in DeriveMessages (PruneToolResults). |

## `session/postgres`

- 返回类型：`agentkit.SessionStore`
- 源码：[`runtime/session/sessstore/db_store.go`](../runtime/session/sessstore/db_store.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `SQLStoreConfig` | `sessstore.SQLStoreConfig` | `{pgx  0  0}` |  |

## `session/sql`

- 返回类型：`agentkit.SessionStore`
- 源码：[`runtime/session/sessstore/db_store.go`](../runtime/session/sessstore/db_store.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `driver` | `string` | — | Driver is database/sql driver name: sqlite, pgx (postgres), or any registered driver. |
| `dsn` | `string` | — | DSN is the driver data source name (postgres URL, sqlite file URI, etc.). |
| `maxCachedSessions` | `int` | — | MaxCachedSessions limits in-memory hot sessions (LRU). Zero keeps every opened session cached. |
| `cacheIdleTTL` | `string` | — | CacheIdleTTL evicts sessions unused for this duration (for example "30m"). Empty disables idle eviction. |
| `maxLoadedEvents` | `int` | — | MaxLoadedEvents limits non-compaction events kept in memory per session on load. Zero loads the full log. |

## `session/sql-index`

- 返回类型：`sessionindex.Service`
- 源码：[`runtime/session/sessindex/sql_index.go`](../runtime/session/sessindex/sql_index.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `driver` | `string` | — | Driver is database/sql driver name: sqlite, pgx (postgres), or any registered driver. |
| `dsn` | `string` | — | DSN is the driver data source name; should match the session/sql store DSN. |

## `session/sqlite`

- 返回类型：`agentkit.SessionStore`
- 源码：[`runtime/session/sessstore/db_store.go`](../runtime/session/sessstore/db_store.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `SQLStoreConfig` | `sessstore.SQLStoreConfig` | `{sqlite  0  0}` |  |

## `session/sqlite-index`

- 返回类型：`sessionindex.Service`
- 源码：[`runtime/session/sessindex/sqlite_index.go`](../runtime/session/sessindex/sqlite_index.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `indexRel` | `string` | `sessions/.index.sqlite` | IndexRel is the workspace-relative SQLite file path. |
| `sessionsRel` | `string` | `sessions` | SessionsRel is the workspace-relative sessions directory to ingest. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |

## `session/static`

- 返回类型：`agentkit.SessionStore`
- 源码：[`runtime/session/sessstore/static.go`](../runtime/session/sessstore/static.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `session`（必填） | `agentkit.Session` |  |

## `session/store`

- 返回类型：`agentkit.SessionStore`
- 源码：[`runtime/session/sessstore/store.go`](../runtime/session/sessstore/store.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `dir` | `string` | — | Dir is root directory holding one file per session, resolved through the workspace. |
| `maxCachedSessions` | `int` | — | MaxCachedSessions limits in-memory hot sessions (LRU). Zero keeps every opened session cached. |
| `cacheIdleTTL` | `string` | — | CacheIdleTTL evicts sessions unused for this duration (for example "30m"). Empty disables idle eviction. |
| `maxLoadedEvents` | `int` | — | MaxLoadedEvents limits non-compaction events kept in memory per session on load. Zero loads the full file. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |

## `skill/filesystem`

- 返回类型：`skill.Registry`
- 源码：[`plugins/skill/skill.go`](../plugins/skill/skill.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `dirs` | `[]string` | `[global:.cursor/skills global:.agents/skills global:skills]` | Dirs are directories to scan, in precedence order; each may use the global: or local: scope prefix. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |
| `workspace`（必填） | `workspace.Service` |  |

## `subagent/composite`

- 返回类型：`subagent.Spawner`

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `inprocess`（必填） | `subagent.Spawner` |  |
| `loop` | `subagent.Spawner` |  |

## `subagent/inprocess`

- 返回类型：`subagent.Spawner`
- 源码：[`runtime/subagent/inprocess.go`](../runtime/subagent/inprocess.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `dirs` | `[]string` | `[local:agents local:../examples/agents global:agents]` | Dirs are definition directories in precedence order; defaults to local:agents then global:agents. |
| `timeoutSeconds` | `int` | — | TimeoutSeconds is wall clock for one delegation; 0 leaves the delegate tool's own timeout as the only bound. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `llm`（必填） | `agentkit.LLMProvider` |  |
| `tools`（必填） | `agentkit.ToolRuntime` |  |
| `prompt`（必填） | `agentkit.PromptAssembler` |  |
| `hooks` | `agentkit.HookRuntime` |  |
| `compaction` | `[]compaction.Service` |  |

## `subagent/loop-agent`

- 返回类型：`subagent.Spawner`
- 源码：[`runtime/subagent/loopagent.go`](../runtime/subagent/loopagent.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `agents` | `[]subagent.LoopAgentEntry` | — | Agents lists Loop-backed subagents exposed to the delegate tool. |
| `timeoutSeconds` | `int` | — | TimeoutSeconds is the default wall clock for one delegation. |
| `maxConcurrentJobsPerSession` | `int` | — | MaxConcurrentJobsPerSession limits async jobs per parent session (channel semaphore). Zero means no limit. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `agents`（必填） | `[]agentkit.Agent` |  |
| `telemetry` | `telemetry.Exporter` |  |

## `telemetry/langfuse`

- 返回类型：`telemetry.Exporter`
- 源码：[`plugins/telemetry/langfuse.go`](../plugins/telemetry/langfuse.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `baseUrl` | `string` | `https://cloud.langfuse.com` | BaseURL is the Langfuse host, e.g. https://cloud.langfuse.com. |
| `publicKeyRef` | `string` | — | PublicKeyRef resolves the Langfuse public key, e.g. env:LANGFUSE_PUBLIC_KEY. |
| `secretKeyRef` | `string` | — | SecretKeyRef resolves the Langfuse secret key, e.g. env:LANGFUSE_SECRET_KEY. |
| `environment` | `string` | — | Environment labels traces in Langfuse. |
| `release` | `string` | — | Release labels the deploying artifact. |
| `sampleRate` | `float64` | `1` | SampleRate in (0,1]; 1 exports every turn. |
| `flushIntervalSeconds` | `int` | `2` | FlushIntervalSeconds configures the Langfuse SDK batch flush interval. |
| `maxPayloadBytes` | `int` | — | MaxPayloadBytes truncates exported input/output payloads. 0 means no limit. |
| `maxFieldBytes` | `*int` | — | MaxFieldBytes truncates individual message content fields in llm.generation input while keeping JSON valid. Oversized fields end with `\n...[truncated N]` where N is omitted bytes. Omit to default to 8192; set 0 for no limit. |
| `deduplicateGenerationPrefix` | `*bool` | — | DeduplicateGenerationPrefix omits message prefixes identical to the previous llm.generation observation within the same trace. |
| `redactInputs` | `bool` | — | RedactInputs scrubs sensitive keys from exported inputs. |
| `redactOutputs` | `bool` | — | RedactOutputs scrubs sensitive keys from exported outputs. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials`（必填） | `credentials.Store` |  |
| `telemetry`（必填） | `telemetry.Toolkit` |  |

## `telemetry/none`

- 返回类型：`telemetry.Exporter`

无 config 字段。

## `telemetry/toolkit`

- 返回类型：`telemetry.Toolkit`

无 config 字段。

## `tool/ask-user`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/askuser/askuser.go`](../plugins/tool/askuser/askuser.go)

无 config 字段。

## `tool/chat-history`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/chathistory/chathistory.go`](../plugins/tool/chathistory/chathistory.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `defaultLimit` | `int` | — |  |
| `maxLimit` | `int` | — |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `history`（必填） | `agentkit.Platform` | History is typically platform.default; adapted to chathistory.Router at init. |
| `delivery`（必填） | `delivery.Assistant` |  |

## `tool/finish`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/finish/finish.go`](../plugins/tool/finish/finish.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `sessionEvents`（必填） | `session.RunLog` |  |

## `tool/fs-memory`

- 返回类型：`agentkit.ToolPack`
- 源码：[`plugins/tool/fs/fs_memory.go`](../plugins/tool/fs/fs_memory.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `files` | `map[string]string` | — | Files is seed contents, keyed by path. |
| `maxBytes` | `int` | `1048576` | MaxBytes is read truncation limit; defaults to 1 MiB. |
| `maxMatches` | `int` | `100` | MaxMatches is grep cap per call; defaults to 100. |
| `maxResults` | `int` | `1000` | MaxResults is find cap per call; defaults to 1000. |
| `maxListEntries` | `int` | `500` | MaxListEntries is ls cap per call; defaults to 500. |
| `tools` | `[]string` | — | Tools limits which model tools are registered; empty means all six. |

## `tool/fs-workspace`

- 返回类型：`agentkit.ToolPack`
- 源码：[`plugins/tool/fs/fs_workspace.go`](../plugins/tool/fs/fs_workspace.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | — | Root is accepted for older configs; path confinement lives on filesystem/local. |
| `maxBytes` | `int` | `1048576` | MaxBytes is read truncation limit; defaults to 1 MiB. |
| `maxMatches` | `int` | `100` | MaxMatches is grep cap per call; defaults to 100. |
| `maxResults` | `int` | `1000` | MaxResults is find cap per call; defaults to 1000. |
| `maxListEntries` | `int` | `500` | MaxListEntries is ls cap per call; defaults to 500. |
| `readOnly` | `bool` | — | ReadOnly rejects write and edit operations. |
| `unrestricted` | `bool` | — | Unrestricted is accepted for older configs; belongs on filesystem/local. |
| `tools` | `[]string` | — | Tools limits which model tools are registered; empty means all six (read, write, edit, grep, find, ls). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` |  |
| `workspace` | `workspace.Service` |  |

## `tool/mcp`

- 返回类型：`agentkit.ToolProvider`
- 源码：[`plugins/tool/mcp/mcp.go`](../plugins/tool/mcp/mcp.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `files` | `[]string` | `[global:mcp.json]` | Files are MCP config paths in precedence order; first file wins for duplicate server names. When omitted, defaults to global:mcp.json only; set EnableLocal to also load local:mcp.json. |
| `enableLocal` | `bool` | — | EnableLocal allows per-tenant/project local:mcp.json and /mcp add writes. Off by default. |
| `idleTimeoutSeconds` | `*int` | — | IdleTimeoutSeconds closes pooled MCP connections after this many seconds without use. Omitted defaults to 300; set to 0 to disable idle eviction. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` | FS reads/writes mcp.json files (scope prefixes allowed). |
| `credentials` | `credentials.Store` |  |
| `telemetry`（必填） | `telemetry.Toolkit` |  |

## `tool/memory`

- 返回类型：`agentkit.Tool`

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `memory`（必填） | `memory.Tool` |  |

## `tool/openapi`

- 返回类型：`agentkit.ToolProvider`
- 源码：[`plugins/tool/openapi/openapi.go`](../plugins/tool/openapi/openapi.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `files` | `[]string` | `[global:api.json]` | Files are api.json paths in precedence order; first file wins for duplicate API names. When omitted, defaults to global:api.json only; set EnableLocal to also load local:api.json. |
| `enableLocal` | `bool` | — | EnableLocal allows per-tenant/project local:api.json and /openapi add writes. Off by default. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `fs`（必填） | `filesystem.Service` | FS reads/writes api.json and spec files. |
| `workspace`（必填） | `workspace.Service` |  |
| `credentials` | `credentials.Store` |  |
| `telemetry`（必填） | `telemetry.Toolkit` |  |

## `tool/recognize`

- 返回类型：`agentkit.ToolPack`
- 源码：[`plugins/tool/recognize/recognize.go`](../plugins/tool/recognize/recognize.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `model` | `string` | — | Model overrides the vision LLM model; empty uses the provider default. |
| `imageSystemPrompt` | `string` | `You are a vision assistant. Describe the image accurately for another agent. Start with a one-sentence conclusion, then list key visible details (text, UI, numbers). Say when something is unreadable; do not guess.` | ImageSystemPrompt overrides the default vision system instruction. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `llm`（必填） | `agentkit.LLMProvider` |  |
| `workspace`（必填） | `workspace.Service` |  |
| `telemetry`（必填） | `telemetry.Toolkit` |  |

## `tool/schedule`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/schedule/schedule.go`](../plugins/tool/schedule/schedule.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxJobs` | `int` | `32` | MaxJobs is cap on agent-created jobs, default 32; jobs declared in config do not count against it. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `schedule`（必填） | `schedule.Registry` |  |
| `engine`（必填） | `schedule.Engine` |  |

## `tool/send`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/send/send.go`](../plugins/tool/send/send.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | — | Root is the workspace subdirectory files are resolved from, matching tool/fs-workspace root (typically "."). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sender`（必填） | `delivery.Sender` |  |
| `delivery`（必填） | `delivery.Assistant` |  |
| `workspace` | `workspace.Service` |  |
| `fs` | `filesystem.Service` | FS verifies attachment existence (wire an unrestricted filesystem/local instance; the resolved path is also handed to the platform as a local URL). |

## `tool/session-query`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/sessionquery/sessionquery.go`](../plugins/tool/sessionquery/sessionquery.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `defaultLimit` | `int` | `10` |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `index`（必填） | `sessionindex.Service` |  |

## `tool/set-model`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/setmodel/setmodel.go`](../plugins/tool/setmodel/setmodel.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `allowModels` | `[]string` | — | AllowModels is the whitelist of model ids the agent may switch to. Empty means no restriction. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |

## `tool/shell-bash`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/shell/shell_bash.go`](../plugins/tool/shell/shell_bash.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `workDir` | `string` | `.` | WorkDir is working directory relative to the workspace root. |
| `timeoutSeconds` | `int` | `60` | TimeoutSeconds is per-command limit when the model omits timeout; 0 means no limit. |
| `commands` | `map[string][]string` | — | Commands optionally overrides env var names injected for shell-bash.<token>. Default: scope is derived from the command's first token; keys come from L1 scopedEnv, secrets.enc.json (/env add), or shell-bash.json manifest allowlist. |
| `trimEnv` | `*bool` | — | TrimEnv trims the child env to a minimal base (PATH/HOME/... + PWD) plus scoped/extra pairs. Default false: the child inherits the full host env (os.Environ) plus scoped pairs — secrets belong in scopedEnv / /env add. |
| `extraEnv` | `map[string]string` | — | ExtraEnv adds static, non-secret KEY=value entries to the child env. Secrets belong in credentials scopedEnv / /env add, not here. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `credentials` | `credentials.Store` |  |

## `tool/shell-bwrap`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/shellbwrap/shellbwrap.go`](../plugins/tool/shellbwrap/shellbwrap.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `workDir` | `string` | `.` |  |
| `timeoutSeconds` | `int` | `60` |  |
| `commands` | `map[string][]string` | — |  |
| `maxOutputBytes` | `int` | `1048576` | MaxOutputBytes caps the collected stdout/stderr bytes each; excess is truncated and marked. <=0 uses the built-in default (1 MiB). Prevents a command with unbounded output from blowing up runner memory. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `workspace`（必填） | `workspace.Service` |  |
| `credentials` | `credentials.Store` |  |
| `sandbox`（必填） | `sandbox.Service` |  |

## `tool/skill`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/skill/skill.go`](../plugins/tool/skill/skill.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `skills`（必填） | `skill.Registry` |  |
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `sessionEvents`（必填） | `session.Skills` |  |

## `tool/subagent`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/subagent/subagent.go`](../plugins/tool/subagent/subagent.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `defaultTimeoutSeconds` | `int` | `900` | DefaultTimeoutSeconds applies when the model omits timeoutSeconds on delegate. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `subagent`（必填） | `subagent.Spawner` |  |

## `tool/todo`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/todo/todo.go`](../plugins/tool/todo/todo.go)

无 config 字段。

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `sessionStore`（必填） | `agentkit.SessionStore` |  |
| `sessionEvents`（必填） | `session.RunLog` |  |

## `tool/web-fetch-http`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_fetch_http.go`](../plugins/tool/web/web_fetch_http.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `timeoutSeconds` | `int` | `30` | TimeoutSeconds is per-request wall clock; defaults to 30. |
| `maxBytes` | `int` | `1048576` | MaxBytes is body read limit before extraction; defaults to 1 MiB. |
| `maxRedirects` | `int` | `5` | MaxRedirects is redirect chain limit; defaults to 5. |
| `userAgent` | `string` | `agentkit/1.0 (+https://github.com/lengzhao/agentkit)` | UserAgent overrides the outgoing User-Agent. |
| `allowPrivateHosts` | `bool` | — | AllowPrivateHosts allows loopback / private / link-local targets; off by default. |
| `allowHosts` | `[]string` | — | AllowHosts, when non-empty, only these hosts (and their subdomains) may be fetched. |
| `denyHosts` | `[]string` | — | DenyHosts are hosts to refuse, applied after AllowHosts. |

## `tool/web-fetch-scripted`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_scripted.go`](../plugins/tool/web/web_scripted.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `pages` | `map[string]string` | — | Pages maps URL substring to the HTML served for it. |
| `default` | `string` | — | Default is body served when no Pages key matches; empty means not found. |

## `tool/web-search-auto`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_search_auto.go`](../plugins/tool/web/web_search_auto.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxResults` | `int` | — | MaxResults is cap on hits per call; defaults to 5. |
| `snippetChars` | `int` | — | SnippetChars is snippet truncation limit; defaults to 800. |
| `tavily` | `web.WebSearchTavilyConfig` | — | Tavily holds Tavily provider settings; tried first when a key is available. |
| `duckduckgo` | `web.WebSearchDuckDuckGoConfig` | — | DuckDuckGo holds DuckDuckGo fallback settings. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `tool/web-search-duckduckgo`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_search_duckduckgo.go`](../plugins/tool/web/web_search_duckduckgo.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `maxResults` | `int` | — | MaxResults is cap on hits per call; defaults to 5. |
| `timeoutSeconds` | `int` | — | TimeoutSeconds is per-request wall clock; defaults to 30. |
| `snippetChars` | `int` | — | SnippetChars is snippet truncation limit; defaults to 800. |

## `tool/web-search-exa`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_search_exa.go`](../plugins/tool/web/web_search_exa.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `apiKeyRef` | `string` | — | APIKeyRef is credential ref resolved via deps.credentials, e.g. "env:EXA_API_KEY". |
| `apiKey` | `string` | — | APIKey is literal key; prefer APIKeyRef so the secret stays out of config files. |
| `baseUrl` | `string` | — | BaseURL overrides the API host; defaults to https://api.exa.ai. |
| `maxResults` | `int` | — | MaxResults is cap on hits per call; defaults to 5. |
| `timeoutSeconds` | `int` | — | TimeoutSeconds is per-request wall clock; defaults to 30. |
| `type` | `string` | — | Type is Exa search mode: auto (default), fast, instant, deep. |
| `category` | `string` | — | Category narrows results, e.g. "news" or "research paper". |
| `includeText` | `bool` | — | IncludeText also requests page text; costs more tokens and money than highlights alone. |
| `snippetChars` | `int` | — | SnippetChars is snippet truncation limit; defaults to 800. |
| `includeDomains` | `[]string` | — | IncludeDomains restricts results to these domains. |
| `excludeDomains` | `[]string` | — | ExcludeDomains drops results from these domains. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `tool/web-search-scripted`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_scripted.go`](../plugins/tool/web/web_scripted.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `results` | `[]web.WebSearchHit` | — | Results are hits returned for any query ByQuery does not match. |
| `byQuery` | `map[string][]web.WebSearchHit` | — | ByQuery maps case-insensitive query substring to hits; keep keys mutually exclusive. |
| `maxResults` | `int` | — | MaxResults is default cap on hits per call when the model does not ask for one. |

## `tool/web-search-tavily`

- 返回类型：`agentkit.Tool`
- 源码：[`plugins/tool/web/web_search_tavily.go`](../plugins/tool/web/web_search_tavily.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `apiKeyRef` | `string` | — | APIKeyRef is credential ref resolved via deps.credentials, e.g. "env:TAVILY_API_KEY". |
| `apiKey` | `string` | — | APIKey is literal key; prefer APIKeyRef so the secret stays out of config files. |
| `baseUrl` | `string` | — | BaseURL overrides the API host; defaults to https://api.tavily.com. |
| `maxResults` | `int` | — | MaxResults is cap on hits per call; defaults to 5. |
| `timeoutSeconds` | `int` | — | TimeoutSeconds is per-request wall clock; defaults to 30. |
| `searchDepth` | `string` | — | SearchDepth is Tavily search mode: basic (default) or advanced. |
| `topic` | `string` | — | Topic narrows results, e.g. "general" or "news". |
| `includeAnswer` | `bool` | — | IncludeAnswer also requests Tavily's synthesized answer; costs more tokens. |
| `snippetChars` | `int` | — | SnippetChars is snippet truncation limit; defaults to 800. |
| `includeDomains` | `[]string` | — | IncludeDomains restricts results to these domains. |
| `excludeDomains` | `[]string` | — | ExcludeDomains drops results from these domains. |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `credentials` | `credentials.Store` |  |

## `tools/deferred`

- 返回类型：`agentkit.ToolRuntime`
- 源码：[`plugins/tools/deferred/config.go`](../plugins/tools/deferred/config.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `RuntimeConfig` | `tools.RuntimeConfig` | — |  |
| `DisclosureConfig` | `deferred.DisclosureConfig` | `{auto 5 4000 5 25 auto [] []}` |  |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `tools` | `[]agentkit.Tool` |  |
| `toolPacks` | `[]agentkit.ToolPack` |  |
| `dynamicTools` | `[]agentkit.ToolProvider` |  |
| `policies` | `[]agentkit.Policy` |  |
| `approval` | `agentkit.Approval` |  |
| `hooks` | `agentkit.HookRuntime` |  |

## `tools/runtime`

- 返回类型：`agentkit.ToolRuntime`
- 源码：[`runtime/tools/runtime.go`](../runtime/tools/runtime.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `defaultTimeoutSeconds` | `int` | — | DefaultTimeoutSeconds is per-call timeout when a tool has no specific entry. |
| `maxResultBytes` | `int` | — | MaxResultBytes is deprecated and ignored; spill/truncation happens in session.PrepareToolResultForStorage. |
| `toolTimeouts` | `map[string]int` | — | ToolTimeouts are per-tool timeout overrides, keyed by tool name (canonical or model-visible; normalized like ExposedToolName). |
| `allowTools` | `[]string` | — | AllowTools is a model-visible tool name whitelist. When non-empty, only listed tools are exposed (names normalized like ExposedToolName). |
| `denyTools` | `[]string` | — | DenyTools is a model-visible tool name blacklist. Ignored when AllowTools is set (names normalized like ExposedToolName). |

| deps 字段 | 类型 | 说明 |
|---|---|---|
| `tools` | `[]agentkit.Tool` |  |
| `toolPacks` | `[]agentkit.ToolPack` |  |
| `dynamicTools` | `[]agentkit.ToolProvider` |  |
| `policies` | `[]agentkit.Policy` |  |
| `approval` | `agentkit.Approval` |  |
| `hooks` | `agentkit.HookRuntime` |  |

## `workspace/default`

- 返回类型：`workspace.Service`
- 源码：[`runtime/workspace/default.go`](../runtime/workspace/default.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `root` | `string` | — | Root is single-root shorthand, kept for older configs; prefer Global and Local. deprecated: alias for global |
| `global` | `string` | `~/.agentkit` | Global is global root, conventionally ~/.agentkit. |
| `local` | `string` | `.agentkit` | Local is local root, default .agentkit under cwd. |
| `scope` | `string` | `global` | Scope is which root an unprefixed path resolves against: global or local. global \| local |
| `workDir` | `string` | `work` | WorkDir is the tenant-local agent work subtree (align with filesystem/local root and shell workDir). |
| `uploadSubdir` | `string` | `upload` | UploadSubdir is the inbound upload folder under WorkDir (default upload). |

## `workspace/tenant`

- 返回类型：`workspace.Service`
- 源码：[`runtime/workspace/tenant.go`](../runtime/workspace/tenant.go)

| config 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `global` | `string` | `~/.agentkit` | Global is the root shared by every tenant, reached with the global: prefix. Conventionally ~/.agentkit. Shared skills, agent definitions and mcp.json live here; nothing under it is tenant-scoped. |
| `localBase` | `string` | `~/.agentkit/tenants` | LocalBase is the parent directory of per-tenant roots. A tenant with no entry in Tenants gets LocalBase/<sanitized tenant key>, so adding a Slack channel needs no configuration at all: it is isolated by default. |
| `scope` | `string` | `local` | Scope is which root an unprefixed path resolves against: global or local. Defaults to local, because the point of this plugin is that unqualified paths land in the caller's own tenant. |
| `tenants` | `map[string]workspace.TenantEntry` | — | Tenants pins an explicit root for specific tenant keys, e.g. "slack:C123ABC": {root: ~/work/project-a}. Keys are tenant keys as derived by session.WorkspaceKey, not session ids: every thread and every user in a Slack channel shares the channel's entry. |
| `omitPlatformPrefix` | `bool` | — | OmitPlatformPrefix drops the platform segment from local directory names. slack:C123 -> C123 instead of slack_C123; chat-api:slack_x -> slack_x. Tenant map keys are unchanged; only the default localBase/<dir> layout moves. |
| `workDir` | `string` | — | WorkDir is the tenant-local agent work subtree (align with filesystem/local root and shell workDir). |
| `uploadSubdir` | `string` | — | UploadSubdir is the inbound upload folder under WorkDir (default upload). |

