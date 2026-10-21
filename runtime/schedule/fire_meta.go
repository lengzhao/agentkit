package schedule

import capschedule "github.com/lengzhao/agentkit/cap/schedule"

// Deprecated: schedule fire-metadata helpers moved to cap/schedule so
// runtime/loop and runtime/runner do not pull in the engine implementation.
// This alias keeps the old import path working; new code should import
// cap/schedule directly.
var (
	FireMeta               = capschedule.FireMeta
	SessionModeFromMeta    = capschedule.SessionModeFromMeta
	IsStatelessSessionMode = capschedule.IsStatelessSessionMode
	IsFireTurn             = capschedule.IsFireTurn
	IsFireStateless        = capschedule.IsFireStateless
)
