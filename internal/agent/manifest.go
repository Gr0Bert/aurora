package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aurora-capcompute/internal/host"
	"aurora-dispatchers/llm"
	"aurora-dispatchers/mcp"
	dispatcherregistry "aurora-dispatchers/registry"
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

type InternetSettings = dispatcherregistry.InternetSettings
type MCPSettings = dispatcherregistry.MCPSettings

func DefaultManifest(allowlist string) (Manifest, error) {
	settings := InternetSettings{}
	for _, entry := range strings.Split(allowlist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		method, origin, ok := strings.Cut(entry, ":")
		if !ok || !strings.EqualFold(method, "GET") {
			return Manifest{}, fmt.Errorf("%w: only GET allowlist entries are supported", ErrInvalid)
		}
		settings.Allow = append(settings.Allow, origin)
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Version: ManifestVersion}
	if len(settings.Allow) > 0 {
		manifest.Capabilities = []CapabilityConfig{{Name: "internet.read", Settings: raw}}
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) (Manifest, error) {
	return validateManifestWithRegistry(manifest, dispatcherregistry.Default())
}

func validateManifestWithRegistry(manifest Manifest, registry *dispatcherregistry.Registry) (Manifest, error) {
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
		normalized, err := registry.Normalize(capability.Name, capability.Settings)
		if err != nil {
			return Manifest{}, fmt.Errorf("%w: %s settings: %v", ErrInvalid, capability.Name, err)
		}
		capability.Settings = normalized
	}
	return cloneManifest(manifest), nil
}

func EffectiveManifest(base Manifest, overrides []CapabilityConfig) (Manifest, error) {
	return effectiveManifestWithRegistry(base, overrides, dispatcherregistry.Default())
}

func effectiveManifestWithRegistry(
	base Manifest,
	overrides []CapabilityConfig,
	registry *dispatcherregistry.Registry,
) (Manifest, error) {
	effective := cloneManifest(base)
	index := make(map[string]int, len(effective.Capabilities))
	for i, capability := range effective.Capabilities {
		index[capability.Name] = i
	}
	for _, override := range overrides {
		overrideManifest, err := validateManifestWithRegistry(Manifest{
			Version:      ManifestVersion,
			SystemPrompt: effective.SystemPrompt,
			Capabilities: []CapabilityConfig{override},
		}, registry)
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

func DispatcherConfig(manifest Manifest, llmClient llm.Client) (host.Config, error) {
	return DispatcherConfigWithMCP(context.Background(), manifest, llmClient, nil)
}

func DispatcherConfigWithMCP(
	ctx context.Context,
	manifest Manifest,
	llmClient llm.Client,
	servers map[string]mcp.ServerConfig,
) (host.Config, error) {
	return dispatcherConfigWithRegistry(ctx, manifest, llmClient, servers, dispatcherregistry.Default())
}

func dispatcherConfigWithRegistry(
	ctx context.Context,
	manifest Manifest,
	llmClient llm.Client,
	servers map[string]mcp.ServerConfig,
	registry *dispatcherregistry.Registry,
) (host.Config, error) {
	entries := make([]dispatcherregistry.Entry, 0, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		entries = append(entries, dispatcherregistry.Entry{
			Name: capability.Name, Settings: capability.Settings,
		})
	}
	config, err := registry.Build(ctx, entries, dispatcherregistry.Services{
		LLM: llmClient, MCPServers: servers,
	})
	if err != nil {
		return host.Config{}, fmt.Errorf("%w: build dispatchers: %v", ErrInvalid, err)
	}
	return config, nil
}

func decodeInternetSettings(raw json.RawMessage) (InternetSettings, error) {
	normalized, err := dispatcherregistry.Default().Normalize("internet.read", raw)
	if err != nil {
		return InternetSettings{}, err
	}
	var settings InternetSettings
	if err := json.Unmarshal(normalized, &settings); err != nil {
		return InternetSettings{}, err
	}
	return settings, nil
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
