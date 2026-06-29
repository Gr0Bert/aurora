package host

import (
	"context"
	"testing"

	"github.com/aurora-capcompute/capcompute/dispatcher"
)

// recordingDispatcher records the calls it receives so a test can assert which
// calls were passed through versus short-circuited by the savepoint layer.
type recordingDispatcher struct {
	seen []string
}

func (d *recordingDispatcher) Dispatch(_ context.Context, _ struct{}, call dispatcher.Call, _ dispatcher.Authorization) (dispatcher.Outcome, error) {
	d.seen = append(d.seen, call.Name)
	return dispatcher.Result([]byte(`"passed"`)), nil
}

func (d *recordingDispatcher) Capabilities() []dispatcher.Capability {
	return []dispatcher.Capability{{Name: "base.cap"}}
}

func TestSavepointDispatcherInterceptsMarkers(t *testing.T) {
	next := &recordingDispatcher{}
	d := &savepointDispatcher[struct{}]{next: next}

	for _, name := range []string{CapTry, CapCommit} {
		out, err := d.Dispatch(context.Background(), struct{}{}, dispatcher.Call{Name: name}, dispatcher.Authorization{})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if out.Kind() != dispatcher.OutcomeResult {
			t.Fatalf("%s: kind = %s, want result", name, out.Kind())
		}
	}
	if len(next.seen) != 0 {
		t.Fatalf("markers reached next dispatcher: %v", next.seen)
	}
}

func TestSavepointDispatcherPassesThroughOtherCalls(t *testing.T) {
	next := &recordingDispatcher{}
	d := &savepointDispatcher[struct{}]{next: next}

	out, err := d.Dispatch(context.Background(), struct{}{}, dispatcher.Call{Name: "k8s.apply"}, dispatcher.Authorization{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out.Result()) != `"passed"` {
		t.Fatalf("result = %s, want passthrough", out.Result())
	}
	if len(next.seen) != 1 || next.seen[0] != "k8s.apply" {
		t.Fatalf("passthrough calls = %v, want [k8s.apply]", next.seen)
	}
}

func TestSavepointDispatcherDelegatesCapabilities(t *testing.T) {
	d := &savepointDispatcher[struct{}]{next: &recordingDispatcher{}}
	caps := d.Capabilities()
	if len(caps) != 1 || caps[0].Name != "base.cap" {
		t.Fatalf("capabilities = %v, want [base.cap]", caps)
	}
}
