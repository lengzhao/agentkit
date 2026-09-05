package permission

import (
	"time"

	capspermission "github.com/lengzhao/agentkit/cap/permission"
)

// EffectiveTimeout resolves the wait limit for an interactive permission request.
func EffectiveTimeout(req capspermission.Request, cap capspermission.Capability) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	if cap.DefaultTimeout > 0 {
		return cap.DefaultTimeout
	}
	if cap.Interactive {
		return capspermission.DefaultTimeout
	}
	return 0
}

func NoHuman(req capspermission.Request, reason string) capspermission.Result {
	switch req.Kind {
	case capspermission.KindAllowDeny:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeNoHuman,
			Allow:    false,
			Reason:   reason,
			Guidance: "No interactive user is available; treat this tool call as denied and continue without it.",
		}
	default:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeNoHuman,
			Reason:   reason,
			Guidance: "No interactive user is available; state your assumptions and continue.",
		}
	}
}

func TimedOut(req capspermission.Request) capspermission.Result {
	switch req.Kind {
	case capspermission.KindAllowDeny:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeTimeout,
			Allow:    false,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; treat this tool call as denied and continue.",
		}
	default:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeTimeout,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; state your assumptions and continue.",
		}
	}
}

func Cancelled(req capspermission.Request, reason string) capspermission.Result {
	if reason == "" {
		reason = "permission cancelled"
	}
	switch req.Kind {
	case capspermission.KindAllowDeny:
		return capspermission.Result{
			Outcome: capspermission.OutcomeCancelled,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeCancelled,
			Reason:   reason,
			Guidance: "The user declined to answer; state your assumptions and continue.",
		}
	}
}

func Superseded(req capspermission.Request, reason string) capspermission.Result {
	if reason == "" {
		reason = "superseded by new inbound message"
	}
	switch req.Kind {
	case capspermission.KindAllowDeny:
		return capspermission.Result{
			Outcome: capspermission.OutcomeSuperseded,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return capspermission.Result{
			Outcome:  capspermission.OutcomeSuperseded,
			Reason:   reason,
			Guidance: "The user sent a new message instead of answering; state your assumptions and continue.",
		}
	}
}
