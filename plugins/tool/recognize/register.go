package recognize

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/recognize", NewRecognize)
}
