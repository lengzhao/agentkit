# Feishu / Lark runtime boundaries

The adapter owns SDK calls, platform identity and routing, native callback decoding,
CardKit entities/sequences, and native message rendering. Feishu and Lark share
this implementation; their domains and credentials remain instance specific.

Shared or business logic lives outside native rendering:

- `runtime/session/derive.HasUserInstruction` classifies existing session history.
- `runtime/platform/common.KeyedExecutor` bounds ingress work and preserves FIFO
  order within a chat. Lark uses 8 active jobs and 256 waiting jobs. Shutdown
  cancels active work and discards waiting work; jobs must honor cancellation.
- `runtime/platform/common.ProgressEntryFromToolResult` reduces tool results to
  presentation data. Native streaming state and writes remain in this adapter.
- `runtime/cardcompat` translates legacy delete-mode cards and form submissions.
  The renderer handles typed forms and checkboxes without knowing their commands.
  Existing command names and callback values remain compatible with downstream
  producers.
- `runtime/larkevent.ReceiptSummary` formats the safe event receipt fields;
  subscription, parsing and event delivery remain native adapter responsibilities.

## Delivery semantics

Ordinary IM Create/Reply uses one write boundary. A confirmed expired-token rejection can
refresh credentials and retry. A lost response, transport failure, or ambiguous
server result is an unknown write: it can already have committed, so neither
retry nor another final-message fallback is safe. An unsuccessful Reply is not
silently converted into a new parent-chat message. Read/update retries and
CardKit entity creation have separate policies. Event-topic root creation retains
its stable UUID and existing retry policy; it does not use an unkeyed retry.

Ingress dedup records successful admission to the process-local queue. Queue
rejection leaves the event retryable. Admission is not durable processing:
shutdown, restart or a failing handler can still lose accepted work. Callback
acknowledgment does not promise exactly-once business execution.

Attachment buffering, interaction submission tracking, reaction identity and
CardKit sequencing remain separate state with different lifetimes. This refactor
does not merge them into a general workflow/state framework or add configuration.
