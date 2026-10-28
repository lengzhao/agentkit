# Shared execution cwd implementation plan

**Goal:** One session/channel cwd binding for local agents, arbitrary loops, tools, prompts and ACP.

**Architecture:** `sessbind.CwdBinding` persists session/channel overrides. `sessctx.Prepare` resolves and validates once per turn, writes a durable `session/context` event on transitions, and injects an immutable `rctx.Execution` plus `cap/workspace` cwd. Runner owns the command and prepares every loop; direct local/ACP agents reuse the same entry point. `sessctx.NextTurn` refreshes subsequent turns within one loop dispatch. Shared history derivation, compaction and recovery respect the latest context event; bounded history loads retain it. Workspace roots and permission boundaries stay unchanged. ACP only consumes cwd/epoch for process/session identity and serializes remote configuration against initialization.

**Tech Stack:** Go, existing memory/file/SQL sidecars, ACP SDK.

1. Test persistence, scope precedence, reset and missing-directory rejection.
2. Test shared turn snapshots, context transitions and bounded history reloads.
3. Test arbitrary runner loops, direct local agents, shell explicit overrides, relative file operations and project instructions.
4. Test ACP warm replacement, cold restoration, configuration persistence and initialization exclusion.
5. Run full tests, focused race tests, static checks and review the shared/adapter boundary.
