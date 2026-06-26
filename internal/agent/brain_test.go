package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

type staticBrains struct {
	defaultID string
	sources   []BrainSource
}

func (b staticBrains) DefaultID() string { return b.defaultID }
func (b staticBrains) List(context.Context) ([]BrainSource, error) {
	return b.sources, nil
}

func TestLoadBrainsCopiesBytesAndPinsDigest(t *testing.T) {
	raw := []byte("wasm")
	brains, err := loadBrains(context.Background(), staticBrains{
		defaultID: "brain@1",
		sources:   []BrainSource{{ID: "brain@1", Wasm: raw}},
	})
	if err != nil {
		t.Fatalf("load brains: %v", err)
	}
	raw[0] = 'X'
	source, err := brains.Source("brain@1")
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	if string(source.Wasm) != "wasm" {
		t.Fatalf("source bytes changed: %q", source.Wasm)
	}
	sum := sha256.Sum256([]byte("wasm"))
	artifact, err := brains.Resolve("brain@1")
	if err != nil {
		t.Fatalf("resolve artifact: %v", err)
	}
	if artifact.Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %q", artifact.Digest)
	}
}

func TestLoadBrainsRejectsInvalidProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider BrainProvider
	}{
		// A provider that lists at least one brain must still name a valid default
		// and supply well-formed, unique sources.
		{name: "missing default", provider: staticBrains{sources: []BrainSource{{ID: "brain@1", Wasm: []byte("wasm")}}}},
		{name: "empty wasm", provider: staticBrains{defaultID: "brain@1", sources: []BrainSource{{ID: "brain@1"}}}},
		{name: "duplicate", provider: staticBrains{defaultID: "brain@1", sources: []BrainSource{
			{ID: "brain@1", Wasm: []byte("one")},
			{ID: "brain@1", Wasm: []byte("two")},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadBrains(context.Background(), test.provider); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// TestLoadedBrainsMutation covers the registry bookkeeping SetBrains relies on
// (put/remove/digest diffing and default recomputation), independent of wasm
// compilation.
func TestLoadedBrainsMutation(t *testing.T) {
	brains, err := loadBrains(context.Background(), nil)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if d := brains.digests(); len(d) != 0 {
		t.Fatalf("empty digests = %v", d)
	}

	// First brain becomes the default.
	brains.put("b", []byte("two"), digestOf([]byte("two")))
	if brains.DefaultID() != "b" {
		t.Fatalf("default = %q, want b", brains.DefaultID())
	}
	// A lexicographically smaller id does not displace an existing valid default.
	brains.put("a", []byte("one"), digestOf([]byte("one")))
	if brains.DefaultID() != "b" {
		t.Fatalf("default changed to %q, want sticky b", brains.DefaultID())
	}
	if d := brains.digests(); d["a"] != digestOf([]byte("one")) || d["b"] != digestOf([]byte("two")) {
		t.Fatalf("digests = %v", d)
	}

	// Removing the default falls back to the lexicographically first remaining id.
	brains.remove("b")
	if brains.DefaultID() != "a" {
		t.Fatalf("default after removing b = %q, want a", brains.DefaultID())
	}
	// Emptying the registry clears the default.
	brains.remove("a")
	if brains.DefaultID() != "" || len(brains.List()) != 0 {
		t.Fatalf("registry not empty: default=%q list=%v", brains.DefaultID(), brains.List())
	}
}

// A nil provider or one with no brains boots an empty registry: the runtime can
// start with no brain, and brain runs fail with a clear error until one is
// registered (e.g. via a Brain CRD through SetBrains).
func TestLoadBrainsAllowsEmpty(t *testing.T) {
	for _, provider := range []BrainProvider{nil, staticBrains{}} {
		brains, err := loadBrains(context.Background(), provider)
		if err != nil {
			t.Fatalf("empty provider: %v", err)
		}
		if brains.DefaultID() != "" {
			t.Fatalf("empty registry default = %q, want \"\"", brains.DefaultID())
		}
		if _, err := brains.Resolve(""); err == nil {
			t.Fatal("resolving against an empty registry should error")
		}
		if got := brains.List(); len(got) != 0 {
			t.Fatalf("empty registry list = %v", got)
		}
	}
}
