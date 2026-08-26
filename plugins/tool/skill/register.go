package skill

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/skill", NewSkill)
}
