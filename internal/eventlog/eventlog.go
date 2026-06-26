// Package eventlog is a generic append-only log: the single source of truth a
// runtime folds into projections. It is domain-agnostic — events carry an opaque
// payload owned by the layer above — so the same primitive backs lifecycle,
// task, and capability-journal events for one thread stream.
//
// A stream is one thread's ordered history, identified by Scope. Appends are
// optimistic: a caller passes the head sequence it last observed, and the append
// is rejected with ErrConflict if another writer advanced the stream first. State
// is reconstructed by reading a stream from the beginning and folding its events;
// there is no in-place mutation and no separate "current row" store.
package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrConflict reports that a stream's head advanced since the caller's
// expectedHead, so the optimistic append was rejected.
var ErrConflict = errors.New("eventlog: concurrent append conflict")

// Scope identifies one append-only stream — one thread's history.
type Scope struct {
	TenantID string
	ThreadID string
}

// Event is one immutable record in a stream. Seq is assigned by the log on
// append (1-based, contiguous per stream). Kind and Data are owned by the domain
// layer; Run and Rev locate the event within a run's revision when applicable
// (zero for thread-level events).
type Event struct {
	Seq  uint64          `json:"seq"`
	Kind string          `json:"kind"`
	Time time.Time       `json:"time"`
	Run  string          `json:"run,omitempty"`
	Rev  uint64          `json:"rev,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Log is the append-only event store. Implementations must make Append atomic
// and totally ordered per stream.
type Log interface {
	// Append atomically writes events to a stream after asserting its current head
	// equals expectedHead (0 for a new stream). On success it assigns each event a
	// contiguous Seq and returns the new head. On a head mismatch it returns
	// ErrConflict and writes nothing.
	Append(ctx context.Context, scope Scope, expectedHead uint64, events ...Event) (head uint64, err error)
	// Read returns the stream's events with Seq > after, in order. after == 0
	// reads from the beginning.
	Read(ctx context.Context, scope Scope, after uint64) ([]Event, error)
	// Streams lists the scopes that have at least one event for a tenant, so a
	// runtime can enumerate threads to fold on restore.
	Streams(ctx context.Context, tenantID string) ([]Scope, error)
}
