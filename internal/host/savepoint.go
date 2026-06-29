package host

import (
	"context"

	"github.com/aurora-capcompute/capcompute/dispatcher"
)

// Reserved capability names for savepoint markers. A guest brackets a unit of
// retryable work with CapTry … CapCommit. The markers carry no side effect: the
// savepointDispatcher short-circuits them to a deterministic Result so they are
// recorded on the journal (by the replay layer above) without ever reaching the
// task layer or a base capability. On a failed run, the runtime forks resume at
// the outermost CapTry that was never followed by a CapCommit.
const (
	CapTry    = "host.try"
	CapCommit = "host.commit"
)

// IsSavepoint reports whether name is one of the reserved savepoint markers.
func IsSavepoint(name string) bool {
	return name == CapTry || name == CapCommit
}

// savepointResult is the canonical, side-effect-free outcome recorded for every
// marker call. It is constant so replay matching stays deterministic.
var savepointResult = []byte("{}")

// savepointDispatcher intercepts the reserved CapTry/CapCommit markers and
// returns a fixed Result without invoking Next. Every other call passes through
// unchanged. It sits just below the replay tape (so markers are journaled) and
// above the task dispatcher (so markers never become durable tasks).
type savepointDispatcher[K any] struct {
	next dispatcher.Dispatcher[K]
}

func (d *savepointDispatcher[K]) Dispatch(ctx context.Context, key K, call dispatcher.Call, auth dispatcher.Authorization) (dispatcher.Outcome, error) {
	if IsSavepoint(call.Name) {
		return dispatcher.Result(savepointResult), nil
	}
	return d.next.Dispatch(ctx, key, call, auth)
}

func (d *savepointDispatcher[K]) Capabilities() []dispatcher.Capability {
	return d.next.Capabilities()
}
