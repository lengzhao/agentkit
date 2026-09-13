# Learning / Dreaming / Background Review — 后续 Todo

本清单承接 [learning-dreaming.zh.md](learning-dreaming.zh.md) §9～§10 与 [roadmap.zh.md](../roadmap.zh.md)。Background review（`hook/background-review`）已默认挂在 L0；以下按 **优先级** 排列，做完可在文中勾选 `[x]`。

相关对照：Hermes 的 post-turn review、`session_search`、Curator、`write_approval`。

---

## 现状（三条学习通路）

| 通路 | 机制 | 强项 | 短板 |
|------|------|------|------|
| Background review | Turn 后 LLM + `learn_capture` | 接近「自己会记」 | 成本、误记、无用户可见反馈 |
| Dreaming sweep | 采集信号 + 日记；晋升由 background review | 可审计、低成本 | L0 默认挂 `learning.dreamSweep`（可用 `/learn dream off` 关闭） |
| Workshop + `/learn` | 提案闸门 | 安全 | 与 review `skill_propose` 重叠时需控 `workshop.mode` |
| 主 agent `tool/memory` | 当轮写 `memory.md` | 显式、可控 | 与 review 分工：用户/模型主动 vs 后台巩固 |

```mermaid
flowchart TB
  subgraph p0 [P0 可运营与信任]
    A[write_approval + 暂存]
    B[aux LLM + 节流 + 成本]
    C[集成测试 + telemetry]
  end
  subgraph p1 [P1 回忆与多租户]
    D[session/sqlite + session-query]
    E[全租户 dream-sweep]
    F[USER 分仓 / frozen snapshot]
  end
  subgraph p2 [P2 治理与产品]
    G[Curator 技能归档]
    H[/learn 增强 + 可选 memory 工具]
    I[review 与 dreaming 写入门统一]
  end
  subgraph p3 [P3 深度]
    J[LLM Dream Diary]
    K[Deep rehydrate]
    L[外部 memory provider]
  end
  p0 --> p1 --> p2 --> p3
  D --> H
```

---

## P0 — 上线 review 前（建议下一迭代）

- [x] **write_approval + 暂存队列**  
  - `memory.default.config.review.writeApproval`（或 `/memory policy approve`）：review 的 memory 类 `learn_capture` 先入 `memory/.staged/`，不立刻进 prompt 注入链。  
  - 用户面：memory 用 `/memory pending`、`approve` / `reject`；skill 用 `/learn workshop list|apply|reject`。  
  - 验收：后台误记可拒绝，且从未写入 `memory.md`。

- [x] **独立 review 用 LLM + 节流**  
  - L0 增加 `llm.review`（或 `hook.background-review.deps.llm` 指向便宜模型实例），不单靠 `model` 字符串覆盖主 `llm.fallback`。  
  - 配置：`minTurnTokens`、`maxReviewsPerDayPerTenant`、`minIdleSeconds`（可选）。  
  - 验收：群聊高频场景 token 可预期。

- [x] **可观测**  
  - Telemetry：`learning.review` span，记录 steps、in/out tokens、cancel/ok。  
  - 可选 platform：`learning.notification`（IM 短句，对齐 Hermes `💾`）；默认 off。  
  - 对齐 roadmap M3「成本汇总 CLI」时纳入 review 用量。

- [x] **集成测试**  
  - `llm/scripted`：一轮用户 turn → 等待 review → 断言 `memory.md` 或 `.workshop` 条目。  
  - 验收：CI 与 `coding-smoke` 同层级可跑。

- [x] **M2 交叉：关停与 review**  
  - `StartStop` / runner 关停时 cancel 进行中的 review goroutine；staged 文件原子写不误半截。  
  - 验收：SIGTERM 后 `memory.md` 可解析、无损坏。

---

## P1 — 跨会话回忆与多租户（依赖 roadmap M3）

- [x] **`session/sqlite` + `tool/session-query`**（roadmap M3）  
  - FTS/查询 API；review digest 可附带「是否已记过类似内容」检索结果。  
  - 验收：agent 或 review prompt 能回答「上周是否讨论过 X」（同 tenant）。

- [x] **多租户 dream-sweep**  
  - 文档现状：sweep 用启动时单一 workspace；需 tenant registry 或按 tenant cron。  
  - 验收：`presets/multi-tenant.yaml` 下每租户独立 `memory/dreaming/state.json` 可 sweep。

- [x] **USER 分仓 + frozen snapshot（可选）**  
  - 文档约定 background review 写入 **下轮** 才进 system prompt（同 turn frozen snapshot）。  
  - 验收：与 Hermes 行为说明一致，且单测覆盖注入时机。

---

## P2 — 技能治理与命令面

- [ ] **Curator（确定性归档优先）**  
  - `active → stale → archive`、pin、schedule/cron 引用保护；LLM 合并二期。  
  - 参考 Hermes curator；对应 learning-dreaming §10「周度 collection review」。  
  - 验收：`workshop.mode=auto` + review 大量 `skill_propose` 时库不无限膨胀。

- [ ] **review ↔ dreaming 写入门策略**  
  - 规则示例：Deep 不晋升与 `background-review` 重复的文本；或 review 只写 staging、Deep 统一晋升。  
  - 验收：同一会话 fact 不出现两条 § 重复。

- [ ] **`/learn` 增强**  
  - 从 URL/路径生成 skill：委派受限 agent（read/web），替代纯 `DraftSkillBody` 模板。  
  - 可选：主 agent `tool/memory`（与 `learn_capture` 共用 `cap/memory.Capture`）。  
  - 验收：文档与 `plugin-catalog` 更新。

