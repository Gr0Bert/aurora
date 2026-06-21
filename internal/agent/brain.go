package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
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

type loadedBrains struct {
	defaultID string
	sources   map[string]BrainSource
	artifacts map[string]BrainArtifact
}

func loadBrains(ctx context.Context, provider BrainProvider) (*loadedBrains, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: brain provider is required", ErrInvalid)
	}
	defaultID := strings.TrimSpace(provider.DefaultID())
	if defaultID == "" {
		return nil, fmt.Errorf("%w: default brain id is required", ErrInvalid)
	}
	list, err := provider.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list brains: %w", err)
	}
	loaded := &loadedBrains{
		defaultID: defaultID,
		sources:   make(map[string]BrainSource, len(list)),
		artifacts: make(map[string]BrainArtifact, len(list)),
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
		sum := sha256.Sum256(wasm)
		loaded.sources[id] = BrainSource{ID: id, Wasm: wasm}
		loaded.artifacts[id] = BrainArtifact{ID: id, Digest: hex.EncodeToString(sum[:])}
	}
	if _, ok := loaded.sources[defaultID]; !ok {
		return nil, fmt.Errorf("%w: default brain %q is not registered", ErrInvalid, defaultID)
	}
	return loaded, nil
}

func (r *loadedBrains) DefaultID() string {
	return r.defaultID
}

func (r *loadedBrains) Resolve(id string) (BrainArtifact, error) {
	if strings.TrimSpace(id) == "" {
		id = r.defaultID
	}
	artifact, ok := r.artifacts[id]
	if !ok {
		return BrainArtifact{}, fmt.Errorf("%w: brain %q is not registered", ErrInvalid, id)
	}
	return artifact, nil
}

func (r *loadedBrains) Source(id string) (BrainSource, error) {
	if strings.TrimSpace(id) == "" {
		id = r.defaultID
	}
	source, ok := r.sources[id]
	if !ok {
		return BrainSource{}, fmt.Errorf("%w: brain %q is not registered", ErrInvalid, id)
	}
	source.Wasm = append([]byte(nil), source.Wasm...)
	return source, nil
}

func (r *loadedBrains) List() []BrainArtifact {
	out := make([]BrainArtifact, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
