package agent

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/aurora-capcompute/aurora-capcompute/internal/eventlog"

	"github.com/aurora-capcompute/capcompute/dispatcher"
)

// rec builds a capability call whose outcome is a JSON-string result (capability
// outcomes are always valid JSON).
func rec(name, result string) (dispatcher.Call, dispatcher.Outcome) {
	return dispatcher.Call{Name: name}, dispatcher.Result([]byte(strconv.Quote(result)))
}

// loadAll reads every record from a journal as `name="result"` strings.
func loadAll(t *testing.T, j *logJournal) []string {
	t.Helper()
	var out []string
	for i := 0; i < j.Length(); i++ {
		r, err := j.Load(i)
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		out = append(out, r.Call.Name+"="+string(r.Outcome.Result()))
	}
	return out
}

func TestLogJournalLinearRoundTrip(t *testing.T) {
	log := eventlog.NewMemory()
	scope := eventlog.Scope{TenantID: "t", ThreadID: "th"}
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	j := newLogJournal(log, scope, "run1", 1, newRunHistory(), 0, now, nil)

	for i, n := range []string{"a", "b", "c"} {
		call, outcome := rec(n, n)
		if err := j.Store(i, call, outcome); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}
	if got := loadAll(t, j); len(got) != 3 || got[2] != "c=\"c\"" {
		t.Fatalf("live journal = %v", got)
	}

	// Rebuild purely from the event stream and confirm identical records.
	events, _ := log.Read(context.Background(), scope, 0)
	journals, _, err := foldJournals(events, log, scope, now, nil)
	if err != nil {
		t.Fatalf("fold journals: %v", err)
	}
	rebuilt := journals["run1"][1]
	if got := loadAll(t, rebuilt); len(got) != 3 || got[0] != `a="a"` || got[2] != `c="c"` {
		t.Fatalf("rebuilt journal = %v", got)
	}
}

func TestLogJournalForkSharesPrefixThenDiverges(t *testing.T) {
	log := eventlog.NewMemory()
	scope := eventlog.Scope{TenantID: "t", ThreadID: "th"}
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	history := newRunHistory()

	base := newLogJournal(log, scope, "run1", 1, history, 0, now, nil)
	for i, n := range []string{"a", "b", "c"} {
		call, outcome := rec(n, n)
		if err := base.Store(i, call, outcome); err != nil {
			t.Fatal(err)
		}
	}
	// Create rev 2 sharing the first two records [a, b] (forkOffset=2),
	// then append a different third record.
	child := newLogJournal(log, scope, "run1", 2, history, 2, now, nil)
	if child.Length() != 2 {
		t.Fatalf("forked length = %d, want 2 (shared prefix)", child.Length())
	}
	call, outcome := rec("c2", "c2")
	if err := child.Store(2, call, outcome); err != nil {
		t.Fatalf("store on fork: %v", err)
	}
	if got := loadAll(t, child); len(got) != 3 || got[0] != `a="a"` || got[1] != `b="b"` || got[2] != `c2="c2"` {
		t.Fatalf("forked journal = %v", got)
	}
	// The base revision is untouched.
	if got := loadAll(t, base); got[2] != `c="c"` {
		t.Fatalf("parent mutated: %v", got)
	}

	// Rebuild both revisions from the stream.
	events, _ := log.Read(context.Background(), scope, 0)
	journals, _, err := foldJournals(events, log, scope, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := loadAll(t, journals["run1"][1]); got[2] != `c="c"` {
		t.Fatalf("rebuilt rev1 = %v", got)
	}
	if got := loadAll(t, journals["run1"][2]); len(got) != 3 || got[2] != `c2="c2"` {
		t.Fatalf("rebuilt rev2 = %v", got)
	}
}
