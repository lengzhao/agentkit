package shell

import (
	"context"
	"time"
)

type Executor interface {
	Run(context.Context, Request) (Result, error)
}

type Request struct {
	Command string
	WorkDir string
	Timeout time.Duration
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}
