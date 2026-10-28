package sessstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"

	"github.com/lengzhao/agentkit"
)

var errTruncatedJSONLTail = errors.New("session jsonl truncated tail")

// newJSONLDecoder returns a streaming JSON decoder over r. Unlike bufio.Scanner,
// json.Decoder has no per-record size limit, so oversized JSONL lines (large
// images, long tool outputs, accumulated long conversations) no longer crash
// the session loader with "bufio.Scanner: token too long".
func newJSONLDecoder(r io.Reader) *json.Decoder {
	return json.NewDecoder(bufio.NewReader(r))
}

// scanJSONLDecoder reads complete JSON records until EOF. A truncated final line
// (process killed mid-write) returns the prefix with errTruncatedJSONLTail.
func scanJSONLDecoder(dec *json.Decoder) ([]agentkit.SessionEvent, error) {
	var events []agentkit.SessionEvent
	for {
		var ev agentkit.SessionEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return events, errTruncatedJSONLTail
			}
			return nil, err
		}
		events = append(events, ev)
	}
}

// repairSessionJSONLFile drops an incomplete trailing line so the next append
// does not leave a permanently broken file.
func repairSessionJSONLFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for len(data) > 0 {
		_, scanErr := scanJSONLDecoder(newJSONLDecoder(bytes.NewReader(data)))
		switch {
		case scanErr == nil:
			if len(data) == 0 {
				return os.WriteFile(path, nil, 0o644)
			}
			return os.WriteFile(path, data, 0o644)
		case errors.Is(scanErr, errTruncatedJSONLTail):
			last := bytes.LastIndexByte(data, '\n')
			if last < 0 {
				return os.WriteFile(path, nil, 0o644)
			}
			data = data[:last+1]
		default:
			return scanErr
		}
	}
	return os.WriteFile(path, nil, 0o644)
}

// ScanSessionFile loads one session JSONL file, folding compacted history the
// same way the store does. maxLoadedEvents <= 0 loads every retained event.
// Exported for the session index, which reads the same on-disk format.
func ScanSessionFile(path string, maxLoadedEvents int) ([]agentkit.SessionEvent, agentkit.EventSeq, bool, error) {
	events, err := loadSessionEventsFromFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	return FoldLoadedSessionEvents(events, maxLoadedEvents)
}

func loadSessionEventsFromFile(path string) ([]agentkit.SessionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	events, err := scanJSONLDecoder(newJSONLDecoder(f))
	if errors.Is(err, errTruncatedJSONLTail) {
		slog.Warn("session jsonl: truncated tail ignored",
			"path", path,
			"retained_events", len(events),
		)
		if repairErr := repairSessionJSONLFile(path); repairErr != nil {
			slog.Warn("session jsonl: repair failed", "path", path, "err", repairErr)
		}
		return events, nil
	}
	return events, err
}

// FoldLoadedSessionEvents applies the same compaction + tail window rules as
// session/store when loading durable history (JSONL file or SQL rows).
func FoldLoadedSessionEvents(all []agentkit.SessionEvent, maxLoadedEvents int) ([]agentkit.SessionEvent, agentkit.EventSeq, bool, error) {
	var (
		compactions       []agentkit.SessionEvent
		ring              eventRing
		maxSeq            agentkit.EventSeq
		nonCompactionSeen int
	)
	ring.max = maxLoadedEvents
	var latestContext *agentkit.SessionEvent
	for _, ev := range all {
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
		if ev.Type == agentkit.EventSessionContext {
			copy := ev
			latestContext = &copy
			continue
		}
		if ev.Type == agentkit.EventCompaction {
			compactions = append(compactions, ev)
			continue
		}
		nonCompactionSeen++
		ring.add(ev)
	}
	if latestContext != nil {
		compactions = append(compactions, *latestContext)
	}
	cutoffs := cutoffsFromCompactions(compactions)
	events := mergeLoadedEvents(compactions, ring.buf, cutoffs)
	trimmed := len(cutoffs) > 0 || (maxLoadedEvents > 0 && nonCompactionSeen > maxLoadedEvents)
	return events, maxSeq, trimmed, nil
}

func readSessionFile(path string, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	events, err := loadSessionEventsFromFile(path)
	if err != nil {
		return nil, err
	}
	if from == 0 {
		return events, nil
	}
	out := make([]agentkit.SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	return out, nil
}
