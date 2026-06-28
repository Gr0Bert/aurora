package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aurora-capcompute/aurora-capcompute/internal/eventlog"

	"github.com/aurora-capcompute/capcompute/dispatcher"
	"github.com/aurora-capcompute/capcompute/dispatcher/replay/tape/journaled"
)

// Capability-journal events. Each recorded call is a capability.recorded event
// carrying its absolute position and the revision that produced it. The fork
// structure (which revision shared which prefix) is fully derivable from the
// flat set of (position, revision) pairs — no separate fork event is needed.
const evCapability = "capability.recorded"

type capabilityData struct {
	Position int             `json:"position"`
	Revision uint64          `json:"revision"` // mirrors ev.Rev for self-documentation
	Call     dispatcher.Call `json:"call"`
	Outcome  JournalOutcome  `json:"outcome"`
}

func encodeOutcome(o dispatcher.Outcome) JournalOutcome {
	return JournalOutcome{Status: o.Kind(), Result: o.Result(), Message: o.Message()}
}

func decodeOutcome(jo JournalOutcome) dispatcher.Outcome {
	switch jo.Status {
	case dispatcher.OutcomeResult:
		return dispatcher.Result(jo.Result)
	case dispatcher.OutcomeYield:
		return dispatcher.Yield(jo.Message)
	default:
		return dispatcher.Fail(jo.Message)
	}
}

// runHistory accumulates every (position, revision, record) triple written for
// a run. All logJournal instances for the same run share one runHistory so a
// forked revision can serve its shared prefix without a parent-pointer chain.
type runHistory struct {
	mu    sync.Mutex
	byPos map[int][]histEntry
}

type histEntry struct {
	revision uint64
	record   journaled.Record
}

func newRunHistory() *runHistory {
	return &runHistory{byPos: make(map[int][]histEntry)}
}

func (h *runHistory) add(position int, revision uint64, rec journaled.Record) {
	h.mu.Lock()
	h.byPos[position] = append(h.byPos[position], histEntry{revision: revision, record: rec})
	h.mu.Unlock()
}

// getAt returns a copy of the entry at position with the highest revision ≤ maxRev.
func (h *runHistory) getAt(position int, maxRev uint64) (journaled.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *histEntry
	for i := range h.byPos[position] {
		e := &h.byPos[position][i]
		if e.revision <= maxRev && (best == nil || e.revision > best.revision) {
			best = e
		}
	}
	if best == nil {
		return journaled.Record{}, false
	}
	return journaled.Record{Call: best.record.Call.Copy(), Outcome: best.record.Outcome.Copy()}, true
}

// revAt returns the revision number of the entry at position with the highest
// revision ≤ maxRev. Used to annotate shared-prefix entries with their origin revision.
func (h *runHistory) revAt(position int, maxRev uint64) (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best uint64
	found := false
	for _, e := range h.byPos[position] {
		if e.revision <= maxRev && (!found || e.revision > best) {
			best = e.revision
			found = true
		}
	}
	return best, found
}

// logJournal implements journaled.Journal over an event stream. Positions
// [0, forkOffset) are served from the shared runHistory (written by prior
// revisions); positions [forkOffset, ...) are from this revision's own records.
type logJournal struct {
	log      eventlog.Log
	scope    eventlog.Scope
	run      string
	rev      uint64
	now      func() time.Time
	onAppend func(run string, position int, revision uint64, call dispatcher.Call, outcome dispatcher.Outcome)

	history    *runHistory
	forkOffset int // positions [0, forkOffset) come from history

	mu      sync.Mutex
	records []journaled.Record // entries appended during this revision
}

func newLogJournal(
	log eventlog.Log,
	scope eventlog.Scope,
	run string,
	rev uint64,
	history *runHistory,
	forkOffset int,
	now func() time.Time,
	onAppend func(string, int, uint64, dispatcher.Call, dispatcher.Outcome),
) *logJournal {
	return &logJournal{
		log: log, scope: scope, run: run, rev: rev,
		history: history, forkOffset: forkOffset,
		now: now, onAppend: onAppend,
	}
}

