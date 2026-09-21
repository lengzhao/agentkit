// Package rctx holds the runtime-context protocol: readers and writers for
// every value carried on context.Context during execution (session, envelope,
// route, metadata, workspace key, outbound emit), plus inbound MessageEvent
// route helpers (WithInboundRoute, WithDeliveryRoute, WithDeliverySession)
// and outbound payload encoding. It depends only on the root agentkit package;
// all runtime layers may depend on it.
package rctx
