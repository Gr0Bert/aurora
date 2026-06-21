package host

import (
	"context"
	"errors"
	"time"

	"aurora-capcompute/internal/task"
	"capcompute/dispatcher"
	"capcompute/dispatcher/replay"
	"capcompute/dispatcher/replay/tape/journaled"
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
	return replay.NewDispatcher[K](journaled.NewTape(journal), withTasks), nil
}
