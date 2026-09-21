package permission

import "time"

func EffectiveTimeout(req Request, cap Capability) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	if cap.DefaultTimeout > 0 {
		return cap.DefaultTimeout
	}
	if cap.Interactive {
		return DefaultTimeout
	}
	return 0
}

func NoHuman(req Request, reason string) Result {
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome:  OutcomeNoHuman,
			Allow:    false,
			Reason:   reason,
			Guidance: "No interactive user is available; treat this tool call as denied and continue without it.",
		}
	default:
		return Result{
			Outcome:  OutcomeNoHuman,
			Reason:   reason,
			Guidance: "No interactive user is available; state your assumptions and continue.",
		}
	}
}

func TimedOut(req Request) Result {
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome:  OutcomeTimeout,
			Allow:    false,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; treat this tool call as denied and continue.",
		}
	default:
		return Result{
			Outcome:  OutcomeTimeout,
			Reason:   "permission timed out",
			Guidance: "The user did not respond in time; state your assumptions and continue.",
		}
	}
}

func Cancelled(req Request, reason string) Result {
	if reason == "" {
		reason = "permission cancelled"
	}
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome: OutcomeCancelled,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return Result{
			Outcome:  OutcomeCancelled,
			Reason:   reason,
			Guidance: "The user declined to answer; state your assumptions and continue.",
		}
	}
}

func Superseded(req Request, reason string) Result {
	if reason == "" {
		reason = "superseded by new inbound message"
	}
	switch req.Kind {
	case KindAllowDeny:
		return Result{
			Outcome: OutcomeSuperseded,
			Allow:   false,
			Reason:  reason,
		}
	default:
		return Result{
			Outcome:  OutcomeSuperseded,
			Reason:   reason,
			Guidance: "The user sent a new message instead of answering; state your assumptions and continue.",
		}
	}
}
