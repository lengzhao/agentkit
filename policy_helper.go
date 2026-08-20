package agentkit

import "context"

func Allow() Decision {
	return Decision{Kind: DecisionAllow}
}

func Deny(reason string) Decision {
	return Decision{Kind: DecisionDeny, Reason: reason}
}

func Ask(reason string) Decision {
	return Decision{Kind: DecisionAsk, Reason: reason}
}

type policyFunc func(context.Context, PolicyInput) Decision

func (f policyFunc) Evaluate(ctx context.Context, in PolicyInput) (Decision, error) {
	return f(ctx, in), nil
}

// PolicyFunc wraps a function as a Policy implementation.
func PolicyFunc(fn func(context.Context, PolicyInput) Decision) Policy {
	return policyFunc(fn)
}
