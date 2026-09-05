package session_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestRouteSessionIDUsesDeliveryIDField(t *testing.T) {
	t.Parallel()

	route := agentkit.SessionRoute("slack", "slack:C001:t:1:u:U1")
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001:t:1:u:U1" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
}

func TestRouteSessionIDBuildsFromParts(t *testing.T) {
	t.Parallel()

	route := session.BuildSessionRoute(session.SessionRouteInput{
		Platform:    "slack",
		ChannelID:   "C001",
		ThreadID:    "1.0",
		ScopeUserID: "U1",
	})
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001:t:1.0:u:U1" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
	target, ok := session.DecodeSessionRoute(route)
	if !ok || string(target.DeliveryID) != "slack:C001:t:1.0:u:U1" {
		t.Fatalf("deliveryId = %q ok = %v", target.DeliveryID, ok)
	}
}

func TestRouteSessionIDDecodesStringData(t *testing.T) {
	t.Parallel()

	route := agentkit.SessionRoute("slack", "slack:C001:t:1:u:U1")
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001:t:1:u:U1" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
}

func TestRouteSessionIDAcceptsLegacyStringData(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"platform":"slack",
		"kind":"session",
		"data":"slack:C001"
	}`)
	var route agentkit.RouteRef
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
}

func TestRouteSessionIDAcceptsLegacyObjectData(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"platform":"slack",
		"kind":"session",
		"data":{"id":"slack:C001"}
	}`)
	var route agentkit.RouteRef
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
}

func TestRouteSessionIDAcceptsLegacySessionField(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"platform":"slack",
		"kind":"session",
		"session":{"deliveryId":"slack:C001:t:1:u:U1"}
	}`)
	var route agentkit.RouteRef
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}
	id, ok := session.RouteSessionID(route)
	if !ok || id != "slack:C001:t:1:u:U1" {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
}

func TestSessionRouteFromDeliveryPopulatesParts(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "1.0", "U1")
	route := session.SessionRouteFromDelivery("slack", delivery, "msg-1")
	id, ok := session.RouteSessionID(route)
	if !ok || id != delivery {
		t.Fatalf("id = %q ok = %v", id, ok)
	}
	target, ok := session.RouteTargetFromRoute(route)
	if !ok {
		t.Fatal("RouteTargetFromRoute failed")
	}
	if target.ChannelID != "C001" || target.ThreadID != "1.0" || target.ScopeUserID != "U1" || target.ReplyTo != "msg-1" {
		t.Fatalf("target = %+v", target)
	}
}

func TestRouteTargetFromRouteFillsPartsFromDeliveryID(t *testing.T) {
	t.Parallel()

	route := agentkit.SessionRoute("slack", "slack:C001:t:1.0:u:U1")
	target, ok := session.RouteTargetFromRoute(route)
	if !ok {
		t.Fatal("RouteTargetFromRoute failed")
	}
	if target.ChannelID != "C001" || target.ThreadID != "1.0" || target.ScopeUserID != "U1" {
		t.Fatalf("target = %+v", target)
	}
}

func TestSessionRouteJSONRoundTrip(t *testing.T) {
	t.Parallel()

	route := session.BuildSessionRoute(session.SessionRouteInput{
		Platform:    "slack",
		ChannelID:   "C001",
		ThreadID:    "1.0",
		ReplyTo:     "msg-1",
		ScopeUserID: "U1",
	})
	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"session":`) {
		t.Fatalf("marshal should not use legacy session field, got %s", raw)
	}
	if !strings.Contains(string(raw), `"target"`) {
		t.Fatalf("marshal should include target, got %s", raw)
	}
	var decoded agentkit.RouteRef
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	target, ok := session.RouteTargetFromRoute(decoded)
	if !ok {
		t.Fatal("RouteTargetFromRoute failed")
	}
	if target.ReplyTo != "msg-1" || target.ChannelID != "C001" {
		t.Fatalf("target = %+v", target)
	}
}

func TestRouteRefUnmarshalLegacyFlatFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"platform":"slack",
		"kind":"session",
		"sessionId":"slack:C001",
		"channelId":"C001",
		"threadId":"1.0",
		"messageId":"msg-1",
		"userId":"U1"
	}`)
	var route agentkit.RouteRef
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}
	target, ok := session.RouteTargetFromRoute(route)
	if !ok {
		t.Fatal("RouteTargetFromRoute failed")
	}
	if target.DeliveryID != "slack:C001" || target.ReplyTo != "msg-1" || target.ScopeUserID != "U1" {
		t.Fatalf("target = %+v", target)
	}
}

func TestRouteRefHasTarget(t *testing.T) {
	t.Parallel()

	if (agentkit.RouteRef{}).HasTarget() {
		t.Fatal("empty route should not have target")
	}
	if !agentkit.SessionRoute("slack", "slack:C001").HasTarget() {
		t.Fatal("session route should have target")
	}
	if !session.BuildSessionRoute(session.SessionRouteInput{
		Platform:  "slack",
		ChannelID: "C001",
	}).HasTarget() {
		t.Fatal("parts-only route should have target")
	}
}

func TestRouteRefIsZero(t *testing.T) {
	t.Parallel()

	if !(agentkit.RouteRef{}).IsZero() {
		t.Fatal("empty route should be zero")
	}
	if agentkit.SessionRoute("slack", "slack:C001").IsZero() {
		t.Fatal("session route should not be zero")
	}
}

func TestDecodeSessionRoutePayload(t *testing.T) {
	t.Parallel()

	route := session.BuildSessionRoute(session.SessionRouteInput{
		Platform:  "slack",
		ChannelID: "C001",
	})
	payload, ok := session.DecodeSessionRoute(route)
	if !ok {
		t.Fatal("DecodeSessionRoute failed")
	}
	if !payload.HasTarget() {
		t.Fatal("payload should have target")
	}
	if route.Kind != agentkit.RouteKindSession {
		t.Fatalf("kind = %q", route.Kind)
	}
}
