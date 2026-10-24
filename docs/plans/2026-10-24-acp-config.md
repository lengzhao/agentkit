# ACP config commands implementation plan

**Goal:** Expose per-agent ACP model and generic session configuration commands with useful show output and examples.

**Architecture:** Each ACP runtime contributes `/acp-<id>`. Commands use the active harness session and its ACP bridge. Keep full remote configOptions on the subprocess session, refresh from session creation, restoration, set responses and unsolicited updates. Protocol current values are authoritative; initial session values are explicitly labelled rather than presented as protocol defaults.

**Tech stack:** Go, existing command registry, coder/acp-go-sdk v0.13.5.

## Approved design

- The existing config.id names the slash suffix; no additional naming field or stricter ID validation. Retain registry duplicate detection.
- Bare command displays navigation without connecting; mode/model/effort display their own option, while show explicitly displays all options; config/show displays options, values and examples. `model <value>` and `config <id> <value>` update only the active session.
- Select values and boolean options are validated locally. No legacy model endpoint or automatic vendor CLI config writes.
- New sessions may be initialized by a command without running a prompt. Unsupported agents receive a clear message. Bare-command navigation never starts a subprocess.
- Query model information through `/acp-<id>` only; do not emit model metadata into the thinking stream.

## Steps

1. Write command behavior tests: dynamic names/validation, show/navigation examples, select and boolean writes, unsupported/invalid input, authoritative responses and session isolation. Run tests and confirm missing-feature failure.
2. Implement config state collection and restore response interception without changing SessionRestorer's interface. Add command provider and generic config/model commands.
3. Test idle notifications, restore state, remote rejection and command transport with an in-process ACP connection. Run ACP and command package tests, race checks and build.
4. Document config.id-based slash usage in the plugin catalog; review diff and report checks.

## Review simplification

Use the existing agent ID for command names. Runtime bridges use a typed map under one mutex. Each bridge owns a single harness session and subprocess, so keep one immutable atomic session binding, one config snapshot and one pending-replay flag instead of session maps. Reuse model-option detection across commands and examples. Parse command syntax in a pure helper before resolving a session or connecting. Preserve current/initial values, remote full-state updates, deferred replay and request-independent process lifetime.

## Concise command output

Bare commands show navigation. Mode/model/effort subcommands resolve semantic config categories and display only the selected option. Set responses refresh the full cache but display only the changed option. Results link to the bare command instead of appending navigation; there is no separate help subcommand.

## Global defaults (-g)

Use `model -g [value]`, `mode -g [value]`, `effort -g [value]` and
`config -g show [id]` / `config -g <id> <value>`. Also accept `--global` in
that position. Omission means session scope.

Persist per-agent defaults through the filesystem capability under global ACP
storage, independently of vendor CLI configuration. Global reads need no remote
connection. Validate global writes in an isolated configuration session, including
dependent options; persist only after the remote accepts the configuration.
Apply defaults before history replay on session/new, never on resume/load.
Keep the full remote state cache and scope output to the requested option.

Implementation and validation:
1. Add failing command tests for flag position, global persistence, session/agent
   isolation, new-session inheritance, restore preservation and remote rejection.
2. Implement an atomic per-agent defaults store, global command handling and
   deterministic mode/model/dependent-option application during session creation.
3. Update navigation and documentation; run ACP, command and ACP client race tests.

## Reset and command deadlines

`model/mode/effort reset` and `config <id> reset` restore the first value observed
on the current connection; they do not claim to restore vendor defaults.
With `-g`, reset removes the saved default without remote validation; `config -g
reset` clears every saved default, including options the agent no longer exposes.
All slash configuration operations have a five-second context deadline, including
cancellable lock acquisition; caller deadlines may be shorter. Global reads use
atomic snapshots without writer locks. Writers lock per resolved storage path.
Validation subprocesses are released after each request, so subsequent requests
spawn under their own workspace/sandbox. Process cleanup can outlive the RPC deadline.
