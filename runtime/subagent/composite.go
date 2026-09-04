package subagent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("subagent/composite", NewComposite)
}

// CompositeDeps merges multiple spawners. Inprocess definitions win on name conflict.
type CompositeDeps struct {
	Inprocess subagent.Spawner `json:"inprocess"`
	Loop      subagent.Spawner `json:"loop,omitempty"`
}

type compositeSpawner struct {
	inprocess subagent.Spawner
	loop      subagent.Spawner
}

var _ subagent.Spawner = (*compositeSpawner)(nil)
var _ subagent.SubmitBinder = (*compositeSpawner)(nil)

// NewComposite registers subagent/composite: merge inprocess and loop-agent delegatable catalogs.
func NewComposite(_ struct{}, deps CompositeDeps) (subagent.Spawner, error) {
	if deps.Inprocess == nil {
		return nil, fmt.Errorf("subagent/composite requires inprocess dependency")
	}
	return &compositeSpawner{
		inprocess: deps.Inprocess,
		loop:      deps.Loop,
	}, nil
}

func (c *compositeSpawner) Definitions(ctx context.Context) ([]subagent.Definition, error) {
	merged, _, err := c.loadOwners(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Name < merged[j].Name })
	return merged, nil
}

func (c *compositeSpawner) Run(ctx context.Context, req subagent.Request) (subagent.Result, error) {
	_, owners, err := c.loadOwners(ctx)
	if err != nil {
		return subagent.Result{}, err
	}
	name := strings.TrimSpace(req.Agent)
	owner, ok := owners[strings.ToLower(name)]
	if !ok {
		return subagent.Result{}, fmt.Errorf("unknown subagent %q", name)
	}
	return owner.Run(ctx, req)
}

func (c *compositeSpawner) BindSubmit(fn capschedule.SubmitFunc) {
	if c.loop == nil {
		return
	}
	if binder, ok := c.loop.(subagent.SubmitBinder); ok {
		binder.BindSubmit(fn)
	}
}

func (c *compositeSpawner) loadOwners(ctx context.Context) ([]subagent.Definition, map[string]subagent.Spawner, error) {
	inDefs, err := c.inprocess.Definitions(ctx)
	if err != nil {
		return nil, nil, err
	}
	owners := make(map[string]subagent.Spawner, len(inDefs))
	merged := make([]subagent.Definition, 0, len(inDefs))
	for _, def := range inDefs {
		owners[strings.ToLower(def.Name)] = c.inprocess
		merged = append(merged, def)
	}
	if c.loop != nil {
		loopDefs, err := c.loop.Definitions(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, def := range loopDefs {
			key := strings.ToLower(def.Name)
			if _, exists := owners[key]; exists {
				continue
			}
			owners[key] = c.loop
			merged = append(merged, def)
		}
	}
	return merged, owners, nil
}
