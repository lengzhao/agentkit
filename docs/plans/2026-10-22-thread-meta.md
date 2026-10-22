# Thread metadata implementation plan

**Goal:** Expose channel and thread routing context in the existing configurable `[meta ...]` message prefix.

**Architecture:** Keep `chat_id` for the channel. Add the built-in `thread_id` token sourced from the delivery route, omitting it when empty. Preserve injection order and existing defaults.

**Tech Stack:** Go runner and existing runner tests.

## Steps

1. Add regression cases for threaded and ordinary deliveries, omitted tokens, and message prefix insertion in `runtime/runner/inbound_format_test.go`.
2. Run `go test ./runtime/runner -run 'Test.*Thread' -count=1` and confirm the new thread cases fail.
3. Add `injectThreadID`, its formatter case, and built-in recognition in `runtime/runner/inbound_format.go`.
4. Update runner configuration comments and the injection guide/catalog with `thread_id`.
5. Run `go test ./runtime/runner ./runtime/rctx` and `git diff --check`; inspect the final diff.
