package learning

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestPolicySkillsMode(t *testing.T) {
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	svc, err := New(Config{Workshop: workshop.Config{Mode: "propose"}}, Deps{
		Workspace:    ws,
		SessionStore: stubSessionStore{},
		Memory:       newTestMemoryStub(ws),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.setSkillsPolicy(ctx, []string{"off"}); err != nil {
		t.Fatal(err)
	}
	if svc.skillsWorkshopEnabled(ctx) {
		t.Fatal("expected skills off")
	}
}
