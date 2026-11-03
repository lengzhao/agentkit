# Custom plugin card delivery

The user narrowed issue #35 to a small entry point allowing custom plugins to send cards.

- Expose the existing generic Card model and an optional CardSender capability in cap/delivery.
- Keep common card types compatible with aliases; reuse Lark/Slack renderers and clients.
- Let custom plugins inject the existing leaf or multiplex platform directly. Add no delivery service or instance configuration.
- Return a serializable message reference. Honor target allowlists and never retry uncertain writes or silently change threads.
- Leave tool/send, generic editing/reading, topic creation, queues and idempotency services out of scope.
- Verify real plugin injection, both platform requests, no-turn Slack sending, instance routing, unsupported capabilities and unknown results.
