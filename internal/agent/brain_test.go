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
		{name: "nil"},
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
