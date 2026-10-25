# Route and Envelope Convergence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make RouteRef the destination contract and TurnEnvelope the single source of inbound routing context.

**Architecture:** RouteRef.Platform identifies the transport protocol; RouteRef.Instance selects a concrete adapter and defaults to Platform for existing data. Multiplex stamps the instance, and logical history/binding/workspace keys use that namespace. Runner resolves inbound context once. Send, history and persisted schedules carry RouteRef; legacy schedule JSON is converted only during reading, with transient reply anchors excluded from scheduled destinations.

**Tech Stack:** Go, database-backed and JSON schedule persistence, existing platform adapters and race tests.

---

## 1. Separate protocol from adapter instance

Files: envelope.go; runtime/platform/multiplex/multiplex.go and multiplex_test.go; runtime/rctx/route_policy.go, bind_keys.go, envelope.go and tests.

1. Add failing tests for two Slack adapters: responses reach the second adapter, protocol remains slack, and conversation/workspace/binding keys differ.
2. Add RouteRef.Instance and a destination-ID accessor with Platform fallback; preserve it in JSON codecs and route construction.
3. Stamp the instance in multiplex without replacing protocol or the platform delivery target. Namespace logical keys without changing the raw platform delivery ID.
4. Run `go test -race ./runtime/platform/multiplex ./runtime/rctx ./runtime/runner` and commit.

## 2. Converge inbound fields on TurnEnvelope

Files: agentkit.go; envelope.go; runtime/rctx/{envelope,id,inbound_route,route_policy}.go; runtime/{runner,loop,agent}; platform and tool producers/fixtures.

1. Add tests for normalization preserving explicit route instance, actor, agent and metadata; invalid Loop requests fail at the boundary.
2. Move MessageEvent routing and metadata fields into its Envelope. Migrate producers and consumers using type-aware AST transformations, then remove redundant synchronization logic.
3. Select outbound adapters from RouteRef; remove the duplicate outbound PlatformID field and its fallback propagation.
4. Run all packages to catch missed producers/fixtures and race-test runner, loop, platforms and root package. Commit the migration.

## 3. Use RouteRef for send, history and schedules

Files: cap/{delivery,chathistory,schedule}; runtime/{delivery,rctx}; plugins/tool/{send,chathistory,schedule}; plugins/schedule; preset dependency wiring and tests.

1. Add tests for explicit destination resolution, instance-aware history selection, schedule JSON round trips and legacy schedule reads.
2. Return RouteRef from pure destination resolution. Remove delivery.Route and duplicate message options; remove history's dependency on the sending capability where it only resolves addresses.
3. Carry RouteRef in history requests. Store stable RouteRef plus separate actor/agent attribution for schedules; strip ReplyTo when capturing a persistent target.
4. Keep legacy schedule decoding in its persistence DTO, always write the new representation. Preserve session-mode and tenant behavior.
5. Run targeted race tests, then `./scripts/test.sh unit` and `./scripts/test.sh integration`.

## Completion

Update migration and routing documentation. Independently review the final diff, create a PR stacked on the route cleanup branch, and verify all CI jobs for the final commit. Preserve the existing unmerged PRs and the original checkout.
