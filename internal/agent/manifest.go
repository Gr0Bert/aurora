package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"capcompute/dispatcher"
)

const (
	LegacyManifestVersion = 1
	ManifestVersion       = 2
)

type Manifest struct {
	Version      int                `json:"version"`
	Brain        string             `json:"brain,omitempty"`
	SystemPrompt string             `json:"system_prompt,omitempty"`
	Capabilities []CapabilityConfig `json:"capabilities"`
}

type CapabilityConfig struct {
	Name     string          `json:"name"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

type DispatcherProvider interface {
	Normalize(name string, settings json.RawMessage) (json.RawMessage, error)
	NewDispatcher(context.Context, RunContext, Manifest) (dispatcher.Dispatcher[RunContext], error)
}

func ValidateManifest(manifest Manifest, provider DispatcherProvider) (Manifest, error) {
	if provider == nil {
		return Manifest{}, fmt.Errorf("%w: dispatcher provider is required", ErrInvalid)
	}
	if manifest.Version == LegacyManifestVersion {
		manifest.Version = ManifestVersion
	}
	if manifest.Version != ManifestVersion {
		return Manifest{}, fmt.Errorf("%w: manifest version must be %d", ErrInvalid, ManifestVersion)
	}
	manifest.SystemPrompt = strings.TrimSpace(manifest.SystemPrompt)
	manifest.Brain = strings.TrimSpace(manifest.Brain)
	seen := make(map[string]struct{}, len(manifest.Capabilities))
	for i := range manifest.Capabilities {
		capability := &manifest.Capabilities[i]
		capability.Name = strings.TrimSpace(capability.Name)
		if capability.Name == "" {
			return Manifest{}, fmt.Errorf("%w: capability %d name is required", ErrInvalid, i)
		}
		if _, exists := seen[capability.Name]; exists {
			return Manifest{}, fmt.Errorf("%w: duplicate capability %q", ErrInvalid, capability.Name)
		}
		seen[capability.Name] = struct{}{}
		normalized, err := provider.Normalize(capability.Name, capability.Settings)
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s settings: %v", ErrInvalid, capability.Name, err)
		}
		capability.Settings = append(json.RawMessage(nil), normalized...)
	}
	return cloneManifest(manifest), nil
}

func EffectiveManifest(base Manifest, overrides []CapabilityConfig, provider DispatcherProvider) (Manifest, error) {
	effective := cloneManifest(base)
	index := make(map[string]int, len(effective.Capabilities))
	for i, capability := range effective.Capabilities {
		index[capability.Name] = i
	}
	for _, override := range overrides {
		overrideManifest, err := ValidateManifest(Manifest{
			Version:      ManifestVersion,
			SystemPrompt: effective.SystemPrompt,
			Brain:        effective.Brain,
			Capabilities: []CapabilityConfig{override},
		}, provider)
		if err != nil {
			return Manifest{}, err
		}
		validated := overrideManifest.Capabilities[0]
		if i, exists := index[validated.Name]; exists {
			effective.Capabilities[i] = validated
		} else {
			index[validated.Name] = len(effective.Capabilities)
			effective.Capabilities = append(effective.Capabilities, validated)
		}
	}
	return effective, nil
}

func cloneManifest(manifest Manifest) Manifest {
	out := manifest
	out.Capabilities = make([]CapabilityConfig, len(manifest.Capabilities))
	for i, capability := range manifest.Capabilities {
		out.Capabilities[i] = capability
		out.Capabilities[i].Settings = append(json.RawMessage(nil), capability.Settings...)
	}
	return out
}
