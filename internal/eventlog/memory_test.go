package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAppendAssignsContiguousSeq(t *testing.T) {
	log := NewMemory()
	ctx := context.Background()
	scope := Scope{TenantID: "t", ThreadID: "th"}

	head, err := log.Append(ctx, scope, 0,
		Event{Kind: "run.created"},
		Event{Kind: "run.started"},
	)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if head != 2 {
		t.Fatalf("head = %d, want 2", head)
	}
	events, err := log.Read(ctx, scope, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("unexpected events %+v", events)
	}
}

func TestAppendRejectsStaleHead(t *testing.T) {
	log := NewMemory()
	ctx := context.Background()
	scope := Scope{TenantID: "t", ThreadID: "th"}

	if _, err := log.Append(ctx, scope, 0, Event{Kind: "a"}); err != nil {
		t.Fatal(err)
	}
	// A writer that still thinks the head is 0 must be rejected.
	head, err := log.Append(ctx, scope, 0, Event{Kind: "b"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if head != 1 {
		t.Fatalf("conflict head = %d, want current head 1", head)
	}
	// Appending at the real head succeeds.
	if _, err := log.Append(ctx, scope, 1, Event{Kind: "b"}); err != nil {
		t.Fatalf("append at head: %v", err)
	}
}

func TestReadAfterIsExclusive(t *testing.T) {
	log := NewMemory()
	ctx := context.Background()
	scope := Scope{TenantID: "t", ThreadID: "th"}
	_, _ = log.Append(ctx, scope, 0, Event{Kind: "a"}, Event{Kind: "b"}, Event{Kind: "c"})

	tail, err := log.Read(ctx, scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].Kind != "b" || tail[1].Kind != "c" {
		t.Fatalf("read after 1 = %+v", tail)
	}
	if rest, _ := log.Read(ctx, scope, 3); len(rest) != 0 {
		t.Fatalf("read past head returned %d events", len(rest))
	}
}

func TestStoredEventsAreIsolatedFromCallerMutation(t *testing.T) {
	log := NewMemory()
	ctx := context.Background()
	scope := Scope{TenantID: "t", ThreadID: "th"}
	data := json.RawMessage(`{"x":1}`)
	if _, err := log.Append(ctx, scope, 0, Event{Kind: "a", Data: data}); err != nil {
		t.Fatal(err)
	}
	data[0] = 'X' // mutate the caller's slice after appending

	got, _ := log.Read(ctx, scope, 0)
	if string(got[0].Data) != `{"x":1}` {
		t.Fatalf("stored event reflected caller mutation: %s", got[0].Data)
	}
	got[0].Data[0] = 'Y' // mutate the read copy
	again, _ := log.Read(ctx, scope, 0)
	if string(again[0].Data) != `{"x":1}` {
		t.Fatalf("read copy aliased stored event: %s", again[0].Data)
	}
}

func TestStreamsListsTenantThreads(t *testing.T) {
	log := NewMemory()
	ctx := context.Background()
	_, _ = log.Append(ctx, Scope{"t1", "b"}, 0, Event{Kind: "x"})
	_, _ = log.Append(ctx, Scope{"t1", "a"}, 0, Event{Kind: "x"})
	_, _ = log.Append(ctx, Scope{"t2", "c"}, 0, Event{Kind: "x"})

	streams, err := log.Streams(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 || streams[0].ThreadID != "a" || streams[1].ThreadID != "b" {
		t.Fatalf("streams = %+v, want sorted [a b] for t1", streams)
	}
}
