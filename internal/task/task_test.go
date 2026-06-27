package task_test

import (
	"github.com/aurora-capcompute/aurora-capcompute/internal/task"
	"github.com/aurora-capcompute/capcompute/dispatcher"
	"github.com/aurora-capcompute/capcompute/dispatcher/replay"
	"github.com/aurora-capcompute/capcompute/dispatcher/replay/tape/journaled"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type run struct{}

type taskStore struct {
	mu      sync.Mutex
	records map[string]task.Record
}

func newTaskStore() *taskStore {
	return &taskStore{records: make(map[string]task.Record)}
}

func (s *taskStore) Find(_ context.Context, scope task.Scope, position int, hash string) (task.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.Scope == scope && record.JournalPosition == position && record.CallHash == hash {
			return record, true, nil
		}
	}
	return task.Record{}, false, nil
}

func (s *taskStore) Create(_ context.Context, record task.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.ID]; ok {
		return task.ErrConflict
	}
	s.records[record.ID] = record
	return nil
}

func (s *taskStore) Get(_ context.Context, _ string, id string) (task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return task.Record{}, task.ErrNotFound
	}
	return record, nil
}

func (s *taskStore) List(_ context.Context, _ string, runID string) ([]task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []task.Record
	for _, record := range s.records {
		if runID == "" || record.Scope.RunID == runID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *taskStore) Resolve(_ context.Context, _ string, id string, tokenHash []byte, resolution task.Resolution, now time.Time) (task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return task.Record{}, task.ErrNotFound
	}
	if !task.VerifyToken(tokenHash, task.Token([]byte("test-secret"), record.Scope.TenantID, record.ID)) {
		return task.Record{}, task.ErrUnauthorized
	}
	record.State = resolution.Decision
	record.Resolution = resolution
	record.ResolvedAt = &now
	s.records[id] = record
	return record, nil
}

func (s *taskStore) MarkExecuted(_ context.Context, _ string, id string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return task.ErrNotFound
	}
	record.State = task.StateExecuted
	s.records[id] = record
	return nil
}

type journal struct {
	records []journaled.Record
}

func (j *journal) Load(index int) (journaled.Record, error) {
	if index < 0 || index >= len(j.records) {
		return journaled.Record{}, errors.New("record not found")
	}
	return j.records[index], nil
}

func (j *journal) Store(index int, call dispatcher.Call, outcome dispatcher.Outcome) error {
	if index != len(j.records) {
		return errors.New("invalid index")
	}
	j.records = append(j.records, journaled.Record{Call: call.Copy(), Outcome: outcome.Copy()})
	return nil
}

func (j *journal) Length() int { return len(j.records) }

type approvalDispatcher struct {
	executions int
}

func (*approvalDispatcher) Capabilities() []dispatcher.Capability { return nil }

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
	store := newTaskStore()
	journal := &journal{}
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
