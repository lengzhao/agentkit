package compaction

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("compaction/token-limit", NewTokenLimit)
	pluginkit.Register("compaction/pipeline", NewPipeline)
}
