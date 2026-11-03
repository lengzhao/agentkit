# Composable Delivery Implementation Plan

**Goal:** Implement issue #35: background message delivery with durable receipts and optional platform capabilities.

**Architecture:** Keep Platform.Send and delivery.Assistant unchanged. Add narrow Publisher, Editor, Reader and TopicCreator contracts in cap/delivery. A dependency-injected delivery service binds named leaf platforms; it routes by RouteRef.Instance and never depends on a containing multiplex platform. Topic use is entirely optional: ordinary publish works without a topic; only explicit topic creation requires the capability.

**Tech Stack:** Go, pluginkit, existing Lark and Slack SDK clients, httptest.

## Approved design

- Persist MessageRef with canonical RouteRef and opaque message ID; receipts retain all confirmed physical message references, including on partial failure.
- Distinguish error reason from outcome certainty. Do not retry uncertain writes or silently fall back from thread to channel.
- Stable caller operation IDs map to deterministic per-part native idempotency IDs; report actual deduplication guarantees, not exactly-once.
- Content supports text and structured mentions initially; unsupported content is rejected before sending. Edit addresses one physical message.
- Lifecycle is synchronous, context-cancellable and independent of Receive/agent turns. The service owns no queue or background goroutines. Host admission policy is injectable.
- Reuse existing adapter clients, leaving existing streaming Send behavior compatible.

## Task 1: Contracts and routed service
Create cap/delivery/messages.go, runtime/delivery/messages.go and tests. First test no-turn publish, absent topic capability, same-protocol instance routing, persisted reference reuse, wrong-instance rejection, partial receipts and unknown outcomes. Run tests to observe missing API failures; implement the minimum contracts and routing; rerun tests.

## Task 2: Platform adapters
Create runtime/platform/{feishu,slack}/messages.go and HTTP-backed contract tests. Test publish/mentions/chunking, edit/read, explicit topic creation, API errors and unknown results. Implement using existing clients, inspect the pinned SDK source for request/response semantics, run adapter tests. Avoid legacy reply fallback/retry on this new path.

## Task 3: Internal consumer
Add opt-in receipt delivery to plugins/tool/send; preserve existing sender/delivery wiring and ordinary sends. Write tests for explicit receipt requests, persisted refs and missing capabilities, then implement. Register delivery/messages and document leaf wiring.

## Task 4: Documentation and validation
Document supported content, optional topics, platform support, dedup windows, lifecycle, host admission and TaskHub recovery example. Run targeted tests with race detection, full go test ./..., go vet for changed packages and diff checks. Review for routing, duplication and compatibility risks.
