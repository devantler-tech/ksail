package versionresolver_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
)

// mockResolver implements Resolver for testing without OCI registry access.
type mockResolver struct {
	versions []versionresolver.Version
	err      error
}

func (m *mockResolver) ListVersions(
	_ context.Context, _ string,
) ([]versionresolver.Version, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.versions, nil
}

func parseTags(tags []string) []versionresolver.Version {
	return versionresolver.ParseTags(tags)
}

func TestComputeUpgradePath_StableOnly(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{
			"v1.35.0", "v1.35.1-alpha.1", "v1.35.1-beta.0",
			"v1.35.1", "v1.35.2-rc.1", "v1.35.2", "v1.36.0",
		}),
	}

	steps, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v1.35.0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only include stable versions: v1.35.1, v1.35.2, v1.36.0
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	expected := []string{"v1.35.1", "v1.35.2", "v1.36.0"}
	for i, step := range steps {
		if step.Version.Original != expected[i] {
			t.Errorf("step %d = %q, want %q", i, step.Version.Original, expected[i])
		}
	}
}

func TestComputeUpgradePath_AscendingOrder(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{"v1.36.0", "v1.35.2", "v1.35.1", "v1.35.0"}),
	}

	steps, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v1.35.0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := 1; i < len(steps); i++ {
		if !steps[i-1].Version.Less(steps[i].Version) {
			t.Errorf("steps not ascending: %s >= %s",
				steps[i-1].Version.Original, steps[i].Version.Original)
		}
	}
}

func TestComputeUpgradePath_K3sSuffix(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{
			"v1.35.0-k3s1", "v1.35.0-k3s2",
			"v1.35.1-k3s1", "v1.35.1-k3s2",
			"v1.35.0", "v1.35.1", // plain tags without suffix
		}),
	}

	steps, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "rancher/k3s", "v1.35.0-k3s1", "k3s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include: v1.35.0-k3s2, v1.35.1-k3s1, v1.35.1-k3s2
	// (filtered to k3s suffix, newer than v1.35.0-k3s1, ascending)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %v", len(steps), steps)
	}

	expected := []string{"v1.35.0-k3s2", "v1.35.1-k3s1", "v1.35.1-k3s2"}
	for i, step := range steps {
		if step.Version.Original != expected[i] {
			t.Errorf("step %d = %q, want %q", i, step.Version.Original, expected[i])
		}
	}
}

func TestComputeUpgradePath_AlreadyLatest(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{"v1.35.0", "v1.35.1"}),
	}

	_, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v1.35.1", "")
	if err == nil {
		t.Fatal("expected error for already-latest version")
	}
}

func TestComputeUpgradePath_ImageRef(t *testing.T) {
	t.Parallel()

	resolver := &mockResolver{
		versions: parseTags([]string{"v1.35.0", "v1.35.1"}),
	}

	steps, err := versionresolver.ComputeUpgradePath(
		context.Background(), resolver, "kindest/node", "v1.35.0", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}

	wantRef := "kindest/node:v1.35.1"
	if steps[0].ImageRef != wantRef {
		t.Errorf("imageRef = %q, want %q", steps[0].ImageRef, wantRef)
	}
}
