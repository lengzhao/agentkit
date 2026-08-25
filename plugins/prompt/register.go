package prompt

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("prompt/section/agents-md", NewAgentsMD)
	pluginkit.Register("prompt/section/skills", NewSkillsSection)
	pluginkit.Register("prompt/section/static", NewStatic)
	pluginkit.Register("prompt/section/subagents", NewSubagentsSection)
}
