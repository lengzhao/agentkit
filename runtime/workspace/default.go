package workspace

import (
	"context"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Root string `json:"root"`
}

type Service struct {
	root string
}

func init() {
	pluginkit.Register("workspace/default", New)
}

func New(cfg Config) (cw.Service, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	abs, err := cw.Resolve(root)
	if err != nil {
		return nil, err
	}
	return &Service{root: abs}, nil
}

func (s *Service) Resolve(ctx context.Context, rel string) (string, error) {
	return cw.ResolveRel(s.root, rel)
}

var _ cw.Service = (*Service)(nil)
