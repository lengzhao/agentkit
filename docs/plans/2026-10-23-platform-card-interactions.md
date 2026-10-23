# Platform card interactions implementation plan

**Goal:** Deliver complete external card questions and answers to agents across Feishu/Lark and Slack, preserving permission routing and supporting handler subplugins.

**Architecture:** Introduce `cap/interaction` with a platform-neutral event and a handler returning `(handled bool, err error)`. Shared runtime helpers render question/answer context and raw payloads, map options within controls, hash complete operation data, and run builtins, enrichment, handlers and fallback in order. Platform adapters enforce access and deduplication before asynchronous work and recover original delivery routes.

**Tech stack:** Go, existing Lark and Slack SDKs, pluginkit dependency injection, httptest.

## Approved design

Permission callbacks remain exclusive to the existing permission broker. Unknown `act:`, `nav:` and `cmd:` actions reach fallback only after command dispatch reports not handled. Card fetch states distinguish unavailable, failed and available, preserving raw card and callback JSON. Accepted callbacks acknowledge immediately; background work performs network calls. Deduplication includes actor, destination and the complete operation/form. Existing `unknownCardAction` defaults to forward and ignore only controls agent fallback; handler plugins still receive unhandled actions.

## Tasks (execute in this session)

1. Add shared contract and helpers (`cap/interaction`, `runtime/platform/common/card_interaction.go`). First write tests for scoped value-label mapping, fetch status and raw preservation, deduplication, and handler/fallback semantics; run red, implement, run green.
2. Adapt Feishu (`feishu.go`, `card_action_forward.go`, deps). Test unknown prefixes, async ACK with blocked HTTP, permissions, ACL, changed answers, fetch failure and topic routing. Implement query snapshots and validated route recovery; re-run tests.
3. Adapt Slack (`interactive.go`, `block_action_forward.go`, deps). Test complete block/state payloads, callback message preference, thread queries, unknown prefixes, handler consumption and ACL. Acknowledge before background processing and apply shared processing.
4. Document plugin injection and routing/failure semantics in `docs/guides/platform-interaction.zh.md`. Run focused package tests, race tests and wider platform tests; inspect diff and report limitations.

No release or downstream acpd dependency update is included: this workspace is agentkit.

## Review follow-up

Addressed distinct callback identity (including A/B/A), lossless numeric deduplication, command profile/mention metadata, and final queue delivery error propagation. Interaction receipts now separate in-flight work from completed work and release failed attempts. Final delivery receives an independent bounded context and profile lookups honor a shorter context. Platform command execution is shared; obsolete private formatting helpers and duplicate SDK callback round-trips were removed. Tests exercise the regressions, backpressure with expired worker context, profile cancellation, and concurrent receipt reservation.
