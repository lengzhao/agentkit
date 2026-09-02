package agentkit

import "context"

// AppInitializer performs one-time setup before the runner serves traffic.
// Examples: seed workspace directories, copy bundled agents/skills, init git.
//
// Config (bootstrap/shell):
//
//	runner.default:
//	  deps:
//	    init:
//	      - bootstrap.shell.default
//
//	bootstrap.shell.default:
//	  use: bootstrap/shell
//	  config:
//	    commands:
//	      - echo "init"
//	  deps:
//	    workspace: workspace.default
//
// Distinct from StartStop.Start: InitApp runs once at process start and should
// be idempotent; Start launches long-running background work.
type AppInitializer interface {
	InitApp(context.Context) error
}
