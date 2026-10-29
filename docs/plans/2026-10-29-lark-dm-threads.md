# Lark DM threads implementation plan

**Goal:** Opt-in Lark/Feishu DM topics with persistent per-user preferences (issue #24).

**Architecture:** `dmThreadMode` supplies the default. `/dm-thread on|off|status` overrides it for the sender within a platform instance. JSON preferences live beneath the workspace, written atomically. DM delivery IDs use the topic root message ID; existing topics remain routed after disabling the default. Text, media and cards share the existing reply API builder. Runner `sessionScope: thread` preserves separate histories and locks.

**Tech Stack:** Go, Lark SDK, workspace service, JSON.

## Tasks
1. Add failing tests for default compatibility, DM root/reply continuity, outgoing thread bodies, and persisted user preferences.
2. Add configuration and preference command; wire message routing and common reply decisions, including card recovery and quote handling.
3. Document configuration, user commands, session scope requirement, and live acceptance limits.
4. Run Feishu/Lark and routing regression tests and inspect the final diff.

## Acceptance
Default DM remains flat; enabling affects only the selected user; every top-level message becomes a separate conversation; topic replies share its root; disabling preserves existing topic continuity; group settings remain independent; settings survive platform restart. Live bot verification is required for end-to-end Lark API behavior.
