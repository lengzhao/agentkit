package compaction

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("compaction/chain", NewChain)
	pluginkit.Register("compaction/summary", NewSummary)
	pluginkit.Register("compaction/prune-tool-results", NewPrune)
}
