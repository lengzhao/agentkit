package common

import (
	"errors"
	"testing"
)

func TestInteractionDedupCommitsOnlySuccessfulWork(t *testing.T) {
	var d InteractionDedup
	finish, duplicate := d.Begin("event")
	if duplicate {
		t.Fatal("new event is duplicate")
	}
	if _, duplicate = d.Begin("event"); !duplicate {
		t.Fatal("in-flight duplicate accepted")
	}
	finish(errors.New("delivery failed"))
	finish, duplicate = d.Begin("event")
	if duplicate {
		t.Fatal("failed event cannot retry")
	}
	finish(nil)
	if _, duplicate = d.Begin("event"); !duplicate {
		t.Fatal("successful event replayed")
	}
}

func TestInteractionDedupConcurrentReservations(t *testing.T) {
	var d InteractionDedup
	const count = 20
	results := make(chan bool, count)
	completions := make(chan func(error), count)
	for i := 0; i < count; i++ {
		go func() {
			finish, duplicate := d.Begin("same")
			results <- duplicate
			if !duplicate {
				completions <- finish
			}
		}()
	}
	owners := 0
	for i := 0; i < count; i++ {
		if !<-results {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("concurrent callback had %d owners", owners)
	}
	(<-completions)(nil)
}
