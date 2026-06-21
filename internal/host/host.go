package host

import (
	"context"

	"aurora-capcompute/internal/task"
	"aurora-dispatchers/builtin"
	"aurora-dispatchers/llm"
	"aurora-dispatchers/mcp"
	"capcompute/dispatcher"
	"capcompute/dispatcher/replay"
	"capcompute/dispatcher/replay/tape/journaled"
	"time"
)

type InternetReader = builtin.InternetReader
type Config = builtin.Config
type Dispatcher[K any] = builtin.Dispatcher[K]

type Factory[K any] struct {
	LLM                     llm.Client
	Internet                InternetReader
	InternetRequireApproval bool
	MCP                     []*mcp.Handler
	Capabilities            []dispatcher.Capability
	Resolve                 ConfigResolver[K]
	NewTape                 TapeFactory[K]
	NewJournal              JournalFactory[K]
	Tasks                   task.Store
	TaskScope               func(K) task.Scope
	TaskSecret              []byte
	TaskTTL                 time.Duration
	OnTaskCreated           func(task.Record)
}

type TapeFactory[K any] func(ctx context.Context, key K) (replay.Tape, error)
type JournalFactory[K any] func(ctx context.Context, key K) (journaled.Journal, error)
type ConfigResolver[K any] func(ctx context.Context, key K) (Config, error)

func (f Factory[K]) NewDispatcher(ctx context.Context, key K) (dispatcher.Dispatcher[K], error) {
	config := Config{
		LLM:                     f.LLM,
		Internet:                f.Internet,
		InternetRequireApproval: f.InternetRequireApproval,
		MCP:                     f.MCP,
		Capabilities:            f.Capabilities,
	}
	if f.Resolve != nil {
		var err error
		config, err = f.Resolve(ctx, key)
		if err != nil {
			return nil, err
		}
	}
	configured := builtin.New[K](config)
	if f.NewJournal != nil {
		journal, err := f.NewJournal(ctx, key)
		if err != nil {
			return nil, err
		}
		if f.Tasks != nil {
			configured = &task.Dispatcher[K]{
				Next:          configured,
				Store:         f.Tasks,
				Journal:       journal,
				Scope:         f.TaskScope,
				TokenSecret:   append([]byte(nil), f.TaskSecret...),
				TaskTTL:       f.TaskTTL,
				OnTaskCreated: f.OnTaskCreated,
			}
		}
		return replay.NewDispatcher[K](journaled.NewTape(journal), configured), nil
	}
	if f.NewTape == nil {
		return configured, nil
	}
	tape, err := f.NewTape(ctx, key)
	if err != nil {
		return nil, err
	}
	return replay.NewDispatcher[K](tape, configured), nil
}
