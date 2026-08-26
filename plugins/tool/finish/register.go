package finish

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/finish", NewFinish)
}
