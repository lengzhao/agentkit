// Package rctx holds the runtime-context protocol: readers and writers for
// every value carried on context.Context during execution (session, envelope,
// route, metadata, workspace key, outbound emit), plus inbound MessageEvent
// route helpers (WithInboundRoute, WithDeliveryRoute, WithDeliverySession)
// and outbound payload encoding. SessionContext is the history snapshot;
// execution.Identity identifies a single invocation. This package depends on
// root/cap contracts, never lifecycle or backend implementations. All runtime
// layers may depend on it; scope lifecycle lives in runtime/execution.
package rctx
