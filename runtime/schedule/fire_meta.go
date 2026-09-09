package schedule

import capschedule "github.com/lengzhao/agentkit/cap/schedule"

const (
	SessionModeStateless = capschedule.SessionModeStateless
	SessionModeReuse     = capschedule.SessionModeReuse
	SessionModeFresh     = capschedule.SessionModeFresh
	SessionModeFixed     = capschedule.SessionModeFixed
)

func FireMeta(metadata map[string]any) (map[string]any, bool) {
	return capschedule.FireMeta(metadata)
}

func SessionModeFromMeta(meta map[string]any) string {
	return capschedule.SessionModeFromMeta(meta)
}

func IsStatelessSessionMode(mode string) bool {
	return capschedule.IsStatelessSessionMode(mode)
}

func IsFireTurn(metadata map[string]any) bool {
	return capschedule.IsFireTurn(metadata)
}

func IsFireStateless(metadata map[string]any) bool {
	return capschedule.IsFireStateless(metadata)
}
