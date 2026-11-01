package ingress

import "testing"

func TestResolve(t *testing.T) {
	base := Plan{Agent: Submit, Handlers: true}
	input := Input{Kind: Event, Default: base}
	got, err := Resolve(input, Decision{})
	if err != nil || got != base {
		t.Fatalf("zero decision changed defaults: %+v %v", got, err)
	}
	got, err = Resolve(input, Decision{Agent: Skip})
	if err != nil || got.Agent != Skip || !got.Handlers {
		t.Fatal(got, err)
	}
	got, err = Resolve(input, Decision{Reject: true})
	if err != nil || got.Agent != Skip || got.Handlers {
		t.Fatal(got, err)
	}
	for _, action := range []AgentAction{Defer, "invalid"} {
		if _, err := Resolve(input, Decision{Agent: action}); err == nil {
			t.Fatalf("accepted %q for event", action)
		}
	}
	input.Default.Agent = Skip
	if _, err := Resolve(input, Decision{Agent: Submit}); err == nil {
		t.Fatal("enabled unsubscribed event forwarding")
	}
	input.Kind = Message
	input.Message = &MessageFacts{AttachmentOnly: true}
	got, err = Resolve(input, Decision{Agent: Defer})
	if err != nil || got.Agent != Defer {
		t.Fatal(got, err)
	}
	input.Message.AttachmentOnly = false
	if _, err := Resolve(input, Decision{Agent: Defer}); err == nil {
		t.Fatal("deferred ordinary text")
	}
}
