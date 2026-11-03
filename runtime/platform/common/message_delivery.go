package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// MessageRoute validates leaf ownership without cached inbound/turn state.
func MessageRoute(ctx context.Context, route agentkit.RouteRef, protocol, instance string) (agentkit.RouteRef, agentkit.SessionRouteTarget, error) {
	if err := ctx.Err(); err != nil {
		return route, agentkit.SessionRouteTarget{}, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeNotSent, Cause: err}
	}
	if instance == "" {
		instance = protocol
	}
	if route.DestinationID() != instance || (route.Platform != "" && route.Platform != protocol) {
		return route, agentkit.SessionRouteTarget{}, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	target, ok := rctx.DecodeSessionRoute(route)
	if !ok || strings.TrimSpace(target.ChannelID) == "" {
		return route, target, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: protocol, Instance: instance, ChannelID: target.ChannelID, ThreadID: target.ThreadID})
	return route, target, nil
}

// SplitMessageContent validates the entire request before side effects. A chunk
// has <=3000 runes and <=100 nodes, with mentions kept intact. Limits leave room
// for platform JSON/escaping overhead. Media types are explicitly unsupported.
func SplitMessageContent(parts []cap.Part) ([][]cap.Part, error) {
	var chunks [][]cap.Part
	var chunk []cap.Part
	size := 0
	flush := func() {
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
			chunk = nil
			size = 0
		}
	}
	for _, p := range parts {
		if !utf8.ValidString(p.Text) || !utf8.ValidString(p.UserID) {
			return nil, &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
		}
		switch p.Type {
		case "text":
			if p.UserID != "" {
				return nil, &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
			}
			runes := []rune(p.Text)
			for len(runes) > 0 {
				if size == 3000 || len(chunk) == 100 {
					flush()
				}
				n := min(3000-size, len(runes))
				chunk = append(chunk, cap.Part{Type: "text", Text: string(runes[:n])})
				size += n
				runes = runes[n:]
			}
		case "mention":
			if p.UserID == "" || len(p.UserID) > 128 || strings.ContainsAny(p.UserID, "<>\"'& \t\r\n") || p.Text != "" {
				return nil, &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
			}
			weight := utf8.RuneCountInString(p.UserID) + 16
			if size+weight > 3000 || len(chunk) == 100 {
				flush()
			}
			chunk = append(chunk, p)
			size += weight
		default:
			return nil, &cap.Error{Code: cap.CodeUnsupported, Outcome: cap.OutcomeNotSent}
		}
	}
	flush()
	if len(chunks) == 0 {
		return nil, &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
	}
	return chunks, nil
}

// MessageOperationKey is stable across retries/restarts and distinct across
// caller, platform instance, target, operation and physical part. Content is
// intentionally excluded: reusing an operation ID with new content is invalid.
func MessageOperationKey(req cap.PublishRequest, operation string, index int) string {
	if req.OperationID == "" {
		return ""
	}
	raw, _ := json.Marshal(struct {
		Caller, ID, Operation string
		Route                 agentkit.RouteRef
		Part                  int
	}{req.Caller, req.OperationID, operation, req.Route.Stable(), index})
	hash := sha256.Sum256(raw)
	s := hex.EncodeToString(hash[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[:8], s[8:12], s[12:16], s[16:20], s[20:])
}
func MessageReceipt(route agentkit.RouteRef) cap.Receipt {
	return cap.Receipt{Route: route, Outcome: cap.OutcomeNotSent, Deduplication: cap.Deduplication{Mode: "none"}}
}
func FailMessagePart(receipt cap.Receipt, index int, err error) (cap.Receipt, error) {
	receipt.FailedPart = &index
	if e, ok := err.(*cap.Error); ok {
		receipt.Outcome = e.Outcome
		if e.Outcome == cap.OutcomeNotSent && len(receipt.Messages) > 0 {
			receipt.Outcome = cap.OutcomePartial
		}
	} else {
		receipt.Outcome = cap.OutcomeUnknown
	}
	return receipt, err
}
