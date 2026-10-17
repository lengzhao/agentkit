package workflow

import (
	"fmt"
	"sort"
	"text/template"
)

// dependencies returns the ids step i waits for. A step without an explicit
// dependsOn waits for the previous step in file order (the serial default,
// keeping pre-DAG workflow files unchanged); an explicit dependsOn — even an
// empty array — replaces that implicit edge.
func (wf *Workflow) dependencies(i int) []string {
	step := wf.Steps[i]
	if step.DependsOn != nil {
		return step.DependsOn
	}
	if i == 0 {
		return nil
	}
	return []string{wf.Steps[i-1].ID}
}

func (wf *Workflow) stepIndex() map[string]int {
	index := make(map[string]int, len(wf.Steps))
	for i := range wf.Steps {
		index[wf.Steps[i].ID] = i
	}
	return index
}

// validateGraph checks when template syntax, dependsOn references, and
// acyclicity over the resolved dependency edges (implicit serial edges
// included). Called from validate after the per-step checks.
func (wf *Workflow) validateGraph() error {
	index := wf.stepIndex()
	for i := range wf.Steps {
		step := &wf.Steps[i]
		fail := func(format string, args ...any) error {
			return fmt.Errorf("workflow %q step %q: %s", wf.Name, step.ID, fmt.Sprintf(format, args...))
		}
		if step.When != "" {
			_, err := template.New(wf.Name + "." + step.ID + ".when").Funcs(templateFuncs(nil)).Parse(step.When)
			if err != nil {
				return fail("invalid when template: %v", err)
			}
		}
		seen := make(map[string]bool, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			switch {
			case dep == step.ID:
				return fail("dependsOn must not reference itself")
			case seen[dep]:
				return fail("duplicate dependsOn %q", dep)
			}
			seen[dep] = true
			if _, ok := index[dep]; !ok {
				return fail("dependsOn %q matches no step id", dep)
			}
		}
	}
	// Cycle check over resolved edges: implicit serial edges never cycle, but
	// a dependsOn pointing backwards across them can (a → b with b also
	// depending, directly or not, on a).
	const (
		white = iota // unvisited
		gray         // on the current DFS path
		black        // done
	)
	color := make([]int, len(wf.Steps))
	var visit func(i int) error
	visit = func(i int) error {
		color[i] = gray
		for _, dep := range wf.dependencies(i) {
			j := index[dep]
			switch color[j] {
			case gray:
				return fmt.Errorf("workflow %q: dependency cycle between steps %q and %q", wf.Name, wf.Steps[i].ID, dep)
			case white:
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		color[i] = black
		return nil
	}
	for i := range wf.Steps {
		if color[i] == white {
			if err := visit(i); err != nil {
				return err
			}
		}
	}
	return nil
}

// levels groups step indices into dependency levels: every step's deps sit in
// strictly earlier levels, so steps inside one level can run concurrently
// (they only read template data produced by earlier levels). File order is
// preserved within a level. Returns nil when the graph has a cycle — engine
// runs go through validate first, this only guards hand-built workflows.
func (wf *Workflow) levels() [][]int {
	index := wf.stepIndex()
	unmet := make([]int, len(wf.Steps))
	dependents := make([][]int, len(wf.Steps))
	for i := range wf.Steps {
		seen := make(map[int]bool)
		for _, dep := range wf.dependencies(i) {
			j, ok := index[dep]
			if !ok || seen[j] {
				continue
			}
			seen[j] = true
			dependents[j] = append(dependents[j], i)
			unmet[i]++
		}
	}
	var frontier []int
	for i := range wf.Steps {
		if unmet[i] == 0 {
			frontier = append(frontier, i)
		}
	}
	var levels [][]int
	done := 0
	for len(frontier) > 0 {
		levels = append(levels, frontier)
		done += len(frontier)
		var next []int
		for _, i := range frontier {
			for _, j := range dependents[i] {
				unmet[j]--
				if unmet[j] == 0 {
					next = append(next, j)
				}
			}
		}
		sort.Ints(next)
		frontier = next
	}
	if done < len(wf.Steps) {
		return nil
	}
	return levels
}

// criticalPathSeconds sums step timeouts along the longest dependency chain.
// Serial workflows (no dependsOn) form a single chain, so this equals the
// previous sum-of-all-steps; parallel branches overlap and stop inflating the
// estimate.
func (wf *Workflow) criticalPathSeconds() int {
	index := wf.stepIndex()
	memo := make([]int, len(wf.Steps))
	known := make([]bool, len(wf.Steps))
	var chain func(i int) int
	chain = func(i int) int {
		if known[i] {
			return memo[i]
		}
		best := 0
		for _, dep := range wf.dependencies(i) {
			j, ok := index[dep]
			if !ok {
				continue
			}
			if c := chain(j); c > best {
				best = c
			}
		}
		memo[i] = best + wf.Steps[i].effectiveTimeoutSeconds()
		known[i] = true
		return memo[i]
	}
	total := 0
	for i := range wf.Steps {
		if c := chain(i); c > total {
			total = c
		}
	}
	return total
}
