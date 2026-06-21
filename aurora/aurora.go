package aurora

import (
	"context"

	"aurora-capcompute/internal/agent"
)

func NewRuntime(ctx context.Context, config Config) (Runtime, error) {
	return agent.NewRuntime(ctx, config)
}

func ValidateManifest(m Manifest, provider DispatcherProvider) (Manifest, error) {
	return agent.ValidateManifest(m, provider)
}

func EffectiveManifest(base Manifest, overrides []CapabilityConfig, provider DispatcherProvider) (Manifest, error) {
	return agent.EffectiveManifest(base, overrides, provider)
}
