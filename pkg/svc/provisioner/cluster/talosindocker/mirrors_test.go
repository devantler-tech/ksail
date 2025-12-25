package talosindockerprovisioner_test

import (
	"testing"

	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
)

func TestGenerateMirrorPatchYAML_EmptySpecs(t *testing.T) {
	t.Parallel()

	result := talosindockerprovisioner.GenerateMirrorPatchYAML(nil)
	assert.Empty(t, result)

	result = talosindockerprovisioner.GenerateMirrorPatchYAML([]registry.MirrorSpec{})
	assert.Empty(t, result)
}

func TestGenerateMirrorPatchYAML_SingleMirror(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry.example.com"},
	}

	result := talosindockerprovisioner.GenerateMirrorPatchYAML(specs)

	assert.Contains(t, result, "machine:")
	assert.Contains(t, result, "registries:")
	assert.Contains(t, result, "mirrors:")
	assert.Contains(t, result, "docker.io:")
	assert.Contains(t, result, "endpoints:")
	assert.Contains(t, result, "https://registry.example.com")
	assert.Contains(t, result, "https://registry-1.docker.io") // fallback
	assert.Contains(t, result, "overridePath: true")
}

func TestGenerateMirrorPatchYAML_MultipleMirrors(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "ghcr.io", Remote: "https://ghcr.example.com"},
		{Host: "docker.io", Remote: "https://docker.example.com"},
	}

	result := talosindockerprovisioner.GenerateMirrorPatchYAML(specs)

	// Verify both hosts are present
	assert.Contains(t, result, "docker.io:")
	assert.Contains(t, result, "ghcr.io:")
	assert.Contains(t, result, "https://ghcr.example.com")
	assert.Contains(t, result, "https://docker.example.com")

	// Verify sorting (docker.io should come before ghcr.io alphabetically)
	dockerIdx := indexOf(result, "docker.io:")
	ghcrIdx := indexOf(result, "ghcr.io:")
	assert.Less(t, dockerIdx, ghcrIdx, "mirrors should be sorted alphabetically")
}

func TestGenerateMirrorPatchYAML_NoRemoteFallsBackToDefault(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "quay.io", Remote: ""},
	}

	result := talosindockerprovisioner.GenerateMirrorPatchYAML(specs)

	assert.Contains(t, result, "quay.io:")
	assert.Contains(t, result, "https://quay.io")
}

func TestGenerateMirrorPatchYAML_SkipsEmptyHosts(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "", Remote: "https://example.com"},
		{Host: "docker.io", Remote: "https://docker.example.com"},
	}

	result := talosindockerprovisioner.GenerateMirrorPatchYAML(specs)

	assert.Contains(t, result, "docker.io:")
	assert.NotContains(t, result, "https://example.com")
}

// indexOf returns the index of the first occurrence of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}
