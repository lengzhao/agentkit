package feishu

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/pluginkit"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func init() {
	pluginkit.Register("platform/feishu", New)
	pluginkit.Register("platform/lark", func(cfg Config, deps Deps) (agentkit.Platform, error) {
		return newPlatform("lark", lark.LarkBaseUrl, cfg, deps)
	})
}

var (
	_ agentkit.Platform       = (*Platform)(nil)
	_ permission.Capable      = (*Platform)(nil)
	_ chathistory.Provider    = (*Platform)(nil)
)
