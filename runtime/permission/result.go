package permission

import (
	"time"

	capspermission "github.com/lengzhao/agentkit/cap/permission"
)

func EffectiveTimeout(req capspermission.Request, cap capspermission.Capability) time.Duration {
	return capspermission.EffectiveTimeout(req, cap)
}

func NoHuman(req capspermission.Request, reason string) capspermission.Result {
	return capspermission.NoHuman(req, reason)
}

func TimedOut(req capspermission.Request) capspermission.Result {
	return capspermission.TimedOut(req)
}

func Cancelled(req capspermission.Request, reason string) capspermission.Result {
	return capspermission.Cancelled(req, reason)
}

func Superseded(req capspermission.Request, reason string) capspermission.Result {
	return capspermission.Superseded(req, reason)
}
