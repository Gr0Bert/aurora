package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aurora-capcompute/capcompute/dispatcher"
)

type testDispatchers struct {
	normalized []string
}

func (p *testDispatchers) Normalize(name string, settings json.RawMessage) (json.RawMessage, error) {
	if name == "unknown" {
		return nil, fmt.Errorf("unsupported capability")
	}
	p.normalized = append(p.normalized, name)
	if len(settings) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return append(json.RawMessage(nil), settings...), nil
}

func (*testDispatchers) NewDispatcher(context.Context, RunContext, Manifest) (dispatcher.Dispatcher[RunContext], error) {
	return nil, nil
}

func (*testDispatchers) IsSubset(name string, parent, child json.RawMessage) error {
	return nil
}

func TestValidateManifestUsesInjectedProvider(t *testing.T) {
	provider := &testDispatchers{}
	manifest, err := ValidateManifest(Manifest{
		Version: LegacyManifestVersion,
		Capabilities: []CapabilityConfig{{
			Name: " custom.call ",
		}},
	}, provider)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if manifest.Version != ManifestVersion || manifest.Capabilities[0].Name != "custom.call" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if string(manifest.Capabilities[0].Settings) != "{}" {
		t.Fatalf("settings = %s", manifest.Capabilities[0].Settings)
	}
}

func TestValidateManifestRejectsMissingProviderAndUnknownCapability(t *testing.T) {
	if _, err := ValidateManifest(Manifest{Version: ManifestVersion}, nil); err == nil {
		t.Fatal("expected missing provider error")
	}
	if _, err := ValidateManifest(Manifest{
		Version:      ManifestVersion,
		Capabilities: []CapabilityConfig{{Name: "unknown"}},
	}, &testDispatchers{}); err == nil {
		t.Fatal("expected unsupported capability error")
	}
}
