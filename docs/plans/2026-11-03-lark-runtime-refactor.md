# Lark Runtime Refactor Implementation Plan

**Goal:** Reduce Lark adapter responsibility while preserving user-visible messaging, interaction and session ownership behavior.

**Architecture:** Keep native SDK, CardKit and route semantics in the adapter. Move session-history facts into the session runtime; keep business commands outside native rendering. Use one bounded, lifecycle-owned ingress executor rather than independent background tasks. Separate confirmed API rejection from uncertain writes before choosing retry or fallback. No new user configuration or global registry.

**Tech Stack:** Go, existing Lark SDK, httptest, existing platform/common and session runtime packages.

## 1. Session facts and ingress lifecycle
- Extract the existing history classification from feishu/ingress.go into a small session-runtime helper, retaining compatibility with attachment-only turns and Unknown behavior.
- Introduce a bounded keyed executor in platform/common and route Lark message/event background work through it; preserve per-chat order, propagate host cancellation and drain/stop predictably.
- Add focused classification and executor tests before implementation. Run relevant session, common and Lark tests.

## 2. Card business boundaries and progress state
- Inspect deletion/navigation card creation and move business action interpretation to the owning command/common presentation logic; Lark should render typed controls and decode native fields.
- Remove unused prefixed progress-payload parsing and rendering. Keep single-consumer tool-result reduction, CardKit seq, native layout preservation and streaming locks local.
- Preserve current callbacks and compatibility formats unless callers can be ruled out. Add behavior tests and run Lark/common/command tests.

## 3. Single message writes and safe policy
- Characterize Create/Reply transport failures with httptest, including a write followed by a malformed/lost response.
- Centralize single SDK writes and typed internal outcomes. Do not retry uncertain creates/replies or move an uncertain reply into the parent chat.
- Retain token refresh for confirmed token rejection and existing read/update retry behavior where safe. Test known rejection, token refresh, unknown result and context cancellation.

## 4. Cleanup and verification
- Remove only verified redundant state/code; keep source ownership and reaction presentation distinct where feasible.
- Document new internal boundaries and retained compatibility limits.
- Run targeted race tests, full go test ./..., go vet, diff checks and a final independent review. Commit reviewable changes on codex/lark-runtime-refactor; leave PR #36 unchanged.
