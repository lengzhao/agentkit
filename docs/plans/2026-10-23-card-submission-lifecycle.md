# Card submission lifecycle implementation plan

**Goal:** Apply in-memory one-shot submission only to default agent fallback; injected handlers own lifecycle when they consume an interaction.

**Design:** Keep callback event dedup separate from a card submission gate. Gate fallback final submissions atomically by platform/chat/card (plus user for per-user answers), retaining successful entries for a configurable 24-hour default; pending entries do not expire. Release failed queue deliveries and retain successful submissions even when card updates fail. Keep agent execution independent. Plugins run before the gate and receive an optional context-bound card responder. Native navigation and form changes remain interactive.

**Steps:**
1. Add failing shared tests for concurrent submissions, TTL cleanup, failure release, update failure, plugin bypass, drafts/navigation, per-user scope, and opt-in plugin close.
2. Implement policy normalization, bounded in-memory submission state, and post-plugin fallback lifecycle in common; add the narrow optional responder contract in cap/interaction.
3. Adapt Feishu/Lark and Slack to normalize final vs draft operations, provide read-only card updates/private per-user receipts, and return submitted acknowledgements. Add mocked HTTP integration tests, including failed updates.
4. Document metadata overrides and TTL. Run complete platform tests and focused race tests; request a read-only code review. No release/push is included.

**Policy metadata:** callback value `agentkit_interaction` contains optional `purpose` (submit/change/navigation) and `mode` (once/per_user/repeat). Feishu accepts a map; Slack's string values may contain its JSON encoding. Unhandled command/navigation prefixes remain repeatable. Controls inside a form are drafts until final submit; native modal submission is final.
