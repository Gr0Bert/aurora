package agent

import (
	"testing"
	"time"

	"github.com/aurora-capcompute/aurora-capcompute/internal/eventlog"
	internalhost "github.com/aurora-capcompute/aurora-capcompute/internal/host"

	"github.com/aurora-capcompute/capcompute/dispatcher"
)

// buildJournal stores a sequence of calls and returns the journal. Each step is
// "try"/"commit" (savepoint markers) or "name:fail"/"name" (a failing or
// successful capability call).
func buildJournal(t *testing.T, steps ...step) *logJournal {
	t.Helper()
	log := eventlog.NewMemory()
	scope := eventlog.Scope{TenantID: "t", ThreadID: "th"}
	now := func() time.Time { return time.Unix(0, 0).UTC() }
	j := newLogJournal(log, scope, "run1", 1, newRunHistory(), 0, now, nil)
	for i, s := range steps {
		if err := j.Store(i, dispatcher.Call{Name: s.name}, s.outcome); err != nil {
			t.Fatalf("store %d (%s): %v", i, s.name, err)
		}
	}
	return j
}

type step struct {
	name    string
	outcome dispatcher.Outcome
}

func try() step    { return step{internalhost.CapTry, dispatcher.Result([]byte("{}"))} }
func commit() step { return step{internalhost.CapCommit, dispatcher.Result([]byte("{}"))} }
func ok(name string) step {
	return step{name, dispatcher.Result([]byte("{}"))}
}
func fail(name string) step { return step{name, dispatcher.Fail(name + " failed")} }

func TestOutermostOpenTry(t *testing.T) {
	cases := []struct {
		name     string
		steps    []step
		wantOff  int
		wantOpen bool
	}{
		{
			name:     "no try at all",
			steps:    []step{ok("a"), fail("b")},
			wantOpen: false,
		},
		{
			name:     "single open try",
			steps:    []step{try(), fail("x")},
			wantOff:  1, // fork right after the try marker at position 0
			wantOpen: true,
		},
		{
			name:     "committed try then bare soft fail",
			steps:    []step{try(), ok("a"), commit(), fail("y")},
			wantOpen: false, // the bare fail synthesizes no fork point
		},
		{
			name:     "sequential committed then open",
			steps:    []step{try(), ok("a"), commit(), try(), fail("b")},
			wantOff:  4, // fork after the second try at position 3
			wantOpen: true,
		},
		{
			name:     "nested both open forks at outermost",
			steps:    []step{try(), ok("a"), try(), fail("b")},
			wantOff:  1, // outermost try at position 0
			wantOpen: true,
		},
		{
			name:     "nested inner committed outer open",
			steps:    []step{try(), ok("a"), try(), ok("b"), commit(), fail("c")},
			wantOff:  1, // outer try still open
			wantOpen: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := buildJournal(t, tc.steps...)
			off, open := j.outermostOpenTry()
			if open != tc.wantOpen {
				t.Fatalf("open = %v, want %v", open, tc.wantOpen)
			}
			if open && off != tc.wantOff {
				t.Fatalf("forkOffset = %d, want %d", off, tc.wantOff)
			}
		})
	}
}
