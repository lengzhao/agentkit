# Preserve task identity during compaction

Fix the observed failure where skill/load is treated as a new user turn and forced truncation drops the task checkpoint. Keep source-derived turn-start flags in persisted retained tails; use a compatibility check for older skill envelopes. Forced trimming must protect the real user request and prior summary, and keep tool confirmations with their matching calls before optional bulky context. Do not change the user's configuration or already-uncommitted card fixes.

Validation: reproduce skill-as-trigger, checkpoint eviction, confirmation eviction, repeated compaction/reopen; run compaction, session replay and loop/smoke tests and focused race checks.
