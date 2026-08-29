package session

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/lengzhao/agentkit"
)

func scanSessionFile(path string, maxLoadedEvents int) ([]agentkit.SessionEvent, agentkit.EventSeq, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	defer f.Close()

	var (
		compactions       []agentkit.SessionEvent
		ring              eventRing
		maxSeq            agentkit.EventSeq
		nonCompactionSeen int
	)
	ring.max = maxLoadedEvents

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, 0, false, err
		}
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
		if ev.Type == agentkit.EventCompaction {
			compactions = append(compactions, ev)
			continue
		}
		nonCompactionSeen++
		ring.add(ev)
	}
	if err := sc.Err(); err != nil {
		return nil, 0, false, err
	}

	cutoffs := cutoffsFromCompactions(compactions)
	events := mergeLoadedEvents(compactions, ring.buf, cutoffs)
	trimmed := len(cutoffs) > 0 || (maxLoadedEvents > 0 && nonCompactionSeen > maxLoadedEvents)
	return events, maxSeq, trimmed, nil
}

func readSessionFile(path string, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	out := make([]agentkit.SessionEvent, 0)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, err
		}
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
