package runner

import (
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/pluginkit/build"
)

func bindSubagentSubmit(result *build.Result, submit capschedule.SubmitFunc) {
	_ = build.WireContributions(
		result,
		func(_ capsubagent.SubmitBinder, providers []capsubagent.SubmitBinder) error {
			for _, p := range providers {
				if p != nil {
					p.BindSubmit(submit)
				}
			}
			return nil
		},
	)
}
