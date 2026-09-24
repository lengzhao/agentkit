# LLM 协议路由 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Agent 只配置模型名；多个协议 LLM 插件实例 + `llm/router` 按 catalog 自动分发；未命中走 default 协议。

**Architecture:** `cap/llm.ModelCatalog` 契约 + `runtime/llm/router` 实现 + `llm/router` 插件注册；OpenAI 拆为 chat/responses kind；Agent 用 `ModalitiesForModel` 做按模型 hydrate。

**设计全文：** [2026-09-23-llm-protocol-router-design.md](./2026-09-23-llm-protocol-router-design.md)

**Tech Stack:** Go、pluginkit、slog、`bash scripts/test.sh unit`

---

### Task 1: `cap/llm` 目录契约

**Files:**
- Create: `cap/llm/catalog.go`
- Test: `cap/llm/catalog_test.go`（纯类型/辅助函数若有）

**Step 1:** 定义 `ModelEntry`（`ID`、`Modalities []string`）与 `ModelCatalog` 接口：

```go
type ModelCatalog interface {
    CatalogModels() []ModelEntry
}
```

**Step 2:** 文档注释：实现者同时实现 `agentkit.LLMProvider`；router 在 build 时 type-assert。

**Step 3:** `go test ./cap/llm/...`

---

### Task 2: 根包 `ModelModalityAwareLLM`

**Files:**
- Modify: `modalities.go`
- Modify: `runtime/llm/modalities.go`
- Test: `runtime/llm/modalities_test.go`

**Step 1:** 在 `agentkit` 增加 `ModelModalityAwareLLM`（见设计 doc）。

**Step 2:** 新增 `ProviderModalitiesForModel(p LLMProvider, model string) []string`：优先 `ModelModalityAwareLLM`，其次 `ModalityAwareLLM`，最后 default。

**Step 3:** 修改 `runtime/agent/agent.go` `prepareStepHistory`：用 `a.model`（或 runtime 当前 model 字段）调用新 helper。

**Step 4:** 测试覆盖 router mock 与旧 `ModalityAwareLLM` 行为。

---

### Task 3: `runtime/llm/router`

**Files:**
- Create: `runtime/llm/router.go`
- Create: `runtime/llm/router_test.go`
- Modify: `runtime/llm/register.go`（注册 `llm/router`）

**Step 1:** 写失败测试：双 protocol 映射、`Stream` 委托、default 路径、重复 id 报错、无 match 无 default 报错。

**Step 2:** 实现 `RouterConfig` + `RouterDeps`：

```go
type RouterDeps struct {
    Protocols []agentkit.LLMProvider `json:"protocols"`
    Default   agentkit.LLMProvider   `json:"default"`
}
```

**Step 3:** `NewRouter` 构建 index；实现 `Stream`、`Name()`、`ModelModalityAwareLLM`。

**Step 4:** `pluginkit.Register("llm/router", NewRouter)`。

---

### Task 4: 协议插件实现 `ModelCatalog`

**Files:**
- Modify: `runtime/llm/openai.go`（或拆文件）
- Create: `runtime/llm/openai_chat_plugin.go` / `openai_responses_plugin.go`（按拆分需要）
- Modify: `runtime/llm/register.go`

**Step 1:** 从 `OpenAIConfig` 抽出 `Models []ModelEntryConfig`；解析 modalities。

**Step 2:** 注册 `llm/openai-chat` 与 `llm/openai-responses`（固定 `api`，忽略旧 `api` 字段或 chat/responses 各自默认）。

**Step 3:** `OpenAI` 实现 `CatalogModels()`；`ModalitiesForModel(model)` 查表。

**Step 4:** 保留 `llm/openai-compatible`：`NewOpenAI` 仍读 `config.api`，**不**强制 catalog（单模型 `config.model` 行为不变）；文档 deprecated。

**Step 5:** 单元测试：catalog 条目 modalities；无 catalog 时 `ModalitiesForModel` 回退 `config.modalities` / default。

---

### Task 5: `llm/anthropic` 骨架（可选同 PR 或 follow-up）

**Files:**
- Create: `runtime/llm/anthropic.go`、`anthropic_stream.go`（最小 Stream stub 或完整 Messages）
- Register: `llm/anthropic`

**Step 1:** 若首 PR 只做路由：anthropic 可注册为返回明确 `not implemented` 的 provider，但须实现 `ModelCatalog` 供集成测试。

**Step 2:** 完整 Messages 实现可单独 PR；设计 doc 已 roadmap。

---

### Task 6: `llm/fallback` 与 router 组合

**Files:**
- Modify: `runtime/llm/fallback.go`

**Step 1:** Fallback 包装 inner `LLMProvider` 时，若 inner 实现 `ModelModalityAwareLLM`，`ModalitiesForModel` 按**当前 fallback target 的 model** 转发。

**Step 2:** 测试：fallback 切换 model 后 modalities 变化。

---

### Task 7: 配置与文档

**Files:**
- Modify: `docs/plugin-catalog.zh.md`
- Modify: `docs/go-agent-harness-architecture.zh.md` §6.8
- Modify: `skills/agentkit-config/SKILL.md`（router 示例）
- Optional: `config/testdata/presets/coding.resolved.yaml` 片段或新 `config/testdata/presets/multi-protocol.yaml`

**Step 1:** 插件表增加 `llm/router`、`llm/openai-chat`、`llm/openai-responses`。

**Step 2:** 架构 doc「Provider 选择」改为 catalog + default 描述。

**Step 3:** preset 可继续用 `llm/openai-compatible`；新增注释/example 展示 router 装配。

---

### Task 8: Smoke

**Files:**
- Create or modify: `testing/smoke/llm_router_test.go`

**Step 1:** 图构建含 router + scripted/mock protocol；agent turn 断言委托到预期 provider（可通过 scripted 不同 `Name()` 响应）。

**Step 2:** `bash scripts/test.sh unit`

---

## 迁移指南（用户）

1. 按协议拆 LLM 实例，填 `models[].id`。
2. 选一个实例作 `llm/router.deps.default`（通常为 OpenAI 兼容网关）。
3. Agent `deps.llm` 改为 `llm/router`（或 `llm/fallback` → router）。
4. 暂不改者可继续 `deps.llm: llm/openai-compatible` + 单 `config.model`。

## 执行顺序建议

Task 1 → 2 → 3 → 4 → 6 → 7 → 8；Task 5 可与 4 并行或后置。
