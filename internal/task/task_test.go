package task_test

import (
	"aurora-capcompute/internal/agent"
	"aurora-capcompute/internal/task"
	"capcompute/dispatcher"
	"capcompute/dispatcher/replay"
	"capcompute/dispatcher/replay/tape/journaled"
	"capcompute/dispatcher/replay/tape/journaled/journal/memory"
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"
)

type run struct{}

type approvalDispatcher struct {
	executions int
}

func (d *approvalDispatcher) Dispatch(ctx context.Context, _ run, _ dispatcher.Call) (dispatcher.Outcome, error) {
	resolution, ok := task.ResolutionFromContext(ctx)
	if !ok {
		return dispatcher.Yield("approve test operation"), nil
	}
	if resolution.Decision != task.StateApproved {
		return dispatcher.Failed("not approved"), nil
	}
	d.executions++
	return dispatcher.Result(json.RawMessage(`{"ok":true}`)), nil
}

func TestDispatcherPersistsAndResumesYieldedTask(t *testing.T) {
	store := agent.NewMemoryStore()
	journal := memory.NewJournal()
	next := &approvalDispatcher{}
	scope := task.Scope{TenantID: "tenant", ThreadID: "thread", RunID: "run", Revision: 1}
	secret := []byte("test-secret")
	build := func() dispatcher.Dispatcher[run] {
		taskDispatcher := &task.Dispatcher[run]{
			Next:        next,
			Store:       store,
			Journal:     journal,
			Scope:       func(run) task.Scope { return scope },
			TokenSecret: secret,
			TaskTTL:     time.Hour,
		}
		return replay.NewDispatcher[run](journaled.NewTape(journal), taskDispatcher)
	}
	call := dispatcher.Call{Name: "internet.read", Args: json.RawMessage(`{"url":"https://example.com"}`)}

	outcome, err := build().Dispatch(context.Background(), run{}, call)
	if err != nil {
		t.Fatalf("initial dispatch: %v", err)
	}
	if outcome.Kind() != dispatcher.OutcomeYield {
		t.Fatalf("initial outcome = %s", outcome.Kind())
	}
	records, err := store.List(context.Background(), scope.TenantID, scope.RunID)
	if err != nil || len(records) != 1 {
		t.Fatalf("tasks = %+v, err=%v", records, err)
	}

	token := task.Token(secret, scope.TenantID, records[0].ID)
	sum := sha256.Sum256([]byte(token))
	if _, err := store.Resolve(context.Background(), scope.TenantID, records[0].ID, sum[:], task.Resolution{
		Decision: task.StateApproved,
		Actor:    "tester",
	}, time.Now().UTC()); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	outcome, err = build().Dispatch(context.Background(), run{}, call)
	if err != nil {
		t.Fatalf("resumed dispatch: %v", err)
	}
	if outcome.Kind() != dispatcher.OutcomeResult || next.executions != 1 {
		t.Fatalf("resumed outcome = %s, executions=%d", outcome.Kind(), next.executions)
	}

	outcome, err = build().Dispatch(context.Background(), run{}, call)
	if err != nil {
		t.Fatalf("replayed dispatch: %v", err)
	}
	if outcome.Kind() != dispatcher.OutcomeResult || next.executions != 1 {
		t.Fatalf("replayed outcome = %s, executions=%d", outcome.Kind(), next.executions)
	}
}
