package sqlite_test

import (
	"aurora-capcompute/internal/agent"
	aurorasqlite "aurora-capcompute/internal/storage/sqlite"
	"capcompute/dispatcher"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsTenantScopedStateAndJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aurora.db")
	store, err := aurorasqlite.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	thread := agent.StoredThread{
		TenantID: "tenant-a", ID: "thread-1", Title: "Thread",
		CreatedAt: now, UpdatedAt: now,
		Manifest: agent.Manifest{Version: agent.ManifestVersion},
	}
	if err := store.SaveThread(context.Background(), thread); err != nil {
		t.Fatalf("save thread: %v", err)
	}
	run := agent.StoredRun{
		TenantID: "tenant-a", ID: "run-1", ThreadID: thread.ID, Revision: 1,
		Message: "hello", Status: agent.RunCompleted, Attempt: 1,
		CreatedAt: now, UpdatedAt: now, Answer: "world",
		EffectiveManifest: thread.Manifest,
	}
	if err := store.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	if err := store.AppendMessages(context.Background(), "tenant-a", thread.ID, []agent.HistoryMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}); err != nil {
		t.Fatalf("append messages: %v", err)
	}
	scope := agent.RunContext{TenantID: "tenant-a", ThreadID: thread.ID, RunID: run.ID, Revision: 1}
	journal, err := store.OpenJournal(context.Background(), scope)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if err := journal.Store(0, dispatcher.Call{Name: "tool", Args: json.RawMessage(`{"x":1}`)}, dispatcher.Failed("denied")); err != nil {
		t.Fatalf("store journal: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, err = aurorasqlite.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	state, err := store.Load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(state.Threads) != 1 || len(state.Runs) != 1 || len(state.Messages) != 2 {
		t.Fatalf("state = %+v", state)
	}
	other, err := store.Load(context.Background(), "tenant-b")
	if err != nil {
		t.Fatalf("load other tenant: %v", err)
	}
	if len(other.Threads)+len(other.Runs)+len(other.Messages) != 0 {
		t.Fatalf("other tenant leaked state: %+v", other)
	}
	journal, err = store.OpenJournal(context.Background(), scope)
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	record, err := journal.Load(0)
	if err != nil {
		t.Fatalf("load journal: %v", err)
	}
	if record.Outcome.Kind() != dispatcher.OutcomeFailed || record.Outcome.Message() != "denied" {
		t.Fatalf("outcome = %#v", record.Outcome)
	}
	otherJournal, err := store.OpenJournal(context.Background(), agent.RunContext{
		TenantID: "tenant-a", ThreadID: thread.ID, RunID: "run-2", Revision: 1,
	})
	if err != nil {
		t.Fatalf("open second journal: %v", err)
	}
	if otherJournal.Length() != 0 {
		t.Fatalf("second run leaked %d journal records", otherJournal.Length())
	}

	acquired, err := store.AcquireLease(context.Background(), "tenant-a", "run", "run-1/1", "one", now, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire first lease = %v, %v", acquired, err)
	}
	acquired, err = store.AcquireLease(context.Background(), "tenant-a", "run", "run-1/1", "two", now, time.Minute)
	if err != nil || acquired {
		t.Fatalf("acquire competing lease = %v, %v", acquired, err)
	}
	if err := store.ReleaseLease(context.Background(), "tenant-a", "run", "run-1/1", "one"); err != nil {
		t.Fatalf("release lease: %v", err)
	}
}
