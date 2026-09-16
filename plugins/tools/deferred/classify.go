package deferred

import "github.com/lengzhao/agentkit"

type classified struct {
	Eager      []agentkit.ToolSpec
	Deferrable []agentkit.ToolSpec
}

func classify(specs []agentkit.ToolSpec, eager map[string]bool, deferForce map[string]bool) classified {
	out := classified{}
	for _, spec := range specs {
		name := spec.Name
		if IsBridge(name) {
			continue
		}
		if eager[name] {
			out.Eager = append(out.Eager, spec)
			continue
		}
		if deferForce[name] {
			out.Deferrable = append(out.Deferrable, spec)
			continue
		}
		out.Deferrable = append(out.Deferrable, spec)
	}
	return out
}
