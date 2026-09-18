package sessstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/lengzhao/agentkit"
)

// newJSONLDecoder returns a streaming JSON decoder over r. Unlike bufio.Scanner,
// json.Decoder has no per-record size limit, so oversized JSONL lines (large
// images, long tool outputs, accumulated long conversations) no longer crash
// the session loader with "bufio.Scanner: token too long".
func newJSONLDecoder(r io.Reader) *json.Decoder {
	return json.NewDecoder(bufio.NewReader(r))
}

// ScanSessionFile loads one session JSONL file, folding compacted history the
// same way the store does. maxLoadedEvents <= 0 loads every retained event.
// Exported for the session index, which reads the same on-disk format.
func ScanSessionFile(path string, maxLoadedEvents int) ([]agentkit.SessionEvent, agentkit.EventSeq, bool, error) {
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

	dec := newJSONLDecoder(f)
	for {
		var ev agentkit.SessionEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
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
	dec := newJSONLDecoder(f)
	for {
		var ev agentkit.SessionEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	return out, nil
}
