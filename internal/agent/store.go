package agent

import (
	"bytes"
	"capcompute/dispatcher"
	"capcompute/dispatcher/replay/tape/journaled"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"aurora-capcompute/internal/task"
)

const DefaultTenantID = "local"

type RunContext struct {
	TenantID string `json:"tenant_id"`
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
	Revision uint64 `json:"revision"`
}

func (r RunContext) SessionKey() string {
	return fmt.Sprintf("%s/%s/%d", r.TenantID, r.RunID, r.Revision)
}

type StoredThread struct {
	TenantID    string
	ID          string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Manifest    Manifest
	ActiveRunID string
}

type StoredRun struct {
	TenantID          string
	ID                string
	ThreadID          string
	Revision          uint64
	Message           string
	Status            RunStatus
	Attempt           int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
	Answer            string
	Error             string
	EffectiveManifest Manifest
	BrainDigest       string
}

type StoredMessage struct {
	TenantID string
	ThreadID string
	Position int
	Role     string
	Content  string
}

type StoredState struct {
	Threads  []StoredThread
	Runs     []StoredRun
	Messages []StoredMessage
}

type Store interface {
	task.Store
	Load(context.Context, string) (StoredState, error)
	SaveThread(context.Context, StoredThread) error
	SaveRun(context.Context, StoredRun) error
	AppendMessages(context.Context, string, string, []HistoryMessage) error
	OpenJournal(context.Context, RunContext) (journaled.Journal, error)
	ResetJournal(context.Context, RunContext) error
	AcquireLease(context.Context, string, string, string, string, time.Time, time.Duration) (bool, error)
	ReleaseLease(context.Context, string, string, string, string) error
	Close() error
}

type MemoryStore struct {
	mu       sync.Mutex
	threads  map[string]StoredThread
	runs     map[string]StoredRun
	messages map[string][]StoredMessage
	journals map[string]*memoryJournal
	tasks    map[string]task.Record
	leases   map[string]memoryLease
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		threads:  make(map[string]StoredThread),
		runs:     make(map[string]StoredRun),
		messages: make(map[string][]StoredMessage),
		journals: make(map[string]*memoryJournal),
		tasks:    make(map[string]task.Record),
		leases:   make(map[string]memoryLease),
	}
}

func (s *MemoryStore) Load(_ context.Context, tenantID string) (StoredState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var state StoredState
	for _, thread := range s.threads {
		if thread.TenantID == tenantID {
			state.Threads = append(state.Threads, cloneStoredThread(thread))
		}
	}
	for _, run := range s.runs {
		if run.TenantID == tenantID {
			state.Runs = append(state.Runs, cloneStoredRun(run))
		}
	}
	for _, messages := range s.messages {
		for _, message := range messages {
			if message.TenantID == tenantID {
				state.Messages = append(state.Messages, message)
			}
		}
	}
	return state, nil
}

func (s *MemoryStore) SaveThread(_ context.Context, thread StoredThread) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[threadStoreKey(thread.TenantID, thread.ID)] = cloneStoredThread(thread)
	return nil
}

func (s *MemoryStore) SaveRun(_ context.Context, run StoredRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[runStoreKey(run.TenantID, run.ID)] = cloneStoredRun(run)
	return nil
}

func (s *MemoryStore) AppendMessages(_ context.Context, tenantID string, threadID string, messages []HistoryMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadStoreKey(tenantID, threadID)
	position := len(s.messages[key])
	for _, message := range messages {
		s.messages[key] = append(s.messages[key], StoredMessage{
			TenantID: tenantID,
			ThreadID: threadID,
			Position: position,
			Role:     message.Role,
			Content:  message.Content,
		})
		position++
	}
	return nil
}

func (s *MemoryStore) OpenJournal(_ context.Context, scope RunContext) (journaled.Journal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scope.SessionKey()
	journal := s.journals[key]
	if journal == nil {
		journal = &memoryJournal{}
		s.journals[key] = journal
	}
	return journal, nil
}

func (s *MemoryStore) ResetJournal(_ context.Context, scope RunContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journals[scope.SessionKey()] = &memoryJournal{}
	return nil
}

func (s *MemoryStore) Close() error {
	return nil
}

type memoryLease struct {
	holder    string
	expiresAt time.Time
}

func (s *MemoryStore) AcquireLease(_ context.Context, tenantID, kind, resourceID, holder string, now time.Time, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + "/" + kind + "/" + resourceID
	lease, exists := s.leases[key]
	if exists && lease.holder != holder && now.Before(lease.expiresAt) {
		return false, nil
	}
	s.leases[key] = memoryLease{holder: holder, expiresAt: now.Add(ttl)}
	return true, nil
}

