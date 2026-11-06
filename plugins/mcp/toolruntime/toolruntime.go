// Package toolruntime registers the optional, transport-neutral mcp/tool-runtime
// adapter. Import it alone to use MCP tools without ACP or a running agent loop.
package toolruntime

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/mcpbridge"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}
type Deps struct {
	Runtime agentkit.ToolRuntime `json:"runtime"`
}

func init()                                              { pluginkit.Register("mcp/tool-runtime", New) }
func New(_ Config, deps Deps) (*mcpbridge.Bridge, error) { return mcpbridge.New(deps.Runtime) }