func (j *logJournal) Length() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.forkOffset + len(j.records)
}

func (j *logJournal) Load(index int) (journaled.Record, error) {
	if index < j.forkOffset {
		// Shared prefix: served from history. By contract this revision never
		// writes to positions < forkOffset, so j.rev is a safe upper bound.
		rec, ok := j.history.getAt(index, j.rev)
		if !ok {
			return journaled.Record{}, fmt.Errorf("journal record not found at index %d", index)
		}
		return rec, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	local := index - j.forkOffset
	if local < 0 || local >= len(j.records) {
		return journaled.Record{}, errors.New("journal record not found")
	}
	r := j.records[local]
	return journaled.Record{Call: r.Call.Copy(), Outcome: r.Outcome.Copy()}, nil
}

func (j *logJournal) Store(index int, call dispatcher.Call, outcome dispatcher.Outcome) error {
	j.mu.Lock()
	if index != j.forkOffset+len(j.records) {
		j.mu.Unlock()
		return errors.New("invalid journal index")
	}
	ev, err := encodeEvent(evCapability, j.run, j.rev, j.now(), capabilityData{
		Position: index,
		Revision: j.rev,
		Call:     call.Copy(),
		Outcome:  encodeOutcome(outcome),
	})
	if err != nil {
		j.mu.Unlock()
		return err
	}
	if _, err := j.log.Append(context.Background(), j.scope, ev); err != nil {
		j.mu.Unlock()
		return err
	}
	rec := journaled.Record{Call: call.Copy(), Outcome: outcome.Copy()}
	j.records = append(j.records, rec)
	j.history.add(index, j.rev, rec) // update shared history while still holding j.mu
	j.mu.Unlock()
	if j.onAppend != nil {
		j.onAppend(j.run, index, j.rev, call, outcome)
	}
	return nil
}

// foldJournals rebuilds every revision's journal for a thread stream from its
// capability.recorded events. Revisions are linked to a shared runHistory so
// forked journals can serve the shared prefix without parent-pointer chains.
// It returns both the journals and the per-run histories (so callers that need
// to create new revisions for an existing run can share the same history).
func foldJournals(
	events []eventlog.Event,
	log eventlog.Log,
	scope eventlog.Scope,
	now func() time.Time,
	onAppend func(string, int, uint64, dispatcher.Call, dispatcher.Outcome),
) (map[string]map[uint64]*logJournal, map[string]*runHistory, error) {
	histories := map[string]*runHistory{}
	type posEntry struct {
		position int
		record   journaled.Record
	}
	revData := map[string]map[uint64][]posEntry{} // run → rev → entries (in log order)

	for _, ev := range events {
		if ev.Kind != evCapability {
			continue
		}
		var cd capabilityData
		if err := json.Unmarshal(ev.Data, &cd); err != nil {
			return nil, nil, fmt.Errorf("decode capability.recorded: %w", err)
		}
		rev := ev.Rev // authoritative; cd.Revision is the same on new entries
		rec := journaled.Record{Call: cd.Call, Outcome: decodeOutcome(cd.Outcome)}

		if histories[ev.Run] == nil {
			histories[ev.Run] = newRunHistory()
		}
		histories[ev.Run].add(cd.Position, rev, rec)

		if revData[ev.Run] == nil {
			revData[ev.Run] = map[uint64][]posEntry{}
		}
		revData[ev.Run][rev] = append(revData[ev.Run][rev], posEntry{cd.Position, rec})
	}

	result := map[string]map[uint64]*logJournal{}
	for run, history := range histories {
		result[run] = map[uint64]*logJournal{}
		for rev, entries := range revData[run] {
			// Sort by position so forkOffset = entries[0].position is reliable.
			sort.Slice(entries, func(i, k int) bool { return entries[i].position < entries[k].position })
			forkOffset := 0
			if len(entries) > 0 {
				forkOffset = entries[0].position
			}
			j := newLogJournal(log, scope, run, rev, history, forkOffset, now, onAppend)
			for _, e := range entries {
				j.records = append(j.records, e.record)
			}
			result[run][rev] = j
		}
	}
	return result, histories, nil
}