func (s *MemoryStore) ReleaseLease(_ context.Context, tenantID, kind, resourceID, holder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + "/" + kind + "/" + resourceID
	if lease, ok := s.leases[key]; ok && lease.holder == holder {
		delete(s.leases, key)
	}
	return nil
}

func (s *MemoryStore) Find(_ context.Context, scope task.Scope, position int, callHash string) (task.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.tasks {
		if record.Scope == scope && record.JournalPosition == position && record.CallHash == callHash {
			return cloneTask(record), true, nil
		}
	}
	return task.Record{}, false, nil
}

func (s *MemoryStore) Create(_ context.Context, record task.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadStoreKey(record.Scope.TenantID, record.ID)
	if _, exists := s.tasks[key]; exists {
		return task.ErrConflict
	}
	s.tasks[key] = cloneTask(record)
	return nil
}

func (s *MemoryStore) Get(_ context.Context, tenantID, taskID string) (task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tasks[threadStoreKey(tenantID, taskID)]
	if !ok {
		return task.Record{}, task.ErrNotFound
	}
	return cloneTask(record), nil
}

func (s *MemoryStore) List(_ context.Context, tenantID, runID string) ([]task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []task.Record
	for _, record := range s.tasks {
		if record.Scope.TenantID == tenantID && (runID == "" || record.Scope.RunID == runID) {
			out = append(out, cloneTask(record))
		}
	}
	return out, nil
}

func (s *MemoryStore) Resolve(_ context.Context, tenantID, taskID string, tokenHash []byte, resolution task.Resolution, now time.Time) (task.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadStoreKey(tenantID, taskID)
	record, ok := s.tasks[key]
	if !ok {
		return task.Record{}, task.ErrNotFound
	}
	if !hmac.Equal(record.TokenHash, tokenHash) {
		return task.Record{}, task.ErrUnauthorized
	}
	if record.State != task.StatePending {
		if record.Resolution.Decision == resolution.Decision &&
			bytes.Equal(record.Resolution.Data, resolution.Data) &&
			record.Resolution.Reason == resolution.Reason {
			return cloneTask(record), nil
		}
		return task.Record{}, task.ErrConflict
	}
	if record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) {
		return task.Record{}, task.ErrGone
	}
	record.State = resolution.Decision
	record.Resolution = resolution
	record.ResolvedAt = &now
	s.tasks[key] = cloneTask(record)
	return cloneTask(record), nil
}

func (s *MemoryStore) MarkExecuted(_ context.Context, tenantID, taskID string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := threadStoreKey(tenantID, taskID)
	record, ok := s.tasks[key]
	if !ok {
		return task.ErrNotFound
	}
	record.State = task.StateExecuted
	s.tasks[key] = cloneTask(record)
	return nil
}

type memoryJournal struct {
	mu      sync.Mutex
	records []journaled.Record
}

func (j *memoryJournal) Load(index int) (journaled.Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if index < 0 || index >= len(j.records) {
		return journaled.Record{}, errors.New("journal record not found")
	}
	record := j.records[index]
	return journaled.Record{Call: record.Call.Copy(), Outcome: record.Outcome.Copy()}, nil
}

func (j *memoryJournal) Store(index int, call dispatcher.Call, outcome dispatcher.Outcome) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if index != len(j.records) {
		return errors.New("invalid journal index")
	}
	j.records = append(j.records, journaled.Record{Call: call.Copy(), Outcome: outcome.Copy()})
	return nil
}

func (j *memoryJournal) Length() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.records)
}

func threadStoreKey(tenantID, threadID string) string {
	return tenantID + "/" + threadID
}

func runStoreKey(tenantID, runID string) string {
	return tenantID + "/" + runID
}

func cloneStoredThread(thread StoredThread) StoredThread {
	thread.Manifest = cloneManifest(thread.Manifest)
	return thread
}

func cloneStoredRun(run StoredRun) StoredRun {
	run.EffectiveManifest = cloneManifest(run.EffectiveManifest)
	run.StartedAt = copyTime(run.StartedAt)
	run.CompletedAt = copyTime(run.CompletedAt)
	return run
}

func cloneTask(record task.Record) task.Record {
	record.Call = record.Call.Copy()
	record.TokenHash = append([]byte(nil), record.TokenHash...)
	record.Resolution.Data = append(json.RawMessage(nil), record.Resolution.Data...)
	record.ExpiresAt = copyTime(record.ExpiresAt)
	record.ResolvedAt = copyTime(record.ResolvedAt)
	return record
}
