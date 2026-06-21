package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

const DefaultBrainID = "aurora-default@1"

type BrainArtifact struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type BrainRegistry struct {
	defaultID string
	artifacts map[string]BrainArtifact
}

func NewBrainRegistry(defaultID string, paths map[string]string) (*BrainRegistry, error) {
	defaultID = strings.TrimSpace(defaultID)
	if defaultID == "" {
		defaultID = DefaultBrainID
	}
	registry := &BrainRegistry{defaultID: defaultID, artifacts: make(map[string]BrainArtifact)}
	for id, path := range paths {
		id = strings.TrimSpace(id)
		path = strings.TrimSpace(path)
		if id == "" || path == "" {
			return nil, fmt.Errorf("%w: brain id and path are required", ErrInvalid)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read brain %q: %w", id, err)
		}
		sum := sha256.Sum256(raw)
		registry.artifacts[id] = BrainArtifact{
			ID: id, Path: path, Digest: hex.EncodeToString(sum[:]),
		}
	}
	if _, ok := registry.artifacts[defaultID]; !ok {
		return nil, fmt.Errorf("%w: default brain %q is not registered", ErrInvalid, defaultID)
	}
	return registry, nil
}

func SingleBrainRegistry(path string) (*BrainRegistry, error) {
	return NewBrainRegistry(DefaultBrainID, map[string]string{DefaultBrainID: path})
}

func (r *BrainRegistry) DefaultID() string {
	if r == nil {
		return ""
	}
	return r.defaultID
}

func (r *BrainRegistry) Resolve(id string) (BrainArtifact, error) {
	if r == nil {
		return BrainArtifact{}, fmt.Errorf("%w: brain registry is required", ErrInvalid)
	}
	if strings.TrimSpace(id) == "" {
		id = r.defaultID
	}
	artifact, ok := r.artifacts[id]
	if !ok {
		return BrainArtifact{}, fmt.Errorf("%w: brain %q is not registered", ErrInvalid, id)
	}
	return artifact, nil
}

func (r *BrainRegistry) List() []BrainArtifact {
	if r == nil {
		return nil
	}
	out := make([]BrainArtifact, 0, len(r.artifacts))
	for _, artifact := range r.artifacts {
		out = append(out, artifact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
