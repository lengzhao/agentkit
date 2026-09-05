package compaction

import (
	"context"

	"github.com/lengzhao/agentkit"
)

type Service interface {
	Compact(context.Context, Request) (Result, error)
}

type Request struct {
	SessionID agentkit.SessionID
	AgentID   agentkit.AgentID
	Session   agentkit.Session
	Messages  []agentkit.ModelMessage
	// Force skips automatic thresholds such as minMessages (manual /compact).
	Force bool
}

type Result struct {
	Applied  bool
	Event    agentkit.SessionEvent
	Messages []agentkit.ModelMessage
}

type EventData struct {
	// BeforeSeq is legacy: highest summarized seq when FirstKeptSeq is unset.
	BeforeSeq agentkit.EventSeq `json:"beforeSeq,omitempty"`
	// FirstKeptSeq is the first retained event after compaction (Pi firstKeptEntryId).
	FirstKeptSeq agentkit.EventSeq `json:"firstKeptSeq,omitempty"`
	// RetainedTail is verbatim recent history kept for the model after compaction.
	RetainedTail []agentkit.ModelMessage `json:"retainedTail,omitempty"`
	TokensBefore int                   `json:"tokensBefore,omitempty"`
	Summary      agentkit.ModelMessage `json:"summary"`
	Kind         string                `json:"kind"`
}

const (
	KindSummary = "summary"
	KindPrune   = "prune"
)

// MemoryCutoffSeq returns the highest event seq dropped from hot memory.
func (d EventData) MemoryCutoffSeq() agentkit.EventSeq {
	if d.FirstKeptSeq > 0 {
		if d.FirstKeptSeq > 1 {
			return d.FirstKeptSeq - 1
		}
		return 0
	}
	return d.BeforeSeq
}

// PreviousSummaryText extracts prior summary body from a compaction event.
func (d EventData) PreviousSummaryText() string {
	for _, part := range d.Summary.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}
