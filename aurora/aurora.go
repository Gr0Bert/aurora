package aurora

import (
	"context"

	"aurora-capcompute/internal/agent"
)

func NewRuntime(ctx context.Context, config Config) (Runtime, error) {
	return agent.NewRuntime(ctx, config)
}

func NewBrainRegistry(defaultID string, paths map[string]string) (*BrainRegistry, error) {
	return agent.NewBrainRegistry(defaultID, paths)
}

func SingleBrainRegistry(path string) (*BrainRegistry, error) {
	return agent.SingleBrainRegistry(path)
}

func DefaultManifest(allowlist string) (Manifest, error) {
	return agent.DefaultManifest(allowlist)
}

func ValidateManifest(m Manifest) (Manifest, error) {
	return agent.ValidateManifest(m)
}
