package main

import (
	"log/slog"
	"strings"

	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

func logConfigLoaded(basePath, overlayPath string, doc manager.Document) {
	overlay := overlayPath
	if overlay == "" {
		overlay = "(none)"
	}
	slog.Info("config loaded",
		"base", basePath,
		"overlay", overlay,
		"root", doc.RootID,
		"root_use", doc.Plugin.Use,
		"shared_instances", len(doc.Shared),
	)
	for id, node := range doc.Shared {
		slog.Debug("config instance", "id", id, "use", node.Use)
	}
}

func logRunnerBuilt(result *build.Result) {
	if result == nil {
		return
	}
	summary := summarizeInstances(result.Instances)
	slog.Info("runner built",
		"instances", len(result.Instances),
		"platforms", summary.platforms,
		"agents", summary.agents,
		"schedules", summary.schedules,
	)
	for _, inst := range result.Instances {
		slog.Debug("plugin instance", "id", inst.ID, "use", inst.Use)
	}
}

type instanceSummary struct {
	platforms []string
	agents    []string
	schedules []string
}

func summarizeInstances(instances []build.Instance) instanceSummary {
	var out instanceSummary
	for _, inst := range instances {
		switch {
		case strings.HasPrefix(inst.Use, "platform/"):
			out.platforms = append(out.platforms, inst.ID+"("+inst.Use+")")
		case strings.HasPrefix(inst.Use, "agent/"):
			out.agents = append(out.agents, inst.ID)
		case strings.HasPrefix(inst.Use, "schedule/"):
			out.schedules = append(out.schedules, inst.ID)
		}
	}
	return out
}
