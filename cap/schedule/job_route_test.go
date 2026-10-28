package schedule_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/schedule"
)

func TestJobReadsLegacyRouteAndWritesCanonicalDestination(t *testing.T) {
	var job schedule.Job
	if err := json.Unmarshal([]byte(`{"id":"j1","route":{"deliverySessionId":"slack:C1:t:1","platformId":"slack1","userId":"U1","agentId":"assistant"}}`), &job); err != nil {
		t.Fatal(err)
	}
	if job.Route.Platform != "slack" || job.Route.DestinationID() != "slack1" || job.Actor.UserID != "U1" || job.AgentID != "assistant" {
		t.Fatalf("legacy route was not migrated: %+v", job)
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "deliverySessionId") || strings.Contains(string(raw), "platformId") {
		t.Fatalf("legacy route written: %s", raw)
	}
	var restored schedule.Job
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Route.DestinationID() != "slack1" || restored.Actor.UserID != "U1" {
		t.Fatalf("round trip lost destination: %+v", restored)
	}
}

func TestJobPersistenceDropsEphemeralReplyAnchor(t *testing.T) {
	target, _ := json.Marshal(agentkit.SessionRouteTarget{DeliveryID: "slack:C1:t:1", ChannelID: "C1", ThreadID: "1", ReplyTo: "old-message"})
	job := schedule.Job{ID: "j1", Route: agentkit.RouteRef{Platform: "slack", Instance: "slack1", Kind: agentkit.RouteKindSession, Target: target}}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "old-message") {
		t.Fatalf("job persisted an ephemeral anchor: %s", raw)
	}
	if !strings.Contains(string(job.Route.Target), "old-message") {
		t.Fatal("persisting job mutated original route")
	}
}
