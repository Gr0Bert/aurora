// Package host owns the per-run dispatcher stack: it takes the caller-supplied
// base dispatcher and wraps it, for one run, with durable task approval and the
// replay/journal middleware. The runtime hands every brain call to a dispatcher
// built here, so a yielded capability becomes a durable task and every recorded
// outcome lands in that run's journal for deterministic replay.
//
// It owns only the wiring of that stack; the task store, journal, and base
// dispatcher are injected.
package host

import (
	"context"
	"errors"
	"time"

	"github.com/aurora-capcompute/aurora-capcompute/internal/task"
	"github.com/aurora-capcompute/capcompute/dispatcher"
	"github.com/aurora-capcompute/capcompute/dispatcher/replay"
	"github.com/aurora-capcompute/capcompute/dispatcher/replay/tape/journaled"
)

type Factory[K any] struct {
	Base          func(context.Context, K) (dispatcher.Dispatcher[K], error)
	NewJournal    func(context.Context, K) (journaled.Journal, error)
	Tasks         task.Store
	TaskScope     func(K) task.Scope
	TaskSecret    []byte
	TaskTTL       time.Duration
	OnTaskCreated func(task.Record)
}

func (f Factory[K]) NewDispatcher(ctx context.Context, key K) (dispatcher.Dispatcher[K], error) {
	if f.Base == nil || f.NewJournal == nil || f.Tasks == nil || f.TaskScope == nil || len(f.TaskSecret) == 0 {
		return nil, errors.New("dispatcher factory is not configured")
	}
	configured, err := f.Base(ctx, key)
	if err != nil {
		return nil, err
	}
	if configured == nil {
		return nil, errors.New("dispatcher provider returned nil dispatcher")
	}
	journal, err := f.NewJournal(ctx, key)
	if err != nil {
		return nil, err
	}
	withTasks := &task.Dispatcher[K]{
		Next:          configured,
		Store:         f.Tasks,
		Journal:       journal,
		Scope:         f.TaskScope,
		TokenSecret:   append([]byte(nil), f.TaskSecret...),
		TaskTTL:       f.TaskTTL,
		OnTaskCreated: f.OnTaskCreated,
	}
	// Savepoint markers sit below replay (so they are journaled) and above the
	// task layer (so they never become durable tasks or hit a base capability).
	withSavepoints := &savepointDispatcher[K]{next: withTasks}
	return replay.NewDispatcher[K](journaled.NewTape(journal), withSavepoints), nil
}
