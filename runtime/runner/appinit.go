package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

const bootstrapUserID = "agentkit"

func runAppInit(ctx context.Context, result *build.Result) error {
	if result == nil {
		return nil
	}
	ctx = session.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{
		Route: agentkit.RouteRef{Platform: "bootstrap"},
		Actor: agentkit.ActorRef{UserID: bootstrapUserID},
	})
	for _, inst := range build.CollectInstances[agentkit.AppInitializer](result) {
		slog.Info("app init starting", "instance_id", inst.ID, "use", inst.Use)
		if err := inst.Value.InitApp(ctx); err != nil {
			return fmt.Errorf("app init %q (%s): %w", inst.ID, inst.Use, err)
		}
		slog.Info("app init finished", "instance_id", inst.ID, "use", inst.Use)
	}
	return nil
}