- [ ] **可运维**  
  - 扩展 `/learn`：memory 用量、pending workshop、最近 review 摘要（读 slog/telemetry 或本地 state）。

---

## P3 — 深度（需求驱动）

- [ ] **LLM 驱动 Dream Diary**（§10）  
  - 叙事写 `DREAMS.md`；**永不**作为 Deep 晋升来源。

- [ ] **Deep rehydrate source snippet**（§10）  
  - 晋升时从 session 拉回原文；依赖 session 存储与 query。

- [ ] **`memory forget` 与会话准入策略**（§10）

- [ ] **外部 memory provider**（Honcho/Mem0 类）  
  - 按需 `cap/*`；与内置 `memory.md` 互斥或分层文档化。

---

## 文档与配置同步（随功能勾选）

- [ ] [roadmap.zh.md](../roadmap.zh.md) 增加「Learning / Review」小节，标明 session-query 为 P1 依赖  
- [ ] [config.example.yaml](../../config.example.yaml) 示例：`hook.background-review`、`writeApproval`、`llm.review`  
- [ ] [learning-dreaming.zh.md](learning-dreaming.zh.md) §10 与本文档互链；大项完成后从 §10 迁入「已做」  
- [ ] 默认策略决策：L0 `hook.background-review` **默认 enabled** 是否对多租户/高流量改为 preset 开启  

---

## 建议执行顺序（简表）

| 序 | 项 | 验收 |
|----|-----|------|
| 1 | write_approval + staged | 误记可 reject |
| 2 | llm.review + 节流 | token 可预期 |
| 3 | telemetry + 可选 IM 通知 | 可查 review 次数/token |
| 4 | scripted 集成测试 | CI 绿 |
| 5 | M2 关停 cancel review | memory 文件一致 |
| 6 | session-query | 跨 session 检索 |
| 7 | 多租户 sweep | 每 tenant state |
| 8 | Curator 归档 | 长期未用 skill 可恢复归档 |
| 9 | review↔dreaming 写入门 | 无重复 memory（review-led，dreaming 不写 memory） |
| 10 | `/learn` URL + 可选 memory 工具 | 产品体感提升 |

---

## 代码简化 / 通用化（learning · delivery · session，2026-03）

承接近期 memory 工具、通用 outbound、`memory_commit` 收敛后的剩余债务。做完勾选 `[x]`。

### P0 — 小步重构（本迭代）

- [x] **`applyMemoryAdd` + `memoryToolSnapshot`**：`plugins/learning` 统一 store.Add + commit；`memoryTool*` / `addMemory` / capture 共用；工具输出拼装一处。
- [x] **`SendProactiveInboxText`**：`runtime/delivery` 封装 `Raw` + `UseContextEmit` 默认；background review 等调用点瘦身。

### P1 — 去重与 helper

- [x] **`FormatMemoryPromptBody`**：`runtime/learning` 统一 prompt 注入正文（`\n\n` 拼接）；`prompt/memorymd` 与测试共用。
- [x] **删薄包装 / 死代码**：`plugins/learning/memory.go` 去掉 `ParseMemory`/`RenderMemory` re-export；`runtime/learning/memory.go` 去掉未用的 `formatMeta`。
- [x] **`FlattenTextParts`**：`runtime/session` 导出文本 part 拼接（sep 参数）；`capture`、`background_review`、`review`、`index_extract` 对齐语义。
- [x] **`TruncateEllipsis`**：`runtime/learning` 单一截断 helper；`background_review` 与 review digest 共用。
- [x] **`SearchSyncedSessions` / `SyncSessionIndex`**：`runtime/session` 封装 FTS sync + search；background review、`session-query`、chat-api 发现共用。
- [x] **通知文案迁回 plugin**：`FormatBackgroundReviewNotification` / `NormalizeMemoryNotifications` 在 `plugins/learning/background_notify.go`。

### P2 — 架构对齐（后续）

- [x] **`cap/learning` 注入边界**：`SkillProposer` / `ReviewHost` / `DreamSweepScheduler`；`cap/memory` + hook deps `memory`+`learning`；内建 `learn_capture`
- [x] **Chat API 会话列表与 `sessionindex` 统一**：L0 `platform.chat-api.deps.sessionIndex`；发现走 `ListSessions` + JSONL 元数据；`chat-api/conversations/*.json` 仍保留 API 专有字段；见 [platform-interaction.zh.md](platform-interaction.zh.md)。
- [x] **`MemoryStore` 在 `runtime/memory`**：插件经 `memoryStore()` 构造；秘密检测 `LooksLikeSecret` 同包。
- [x] **`memory/default` + `/memory`**：`cap/memory`；`tool/memory` / `prompt/section/memory` deps `memory`；`/learn session` 仅 dreaming 信号。
- [x] **review `sessionRecall` 可关**：`hook.background-review.config.sessionRecall: false` 跳过 digest FTS 块。
- [x] **`CommitObserver` 多订阅**：`memory/default` `RegisterCommitObserver` append，commit 时通知全部观察者。

### P2 — runtime/memory 迁移（进行中）

- [x] **`MemoryStore` / ledger / staged → `runtime/memory`**：`AddOutcome` 统一 `cap/memory`；`plugins/memory` 不再依赖 `runtime/learning`。
- [ ] **Dreaming / Workshop → `runtime/learning/*`**（可选阶段 3）。

---

## 核对

改插件或配置后：

```sh
grep -E 'background-review|learning/' docs/plugin-catalog.zh.md docs/guides/learning-dreaming.zh.md config.base.yaml
go test ./cap/learning/... ./plugins/learning/... ./runtime/learning/... -count=1
```
