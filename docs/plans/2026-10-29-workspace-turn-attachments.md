# Workspace turn attachments implementation plan

**Goal:** Implement #26: unify inbound uploads and turn cwd, add opt-in workspace scope policies, and protect buffered file ownership without moving control data or historical files.

**Architecture:** Platform adapters retain downloaded bytes as transient typed uploads on MessageEvent. Runner stages downloaded bytes to owned temporary files before queue retention, then streams them to the turn directory after session queue acquisition and sessctx.Prepare. Queue emit closures must not capture the original byte-bearing event. Legacy common callers keep eager saves. Presaved files retain stable absolute references. Attachment messages queue as independent turns rather than losing upload state through the message-only steering API. The first local loop turn preserves the runner snapshot; subsequent turns refresh bindings.

**Tech Stack:** Go, workspace.Service, sessctx, runner scheduler, platform/common.

## Compatibility and boundaries

- Channel workspace layout and session/channel cwd precedence remain unchanged.
- Directory scope is independently configured as workspaceScope/workspaceScopes; never change local:/global: storage semantics or migrate/delete old files.
- Files use upload/ beneath the effective cwd; custom upload layouts remain supported.
- Deferred sources are process-local and excluded from JSON/session logs. Before queue retention, bytes are replaced by owned 0600 temporary file sources. Release is idempotent on success, failure, cancellation and shutdown. Platform download limits remain in force.
- Buffered files retain their original absolute paths and resolved Session/Route owners. A later route/session mismatch aborts the turn; unknown legacy owners remain compatible only without explicit scope policies.
- Errors preparing uploads abort the turn with a diagnosable error. Invalid cwd blocks execution under explicit policies; legacy deployments retain warning/fallback behavior.

## Tasks

1. Add regression tests for deferred file/image saves, execution cwd changes, stable buffered references, same-name collision handling, and save errors. Run tests and verify missing behavior.
2. Add transient inbound upload DTO and common materializer. Resolve all paths using the provided turn context. Keep old eager helper API behavior. Use exclusive creation for collisions.
3. Wire Slack, Feishu/Lark and Chat API fresh uploads to deferral. Preserve file-first buffers. Runner prepares after taking its execution snapshot and queues deferred uploads during busy turns.
4. Preserve first loop snapshot and refresh subsequent turns only. Add runner/loop regression tests.
5. Run affected packages, race tests and broader Go checks. Document stage-one behavior and limitations.

## Scope and ownership tasks

6. Test missing scope policies, four sharing boundaries, missing Thread fallback, restart stability, ID sanitization collisions, and platform instance overrides. Implement pure DefaultDirectoryResolver on both standard workspace services. Use structured identity hashes inside each configured root.
7. Test cwd override/reset and stable store roots. Resolve the default in sessctx only when no explicit/inherited cwd wins. Create defaults at turn preparation. Preserve session/channel binding precedence and repair session_cwd readback to verify its own binding rather than the effective fallback.
8. Test file-first owner pinning and session switches. Resolve active session and cwd at Feishu buffer save, preserve buffers across slash commands, and reject a different Session/Thread at materialization. Treat Chat API managed uploads as staging until a routed chat turn archives them.
9. Regenerate configuration catalog, document layouts and compatibility, run scripts/test.sh unit and integration, and obtain code review.

## Review follow-up: incomplete snapshots and upload retention

10. Reproduce optional Runner workspace + CLI/ACP cwd binding loss. Make Prepare defer when no workspace resolver exists, leaving no fake snapshot or durable context event. Preserve already-prepared snapshots.
11. Reproduce 32 queued one-MiB uploads through the real handleInbound path, including Emit closures. Stage inputs before enqueue and retain only metadata/file sources; capture only scalar agent identity in Emit. Stream ordinary files at execution and create image data URLs only during execution.
12. Exercise temporary-source cleanup on materialization failure, invalid cwd, canceled submit, queued shutdown and late submit. Run affected tests, full unit/race and integration scripts, and code review.
