package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrainRegistryPinsArtifactDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.wasm")
	if err := os.WriteFile(path, []byte("wasm"), 0o600); err != nil {
		t.Fatalf("write brain: %v", err)
	}
	registry, err := NewBrainRegistry("brain@1", map[string]string{"brain@1": path})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	artifact, err := registry.Resolve("brain@1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if artifact.Digest == "" || artifact.Path != path {
		t.Fatalf("artifact = %+v", artifact)
	}
	if _, err := registry.Resolve("missing"); err == nil {
		t.Fatal("missing brain was accepted")
	}
}
