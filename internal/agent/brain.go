package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const DefaultBrainID = "aurora-default@1"

type BrainSource struct {
	ID   string
	Wasm []byte
}

type BrainArtifact struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type BrainProvider interface {
	DefaultID() string
	List(context.Context) ([]BrainSource, error)
}

// loadedBrains is the runtime's brain registry. It is mutable (brains can be
// added, replaced, or removed at runtime via Runtime.SetBrains) and guards its
// own state with an RWMutex, because it is read on paths that do not hold the
// Runtime mutex (e.g. CreateThread).
type loadedBrains struct {
	mu        sync.RWMutex
	defaultID string
	sources   map[string]BrainSource
	artifacts map[string]BrainArtifact
}

// digestOf returns the canonical content digest recorded for a brain's wasm.
func digestOf(wasm []byte) string {
	sum := sha256.Sum256(wasm)
	return hex.EncodeToString(sum[:])
}

// loadBrains snapshots the provider into a registry. A nil provider or an empty
// brain list yields an empty registry (no default): the runtime then boots with
// no brain and brain runs fail with a clear error until one is registered. When
// the provider lists at least one brain, its declared default must be present.
func loadBrains(ctx context.Context, provider BrainProvider) (*loadedBrains, error) {
	loaded := &loadedBrains{
		sources:   make(map[string]BrainSource),
		artifacts: make(map[string]BrainArtifact),
	}
	if provider == nil {
		return loaded, nil
	}
	list, err := provider.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list brains: %w", err)
	}
	for _, source := range list {
		id := strings.TrimSpace(source.ID)
		if id == "" || len(source.Wasm) == 0 {
			return nil, fmt.Errorf("%w: brain id and wasm bytes are required", ErrInvalid)
		}
		if _, exists := loaded.sources[id]; exists {
			return nil, fmt.Errorf("%w: duplicate brain %q", ErrInvalid, id)
		}
		wasm := append([]byte(nil), source.Wasm...)
		loaded.sources[id] = BrainSource{ID: id, Wasm: wasm}
		loaded.artifacts[id] = BrainArtifact{ID: id, Digest: digestOf(wasm)}
	}
	if len(loaded.sources) > 0 {
		defaultID := strings.TrimSpace(provider.DefaultID())
		if defaultID == "" {
			return nil, fmt.Errorf("%w: default brain id is required", ErrInvalid)
		}
		if _, ok := loaded.sources[defaultID]; !ok {
			return nil, fmt.Errorf("%w: default brain %q is not registered", ErrInvalid, defaultID)
		}
		loaded.defaultID = defaultID
	}
	return loaded, nil
}

func (r *loadedBrains) DefaultID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultID
}

func (r *loadedBrains) Resolve(id string) (BrainArtifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if strings.TrimSpace(id) == "" {
		id = r.defaultID
	}
	if id == "" {
		return BrainArtifact{}, fmt.Errorf("%w: no brain registered", ErrInvalid)
	}
	artifact, ok := r.artifacts[id]
	if !ok {
		return BrainArtifact{}, fmt.Errorf("%w: brain %q is not registered", ErrInvalid, id)
	}
	return artifact, nil
}

func (r *loadedBrains) Source(id string) (BrainSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if strings.TrimSpace(id) == "" {
		id = r.defaultID
	}
	if id == "" {
		return BrainSource{}, fmt.Errorf("%w: no brain registered", ErrInvalid)
	}
	source, ok := r.sources[id]
	if !ok {
		return BrainSource{}, fmt.Errorf("%w: brain %q is not registered", ErrInvalid, id)
	}
	source.Wasm = append([]byte(nil), source.Wasm...)
	return source, nil
}

func (r *loadedBrains) List() []BrainArtifact {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BrainArtifact, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// digests returns the current id→digest map, for diffing a desired brain set
// against what is registered.
func (r *loadedBrains) digests() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.artifacts))
	for id, artifact := range r.artifacts {
		out[id] = artifact.Digest
	}
	return out
}

// put registers or replaces a brain. The caller has already validated id/wasm.
func (r *loadedBrains) put(id string, wasm []byte, digest string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[id] = BrainSource{ID: id, Wasm: wasm}
	r.artifacts[id] = BrainArtifact{ID: id, Digest: digest}
	r.recomputeDefaultLocked()
}

// remove unregisters a brain.
func (r *loadedBrains) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sources, id)
	delete(r.artifacts, id)
	r.recomputeDefaultLocked()
}

// recomputeDefaultLocked keeps a stable default: the existing one if it still
// exists, else the lexicographically first registered brain, else empty.
func (r *loadedBrains) recomputeDefaultLocked() {
	if _, ok := r.sources[r.defaultID]; ok && r.defaultID != "" {
		return
	}
	ids := make([]string, 0, len(r.sources))
	for id := range r.sources {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		r.defaultID = ""
		return
	}
	sort.Strings(ids)
	r.defaultID = ids[0]
}
